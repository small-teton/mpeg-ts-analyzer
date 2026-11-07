package tsparser

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

// PCR jitter analysis.
//
// What this measures — and what it does NOT
// -----------------------------------------
// "PCR jitter" (a.k.a. PCR accuracy; ISO/IEC 13818-1 §2.4.2.2, DVB TR 101 290
// §5.3.2 "PCR_AC") is the error in each PCR *value*: how far the stamped clock
// is from the time that byte was actually delivered. This is a different thing
// from the PCR *interval* check (how often a PCR appears, ≤100 ms) that the tool
// already reports — a PCR can arrive on time (good interval) yet carry a wrong
// value (bad jitter), which upsets the decoder's clock recovery (audio pops,
// video stutter).
//
// A file has no real arrival timestamps, so the "expected" delivery time of a
// PCR is approximated from its byte position, assuming the transport stream is
// delivered at a (locally) constant byte rate — i.e. byte position stands in for
// time (the "byte_position" model). That premise holds for constant-bitrate
// (CBR) delivery. The result is therefore a file-based estimate, not a hardware
// TR 101 290 measurement against a real reference clock.
//
// Without arrival times, a PCR stamping error and a change of delivery rate are
// the same observation, so the estimate is judged only when the stream itself
// shows constant delivery (see assessDelivery): its multiplex rate over 1-second
// windows of PCR time stays within ±1 % of the range average, and no estimated
// jitter reaches the millisecond scale that only rate variation explains. A
// variable or unknown delivery makes the status not_assessable; the estimate is
// still reported. Validated M2TS arrival timestamps could later provide a second
// model that needs no such assessment.
//
// To stay robust to variable bitrate (VBR), jitter is measured *per interval*
// (locally), not against one global straight line: each interior PCR is compared
// to the value obtained by interpolating its two neighbours by byte position.
// Over three consecutive PCRs the byte rate is ~constant even when it drifts
// across the whole stream, so a smooth rate change affects the neighbours too
// and cancels out, leaving only genuine PCR error. (We do not assume PCRs are
// evenly spaced; the actual byte positions are used.)
//
// A PCR discontinuity splits the samples into timing segments: a declared
// discontinuity, a PCR_PID change, an undeclared backward step or a forward
// jump of more than 1000 ms, classified by analysis.PCRClock exactly as for the
// maximum PCR interval and the bitrate time base. A 33-bit wraparound is not a
// discontinuity; the samples are compared across it. Jitter is measured within
// a segment only, and the discontinuities are reported separately — including
// their kind, how many and how large — so a broken stream is visible rather
// than silently inflating the jitter numbers. If the stream is too
// discontinuous to estimate from, the analysis is skipped.
const (
	// pcrClockHz is the PCR system clock frequency (27 MHz); one PCR tick is
	// 1/27_000_000 s.
	pcrClockHz = analysis.PCRClockHz
	// pcrTicksPerMs is 27 MHz expressed per millisecond.
	pcrTicksPerMs = analysis.PCRTicksPerMs
	// pcrAccuracyLimitNs is the PCR accuracy limit from ISO/IEC 13818-1: ±500 ns.
	// Max |jitter| within this is a pass (OK).
	pcrAccuracyLimitNs = 500.0
	// pcrDvbLimitNs is the ±25 µs jitter tolerance DVB expects compliant
	// receivers to handle. Max |jitter| between the ISO limit and this is a
	// warning; beyond it is a failure (NG).
	pcrDvbLimitNs = 25_000.0
	// pcrJitterGiveUpRatio: if more than this fraction of PCR samples start a new
	// segment, the stream is too broken to estimate jitter from.
	pcrJitterGiveUpRatio = 0.30
	// deliveryWindowTicks is the length of a delivery-rate window: 1 second of
	// PCR time.
	deliveryWindowTicks = pcrClockHz
	// deliveryMinWindows is the number of windows needed to compare rates.
	deliveryMinWindows = 2
	// deliveryTolerancePercent is how far a window's multiplex rate may deviate
	// from the range average for delivery to count as constant. A CBR
	// multiplex measures 0.00 % (windows end on PCR packets, so the rate is
	// exact up to PCR error); the bundled VBR samples deviate by 2.5-3 % and
	// longer VBR encodes by over 10 %.
	deliveryTolerancePercent = 1.0
	// pcrPlausibleJitterNs is the largest byte-position jitter that can still be
	// a PCR stamping error (1 ms, 40 times the DVB tolerance). Beyond it the
	// "jitter" is a sizeable fraction of a PCR interval, which only a varying
	// delivery rate explains.
	pcrPlausibleJitterNs = 1_000_000.0
)

