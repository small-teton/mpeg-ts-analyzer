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
// deliberate defect per Overview transport-health check
// (tools/generate_all_findings). Packet n is slot n of the generator.
var allFindingsFixtures = []struct {
	path       string
	packetSize int64
	// flag selects the packet size of the generator: none, -m2ts or -trailer.
	flag string
}{
	{"../sample_data/sample_188byte_all_findings.ts", 188, ""},
	{"../sample_data/sample_192byte_all_findings.ts", 192, "-m2ts"},
	{"../sample_data/sample_204byte_all_findings.ts", 204, "-trailer"},
}

// cardStatuses returns the statuses of the ten Overview checks of the only
// program: max PCR interval, PCR-PTS/DTS gap, PTS/DTS anomalies, continuity
// counter, PCR jitter, transport errors, scrambled packets, PAT/PMT health,
// PES headers and PTS repetition.
func cardStatuses(t *testing.T, report RangeAnalysisReport) [10]string {
	t.Helper()
	if report.Status != "complete" || len(report.Programs) != 1 || report.ContinuityCounter == nil || report.TransportErrors == nil || report.ScrambledPackets == nil || report.PSIHealth == nil ||
		report.PESHeaders == nil || report.PTSRepetition == nil {
		t.Fatalf("unexpected analysis: %s", toJSON(t, report))
	}
	p := report.Programs[0]
	return [10]string{
		p.Compliance.MaxPCRInterval.Status,
		p.Compliance.MaxPCRPTSDTSGap.Status,
		p.TimestampAnomalies.Status,
		report.ContinuityCounter.Status,
		p.PCRJitter.Status,
		report.TransportErrors.Status,
		report.ScrambledPackets.Status,
		report.PSIHealth.Status,
		report.PESHeaders.Status,
		report.PTSRepetition.Status,
	}
}

