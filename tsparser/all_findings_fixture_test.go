package tsparser

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// The all-findings fixtures are a short constant-rate stream with one
// deliberate defect per Overview compliance card
// (tools/generate_all_findings). Packet n is slot n of the generator.
var allFindingsFixtures = []struct {
	path       string
	packetSize int64
	m2ts       bool
}{
	{"../sample_data/sample_188byte_all_findings.ts", 188, false},
	{"../sample_data/sample_192byte_all_findings.ts", 192, true},
}

// cardStatuses returns the statuses of the five Overview cards of the only
// program: max PCR interval, PCR-PTS/DTS gap, PTS/DTS anomalies, continuity
// counter and PCR jitter.
func cardStatuses(t *testing.T, report RangeAnalysisReport) [5]string {
	t.Helper()
	if report.Status != "complete" || len(report.Programs) != 1 || report.ContinuityCounter == nil {
		t.Fatalf("unexpected analysis: %s", toJSON(t, report))
	}
	p := report.Programs[0]
	return [5]string{
		p.Compliance.MaxPCRInterval.Status,
		p.Compliance.MaxPCRPTSDTSGap.Status,
		p.TimestampAnomalies.Status,
		report.ContinuityCounter.Status,
		p.PCRJitter.Status,
	}
}

var allFindingsStatuses = [5]string{"ng", "ng", "anomalies", "errors", "ng"}

func TestAllFindingsFixtures(t *testing.T) {
	for _, fixture := range allFindingsFixtures {
		at := func(slot int64) int64 { return slot * fixture.packetSize }
		report, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatalf("%s: %v", fixture.path, err)
		}
		if got := cardStatuses(t, report); got != allFindingsStatuses {
			t.Errorf("%s: card statuses %v, want %v", fixture.path, got, allFindingsStatuses)
		}
		if report.Input.PacketSizeBytes != int(fixture.packetSize) || *report.AnalysisStartOffsetBytes != at(4) || len(report.Warnings) != 0 || len(report.PSITransitions) != 0 {
			t.Errorf("%s: envelope %s", fixture.path, toJSON(t, report.Input))
		}
		p := report.Programs[0]

		// Max PCR interval: slots 26, 30 and 34 carry no PCR.
		interval := p.Compliance.MaxPCRInterval
		if *interval.ValueMs != 160 || interval.LimitMs != 100 || interval.Observations != 15 {
			t.Errorf("%s: max PCR interval %s", fixture.path, toJSON(t, interval))
		}
		// PCR-PTS/DTS gap: every audio PTS is 1200 ms ahead.
		gap := p.Compliance.MaxPCRPTSDTSGap
		if *gap.ValueMs != 1200 || gap.LimitMs != 1000 || gap.Observations != 23 {
			t.Errorf("%s: PCR-PTS/DTS gap %s", fixture.path, toJSON(t, gap))
		}
		// PTS/DTS anomalies: the video PTS in slot 46 goes back 40 ms.
		anomalies := p.TimestampAnomalies.Items
		events := p.Series.TimestampEvents
		if len(anomalies) != 1 || anomalies[0].PID != "0x0100" || anomalies[0].Kind != "backward" || anomalies[0].Count != 1 ||
			len(events) != 1 || events[0].OffsetBytes != at(46) {
			t.Errorf("%s: timestamp anomalies %s %s", fixture.path, toJSON(t, anomalies), toJSON(t, events))
		}
		// Continuity counter: the audio packet in slot 44 skips counter 5,
		// for the whole multiplex.
		cc := report.ContinuityCounter
		wantEvents := []ReportContinuityEvent{{OffsetBytes: at(44), PID: "0x0101", ExpectedCC: 5, ActualCC: 6}}
		if cc.TotalErrorCount != 1 || len(cc.PIDs) != 1 || cc.PIDs[0] != (ReportContinuityPID{PID: "0x0101", Label: "audio", ErrorCount: 1}) ||
			!jsonEqual(t, report.ContinuityEvents, wantEvents) {
			t.Errorf("%s: continuity %s %s", fixture.path, toJSON(t, cc), toJSON(t, report.ContinuityEvents))
		}
		// PCR jitter: the PCR in slot 58 is 100 µs late; its neighbors in
		// slots 54 and 62 read -50 µs.
		jitter := p.PCRJitter
		if jitter.SampleCount != 16 || jitter.SegmentCount != 1 || jitter.MeasuredSampleCount != 14 || len(jitter.Discontinuities) != 0 ||
			*jitter.MaxJitterNs != 100_000 || *jitter.MaxJitterOffsetBytes != at(58) ||
			*jitter.MinJitterNs != -50_000 || *jitter.MinJitterOffsetBytes != at(54) {
			t.Errorf("%s: PCR jitter %s", fixture.path, toJSON(t, jitter))
		}
	}
}

