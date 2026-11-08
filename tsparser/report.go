package tsparser

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

const jsonSchemaVersion = "2.0"

// AnalysisReport is the stable, machine-readable result emitted by
// --format json. New fields and enum values may be added within a schema
// version; existing fields are not removed or retyped and existing enum
// meanings do not change without incrementing schema_version.
type AnalysisReport struct {
	SchemaVersion         string                    `json:"schema_version"`
	Input                 ReportInput               `json:"input"`
	Status                string                    `json:"status"`
	Reason                string                    `json:"reason,omitempty"`
	Error                 string                    `json:"error,omitempty"`
	Acquisition           *ReportAcquisition        `json:"acquisition,omitempty"`
	Programs              []ReportProgram           `json:"programs"`
	SelectedProgramNumber *uint16                   `json:"selected_program_number,omitempty"`
	Sync                  *ReportSync               `json:"sync,omitempty"`
	Compliance            *ReportCompliance         `json:"compliance,omitempty"`
	ContinuityCounter     *ReportContinuity         `json:"continuity_counter,omitempty"`
	TransportErrors       *ReportAffectedPackets    `json:"transport_errors,omitempty"`
	ScrambledPackets      *ReportAffectedPackets    `json:"scrambled_packets,omitempty"`
	PSIHealth             *ReportPSIHealth          `json:"psi_health,omitempty"`
	PIDReferences         *ReportPIDReferences      `json:"pid_references,omitempty"`
	PESHeaders            *ReportPESHeaders         `json:"pes_headers,omitempty"`
	PTSRepetition         *ReportPTSRepetition      `json:"pts_repetition,omitempty"`
	Multiplexing          *ReportMultiplexing       `json:"multiplexing,omitempty"`
	TimestampAnomalies    *ReportTimestampAnomalies `json:"timestamp_anomalies,omitempty"`
	PCRJitter             *ReportPCRJitter          `json:"pcr_jitter,omitempty"`
	Bitrate               *ReportBitrate            `json:"bitrate,omitempty"`
	PSITransitions        []ReportPSITransition     `json:"psi_transitions,omitempty"`
	Warnings              []ReportWarning           `json:"warnings"`

	// analysis is set for --output analysis, which writes a RangeAnalysisReport
	// built from this envelope instead of the report itself.
	analysis *rangeAnalysis
}

type ReportInput struct {
	Path            string `json:"path"`
	PacketSizeBytes int    `json:"packet_size_bytes,omitempty"`
	OffsetBytes     int64  `json:"offset_bytes"`
	LimitBytes      int64  `json:"limit_bytes,omitempty"`
}

type ReportProgram struct {
	ProgramNumber     uint16                   `json:"program_number"`
	PMTPID            string                   `json:"pmt_pid"`
	PCRPID            string                   `json:"pcr_pid,omitempty"`
	ElementaryStreams []ReportElementaryStream `json:"elementary_streams"`
	Service           *ReportService           `json:"service,omitempty"`
	Status            string                   `json:"status"`
	Reason            string                   `json:"reason,omitempty"`
	Error             string                   `json:"error,omitempty"`
}

type ReportElementaryStream struct {
	PID                   string `json:"pid"`
	StreamType            string `json:"stream_type"`
	StreamTypeDescription string `json:"stream_type_description"`
}

type ReportCheck struct {
	Status       string   `json:"status"`
	ValueMs      *float64 `json:"value_ms,omitempty"`
	LimitMs      float64  `json:"limit_ms"`
	Observations int      `json:"observations"`
	Reason       string   `json:"reason,omitempty"`
}

type ReportCompliance struct {
	MaxPCRInterval     ReportCheck                 `json:"max_pcr_interval"`
	PCRDiscontinuities ReportPCRDiscontinuityCheck `json:"pcr_discontinuities"`
	MaxPCRPTSDTSGap    ReportCheck                 `json:"max_pcr_pts_dts_gap"`
}

// ReportPCRDiscontinuityCheck classifies every PCR of the program against the
// previous one. Status is ng when at least one discontinuity has no
// discontinuity_indicator (kind backward or forward_jump), skipped with fewer
// than two PCRs, and ok otherwise. Items lists each kind other than continuous
// steps that occurred, in a fixed order: wraparound, declared, pcr_pid_change,
// backward, forward_jump.
type ReportPCRDiscontinuityCheck struct {
	Status          string          `json:"status"`
	Reason          string          `json:"reason,omitempty"`
	PCRCount        int             `json:"pcr_count"`
	UndeclaredCount int             `json:"undeclared_count"`
	JumpLimitMs     float64         `json:"jump_limit_ms"`
	Items           []ReportPCRStep `json:"items"`
}

