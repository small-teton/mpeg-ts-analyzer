package tsparser

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// The packet-gap fixtures (tools/generate_packet_gaps) are a bursty remux:
// frame f is packets 8f to 8f+7, audio arrives in bursts with a PCR
// discontinuity inside its longest gap, and PID 0x0102 is removed and
// re-added by PMT changes.
var packetGapsFixtures = []struct {
	path       string
	packetSize int64
	m2ts       bool
}{
	{"../sample_data/sample_188byte_packet_gaps.ts", 188, false},
	{"../sample_data/sample_192byte_packet_gaps.ts", 192, true},
}

func TestPacketGapsFixtureGenerator(t *testing.T) {
	for _, fixture := range packetGapsFixtures {
		path := filepath.Join(t.TempDir(), "fixture.ts")
		args := []string{"run", "../tools/generate_packet_gaps", "-output", path}
		if fixture.m2ts {
			args = append(args, "-m2ts")
		}
		if output, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("generate fixture: %v\n%s", err, output)
		}
		generated, err := os.ReadFile(path)
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

// wantPacketGaps is the expected result of the fixture at packet size size.
// The analysis, and so the PCR time basis, starts at packet 85 (after the
// PMT acquisition); its first PCR is packet 88. The packet counts are
// TSDuck's (tsanalyze) and the gaps those of an independent packet-index scan.
func wantPacketGaps(size int64) ReportPacketGaps {
	at := func(packet int64) *int64 { v := packet * size; return &v }
	count := func(v int64) *int64 { return &v }
	ms := func(v float64) *float64 { return &v }
	yes, no := true, false
	es := func(streamType string) []ReportPIDReference {
		return []ReportPIDReference{{ProgramNumber: 1, Role: "elementary_stream", StreamType: streamType}}
	}
	gap := func(pid string, refs []ReportPIDReference, packets, gap, start, end int64, value float64, exact *bool) ReportPacketGapPID {
		return ReportPacketGapPID{
			PID: pid, References: refs, PacketCount: packets, Status: "info",
			MaxGapPackets: count(gap), MaxGapStartOffsetBytes: at(start), MaxGapEndOffsetBytes: at(end), MaxGapMs: ms(value), MaxGapExact: exact,
		}
	}
	none := []ReportPIDReference{}
	return ReportPacketGaps{TimeBasis: "pcr_interpolated_file_position", PIDs: []ReportPacketGapPID{
		// The PAT and PMT gaps before the first PCR are as large, but only
		// their time after it is exact.
		gap("0x0000", none, 25, 31, 100, 132, 160, &yes),
		gap("0x0011", none, 13, 63, 140, 204, 320, &yes),
		{PID: "0x0012", References: none, PacketCount: 1, Status: "insufficient_data", Reason: "no_consecutive_packets"},
		gap("0x0100", es("0x02"), 300, 5, 91, 97, 30, &yes),
		// Bursts of frames 48 and 64 around the PCR discontinuity of frame
		// 50: the PCR interval across it is not counted, a lower bound.
		gap("0x0101", es("0x03"), 36, 125, 391, 517, 590, &no),
		// 383 packets across its absence (frames 28 to 76) are not a gap.
		gap("0x0102", es("0x06"), 7, 63, 101, 165, 320, &yes),
		gap("0x01ff", []ReportPIDReference{{ProgramNumber: 1, Role: "pcr"}}, 100, 7, 88, 96, 40, &yes),
		gap("0x1000", []ReportPIDReference{{ProgramNumber: 1, Role: "pmt"}}, 25, 31, 116, 148, 160, &yes),
	}}
}

func TestPacketGapsFixtures(t *testing.T) {
	for _, fixture := range packetGapsFixtures {
		want := wantPacketGaps(fixture.packetSize)
		report, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatalf("%s: %v", fixture.path, err)
		}
		if !jsonEqual(t, report.Multiplexing.PacketGaps, want) {
			t.Errorf("%s: multiplexing %s", fixture.path, toJSON(t, report.Multiplexing))
		}
		summary, err := decodeJSONReport(t, func() error { return ParseTsFile(fixture.path, options.Options{Format: "json"}) })
		if err != nil || summary.Status != "complete" || !jsonEqual(t, summary.Multiplexing.PacketGaps, want) {
			t.Errorf("%s: summary %v %s", fixture.path, err, toJSON(t, summary.Multiplexing))
		}
	}
}

