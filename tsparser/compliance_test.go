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
		t.Errorf("PCR-PTS/DTS boundary status = %s, want OK", got)
	}
	if failures := report.failures(); len(failures) != 0 {
		t.Errorf("boundary failures = %v, want none", failures)
	}

	out := captureStdout(t, report.dump)
	for _, want := range []string{
		"Compliance Check Results:",
		"Max PCR interval: 100.000000ms [OK, limit: <= 100.000000ms]",
		"PCR-PTS/DTS max gap: 1000.000000ms [OK, limit: <= 1000.000000ms]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("boundary output missing %q:\n%s", want, out)
		}
	}
}

func TestComplianceNG(t *testing.T) {
	report := newComplianceReport(100.001, 1, 1000.001, 1)
	failures := report.failures()
	if len(failures) != 2 || failures[0] != "Max PCR interval" || failures[1] != "PCR-PTS/DTS max gap" {
		t.Fatalf("failures = %v", failures)
	}
	err := (&ComplianceError{Checks: failures}).Error()
	if err != "compliance check failed: Max PCR interval, PCR-PTS/DTS max gap" {
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
		"PCR-PTS/DTS max gap: SKIPPED (need a parsed PTS/DTS bracketed by PCR observations)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("SKIPPED output missing %q:\n%s", want, out)
		}
	}
}

func TestPcrIntervalTracker(t *testing.T) {
	const ms = uint64(pcrTicksPerMs)
	type obs struct {
		pcr           uint64
		discontinuity bool
	}
	tests := []struct {
		name      string
		obs       []obs
		wantMaxMs float64
		wantCount int
	}{
		{"single PCR", []obs{{27_000_000, false}}, 0, 0},
		{"ordinary intervals", []obs{{0, false}, {40 * ms, false}, {80 * ms, false}}, 40, 2},
		{"exact 100 ms boundary", []obs{{1000 * ms, false}, {1100 * ms, false}}, 100, 1},
		{"forward jump with discontinuity_indicator", []obs{{0, false}, {40 * ms, false}, {10_040 * ms, true}, {10_080 * ms, false}}, 40, 2},
		{"backward reset with discontinuity_indicator", []obs{{5000 * ms, false}, {5040 * ms, false}, {100 * ms, true}, {150 * ms, false}}, 50, 2},
		{"wraparound", []obs{{pcrWrapTicks - 20*ms, false}, {20 * ms, false}, {60 * ms, false}}, 40, 2},
		{"wraparound at the 1000 ms limit", []obs{{pcrWrapTicks - 600*ms, false}, {400 * ms, false}}, 1000, 1},
		{"undeclared backward reset", []obs{{5000 * ms, false}, {5040 * ms, false}, {100 * ms, false}, {130 * ms, false}}, 40, 2},
		{"backward beyond the wrap limit", []obs{{pcrWrapTicks - 600*ms, false}, {401 * ms, false}, {441 * ms, false}}, 40, 1},
		{"undeclared forward jump", []obs{{0, false}, {2000 * ms, false}}, 2000, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tracker pcrIntervalTracker
			for _, o := range tt.obs {
				tracker.Add(o.pcr, o.discontinuity)
			}
			if got := tracker.maxMs(); got != tt.wantMaxMs {
				t.Errorf("maxMs = %f, want %f", got, tt.wantMaxMs)
			}
			if tracker.intervals != tt.wantCount {
				t.Errorf("intervals = %d, want %d", tracker.intervals, tt.wantCount)
			}
		})
	}
}
