package tsparser

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

const (
	twoPrograms188Path = "../sample_data/sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts"
	twoPrograms192Path = "../sample_data/sample_192byte_two_programs_mpeg2_160x120_25fps_mp2.ts"
)

func decodeRangeAnalysis(t *testing.T, path string, opts options.Options) (RangeAnalysisReport, error) {
	t.Helper()
	opts.Format, opts.Output = "json", "analysis"
	var runErr error
	out := captureStdout(t, func() { runErr = ParseTsFile(path, opts) })
	var report RangeAnalysisReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("analysis output did not decode: %v\n%s", err, out)
	}
	return report, runErr
}

func jsonEqual(t *testing.T, got, want any) bool {
	t.Helper()
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	return bytes.Equal(a, b)
}

func TestRangeAnalysisMatchesPerProgramRuns(t *testing.T) {
	for _, path := range []string{twoPrograms188Path, twoPrograms192Path} {
		report, err := decodeRangeAnalysis(t, path, options.Options{})
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if report.SchemaVersion != "2.0" || report.Output != "analysis" || report.Status != "complete" || len(report.Programs) != 2 {
			t.Fatalf("%s: unexpected report header: %+v", path, report)
		}
		info, _ := os.Stat(path)
		if report.Input.FileSizeBytes != info.Size() || report.AnalysisEndOffsetBytes == nil || *report.AnalysisEndOffsetBytes != info.Size() {
			t.Errorf("%s: input/end = %+v / %v", path, report.Input, report.AnalysisEndOffsetBytes)
		}
		earliest := *report.Programs[0].AnalysisStartOffsetBytes
		for _, program := range report.Programs {
			earliest = min(earliest, *program.AnalysisStartOffsetBytes)
		}
		if *report.AnalysisStartOffsetBytes != earliest {
			t.Errorf("%s: analysis start %d, want the earliest program start %d", path, *report.AnalysisStartOffsetBytes, earliest)
		}
		if report.ContinuityCounter == nil || report.ContinuityCounter.Status != "ok" || len(report.ContinuityEvents) != 0 {
			t.Errorf("%s: continuity = %+v %+v", path, report.ContinuityCounter, report.ContinuityEvents)
		}

		for _, program := range report.Programs {
			single, err := decodeJSONReport(t, func() error {
				return ParseTsFile(path, options.Options{Format: "json", Program: int(program.ProgramNumber), DumpBitrate: true, DumpPcrJitter: true})
			})
			if err != nil {
				t.Fatal(err)
			}
			for name, pair := range map[string][2]any{
				"compliance":          {program.Compliance, single.Compliance},
				"timestamp_anomalies": {program.TimestampAnomalies, single.TimestampAnomalies},
				"pcr_jitter":          {program.PCRJitter, single.PCRJitter},
				"bitrate":             {program.Bitrate, single.Bitrate},
				"program":             {program.ReportProgram, single.Programs[0]},
			} {
				if !jsonEqual(t, pair[0], pair[1]) {
					t.Errorf("%s program %d: %s differs from the --program run\nanalysis: %+v\nprogram:  %+v", path, program.ProgramNumber, name, pair[0], pair[1])
				}
			}
			if program.AverageBitrateBPS == nil || *program.AverageBitrateBPS != single.Bitrate.ProgramAverageBPS {
				t.Errorf("%s program %d: average bitrate %v", path, program.ProgramNumber, program.AverageBitrateBPS)
			}

			series := program.Series
			if len(series.PCR) == 0 || len(series.PES) == 0 || len(series.PCRJitter) == 0 || len(series.Bitrate.PIDs) == 0 || series.Bitrate.WindowSeconds != 1 {
				t.Fatalf("%s program %d: empty series %+v", path, program.ProgramNumber, series)
			}
			for i := 1; i < len(series.PCR); i++ {
				if series.PCR[i].ElapsedMs < series.PCR[i-1].ElapsedMs || series.PCR[i].OffsetBytes <= series.PCR[i-1].OffsetBytes {
					t.Fatalf("%s program %d: PCR series not in order at %d", path, program.ProgramNumber, i)
				}
			}
			var maxInterval, maxGap float64
			for _, p := range series.PCR {
				if p.IntervalMs != nil {
					maxInterval = max(maxInterval, *p.IntervalMs)
				}
			}
			for _, p := range series.PES {
				if p.GapMs != nil {
					maxGap = max(maxGap, *p.GapMs)
				}
			}
			for i := 1; i < len(series.PES); i++ {
				if series.PES[i].OffsetBytes < series.PES[i-1].OffsetBytes {
					t.Fatalf("%s program %d: PES series not in stream order at %d", path, program.ProgramNumber, i)
				}
			}
			durations := series.Bitrate.DurationSeconds
			var seconds float64
			for _, d := range durations {
				seconds += d
			}
			if len(durations) == 0 || math.Abs(seconds-program.Bitrate.DurationSeconds) > 1e-9 {
				t.Errorf("%s program %d: window durations %v, measured %v s", path, program.ProgramNumber, durations, program.Bitrate.DurationSeconds)
			}
			for _, row := range program.Bitrate.PIDs {
				var peak, bytes float64
				for _, windows := range series.Bitrate.PIDs {
					if windows.PID != row.PID {
						continue
					}
					for i, n := range windows.Bytes {
						bytes += float64(n)
						if durations[i] == 1 {
							peak = max(peak, windows.BPS[i])
						}
					}
				}
				if row.Peak1sBPS == nil || peak != *row.Peak1sBPS {
					t.Errorf("%s program %d: PID %s windows peak %v, summary peak %v", path, program.ProgramNumber, row.PID, peak, row.Peak1sBPS)
				}
				if average := bytes * 8 / seconds; math.Abs(average-row.AverageBPS) > 1e-6 {
					t.Errorf("%s program %d: PID %s windows average %v, summary average %v", path, program.ProgramNumber, row.PID, average, row.AverageBPS)
				}
			}
			if maxInterval != *program.Compliance.MaxPCRInterval.ValueMs || maxGap != *program.Compliance.MaxPCRPTSDTSGap.ValueMs {
				t.Errorf("%s program %d: series maxima %v / %v disagree with the verdicts %+v", path, program.ProgramNumber, maxInterval, maxGap, program.Compliance)
			}
		}
	}
}

