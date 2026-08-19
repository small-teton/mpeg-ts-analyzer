package tsparser

import (
	"strings"
	"testing"
)

// feed builds a PES carrying the given decode/presentation stamps and runs it
// through the checker. flags 2 = PTS only, 3 = PTS+DTS.
func feed(a *TimestampAnomaly, pid uint16, pos int64, flags uint8, pts, dts uint64) {
	a.Check(&Pes{pid: pid, pos: pos, ptsDtsFlags: flags, pts: pts, dts: dts})
}

const frame = uint64(3600) // 40 ms at 90 kHz

func TestAnomalyHealthyMonotonicIsQuiet(t *testing.T) {
	a := NewTimestampAnomaly()
	for i := uint64(0); i < 5; i++ {
		feed(a, 0x100, int64(i), 2, i*frame, 0)
	}
	if len(a.order) != 0 {
		t.Errorf("healthy monotonic stream reported anomalies: %v", a.order)
	}
}

func TestAnomalyIgnoresPesWithoutTimestamp(t *testing.T) {
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0, 0, 0, 0) // ptsDtsFlags 0: no PTS/DTS
	if len(a.order) != 0 {
		t.Error("a PES without PTS/DTS must not be checked")
	}
}

func TestAnomalyBFrameReorderIsQuiet(t *testing.T) {
	// Decode order I,P,B: DTS rises monotonically while PTS steps backward at the
	// B-frame. That is legal — only the decode timeline (DTS) must be monotonic,
	// and every DTS <= its PTS.
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0, 3, 1000, 1000)
	feed(a, 0x100, 1, 3, 1400, 1100) // P: presented later
	feed(a, 0x100, 2, 3, 1200, 1200) // B: PTS goes 1400 -> 1200, DTS 1100 -> 1200
	if len(a.order) != 0 {
		t.Errorf("B-frame reorder must not be flagged: %v", a.order)
	}
}

func TestAnomalyBackward(t *testing.T) {
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0x10, 2, 10000, 0)
	feed(a, 0x100, 0x20, 2, 9000, 0) // backward
	if a.counts[anomalyKey{0x100, kindBackward}] != 1 {
		t.Fatalf("expected one backward anomaly, got %v", a.counts)
	}
}

func TestAnomalyBackwardOnDtsTimeline(t *testing.T) {
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0x10, 3, 5000, 5000)
	feed(a, 0x100, 0x20, 3, 4000, 4000) // DTS backward
	if a.counts[anomalyKey{0x100, kindBackward}] != 1 {
		t.Fatalf("expected DTS backward anomaly, got %v", a.counts)
	}
}

func TestAnomalyWraparound(t *testing.T) {
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0x10, 2, uint64(ptsWrap)-frame, 0) // just below the 33-bit max
	feed(a, 0x100, 0x20, 2, frame, 0)                 // wrapped to near zero
	if a.counts[anomalyKey{0x100, kindWraparound}] != 1 {
		t.Fatalf("expected wraparound, got %v", a.counts)
	}
}

func TestAnomalyGap(t *testing.T) {
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0x10, 2, 0, 0)
	feed(a, 0x100, 0x20, 2, frame, 0)        // establishes the frame interval
	feed(a, 0x100, 0x30, 2, frame+480000, 0) // ~5.3 s jump
	if a.counts[anomalyKey{0x100, kindGap}] != 1 {
		t.Fatalf("expected a gap, got %v", a.counts)
	}
}

func TestAnomalyGapSubSecondUsesMs(t *testing.T) {
	// A ~0.7 s gap exercises durTicks' millisecond branch.
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0x10, 2, 0, 0)
	feed(a, 0x100, 0x20, 2, frame, 0)
	feed(a, 0x100, 0x30, 2, frame+63000, 0) // 700 ms
	if a.counts[anomalyKey{0x100, kindGap}] != 1 {
		t.Fatalf("expected a sub-second gap, got %v", a.counts)
	}
}

func TestAnomalyModerateJumpIsNotAGap(t *testing.T) {
	// A doubled interval (one dropped frame) is below both gap thresholds.
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0x10, 2, 0, 0)
	feed(a, 0x100, 0x20, 2, frame, 0)
	feed(a, 0x100, 0x30, 2, frame+2*frame, 0)
	if len(a.order) != 0 {
		t.Errorf("a doubled frame interval must not be a gap: %v", a.order)
	}
}

