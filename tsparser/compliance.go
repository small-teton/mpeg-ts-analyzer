package tsparser

import (
	"fmt"
	"strings"
)

const (
	maxPcrIntervalLimitMs = 100.0
	maxPcrPtsGapLimitMs   = 1000.0
)

// pcrWrapTicks is the PCR modulus. program_clock_reference_base is a 33-bit
// 90 kHz counter and the extension counts 0..299 at 27 MHz, so the combined
// 27 MHz value wraps from 2^33*300-1 back to 0.
const pcrWrapTicks = (uint64(1) << 33) * 300

// pcrIntervalTracker measures the maximum interval between consecutive PCRs on
// the selected program's PCR PID. The segment-boundary policy is:
//
//   - discontinuity_indicator on the current PCR packet starts a new segment;
//     no interval is measured across a declared discontinuity, whichever way
//     the clock jumped.
//   - A smaller PCR within pcrDiscontinuityJumpMs of the previous one across
//     the 2^33*300 boundary is a normal wraparound and is measured.
//   - Any other smaller PCR is an undeclared backward reset; it has no
//     meaningful interval, so it starts a new segment.
//   - An undeclared forward jump is measured as is. Without a time reference
//     it cannot be told apart from a genuinely late PCR, and either way the
//     stream violates the 100 ms requirement.
type pcrIntervalTracker struct {
	prev      uint64
	hasPrev   bool
	maxTicks  uint64
	intervals int
}

// Add records one PCR observation. discontinuity is the PCR packet's
// adaptation_field discontinuity_indicator.
func (t *pcrIntervalTracker) Add(pcr uint64, discontinuity bool) {
	prev, hasPrev := t.prev, t.hasPrev
	t.prev, t.hasPrev = pcr, true
	if !hasPrev || discontinuity {
		return
	}
	var delta uint64
	if pcr >= prev {
		delta = pcr - prev
	} else {
		delta = pcr + pcrWrapTicks - prev
		if delta > pcrDiscontinuityJumpMs*pcrTicksPerMs {
			return
		}
	}
	t.maxTicks = max(t.maxTicks, delta)
	t.intervals++
}

// maxMs returns the largest measured interval in milliseconds.
func (t *pcrIntervalTracker) maxMs() float64 { return pcrToMs(float64(t.maxTicks)) }

type complianceStatus string

const (
	complianceOK      complianceStatus = "OK"
	complianceNG      complianceStatus = "NG"
	complianceSkipped complianceStatus = "SKIPPED"
)

type complianceCheck struct {
	name         string
	valueMs      float64
	limitMs      float64
	observations int
	skippedWhy   string
}

func (c complianceCheck) status() complianceStatus {
	if c.observations == 0 {
		return complianceSkipped
	}
	if c.valueMs <= c.limitMs {
		return complianceOK
	}
	return complianceNG
}

func (c complianceCheck) dump() {
	switch c.status() {
	case complianceSkipped:
		fmt.Printf("%s: SKIPPED (%s)\n", c.name, c.skippedWhy)
	default:
		fmt.Printf("%s: %fms [%s, limit: <= %fms]\n", c.name, c.valueMs, c.status(), c.limitMs)
	}
}

type complianceReport struct {
	maxPcrInterval complianceCheck
	maxPcrPtsGap   complianceCheck
}

func newComplianceReport(maxPcrIntervalMs float64, pcrIntervals int, maxPcrPtsGapMs float64, pcrPtsSamples int) complianceReport {
	return complianceReport{
		maxPcrInterval: complianceCheck{
			name:         "Max PCR interval",
			valueMs:      maxPcrIntervalMs,
			limitMs:      maxPcrIntervalLimitMs,
			observations: pcrIntervals,
			skippedWhy:   "need at least two comparable PCR observations",
		},
		maxPcrPtsGap: complianceCheck{
			name:         "PCR-PTS/DTS max gap",
			valueMs:      maxPcrPtsGapMs,
			limitMs:      maxPcrPtsGapLimitMs,
			observations: pcrPtsSamples,
			skippedWhy:   "need a parsed PTS/DTS bracketed by PCR observations",
		},
	}
}

func (r complianceReport) dump() {
	fmt.Println("-----------------------------")
	fmt.Println("Compliance Check Results:")
	r.maxPcrInterval.dump()
	r.maxPcrPtsGap.dump()
}

func (r complianceReport) failures() []string {
	var failures []string
	for _, check := range []complianceCheck{r.maxPcrInterval, r.maxPcrPtsGap} {
		if check.status() == complianceNG {
			failures = append(failures, check.name)
		}
	}
	return failures
}

// ComplianceError indicates that parsing completed but one or more enabled
// compliance checks failed. The CLI maps this error to exit code 2 so it stays
// distinguishable from usage, input, and parsing errors (exit code 1).
type ComplianceError struct {
	Checks []string
}

func (e *ComplianceError) Error() string {
	return "compliance check failed: " + strings.Join(e.Checks, ", ")
}