func TestRangeAnalysisSingleProgramMatchesSummary(t *testing.T) {
	report, err := decodeRangeAnalysis(t, sample188Path, options.Options{})
	if err != nil {
		t.Fatal(err)
	}
	summary, err := decodeJSONReport(t, func() error { return ParseTsFile(sample188Path, options.Options{Format: "json"}) })
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Programs) != 1 || !jsonEqual(t, report.Programs[0].Compliance, summary.Compliance) || !jsonEqual(t, report.ContinuityCounter, summary.ContinuityCounter) {
		t.Errorf("analysis differs from the summary:\n%+v\n%+v", report.Programs, summary)
	}
}

func TestRangeAnalysisOptionErrors(t *testing.T) {
	if err := ParseTsFile(sample188Path, options.Options{Format: "json", Output: "xml"}); err == nil || !strings.Contains(err.Error(), `unsupported output "xml"`) {
		t.Errorf("unknown output: %v", err)
	}
	if err := ParseTsFile(sample188Path, options.Options{RangeOffset: -1}); err == nil || !strings.Contains(err.Error(), "--range-offset and --range-limit must not be negative") {
		t.Errorf("negative range offset: %v", err)
	}
	if err := ParseTsFile(sample188Path, options.Options{Output: "analysis"}); err == nil || !strings.Contains(err.Error(), "requires --format json") {
		t.Errorf("analysis in text mode: %v", err)
	}
	for _, opts := range []options.Options{{Program: 1}, {ListPrograms: true}, {FailOnError: true}} {
		report, err := decodeRangeAnalysis(t, sample188Path, opts)
		if err == nil || report.Status != "failed" || report.Reason != "incompatible_options" || report.Output != "analysis" || len(report.Programs) != 0 || report.AnalysisStartOffsetBytes != nil {
			t.Errorf("%+v: report %+v err %v", opts, report, err)
		}
	}
}

// Synthetic multi-program streams for cases the fixtures do not cover.

