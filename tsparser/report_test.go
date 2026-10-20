package tsparser

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

func TestJSONSchemaDocument(t *testing.T) {
	data, err := os.ReadFile("../docs/json-schema-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("invalid JSON Schema document: %v", err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("unexpected JSON Schema dialect: %v", schema["$schema"])
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestReportConversions(t *testing.T) {
	if got := reportCheck(complianceCheck{limitMs: 1, skippedWhy: "missing"}); got.Status != "skipped" || got.Reason != "missing" || got.ValueMs != nil {
		t.Fatalf("skipped check: %+v", got)
	}

	summary := &continuityCounterSummary{counts: map[uint16]int{0x20: 2, 0x10: 1}, labels: map[uint16]string{0x10: "video"}}
	continuity := reportContinuity(summary)
	if continuity.Status != "errors" || continuity.TotalErrorCount != 3 || continuity.PIDs[0].PID != "0x0010" || continuity.PIDs[1].Label != "unknown" {
		t.Fatalf("continuity: %+v", continuity)
	}

	anomalies := NewTimestampAnomaly()
	for _, kind := range []anomalyKind{kindBackward, kindWraparound, kindGap, kindDtsAfterPts} {
		anomalies.add(0x31, kind, "detail")
	}
	anomalyReport := reportAnomalies(anomalies)
	if anomalyReport.Status != "anomalies" || len(anomalyReport.Items) != 4 || anomalyReport.Items[2].Kind != "forward_gap" {
		t.Fatalf("anomalies: %+v", anomalyReport)
	}

	if got := reportJitter(&PcrJitter{}); got.Status != "skipped" || got.Reason == "" {
		t.Fatalf("few samples: %+v", got)
	}
	noInterior := &PcrJitter{samples: []pcrSample{{pcr: 1}, {pcr: 2, discontinuity: true}, {pcr: 3, discontinuity: true}}}
	if got := reportJitter(noInterior); got.MeasuredSampleCount != 0 || len(got.Discontinuities) != 2 {
		t.Fatalf("no interior: %+v", got)
	}
	tooMany := &PcrJitter{samples: []pcrSample{{pos: 0, pcr: 1000}, {pos: 1, pcr: 2000}, {pos: 2, pcr: 3000}, {pos: 3, pcr: 4000, discontinuity: true}, {pos: 4, pcr: 5000, discontinuity: true}, {pos: 5, pcr: 6000, discontinuity: true}}}
	if got := reportJitter(tooMany); got.Reason != "too many discontinuities" {
		t.Fatalf("too many: %+v", got)
	}
	warning := &PcrJitter{samples: []pcrSample{{pos: 0, pcr: 1000}, {pos: 1, pcr: 2027}, {pos: 2, pcr: 3000}}}
	if got := reportJitter(warning); got.Status != "warning" {
		t.Fatalf("warning: %+v", got)
	}
	ok := &PcrJitter{samples: []pcrSample{{pos: 0, pcr: 1000}, {pos: 1, pcr: 2000}, {pos: 2, pcr: 3000}}}
	if got := reportJitter(ok); got.Status != "ok" {
		t.Fatalf("ok: %+v", got)
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
