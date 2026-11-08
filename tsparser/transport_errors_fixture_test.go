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
	// malformed is the twin written with -malformed, whose TEI packets also
	// have a header or adaptation field that cannot be parsed.
	malformed string
}{
	{"../sample_data/diagnostics/sample_188byte_transport_errors.ts", 188, false, "../sample_data/diagnostics/sample_188byte_transport_errors_malformed.ts"},
	{"../sample_data/diagnostics/sample_192byte_transport_errors.ts", 192, true, "../sample_data/diagnostics/sample_192byte_transport_errors_malformed.ts"},
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
		for path, extra := range map[string][]string{fixture.path: nil, fixture.malformed: {"-malformed"}} {
			generated, err := os.ReadFile(generateTransportErrors(t, append(args, extra...)...))
			if err != nil {
				t.Fatal(err)
			}
			committed, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(generated, committed) {
				t.Errorf("%s does not match the generator output", path)
			}
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
		"-----------------------------",
		"",
	}, "\n")
	// The PAT/PMT health summary follows.
	if !strings.Contains(out, want+"PAT/PMT health: ") {
		t.Errorf("text summary:\n%s\nwant:\n%s", out, want)
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
	if !strings.Contains(out, want) {
		t.Errorf("healthy: %s", out)
	}
}

// malformedTEISlots are the slots of the TEI packets that the -malformed
// fixture damages, with the adaptation_field_control each one ends up with.
var malformedTEISlots = map[int64]uint8{0: 0, 25: 3, 30: 3, 39: 0, 43: 3, 70: 3}

// The TEI packets of the malformed twin cannot be parsed as clear packets:
// with transport_error_indicator cleared, each one is a parse error.
func TestTransportErrorsMalformedFixtureIsMalformed(t *testing.T) {
	for _, fixture := range transportErrorsFixtures {
		data, err := os.ReadFile(fixture.malformed)
		if err != nil {
			t.Fatal(err)
		}
		for slot := range int64(100) {
			raw := data[(slot+1)*fixture.packetSize-tsPayloadSize : (slot+1)*fixture.packetSize]
			_, damaged := malformedTEISlots[slot]
			if raw[1]&0x80 != 0 != damaged {
				t.Fatalf("%s: slot %d transport_error_indicator %d", fixture.malformed, slot, raw[1]>>7)
			}
			if !damaged {
				continue
			}
			clear := NewTsPacket()
			clear.Append(raw)
			clear.buf[1] &^= 0x80
			if err := clear.Parse(); err == nil {
				t.Errorf("%s: slot %d parses without transport_error_indicator", fixture.malformed, slot)
			}
		}
	}
}

// Damaging the TEI packets beyond their fixed header changes nothing: the
// analysis, the summary and the text output equal those of the twin with
// well-formed TEI packets.
func TestTransportErrorsMalformedFixture(t *testing.T) {
	for _, fixture := range transportErrorsFixtures {
		want, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeRangeAnalysis(t, fixture.malformed, options.Options{})
		if err != nil {
			t.Fatalf("%s: %v", fixture.malformed, err)
		}
		got.Input.Path, want.Input.Path = "", ""
		if got.Status != "complete" || !jsonEqual(t, got, want) {
			t.Errorf("%s: analysis\n%s\nwant\n%s", fixture.malformed, toJSON(t, got), toJSON(t, want))
		}

		summary := func(path string) AnalysisReport {
			report, err := decodeJSONReport(t, func() error { return ParseTsFile(path, options.Options{Format: "json"}) })
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			report.Input.Path = ""
			return report
		}
		if got, want := summary(fixture.malformed), summary(fixture.path); got.Status != "complete" || !jsonEqual(t, got, want) {
			t.Errorf("%s: summary\n%s\nwant\n%s", fixture.malformed, toJSON(t, got), toJSON(t, want))
		}

		text := func(path string) string {
			out := captureStdout(t, func() {
				if err := ParseTsFile(path, options.Options{}); err != nil {
					t.Errorf("%s: %v", path, err)
				}
			})
			_, rest, _ := strings.Cut(out, "\n") // the input file line
			return rest
		}
		if got, want := text(fixture.malformed), text(fixture.path); got != want {
			t.Errorf("%s: text\n%s\nwant\n%s", fixture.malformed, got, want)
		}
	}
}