func psiSection(tableID byte, idExtension uint16, body []byte) []byte {
	length := 5 + len(body) + 4
	section := []byte{tableID, 0xB0 | byte(length>>8), byte(length), byte(idExtension >> 8), byte(idExtension), 0xC1, 0x00, 0x00}
	section = append(section, body...)
	c := crc32(section)
	return append(section, byte(c>>24), byte(c>>16), byte(c>>8), byte(c))
}

// patSection lists (program_number, PMT PID) pairs.
func patSection(programs ...[2]uint16) []byte {
	var body []byte
	for _, p := range programs {
		body = append(body, byte(p[0]>>8), byte(p[0]), 0xE0|byte(p[1]>>8), byte(p[1]))
	}
	return psiSection(0x00, 1, body)
}

// pmtSection lists (stream_type, PID) pairs.
func pmtSection(programNumber, pcrPid uint16, streams ...[2]uint16) []byte {
	body := []byte{0xE0 | byte(pcrPid>>8), byte(pcrPid), 0xF0, 0x00}
	for _, s := range streams {
		body = append(body, byte(s[0]), 0xE0|byte(s[1]>>8), byte(s[1]), 0xF0, 0x00)
	}
	return psiSection(0x02, programNumber, body)
}

// pesStartPacket starts a video PES on pid with a PTS (90 kHz ticks).
func pesStartPacket(pid uint16, cc uint8, pts uint64) []byte {
	pkt := bytes.Repeat([]byte{0xFF}, 188)
	pkt[0], pkt[1], pkt[2], pkt[3] = 0x47, 0x40|byte(pid>>8), byte(pid), 0x10|cc
	header := []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05,
		0x21 | byte(pts>>29)&0x0E, byte(pts >> 22), byte(pts>>14) | 0x01, byte(pts >> 7), byte(pts<<1) | 0x01}
	copy(pkt[4:], header)
	return pkt
}

type syntheticProgram struct {
	number, pmtPid, pcrPid, videoPid uint16
	withPmt                          bool
}

// syntheticStream builds PAT and PMTs (each sent twice so section buffering
// ends), followed by PCR and PES packets for every program with a PMT.
// discontinuityAt marks the PCR with that index as a discontinuity.
func syntheticStream(programs []syntheticProgram, extraPat [][2]uint16, discontinuityAt int) []byte {
	var pat [][2]uint16
	pat = append(pat, extraPat...)
	for _, p := range programs {
		pat = append(pat, [2]uint16{p.number, p.pmtPid})
	}
	var buf bytes.Buffer
	patPayload := patSection(pat...)
	buf.Write(buildTsPacket(0x0000, true, 0, patPayload))
	buf.Write(buildTsPacket(0x0000, true, 1, patPayload))
	for round := uint8(0); round < 2; round++ {
		for _, p := range programs {
			if p.withPmt {
				buf.Write(buildTsPacket(p.pmtPid, true, round, pmtSection(p.number, p.pcrPid, [2]uint16{0x02, p.videoPid})))
			}
		}
	}
	const ms = uint64(pcrTicksPerMs)
	// Programs sharing a PID share its packets; write each carrier once.
	var carriers []syntheticProgram
	seen := map[uint16]bool{}
	for _, p := range programs {
		if p.withPmt && !seen[p.videoPid] {
			seen[p.videoPid] = true
			carriers = append(carriers, p)
		}
	}
	for i := 0; i < 6; i++ {
		for _, p := range carriers {
			pcr := 1000*ms + uint64(i)*40*ms
			if i == discontinuityAt {
				buf.Write(buildPcrPacketDI(p.pcrPid, pcr+5000*ms))
			} else {
				buf.Write(buildPcrPacket(p.pcrPid, pcr))
			}
			buf.Write(pesStartPacket(p.videoPid, uint8(i), pcr/300+90*700))
		}
	}
	return buf.Bytes()
}