// PCR jitter statuses and delivery assessments as reported in JSON.
const (
	jitterStatusOk            = "ok"
	jitterStatusWarning       = "warning"
	jitterStatusNg            = "ng"
	jitterStatusNotAssessable = "not_assessable"
	jitterStatusSkipped       = "skipped"

	deliveryConstant = "constant"
	deliveryVariable = "variable"
	deliveryUnknown  = "unknown"
)

// pcrTickNs converts a (possibly fractional) count of 27 MHz PCR ticks to
// nanoseconds: 1 tick = 1e9 / 27e6 = 1000/27 ns.
func pcrTickNs(ticks float64) float64 { return ticks * 1000.0 / 27.0 }

// pcrSample is one PCR observation: the byte offset and PID of the
// PCR-carrying packet and the PCR value in 27 MHz ticks. discontinuity is set
// when the PCR is declared to start a new time base, and pidChanged when a
// PMT version changed the PCR_PID since the previous PCR (see analysis.PCR).
type pcrSample struct {
	pos           int64
	pid           uint16
	pcr           uint64
	discontinuity bool
	pidChanged    bool
}

// pcrStep classifies cur against the PCR before it, prev, with the policy
// shared by every PCR-based result (analysis.PCRClock).
func pcrStep(prev, cur pcrSample) analysis.PCRStep {
	var clock analysis.PCRClock
	clock.Observe(analysis.PCR{PID: prev.pid, Value: prev.pcr, DiscontinuityIndicator: prev.discontinuity})
	return clock.Observe(analysis.PCR{PID: cur.pid, Value: cur.pcr, DiscontinuityIndicator: cur.discontinuity, PCRPIDChanged: cur.pidChanged})
}

// pcrDiscontinuity records a segment boundary: where it happened, its kind and
// how far the PCR jumped across it (positive = forward, negative = backward).
type pcrDiscontinuity struct {
	pos     int64
	kind    analysis.PCRStepKind
	jumpMs  float64
	fromSeg int
	toSeg   int
}

// pcrJitterResult is the outcome of analyzePcrJitter.
type pcrJitterResult struct {
	segments        int
	measured        int // interior samples that produced a jitter value
	maxNs           float64
	maxPos          int64
	minNs           float64
	minPos          int64
	avgAbsNs        float64
	within500Pct    float64
	discontinuities []pcrDiscontinuity
	points          []pcrJitterPoint // jitter of every measured sample, in stream order
}

// pcrJitterPoint is the jitter measured at one PCR sample.
type pcrJitterPoint struct {
	pos int64
	ns  float64
}

// PcrJitter accumulates PCR observations during the stream scan and estimates
// PCR jitter once the whole stream has been read (the "ideal" reference for each
// PCR depends on its neighbours, so the samples are analysed at the end).
type PcrJitter struct {
	samples []pcrSample
	// packetSize is the size of a packet in the file (188, 192 or 204), used to
	// convert byte offsets to transport-stream bytes; 0 means 188.
	packetSize int
}

// Add records one PCR observation of the packet at pos on pid. discontinuity is
// the PCR packet's adaptation_field discontinuity_indicator.
func (j *PcrJitter) Add(pos int64, pid uint16, pcr uint64, discontinuity bool) {
	j.add(pcrSample{pos: pos, pid: pid, pcr: pcr, discontinuity: discontinuity})
}

// add records one PCR observation.
func (j *PcrJitter) add(s pcrSample) { j.samples = append(j.samples, s) }

// timedSample is a sample of a timing segment with its PCR counted from the
// segment start across a wraparound, so values within a segment increase.
type timedSample struct {
	pcrSample
	clock uint64
}

