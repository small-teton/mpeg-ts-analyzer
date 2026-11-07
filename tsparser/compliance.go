package tsparser

import (
	"fmt"
	"strings"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

const maxPcrPtsGapLimitMs = 1000.0

// pcrWrapTicks is the PCR modulus (see analysis.PCRWrapTicks).
const pcrWrapTicks = analysis.PCRWrapTicks

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

// complianceReport holds the timing compliance checks of one program. The PCR
// discontinuity check counts as a timing compliance check: ETSI TR 101 290
// groups an undeclared PCR discontinuity (indicator 2.3b) with the PCR
// repetition interval (2.3a) under PCR_error (2.3).
type complianceReport struct {
	maxPcrInterval     complianceCheck
	pcrDiscontinuities analysis.PCRDiscontinuityResult
	maxPcrPtsGap       complianceCheck
}

const pcrDiscontinuitiesName = "PCR discontinuities"

func newComplianceReport(interval analysis.PCRIntervalResult, discontinuities analysis.PCRDiscontinuityResult, maxPcrPtsGapMs float64, pcrPtsSamples int) complianceReport {
	return complianceReport{
		maxPcrInterval: complianceCheck{
			name:         "Max PCR interval",
			valueMs:      interval.MaxMs,
			limitMs:      analysis.MaxPCRIntervalMs,
			observations: interval.Intervals,
			skippedWhy:   "need at least two comparable PCR observations",
		},
		pcrDiscontinuities: discontinuities,
		maxPcrPtsGap: complianceCheck{
			name:         "PCR-PTS/DTS max gap",
			valueMs:      maxPcrPtsGapMs,
			limitMs:      maxPcrPtsGapLimitMs,
			observations: pcrPtsSamples,
			skippedWhy:   "need a parsed PTS/DTS bracketed by PCR observations",
		},
	}
}

// pcrDiscontinuitiesSkippedWhy explains a skipped PCR discontinuity check.
const pcrDiscontinuitiesSkippedWhy = "need at least two PCR observations"

// pcrDiscontinuityStatus is the JSON status of the PCR discontinuity check.
func pcrDiscontinuityStatus(r analysis.PCRDiscontinuityResult) string {
	switch r.Status {
	case analysis.StatusSkipped:
		return "skipped"
	case analysis.StatusErrors:
		return "ng"
	default:
		return "ok"
	}
}

func dumpPcrDiscontinuities(r analysis.PCRDiscontinuityResult) {
	status := strings.ToUpper(pcrDiscontinuityStatus(r))
	if r.Status == analysis.StatusSkipped {
		fmt.Printf("%s: SKIPPED (%s)\n", pcrDiscontinuitiesName, pcrDiscontinuitiesSkippedWhy)
		return
	}
	fmt.Printf("%s: %d undeclared in %d PCRs [%s]\n", pcrDiscontinuitiesName, r.Undeclared, r.PCRs, status)
	for _, step := range r.Steps {
		fmt.Printf("  - %-14s: %d, first at 0x%08x (PCR jumped %+.3fms)\n", step.Kind, step.Count, step.FirstOffset, step.FirstJumpMs)
	}
}

func (r complianceReport) dump() {
	fmt.Println("-----------------------------")
	fmt.Println("Compliance Check Results:")
	r.maxPcrInterval.dump()
	dumpPcrDiscontinuities(r.pcrDiscontinuities)
	r.maxPcrPtsGap.dump()
}

func (r complianceReport) failures() []string {
	var failures []string
	if r.maxPcrInterval.status() == complianceNG {
		failures = append(failures, r.maxPcrInterval.name)
	}
	if r.pcrDiscontinuities.Status == analysis.StatusErrors {
		failures = append(failures, pcrDiscontinuitiesName)
	}
	if r.maxPcrPtsGap.status() == complianceNG {
		failures = append(failures, r.maxPcrPtsGap.name)
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