func analyzeSynthetic(t *testing.T, data []byte, opts options.Options) RangeAnalysisReport {
	t.Helper()
	path := t.TempDir() + "/synthetic.ts"
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := decodeRangeAnalysis(t, path, opts)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestRangeAnalysisProgramWithoutPmt(t *testing.T) {
	report := analyzeSynthetic(t, syntheticStream([]syntheticProgram{
		{number: 1, pmtPid: 0x1000, pcrPid: 0x100, videoPid: 0x100, withPmt: true},
		{number: 2, pmtPid: 0x1001, pcrPid: 0x200, videoPid: 0x200},
	}, nil, -1), options.Options{})
	if report.Status != "complete" || len(report.Programs) != 2 {
		t.Fatalf("report = %+v", report)
	}
	first, second := report.Programs[0], report.Programs[1]
	if first.Status != "complete" || first.Series == nil || len(first.Series.PCR) == 0 {
		t.Errorf("program 1 = %+v", first)
	}
	if second.Status != "incomplete" || second.Reason != "pmt_parse_failed" || second.Error == "" || second.Series != nil || second.AnalysisStartOffsetBytes != nil {
		t.Errorf("program 2 = %+v", second)
	}
}

func TestRangeAnalysisWithoutAnyPmt(t *testing.T) {
	data := syntheticStream([]syntheticProgram{{number: 1, pmtPid: 0x1000, pcrPid: 0x100, videoPid: 0x100}}, nil, -1)
	for range 3 {
		data = append(data, buildStuffingPacket()...)
	}
	report := analyzeSynthetic(t, data, options.Options{})
	if report.Status != "incomplete" || report.Reason != "no_program_analyzed" || report.AnalysisStartOffsetBytes != nil || report.ContinuityCounter != nil || len(report.Programs) != 1 {
		t.Errorf("report = %+v", report)
	}
}

func TestRangeAnalysisSharedPidsAndNetworkEntry(t *testing.T) {
	report := analyzeSynthetic(t, syntheticStream([]syntheticProgram{
		{number: 1, pmtPid: 0x1000, pcrPid: 0x100, videoPid: 0x100, withPmt: true},
		{number: 2, pmtPid: 0x1001, pcrPid: 0x100, videoPid: 0x100, withPmt: true},
	}, [][2]uint16{{0, 0x0010}}, -1), options.Options{})
	if len(report.Programs) != 2 || report.Programs[0].ProgramNumber != 1 || report.Programs[1].ProgramNumber != 2 {
		t.Fatalf("programs = %+v", report.Programs)
	}
	a, b := report.Programs[0].Series, report.Programs[1].Series
	if len(a.PCR) == 0 || len(a.PES) == 0 || !reflect.DeepEqual(a.PCR, b.PCR) || len(a.PES) != len(b.PES) {
		t.Errorf("shared PIDs should give the same series:\n%+v\n%+v", a, b)
	}
}

// The jump forward and the return are both segment boundaries, so elapsed time
// stays frozen across them and advances again afterwards.
func TestRangeAnalysisPcrDiscontinuityFreezesElapsedTime(t *testing.T) {
	report := analyzeSynthetic(t, syntheticStream([]syntheticProgram{
		{number: 1, pmtPid: 0x1000, pcrPid: 0x100, videoPid: 0x100, withPmt: true},
	}, nil, 3), options.Options{})
	pcr := report.Programs[0].Series.PCR
	if len(pcr) != 6 || !pcr[3].Discontinuity || pcr[3].ElapsedMs != pcr[2].ElapsedMs || pcr[3].IntervalMs != nil || pcr[5].ElapsedMs <= pcr[3].ElapsedMs {
		t.Errorf("PCR series around the discontinuity: %+v", pcr)
	}
}

func TestRangeAnalysisReportsTimestampEvents(t *testing.T) {
	data := syntheticStream([]syntheticProgram{{number: 1, pmtPid: 0x1000, pcrPid: 0x100, videoPid: 0x100, withPmt: true}}, nil, -1)
	// A last PES whose PTS goes back to zero.
	report := analyzeSynthetic(t, append(data, pesStartPacket(0x100, 6, 0)...), options.Options{})
	events := report.Programs[0].Series.TimestampEvents
	if len(events) != 1 || events[0].PID != "0x0100" || events[0].Kind != "backward" || events[0].OffsetBytes != int64(len(data)) {
		t.Errorf("timestamp events = %+v", events)
	}
}

func TestRangeAnalysisOrdersTimestampEventsByOffset(t *testing.T) {
	data := syntheticStream([]syntheticProgram{{number: 1, pmtPid: 0x1000, pcrPid: 0x100, videoPid: 0x100, withPmt: true}}, nil, -1)
	data = append(data, pesStartPacket(0x100, 6, 90*100)...)
	data = append(data, pesStartPacket(0x100, 7, 0)...)
	events := analyzeSynthetic(t, data, options.Options{}).Programs[0].Series.TimestampEvents
	if len(events) != 2 || events[0].OffsetBytes >= events[1].OffsetBytes {
		t.Errorf("timestamp events = %+v", events)
	}
}

func TestRangeAnalysisReportsTruncatedTail(t *testing.T) {
	data := syntheticStream([]syntheticProgram{{number: 1, pmtPid: 0x1000, pcrPid: 0x100, videoPid: 0x100, withPmt: true}}, nil, -1)
	report := analyzeSynthetic(t, append(data, 0x47, 0x01), options.Options{})
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0].Message, "ignored 2 trailing bytes") {
		t.Errorf("warnings = %+v", report.Warnings)
	}
}