// analyzePcrJitter splits samples into segments at discontinuities and measures
// per-interval jitter within each segment. It assumes len(samples) >= 1.
func analyzePcrJitter(samples []pcrSample) pcrJitterResult {
	// Split into segments, recording each boundary as a discontinuity.
	var segs [][]timedSample
	var discs []pcrDiscontinuity
	cur := []timedSample{{samples[0], samples[0].pcr}}
	for i := 1; i < len(samples); i++ {
		step := pcrStep(samples[i-1], samples[i])
		if step.Boundary() {
			segs = append(segs, cur)
			discs = append(discs, pcrDiscontinuity{
				pos:     samples[i].pos,
				kind:    step.Kind,
				jumpMs:  float64(step.JumpTicks) / float64(pcrTicksPerMs),
				fromSeg: len(segs),
				toSeg:   len(segs) + 1,
			})
			cur = []timedSample{{samples[i], samples[i].pcr}}
			continue
		}
		cur = append(cur, timedSample{samples[i], cur[len(cur)-1].clock + step.Ticks})
	}
	segs = append(segs, cur)

	res := pcrJitterResult{
		segments:        len(segs),
		maxNs:           math.Inf(-1),
		minNs:           math.Inf(1),
		discontinuities: discs,
	}
	var sumAbsNs float64
	var within int
	for _, seg := range segs {
		// Interior samples only: each needs a neighbour on both sides.
		for i := 1; i < len(seg)-1; i++ {
			p0, p1, p2 := seg[i-1], seg[i], seg[i+1]
			predicted := float64(p0.clock) +
				float64(int64(p2.clock)-int64(p0.clock))*float64(p1.pos-p0.pos)/float64(p2.pos-p0.pos)
			jitterNs := pcrTickNs(float64(int64(p1.clock)) - predicted)
			res.points = append(res.points, pcrJitterPoint{pos: p1.pos, ns: jitterNs})
			if jitterNs > res.maxNs {
				res.maxNs, res.maxPos = jitterNs, p1.pos
			}
			if jitterNs < res.minNs {
				res.minNs, res.minPos = jitterNs, p1.pos
			}
			sumAbsNs += math.Abs(jitterNs)
			if math.Abs(jitterNs) <= pcrAccuracyLimitNs {
				within++
			}
			res.measured++
		}
	}
	if res.measured > 0 {
		res.avgAbsNs = sumAbsNs / float64(res.measured)
		res.within500Pct = float64(within) / float64(res.measured) * 100
	}
	return res
}

// deliveryAssessment tells whether the stream supports the constant-delivery
// premise of the byte-position model.
type deliveryAssessment struct {
	assessment string
	reason     string
	windows    int
	// The multiplex rates in bit/s: over all windows, and of the slowest and
	// fastest window. Valid when windows > 0.
	averageBps, minBps, maxBps float64
	// maxDeviationPct is the largest deviation of a window's rate from
	// averageBps, in percent. Valid when windows >= deliveryMinWindows.
	maxDeviationPct float64
}

// deliveryWindow is the transport-stream bytes and PCR time of consecutive PCR
// intervals adding up to at least one second.
type deliveryWindow struct {
	bytes int64
	ticks uint64
}

// deliveryWindows groups consecutive PCR intervals of one timing segment into
// windows of at least deliveryWindowTicks. Each window starts and ends on a PCR
// packet, so its byte count and duration are exact. An interval across a segment
// boundary is not counted, and a window does not span one; the remainder of a
// segment shorter than a window is dropped.
func deliveryWindows(samples []pcrSample, packetSize int) []deliveryWindow {
	if packetSize == 0 {
		packetSize = tsPayloadSize
	}
	var windows []deliveryWindow
	var cur deliveryWindow
	for i := 1; i < len(samples); i++ {
		step := pcrStep(samples[i-1], samples[i])
		if step.Boundary() {
			cur = deliveryWindow{}
			continue
		}
		cur.bytes += (samples[i].pos - samples[i-1].pos) / int64(packetSize) * tsPayloadSize
		cur.ticks += step.Ticks
		if cur.ticks >= deliveryWindowTicks {
			windows = append(windows, cur)
			cur = deliveryWindow{}
		}
	}
	return windows
}

