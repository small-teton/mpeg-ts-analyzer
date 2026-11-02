package tsparser

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// The multi-section fixtures are the two-program samples with the PAT split
// into one section per program and each PMT into one section per elementary
// stream, sent from the last section to section 0
// (tools/generate_multi_section_psi).
var multiSectionFixtures = []struct{ multi, single string }{
	{"../sample_data/sample_188byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts", twoPrograms188Path},
	{"../sample_data/sample_192byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts", twoPrograms192Path},
}

func TestMultiSectionFixtureMatchesSingleSectionSample(t *testing.T) {
	for _, fixture := range multiSectionFixtures {
		for _, opts := range []options.Options{
			{Format: "json", ListPrograms: true},
			{Format: "json", Program: 1},
			{Format: "json", Program: 2},
		} {
			multi, err := decodeJSONReport(t, func() error { return ParseTsFile(fixture.multi, opts) })
			if err != nil {
				t.Fatal(err)
			}
			single, err := decodeJSONReport(t, func() error { return ParseTsFile(fixture.single, opts) })
			if err != nil {
				t.Fatal(err)
			}
			if multi.Status != single.Status || len(multi.Programs) == 0 || !jsonEqual(t, multi.Programs, single.Programs) || len(multi.Warnings) != 0 {
				t.Errorf("%s %+v:\nmulti-section:  %+v\nsingle-section: %+v", fixture.multi, opts, multi, single)
			}
		}

		multi, err := decodeRangeAnalysis(t, fixture.multi, options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		single, err := decodeRangeAnalysis(t, fixture.single, options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if multi.Status != "complete" || len(multi.Programs) != 2 || len(multi.Warnings) != 0 {
			t.Fatalf("%s: analysis %+v", fixture.multi, multi)
		}
		for i := range multi.Programs {
			if !jsonEqual(t, multi.Programs[i].ReportProgram, single.Programs[i].ReportProgram) || multi.Programs[i].Series == nil {
				t.Errorf("%s: program %+v, want %+v", fixture.multi, multi.Programs[i].ReportProgram, single.Programs[i].ReportProgram)
			}
		}

		multiText := captureStdout(t, func() { _ = ParseTsFile(fixture.multi, options.Options{ListPrograms: true}) })
		singleText := captureStdout(t, func() { _ = ParseTsFile(fixture.single, options.Options{ListPrograms: true}) })
		if strings.SplitN(multiText, "\n", 2)[1] != strings.SplitN(singleText, "\n", 2)[1] {
			t.Errorf("%s: text listing differs:\n%s\nwant:\n%s", fixture.multi, multiText, singleText)
		}
	}
}

func TestMultiSectionFixtureDumpsEverySection(t *testing.T) {
	out := captureStdout(t, func() {
		_ = ParseTsFile(multiSectionFixtures[0].multi, options.Options{Program: 2, DumpPsi: true})
	})
	if !strings.Contains(out, "Detected PAT: 2 program(s)") || strings.Count(out, "last_section_number") != 4 || !strings.Contains(out, "PMT : Program Info : elementary_PID     : 0x103") {
		t.Errorf("unexpected dump:\n%s", out)
	}
	if strings.Count(out, "PAT : section_number") != 2 || strings.Count(out, "PMT : section_number") != 2 {
		t.Errorf("expected two PAT and two PMT sections:\n%s", out)
	}
}

func TestMultiSectionFixtureGenerator(t *testing.T) {
	for _, fixture := range multiSectionFixtures {
		path := filepath.Join(t.TempDir(), "fixture.ts")
		if output, err := exec.Command("go", "run", "../tools/generate_multi_section_psi", "-input", fixture.single, "-output", path).CombinedOutput(); err != nil {
			t.Fatalf("generate fixture: %v\n%s", err, output)
		}
		generated, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		committed, err := os.ReadFile(fixture.multi)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, committed) {
			t.Errorf("%s does not match the generator output", fixture.multi)
		}
	}
}

// diagnosticStream has a PAT whose section 1 first arrives with a bad CRC, a
// PMT of program 1 that never sends section 1, and a complete program 2.
func diagnosticStream(m2ts bool) string {
	badSection := patSectionN(0, 1, 1, [2]uint16{2, 0x1001})
	badSection[len(badSection)-1] ^= 0xFF
	pmt2 := pmtSectionN(2, 0, 0, 0, 0x200, [2]uint16{0x02, 0x200})
	packets := [][]byte{
		buildTsPacket(0x0000, true, 0, badSection),
		buildTsPacket(0x0000, true, 1, patSectionN(0, 0, 1, [2]uint16{1, 0x1000})),
		buildTsPacket(0x0000, true, 2, patSectionN(0, 1, 1, [2]uint16{2, 0x1001})),
		buildTsPacket(0x0000, true, 3, patSectionN(0, 0, 1, [2]uint16{1, 0x1000})),
		buildTsPacket(0x1000, true, 0, pmtSectionN(1, 0, 0, 1, 0x100, [2]uint16{0x02, 0x100})),
		buildTsPacket(0x1001, true, 0, pmt2),
		buildTsPacket(0x1001, true, 1, pmt2),
	}
	const ms = uint64(pcrTicksPerMs)
	for i := range 3 {
		pcr := 1000*ms + uint64(i)*40*ms
		packets = append(packets, buildPcrPacket(0x200, pcr), pesStartPacket(0x200, uint8(i), pcr/300+90*700))
	}
	var buf bytes.Buffer
	for _, packet := range packets {
		if m2ts {
			packet = wrapM2TS(packet)
		}
		buf.Write(packet)
	}
	return buf.String()
}

func writeDiagnosticStream(t *testing.T, m2ts bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "diagnostics.ts")
	if err := os.WriteFile(path, []byte(diagnosticStream(m2ts)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func warningMessages(warnings []ReportWarning) []string {
	var messages []string
	for _, w := range warnings {
		messages = append(messages, w.Message)
	}
	return messages
}

func TestPsiDiagnosticsInReports(t *testing.T) {
	const (
		badCRC     = "PAT section 1 (version 0) has an invalid CRC32; section ignored"
		missingPmt = "incomplete PMT of program 1 on PID 0x1000 (version 0): missing section(s) 1 of 0..1"
	)
	for _, m2ts := range []bool{false, true} {
		path := writeDiagnosticStream(t, m2ts)

		listing, err := decodeJSONReport(t, func() error { return ParseTsFile(path, options.Options{Format: "json", ListPrograms: true}) })
		if err != nil {
			t.Fatal(err)
		}
		if listing.Status != "programs_listed" || len(listing.Programs) != 2 || strings.Join(warningMessages(listing.Warnings), "|") != badCRC || listing.Warnings[0].OffsetBytes != 0 {
			t.Fatalf("m2ts=%v listing: %+v", m2ts, listing)
		}
		if p := listing.Programs[0]; p.Status != "incomplete" || p.Reason != "pmt_parse_failed" || p.Error != missingPmt {
			t.Errorf("m2ts=%v program 1: %+v", m2ts, p)
		}
		if p := listing.Programs[1]; p.Status != "complete" || p.PCRPID != "0x0200" || len(p.ElementaryStreams) != 1 {
			t.Errorf("m2ts=%v program 2: %+v", m2ts, p)
		}

		selected, err := decodeJSONReport(t, func() error { return ParseTsFile(path, options.Options{Format: "json", Program: 1}) })
		if err != nil {
			t.Fatal(err)
		}
		if selected.Status != "incomplete" || strings.Join(warningMessages(selected.Warnings), "|") != badCRC+"|"+missingPmt+"; retrying PAT discovery" {
			t.Errorf("m2ts=%v --program 1: %+v", m2ts, selected)
		}
		if selected, _ = decodeJSONReport(t, func() error { return ParseTsFile(path, options.Options{Format: "json", Program: 2}) }); selected.Status != "complete" {
			t.Errorf("m2ts=%v --program 2: %+v", m2ts, selected)
		}

		analysis, err := decodeRangeAnalysis(t, path, options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if analysis.Status != "complete" || len(analysis.Programs) != 2 || analysis.Programs[0].Error != missingPmt || analysis.Programs[1].Series == nil || strings.Join(warningMessages(analysis.Warnings), "|") != badCRC {
			t.Errorf("m2ts=%v analysis: %+v", m2ts, analysis)
		}

		text := captureStdout(t, func() { _ = ParseTsFile(path, options.Options{ListPrograms: true}) })
		for _, want := range []string{"0x00000000 " + badCRC + "\n", "Program 1: PMT PID 0x1000 (PMT parse error: " + missingPmt + ")\n", "Program 2: PMT PID 0x1001, PCR PID 0x200\n"} {
			if !strings.Contains(text, want) {
				t.Errorf("m2ts=%v: missing %q in:\n%s", m2ts, want, text)
			}
		}
	}
}

func TestIncompletePatIsNotUsed(t *testing.T) {
	section := patSectionN(0, 0, 1, [2]uint16{1, 0x1000})
	path := filepath.Join(t.TempDir(), "incomplete.ts")
	var data []byte
	for cc := range uint8(4) {
		data = append(data, buildTsPacket(0x0000, true, cc, section)...)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := decodeJSONReport(t, func() error { return ParseTsFile(path, options.Options{Format: "json"}) })
	if err == nil || report.Status != "failed" || len(report.Programs) != 0 ||
		strings.Join(warningMessages(report.Warnings), "|") != "incomplete PAT (version 0): missing section(s) 1 of 0..1; retrying PAT discovery" {
		t.Errorf("report %+v, err %v", report, err)
	}
	text := captureStdout(t, func() { _ = ParseTsFile(path, options.Options{}) })
	if !strings.Contains(text, "0x000002f0 incomplete PAT (version 0): missing section(s) 1 of 0..1, retrying PAT discovery...\n") {
		t.Errorf("text output:\n%s", text)
	}
}
