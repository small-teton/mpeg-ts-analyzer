package tsparser

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// The mux-lead fixtures have known leads across a PCR/PTS wraparound and a
// declared PCR discontinuity (tools/generate_mux_lead). Packet n is slot n of
// the generator.
var muxLeadFixtures = []struct {
	path       string
	packetSize int64
	m2ts       bool
}{
	{"../sample_data/sample_188byte_mux_lead.ts", 188, false},
	{"../sample_data/sample_192byte_mux_lead.ts", 192, true},
}

func TestMuxLeadFixtureGenerator(t *testing.T) {
	for _, fixture := range muxLeadFixtures {
		path := filepath.Join(t.TempDir(), "fixture.ts")
		args := []string{"run", "../tools/generate_mux_lead", "-output", path}
		if fixture.m2ts {
			args = append(args, "-m2ts")
		}
		if output, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("generate fixture: %v\n%s", err, output)
		}
		generated, err := os.ReadFile(path)
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

// wantMuxLead is the expected result of the fixture at packet size size.
func wantMuxLead(size int64) *ReportMultiplexing {
	at := func(slot int64) *int64 { v := slot * size; return &v }
	ms := func(v float64) *float64 { return &v }
	stream := func(pid, streamType, label string) ReportPESStream {
		return ReportPESStream{ProgramNumber: 1, PID: pid, StreamType: streamType, Label: label}
	}
	audioDiff := ReportAVLeadDifference{VideoPID: "0x0100", MaxAVLeadDifferenceMs: ms(800), AudioLeadMs: ms(1500), VideoLeadMs: ms(700), AudioOffsetBytes: at(9), VideoOffsetBytes: at(10)}
	return &ReportMultiplexing{MuxLead: ReportMuxLead{
		TimeBasis: "pcr_interpolated_file_position",
		PIDs: []ReportMuxLeadPID{
			// Slots 148 (its next PCR starts a new segment) and 238 (no PCR
			// after it) are unbracketed.
			{ReportPESStream: stream("0x0100", "0x02", "video"), AVClass: "video", Status: "info", SampleCount: 115, UnbracketedCount: 2, DTSSampleCount: 115,
				MinLeadMs: ms(700), MinLeadOffsetBytes: at(10), MaxLeadMs: ms(708), MaxLeadOffsetBytes: at(8), SpreadMs: ms(8)},
			// Slots 5 (before the first PCR) and 149 (across the
			// discontinuity) are unbracketed; slot 61 has a transport error.
			{ReportPESStream: stream("0x0101", "0x03", "audio"), AVClass: "audio", Status: "info", SampleCount: 56, UnbracketedCount: 2,
				MinLeadMs: ms(1500), MinLeadOffsetBytes: at(9), MaxLeadMs: ms(1500), MaxLeadOffsetBytes: at(9), SpreadMs: ms(0)},
			// Slot 67 has no PTS; slot 51 was multiplexed 20 ms after its PTS.
			{ReportPESStream: stream("0x0102", "0x0f", "audio"), AVClass: "audio", Status: "info", SampleCount: 17, NegativeCount: 1,
				MinLeadMs: ms(-20), MinLeadOffsetBytes: at(51), MaxLeadMs: ms(650), MaxLeadOffsetBytes: at(11), SpreadMs: ms(670)},
			{ReportPESStream: stream("0x0103", "0x06", "stream_type 0x06"), AVClass: "other", Status: "info", SampleCount: 1,
				MinLeadMs: ms(300), MinLeadOffsetBytes: at(15), MaxLeadMs: ms(300), MaxLeadOffsetBytes: at(15), SpreadMs: ms(0)},
		},
		Programs: []ReportAVLeadProgram{{
			ProgramNumber: 1, Status: "info", AudioPID: "0x0101", ReportAVLeadDifference: audioDiff,
			Audio: []ReportAVLeadAudio{
				{PID: "0x0101", Status: "info", ComparisonCount: 161, ReportAVLeadDifference: audioDiff},
				{PID: "0x0102", Status: "info", ComparisonCount: 111, ReportAVLeadDifference: ReportAVLeadDifference{
					VideoPID: "0x0100", MaxAVLeadDifferenceMs: ms(728), AudioLeadMs: ms(-20), VideoLeadMs: ms(708), AudioOffsetBytes: at(51), VideoOffsetBytes: at(68)}},
			},
		}},
	}}
}

func TestMuxLeadFixtures(t *testing.T) {
	for _, fixture := range muxLeadFixtures {
		report, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatalf("%s: %v", fixture.path, err)
		}
		want := wantMuxLead(fixture.packetSize)
		if !jsonEqual(t, report.Multiplexing, want) {
			t.Errorf("%s: multiplexing %s", fixture.path, toJSON(t, report.Multiplexing))
		}
		// The PCR-PTS/DTS gap check judges the same leads: its value is the
		// largest lead of the program, and it is the only verdict.
		gap := report.Programs[0].Compliance.MaxPCRPTSDTSGap
		if gap.Status != "ng" || *gap.ValueMs != 1500 {
			t.Errorf("%s: gap %s", fixture.path, toJSON(t, gap))
		}
		summary, err := decodeJSONReport(t, func() error { return ParseTsFile(fixture.path, options.Options{Format: "json"}) })
		if err != nil || !jsonEqual(t, summary.Multiplexing, want) {
			t.Errorf("%s: summary %v %s", fixture.path, err, toJSON(t, summary.Multiplexing))
		}
		// Report-only: --fail-on-error fails on the gap check alone.
		var compliance *ComplianceError
		captureStdout(t, func() { err = ParseTsFile(fixture.path, options.Options{FailOnError: true}) })
		if !errors.As(err, &compliance) || !slices.Equal(compliance.Checks, []string{"PCR-PTS/DTS max gap"}) {
			t.Errorf("%s: --fail-on-error = %v", fixture.path, err)
		}
	}
}