// ReportPCRStep is how often one kind of PCR step occurred, with the first
// PCR packet of that kind and how far the PCR jumped there.
type ReportPCRStep struct {
	Kind             string  `json:"kind"`
	Count            int     `json:"count"`
	FirstOffsetBytes int64   `json:"first_offset_bytes"`
	FirstJumpMs      float64 `json:"first_jump_ms"`
}

type ReportContinuityPID struct {
	PID        string `json:"pid"`
	Label      string `json:"label"`
	ErrorCount int    `json:"error_count"`
}

type ReportContinuity struct {
	Status          string                `json:"status"`
	TotalErrorCount int                   `json:"total_error_count"`
	PIDs            []ReportContinuityPID `json:"pids"`
}

type ReportTimestampAnomaly struct {
	PID     string `json:"pid"`
	Kind    string `json:"kind"`
	Count   int    `json:"count"`
	Message string `json:"message"`
}

type ReportTimestampAnomalies struct {
	Status string                   `json:"status"`
	Items  []ReportTimestampAnomaly `json:"items"`
}

// ReportPCRDiscontinuity is one boundary between timing segments of the PCR
// jitter estimate. Kind is declared, pcr_pid_change, backward or forward_jump
// (see PCR discontinuities in docs/reference/json-output.md).
type ReportPCRDiscontinuity struct {
	OffsetBytes int64   `json:"offset_bytes"`
	Kind        string  `json:"kind"`
	JumpMs      float64 `json:"jump_ms"`
	FromSegment int     `json:"from_segment"`
	ToSegment   int     `json:"to_segment"`
}

// ReportPCRJitter is the PCR jitter result: the byte-position estimate, the
// delivery assessment that decides whether the estimate can be judged, and the
// resulting status (ok, warning, ng, not_assessable or skipped).
type ReportPCRJitter struct {
	Status   string                  `json:"status"`
	Reason   string                  `json:"reason,omitempty"`
	Estimate ReportPCRJitterEstimate `json:"estimate"`
	Delivery ReportPCRDelivery       `json:"delivery"`
}

// ReportPCRJitterEstimate is the jitter estimated with one model. The value
// fields are present when the estimate was computed (status other than
// skipped).
type ReportPCRJitterEstimate struct {
	Model                   string                   `json:"model"`
	PCRPIDs                 []string                 `json:"pcr_pids"`
	SampleCount             int                      `json:"sample_count"`
	SegmentCount            int                      `json:"segment_count"`
	Observations            int                      `json:"observations"`
	MaxAbsJitterNs          *float64                 `json:"max_abs_jitter_ns,omitempty"`
	MaxAbsJitterOffsetBytes *int64                   `json:"max_abs_jitter_offset_bytes,omitempty"`
	MaxJitterNs             *float64                 `json:"max_jitter_ns,omitempty"`
	MaxJitterOffsetBytes    *int64                   `json:"max_jitter_offset_bytes,omitempty"`
	MinJitterNs             *float64                 `json:"min_jitter_ns,omitempty"`
	MinJitterOffsetBytes    *int64                   `json:"min_jitter_offset_bytes,omitempty"`
	AverageAbsoluteJitterNs *float64                 `json:"average_absolute_jitter_ns,omitempty"`
	Within500NsPercent      *float64                 `json:"within_500ns_percent,omitempty"`
	Discontinuities         []ReportPCRDiscontinuity `json:"discontinuities"`
}

// ReportPCRDelivery is the delivery assessment and its evidence: the
// multiplex rate over 1-second windows of PCR time.
type ReportPCRDelivery struct {
	Assessment             string   `json:"assessment"`
	Reason                 string   `json:"reason"`
	WindowSeconds          int      `json:"window_seconds"`
	WindowCount            int      `json:"window_count"`
	AverageRateBPS         *float64 `json:"average_rate_bps,omitempty"`
	MinWindowRateBPS       *float64 `json:"min_window_rate_bps,omitempty"`
	MaxWindowRateBPS       *float64 `json:"max_window_rate_bps,omitempty"`
	MaxDeviationPercent    *float64 `json:"max_deviation_percent,omitempty"`
	TolerancePercent       float64  `json:"tolerance_percent"`
	PlausibleJitterLimitNs float64  `json:"plausible_jitter_limit_ns"`
}