// assessDelivery assesses delivery from the 1-second multiplex rates of the
// samples and, when the estimate was computed, its largest |jitter|
// (maxAbsNs, negative when not computed). Variable delivery is established by
// a window rate beyond the tolerance or by an implausibly large jitter;
// constant delivery needs at least deliveryMinWindows windows within the
// tolerance and a plausible jitter.
func assessDelivery(samples []pcrSample, packetSize int, maxAbsNs float64) deliveryAssessment {
	windows := deliveryWindows(samples, packetSize)
	d := deliveryAssessment{windows: len(windows)}
	if len(windows) > 0 {
		var bytes int64
		var ticks uint64
		d.minBps = math.Inf(1)
		for _, w := range windows {
			bytes += w.bytes
			ticks += w.ticks
			rate := float64(w.bytes) * 8 * pcrClockHz / float64(w.ticks)
			d.minBps = math.Min(d.minBps, rate)
			d.maxBps = math.Max(d.maxBps, rate)
		}
		d.averageBps = float64(bytes) * 8 * pcrClockHz / float64(ticks)
		d.maxDeviationPct = math.Max(d.maxBps-d.averageBps, d.averageBps-d.minBps) / d.averageBps * 100
	}
	windowsReason := fmt.Sprintf("the multiplex rate of %d one-second windows deviates up to %.2f%% from the average %s b/s (tolerance %.0f%%)",
		d.windows, d.maxDeviationPct, commaInt(int64(math.Round(d.averageBps))), deliveryTolerancePercent)
	switch {
	case d.windows >= deliveryMinWindows && d.maxDeviationPct > deliveryTolerancePercent:
		d.assessment, d.reason = deliveryVariable, windowsReason
	case maxAbsNs > pcrPlausibleJitterNs:
		d.assessment = deliveryVariable
		d.reason = fmt.Sprintf("the estimated jitter reaches %.3fms, beyond the %.0fms a PCR error plausibly reaches; the delivery rate varies between PCRs",
			maxAbsNs/1e6, pcrPlausibleJitterNs/1e6)
	case d.windows < deliveryMinWindows:
		d.assessment = deliveryUnknown
		d.reason = fmt.Sprintf("need at least %d one-second windows of PCR time to compare multiplex rates, got %d", deliveryMinWindows, d.windows)
	default:
		d.assessment, d.reason = deliveryConstant, windowsReason
	}
	return d
}

// pcrJitterEvaluation is the complete PCR jitter result shared by the text
// and JSON outputs.
type pcrJitterEvaluation struct {
	// status is ok, warning or ng only when the estimate was computed and
	// delivery is constant; not_assessable when it was computed but delivery
	// is not constant; skipped when no estimate could be made (reason says
	// why). skipped takes precedence over not_assessable.
	status string
	reason string
	// estimated reports whether res holds a jitter estimate. res is analyzed
	// whenever there are at least 3 samples.
	estimated bool
	analyzed  bool
	res       pcrJitterResult
	maxAbsNs  float64
	maxAbsPos int64
	delivery  deliveryAssessment
}

// evaluate estimates the jitter, assesses delivery and derives the status.
func (j *PcrJitter) evaluate() pcrJitterEvaluation {
	e := pcrJitterEvaluation{status: jitterStatusSkipped, maxAbsNs: -1}
	if len(j.samples) < 3 {
		e.reason = "need at least 3 PCR samples"
	} else {
		e.analyzed = true
		e.res = analyzePcrJitter(j.samples)
		switch {
		case e.res.measured == 0:
			e.reason = "no segment long enough for a stable estimate"
		case float64(len(e.res.discontinuities))/float64(len(j.samples)) > pcrJitterGiveUpRatio:
			e.reason = "too many discontinuities"
		default:
			e.estimated = true
			e.maxAbsNs, e.maxAbsPos = math.Abs(e.res.maxNs), e.res.maxPos
			if math.Abs(e.res.minNs) > e.maxAbsNs {
				e.maxAbsNs, e.maxAbsPos = math.Abs(e.res.minNs), e.res.minPos
			}
		}
	}
	e.delivery = assessDelivery(j.samples, j.packetSize, e.maxAbsNs)
	if !e.estimated {
		return e
	}
	switch {
	case e.delivery.assessment != deliveryConstant:
		e.status = jitterStatusNotAssessable
		e.reason = fmt.Sprintf("delivery rate is %s, so the byte-position estimate does not measure PCR accuracy", e.delivery.assessment)
	case e.maxAbsNs > pcrDvbLimitNs:
		e.status = jitterStatusNg
	case e.maxAbsNs > pcrAccuracyLimitNs:
		e.status = jitterStatusWarning
	default:
		e.status = jitterStatusOk
	}
	return e
}

