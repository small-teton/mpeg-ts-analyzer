package tsparser

import (
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

// pcrResults returns the PCR results of PCRs on PID 0x100 with the given
// values in milliseconds; a negative value is declared with
// discontinuity_indicator at its absolute value.
func pcrResults(values ...int64) (analysis.PCRIntervalResult, analysis.PCRDiscontinuityResult) {
	var c analysis.PCRCheck
	for i, v := range values {
		declared := v < 0
		if declared {
			v = -v
		}
		c.Observe(analysis.PCR{PID: 0x100, Offset: int64(i) * 188, Value: uint64(v) * pcrTicksPerMs, DiscontinuityIndicator: declared})
	}
	return c.Interval(), c.Discontinuities()
}

func complianceOf(maxGapMs float64, gapSamples int, pcrMs ...int64) complianceReport {
	interval, discontinuities := pcrResults(pcrMs...)
	return newComplianceReport(interval, discontinuities, maxGapMs, gapSamples)
}

func TestComplianceBoundaryIsOK(t *testing.T) {
	report := complianceOf(1000, 1, 1000, 1100)
	if got := report.maxPcrInterval.status(); got != complianceOK {
		t.Errorf("PCR boundary status = %s, want OK", got)
	}
	if got := report.maxPcrPtsGap.status(); got != complianceOK {
		t.Errorf("PCR-PTS/DTS boundary status = %s, want OK", got)
	}
	if failures := report.failures(); len(failures) != 0 {
		t.Errorf("boundary failures = %v, want none", failures)
	}

	out := captureStdout(t, report.dump)
	for _, want := range []string{
		"Compliance Check Results:",
		"Max PCR interval: 100.000000ms [OK, limit: <= 100.000000ms]",
		"PCR discontinuities: 0 undeclared in 2 PCRs [OK, limit: 0 without discontinuity_indicator]",
		"PCR-PTS/DTS max gap: 1000.000000ms [OK, limit: <= 1000.000000ms]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("boundary output missing %q:\n%s", want, out)
		}
	}
}

func TestComplianceNG(t *testing.T) {
	// A 101 ms interval, then an undeclared backward step.
	report := complianceOf(1000.001, 1, 1000, 1101, 500)
	failures := report.failures()
	if len(failures) != 3 || failures[0] != "Max PCR interval" || failures[1] != "PCR discontinuities" || failures[2] != "PCR-PTS/DTS max gap" {
		t.Fatalf("failures = %v", failures)
	}
	err := (&ComplianceError{Checks: failures}).Error()
	if err != "compliance check failed: Max PCR interval, PCR discontinuities, PCR-PTS/DTS max gap" {
		t.Errorf("ComplianceError = %q", err)
	}
	out := captureStdout(t, report.dump)
	for _, want := range []string{
		"Max PCR interval: 101.000000ms [NG, limit: <= 100.000000ms]",
		"PCR discontinuities: 1 undeclared in 3 PCRs [NG, limit: 0 without discontinuity_indicator]",
		"  - backward      : 1, first at 0x00000178 (PCR jumped -601.000ms)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("NG output missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "[NG,") != 3 {
		t.Errorf("NG output =\n%s", out)
	}
}

func TestComplianceSkipped(t *testing.T) {
	report := complianceOf(0, 0)
	if report.maxPcrInterval.status() != complianceSkipped || report.maxPcrPtsGap.status() != complianceSkipped ||
		report.pcrDiscontinuities.Status != analysis.StatusSkipped {
		t.Fatalf("statuses = %s, %s", report.maxPcrInterval.status(), report.maxPcrPtsGap.status())
	}
	if failures := report.failures(); len(failures) != 0 {
		t.Errorf("SKIPPED checks must not fail: %v", failures)
	}
	out := captureStdout(t, report.dump)
	for _, want := range []string{
		"Max PCR interval: SKIPPED (need at least two comparable PCR observations)",
		"PCR discontinuities: SKIPPED (need at least two PCR observations)",
		"PCR-PTS/DTS max gap: SKIPPED (need a parsed PTS/DTS bracketed by PCR observations)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("SKIPPED output missing %q:\n%s", want, out)
		}
	}
}

// TestComplianceDeclaredDiscontinuitiesPass lists declared discontinuities
// without failing: a declared time-base change is legal.
func TestComplianceDeclaredDiscontinuitiesPass(t *testing.T) {
	report := complianceOf(0, 0, 1000, 1040, -9000, 9040)
	if failures := report.failures(); len(failures) != 0 {
		t.Errorf("failures = %v", failures)
	}
	out := captureStdout(t, report.dump)
	for _, want := range []string{
		"PCR discontinuities: 0 undeclared in 4 PCRs [OK,",
		"  - declared      : 1, first at 0x00000178 (PCR jumped +7960.000ms)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
