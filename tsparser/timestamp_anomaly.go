package tsparser

import "fmt"

// PTS/DTS continuity and monotonicity checks (ISO/IEC 13818-1 §2.4.3.7).
//
// Properly advancing PTS/DTS values are essential for smooth playback. This
// detects the common failure modes — timestamps going backward, 33-bit
// wraparound, large forward jumps (ad insertion / splicing), and DTS after PTS
// — that are hard to spot without packet-level inspection.
//
// Monotonicity is judged on the *decode* timeline: DTS when present, otherwise
// PTS (which equals DTS when no DTS is signalled). Presentation-order PTS
// legitimately steps backward between B-frames, so it is not a reliable
// monotonic axis; the decode timeline is.
//
// The check is always on; a healthy stream produces no output. Repeated
// occurrences of the same problem on the same PID are collapsed to one line
// (the first occurrence, plus a count of the rest), so a badly broken stream
// that trips on every access unit stays readable.

const (
	// ptsClockHz is the 90 kHz clock PTS/DTS are expressed in.
	ptsClockHz = 90000
	// ptsWrap is the 33-bit PTS/DTS wrap period (~26.5 hours in ticks).
	ptsWrap = int64(1) << 33
	// ptsHalfWrap separates a genuine backward step from a 33-bit counter
	// wraparound. A raw drop larger than half the range is a forward wrap (the
	// counter overflowed 2^33 -> 0); a raw *rise* larger than half the range is a
	// small backward step whose unsigned value wrapped up past zero.
	ptsHalfWrap = ptsWrap / 2
	// tsGapRatio: a forward decode step at least this many times the stream's
	// smallest observed step (its frame interval) is a candidate gap.
	tsGapRatio = 5
	// tsGapMinTicks: ignore gaps whose excess over the frame interval is below
	// this (0.5 s) so a variable GOP or a single dropped frame is not reported.
	tsGapMinTicks = ptsClockHz / 2
)

// anomalyKind identifies a category of PTS/DTS anomaly, used only to group
// repeated occurrences on the same PID.
type anomalyKind int

const (
	kindBackward anomalyKind = iota
	kindWraparound
	kindGap
	kindDtsAfterPts
)

// tsState tracks the decode timeline of one PID.
type tsState struct {
	have    bool
	last    uint64 // last decode timestamp (DTS, or PTS when no DTS)
	lastPos int64  // byte position of that timestamp's PES
	minStep int64  // smallest positive step seen (~frame interval), 0 until known
}

type anomalyKey struct {
	pid  uint16
	kind anomalyKind
}

// TimestampAnomaly collects PTS/DTS anomalies per PID during a scan and prints
// them at the end.
type TimestampAnomaly struct {
	states map[uint16]*tsState
	order  []anomalyKey          // (PID, kind) in first-seen order
	first  map[anomalyKey]string // first occurrence's message
	counts map[anomalyKey]int    // total occurrences
}

// NewTimestampAnomaly creates an empty collector.
func NewTimestampAnomaly() *TimestampAnomaly {
	return &TimestampAnomaly{
		states: make(map[uint16]*tsState),
		first:  make(map[anomalyKey]string),
		counts: make(map[anomalyKey]int),
	}
}

// Check examines one successfully parsed PES that carries PTS/DTS.
func (a *TimestampAnomaly) Check(p *Pes) {
	if p.ptsDtsFlags != 2 && p.ptsDtsFlags != 3 {
		return
	}

	// DTS must be at or before PTS within one access unit (only meaningful when
	// both are present). signedDelta folds the 33-bit wrap so a stamp straddling
	// the wrap boundary is not misread as a violation.
	if p.ptsDtsFlags == 3 && signedDelta(p.pts, p.dts) < 0 {
		a.add(p.pid, kindDtsAfterPts, fmt.Sprintf(
			"DTS is later than PTS in the access unit at 0x%08x (DTS %s > PTS %s)",
			p.pos, stampMs(p.dts), stampMs(p.pts)))
	}

	label, decode := "PTS", p.pts
	if p.ptsDtsFlags == 3 {
		label, decode = "DTS", p.dts
	}

	st := a.states[p.pid]
	if st == nil {
		st = &tsState{}
		a.states[p.pid] = st
	}
	if !st.have {
		st.have, st.last, st.lastPos = true, decode, p.pos
		return
	}

	// Raw (unfolded) difference of two values in [0, 2^33), so the result is in
	// (-2^33, 2^33). A forward counter overflow shows as a near -2^33 drop; a
	// backward step across zero shows as a near +2^33 rise.
	raw := int64(decode) - int64(st.last)
	switch {
	case raw <= -ptsHalfWrap:
		a.add(p.pid, kindWraparound, fmt.Sprintf(
			"%s wrapped around (33-bit counter overflow) at 0x%08x", label, p.pos))
	case raw < 0 || raw >= ptsHalfWrap:
		a.add(p.pid, kindBackward, fmt.Sprintf(
			"%s went backward at 0x%08x (%s) after 0x%08x (%s)",
			label, p.pos, stampMs(decode), st.lastPos, stampMs(st.last)))
	default:
		if st.minStep > 0 && raw > int64(tsGapRatio)*st.minStep && raw-st.minStep > tsGapMinTicks {
			a.add(p.pid, kindGap, fmt.Sprintf(
				"%s jumped forward %s at 0x%08x (%s) after 0x%08x (%s), expected ~%s",
				label, durTicks(raw), p.pos, stampMs(decode), st.lastPos, stampMs(st.last), durTicks(st.minStep)))
		}
		if raw > 0 && (st.minStep == 0 || raw < st.minStep) {
			st.minStep = raw
		}
	}
	st.last, st.lastPos = decode, p.pos
}

// Dump prints the anomaly report, or nothing when the stream is clean.
func (a *TimestampAnomaly) Dump() {
	if len(a.order) == 0 {
		return
	}
	fmt.Println("-----------------------------")
	fmt.Println("Timestamp Anomaly Report:")
	for _, k := range a.order {
		line := fmt.Sprintf("  PID 0x%04x: %s", k.pid, a.first[k])
		if extra := a.counts[k] - 1; extra > 0 {
			line += fmt.Sprintf(" (and %d more)", extra)
		}
		fmt.Println(line)
	}
}

// add records one occurrence; only the first per (PID, kind) keeps its message.
func (a *TimestampAnomaly) add(pid uint16, kind anomalyKind, msg string) {
	key := anomalyKey{pid, kind}
	if a.counts[key] == 0 {
		a.first[key] = msg
		a.order = append(a.order, key)
	}
	a.counts[key]++
}

// signedDelta returns (a - b) folded into the 33-bit wrap window
// [-2^32, 2^32), so a small step reads with its true sign regardless of a wrap
// between the two values.
func signedDelta(a, b uint64) int64 {
	d := (int64(a) - int64(b)) % ptsWrap
	switch {
	case d >= ptsHalfWrap:
		d -= ptsWrap
	case d < -ptsHalfWrap:
		d += ptsWrap
	}
	return d
}

// stampMs formats a 90 kHz stamp as milliseconds.
func stampMs(stamp uint64) string { return fmt.Sprintf("%.3fms", float64(stamp)/90.0) }

// durTicks formats a 90 kHz tick count as a human duration (s for >= 1 s).
func durTicks(ticks int64) string {
	ms := float64(ticks) / 90.0
	if ms >= 1000 {
		return fmt.Sprintf("%.1fs", ms/1000)
	}
	return fmt.Sprintf("%.0fms", ms)
}
