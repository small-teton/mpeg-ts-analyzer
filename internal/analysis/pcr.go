package analysis

import "fmt"

// PCR timing: the PCR repetition interval and PCR time-base discontinuities.
//
// Every PCR-based result of the analyzer (the maximum PCR interval, the PCR
// discontinuity check, the PCR jitter estimate, the bitrate time base and the
// elapsed time of the time series) uses the same classification of each pair
// of consecutive PCRs of a program, made by PCRClock:
//
//   - first: the first PCR of the analyzed range (range acquisition). It has
//     no predecessor, so no interval is measured.
//   - pcr_pid_change: a PMT version changed the PCR_PID since the previous
//     PCR (PCR.PCRPIDChanged), or the PCR comes from a different PID than the
//     previous one. The PIDs need not share a clock, so the pair is not
//     compared, even when the PCR_PID changed away and back again.
//   - declared: a system time-base discontinuity signalled with
//     discontinuity_indicator on the PCR_PID (ISO/IEC 13818-1, 2.4.3.4 and
//     2.4.3.5); the PCR is the first sample of a new time base. Legal.
//   - continuous: the PCR is 0 to PCRJumpLimitMs later than the previous one.
//   - wraparound: the PCR is smaller, but counting across the 2^33*300
//     modulus it is at most PCRJumpLimitMs later. A normal interval.
//   - backward: the PCR is otherwise smaller, without discontinuity_indicator.
//   - forward_jump: the PCR is more than PCRJumpLimitMs later, without
//     discontinuity_indicator.
//
// continuous and wraparound pairs are comparable: they are measured as an
// interval and stay in one timing segment. Every other kind except first
// starts a new timing segment. backward and forward_jump are undeclared
// discontinuities: ETSI TR 101 290 (V1.4.1, table 5.0b, indicator 2.3b
// PCR_discontinuity_indicator_error) reports a difference between consecutive
// PCRs outside 0...100 ms without discontinuity_indicator. A file has no
// arrival times, so a forward step of 100 ms to PCRJumpLimitMs cannot be told
// apart from a PCR that genuinely arrived late (indicator 2.3a,
// PCR_repetition_error); it is measured as an interval, which then exceeds
// MaxPCRIntervalMs and fails the interval check, rather than reported as an
// undeclared discontinuity. Only a step that no PCR interval can explain
// (more than ten times the 100 ms limit) is a discontinuity.

const (
	// PCRClockHz is the 27 MHz system clock frequency of the PCR.
	PCRClockHz = 27_000_000
	// PCRTicksPerMs is one millisecond of the PCR clock.
	PCRTicksPerMs = PCRClockHz / 1000
	// PCRWrapTicks is the PCR modulus. program_clock_reference_base is a
	// 33-bit 90 kHz counter and the extension counts 0..299 at 27 MHz, so the
	// combined value wraps from 2^33*300-1 back to 0.
	PCRWrapTicks = (uint64(1) << 33) * 300
	// MaxPCRIntervalMs is the largest permitted interval between consecutive
	// PCRs of a program: ISO/IEC 13818-1, 2.7.2 (0.1 s), which ETSI TR 101 290
	// V1.4.1 indicator 2.3a also applies (its earlier 40 ms DVB
	// recommendation was withdrawn, note 2 of table 5.0b).
	MaxPCRIntervalMs = 100.0
	// PCRJumpLimitMs is the largest forward step between consecutive PCRs
	// that is still measured as an interval; a larger undeclared step is a
	// forward_jump discontinuity. It is ten times MaxPCRIntervalMs: beyond it
	// no late PCR explains the step.
	PCRJumpLimitMs = 1000.0
)

