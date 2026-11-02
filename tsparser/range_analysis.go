package tsparser

import (
	"cmp"
	"fmt"
	"io"
	"slices"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// analysisSchemaVersion versions the document written by --output analysis,
// independently of the summary report's schema_version.
const analysisSchemaVersion = "1.0"

// rangeAnalysis is the state of an --output analysis run: every program of the
// first complete PAT/PMT set, analyzed in one pass over the range.
type rangeAnalysis struct {
	fileSize   int64
	programs   []rangeProgram
	scanned    bool
	scanStart  int64
	scanEnd    int64
	scan       scanResult
	continuity *continuityCounterSummary
}

type rangeProgram struct {
	program  Program
	pmtPid   uint16
	pmt      *Pmt
	err      error
	analyzer *programAnalyzer
}

// analyzeAllPrograms reads the PMT of every program announced by the PAT that
// ends at programStart, then analyzes all programs whose PMT was found in a
// single pass. Each program starts collecting after its own PMT, as a
// --program run would; continuity counters are checked for the whole
// multiplex from the earliest of those offsets.
func analyzeAllPrograms(reader io.ReadSeeker, patPrograms []Program, programStart int64, options options.Options, packetSize int, endOffset int64, report *AnalysisReport) error {
	analysis := report.analysis
	var analyzers []*programAnalyzer
	analysis.continuity = &continuityCounterSummary{counts: make(map[uint16]int), labels: make(map[uint16]string)}
	// Programs() already leaves out program_number 0 (the network PID).
	for _, prog := range patPrograms {
		entry := rangeProgram{program: prog, pmtPid: prog.PmtPid}
		pos := programStart
		if _, err := reader.Seek(pos, io.SeekStart); err != nil {
			return fmt.Errorf("file seek error: %w", err)
		}
		pmt := NewPmt()
		err := BufferPsi(reader, &pos, prog.PmtPid, pmt, options, packetSize, endOffset)
		if err == nil {
			err = pmt.Parse()
		}
		if err != nil {
			entry.err = err
		} else {
			entry.pmt = pmt
			entry.analyzer = newProgramAnalyzer(options, pos, prog.ProgramNumber, prog.PmtPid, pmt.PcrPid(), pmt.ProgramInfos(), true)
			analysis.continuity.addProgramLabels(prog.PmtPid, pmt.PcrPid(), pmt.ProgramInfos())
			analyzers = append(analyzers, entry.analyzer)
		}
		analysis.programs = append(analysis.programs, entry)
	}
	if len(analyzers) == 0 {
		report.Status = "incomplete"
		report.Reason = "no_program_analyzed"
		return nil
	}

	pos := slices.MinFunc(analyzers, func(a, b *programAnalyzer) int { return cmp.Compare(a.start, b.start) }).start
	analysis.scanStart = pos
	if _, err := reader.Seek(pos, io.SeekStart); err != nil {
		return fmt.Errorf("file seek error: %w", err)
	}
	scan, err := scanPrograms(reader, &pos, analyzers, analysis.continuity, options, packetSize, endOffset, true)
	if err != nil {
		return err
	}
	for _, analyzer := range analyzers {
		analyzer.finish()
	}
	analysis.scanned = true
	analysis.scanEnd = pos
	analysis.scan = scan
	if scan.truncatedBytes > 0 {
		report.Warnings = append(report.Warnings, ReportWarning{scan.truncatedPos, truncatedPacketMessage(scan.truncatedBytes)})
	}
	report.Status = "complete"
	report.Reason = ""
	report.Error = ""
	return nil
}

// RangeAnalysisReport is the document written by --output analysis. It shares
// the envelope of AnalysisReport (schema version, input, status, reason,
// error, warnings) and carries results for every program of the range.
type RangeAnalysisReport struct {
	SchemaVersion            string                  `json:"schema_version"`
	Output                   string                  `json:"output"`
	Input                    RangeAnalysisInput      `json:"input"`
	Status                   string                  `json:"status"`
	Reason                   string                  `json:"reason,omitempty"`
	Error                    string                  `json:"error,omitempty"`
	AnalysisStartOffsetBytes *int64                  `json:"analysis_start_offset_bytes,omitempty"`
	AnalysisEndOffsetBytes   *int64                  `json:"analysis_end_offset_bytes,omitempty"`
	ContinuityCounter        *ReportContinuity       `json:"continuity_counter,omitempty"`
	ContinuityEvents         []ReportContinuityEvent `json:"continuity_events"`
	Programs                 []RangeAnalysisProgram  `json:"programs"`
	Warnings                 []ReportWarning         `json:"warnings"`
}

type RangeAnalysisInput struct {
	Path            string `json:"path"`
	FileSizeBytes   int64  `json:"file_size_bytes"`
	PacketSizeBytes int    `json:"packet_size_bytes,omitempty"`
	OffsetBytes     int64  `json:"offset_bytes"`
	LimitBytes      int64  `json:"limit_bytes,omitempty"`
}

type ReportContinuityEvent struct {
	OffsetBytes int64  `json:"offset_bytes"`
	PID         string `json:"pid"`
	ExpectedCC  uint8  `json:"expected_cc"`
	ActualCC    uint8  `json:"actual_cc"`
}

// RangeAnalysisProgram is one program's results. Continuity counters are
// reported once for the multiplex, not per program.
type RangeAnalysisProgram struct {
	ReportProgram
	AnalysisStartOffsetBytes *int64                    `json:"analysis_start_offset_bytes,omitempty"`
	AverageBitrateBPS        *float64                  `json:"average_bitrate_bps,omitempty"`
	Compliance               *ReportCompliance         `json:"compliance,omitempty"`
	TimestampAnomalies       *ReportTimestampAnomalies `json:"timestamp_anomalies,omitempty"`
	PCRJitter                *ReportPCRJitter          `json:"pcr_jitter,omitempty"`
	Bitrate                  *ReportBitrate            `json:"bitrate,omitempty"`
	Series                   *ReportSeries             `json:"series,omitempty"`
}

// ReportSeries holds a program's time series. elapsed_ms is PCR time since the
// program's first PCR in the range; it does not advance across a PCR
// discontinuity, so it matches the 1-second bitrate windows.
type ReportSeries struct {
	PCR             []ReportPCRPoint       `json:"pcr"`
	PES             []ReportPESPoint       `json:"pes"`
	TimestampEvents []ReportTimestampEvent `json:"timestamp_events"`
	PCRJitter       []ReportJitterPoint    `json:"pcr_jitter"`
	Bitrate         ReportBitrateWindows   `json:"bitrate"`
}

type ReportPCRPoint struct {
	OffsetBytes   int64    `json:"offset_bytes"`
	ElapsedMs     float64  `json:"elapsed_ms"`
	PCRMs         float64  `json:"pcr_ms"`
	IntervalMs    *float64 `json:"interval_ms,omitempty"`
	Discontinuity bool     `json:"discontinuity,omitempty"`
}

type ReportPESPoint struct {
	PID         string   `json:"pid"`
	OffsetBytes int64    `json:"offset_bytes"`
	ElapsedMs   *float64 `json:"elapsed_ms,omitempty"`
	PTSMs       *float64 `json:"pts_ms,omitempty"`
	DTSMs       *float64 `json:"dts_ms,omitempty"`
	GapMs       *float64 `json:"pcr_gap_ms,omitempty"`
}

type ReportTimestampEvent struct {
	PID         string `json:"pid"`
	OffsetBytes int64  `json:"offset_bytes"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
}

type ReportJitterPoint struct {
	OffsetBytes int64   `json:"offset_bytes"`
	JitterNs    float64 `json:"jitter_ns"`
}

type ReportBitrateWindows struct {
	WindowSeconds   int                      `json:"window_seconds"`
	DurationSeconds []float64                `json:"duration_seconds"`
	PIDs            []ReportBitrateWindowPID `json:"pids"`
}

type ReportBitrateWindowPID struct {
	PID   string    `json:"pid"`
	Label string    `json:"label"`
	Bytes []int64   `json:"bytes"`
	BPS   []float64 `json:"bps"`
}

// rangeAnalysisReport builds the --output analysis document from the shared
// envelope in report and the analysis state.
func rangeAnalysisReport(report *AnalysisReport) *RangeAnalysisReport {
	analysis := report.analysis
	r := &RangeAnalysisReport{
		SchemaVersion: analysisSchemaVersion,
		Output:        "analysis",
		Input: RangeAnalysisInput{
			Path:            report.Input.Path,
			FileSizeBytes:   analysis.fileSize,
			PacketSizeBytes: report.Input.PacketSizeBytes,
			OffsetBytes:     report.Input.OffsetBytes,
			LimitBytes:      report.Input.LimitBytes,
		},
		Status:           report.Status,
		Reason:           report.Reason,
		Error:            report.Error,
		ContinuityEvents: []ReportContinuityEvent{},
		Programs:         []RangeAnalysisProgram{},
		Warnings:         report.Warnings,
	}
	if analysis.scanned {
		start, end := analysis.scanStart, analysis.scanEnd
		r.AnalysisStartOffsetBytes, r.AnalysisEndOffsetBytes = &start, &end
		r.ContinuityCounter = reportContinuity(analysis.continuity)
		for _, e := range analysis.scan.continuityEvents {
			r.ContinuityEvents = append(r.ContinuityEvents, ReportContinuityEvent{OffsetBytes: e.Pos, PID: reportPID(e.PID), ExpectedCC: e.Expected, ActualCC: e.Actual})
		}
	}
	for _, entry := range analysis.programs {
		p := RangeAnalysisProgram{ReportProgram: reportProgram(entry.program.ProgramNumber, entry.pmtPid, entry.pmt)}
		if entry.err != nil {
			p.Reason = "pmt_parse_failed"
			p.Error = entry.err.Error()
		}
		if a := entry.analyzer; a != nil && analysis.scanned {
			start := a.start
			p.AnalysisStartOffsetBytes = &start
			p.Compliance = reportCompliance(a.complianceReport())
			p.TimestampAnomalies = reportAnomalies(a.anomaly)
			p.PCRJitter = reportJitter(&a.pcrJitter)
			p.Bitrate = reportBitrate(a.bitrate)
			if p.Bitrate.Status == "ok" {
				average := p.Bitrate.ProgramAverageBPS
				p.AverageBitrateBPS = &average
			}
			p.Series = reportSeries(a)
		}
		r.Programs = append(r.Programs, p)
	}
	return r
}

func reportSeries(a *programAnalyzer) *ReportSeries {
	s := &ReportSeries{
		PCR:             []ReportPCRPoint{},
		PES:             []ReportPESPoint{},
		TimestampEvents: []ReportTimestampEvent{},
		PCRJitter:       []ReportJitterPoint{},
		Bitrate:         ReportBitrateWindows{WindowSeconds: 1, DurationSeconds: a.bitrate.windowDurations(), PIDs: []ReportBitrateWindowPID{}},
	}
	for _, p := range a.series.pcr {
		point := ReportPCRPoint{OffsetBytes: p.pos, ElapsedMs: p.elapsedMs, PCRMs: p.pcrMs, Discontinuity: p.discontinuity}
		if p.hasInterval {
			interval := p.intervalMs
			point.IntervalMs = &interval
		}
		s.PCR = append(s.PCR, point)
	}
	// A PES is recorded when it completes, which interleaved PIDs reach out
	// of stream order.
	pes := slices.Clone(a.series.pes)
	slices.SortStableFunc(pes, func(x, y seriesPes) int { return cmp.Compare(x.pos, y.pos) })
	for _, p := range pes {
		point := ReportPESPoint{PID: reportPID(p.pid), OffsetBytes: p.pos}
		point.ElapsedMs = optionalFloat(p.elapsedMs, p.hasElapsed)
		point.PTSMs = optionalFloat(p.ptsMs, p.hasPts)
		point.DTSMs = optionalFloat(p.dtsMs, p.hasDts)
		point.GapMs = optionalFloat(p.gapMs, p.hasGap)
		s.PES = append(s.PES, point)
	}
	events := slices.Clone(a.anomaly.events)
	slices.SortStableFunc(events, func(x, y anomalyEvent) int { return cmp.Compare(x.pos, y.pos) })
	for _, e := range events {
		s.TimestampEvents = append(s.TimestampEvents, ReportTimestampEvent{PID: reportPID(e.pid), OffsetBytes: e.pos, Kind: anomalyKindString(e.kind), Message: e.message})
	}
	// Jitter needs a neighbor on both sides of a sample, as in the summary.
	if len(a.pcrJitter.samples) >= 3 {
		for _, p := range analyzePcrJitter(a.pcrJitter.samples).points {
			s.PCRJitter = append(s.PCRJitter, ReportJitterPoint{OffsetBytes: p.pos, JitterNs: p.ns})
		}
	}
	pids := make([]uint16, 0, len(a.bitrate.stats))
	for pid := range a.bitrate.stats {
		pids = append(pids, pid)
	}
	slices.Sort(pids)
	for _, pid := range pids {
		bytes := a.bitrate.windowBytes(pid)
		bps := make([]float64, len(bytes))
		for idx, n := range bytes {
			bps[idx] = float64(n) * 8 / s.Bitrate.DurationSeconds[idx]
		}
		s.Bitrate.PIDs = append(s.Bitrate.PIDs, ReportBitrateWindowPID{PID: reportPID(pid), Label: a.bitrate.labels[pid], Bytes: bytes, BPS: bps})
	}
	return s
}

func optionalFloat(value float64, ok bool) *float64 {
	if !ok {
		return nil
	}
	return &value
}
