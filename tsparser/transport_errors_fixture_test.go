package tsparser

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// The transport-errors fixtures carry packets with transport_error_indicator
// and scrambled packets on the PAT, PMT, PCR, elementary-stream, unreferenced
// and null PIDs (tools/generate_transport_errors). Packet n is slot n of the
// generator.
var transportErrorsFixtures = []struct {
	path       string
	packetSize int64
	m2ts       bool
}{
	{"../sample_data/sample_188byte_transport_errors.ts", 188, false},
	{"../sample_data/sample_192byte_transport_errors.ts", 192, true},
}

func generateTransportErrors(t *testing.T, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.ts")
	args = append([]string{"run", "../tools/generate_transport_errors", "-output", path}, args...)
	if output, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate fixture: %v\n%s", err, output)
	}
	return path
}

func TestTransportErrorsFixtureGenerator(t *testing.T) {
	for _, fixture := range transportErrorsFixtures {
		var args []string
		if fixture.m2ts {
			args = append(args, "-m2ts")
		}
		generated, err := os.ReadFile(generateTransportErrors(t, args...))
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
	}
}

// wantTransportTrust is the expected result of the fixture at packet size
// size: TEI on the null packet read during acquisition (slot 0), audio (25),
// video with PCR (30, 70), PAT (39) and PMT (43); scrambling on an
// unreferenced PID (7), audio (45), video (50, 70), PAT (59) and PMT (63).
func wantTransportTrust(size int64) (tei, scrambled *ReportAffectedPackets) {
	at := func(slot int64) int64 { return slot * size }
	first := func(slot int64) *int64 { v := at(slot); return &v }
	tei = &ReportAffectedPackets{Status: "data_excluded", PacketCount: 6, ExcludedPacketCount: 5, FirstOffsetBytes: first(0), PIDs: []ReportAffectedPID{
		{PID: "0x0000", Label: "PAT", PacketCount: 1, ExcludedPacketCount: 1, FirstOffsetBytes: at(39)},
		{PID: "0x0100", Label: "video", PacketCount: 2, ExcludedPacketCount: 2, FirstOffsetBytes: at(30)},
		{PID: "0x0101", Label: "audio", PacketCount: 1, ExcludedPacketCount: 1, FirstOffsetBytes: at(25)},
		{PID: "0x1000", Label: "PMT", PacketCount: 1, ExcludedPacketCount: 1, FirstOffsetBytes: at(43)},
		{PID: "0x1fff", Label: "null", PacketCount: 1, FirstOffsetBytes: at(0)},
	}}
	scrambled = &ReportAffectedPackets{Status: "data_excluded", PacketCount: 6, ExcludedPacketCount: 5, FirstOffsetBytes: first(7), PIDs: []ReportAffectedPID{
		{PID: "0x0000", Label: "PAT", PacketCount: 1, ExcludedPacketCount: 1, FirstOffsetBytes: at(59)},
		{PID: "0x0100", Label: "video", PacketCount: 2, ExcludedPacketCount: 2, FirstOffsetBytes: at(50)},
		{PID: "0x0101", Label: "audio", PacketCount: 1, ExcludedPacketCount: 1, FirstOffsetBytes: at(45)},
		{PID: "0x0200", Label: "unknown", PacketCount: 1, FirstOffsetBytes: at(7)},
		{PID: "0x1000", Label: "PMT", PacketCount: 1, ExcludedPacketCount: 1, FirstOffsetBytes: at(63)},
	}}
	return tei, scrambled
}

