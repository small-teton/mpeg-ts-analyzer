package analysis

import (
	"reflect"
	"testing"
)

const ms = PCRTicksPerMs

func TestPCRClockClassifiesEveryStep(t *testing.T) {
	top := PCRWrapTicks - 10*ms
	tests := []struct {
		name      string
		prev, cur PCR
		want      PCRStep
	}{
		{"continuous", PCR{PID: 0x100, Value: 1000 * ms}, PCR{PID: 0x100, Value: 1040 * ms},
			PCRStep{Kind: PCRStepContinuous, Ticks: 40 * ms, JumpTicks: 40 * ms}},
		{"equal value is a zero interval", PCR{PID: 0x100, Value: 5 * ms}, PCR{PID: 0x100, Value: 5 * ms},
			PCRStep{Kind: PCRStepContinuous}},
		{"late PCR within the jump limit is an interval", PCR{PID: 0x100, Value: 0}, PCR{PID: 0x100, Value: 1000 * ms},
			PCRStep{Kind: PCRStepContinuous, Ticks: 1000 * ms, JumpTicks: 1000 * ms}},
		{"forward jump", PCR{PID: 0x100, Value: 0}, PCR{PID: 0x100, Value: 1000*ms + 1},
			PCRStep{Kind: PCRStepForwardJump, JumpTicks: 1000*ms + 1}},
		{"wraparound", PCR{PID: 0x100, Value: top}, PCR{PID: 0x100, Value: 30 * ms},
			PCRStep{Kind: PCRStepWraparound, Ticks: 40 * ms, JumpTicks: 40 * ms}},
		{"backward", PCR{PID: 0x100, Value: 3000 * ms}, PCR{PID: 0x100, Value: 1000 * ms},
			PCRStep{Kind: PCRStepBackward, JumpTicks: -2000 * ms}},
		{"backward near the top is not a wrap", PCR{PID: 0x100, Value: top}, PCR{PID: 0x100, Value: 2000 * ms},
			PCRStep{Kind: PCRStepBackward, JumpTicks: int64(2000*ms) - int64(top)}},
		{"declared", PCR{PID: 0x100, Value: 1000 * ms}, PCR{PID: 0x100, Value: 1040 * ms, DiscontinuityIndicator: true},
			PCRStep{Kind: PCRStepDeclared, JumpTicks: 40 * ms}},
		{"PCR_PID changed away and back", PCR{PID: 0x100, Value: 1000 * ms}, PCR{PID: 0x100, Value: 1040 * ms, PCRPIDChanged: true},
			PCRStep{Kind: PCRStepPIDChange, JumpTicks: 40 * ms}},
		{"PID change takes precedence", PCR{PID: 0x100, Value: 1000 * ms}, PCR{PID: 0x101, Value: 1040 * ms, DiscontinuityIndicator: true},
			PCRStep{Kind: PCRStepPIDChange, JumpTicks: 40 * ms}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c PCRClock
			if got := c.Observe(tt.prev); got != (PCRStep{Kind: PCRStepFirst}) {
				t.Fatalf("first step = %+v", got)
			}
			if got := c.Observe(tt.cur); got != tt.want {
				t.Errorf("step = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPCRStepPredicates(t *testing.T) {
	for kind, want := range map[PCRStepKind][3]bool{ // comparable, boundary, undeclared
		PCRStepFirst:       {false, false, false},
		PCRStepContinuous:  {true, false, false},
		PCRStepWraparound:  {true, false, false},
		PCRStepDeclared:    {false, true, false},
		PCRStepPIDChange:   {false, true, false},
		PCRStepBackward:    {false, true, true},
		PCRStepForwardJump: {false, true, true},
	} {
		s := PCRStep{Kind: kind}
		if got := [3]bool{s.Comparable(), s.Boundary(), s.Undeclared()}; got != want {
			t.Errorf("%v: comparable/boundary/undeclared = %v, want %v", kind, got, want)
		}
	}
}

func TestPCRStepKindString(t *testing.T) {
	want := []string{"first", "continuous", "wraparound", "declared", "pcr_pid_change", "backward", "forward_jump", "PCRStepKind(7)"}
	for kind := PCRStepFirst; kind <= pcrStepKinds; kind++ {
		if got := kind.String(); got != want[kind] {
			t.Errorf("String(%d) = %q, want %q", int(kind), got, want[kind])
		}
	}
}

func TestPCRCheckSkippedBelowTwoPCRs(t *testing.T) {
	var c PCRCheck
	want := PCRDiscontinuityResult{Status: StatusSkipped, Steps: []PCRStepCount{}}
	if got := c.Discontinuities(); !reflect.DeepEqual(got, want) {
		t.Errorf("no PCR: %+v", got)
	}
	c.Observe(PCR{PID: 0x100, Offset: 188, Value: 0})
	want.PCRs = 1
	if got := c.Discontinuities(); !reflect.DeepEqual(got, want) {
		t.Errorf("one PCR: %+v", got)
	}
	if got := c.Interval(); got != (PCRIntervalResult{Status: StatusSkipped, LimitMs: 100}) {
		t.Errorf("interval = %+v", got)
	}
}

func TestPCRCheckAggregatesKinds(t *testing.T) {
	var c PCRCheck
	top := PCRWrapTicks - 20*ms
	pcrs := []PCR{
		{PID: 0x100, Offset: 0, Value: top},                                       // first
		{PID: 0x100, Offset: 100, Value: 20 * ms},                                 // wraparound, 40 ms
		{PID: 0x100, Offset: 200, Value: 100 * ms},                                // continuous, 80 ms
		{PID: 0x100, Offset: 300, Value: 5000 * ms, DiscontinuityIndicator: true}, // declared
		{PID: 0x100, Offset: 400, Value: 3000 * ms},                               // backward
		{PID: 0x100, Offset: 500, Value: 3060 * ms},                               // continuous, 60 ms
		{PID: 0x100, Offset: 600, Value: 9060 * ms},                               // forward jump
		{PID: 0x100, Offset: 700, Value: 1000 * ms},                               // backward again
		{PID: 0x101, Offset: 800, Value: 1040 * ms},                               // PID change
		{PID: 0x101, Offset: 900, Value: 1200 * ms},                               // continuous, 160 ms
	}
	for _, p := range pcrs {
		c.Observe(p)
	}
	if got, want := c.Interval(), (PCRIntervalResult{Status: StatusErrors, MaxMs: 160, LimitMs: 100, Intervals: 4}); got != want {
		t.Errorf("interval = %+v, want %+v", got, want)
	}
	want := PCRDiscontinuityResult{Status: StatusErrors, PCRs: 10, Undeclared: 3, Steps: []PCRStepCount{
		{Kind: PCRStepWraparound, Count: 1, FirstOffset: 100, FirstJumpMs: 40},
		{Kind: PCRStepDeclared, Count: 1, FirstOffset: 300, FirstJumpMs: 4900},
		{Kind: PCRStepPIDChange, Count: 1, FirstOffset: 800, FirstJumpMs: 40},
		{Kind: PCRStepBackward, Count: 2, FirstOffset: 400, FirstJumpMs: -2000},
		{Kind: PCRStepForwardJump, Count: 1, FirstOffset: 600, FirstJumpMs: 6000},
	}}
	if got := c.Discontinuities(); !reflect.DeepEqual(got, want) {
		t.Errorf("discontinuities = %+v\nwant %+v", got, want)
	}
}

func TestPCRCheckDeclaredOnlyIsOK(t *testing.T) {
	var c PCRCheck
	c.Observe(PCR{PID: 0x100, Value: 0})
	c.Observe(PCR{PID: 0x100, Offset: 188, Value: 0, DiscontinuityIndicator: true})
	got := c.Discontinuities()
	if got.Status != StatusOK || got.Undeclared != 0 || len(got.Steps) != 1 {
		t.Errorf("declared only: %+v", got)
	}
	if c.Interval().Intervals != 0 {
		t.Errorf("an interval was measured across a declared discontinuity")
	}
}

// TestPCRCheckIntervalStatus judges the interval against the inclusive 100 ms
// limit (ISO/IEC 13818-1, 2.7.2).
func TestPCRCheckIntervalStatus(t *testing.T) {
	for _, tt := range []struct {
		stepMs uint64
		want   Status
	}{{100, StatusOK}, {101, StatusErrors}} {
		var c PCRCheck
		c.Observe(PCR{PID: 0x100, Value: 0})
		c.Observe(PCR{PID: 0x100, Value: tt.stepMs * ms})
		if got := c.Interval(); got.Status != tt.want || got.MaxMs != float64(tt.stepMs) || got.Intervals != 1 {
			t.Errorf("%d ms: %+v", tt.stepMs, got)
		}
	}
}
