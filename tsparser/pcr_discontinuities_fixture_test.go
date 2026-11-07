package tsparser

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// The PCR-discontinuity fixtures pass through every kind of PCR step: a
// wraparound (slot 50), declared discontinuities (slot 80, and slot 110 after
// discontinuity_indicator on the PCR-less packet of slot 109), an undeclared
// backward step (slot 140), an undeclared forward jump (slot 170) and a
// PCR_PID change (slot 202) (tools/generate_pcr_discontinuities). Packet n is
// slot n of the generator; every PCR interval is 40 ms.
var pcrDiscontinuitiesFixtures = []struct {
	path       string
	packetSize int64
	m2ts       bool
}{
	{"../sample_data/sample_188byte_pcr_discontinuities.ts", 188, false},
	{"../sample_data/sample_192byte_pcr_discontinuities.ts", 192, true},
}

func generatePcrDiscontinuities(t *testing.T, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.ts")
	args = append([]string{"run", "../tools/generate_pcr_discontinuities", "-output", path}, args...)
	if output, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate fixture: %v\n%s", err, output)
	}
	return path
}

func TestPcrDiscontinuitiesFixtureGenerator(t *testing.T) {
	for _, fixture := range pcrDiscontinuitiesFixtures {
		var args []string
		if fixture.m2ts {
			args = append(args, "-m2ts")
		}
		generated, err := os.ReadFile(generatePcrDiscontinuities(t, args...))
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

func TestPcrDiscontinuitiesFixtures(t *testing.T) {
	for _, fixture := range pcrDiscontinuitiesFixtures {
		at := func(slot int64) int64 { return slot * fixture.packetSize }
		report, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatalf("%s: %v", fixture.path, err)
		}
		p := report.Programs[0]
		// Every interval is 40 ms; none is measured across a boundary.
		interval := p.Compliance.MaxPCRInterval
		if interval.Status != "ok" || *interval.ValueMs != 40 || interval.Observations != 121 {
			t.Errorf("%s: interval %s", fixture.path, toJSON(t, interval))
		}
		wantDiscontinuities := ReportPCRDiscontinuityCheck{Status: "ng", PCRCount: 127, UndeclaredCount: 2, JumpLimitMs: 1000, Items: []ReportPCRStep{
			{Kind: "wraparound", Count: 1, FirstOffsetBytes: at(50), FirstJumpMs: 40},
			{Kind: "declared", Count: 2, FirstOffsetBytes: at(80), FirstJumpMs: 5040},
			{Kind: "pcr_pid_change", Count: 1, FirstOffsetBytes: at(202), FirstJumpMs: 40},
			{Kind: "backward", Count: 1, FirstOffsetBytes: at(140), FirstJumpMs: -1960},
			{Kind: "forward_jump", Count: 1, FirstOffsetBytes: at(170), FirstJumpMs: 4040},
		}}
		if !jsonEqual(t, p.Compliance.PCRDiscontinuities, wantDiscontinuities) {
			t.Errorf("%s: discontinuities %s", fixture.path, toJSON(t, p.Compliance.PCRDiscontinuities))
		}
		// The jitter estimate, the bitrate and the series split at the same
		// five boundaries, and not at the wraparound.
		wantJitter := []ReportPCRDiscontinuity{
			{at(80), "declared", 5040, 1, 2},
			{at(110), "declared", -2960, 2, 3},
			{at(140), "backward", -1960, 3, 4},
			{at(170), "forward_jump", 4040, 4, 5},
			{at(202), "pcr_pid_change", 40, 5, 6},
		}
		if !jsonEqual(t, p.PCRJitter.Estimate.Discontinuities, wantJitter) || p.PCRJitter.Estimate.SegmentCount != 6 ||
			p.PCRJitter.Status != "ok" || p.Bitrate.SegmentCount != 6 {
			t.Errorf("%s: jitter %s bitrate segments %d", fixture.path, toJSON(t, p.PCRJitter), p.Bitrate.SegmentCount)
		}
		var boundaries []int64
		for _, point := range p.Series.PCR {
			if point.Discontinuity {
				boundaries = append(boundaries, point.OffsetBytes/fixture.packetSize)
				if point.IntervalMs != nil {
					t.Errorf("%s: interval at a boundary: %s", fixture.path, toJSON(t, point))
				}
			} else if point.OffsetBytes != at(6) && (point.IntervalMs == nil || *point.IntervalMs != 40) {
				t.Errorf("%s: point %s", fixture.path, toJSON(t, point))
			}
		}
		if !jsonEqual(t, boundaries, []int64{80, 110, 140, 170, 202}) {
			t.Errorf("%s: series boundaries %v", fixture.path, boundaries)
		}
		// 127 PCRs, six segments: 121 intervals of 40 ms advance the clock.
		if last := p.Series.PCR[len(p.Series.PCR)-1]; last.ElapsedMs != 121*40 {
			t.Errorf("%s: elapsed %v", fixture.path, last.ElapsedMs)
		}

		// --fail-on-error fails on the undeclared discontinuities alone.
		var compliance *ComplianceError
		_ = captureStdout(t, func() {
			err = ParseTsFile(fixture.path, options.Options{FailOnError: true})
		})
		if !errors.As(err, &compliance) || strings.Join(compliance.Checks, ",") != "PCR discontinuities" {
			t.Errorf("%s: fail-on-error %v", fixture.path, err)
		}
	}
}

// Without the events the clock is continuous through the wraparound: every
// result sees one segment and the discontinuity check is OK.
func TestPcrDiscontinuitiesFixtureClean(t *testing.T) {
	for _, args := range [][]string{{"-clean"}, {"-clean", "-m2ts"}} {
		report, err := decodeRangeAnalysis(t, generatePcrDiscontinuities(t, args...), options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		p := report.Programs[0]
		d := p.Compliance.PCRDiscontinuities
		if d.Status != "ok" || d.PCRCount != 127 || len(d.Items) != 1 || d.Items[0].Kind != "wraparound" ||
			p.Compliance.MaxPCRInterval.Observations != 126 || p.PCRJitter.Estimate.SegmentCount != 1 || p.PCRJitter.Status != "ok" ||
			p.PCRJitter.Delivery.WindowCount != 5 || p.Bitrate.SegmentCount != 1 {
			t.Errorf("%v: %s", args, toJSON(t, p))
		}
	}
}

func TestPcrDiscontinuitiesText(t *testing.T) {
	out := captureStdout(t, func() {
		if err := ParseTsFile(pcrDiscontinuitiesFixtures[0].path, options.Options{DumpPcrJitter: true}); err != nil {
			t.Error(err)
		}
	})
	for _, want := range []string{
		"Max PCR interval: 40.000000ms [OK, limit: <= 100.000000ms]\n" +
			"PCR discontinuities: 2 undeclared in 127 PCRs [NG, limit: 0 without discontinuity_indicator]\n" +
			"  - wraparound    : 1, first at 0x000024b8 (PCR jumped +40.000ms)\n" +
			"  - declared      : 2, first at 0x00003ac0 (PCR jumped +5040.000ms)\n" +
			"  - pcr_pid_change: 1, first at 0x00009458 (PCR jumped +40.000ms)\n" +
			"  - backward      : 1, first at 0x000066d0 (PCR jumped -1960.000ms)\n" +
			"  - forward_jump  : 1, first at 0x00007cd8 (PCR jumped +4040.000ms)\n",
		"    - 0x000066d0 : backward, PCR jumped -1960.000ms (segment 3 -> 4)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text missing %q:\n%s", want, out)
		}
	}
}