// Every packet of the malformed twin is listed as valid, with the header
// fields as decoded; the PCR of a TEI packet is listed from its bytes.
func TestTransportErrorsMalformedPackets(t *testing.T) {
	for _, fixture := range transportErrorsFixtures {
		page, err := decodePacketsPage(t, fixture.malformed, options.Options{Count: 100})
		if err != nil || len(page.Packets) != 100 {
			t.Fatalf("%s: %v %d packets", fixture.malformed, err, len(page.Packets))
		}
		pcrMs := map[int64]float64{30: 11600, 70: 11400} // slot 30 carries a PCR 1 s late
		for slot, packet := range page.Packets {
			afc, damaged := malformedTEISlots[int64(slot)]
			if !packet.Valid || packet.TransportError != damaged {
				t.Errorf("%s: slot %d %+v", fixture.malformed, slot, packet)
			}
			if !damaged {
				continue
			}
			pcr, hasPcr := pcrMs[int64(slot)]
			if packet.AdaptationFieldControl != afc || (packet.PCRMs != nil) != hasPcr || hasPcr && *packet.PCRMs != pcr || packet.Sections != nil {
				t.Errorf("%s: slot %d %s", fixture.malformed, slot, toJSON(t, packet))
			}
		}

		// The detail decodes each damaged packet from its bytes.
		for slot := range malformedTEISlots {
			detail, err := decodePacketDetail(t, fixture.malformed, options.Options{Offset: slot * fixture.packetSize})
			if err != nil || detail.Status != "complete" || detail.Packet == nil {
				t.Errorf("%s: slot %d detail %v %s", fixture.malformed, slot, err, toJSON(t, detail))
			}
		}
	}
}

// The dumps of the malformed twin run to the end and print, for each damaged
// TEI packet with an adaptation field, that it was not parsed.
func TestTransportErrorsMalformedDump(t *testing.T) {
	opts := options.Options{DumpHeader: true, DumpPayload: true, DumpAdaptationField: true}
	out := captureStdout(t, func() {
		if err := ParseTsFile(transportErrorsFixtures[0].malformed, opts); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "not parsed: transport_error_indicator is set\n") || !strings.Contains(out, "Transport Error Indicator: 6 packets in 5 PIDs") {
		t.Errorf("dump:\n%s", out)
	}
}

// Without transport_error_indicator the same damage is a parse error: with
// the indicator cleared on the audio packet of slot 25 (adaptation_field_length
// 200) or the PAT of slot 39 (adaptation_field_control 0), the range analysis
// fails there and the packet list shows the packet as invalid.
func TestTransportErrorsMalformedWithoutIndicator(t *testing.T) {
	fixture := transportErrorsFixtures[0]
	data, err := os.ReadFile(fixture.malformed)
	if err != nil {
		t.Fatal(err)
	}
	for slot, want := range map[int64]string{25: "invalid adaptation_field_length: 200", 39: "invalid adaptation_field_control: 0 (reserved)"} {
		damaged := slices.Clone(data)
		damaged[slot*188+1] &^= 0x80
		path := filepath.Join(t.TempDir(), "damaged.ts")
		if err := os.WriteFile(path, damaged, 0o600); err != nil {
			t.Fatal(err)
		}
		report, err := decodeRangeAnalysis(t, path, options.Options{})
		if err == nil || !strings.Contains(err.Error(), want) || report.Status != "failed" {
			t.Errorf("slot %d: %v %s", slot, err, toJSON(t, report))
		}
		page, err := decodePacketsPage(t, path, options.Options{Count: 100})
		if err != nil || page.Packets[slot].Valid {
			t.Errorf("slot %d: %v %s", slot, err, toJSON(t, page.Packets[slot]))
		}
	}
}
