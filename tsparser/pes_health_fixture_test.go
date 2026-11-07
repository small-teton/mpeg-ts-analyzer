package tsparser

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// The PES-health fixtures carry malformed PES headers and a 1080 ms PTS
// repetition interval (tools/generate_pes_health). Packet n is slot n of the
// generator.
var pesHealthFixtures = []struct {
	path       string
	packetSize int64
	m2ts       bool
}{
	{"../sample_data/sample_188byte_pes_health.ts", 188, false},
	{"../sample_data/sample_192byte_pes_health.ts", 192, true},
}

func generatePESHealth(t *testing.T, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.ts")
	args = append([]string{"run", "../tools/generate_pes_health", "-output", path}, args...)
	if output, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate fixture: %v\n%s", err, output)
	}
	return path
}

func TestPESHealthFixtureGenerator(t *testing.T) {
	for _, fixture := range pesHealthFixtures {
		var args []string
		if fixture.m2ts {
			args = append(args, "-m2ts")
		}
		generated, err := os.ReadFile(generatePESHealth(t, args...))
		if err != nil {
			t.Fatal(err)
		}
		committed, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, committed) {
			t.Errorf("%s does not match the generator output", fixture.path)
		}
	}
}

// wantPESHealth is the expected result of the fixture at packet size size.
func wantPESHealth(size int64) (*ReportPESHeaders, *ReportPTSRepetition) {
	at := func(slot int64) *int64 { v := slot * size; return &v }
	stream := func(pid, streamType, label string) ReportPESStream {
		return ReportPESStream{ProgramNumber: 1, PID: pid, StreamType: streamType, Label: label}
	}
	video, audio := stream("0x0100", "0x02", "video"), stream("0x0101", "0x03", "audio")
	private, audio2, avc := stream("0x0102", "0x06", "stream_type 0x06"), stream("0x0103", "0x04", "audio"), stream("0x0104", "0x1b", "video")
	ok := func(checked int) ReportPESHeaderCounts {
		return ReportPESHeaderCounts{Status: "ok", CheckedCount: checked, Errors: []ReportPESHeaderError{}}
	}
	headers := &ReportPESHeaders{
		ReportPESHeaderCounts: ReportPESHeaderCounts{Status: "errors", CheckedCount: 161, ErrorCount: 8, FirstErrorOffsetBytes: at(27), Errors: []ReportPESHeaderError{
			{"truncated", 1, *at(35)}, {"bad_start_code", 1, *at(27)}, {"bad_flags", 2, *at(67)}, {"inconsistent_length", 2, *at(91)}, {"bad_marker_bits", 2, *at(61)},
		}},
		PIDs: []ReportPESHeaderPID{
			{video, ok(96)},
			{audio, ReportPESHeaderCounts{Status: "errors", CheckedCount: 49, ErrorCount: 1, FirstErrorOffsetBytes: at(61), Errors: []ReportPESHeaderError{{"bad_marker_bits", 1, *at(61)}}}},
			{private, ReportPESHeaderCounts{Status: "errors", CheckedCount: 15, ErrorCount: 7, FirstErrorOffsetBytes: at(27), Errors: []ReportPESHeaderError{
				{"truncated", 1, *at(35)}, {"bad_start_code", 1, *at(27)}, {"bad_flags", 2, *at(67)}, {"inconsistent_length", 2, *at(91)}, {"bad_marker_bits", 1, *at(115)},
			}}},
			{audio2, ok(1)},
			{avc, ReportPESHeaderCounts{Status: "insufficient_data", Reason: "no_pes", Errors: []ReportPESHeaderError{}}},
		},
	}
	ms := func(v float64) *float64 { return &v }
	repetition := &ReportPTSRepetition{
		LimitMs: 700,
		ReportPTSRepetitionCounts: ReportPTSRepetitionCounts{Status: "ng", PTSCount: 127, IntervalCount: 113, MaxIntervalMs: ms(1080),
			MaxIntervalStartOffsetBytes: at(98), MaxIntervalEndOffsetBytes: at(152), ExceededCount: 1, FirstExceededOffsetBytes: at(152)},
		PIDs: []ReportPTSRepetitionPID{
			{video, ReportPTSRepetitionCounts{Status: "ng", PTSCount: 70, IntervalCount: 68, MaxIntervalMs: ms(1080),
				MaxIntervalStartOffsetBytes: at(98), MaxIntervalEndOffsetBytes: at(152), ExceededCount: 1, FirstExceededOffsetBytes: at(152)}},
			// The malformed header in slot 61 breaks the interval, so 57 to 65
			// is not measured.
			{audio, ReportPTSRepetitionCounts{Status: "ok", PTSCount: 48, IntervalCount: 45, MaxIntervalMs: ms(80),
				MaxIntervalStartOffsetBytes: at(9), MaxIntervalEndOffsetBytes: at(13)}},
			{private, ReportPTSRepetitionCounts{Status: "skipped", Reason: "not_video_or_audio", PTSCount: 8}},
			{audio2, ReportPTSRepetitionCounts{Status: "insufficient_data", Reason: "single_pts", PTSCount: 1}},
			{avc, ReportPTSRepetitionCounts{Status: "insufficient_data", Reason: "no_pes"}},
		},
	}
	return headers, repetition
}