type ReportBitratePID struct {
	PID        string   `json:"pid"`
	Label      string   `json:"label"`
	AverageBPS float64  `json:"average_bps"`
	Peak1sBPS  *float64 `json:"peak_1s_bps,omitempty"`
}

type ReportBitrate struct {
	Status            string             `json:"status"`
	Reason            string             `json:"reason,omitempty"`
	DurationSeconds   float64            `json:"duration_seconds"`
	SegmentCount      int                `json:"segment_count"`
	PIDs              []ReportBitratePID `json:"pids"`
	NullPID           *ReportBitratePID  `json:"null_pid,omitempty"`
	ProgramAverageBPS float64            `json:"program_average_bps"`
	ProgramPeak1sBPS  *float64           `json:"program_peak_1s_bps,omitempty"`
}

// ReportPSITransition is a PAT or PMT version change applied during the
// analysis. The fields of the table's type are always present; those of the
// other type are omitted.
type ReportPSITransition struct {
	OffsetBytes     int64  `json:"offset_bytes"`
	Table           string `json:"table"`
	PID             string `json:"pid"`
	PreviousVersion uint8  `json:"previous_version"`
	Version         uint8  `json:"version"`
	*ReportPATTransition
	*ReportPMTTransition
}

type ReportPATTransition struct {
	Programs        []ReportPATProgram   `json:"programs"`
	ProgramsAdded   []ReportPATProgram   `json:"programs_added"`
	ProgramsRemoved []ReportPATProgram   `json:"programs_removed"`
	PMTPIDChanges   []ReportPMTPIDChange `json:"pmt_pid_changes"`
}

type ReportPATProgram struct {
	ProgramNumber uint16 `json:"program_number"`
	PMTPID        string `json:"pmt_pid"`
}

type ReportPMTPIDChange struct {
	ProgramNumber  uint16 `json:"program_number"`
	PreviousPMTPID string `json:"previous_pmt_pid"`
	PMTPID         string `json:"pmt_pid"`
}

type ReportPMTTransition struct {
	ProgramNumber            uint16                   `json:"program_number"`
	PreviousPCRPID           string                   `json:"previous_pcr_pid"`
	PCRPID                   string                   `json:"pcr_pid"`
	ElementaryStreams        []ReportElementaryStream `json:"elementary_streams"`
	ElementaryStreamsAdded   []ReportElementaryStream `json:"elementary_streams_added"`
	ElementaryStreamsRemoved []ReportElementaryStream `json:"elementary_streams_removed"`
	StreamTypeChanges        []ReportStreamTypeChange `json:"stream_type_changes"`
}

type ReportStreamTypeChange struct {
	PID                   string `json:"pid"`
	PreviousStreamType    string `json:"previous_stream_type"`
	StreamType            string `json:"stream_type"`
	StreamTypeDescription string `json:"stream_type_description"`
}

type ReportWarning struct {
	OffsetBytes int64  `json:"offset_bytes"`
	Message     string `json:"message"`
}

func newAnalysisReport(path string, offset, limit int64) *AnalysisReport {
	return &AnalysisReport{
		SchemaVersion: jsonSchemaVersion,
		Input:         ReportInput{Path: path, OffsetBytes: offset, LimitBytes: limit},
		Status:        "failed",
		Programs:      []ReportProgram{},
		Warnings:      []ReportWarning{},
	}
}

