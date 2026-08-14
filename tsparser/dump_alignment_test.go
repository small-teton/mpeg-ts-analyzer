package tsparser

import (
	"strings"
	"testing"
)

// alignedColonCol returns the tab-expanded column of a line's "label : value"
// separator, i.e. the first ':' that is padded away from its label by two or
// more spaces. This handles both the tab-aligned dumps (PAT, PES, adaptation
// field, TS header) and the space-padded PMT dump, while ignoring embedded
// colons such as the "PMT :" prefix or "Program Info :" that are preceded by a
// single space. Lines without such a separator return -1.
func alignedColonCol(line string) int {
	exp := expandTabs(line)
	for i := 2; i < len(exp); i++ {
		if exp[i] == ':' && exp[i-1] == ' ' && exp[i-2] == ' ' {
			return i
		}
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

	// PMT: header fields, the per-stream Program Info lines and the descriptor
	// detail lines all share a single colon column, so one check covers them all.
	pmt := parsePmtWithDescriptor(t, 0x0F, []byte{0x0A, 0x04, 'e', 'n', 'g', 0x00})
	assertAligned(t, "PMT", dumpLines(t, pmt.Dump))
}

func dumpLines(t *testing.T, fn func()) []string {
	t.Helper()
	return strings.Split(captureStdout(t, fn), "\n")
}
