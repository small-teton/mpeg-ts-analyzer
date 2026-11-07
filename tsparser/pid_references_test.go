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

// The PID-reference fixtures (tools/generate_pid_references) reference absent,
// silent and intermittent PIDs and carry unreferenced PIDs. Packet n is slot
// n%4 of frame n/4 of the generator.
var pidReferencesFixtures = []struct {
	path       string
	packetSize int64
	m2ts       bool
}{
	{"../sample_data/sample_188byte_pid_references.ts", 188, false},
	{"../sample_data/sample_192byte_pid_references.ts", 192, true},
}

func TestPidReferencesFixtureGenerator(t *testing.T) {
	for _, fixture := range pidReferencesFixtures {
		path := filepath.Join(t.TempDir(), "fixture.ts")
		args := []string{"run", "../tools/generate_pid_references", "-output", path}
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

// wantPidReferences is the expected result of the fixture at packet size
// size. The analysis starts at packet 43, after the PAT and PMT acquisition;
// PMT version 1 applies from packet 571; the scan ends at packet 640.
func wantPidReferences(size int64) *ReportPIDReferences {
	at := func(packet int64) int64 { return packet * size }
	ptr := func(v int64) *int64 { return &v }
	ms := func(v float64) *float64 { return &v }
	yes, no := true, false
	row := func(role, pid, streamType, status, reason string, judged bool, packets, first, last, referenced, from, until int64) ReportReferencedPID {
		r := ReportReferencedPID{
			ProgramNumber: 1, Role: role, PID: pid, StreamType: streamType, Status: status, Reason: reason, SilenceJudged: judged,
			PacketCount: packets, ReferencedPacketCount: referenced, ReferencedFromOffsetBytes: at(from), ReferencedUntilOffsetBytes: at(until),
		}
		if packets > 0 {
			r.FirstOffsetBytes, r.LastOffsetBytes = ptr(at(first)), ptr(at(last))
		}
		return r
	}
	silence := func(r ReportReferencedPID, value float64, exact *bool, start, end int64) ReportReferencedPID {
		r.MaxSilenceMs, r.MaxSilenceExact, r.MaxSilenceStartOffsetBytes, r.MaxSilenceEndOffsetBytes = ms(value), exact, ptr(at(start)), ptr(at(end))
		return r
	}
	video := func(role, streamType string) ReportReferencedPID {
		return silence(row(role, "0x0100", streamType, "ok", "", true, 160, 0, 636, 149, 43, 640), 100, &yes, 44, 48)
	}
	return &ReportPIDReferences{
		TimeBasis: "pcr_interpolated_file_position", SilenceLimitMs: 5000, UnreferencedGraceMs: 500,
		Referenced: ReportReferencedPIDs{Status: "absent", PIDs: []ReportReferencedPID{
			silence(row("pmt", "0x1000", "", "ok", "", true, 40, 10, 634, 37, 43, 640), 400, &yes, 58, 74),
			video("pcr", ""),
			video("elementary_stream", "0x02"),
			// Audio frames 60 to 119 are missing.
			silence(row("elementary_stream", "0x0101", "0x03", "silent", "", true, 100, 1, 637, 89, 43, 640), 6100, &yes, 237, 481),
			// Private data is not judged. The silence of a reference without
			// packets starts before the first PCR (packet 44), whose time it
			// gets: a lower bound.
			silence(row("elementary_stream", "0x0102", "0x06", "insufficient_data", "intermittent_stream_type", false, 0, 0, 0, 0, 43, 640), 14800, &no, 43, 640),
			silence(row("elementary_stream", "0x0103", "0x04", "absent", "", true, 0, 0, 0, 0, 43, 571), 13175, &no, 43, 571),
			silence(row("elementary_stream", "0x0105", "0x03", "ok", "", true, 11, 559, 639, 9, 571, 640), 200, &yes, 575, 583),
			silence(row("elementary_stream", "0x0106", "0x03", "ok", "", true, 74, 3, 587, 66, 43, 571), 200, &yes, 51, 59),
		}},
		Unreferenced: ReportUnreferencedPIDs{Status: "unreferenced", PIDs: []ReportUnreferencedPID{
			{PID: "0x0300", Status: "unreferenced", PacketCount: 5, FirstOffsetBytes: at(126), LastOffsetBytes: at(190), LongestRunMs: 12750},
			{PID: "0x0500", Status: "insufficient_data", PacketCount: 2, FirstOffsetBytes: at(622), LastOffsetBytes: at(638), LongestRunMs: 350},
		}},
	}
}

func TestPidReferencesFixtures(t *testing.T) {
	for _, fixture := range pidReferencesFixtures {
		want := wantPidReferences(fixture.packetSize)
		report, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatalf("%s: %v", fixture.path, err)
		}
		if !jsonEqual(t, report.PIDReferences, want) {
			t.Errorf("%s: pid_references %s", fixture.path, toJSON(t, report.PIDReferences))
		}
		if report.Status != "complete" || *report.AnalysisStartOffsetBytes != 43*fixture.packetSize || len(report.PSITransitions) != 1 ||
			report.PSITransitions[0].OffsetBytes != 570*fixture.packetSize || report.ContinuityCounter.Status != "ok" {
			t.Errorf("%s: envelope %s", fixture.path, toJSON(t, report))
		}

		summary, err := decodeJSONReport(t, func() error { return ParseTsFile(fixture.path, options.Options{Format: "json"}) })
		if err != nil || summary.Status != "complete" || !jsonEqual(t, summary.PIDReferences, want) {
			t.Errorf("%s: summary %v %s", fixture.path, err, toJSON(t, summary.PIDReferences))
		}
	}
}

// pesSummaryStart begins the PES header and PTS repetition summary, which
// follows the PID reference summary.
const pesSummaryStart = "-----------------------------\nPES Headers: "

func TestPidReferencesText(t *testing.T) {
	out := captureStdout(t, func() {
		if err := ParseTsFile(pidReferencesFixtures[0].path, options.Options{}); err != nil {
			t.Error(err)
		}
	})
	want := strings.Join([]string{
		"-----------------------------",
		"Referenced PIDs: 8 references, silence limit 5000 ms [ABSENT]",
		"  program 1 ES PID 0x0101 (stream_type 0x03) : silent, no packet for 6100.0 ms from 0x0000ae0c to 0x0001613c",
		"  program 1 ES PID 0x0102 (stream_type 0x06) : insufficient data (intermittent_stream_type)",
		"  program 1 ES PID 0x0103 (stream_type 0x04) : absent, no packet for 13175.0 ms from 0x00001f94 to 0x0001a354",
		"Unreferenced PIDs: 2 PIDs, grace 500 ms [UNREFERENCED]",
		"  PID 0x0300 : unreferenced, 5 packets from 0x00005c88 to 0x00008b88, 12750.0 ms",
		"  PID 0x0500 : insufficient data, 2 packets from 0x0001c8c8 to 0x0001d488, 350.0 ms",
		"",
	}, "\n")
	if !strings.Contains(out, want+pesSummaryStart) {
		t.Errorf("text summary:\n%s\nwant suffix:\n%s", out, want)
	}

	out = captureStdout(t, func() {
		if err := ParseTsFile(sample188Path, options.Options{}); err != nil {
			t.Error(err)
		}
	})
	want = "Referenced PIDs: 4 references, silence limit 5000 ms [OK]\nUnreferenced PIDs: none [OK]\n"
	if !strings.Contains(out, want+pesSummaryStart) {
		t.Errorf("healthy: %s", out)
	}

	// One selected program of two: the other program's streams are unknown.
	out = captureStdout(t, func() {
		if err := ParseTsFile(twoPrograms188Path, options.Options{Program: 2}); err != nil {
			t.Error(err)
		}
	})
	want = "Referenced PIDs: 5 references, silence limit 5000 ms [OK]\nUnreferenced PIDs: not checked (program_layouts_unknown) [SKIPPED]\n"
	if !strings.Contains(out, want+pesSummaryStart) {
		t.Errorf("selected program: %s", out)
	}
}

// Without any PCR of the reference program, durations are unknown.
func TestPidReferencesWithoutTimeBaseText(t *testing.T) {
	s := newPidReferenceSummary(0, 188)
	s.usePcrOf([]*programAnalyzer{{pcrPid: nullPidValue}, {pcrPid: 0}})
	s.check.ObserveLayout(analysis.ReferenceLayout{Programs: []analysis.ReferencedProgram{{Number: 1, PMTPID: 0x1000, Known: true, PCRPID: nullPidValue}}})
	s.observeLead(&TsPacket{pid: 0x1000}, 0)
	s.observeLead(&TsPacket{pid: 0x0700}, 188)
	s.finish(376)
	out := captureStdout(t, s.Dump)
	want := strings.Join([]string{
		"-----------------------------",
		"Referenced PIDs: 1 reference, silence limit 5000 ms [INSUFFICIENT DATA]",
		"  program 1 PMT PID 0x1000 : insufficient data (no_pcr_time_base)",
		"Unreferenced PIDs: 0 PIDs, grace 500 ms [INSUFFICIENT DATA]",
		"  reason: no_pcr_time_base",
		"",
	}, "\n")
	if out != want {
		t.Errorf("text:\n%s\nwant:\n%s", out, want)
	}
	if roleText(analysis.RolePCR) != "PCR" {
		t.Error("PCR role text")
	}
}

func TestCaPidsAndNetworkPid(t *testing.T) {
	descriptors := []Descriptor{
		{tag: caDescriptorTag, data: []byte{0x0B, 0x00, 0xE4, 0x00}},
		{tag: caDescriptorTag, data: []byte{0x0B, 0x00, 0xE4}}, // too short
		{tag: 0x0A, data: []byte{0x0B, 0x00, 0xE5, 0x00}},
		{tag: caDescriptorTag, data: []byte{0x0B, 0x00, 0xFF, 0xFF, 0x01}},
	}
	if got := caPids(descriptors); len(got) != 2 || got[0] != 0x0400 || got[1] != 0x1FFF {
		t.Errorf("CA PIDs %v", got)
	}
	pat := &patTable{sections: []*Pat{{programInfo: []PatProgramInfo{{programNumber: 1, programMapPid: 0x1000}}}}}
	if pid := pat.networkPid(); pid != 0 {
		t.Errorf("network PID %#x", pid)
	}
}