// Every program's PIDs are listed with the programs that reference them;
// a selected program leaves the other program's streams without reference.
func TestPacketGapsReferences(t *testing.T) {
	report, err := decodeRangeAnalysis(t, twoPrograms188Path, options.Options{})
	if err != nil {
		t.Fatal(err)
	}
	programs := map[uint16]bool{}
	for _, pid := range report.Multiplexing.PacketGaps.PIDs {
		for _, ref := range pid.References {
			programs[ref.ProgramNumber] = true
		}
	}
	if len(programs) != 2 {
		t.Errorf("programs %v in %s", programs, toJSON(t, report.Multiplexing))
	}
	summary, err := decodeJSONReport(t, func() error {
		return ParseTsFile(twoPrograms188Path, options.Options{Format: "json", Program: 2})
	})
	if err != nil {
		t.Fatal(err)
	}
	unreferenced := 0
	for _, pid := range summary.Multiplexing.PacketGaps.PIDs {
		if len(pid.References) == 0 {
			unreferenced++
		}
	}
	// The other program's streams, beside the PAT and SDT.
	if unreferenced < 3 || len(summary.Multiplexing.PacketGaps.PIDs) != len(report.Multiplexing.PacketGaps.PIDs) {
		t.Errorf("selected program %s", toJSON(t, summary.Multiplexing))
	}
}

func TestPacketGapsText(t *testing.T) {
	out := captureStdout(t, func() {
		if err := ParseTsFile(packetGapsFixtures[0].path, options.Options{}); err != nil {
			t.Error(err)
		}
	})
	want := strings.Join([]string{
		"-----------------------------",
		"Max packet gaps (report only): 8 PIDs, time basis pcr_interpolated_file_position",
		"  PID 0x0000 : 25 packets, max gap 31 packets, 160.000 ms from 0x00004970 to 0x000060f0",
		"  PID 0x0011 : 13 packets, max gap 63 packets, 320.000 ms from 0x000066d0 to 0x000095d0",
		"  PID 0x0012 : 1 packet, no gap (no_consecutive_packets)",
		"  PID 0x0100 (program 1 ES stream_type 0x02) : 300 packets, max gap 5 packets, 30.000 ms from 0x000042d4 to 0x0000473c",
		"  PID 0x0101 (program 1 ES stream_type 0x03) : 36 packets, max gap 125 packets, at least 590.000 ms from 0x00011f24 to 0x00017bac",
		"  PID 0x0102 (program 1 ES stream_type 0x06) : 7 packets, max gap 63 packets, 320.000 ms from 0x00004a2c to 0x0000792c",
		"  PID 0x01ff (program 1 PCR) : 100 packets, max gap 7 packets, 40.000 ms from 0x000040a0 to 0x00004680",
		"  PID 0x1000 (program 1 PMT) : 25 packets, max gap 31 packets, 160.000 ms from 0x00005530 to 0x00006cb0",
		"",
	}, "\n")
	// Only the report-only mux lead follows.
	if !strings.Contains(out, want+"-----------------------------\nMux Lead") {
		t.Errorf("text:\n%s\nwant:\n%s", out, want)
	}
}

// Without any PCR the gaps have no time; a PID that carries both the PCR
// and a stream lists both references.
func TestPacketGapsWithoutTimeBase(t *testing.T) {
	s := newPidReferenceSummary(0, 188)
	s.check.ObserveLayout(analysis.ReferenceLayout{Programs: []analysis.ReferencedProgram{{
		Number: 1, PMTPID: 0x1000, Known: true, PCRPID: 0x0100,
		Streams: []analysis.ElementaryStream{{PID: 0x0100, StreamType: 0x1B}},
	}}})
	s.observeLead(&TsPacket{pid: 0x0100}, 0)
	s.observeLead(&TsPacket{pid: 0x1FFF}, 188)
	s.observeLead(&TsPacket{pid: 0x0100}, 376)
	s.finish(564)
	out := captureStdout(t, func() { dumpPacketGaps(s) })
	want := strings.Join([]string{
		"-----------------------------",
		"Max packet gaps (report only): 1 PID, time basis pcr_interpolated_file_position",
		"  PID 0x0100 (program 1 PCR, program 1 ES stream_type 0x1b) : 2 packets, max gap 1 packet (no_pcr_time_base) from 0x00000000 to 0x00000178",
		"",
	}, "\n")
	if out != want {
		t.Errorf("text:\n%s\nwant:\n%s", out, want)
	}
	r := reportPacketGaps(s).PIDs
	if len(r) != 1 || r[0].Reason != "no_pcr_time_base" || r[0].MaxGapMs != nil || r[0].MaxGapExact != nil || *r[0].MaxGapPackets != 1 || len(r[0].References) != 2 {
		t.Errorf("JSON %s", toJSON(t, r))
	}
}