// pcrPids returns the PIDs the samples were taken from, in order of first
// appearance (the PCR_PID can change with a PMT version).
func (j *PcrJitter) pcrPids() []uint16 {
	var pids []uint16
	for _, s := range j.samples {
		if !slices.Contains(pids, s.pid) {
			pids = append(pids, s.pid)
		}
	}
	return pids
}

// Dump analyses the collected PCR samples and prints the jitter summary.
func (j *PcrJitter) Dump() {
	fmt.Println("-----------------------------")
	fmt.Println("PCR Jitter Summary (per-interval, byte-position model):")
	fmt.Println("  (file-based estimate; judged only when the stream shows constant delivery, not a TR 101 290 measurement)")

	e := j.evaluate()
	switch {
	case !e.analyzed:
		fmt.Printf("  analysis skipped - need at least 3 PCR samples, got %d\n", len(j.samples))
	case !e.estimated && e.res.measured == 0:
		fmt.Printf("  analysis skipped - no segment long enough for a stable estimate (%d segment(s) from %d samples)\n",
			e.res.segments, len(j.samples))
	case !e.estimated:
		fmt.Printf("  analysis skipped - too many discontinuities (%d of %d samples)\n",
			len(e.res.discontinuities), len(j.samples))
	default:
		fmt.Printf("  Samples          : %d PCR in %d segment(s)\n", len(j.samples), e.res.segments)
		fmt.Printf("  Measured         : %d interior samples\n", e.res.measured)
		fmt.Printf("  Max jitter       : %+.6fms at 0x%08x\n", e.res.maxNs/1e6, e.res.maxPos)
		fmt.Printf("  Min jitter       : %+.6fms at 0x%08x\n", e.res.minNs/1e6, e.res.minPos)
		fmt.Printf("  Avg |jitter|     : %.6fms\n", e.res.avgAbsNs/1e6)
		fmt.Printf("  Within +/-500ns  : %.1f%% (ISO/IEC 13818-1 accuracy limit)\n", e.res.within500Pct)
	}
	fmt.Printf("  Delivery         : %s - %s\n", strings.ToUpper(e.delivery.assessment), e.delivery.reason)
	if e.estimated {
		if e.status == jitterStatusNotAssessable {
			fmt.Printf("  Status           : NOT ASSESSABLE (%s)\n", e.reason)
		} else {
			fmt.Printf("  Status           : %s\n", pcrJitterVerdict(e.maxAbsNs))
		}
	}
	if e.analyzed {
		printPcrDiscontinuities(e.res.discontinuities)
	}
}

// pcrJitterVerdict classifies the worst-case |jitter| (in ns) against the
// ISO/IEC 13818-1 accuracy limit (±500 ns) and the DVB receiver tolerance
// (±25 µs):
//
//	OK      max |jitter| within ±500 ns (ISO/IEC 13818-1)
//	WARNING beyond ±500 ns but within ±25 µs (DVB)
//	NG      beyond ±25 µs
func pcrJitterVerdict(maxAbsNs float64) string {
	switch {
	case maxAbsNs <= pcrAccuracyLimitNs:
		return "OK (max |jitter| within the +/-500ns ISO/IEC 13818-1 limit)"
	case maxAbsNs <= pcrDvbLimitNs:
		return "WARNING (max |jitter| exceeds +/-500ns (ISO) but within +/-25 microsec (DVB))"
	default:
		return "NG (max |jitter| exceeds the +/-25 microsec DVB limit)"
	}
}

// printPcrDiscontinuities prints the discontinuity count and, if any, where each
// happened and how far the PCR jumped.
func printPcrDiscontinuities(discs []pcrDiscontinuity) {
	fmt.Printf("  Discontinuities  : %d\n", len(discs))
	for _, d := range discs {
		fmt.Printf("    - 0x%08x : %s, PCR jumped %+.3fms (segment %d -> %d)\n", d.pos, d.kind, d.jumpMs, d.fromSeg, d.toSeg)
	}
}