func TestTransportErrorsFixtures(t *testing.T) {
	for _, fixture := range transportErrorsFixtures {
		at := func(slot int64) int64 { return slot * fixture.packetSize }
		report, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatalf("%s: %v", fixture.path, err)
		}
		wantTei, wantScrambled := wantTransportTrust(fixture.packetSize)
		if !jsonEqual(t, report.TransportErrors, wantTei) || !jsonEqual(t, report.ScrambledPackets, wantScrambled) {
			t.Errorf("%s:\ntransport_errors %s\nscrambled_packets %s", fixture.path, toJSON(t, report.TransportErrors), toJSON(t, report.ScrambledPackets))
		}
		// The analysis starts at slot 5, after the PAT and PMT acquisition.
		if report.Status != "complete" || *report.AnalysisStartOffsetBytes != at(5) || len(report.PSITransitions) != 0 || report.ContinuityCounter.Status != "ok" {
			t.Errorf("%s: envelope %s", fixture.path, toJSON(t, report))
		}
		// The PAT version 1 and PMT version 1 in the TEI packets, and the
		// scrambled PAT and PMT, are never applied; each is a warning.
		wantWarnings := []ReportWarning{
			{at(39), "PAT: transport_error_indicator set; packet ignored and any partial section discarded"},
			{at(43), "PMT of program 1 on PID 0x1000: transport_error_indicator set; packet ignored and any partial section discarded"},
			{at(59), "PAT: transport_scrambling_control set; scrambled payload ignored and any partial section discarded"},
			{at(63), "PMT of program 1 on PID 0x1000: transport_scrambling_control set; scrambled payload ignored and any partial section discarded"},
		}
		if !jsonEqual(t, report.Warnings, wantWarnings) {
			t.Errorf("%s: warnings %s", fixture.path, toJSON(t, report.Warnings))
		}
		p := report.Programs[0]
		slotsOf := func(offsets []int64) []int64 {
			var slots []int64
			for _, offset := range offsets {
				slots = append(slots, offset/fixture.packetSize)
			}
			slices.Sort(slots)
			return slots
		}
		// PCR: the TEI packets in slots 30 and 70 lose their PCR (one of them
		// 1 s off); the scrambled video packet in slot 50 keeps its clear PCR.
		var pcrOffsets []int64
		for _, point := range p.Series.PCR {
			pcrOffsets = append(pcrOffsets, point.OffsetBytes)
		}
		var wantPcr []int64
		for slot := int64(6); slot < 100; slot += 2 {
			if slot != 30 && slot != 70 {
				wantPcr = append(wantPcr, slot)
			}
		}
		if got := slotsOf(pcrOffsets); !slices.Equal(got, wantPcr) {
			t.Errorf("%s: PCR slots %v", fixture.path, got)
		}
		interval := p.Compliance.MaxPCRInterval
		if interval.Status != "ok" || *interval.ValueMs != 80 || interval.Observations != 44 || len(p.PCRJitter.Estimate.Discontinuities) != 0 {
			t.Errorf("%s: PCR %s %s", fixture.path, toJSON(t, interval), toJSON(t, p.PCRJitter.Estimate))
		}
		// PES: no affected payload is parsed (their PTS would be 1 s off or
		// scrambled); every other PES, including the one before each affected
		// packet, is.
		var pesOffsets []int64
		for _, point := range p.Series.PES {
			pesOffsets = append(pesOffsets, point.OffsetBytes)
		}
		var wantPes []int64
		for slot := int64(5); slot < 100; slot++ {
			es := slot >= 6 && slot%2 == 0 || slot%4 == 1
			if es && !slices.Contains([]int64{25, 30, 45, 50, 70}, slot) {
				wantPes = append(wantPes, slot)
			}
		}
		if got := slotsOf(pesOffsets); !slices.Equal(got, wantPes) {
			t.Errorf("%s: PES slots %v", fixture.path, got)
		}
		if p.TimestampAnomalies.Status != "ok" || p.Compliance.MaxPCRPTSDTSGap.Status != "ok" || *p.Compliance.MaxPCRPTSDTSGap.ValueMs != 200 {
			t.Errorf("%s: timestamps %s %s", fixture.path, toJSON(t, p.TimestampAnomalies), toJSON(t, p.Compliance))
		}

		// The summary report has the same results.
		summary, err := decodeJSONReport(t, func() error { return ParseTsFile(fixture.path, options.Options{Format: "json"}) })
		if err != nil || summary.Status != "complete" || !jsonEqual(t, summary.TransportErrors, wantTei) || !jsonEqual(t, summary.ScrambledPackets, wantScrambled) ||
			!jsonEqual(t, summary.Warnings, wantWarnings) || summary.TimestampAnomalies.Status != "ok" || len(summary.PSITransitions) != 0 {
			t.Errorf("%s: summary %v %s", fixture.path, err, toJSON(t, summary))
		}
	}
}