func TestMuxLeadText(t *testing.T) {
	out := captureStdout(t, func() {
		if err := ParseTsFile(muxLeadFixtures[0].path, options.Options{}); err != nil {
			t.Error(err)
		}
	})
	at := func(slot int) string { return fmt.Sprintf("0x%08x", slot*188) }
	want := strings.Join([]string{
		"-----------------------------",
		"Mux Lead (report only: DTS, or PTS without DTS, minus PCR-interpolated arrival)",
		"  Program 1 PID 0x0100 (video) : 700.000ms to 708.000ms (spread 8.000ms) over 115 PES, min at " + at(10) + ", max at " + at(8) + ", 2 unbracketed",
		"  Program 1 PID 0x0101 (audio) : 1500.000ms to 1500.000ms (spread 0.000ms) over 56 PES, min at " + at(9) + ", max at " + at(9) + ", 2 unbracketed",
		"  Program 1 PID 0x0102 (audio) : -20.000ms to 650.000ms (spread 670.000ms) over 17 PES, min at " + at(51) + ", max at " + at(11),
		"    1 PES multiplexed after their DTS/PTS (negative lead: an observation, not an error)",
		"  Program 1 PID 0x0103 (stream_type 0x06) : 300.000ms to 300.000ms (spread 0.000ms) over 1 PES, min at " + at(15) + ", max at " + at(15),
		"  Program 1 A/V lead difference: 800.000ms, audio PID 0x0101 (lead 1500.000ms at " + at(9) + ") against video PID 0x0100 (lead 700.000ms at " + at(10) + ")",
		"    audio PID 0x0101 : 800.000ms",
		"    audio PID 0x0102 : 728.000ms",
		"",
	}, "\n")
	if !strings.HasSuffix(out, want) {
		t.Errorf("text summary:\n%s\nwant suffix:\n%s", out, want)
	}
}

// A PID without a lead and a program whose audio was never compared.
func TestMuxLeadUnmeasured(t *testing.T) {
	check := analysis.NewPESCheck()
	check.ObserveProgramLayout(2, analysis.ProgramLayout{PCRPID: 0x200, Streams: []analysis.ElementaryStream{{PID: 0x200, StreamType: 0x1B}, {PID: 0x201, StreamType: 0x0F}, {PID: 0x202, StreamType: 0x81}}})
	check.ObservePES(analysis.PES{Program: 2, PID: 0x201, Offset: 376, HasPTS: true})
	out := captureStdout(t, func() { dumpMuxLead(check) })
	want := strings.Join([]string{
		"-----------------------------",
		"Mux Lead (report only: DTS, or PTS without DTS, minus PCR-interpolated arrival)",
		"  Program 2 PID 0x0200 (video) : no lead (no PES packet started)",
		"  Program 2 PID 0x0201 (audio) : no lead (no PES packet lies between two PCRs), 1 unbracketed",
		"  Program 2 PID 0x0202 (stream_type 0x81) : no lead (no PES packet started)",
		"  Program 2 A/V lead difference: not measured (the audio and video leads never lay in one PCR segment at the same time)",
		"",
	}, "\n")
	if out != want {
		t.Errorf("text:\n%s\nwant:\n%s", out, want)
	}
	r := reportMultiplexing(check).MuxLead
	program := r.Programs[0]
	if r.PIDs[1].Reason != "no_pcr_timing" || r.PIDs[1].MinLeadMs != nil || program.Status != "insufficient_data" || program.Reason != "no_comparable_leads" ||
		program.AudioPID != "" || program.MaxAVLeadDifferenceMs != nil || len(program.Audio) != 2 || program.Audio[1].Reason != "no_comparable_leads" {
		t.Errorf("report = %s", toJSON(t, r))
	}
}

// With several audio PIDs, one that was never compared is listed as such.
func TestMuxLeadTextPartlyMeasured(t *testing.T) {
	check := analysis.NewPESCheck()
	check.ObserveProgramLayout(1, analysis.ProgramLayout{PCRPID: 0x100, Streams: []analysis.ElementaryStream{{PID: 0x100, StreamType: 0x02}, {PID: 0x101, StreamType: 0x03}, {PID: 0x102, StreamType: 0x04}}})
	timed := analysis.ClockTime{Valid: true, Ticks: 27_000_000}
	check.ObservePES(analysis.PES{Program: 1, PID: 0x100, Offset: 0, HasPTS: true, PTS: 90_000 + 63_000, Arrival: timed})
	check.ObservePES(analysis.PES{Program: 1, PID: 0x101, Offset: 188, HasPTS: true, PTS: 90_000 + 45_000, Arrival: timed})
	out := captureStdout(t, func() { dumpMuxLead(check) })
	for _, line := range []string{
		"  Program 1 A/V lead difference: 200.000ms, audio PID 0x0101 (lead 500.000ms at 0x000000bc) against video PID 0x0100 (lead 700.000ms at 0x00000000)\n",
		"    audio PID 0x0101 : 200.000ms\n",
		"    audio PID 0x0102 : not measured (the audio and video leads never lay in one PCR segment at the same time)\n",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("text:\n%s\nwant line:\n%s", out, line)
		}
	}
}