func TestRangeAnalysisReaderErrors(t *testing.T) {
	data := syntheticStream([]syntheticProgram{{number: 1, pmtPid: 0x1000, pcrPid: 0x100, videoPid: 0x100, withPmt: true}}, nil, -1)
	// Seek 1 locates the PAT; seek 2 starts the PMT search, seek 3 the PSI
	// layout of the lead, seek 4 the scan.
	for _, seek := range []int{2, 3, 4} {
		report := newAnalysisReport("synthetic", 0, 0)
		report.analysis = &rangeAnalysis{}
		err := parseTsReaderReport(&errReadSeeker{data: data, failAt: -1, seekFailAt: seek}, options.Options{}, report)
		if err == nil || !strings.Contains(err.Error(), "seek") {
			t.Errorf("seek %d: err = %v", seek, err)
		}
	}

	// A broken packet after the PSI fails the scan.
	broken := append(bytes.Clone(data), make([]byte, 188)...)
	report := newAnalysisReport("synthetic", 0, 0)
	report.analysis = &rangeAnalysis{}
	if err := parseTsReaderReport(bytes.NewReader(broken), options.Options{}, report); err == nil || !strings.Contains(err.Error(), "failed to parse TS packet") {
		t.Errorf("broken packet: err = %v", err)
	}
}

// A PCR of zero is a valid clock value (a stream starting at zero, or after
// the 42-bit wrap), so a PES after it still has a reference time.
func TestRangeAnalysisPcrOfZero(t *testing.T) {
	const ms = uint64(pcrTicksPerMs)
	var buf bytes.Buffer
	pat := patSection([2]uint16{1, 0x1000})
	pmt := pmtSection(1, 0x100, [2]uint16{0x02, 0x100})
	buf.Write(buildTsPacket(0x0000, true, 0, pat))
	buf.Write(buildTsPacket(0x0000, true, 1, pat))
	buf.Write(buildTsPacket(0x1000, true, 0, pmt))
	buf.Write(buildTsPacket(0x1000, true, 1, pmt))
	for i := range 3 {
		pcr := uint64(i) * 40 * ms
		buf.Write(buildPcrPacket(0x100, pcr))
		buf.Write(pesStartPacket(0x100, uint8(i), pcr/300+90*700))
	}
	pes := analyzeSynthetic(t, buf.Bytes(), options.Options{}).Programs[0].Series.PES
	if len(pes) == 0 || pes[0].ElapsedMs == nil || *pes[0].ElapsedMs != 0 {
		t.Fatalf("first PES = %+v, want elapsed_ms 0", pes)
	}
	// The PES sits halfway between the PCRs at 0 and 40 ms: 700 - 20 ms.
	if pes[0].GapMs == nil || math.Abs(*pes[0].GapMs-680) > 1e-6 {
		t.Errorf("first PES gap = %v, want 680 ms", pes[0].GapMs)
	}
}