// Without the affected packets every check is OK and every PCR and PES is
// used.
func TestTransportErrorsFixtureClean(t *testing.T) {
	report, err := decodeRangeAnalysis(t, generateTransportErrors(t, "-clean"), options.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ok := &ReportAffectedPackets{Status: "ok", PIDs: []ReportAffectedPID{}}
	p := report.Programs[0]
	if !jsonEqual(t, report.TransportErrors, ok) || !jsonEqual(t, report.ScrambledPackets, ok) || len(report.Warnings) != 0 ||
		len(p.Series.PCR) != 47 || len(p.Series.PES) != 71 || *p.Compliance.MaxPCRInterval.ValueMs != 40 {
		t.Errorf("clean: %s", toJSON(t, report))
	}
}

func TestTransportErrorsText(t *testing.T) {
	out := captureStdout(t, func() {
		if err := ParseTsFile(transportErrorsFixtures[0].path, options.Options{}); err != nil {
			t.Error(err)
		}
	})
	want := strings.Join([]string{
		"-----------------------------",
		"Transport Error Indicator: 6 packets in 5 PIDs, first at 0x00000000, 5 excluded from analysis [DATA EXCLUDED]",
		"  PID 0x0000 (PAT) : 1 packet, first at 0x00001ca4, 1 excluded",
		"  PID 0x0100 (video) : 2 packets, first at 0x00001608, 2 excluded",
		"  PID 0x0101 (audio) : 1 packet, first at 0x0000125c, 1 excluded",
		"  PID 0x1000 (PMT) : 1 packet, first at 0x00001f94, 1 excluded",
		"  PID 0x1fff (null) : 1 packet, first at 0x00000000, 0 excluded",
		"Transport Scrambling: 6 packets in 5 PIDs, first at 0x00000524, 5 excluded from analysis [DATA EXCLUDED]",
		"  PID 0x0000 (PAT) : 1 packet, first at 0x00002b54, 1 excluded",
		"  PID 0x0100 (video) : 2 packets, first at 0x000024b8, 2 excluded",
		"  PID 0x0101 (audio) : 1 packet, first at 0x0000210c, 1 excluded",
		"  PID 0x0200 (unknown) : 1 packet, first at 0x00000524, 0 excluded",
		"  PID 0x1000 (PMT) : 1 packet, first at 0x00002e44, 1 excluded",
		"",
	}, "\n")
	// The PES header and PTS repetition summary follows.
	if !strings.Contains(out, want+"-----------------------------\nPES Headers: ") {
		t.Errorf("text summary:\n%s\nwant suffix:\n%s", out, want)
	}

	out = captureStdout(t, func() {
		if err := ParseTsFile(allFindingsFixtures[0].path, options.Options{}); err != nil {
			t.Error(err)
		}
	})
	want = fmt.Sprintf("Transport Error Indicator: 1 packet in 1 PID, first at 0x%08x, 0 excluded from analysis [AFFECTED]\n", 7*188)
	if !strings.Contains(out, want) {
		t.Errorf("all findings: %s", out)
	}

	out = captureStdout(t, func() {
		if err := ParseTsFile(sample188Path, options.Options{}); err != nil {
			t.Error(err)
		}
	})
	want = "Transport Error Indicator: no packets with transport_error_indicator set\nTransport Scrambling: no scrambled packets\n"
	if !strings.Contains(out, want+"-----------------------------\nPES Headers: ") {
		t.Errorf("healthy: %s", out)
	}
}
