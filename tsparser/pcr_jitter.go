package tsparser

import (
	"fmt"
	"math"
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
// time. That premise holds for constant-bitrate (CBR) delivery. The result is
// therefore a file-based estimate, not a hardware TR 101 290 measurement against
// a real reference clock.
//
// To stay robust to variable bitrate (VBR), jitter is measured *per interval*
// (locally), not against one global straight line: each interior PCR is compared
// to the value obtained by interpolating its two neighbours by byte position.
// Over three consecutive PCRs the byte rate is ~constant even when it drifts
// across the whole stream, so a smooth rate change affects the neighbours too
// and cancels out, leaving only genuine PCR error. (We do not assume PCRs are
// evenly spaced; the actual byte positions are used.)
//
// A PCR discontinuity (an intentional clock reset, signalled by
// discontinuity_indicator, or an implausible jump) splits the samples into
// segments. Jitter is measured within a segment only, and the discontinuities
// are reported separately — including how many and how large — so a broken
// stream is visible rather than silently inflating the jitter numbers. If the
// stream is too discontinuous to estimate from, the analysis is skipped.
const (
	// pcrClockHz is the PCR system clock frequency (27 MHz); one PCR tick is
	// 1/27_000_000 s.
	pcrClockHz = 27_000_000
	// pcrTicksPerMs is 27 MHz expressed per millisecond.
	pcrTicksPerMs = pcrClockHz / 1000
	// pcrAccuracyLimitNs is the PCR accuracy limit from ISO/IEC 13818-1: ±500 ns.
	// Max |jitter| within this is a pass (OK).
	pcrAccuracyLimitNs = 500.0
	// pcrDvbLimitNs is the ±25 µs jitter tolerance DVB expects compliant
	// receivers to handle. Max |jitter| between the ISO limit and this is a
	// warning; beyond it is a failure (NG).
	pcrDvbLimitNs = 25_000.0
	// pcrDiscontinuityJumpMs: a PCR-to-PCR gap larger than this is treated as a
	// discontinuity rather than jitter. ISO/IEC 13818-1 caps the legal PCR
	// interval at 100 ms, so a jump beyond 1000 ms cannot be a normal interval.
	pcrDiscontinuityJumpMs = 1000.0
	// pcrJitterGiveUpRatio: if more than this fraction of PCR samples start a new
	// segment, the stream is too broken to estimate jitter from.
	pcrJitterGiveUpRatio = 0.30
)

// pcrTickNs converts a (possibly fractional) count of 27 MHz PCR ticks to
// nanoseconds: 1 tick = 1e9 / 27e6 = 1000/27 ns.
func pcrTickNs(ticks float64) float64 { return ticks * 1000.0 / 27.0 }

// pcrSample is one PCR observation: the byte offset of the PCR-carrying packet
// and the PCR value in 27 MHz ticks. discontinuity is the packet's
// adaptation_field discontinuity_indicator.
type pcrSample struct {
	pos           int64
	pcr           uint64
	discontinuity bool
}

// pcrDiscontinuity records a segment boundary: where it happened and how far the
// PCR jumped across it (positive = forward, negative = backward).
type pcrDiscontinuity struct {
	pos     int64
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
}

// PcrJitter accumulates PCR observations during the stream scan and estimates
// PCR jitter once the whole stream has been read (the "ideal" reference for each
// PCR depends on its neighbours, so the samples are analysed at the end).
type PcrJitter struct {
	samples []pcrSample
}

// Add records one PCR observation. discontinuity is the PCR packet's
// adaptation_field discontinuity_indicator.
func (j *PcrJitter) Add(pos int64, pcr uint64, discontinuity bool) {
	j.samples = append(j.samples, pcrSample{pos: pos, pcr: pcr, discontinuity: discontinuity})
}

// isSegmentBoundary reports whether cur starts a new timing segment relative to
// prev: an explicit discontinuity_indicator, a non-increasing PCR (backward or
// equal, e.g. a 33-bit wrap or reset), or an implausibly large forward jump.
func isSegmentBoundary(prev, cur pcrSample) bool {
	if cur.discontinuity {
		return true
	}
	deltaMs := float64(int64(cur.pcr)-int64(prev.pcr)) / float64(pcrTicksPerMs)
	return deltaMs <= 0 || deltaMs > pcrDiscontinuityJumpMs
}

// analyzePcrJitter splits samples into segments at discontinuities and measures
// per-interval jitter within each segment. It assumes len(samples) >= 1.
func analyzePcrJitter(samples []pcrSample) pcrJitterResult {
	// Split into segments, recording each boundary as a discontinuity.
	var segs [][]pcrSample
	var discs []pcrDiscontinuity
	cur := []pcrSample{samples[0]}
	for i := 1; i < len(samples); i++ {
		if isSegmentBoundary(samples[i-1], samples[i]) {
			segs = append(segs, cur)
			discs = append(discs, pcrDiscontinuity{
				pos:     samples[i].pos,
				jumpMs:  float64(int64(samples[i].pcr)-int64(samples[i-1].pcr)) / float64(pcrTicksPerMs),
				fromSeg: len(segs),
				toSeg:   len(segs) + 1,
			})
			cur = nil
		}
		cur = append(cur, samples[i])
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
			predicted := float64(p0.pcr) +
				float64(int64(p2.pcr)-int64(p0.pcr))*float64(p1.pos-p0.pos)/float64(p2.pos-p0.pos)
			jitterNs := pcrTickNs(float64(int64(p1.pcr)) - predicted)
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

// Dump analyses the collected PCR samples and prints the jitter summary.
func (j *PcrJitter) Dump() {
	fmt.Println("-----------------------------")
	fmt.Println("PCR Jitter Summary (per-interval, byte-position model):")
	fmt.Println("  (file-based estimate; assumes ~constant delivery rate, not a TR 101 290 measurement)")

	if len(j.samples) < 3 {
		fmt.Printf("  analysis skipped - need at least 3 PCR samples, got %d\n", len(j.samples))
		return
	}

	res := analyzePcrJitter(j.samples)

	if res.measured == 0 {
		fmt.Printf("  analysis skipped - no segment long enough for a stable estimate (%d segment(s) from %d samples)\n",
			res.segments, len(j.samples))
		printPcrDiscontinuities(res.discontinuities)
		return
	}
	if float64(len(res.discontinuities))/float64(len(j.samples)) > pcrJitterGiveUpRatio {
		fmt.Printf("  analysis skipped - too many discontinuities (%d of %d samples)\n",
			len(res.discontinuities), len(j.samples))
		printPcrDiscontinuities(res.discontinuities)
		return
	}

	maxAbsNs := math.Max(math.Abs(res.maxNs), math.Abs(res.minNs))
	fmt.Printf("  Samples          : %d PCR in %d segment(s)\n", len(j.samples), res.segments)
	fmt.Printf("  Measured         : %d interior samples\n", res.measured)
	fmt.Printf("  Max jitter       : %+.6fms at 0x%08x\n", res.maxNs/1e6, res.maxPos)
	fmt.Printf("  Min jitter       : %+.6fms at 0x%08x\n", res.minNs/1e6, res.minPos)
	fmt.Printf("  Avg |jitter|     : %.6fms\n", res.avgAbsNs/1e6)
	fmt.Printf("  Within +/-500ns  : %.1f%% (ISO/IEC 13818-1 accuracy limit)\n", res.within500Pct)
	fmt.Printf("  Status           : %s\n", pcrJitterVerdict(maxAbsNs))
	printPcrDiscontinuities(res.discontinuities)
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
		fmt.Printf("    - 0x%08x : PCR jumped %+.3fms (segment %d -> %d)\n", d.pos, d.jumpMs, d.fromSeg, d.toSeg)
	}
}
