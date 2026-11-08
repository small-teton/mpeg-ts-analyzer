package tsparser

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// The PCR-to-PTS/DTS gap wraparound fixtures cross the 33-bit wraparounds in
// both orders (tools/generate_pts_gap_wraparound): the video DTS (lead
// 700 ms) and the audio PTS (lead 30 ms) wrap before the PCR, the AAC PTS
// (lead -150 ms) after it, and the PCR wraps between the PCRs of packets 98
// and 102. Packet n is slot n of the generator.
var ptsGapWraparoundFixtures = []struct {
	path       string
	packetSize int64
	// flag selects the packet size of the generator: none, -m2ts or -trailer.
	flag string
}{
	{"../sample_data/diagnostics/sample_188byte_pts_gap_wraparound.ts", 188, ""},
	{"../sample_data/diagnostics/sample_192byte_pts_gap_wraparound.ts", 192, "-m2ts"},
	{"../sample_data/diagnostics/sample_204byte_pts_gap_wraparound.ts", 204, "-trailer"},
}

// ptsGapWraparoundLeads is the lead of each elementary PID in ms.
var ptsGapWraparoundLeads = map[string]float64{"0x0100": 700, "0x0101": 30, "0x0102": -150}

func TestPtsGapWraparoundFixtureGenerator(t *testing.T) {
	for _, fixture := range ptsGapWraparoundFixtures {
		path := filepath.Join(t.TempDir(), "fixture.ts")
		args := []string{"run", "../tools/generate_pts_gap_wraparound", "-output", path}
		if fixture.flag != "" {
			args = append(args, fixture.flag)
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

// The gap check, the time series and the mux lead give every bracketed PES
// packet its stream's lead, across both orders of the wraps.
func TestPtsGapWraparoundFixtures(t *testing.T) {
	for _, fixture := range ptsGapWraparoundFixtures {
		report, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatalf("%s: %v", fixture.path, err)
		}
		program := report.Programs[0]
		gap := program.Compliance.MaxPCRPTSDTSGap
		if gap.Status != "ok" || gap.ValueMs == nil || *gap.ValueMs != 700 || gap.Observations != 144 {
			t.Errorf("%s: gap %s", fixture.path, toJSON(t, gap))
		}
		gaps := map[string]int{}
		for _, p := range program.Series.PES {
			if p.GapMs == nil {
				continue
			}
			if want := ptsGapWraparoundLeads[p.PID]; math.Abs(*p.GapMs-want) > 1e-6 {
				t.Errorf("%s: PES at %d on PID %s: gap %f, want %f", fixture.path, p.OffsetBytes, p.PID, *p.GapMs, want)
			}
			gaps[p.PID]++
		}
		for _, p := range report.Multiplexing.MuxLead.PIDs {
			want := ptsGapWraparoundLeads[p.PID]
			if p.SampleCount != 48 || p.UnbracketedCount != 1 || *p.MinLeadMs != want || *p.MaxLeadMs != want || gaps[p.PID] != p.SampleCount {
				t.Errorf("%s: mux lead %s, %d gaps", fixture.path, toJSON(t, p), gaps[p.PID])
			}
		}
		// The AAC PES packet of packet 105 has a PCR that wrapped and a PTS
		// that has not: a small negative lead, not a gap of +26.5 hours.
		summary, err := decodeJSONReport(t, func() error { return ParseTsFile(fixture.path, options.Options{Format: "json", FailOnError: true}) })
		if err != nil || summary.Compliance.MaxPCRPTSDTSGap.Status != "ok" {
			t.Errorf("%s: summary %v %s", fixture.path, err, toJSON(t, summary.Compliance))
		}
	}
}

// --dump-timestamp prints the same lead for every PES packet, including those
// whose PCRs straddle the PCR wrap (packets 98, 99 and 101).
func TestPtsGapWraparoundDumpTimestamp(t *testing.T) {
	line := regexp.MustCompile(`(?m)^0x([0-9a-f]{8}) (?:PTS|DTS): \S+ \(pid:0x(\w+)\)(?: \(delay:(-?[0-9.]+)ms\))?$`)
	for _, fixture := range ptsGapWraparoundFixtures {
		out := captureStdout(t, func() {
			if err := ParseTsFile(fixture.path, options.Options{DumpTimestamp: true}); err != nil {
				t.Error(err)
			}
		})
		delays := map[int64]string{}
		for _, m := range line.FindAllStringSubmatch(out, -1) {
			offset, _ := strconv.ParseInt(m[1], 16, 64)
			delays[offset/fixture.packetSize] = m[2] + " " + m[3]
		}
		for slot, want := range map[int64]string{
			5:   "102 ",            // before the first PCR
			66:  "100 700.000000",  // the first wrapped DTS
			98:  "100 700.000000",  // bracketed by the PCRs of 98 and 102
			99:  "101 30.000000",   // wrapped PTS, PCRs straddle the wrap
			101: "102 -150.000000", // wrapped PCR, PTS not yet wrapped
			105: "102 -150.000000",
			109: "102 -150.000000",
			199: "101 50.000000", // after the last PCR: against it alone
		} {
			if delays[slot] != want {
				t.Errorf("%s: packet %d: %q, want %q", fixture.path, slot, delays[slot], want)
			}
		}
		if len(delays) != 147 {
			t.Errorf("%s: %d PES packets dumped, want 147", fixture.path, len(delays))
		}
		for slot, d := range delays {
			var pid string
			var ms float64
			if n, _ := fmt.Sscanf(d, "%s %f", &pid, &ms); n == 2 && slot != 199 && ms != ptsGapWraparoundLeads["0x0"+pid] {
				t.Errorf("%s: packet %d: delay %q", fixture.path, slot, d)
			}
		}
	}
}
