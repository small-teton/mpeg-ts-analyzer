package tsparser

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/options"
)

// linearSamples builds n PCR samples on a perfect constant-rate line:
// pos = i*posStep, pcr = pcr0 + i*pcrStep, no discontinuity.
func linearSamples(n int, posStep int64, pcr0, pcrStep uint64) []pcrSample {
	s := make([]pcrSample, n)
	for i := 0; i < n; i++ {
		s[i] = pcrSample{pos: int64(i) * posStep, pcr: pcr0 + uint64(i)*pcrStep}
	}
	return s
}

func TestPcrTickNs(t *testing.T) {
	// 27 ticks == 1000 ns exactly (1 tick = 1000/27 ns).
	if got := pcrTickNs(27); math.Abs(got-1000) > 1e-9 {
		t.Errorf("pcrTickNs(27) = %f, want 1000", got)
	}
}

func TestIsSegmentBoundary(t *testing.T) {
	base := pcrSample{pos: 1000, pcr: 1000}
	cases := []struct {
		name string
		cur  pcrSample
		want bool
	}{
		{"normal", pcrSample{pos: 2000, pcr: 2000}, false},
		{"flag", pcrSample{pos: 2000, pcr: 2000, discontinuity: true}, true},
		{"backward", pcrSample{pos: 2000, pcr: 500}, true},
		{"equal", pcrSample{pos: 2000, pcr: 1000}, true},
		{"huge forward jump", pcrSample{pos: 2000, pcr: 1000 + 40*pcrClockHz}, true},
	}
	for _, c := range cases {
		if got := isSegmentBoundary(base, c.cur); got != c.want {
			t.Errorf("%s: isSegmentBoundary = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAnalyzePcrJitter_PerfectLine(t *testing.T) {
	res := analyzePcrJitter(linearSamples(4, 1000, 0, 1000))
	if res.segments != 1 {
		t.Errorf("segments = %d, want 1", res.segments)
	}
	if res.measured != 2 {
		t.Errorf("measured = %d, want 2", res.measured)
	}
	if math.Abs(res.maxNs) > 1e-6 || math.Abs(res.minNs) > 1e-6 {
		t.Errorf("expected ~0 jitter, got max=%f min=%f", res.maxNs, res.minNs)
	}
	if res.within500Pct != 100 {
		t.Errorf("within500Pct = %f, want 100", res.within500Pct)
	}
	if len(res.discontinuities) != 0 {
		t.Errorf("discontinuities = %d, want 0", len(res.discontinuities))
	}
}

func TestAnalyzePcrJitter_KnownPerturbation(t *testing.T) {
	// Middle PCR is 270 ticks above the line -> 270 * 1000/27 = 10000 ns = 10us.
	samples := []pcrSample{
		{pos: 0, pcr: 0},
		{pos: 1000, pcr: 1270},
		{pos: 2000, pcr: 2000},
	}
	res := analyzePcrJitter(samples)
	if res.measured != 1 {
		t.Fatalf("measured = %d, want 1", res.measured)
	}
	if math.Abs(res.maxNs-10000) > 1e-6 {
		t.Errorf("maxNs = %f, want 10000", res.maxNs)
	}
	if res.maxPos != 1000 {
		t.Errorf("maxPos = %d, want 1000", res.maxPos)
	}
	if res.within500Pct != 0 {
		t.Errorf("within500Pct = %f, want 0 (10us > 500ns)", res.within500Pct)
	}
}

func TestAnalyzePcrJitter_DiscontinuitySegments(t *testing.T) {
	samples := []pcrSample{
		{pos: 0, pcr: 0}, {pos: 1000, pcr: 1000}, {pos: 2000, pcr: 2000},
		{pos: 3000, pcr: 50, discontinuity: true}, // reset
		{pos: 4000, pcr: 1050}, {pos: 5000, pcr: 2050},
	}
	res := analyzePcrJitter(samples)
	if res.segments != 2 {
		t.Errorf("segments = %d, want 2", res.segments)
	}
	if res.measured != 2 {
		t.Errorf("measured = %d, want 2 (one interior per segment)", res.measured)
	}
	if len(res.discontinuities) != 1 {
		t.Fatalf("discontinuities = %d, want 1", len(res.discontinuities))
	}
	d := res.discontinuities[0]
	if d.pos != 3000 || d.fromSeg != 1 || d.toSeg != 2 {
		t.Errorf("unexpected discontinuity: %+v", d)
	}
	if d.jumpMs >= 0 {
		t.Errorf("expected backward (negative) jump, got %f", d.jumpMs)
	}
}

func TestPcrJitterDump_TooFewSamples(t *testing.T) {
	var j PcrJitter
	j.Add(0, 0, false)
	j.Add(1000, 1000, false)
	out := captureStdout(t, j.Dump)
	if !strings.Contains(out, "need at least 3 PCR samples") {
		t.Errorf("expected too-few-samples message, got:\n%s", out)
	}
}

func TestPcrJitterDump_Normal(t *testing.T) {
	var j PcrJitter
	for _, s := range linearSamples(5, 1000, 0, 1000) {
		j.Add(s.pos, s.pcr, s.discontinuity)
	}
	out := captureStdout(t, j.Dump)
	for _, want := range []string{"Max jitter", "Avg |jitter|", "Within +/-500ns", "Discontinuities  : 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPcrJitterDump_NormalWithDiscontinuity(t *testing.T) {
	var j PcrJitter
	// Two clean 6-sample segments split by one discontinuity (ratio 1/12, measurable).
	seg1 := linearSamples(6, 1000, 0, 1000)
	for _, s := range seg1 {
		j.Add(s.pos, s.pcr, s.discontinuity)
	}
	j.Add(6000, 10, true) // discontinuity
	for i := 1; i < 6; i++ {
		j.Add(6000+int64(i)*1000, 10+uint64(i)*1000, false)
	}
	out := captureStdout(t, j.Dump)
	if !strings.Contains(out, "Discontinuities  : 1") {
		t.Errorf("expected 1 discontinuity listed:\n%s", out)
	}
	if !strings.Contains(out, "PCR jumped") {
		t.Errorf("expected discontinuity detail line:\n%s", out)
	}
}

func TestPcrJitterDump_NoSegmentLongEnough(t *testing.T) {
	var j PcrJitter
	// Every sample after the first starts a new segment -> no interior samples.
	j.Add(0, 0, false)
	j.Add(1000, 1000, true)
	j.Add(2000, 2000, true)
	j.Add(3000, 3000, true)
	out := captureStdout(t, j.Dump)
	if !strings.Contains(out, "no segment long enough") {
		t.Errorf("expected no-segment message:\n%s", out)
	}
}

func TestPcrJitterDump_TooManyDiscontinuities(t *testing.T) {
	var j PcrJitter
	// One measurable 3-sample segment, then four single-sample segments:
	// measured>0 but discontinuity ratio 4/7 > 0.30.
	for _, s := range linearSamples(3, 1000, 0, 1000) {
		j.Add(s.pos, s.pcr, s.discontinuity)
	}
	for i := 0; i < 4; i++ {
		j.Add(3000+int64(i)*1000, 3000+uint64(i)*1000, true)
	}
	out := captureStdout(t, j.Dump)
	if !strings.Contains(out, "too many discontinuities") {
		t.Errorf("expected too-many-discontinuities message:\n%s", out)
	}
}

func TestAdaptationFieldDiscontinuityIndicator(t *testing.T) {
	af := NewAdaptationField()
	if af.DiscontinuityIndicator() {
		t.Error("expected false on fresh adaptation field")
	}
	af.discontinuityIndicator = 1
	if !af.DiscontinuityIndicator() {
		t.Error("expected true when discontinuity_indicator is set")
	}
}

// TestParseTsFile_DumpPcrJitterOption exercises the BufferPes collection and the
// end-of-stream Dump through the real parse path with several PCRs.
func TestParseTsFile_DumpPcrJitterOption(t *testing.T) {
	f, err := os.CreateTemp("", "pcrjitter*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	writeFullStream(f, 2, []uint64{13500, 27000, 40500})
	f.Close()

	var opt options.Options
	opt.DumpPcrJitter = true
	if err := ParseTsFile(f.Name(), opt); err != nil {
		t.Errorf("expected successful parse with DumpPcrJitter, got: %s", err)
	}
}