func TestAnomalyBackwardAcrossZeroBoundary(t *testing.T) {
	// A small backward step when the timestamp is near zero wraps the unsigned
	// value up past the max; it must be read as backward, not a huge gap.
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0x10, 2, 100, 0)
	feed(a, 0x100, 0x20, 2, uint64(ptsWrap)-100, 0) // 100 - 200 -> wraps up
	if a.counts[anomalyKey{0x100, kindBackward}] != 1 {
		t.Fatalf("expected backward across the zero boundary, got %v", a.counts)
	}
	if a.counts[anomalyKey{0x100, kindGap}] != 0 {
		t.Fatalf("must not be reported as a gap, got %v", a.counts)
	}
}

func TestAnomalyMixedPtsOnlyAndPtsDts(t *testing.T) {
	// A PID that alternates PTS-only and PTS+DTS access units stays on one decode
	// timeline (DTS when present, else PTS) and is not falsely flagged.
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0, 3, 1000, 1000) // decode 1000
	feed(a, 0x100, 1, 2, 1100, 0)    // decode 1100 (PTS==DTS)
	feed(a, 0x100, 2, 3, 1300, 1200) // decode 1200
	if len(a.order) != 0 {
		t.Errorf("mixed PTS-only / PTS+DTS must not be flagged: %v", a.order)
	}
}

func TestAnomalyDtsAfterPts(t *testing.T) {
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0x10, 3, 1000, 2000) // DTS 2000 > PTS 1000
	if a.counts[anomalyKey{0x100, kindDtsAfterPts}] != 1 {
		t.Fatalf("expected DTS-after-PTS, got %v", a.counts)
	}
}

func TestAnomalyAggregatesRepeats(t *testing.T) {
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0, 2, 10000, 0)
	feed(a, 0x100, 1, 2, 9000, 0) // backward #1
	feed(a, 0x100, 2, 2, 8000, 0) // backward #2
	feed(a, 0x100, 3, 2, 7000, 0) // backward #3
	if got := a.counts[anomalyKey{0x100, kindBackward}]; got != 3 {
		t.Fatalf("expected 3 backward occurrences, got %d", got)
	}
	if len(a.order) != 1 {
		t.Fatalf("repeats on one PID+kind must collapse to one report entry, got %d", len(a.order))
	}
}

func TestAnomalyDumpEmpty(t *testing.T) {
	// Clean stream: Dump prints nothing and does not panic.
	NewTimestampAnomaly().Dump()
}

func TestAnomalyDumpWithFindings(t *testing.T) {
	a := NewTimestampAnomaly()
	feed(a, 0x100, 0, 2, 10000, 0)
	feed(a, 0x100, 1, 2, 9000, 0) // backward #1
	feed(a, 0x100, 2, 2, 8000, 0) // backward #2 (collapsed)
	feed(a, 0x101, 0, 3, 1000, 2000)

	out := captureStdout(t, a.Dump)
	if !strings.Contains(out, "Timestamp Anomaly Report:") {
		t.Errorf("missing report header:\n%s", out)
	}
	// Two backward occurrences on 0x0100 collapse to one line with a count.
	if !strings.Contains(out, "(and 1 more)") {
		t.Errorf("repeated anomaly not collapsed with a count:\n%s", out)
	}
	// A single occurrence on 0x0101 has no count suffix.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "0x0101") && strings.Contains(line, "and ") {
			t.Errorf("single occurrence should not carry a count: %q", line)
		}
	}
}

func TestSignedDelta(t *testing.T) {
	cases := []struct {
		a, b uint64
		want int64
	}{
		{100, 50, 50},                           // normal forward
		{50, 100, -50},                          // normal backward
		{frame, uint64(ptsWrap) - frame, 7200},  // small forward across wrap (2*frame)
		{uint64(ptsWrap) - frame, frame, -7200}, // small backward across wrap
	}
	for _, c := range cases {
		if got := signedDelta(c.a, c.b); got != c.want {
			t.Errorf("signedDelta(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
