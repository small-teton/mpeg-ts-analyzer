package tsparser

import (
	"math"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
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

// cbrSamples builds n PCR samples of one PID, 40 ms apart, delivered at a
// constant 75.2 kbit/s (one 188-byte packet every 20 ms): n = 76 spans 3 s.
func cbrSamples(n int, pid uint16) []pcrSample {
	s := linearSamples(n, 2*188, 27_000_000, 40*pcrTicksPerMs)
	for i := range s {
		s[i].pid = pid
	}
	return s
}

func TestDeliveryWindows(t *testing.T) {
	// 76 samples at 40 ms: 75 intervals, 3 s, three windows of 25 intervals.
	samples := cbrSamples(76, 0x100)
	windows := deliveryWindows(samples, 188)
	if len(windows) != 3 || windows[0] != (deliveryWindow{bytes: 25 * 2 * 188, ticks: pcrClockHz}) {
		t.Fatalf("windows = %+v", windows)
	}
	// 192-byte packets count as 188 transport-stream bytes, and 0 means 188.
	m2ts := cbrSamples(76, 0x100)
	for i := range m2ts {
		m2ts[i].pos = m2ts[i].pos / 188 * 192
	}
	if got := deliveryWindows(m2ts, 192); !slices.Equal(got, windows) {
		t.Errorf("192-byte windows = %+v", got)
	}
	if got := deliveryWindows(samples, 0); !slices.Equal(got, windows) {
		t.Errorf("default packet size windows = %+v", got)
	}
	// A discontinuity drops the interval across it and the partial window
	// before it: 30 intervals, then 45 in the next segment.
	broken := cbrSamples(76, 0x100)
	broken[30].discontinuity = true
	if got := deliveryWindows(broken, 188); len(got) != 2 || got[1].ticks != pcrClockHz {
		t.Errorf("windows across a discontinuity = %+v", got)
	}
}

func TestAssessDelivery(t *testing.T) {
	constant := assessDelivery(cbrSamples(76, 0x100), 188, 0)
	if constant.assessment != deliveryConstant || constant.windows != 3 || constant.averageBps != 75_200 || constant.minBps != 75_200 ||
		constant.maxBps != 75_200 || constant.maxDeviationPct != 0 || !strings.Contains(constant.reason, "deviates up to 0.00% from the average 75,200 b/s") {
		t.Errorf("constant: %+v", constant)
	}
	// The second second carries 3 % more bytes than the others.
	variable := cbrSamples(76, 0x100)
	for i := 26; i < len(variable); i++ {
		variable[i].pos += 3 * 188
		if i >= 51 {
			variable[i].pos -= 3 * 188
		}
	}
	got := assessDelivery(variable, 188, 0)
	if got.assessment != deliveryVariable || got.maxDeviationPct < 1 || !strings.Contains(got.reason, "tolerance 1%") {
		t.Errorf("variable windows: %+v", got)
	}
	// Window rates within the tolerance, but a jitter of more than 1 ms.
	if got := assessDelivery(cbrSamples(76, 0x100), 188, 1_500_000); got.assessment != deliveryVariable || !strings.Contains(got.reason, "1.500ms") {
		t.Errorf("implausible jitter: %+v", got)
	}
	// It applies with too few windows as well.
	if got := assessDelivery(cbrSamples(10, 0x100), 188, 1_500_000); got.assessment != deliveryVariable {
		t.Errorf("implausible jitter, one window: %+v", got)
	}
	if got := assessDelivery(cbrSamples(30, 0x100), 188, pcrPlausibleJitterNs); got.assessment != deliveryUnknown || got.windows != 1 ||
		!strings.Contains(got.reason, "got 1") {
		t.Errorf("one window: %+v", got)
	}
	if got := assessDelivery(nil, 188, -1); got.assessment != deliveryUnknown || got.windows != 0 {
		t.Errorf("no samples: %+v", got)
	}
}

func TestPcrJitterEvaluate(t *testing.T) {
	jitterAt := func(n int, ticks uint64) *PcrJitter {
		j := &PcrJitter{samples: cbrSamples(n, 0x100)}
		j.samples[n/2].pcr += ticks
		return j
	}
	cases := []struct {
		name   string
		j      *PcrJitter
		status string
		reason string
	}{
		{"ok", jitterAt(76, 0), jitterStatusOk, ""},
		{"warning", jitterAt(76, 27), jitterStatusWarning, ""},
		{"ng", jitterAt(76, 2700), jitterStatusNg, ""},
		{"ng at the plausibility limit", jitterAt(76, 27_000), jitterStatusNg, ""},
		{"variable", jitterAt(76, 27_001), jitterStatusNotAssessable, "delivery rate is variable"},
		{"unknown", jitterAt(30, 2700), jitterStatusNotAssessable, "delivery rate is unknown"},
		{"skipped", &PcrJitter{samples: cbrSamples(2, 0x100)}, jitterStatusSkipped, "need at least 3 PCR samples"},
	}
	for _, c := range cases {
		e := c.j.evaluate()
		if e.status != c.status || !strings.HasPrefix(e.reason, c.reason) {
			t.Errorf("%s: status %q reason %q", c.name, e.status, e.reason)
		}
	}
	// skipped takes precedence over a variable delivery.
	broken := &PcrJitter{samples: cbrSamples(76, 0x100)}
	for i := 3; i < len(broken.samples); i += 3 {
		broken.samples[i].discontinuity = true
	}
	if e := broken.evaluate(); e.status != jitterStatusSkipped || e.reason != "too many discontinuities" || e.estimated {
		t.Errorf("broken: %+v", e)
	}
}

func TestPcrJitterDump_NotAssessable(t *testing.T) {
	j := &PcrJitter{samples: cbrSamples(30, 0x100)}
	out := captureStdout(t, j.Dump)
	for _, want := range []string{"Delivery         : UNKNOWN - need at least 2 one-second windows", "Status           : NOT ASSESSABLE (delivery rate is unknown"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	j = &PcrJitter{samples: cbrSamples(76, 0x100)}
	out = captureStdout(t, j.Dump)
	for _, want := range []string{"Delivery         : CONSTANT - the multiplex rate of 3 one-second windows", "Status           : OK"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPcrTickNs(t *testing.T) {
	// 27 ticks == 1000 ns exactly (1 tick = 1000/27 ns).
	if got := pcrTickNs(27); math.Abs(got-1000) > 1e-9 {
		t.Errorf("pcrTickNs(27) = %f, want 1000", got)
	}
}

func TestPcrStepBoundaries(t *testing.T) {
	base := pcrSample{pos: 1000, pcr: 1000}
	cases := []struct {
		name string
		cur  pcrSample
		want bool
	}{
		{"normal", pcrSample{pos: 2000, pcr: 2000}, false},
		{"flag", pcrSample{pos: 2000, pcr: 2000, discontinuity: true}, true},
		{"backward", pcrSample{pos: 2000, pcr: 500}, true},
		{"equal", pcrSample{pos: 2000, pcr: 1000}, false},
		{"PID change", pcrSample{pos: 2000, pid: 0x101, pcr: 2000}, true},
		{"huge forward jump", pcrSample{pos: 2000, pcr: 1000 + 40*pcrClockHz}, true},
	}
	for _, c := range cases {
		if got := pcrStep(base, c.cur).Boundary(); got != c.want {
			t.Errorf("%s: boundary = %v, want %v", c.name, got, c.want)
		}
	}
	// A wraparound stays in the segment.
	if step := pcrStep(pcrSample{pcr: pcrWrapTicks - 10}, pcrSample{pcr: 30}); step.Boundary() || step.Ticks != 40 {
		t.Errorf("wraparound: %+v", step)
	}
}

// TestAnalyzePcrJitter_AcrossWraparound measures jitter across the 33-bit
// wrap as within one segment, with the clock counted across it.
func TestAnalyzePcrJitter_AcrossWraparound(t *testing.T) {
	samples := []pcrSample{
		{pos: 0, pcr: pcrWrapTicks - 2000}, {pos: 1000, pcr: pcrWrapTicks - 730}, // 270 ticks late
		{pos: 2000, pcr: 0}, {pos: 3000, pcr: 1000},
	}
	res := analyzePcrJitter(samples)
	if res.segments != 1 || len(res.discontinuities) != 0 || res.measured != 2 {
		t.Fatalf("result = %+v", res)
	}
	if math.Abs(res.maxNs-10000) > 1e-6 || res.maxPos != 1000 {
		t.Errorf("max = %f at %d, want 10000 at 1000", res.maxNs, res.maxPos)
	}
	if windows := deliveryWindows([]pcrSample{{pos: 0, pcr: pcrWrapTicks - pcrClockHz/2}, {pos: 188, pcr: pcrClockHz / 2}}, 188); len(windows) != 1 ||
		windows[0].ticks != pcrClockHz {
		t.Errorf("windows across the wrap = %+v", windows)
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
	if d.pos != 3000 || d.kind != analysis.PCRStepDeclared || d.fromSeg != 1 || d.toSeg != 2 {
		t.Errorf("unexpected discontinuity: %+v", d)
	}
	if d.jumpMs >= 0 {
		t.Errorf("expected backward (negative) jump, got %f", d.jumpMs)
	}
}

func TestPcrJitterDump_TooFewSamples(t *testing.T) {
	var j PcrJitter
	j.Add(0, 0x0100, 0, false)
	j.Add(1000, 0x0100, 1000, false)
	out := captureStdout(t, j.Dump)
	if !strings.Contains(out, "need at least 3 PCR samples") {
		t.Errorf("expected too-few-samples message, got:\n%s", out)
	}
}

func TestPcrJitterDump_Normal(t *testing.T) {
	var j PcrJitter
	for _, s := range linearSamples(5, 1000, 0, 1000) {
		j.Add(s.pos, 0x0100, s.pcr, s.discontinuity)
	}
	out := captureStdout(t, j.Dump)
	// Too short to establish the delivery rate.
	for _, want := range []string{"Max jitter", "Avg |jitter|", "Within +/-500ns", "Status           : NOT ASSESSABLE", "Discontinuities  : 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPcrJitterVerdict(t *testing.T) {
	cases := []struct {
		maxAbsNs float64
		want     string
	}{
		{0, "OK"},
		{pcrAccuracyLimitNs, "OK"}, // exactly ±500ns is still OK
		{10_000, "WARNING"},        // 10us: over ISO, under DVB
		{pcrDvbLimitNs, "WARNING"}, // exactly ±25us is still within DVB
		{pcrDvbLimitNs + 1, "NG"},  // just over ±25us
	}
	for _, c := range cases {
		if got := pcrJitterVerdict(c.maxAbsNs); !strings.HasPrefix(got, c.want) {
			t.Errorf("pcrJitterVerdict(%.0f) = %q, want prefix %q", c.maxAbsNs, got, c.want)
		}
	}
}

func TestPcrJitterDump_NormalWithDiscontinuity(t *testing.T) {
	var j PcrJitter
	// Two clean 6-sample segments split by one discontinuity (ratio 1/12, measurable).
	seg1 := linearSamples(6, 1000, 0, 1000)
	for _, s := range seg1 {
		j.Add(s.pos, 0x0100, s.pcr, s.discontinuity)
	}
	j.Add(6000, 0x0100, 10, true) // discontinuity
	for i := 1; i < 6; i++ {
		j.Add(6000+int64(i)*1000, 0x0100, 10+uint64(i)*1000, false)
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
	j.Add(0, 0x0100, 0, false)
	j.Add(1000, 0x0100, 1000, true)
	j.Add(2000, 0x0100, 2000, true)
	j.Add(3000, 0x0100, 3000, true)
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
		j.Add(s.pos, 0x0100, s.pcr, s.discontinuity)
	}
	for i := 0; i < 4; i++ {
		j.Add(3000+int64(i)*1000, 0x0100, 3000+uint64(i)*1000, true)
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
	defer func() { _ = os.Remove(f.Name()) }()

	writeFullStream(f, 2, []uint64{13500, 27000, 40500})
	_ = f.Close()

	var opt options.Options
	opt.DumpPcrJitter = true
	if err := ParseTsFile(f.Name(), opt); err != nil {
		t.Errorf("expected successful parse with DumpPcrJitter, got: %s", err)
	}
}
