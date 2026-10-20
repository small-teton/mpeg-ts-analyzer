package tsparser

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

const jsonSchemaVersion = "1.0"

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
	Programs              []ReportProgram           `json:"programs"`
	SelectedProgramNumber *uint16                   `json:"selected_program_number,omitempty"`
	Compliance            *ReportCompliance         `json:"compliance,omitempty"`
	ContinuityCounter     *ReportContinuity         `json:"continuity_counter,omitempty"`
	TimestampAnomalies    *ReportTimestampAnomalies `json:"timestamp_anomalies,omitempty"`
	PCRJitter             *ReportPCRJitter          `json:"pcr_jitter,omitempty"`
	Bitrate               *ReportBitrate            `json:"bitrate,omitempty"`
	Warnings              []ReportWarning           `json:"warnings"`
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
	MaxPCRInterval  ReportCheck `json:"max_pcr_interval"`
	MaxPCRPTSDTSGap ReportCheck `json:"max_pcr_pts_dts_gap"`
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

type ReportPCRDiscontinuity struct {
	OffsetBytes int64   `json:"offset_bytes"`
	JumpMs      float64 `json:"jump_ms"`
	FromSegment int     `json:"from_segment"`
	ToSegment   int     `json:"to_segment"`
}

type ReportPCRJitter struct {
	Status                  string                   `json:"status"`
	Reason                  string                   `json:"reason,omitempty"`
	SampleCount             int                      `json:"sample_count"`
	SegmentCount            int                      `json:"segment_count"`
	MeasuredSampleCount     int                      `json:"measured_sample_count"`
	MaxJitterNs             *float64                 `json:"max_jitter_ns,omitempty"`
	MaxJitterOffsetBytes    *int64                   `json:"max_jitter_offset_bytes,omitempty"`
	MinJitterNs             *float64                 `json:"min_jitter_ns,omitempty"`
	MinJitterOffsetBytes    *int64                   `json:"min_jitter_offset_bytes,omitempty"`
	AverageAbsoluteJitterNs *float64                 `json:"average_absolute_jitter_ns,omitempty"`
	Within500NsPercent      *float64                 `json:"within_500ns_percent,omitempty"`
	Discontinuities         []ReportPCRDiscontinuity `json:"discontinuities"`
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

func writeJSONReport(w io.Writer, report *AnalysisReport) error {
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
	for _, info := range pmt.ProgramInfos() {
		r.ElementaryStreams = append(r.ElementaryStreams, ReportElementaryStream{
			PID: reportPID(info.elementaryPid), StreamType: fmt.Sprintf("0x%02x", info.streamType), StreamTypeDescription: StreamTypeString(info.streamType),
		})
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
	return &ReportCompliance{MaxPCRInterval: reportCheck(r.maxPcrInterval), MaxPCRPTSDTSGap: reportCheck(r.maxPcrPtsGap)}
}

func reportContinuity(s *continuityCounterSummary) *ReportContinuity {
	r := &ReportContinuity{Status: "ok", PIDs: []ReportContinuityPID{}}
	pids := make([]uint16, 0, len(s.counts))
	for pid := range s.counts {
		pids = append(pids, pid)
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	for _, pid := range pids {
		label := s.labels[pid]
		if label == "" {
			label = "unknown"
		}
		count := s.counts[pid]
		r.TotalErrorCount += count
		r.PIDs = append(r.PIDs, ReportContinuityPID{PID: reportPID(pid), Label: label, ErrorCount: count})
	}
	if r.TotalErrorCount > 0 {
		r.Status = "errors"
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
	r := &ReportPCRJitter{Status: "skipped", SampleCount: len(j.samples), Discontinuities: []ReportPCRDiscontinuity{}}
	if len(j.samples) < 3 {
		r.Reason = "need at least 3 PCR samples"
		return r
	}
	res := analyzePcrJitter(j.samples)
	r.SegmentCount = res.segments
	r.MeasuredSampleCount = res.measured
	for _, d := range res.discontinuities {
		r.Discontinuities = append(r.Discontinuities, ReportPCRDiscontinuity{d.pos, d.jumpMs, d.fromSeg, d.toSeg})
	}
	if res.measured == 0 {
		r.Reason = "no segment long enough for a stable estimate"
		return r
	}
	if float64(len(res.discontinuities))/float64(len(j.samples)) > pcrJitterGiveUpRatio {
		r.Reason = "too many discontinuities"
		return r
	}
	r.Status = "ok"
	maxAbs := math.Max(math.Abs(res.maxNs), math.Abs(res.minNs))
	if maxAbs > pcrDvbLimitNs {
		r.Status = "ng"
	} else if maxAbs > pcrAccuracyLimitNs {
		r.Status = "warning"
	}
	r.MaxJitterNs, r.MinJitterNs, r.AverageAbsoluteJitterNs, r.Within500NsPercent = &res.maxNs, &res.minNs, &res.avgAbsNs, &res.within500Pct
	r.MaxJitterOffsetBytes, r.MinJitterOffsetBytes = &res.maxPos, &res.minPos
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