var allFindingsStatuses = [10]string{"ng", "ng", "anomalies", "errors", "ng", "affected", "affected", "ng", "errors", "ng"}

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

		// Max PCR interval: slots 22, 24 and 26 carry no PCR.
		interval := p.Compliance.MaxPCRInterval
		if *interval.ValueMs != 160 || interval.LimitMs != 100 || interval.Observations != 54 {
			t.Errorf("%s: max PCR interval %s", fixture.path, toJSON(t, interval))
		}
		// PCR-PTS/DTS gap: every audio PTS is 1200 ms ahead. The malformed
		// video header in slot 106 and the nine audio PES packets without PTS
		// in slots 65 to 97 give no timestamp.
		gap := p.Compliance.MaxPCRPTSDTSGap
		if *gap.ValueMs != 1200 || gap.LimitMs != 1000 || gap.Observations != 72 {
			t.Errorf("%s: PCR-PTS/DTS gap %s", fixture.path, toJSON(t, gap))
		}
		// PTS/DTS anomalies: the video PTS in slot 46 goes back 40 ms.
		anomalies := p.TimestampAnomalies.Items
		events := p.Series.TimestampEvents
		if len(anomalies) != 1 || anomalies[0].PID != "0x0100" || anomalies[0].Kind != "backward" || anomalies[0].Count != 1 ||
			len(events) != 1 || events[0].OffsetBytes != at(46) {
			t.Errorf("%s: timestamp anomalies %s %s", fixture.path, toJSON(t, anomalies), toJSON(t, events))
		}
		// Continuity counter: the audio packet in slot 25 skips counter 5,
		// for the whole multiplex.
		cc := report.ContinuityCounter
		wantEvents := []ReportContinuityEvent{{OffsetBytes: at(25), PID: "0x0101", ExpectedCC: 5, ActualCC: 6}}
		if cc.TotalErrorCount != 1 || len(cc.PIDs) != 1 || cc.PIDs[0] != (ReportContinuityPID{PID: "0x0101", Label: "audio", ErrorCount: 1}) ||
			!jsonEqual(t, report.ContinuityEvents, wantEvents) {
			t.Errorf("%s: continuity %s %s", fixture.path, toJSON(t, cc), toJSON(t, report.ContinuityEvents))
		}
		// PCR jitter: the PCR in slot 58 is 100 µs late; its neighbors in
		// slots 56 and 60 read -50 µs. The 2.4 s stream gives two 1-second
		// windows at exactly 75.2 kbit/s, so delivery is constant and the
		// estimate is judged.
		jitter := p.PCRJitter
		est := jitter.Estimate
		if est.Model != "byte_position" || !jsonEqual(t, est.PCRPIDs, []string{"0x0100"}) || est.SampleCount != 55 || est.SegmentCount != 1 || est.Observations != 53 ||
			len(est.Discontinuities) != 0 || *est.MaxAbsJitterNs != 100_000 || *est.MaxAbsJitterOffsetBytes != at(58) ||
			*est.MaxJitterNs != 100_000 || *est.MaxJitterOffsetBytes != at(58) ||
			*est.MinJitterNs != -50_000 || *est.MinJitterOffsetBytes != at(56) {
			t.Errorf("%s: PCR jitter estimate %s", fixture.path, toJSON(t, est))
		}
		if d := jitter.Delivery; d.Assessment != "constant" || d.WindowCount != 2 || *d.AverageRateBPS != 75_200 || *d.MinWindowRateBPS != 75_200 ||
			*d.MaxWindowRateBPS != 75_200 || *d.MaxDeviationPercent != 0 || jitter.Reason != "" {
			t.Errorf("%s: PCR jitter delivery %s", fixture.path, toJSON(t, jitter))
		}
		// Transport errors and scrambling: the null packets in slots 7 and 11,
		// whose payload the analysis never reads.
		for _, tt := range []struct {
			got  *ReportAffectedPackets
			slot int64
		}{{report.TransportErrors, 7}, {report.ScrambledPackets, 11}} {
			first := at(tt.slot)
			want := &ReportAffectedPackets{Status: "affected", PacketCount: 1, FirstOffsetBytes: &first, PIDs: []ReportAffectedPID{
				{PID: "0x1fff", Label: "null", PacketCount: 1, FirstOffsetBytes: first},
			}}
			if !jsonEqual(t, tt.got, want) {
				t.Errorf("%s: slot %d: %s", fixture.path, tt.slot, toJSON(t, tt.got))
			}
		}
		// PAT/PMT health: no PAT in slot 59 and no PMT in slot 63, so 800 ms
		// pass between the PATs in slots 39 and 79 and between the PMTs in
		// slots 43 and 83.
		h := report.PSIHealth
		if len(h.Tables) != 2 {
			t.Fatalf("%s: PAT/PMT health %s", fixture.path, toJSON(t, h))
		}
		for i, want := range []struct {
			instances int
			end       int64
		}{{7, 79}, {6, 83}} {
			table := h.Tables[i]
			if !jsonEqual(t, table.Reasons, []string{"late"}) || table.InstanceCount != want.instances || *table.Repetition.MaxIntervalMs != 800 ||
				*table.Repetition.MaxIntervalEndOffsetBytes != at(want.end) || table.Repetition.LateIntervalCount != 1 {
				t.Errorf("%s: PAT/PMT health %s", fixture.path, toJSON(t, table))
			}
		}
		// PES headers: the video PTS in slot 106 has a marker_bit of 0.
		headers := report.PESHeaders
		wantErrors := []ReportPESHeaderError{{Kind: "bad_marker_bits", Count: 1, FirstOffsetBytes: at(106)}}
		if headers.ErrorCount != 1 || headers.CheckedCount != 87 || *headers.FirstErrorOffsetBytes != at(106) || !jsonEqual(t, headers.Errors, wantErrors) ||
			headers.PIDs[0].Status != "errors" || headers.PIDs[1].Status != "ok" {
			t.Errorf("%s: PES headers %s", fixture.path, toJSON(t, headers))
		}
		// PTS repetition: no audio PTS between slots 61 and 101 (800 ms).
		repetition := report.PTSRepetition
		if repetition.ExceededCount != 1 || *repetition.MaxIntervalMs != 800 || *repetition.MaxIntervalStartOffsetBytes != at(61) ||
			*repetition.MaxIntervalEndOffsetBytes != at(101) || repetition.PIDs[0].Status != "ok" || repetition.PIDs[1].Status != "ng" {
			t.Errorf("%s: PTS repetition %s", fixture.path, toJSON(t, repetition))
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
		if fixture.flag != "" {
			args = append(args, fixture.flag)
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
		if len(committed) >= 32*1024 {
			t.Errorf("%s is %d bytes", fixture.path, len(committed))
		}
	}
}

// Leaving one defect out turns only its own card OK, so no defect depends on
// another to produce its finding; without any defect every card is OK.
func TestAllFindingsDefectsAreIndependent(t *testing.T) {
	ok := [10]string{"ok", "ok", "ok", "ok", "ok", "ok", "ok", "ok", "ok", "ok"}
	for card, defect := range []string{"pcr-interval", "pcr-pts-gap", "pts-backward", "cc-gap", "pcr-jitter", "tei", "scrambled", "psi-late", "pes-header", "pts-repetition"} {
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
	if jitter := report.Programs[0].PCRJitter; jitter.Delivery.Assessment != "constant" || *jitter.Estimate.MaxJitterNs != 0 || *jitter.Estimate.MinJitterNs != 0 {
		t.Errorf("constant-rate stream without defects: %s", toJSON(t, jitter))
	}
	if output, err := exec.Command("go", "run", "../tools/generate_all_findings", "-output", filepath.Join(t.TempDir(), "x.ts"), "-omit", "nope").CombinedOutput(); err == nil {
		t.Errorf("an unknown defect was accepted: %s", output)
	} else if !bytes.Contains(output, []byte(`unknown defect "nope"`)) {
		t.Errorf("unexpected error: %s", output)
	}
}