// PCR is one program_clock_reference that the parser accepted for a program's
// timing: a PCR on the program's PCR_PID in a packet without
// transport_error_indicator. Offset is the byte offset of the packet. Value is
// the PCR in 27 MHz ticks (program_clock_reference_base * 300 +
// program_clock_reference_extension), below PCRWrapTicks.
// DiscontinuityIndicator is set when the PCR packet has
// discontinuity_indicator set, or when an earlier packet of the PCR_PID had it
// set without carrying a PCR since the previous PCR: the next PCR of the PID
// then starts the new time base (ISO/IEC 13818-1, 2.4.3.5). PCRPIDChanged
// is set when a PMT version changed the program's PCR_PID since the previous
// PCR, even if a later version changed it back to the previous PCR's PID
// before a PCR arrived on the interim PID.
type PCR struct {
	PID                    uint16
	Offset                 int64
	Value                  uint64
	DiscontinuityIndicator bool
	PCRPIDChanged          bool
}

// PCRStepKind classifies a PCR relative to the previous PCR of its program.
type PCRStepKind int

// The step kinds, in the order the results list them.
const (
	PCRStepFirst PCRStepKind = iota
	PCRStepContinuous
	PCRStepWraparound
	PCRStepDeclared
	PCRStepPIDChange
	PCRStepBackward
	PCRStepForwardJump
	pcrStepKinds
)

// String returns the kind as reported in JSON.
func (k PCRStepKind) String() string {
	switch k {
	case PCRStepFirst:
		return "first"
	case PCRStepContinuous:
		return "continuous"
	case PCRStepWraparound:
		return "wraparound"
	case PCRStepDeclared:
		return "declared"
	case PCRStepPIDChange:
		return "pcr_pid_change"
	case PCRStepBackward:
		return "backward"
	case PCRStepForwardJump:
		return "forward_jump"
	default:
		return fmt.Sprintf("PCRStepKind(%d)", int(k))
	}
}

// PCRStep is the classification of one PCR.
type PCRStep struct {
	Kind PCRStepKind
	// Ticks is the interval to the previous PCR in 27 MHz ticks, counted
	// across the wrap; valid when the step is comparable.
	Ticks uint64
	// JumpTicks is how far the PCR moved from the previous one: the signed
	// difference of the values (this minus the previous one), except for a
	// wraparound, where it is the interval counted across the wrap; 0 for the
	// first PCR.
	JumpTicks int64
}

// Comparable reports whether the step is a measured interval within one
// timing segment (continuous or wraparound).
func (s PCRStep) Comparable() bool {
	return s.Kind == PCRStepContinuous || s.Kind == PCRStepWraparound
}

// Boundary reports whether the PCR starts a new timing segment after an
// earlier one: declared, pcr_pid_change, backward or forward_jump.
func (s PCRStep) Boundary() bool { return s.Kind != PCRStepFirst && !s.Comparable() }

// Undeclared reports whether the step is a discontinuity without
// discontinuity_indicator: backward or forward_jump.
func (s PCRStep) Undeclared() bool {
	return s.Kind == PCRStepBackward || s.Kind == PCRStepForwardJump
}

// PCRClock classifies consecutive PCRs of one program. Its state is the
// previous PCR only.
type PCRClock struct {
	prev    PCR
	hasPrev bool
}

// Observe classifies p against the previous PCR and makes p the previous one.
func (c *PCRClock) Observe(p PCR) PCRStep {
	prev, hasPrev := c.prev, c.hasPrev
	c.prev, c.hasPrev = p, true
	if !hasPrev {
		return PCRStep{Kind: PCRStepFirst}
	}
	step := PCRStep{JumpTicks: int64(p.Value) - int64(prev.Value)}
	limit := uint64(PCRJumpLimitMs * PCRTicksPerMs)
	switch {
	case p.PID != prev.PID || p.PCRPIDChanged:
		step.Kind = PCRStepPIDChange
	case p.DiscontinuityIndicator:
		step.Kind = PCRStepDeclared
	case p.Value >= prev.Value && p.Value-prev.Value <= limit:
		step.Kind, step.Ticks = PCRStepContinuous, p.Value-prev.Value
	case p.Value >= prev.Value:
		step.Kind = PCRStepForwardJump
	case p.Value+PCRWrapTicks-prev.Value <= limit:
		step.Kind, step.Ticks = PCRStepWraparound, p.Value+PCRWrapTicks-prev.Value
		step.JumpTicks = int64(step.Ticks)
	default:
		step.Kind = PCRStepBackward
	}
	return step
}