// The summary output carries the same verdicts, and --fail-on-error still
// writes the complete report before reporting the failed checks.
func TestAllFindingsFixturesFailOnError(t *testing.T) {
	for _, fixture := range allFindingsFixtures {
		report, err := decodeJSONReport(t, func() error {
			return ParseTsFile(fixture.path, options.Options{Format: "json", FailOnError: true})
		})
		var complianceErr *ComplianceError
		if !errors.As(err, &complianceErr) || len(complianceErr.Checks) != 2 {
			t.Fatalf("%s: err = %v", fixture.path, err)
		}
		if report.Status != "complete" || report.Compliance.MaxPCRInterval.Status != "ng" || report.Compliance.MaxPCRPTSDTSGap.Status != "ng" ||
			report.ContinuityCounter.TotalErrorCount != 1 || report.TimestampAnomalies.Status != "anomalies" {
			t.Errorf("%s: %s", fixture.path, toJSON(t, report))
		}
	}
}

func generateAllFindings(t *testing.T, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.ts")
	args = append([]string{"run", "../tools/generate_all_findings", "-output", path}, args...)
	if output, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate fixture: %v\n%s", err, output)
	}
	return path
}

func TestAllFindingsFixtureGenerator(t *testing.T) {
	for _, fixture := range allFindingsFixtures {
		var args []string
		if fixture.m2ts {
			args = append(args, "-m2ts")
		}
		generated, err := os.ReadFile(generateAllFindings(t, args...))
		if err != nil {
			t.Fatal(err)
		}
		committed, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, committed) {
			t.Errorf("%s does not match the generator output", fixture.path)
		}
		if len(committed) >= 16*1024 {
			t.Errorf("%s is %d bytes", fixture.path, len(committed))
		}
	}
}

// Leaving one defect out turns only its own card OK, so no defect depends on
// another to produce its finding; without any defect every card is OK.
func TestAllFindingsDefectsAreIndependent(t *testing.T) {
	ok := [5]string{"ok", "ok", "ok", "ok", "ok"}
	for card, defect := range []string{"pcr-interval", "pcr-pts-gap", "pts-backward", "cc-gap", "pcr-jitter"} {
		want := allFindingsStatuses
		want[card] = "ok"
		report, err := decodeRangeAnalysis(t, generateAllFindings(t, "-omit", defect), options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got := cardStatuses(t, report); got != want {
			t.Errorf("without %s: %v, want %v", defect, got, want)
		}
	}
	report, err := decodeRangeAnalysis(t, generateAllFindings(t, "-omit", "all"), options.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := cardStatuses(t, report); got != ok {
		t.Errorf("without defects: %v", got)
	}
	if jitter := report.Programs[0].PCRJitter; *jitter.MaxJitterNs != 0 || *jitter.MinJitterNs != 0 {
		t.Errorf("constant-rate stream without defects: %s", toJSON(t, jitter))
	}
	if output, err := exec.Command("go", "run", "../tools/generate_all_findings", "-output", filepath.Join(t.TempDir(), "x.ts"), "-omit", "nope").CombinedOutput(); err == nil {
		t.Errorf("an unknown defect was accepted: %s", output)
	} else if !bytes.Contains(output, []byte(`unknown defect "nope"`)) {
		t.Errorf("unexpected error: %s", output)
	}
}