func TestPESHealthFixtures(t *testing.T) {
	for _, fixture := range pesHealthFixtures {
		report, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatalf("%s: %v", fixture.path, err)
		}
		wantHeaders, wantRepetition := wantPESHealth(fixture.packetSize)
		if !jsonEqual(t, report.PESHeaders, wantHeaders) || !jsonEqual(t, report.PTSRepetition, wantRepetition) {
			t.Errorf("%s:\npes_headers %s\npts_repetition %s", fixture.path, toJSON(t, report.PESHeaders), toJSON(t, report.PTSRepetition))
		}
		// No malformed header and no PES without PTS feeds the other
		// timestamp checks: no anomaly, and the PCR-PTS/DTS gap comes from
		// valid timestamps only (private PTS +300 ms).
		p := report.Programs[0]
		gap := p.Compliance.MaxPCRPTSDTSGap
		if report.Status != "complete" || len(report.Warnings) != 0 || p.TimestampAnomalies.Status != "ok" || gap.Status != "ok" || *gap.ValueMs != 300 {
			t.Errorf("%s: %s %s %s", fixture.path, toJSON(t, report.Warnings), toJSON(t, p.TimestampAnomalies), toJSON(t, p.Compliance))
		}

		// The summary report has the same results.
		summary, err := decodeJSONReport(t, func() error { return ParseTsFile(fixture.path, options.Options{Format: "json"}) })
		if err != nil || summary.Status != "complete" || !jsonEqual(t, summary.PESHeaders, wantHeaders) || !jsonEqual(t, summary.PTSRepetition, wantRepetition) ||
			summary.TimestampAnomalies.Status != "ok" {
			t.Errorf("%s: summary %v %s", fixture.path, err, toJSON(t, summary))
		}
	}
}

// Without the defects every header is valid and the largest video PTS
// interval is 80 ms, around the PES split over slots 10 and 12.
func TestPESHealthFixtureClean(t *testing.T) {
	report, err := decodeRangeAnalysis(t, generatePESHealth(t, "-clean"), options.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h, r := report.PESHeaders, report.PTSRepetition
	if h.Status != "ok" || h.CheckedCount != 161 || h.ErrorCount != 0 || r.Status != "ok" || *r.MaxIntervalMs != 80 || r.PIDs[0].Status != "ok" || *r.PIDs[0].MaxIntervalStartOffsetBytes != 10*188 {
		t.Errorf("clean: %s %s", toJSON(t, h), toJSON(t, r))
	}
}

func TestPESHealthText(t *testing.T) {
	out := captureStdout(t, func() {
		if err := ParseTsFile(pesHealthFixtures[0].path, options.Options{}); err != nil {
			t.Error(err)
		}
	})
	at := func(slot int) string { return fmt.Sprintf("0x%08x", slot*188) }
	want := strings.Join([]string{
		"-----------------------------",
		"PES Headers: 8 errors in 161 checked on 5 PIDs, first at " + at(27) + " [ERRORS]",
		"  Program 1 PID 0x0101 (audio) : 1 error in 49 checked, first at " + at(61) + " [ERRORS]",
		"    bad_marker_bits: 1, first at " + at(61),
		"  Program 1 PID 0x0102 (stream_type 0x06) : 7 errors in 15 checked, first at " + at(27) + " [ERRORS]",
		"    truncated: 1, first at " + at(35),
		"    bad_start_code: 1, first at " + at(27),
		"    bad_flags: 2, first at " + at(67),
		"    inconsistent_length: 2, first at " + at(91),
		"    bad_marker_bits: 1, first at " + at(115),
		"  Program 1 PID 0x0104 (video) : INSUFFICIENT DATA (no PES packet started)",
		"PTS Repetition (limit 700ms): max interval 1080.000000ms from " + at(98) + " to " + at(152) + ", 1 exceeded, first ending at " + at(152) + " [NG]",
		"  Program 1 PID 0x0100 (video) : max interval 1080.000000ms from " + at(98) + " to " + at(152) + ", 1 exceeded, first ending at " + at(152) + " [NG]",
		"  Program 1 PID 0x0103 (audio) : INSUFFICIENT DATA (fewer than two comparable PTS)",
		"  Program 1 PID 0x0104 (video) : INSUFFICIENT DATA (no PES packet started)",
		"",
	}, "\n")
	if !strings.Contains(out, want) {
		t.Errorf("text summary:\n%s\nwant:\n%s", out, want)
	}
	if strings.Contains(out, "Timestamp Anomaly") {
		t.Errorf("unexpected timestamp anomaly:\n%s", out)
	}
}