// PCRCheck aggregates the PCRs of one program into the maximum PCR interval
// and the PCR discontinuity summary. Its state is fixed: the previous PCR, the
// largest interval and one count, first offset and first jump per step kind,
// regardless of the input length.
type PCRCheck struct {
	clock     PCRClock
	pcrs      int
	intervals int
	maxTicks  uint64
	kinds     [pcrStepKinds]pcrKindState
}

type pcrKindState struct {
	count       int
	firstOffset int64
	firstJump   int64
}

// Observe classifies p, adds it to the summary and returns its step, so the
// caller's other PCR-based results use the same segments.
func (c *PCRCheck) Observe(p PCR) PCRStep {
	step := c.clock.Observe(p)
	c.pcrs++
	if step.Comparable() {
		c.intervals++
		c.maxTicks = max(c.maxTicks, step.Ticks)
	}
	kind := &c.kinds[step.Kind]
	if kind.count == 0 {
		kind.firstOffset, kind.firstJump = p.Offset, step.JumpTicks
	}
	kind.count++
	return step
}

// PCRIntervalResult is the maximum PCR interval: the largest comparable
// interval, the limit it is judged against and how many intervals were
// measured. Status is StatusSkipped without a measured interval, StatusOK when
// MaxMs is at most LimitMs (inclusive) and StatusErrors otherwise.
type PCRIntervalResult struct {
	Status    Status
	MaxMs     float64
	LimitMs   float64
	Intervals int
}

// Interval returns the maximum PCR interval measured so far.
func (c *PCRCheck) Interval() PCRIntervalResult {
	r := PCRIntervalResult{Status: StatusOK, MaxMs: float64(c.maxTicks) / PCRTicksPerMs, LimitMs: MaxPCRIntervalMs, Intervals: c.intervals}
	switch {
	case c.intervals == 0:
		r.Status = StatusSkipped
	case r.MaxMs > r.LimitMs:
		r.Status = StatusErrors
	}
	return r
}

// PCRStepCount is how often one kind of step occurred, with the byte offset
// of the first such PCR packet and its jump from the previous PCR.
type PCRStepCount struct {
	Kind        PCRStepKind
	Count       int
	FirstOffset int64
	FirstJumpMs float64
}

// PCRDiscontinuityResult is the PCR discontinuity summary. Status is
// StatusSkipped with fewer than two PCRs (no pair to classify), StatusErrors
// when at least one undeclared discontinuity (backward or forward_jump)
// occurred, and StatusOK otherwise; declared discontinuities, PCR_PID changes
// and wraparounds are legal and only listed. Steps lists every kind except
// first and continuous that occurred, in PCRStepKind order.
type PCRDiscontinuityResult struct {
	Status     Status
	PCRs       int
	Undeclared int
	Steps      []PCRStepCount
}

// Discontinuities returns the discontinuity summary of the PCRs so far.
func (c *PCRCheck) Discontinuities() PCRDiscontinuityResult {
	r := PCRDiscontinuityResult{Status: StatusOK, PCRs: c.pcrs, Steps: []PCRStepCount{}}
	for kind := PCRStepWraparound; kind < pcrStepKinds; kind++ {
		state := c.kinds[kind]
		if state.count == 0 {
			continue
		}
		r.Steps = append(r.Steps, PCRStepCount{
			Kind: kind, Count: state.count, FirstOffset: state.firstOffset,
			FirstJumpMs: float64(state.firstJump) / PCRTicksPerMs,
		})
		if (PCRStep{Kind: kind}).Undeclared() {
			r.Undeclared += state.count
		}
	}
	switch {
	case c.pcrs < 2:
		r.Status = StatusSkipped
	case r.Undeclared > 0:
		r.Status = StatusErrors
	}
	return r
}
