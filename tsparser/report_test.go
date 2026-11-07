package tsparser

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestReportConversions(t *testing.T) {
	if got := reportCheck(complianceCheck{limitMs: 1, skippedWhy: "missing"}); got.Status != "skipped" || got.Reason != "missing" || got.ValueMs != nil {
		t.Fatalf("skipped check: %+v", got)
	}

	summary := newEmptyContinuitySummary()
	summary.addProgramLabels(0x1000, 0x10, []ProgramInfo{{streamType: 0x02, elementaryPid: 0x10}})
	for _, pid := range []uint16{0x20, 0x10, 0x20} {
		summary.Add(continuityEvent{PID: pid})
	}
	continuity := reportContinuity(summary)
	if continuity.Status != "errors" || continuity.TotalErrorCount != 3 || continuity.PIDs[0].PID != "0x0010" || continuity.PIDs[1].Label != "unknown" {
		t.Fatalf("continuity: %+v", continuity)
	}

	anomalies := NewTimestampAnomaly()
	for _, kind := range []anomalyKind{kindBackward, kindWraparound, kindGap, kindDtsAfterPts} {
		anomalies.add(0x31, 0, kind, "detail")
	}
	anomalyReport := reportAnomalies(anomalies)
	if anomalyReport.Status != "anomalies" || len(anomalyReport.Items) != 4 || anomalyReport.Items[2].Kind != "forward_gap" {
		t.Fatalf("anomalies: %+v", anomalyReport)
	}

	if got := reportJitter(&PcrJitter{}); got.Status != "skipped" || got.Reason == "" || got.Estimate.Model != "byte_position" ||
		len(got.Estimate.PCRPIDs) != 0 || got.Delivery.Assessment != "unknown" || got.Delivery.AverageRateBPS != nil {
		t.Fatalf("few samples: %+v", got)
	}
	noInterior := &PcrJitter{samples: []pcrSample{{pcr: 1}, {pcr: 2, discontinuity: true}, {pcr: 3, discontinuity: true}}}
	if got := reportJitter(noInterior); got.Estimate.Observations != 0 || len(got.Estimate.Discontinuities) != 2 || got.Estimate.MaxAbsJitterNs != nil {
		t.Fatalf("no interior: %+v", got)
	}
	tooMany := &PcrJitter{samples: []pcrSample{{pos: 0, pcr: 1000}, {pos: 1, pcr: 2000}, {pos: 2, pcr: 3000}, {pos: 3, pcr: 4000, discontinuity: true}, {pos: 4, pcr: 5000, discontinuity: true}, {pos: 5, pcr: 6000, discontinuity: true}}}
	if got := reportJitter(tooMany); got.Reason != "too many discontinuities" {
		t.Fatalf("too many: %+v", got)
	}
	// Three seconds of constant delivery (two full windows) with a 1 µs error
	// in one PCR.
	warning := &PcrJitter{samples: cbrSamples(76, 0x0100)}
	warning.samples[40].pcr += 27
	if got := reportJitter(warning); got.Status != "warning" || got.Reason != "" || *got.Estimate.MaxAbsJitterNs != 1000 ||
		*got.Estimate.MaxAbsJitterOffsetBytes != warning.samples[40].pos || got.Delivery.Assessment != "constant" || *got.Delivery.MaxDeviationPercent > 0.01 {
		t.Fatalf("warning: %s", toJSON(t, got))
	}
	ok := &PcrJitter{samples: cbrSamples(76, 0x0100)}
	// The PCR_PID changes at sample 50: a pcr_pid_change discontinuity, with
	// one window before and one after it.
	for i := 50; i < len(ok.samples); i++ {
		ok.samples[i].pid = 0x0200
	}
	if got := reportJitter(ok); got.Status != "ok" || !jsonEqual(t, got.Estimate.PCRPIDs, []string{"0x0100", "0x0200"}) || got.Delivery.WindowCount != 2 ||
		len(got.Estimate.Discontinuities) != 1 || got.Estimate.Discontinuities[0].Kind != "pcr_pid_change" {
		t.Fatalf("ok: %s", toJSON(t, got))
	}
	// One window: the evidence has rates but no deviation.
	oneWindow := &PcrJitter{samples: cbrSamples(30, 0x0100)}
	if got := reportJitter(oneWindow); got.Status != "not_assessable" || got.Delivery.Assessment != "unknown" || got.Delivery.WindowCount != 1 ||
		*got.Delivery.AverageRateBPS != 75_200 || got.Delivery.MaxDeviationPercent != nil || !strings.Contains(got.Reason, "unknown") {
		t.Fatalf("one window: %s", toJSON(t, got))
	}

	skippedBitrate := reportBitrate(NewBitrateStats(0, 0x20, 0x31, nil))
	if skippedBitrate.Status != "skipped" || skippedBitrate.Reason == "" || skippedBitrate.ProgramAverageBPS != 0 {
		got := skippedBitrate
		t.Fatalf("bitrate skipped: %+v", got)
	}
	encodedBitrate, err := json.Marshal(skippedBitrate)
	if err != nil || !strings.Contains(string(encodedBitrate), `"program_average_bps":0`) {
		t.Fatalf("zero program average must remain explicit: %s, %v", encodedBitrate, err)
	}

	if err := writeJSONReport(failingWriter{}, newAnalysisReport("x.ts", 0, 0)); err == nil {
		t.Fatal("expected JSON write error")
	}
	if err := parseTsFileJSONTo("/nonexistent/input.ts", options.Options{}, failingWriter{}); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected joined parse/write error, got %v", err)
	}
	if err := parseTsFileJSONTo("../sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts", options.Options{}, failingWriter{}); err == nil || !strings.Contains(err.Error(), "write JSON output") {
		t.Fatalf("expected output-only error, got %v", err)
	}
}
