package tsparser

import (
	"strings"
	"testing"
)

// alignedColonCol returns the tab-expanded column of the ':' separator that
// follows the first tab on the line (the "label<tabs>: value" separator used by
// the dump functions). Lines without a tab-aligned separator return -1.
func alignedColonCol(line string) int {
	firstTab := strings.IndexByte(line, '\t')
	if firstTab < 0 {
		return -1
	}
	col := 0
	for i, r := range line {
		if r == '\t' {
			col += 8 - col%8
			continue
		}
		if r == ':' && i > firstTab {
			return col
		}
		col++
	}
	return -1
}

// assertAligned checks that every tab-aligned field line in lines puts its colon
// at the same column. It requires at least one such line.
func assertAligned(t *testing.T, name string, lines []string) {
	t.Helper()
	want := -1
	for _, line := range lines {
		col := alignedColonCol(line)
		if col < 0 {
			continue
		}
		if want < 0 {
			want = col
			continue
		}
		if col != want {
			t.Errorf("%s: colon column not aligned: expected %d, got %d in %q", name, want, col, expandTabs(line))
		}
	}
	if want < 0 {
		t.Errorf("%s: found no tab-aligned field lines", name)
	}
}

// TestDumpColonAlignment verifies that each dump keeps its "label : value" colons
// aligned in a single column. PMT is special-cased: its header fields and the
// per-stream "Program Info" lines are two independent column groups.
func TestDumpColonAlignment(t *testing.T) {
	// Adaptation Field with every flag set so all branches are printed.
	afData := []byte{0x1C, 0xFF, 0x00, 0x00, 0x00, 0x32, 0x7E, 0x32, 0x00, 0x00, 0x00, 0x32, 0x7E, 0x32,
		0xAB, 0x01, 0xDD, 0x0B, 0xE0, 0x92, 0x34, 0x01, 0x86, 0xA0, 0x35, 0x00, 0xC9, 0x01, 0x91}
	af := NewAdaptationField()
	af.Append(afData)
	if _, err := af.Parse(); err != nil {
		t.Fatalf("AF parse: %s", err)
	}
	assertAligned(t, "AdaptationField", dumpLines(t, af.Dump))

	// PAT
	pat := NewPat()
	pat.Append([]byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53})
	if err := pat.Parse(); err != nil {
		t.Fatalf("PAT parse: %s", err)
	}
	assertAligned(t, "PAT", dumpLines(t, pat.Dump))

	// TS header
	tsData := make([]byte, 188)
	tsData[0], tsData[1], tsData[2], tsData[3] = 0x47, 0x40, 0x00, 0x10
	tp := NewTsPacket()
	tp.Append(tsData)
	if err := tp.Parse(); err != nil {
		t.Fatalf("TS parse: %s", err)
	}
	assertAligned(t, "TsHeader", dumpLines(t, tp.DumpHeader))

	// PES header (with PTS/DTS)
	pes := NewPes()
	pes.Append([]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x84, 0xC0, 0x0A,
		0x31, 0x00, 0x01, 0xC7, 0x3F, 0x11, 0x00, 0x01, 0xAF, 0xC9})
	if err := pes.Parse(); err != nil {
		t.Fatalf("PES parse: %s", err)
	}
	assertAligned(t, "PES", dumpLines(t, pes.DumpHeader))

	// PMT with three elementary streams. The header fields and the Program Info
	// lines are separate column groups, so check them independently.
	pmt := NewPmt()
	pmt.Append([]byte{0x02, 0xB0, 0x1C, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00,
		0x1B, 0xE0, 0x31, 0xF0, 0x00, 0x0F, 0xE0, 0x64, 0xF0, 0x00, 0x0F, 0xE0, 0x98, 0xF0, 0x00,
		0x3D, 0xFE, 0xAE, 0x61, 0xFF})
	if err := pmt.Parse(); err != nil {
		t.Fatalf("PMT parse: %s", err)
	}
	var header, programInfo []string
	for _, line := range dumpLines(t, pmt.Dump) {
		if strings.Contains(line, "Program Info") {
			programInfo = append(programInfo, line)
		} else {
			header = append(header, line)
		}
	}
	assertAligned(t, "PMT header fields", header)
	assertAligned(t, "PMT Program Info", programInfo)
}

func dumpLines(t *testing.T, fn func()) []string {
	t.Helper()
	return strings.Split(captureStdout(t, fn), "\n")
}
