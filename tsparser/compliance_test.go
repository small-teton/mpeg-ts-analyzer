package tsparser

import (
	"strings"
	"testing"
)

func TestComplianceBoundaryIsOK(t *testing.T) {
	report := newComplianceReport(100, 1, 1000, 1)
	if got := report.maxPcrInterval.status(); got != complianceOK {
		t.Errorf("PCR boundary status = %s, want OK", got)
	}
	if got := report.maxPcrPtsGap.status(); got != complianceOK {
		t.Errorf("PCR-PTS boundary status = %s, want OK", got)
	}
	if failures := report.failures(); len(failures) != 0 {
		t.Errorf("boundary failures = %v, want none", failures)
	}

	out := captureStdout(t, report.dump)
	for _, want := range []string{
		"Compliance Check Results:",
		"Max PCR interval: 100.000000ms [OK, limit: <= 100.000000ms]",
		"PCR-PTS max gap: 1000.000000ms [OK, limit: <= 1000.000000ms]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("boundary output missing %q:\n%s", want, out)
		}
	}
}

func TestComplianceNG(t *testing.T) {
	report := newComplianceReport(100.001, 1, 1000.001, 1)
	failures := report.failures()
	if len(failures) != 2 || failures[0] != "Max PCR interval" || failures[1] != "PCR-PTS max gap" {
		t.Fatalf("failures = %v", failures)
	}
	err := (&ComplianceError{Checks: failures}).Error()
	if err != "compliance check failed: Max PCR interval, PCR-PTS max gap" {
		t.Errorf("ComplianceError = %q", err)
	}
	out := captureStdout(t, report.dump)
	if strings.Count(out, "[NG,") != 2 {
		t.Errorf("NG output =\n%s", out)
	}
}

func TestComplianceSkipped(t *testing.T) {
	report := newComplianceReport(0, 0, 0, 0)
	if report.maxPcrInterval.status() != complianceSkipped || report.maxPcrPtsGap.status() != complianceSkipped {
		t.Fatalf("statuses = %s, %s", report.maxPcrInterval.status(), report.maxPcrPtsGap.status())
	}
	if failures := report.failures(); len(failures) != 0 {
		t.Errorf("SKIPPED checks must not fail: %v", failures)
	}
	out := captureStdout(t, report.dump)
	for _, want := range []string{
		"Max PCR interval: SKIPPED (need at least two comparable PCR observations)",
		"PCR-PTS max gap: SKIPPED (need a parsed PTS/DTS bracketed by PCR observations)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("SKIPPED output missing %q:\n%s", want, out)
		}
	}
}
