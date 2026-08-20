package tsparser

import (
	"fmt"
	"strings"
)

const (
	maxPcrIntervalLimitMs = 100.0
	maxPcrPtsGapLimitMs   = 1000.0
)

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
			name:         "PCR-PTS max gap",
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