func writeJSONReport(w io.Writer, report any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func reportPID(pid uint16) string { return fmt.Sprintf("0x%04x", pid) }

func reportProgram(programNumber, pmtPID uint16, pmt *Pmt) ReportProgram {
	r := ReportProgram{ProgramNumber: programNumber, PMTPID: reportPID(pmtPID), Status: "complete", ElementaryStreams: []ReportElementaryStream{}}
	if pmt == nil {
		r.Status = "incomplete"
		return r
	}
	r.PCRPID = reportPID(pmt.PcrPid())
	r.ElementaryStreams = reportElementaryStreams(pmt.ProgramInfos())
	return r
}

func reportElementaryStreams(infos []ProgramInfo) []ReportElementaryStream {
	streams := []ReportElementaryStream{}
	for _, info := range infos {
		streams = append(streams, ReportElementaryStream{
			PID: reportPID(info.elementaryPid), StreamType: fmt.Sprintf("0x%02x", info.streamType), StreamTypeDescription: StreamTypeString(info.streamType),
		})
	}
	return streams
}

func reportPatPrograms(programs []Program) []ReportPATProgram {
	r := []ReportPATProgram{}
	for _, p := range programs {
		r = append(r, ReportPATProgram{ProgramNumber: p.ProgramNumber, PMTPID: reportPID(p.PmtPid)})
	}
	return r
}

// reportPsiTransitions returns nil when no change was applied, so the field is
// left out of reports for streams without table updates.
func reportPsiTransitions(transitions []psiTransition) []ReportPSITransition {
	var r []ReportPSITransition
	for _, tr := range transitions {
		item := ReportPSITransition{OffsetBytes: tr.pos, Table: tr.table, PID: reportPID(tr.pid), PreviousVersion: tr.previousVersion, Version: tr.version}
		if tr.table == "pat" {
			pat := &ReportPATTransition{Programs: reportPatPrograms(tr.programs), ProgramsAdded: reportPatPrograms(tr.programsAdded), ProgramsRemoved: reportPatPrograms(tr.programsRemoved), PMTPIDChanges: []ReportPMTPIDChange{}}
			for _, c := range tr.pmtPidChanges {
				pat.PMTPIDChanges = append(pat.PMTPIDChanges, ReportPMTPIDChange{ProgramNumber: c.programNumber, PreviousPMTPID: reportPID(c.previousPmtPid), PMTPID: reportPID(c.pmtPid)})
			}
			item.ReportPATTransition = pat
		} else {
			pmt := &ReportPMTTransition{
				ProgramNumber: tr.programNumber, PreviousPCRPID: reportPID(tr.previousPcrPid), PCRPID: reportPID(tr.pcrPid),
				ElementaryStreams: reportElementaryStreams(tr.streams), ElementaryStreamsAdded: reportElementaryStreams(tr.streamsAdded),
				ElementaryStreamsRemoved: reportElementaryStreams(tr.streamsRemoved), StreamTypeChanges: []ReportStreamTypeChange{},
			}
			for _, c := range tr.streamTypeChanges {
				pmt.StreamTypeChanges = append(pmt.StreamTypeChanges, ReportStreamTypeChange{
					PID: reportPID(c.pid), PreviousStreamType: fmt.Sprintf("0x%02x", c.previousStreamType), StreamType: fmt.Sprintf("0x%02x", c.streamType), StreamTypeDescription: StreamTypeString(c.streamType),
				})
			}
			item.ReportPMTTransition = pmt
		}
		r = append(r, item)
	}
	return r
}

func reportCheck(c complianceCheck) ReportCheck {
	r := ReportCheck{Status: strings.ToLower(string(c.status())), LimitMs: c.limitMs, Observations: c.observations}
	if c.status() == complianceSkipped {
		r.Reason = c.skippedWhy
	} else {
		v := c.valueMs
		r.ValueMs = &v
	}
	return r
}

func reportCompliance(r complianceReport) *ReportCompliance {
	return &ReportCompliance{
		MaxPCRInterval:     reportCheck(r.maxPcrInterval),
		PCRDiscontinuities: reportPcrDiscontinuities(r.pcrDiscontinuities),
		MaxPCRPTSDTSGap:    reportCheck(r.maxPcrPtsGap),
	}
}

func reportPcrDiscontinuities(d analysis.PCRDiscontinuityResult) ReportPCRDiscontinuityCheck {
	r := ReportPCRDiscontinuityCheck{
		Status: pcrDiscontinuityStatus(d), PCRCount: d.PCRs, UndeclaredCount: d.Undeclared,
		JumpLimitMs: analysis.PCRJumpLimitMs, Items: []ReportPCRStep{},
	}
	if d.Status == analysis.StatusSkipped {
		r.Reason = pcrDiscontinuitiesSkippedWhy
	}
	for _, step := range d.Steps {
		r.Items = append(r.Items, ReportPCRStep{Kind: step.Kind.String(), Count: step.Count, FirstOffsetBytes: step.FirstOffset, FirstJumpMs: step.FirstJumpMs})
	}
	return r
}

func reportContinuity(s *continuityCounterSummary) *ReportContinuity {
	result := s.check.Result()
	r := &ReportContinuity{Status: "ok", TotalErrorCount: result.TotalErrors, PIDs: []ReportContinuityPID{}}
	if result.Status == analysis.StatusErrors {
		r.Status = "errors"
	}
	for _, pid := range result.PIDs {
		r.PIDs = append(r.PIDs, ReportContinuityPID{PID: reportPID(pid.PID), Label: pid.Label, ErrorCount: pid.Errors})
	}
	return r
}

func anomalyKindString(k anomalyKind) string {
	switch k {
	case kindBackward:
		return "backward"
	case kindWraparound:
		return "wraparound"
	case kindGap:
		return "forward_gap"
	default:
		return "dts_after_pts"
	}
}

func reportAnomalies(a *TimestampAnomaly) *ReportTimestampAnomalies {
	r := &ReportTimestampAnomalies{Status: "ok", Items: []ReportTimestampAnomaly{}}
	for _, k := range a.order {
		r.Items = append(r.Items, ReportTimestampAnomaly{PID: reportPID(k.pid), Kind: anomalyKindString(k.kind), Count: a.counts[k], Message: a.first[k]})
	}
	if len(r.Items) > 0 {
		r.Status = "anomalies"
	}
	return r
}

func reportJitter(j *PcrJitter) *ReportPCRJitter {
	e := j.evaluate()
	r := &ReportPCRJitter{
		Status: e.status,
		Reason: e.reason,
		Estimate: ReportPCRJitterEstimate{
			Model: "byte_position", PCRPIDs: []string{}, SampleCount: len(j.samples), Discontinuities: []ReportPCRDiscontinuity{},
		},
		Delivery: reportDelivery(e.delivery),
	}
	for _, pid := range j.pcrPids() {
		r.Estimate.PCRPIDs = append(r.Estimate.PCRPIDs, reportPID(pid))
	}
	if !e.analyzed {
		return r
	}
	res := e.res
	r.Estimate.SegmentCount = res.segments
	r.Estimate.Observations = res.measured
	for _, d := range res.discontinuities {
		r.Estimate.Discontinuities = append(r.Estimate.Discontinuities, ReportPCRDiscontinuity{d.pos, d.kind.String(), d.jumpMs, d.fromSeg, d.toSeg})
	}
	if e.estimated {
		est := &r.Estimate
		est.MaxAbsJitterNs, est.MaxAbsJitterOffsetBytes = &e.maxAbsNs, &e.maxAbsPos
		est.MaxJitterNs, est.MinJitterNs, est.AverageAbsoluteJitterNs, est.Within500NsPercent = &res.maxNs, &res.minNs, &res.avgAbsNs, &res.within500Pct
		est.MaxJitterOffsetBytes, est.MinJitterOffsetBytes = &res.maxPos, &res.minPos
	}
	return r
}

func reportDelivery(d deliveryAssessment) ReportPCRDelivery {
	r := ReportPCRDelivery{
		Assessment: d.assessment, Reason: d.reason, WindowSeconds: deliveryWindowTicks / pcrClockHz, WindowCount: d.windows,
		TolerancePercent: deliveryTolerancePercent, PlausibleJitterLimitNs: pcrPlausibleJitterNs,
	}
	if d.windows > 0 {
		r.AverageRateBPS, r.MinWindowRateBPS, r.MaxWindowRateBPS = &d.averageBps, &d.minBps, &d.maxBps
	}
	if d.windows >= deliveryMinWindows {
		r.MaxDeviationPercent = &d.maxDeviationPct
	}
	return r
}

func reportBitrate(b *BitrateStats) *ReportBitrate {
	seconds := float64(b.elapsedTicks) / float64(pcrClockHz)
	r := &ReportBitrate{Status: "skipped", DurationSeconds: seconds, SegmentCount: b.segments, PIDs: []ReportBitratePID{}}
	if !b.started || seconds <= 0 {
		r.Reason = "no usable PCR time base (need at least 2 PCRs in one segment)"
		return r
	}
	r.Status = "ok"
	full := b.fullBuckets() > 0
	pids := make([]uint16, 0, len(b.programPids))
	for pid := range b.programPids {
		pids = append(pids, pid)
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	var totalBytes int64
	for _, pid := range pids {
		row := ReportBitratePID{PID: reportPID(pid), Label: b.labels[pid], AverageBPS: b.avgBps(pid)}
		if full {
			v := b.peakBps(pid)
			row.Peak1sBPS = &v
		}
		r.PIDs = append(r.PIDs, row)
		totalBytes += b.stats[pid].totalBytes
	}
	if _, ok := b.stats[nullPidValue]; ok {
		row := ReportBitratePID{PID: reportPID(nullPidValue), Label: b.labels[nullPidValue], AverageBPS: b.avgBps(nullPidValue)}
		if full {
			v := b.peakBps(nullPidValue)
			row.Peak1sBPS = &v
		}
		r.NullPID = &row
	}
	r.ProgramAverageBPS = float64(totalBytes) * 8 / seconds
	if full {
		v := b.programPeakBps()
		r.ProgramPeak1sBPS = &v
	}
	return r
}
