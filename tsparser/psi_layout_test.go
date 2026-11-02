package tsparser

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// sectionSummary lists each logged section as
// "pid table status count first..last [version/section/last cni]", with
// offsets in packets.
func sectionSummary(sections []ReportPSISection, packetSize int64) string {
	var got []string
	for _, s := range sections {
		line := fmt.Sprintf("%s %s %s %d %d..%d", s.PID, s.Table, s.Status, s.Count, s.OffsetBytes/packetSize, s.LastOffsetBytes/packetSize)
		if s.ReportPSISectionHeader != nil {
			line += fmt.Sprintf(" v%d %d/%d cni%d", s.VersionNumber, s.SectionNumber, s.LastSectionNumber, s.CurrentNextIndicator)
		} else {
			line += " " + s.InvalidReason
		}
		got = append(got, line)
	}
	return strings.Join(got, "\n")
}

// layoutSummary lists each layout as "start..end [transitions] PAT vV@pos
// programs", with offsets in packets.
func layoutSummary(layouts []ReportPSILayoutEntry, packetSize int64) string {
	var got []string
	for _, l := range layouts {
		line := fmt.Sprintf("%d..%d %v PAT v%d@%d", l.OffsetBytes/packetSize, l.EndOffsetBytes/packetSize, l.TransitionIndices, l.PATVersion, l.PATOffsetBytes/packetSize)
		for _, p := range l.Programs {
			line += fmt.Sprintf(" | %d %s", p.ProgramNumber, p.PMTPID)
			if p.PMTVersion != nil {
				line += fmt.Sprintf(" v%d@%d PCR %s", *p.PMTVersion, *p.PMTOffsetBytes/packetSize, p.PCRPID)
				for _, es := range p.ElementaryStreams {
					line += fmt.Sprintf(" %s:%s", es.PID, es.StreamType)
				}
			}
		}
		got = append(got, line)
	}
	return strings.Join(got, "\n")
}

// pidSummary lists each observed PID as "pid count first..last roles", with
// offsets in packets.
func pidSummary(pids []ReportObservedPID, packetSize int64) string {
	var got []string
	for _, p := range pids {
		var roles []string
		for _, r := range p.Roles {
			role := r.Kind
			if r.ProgramNumber != nil {
				role += fmt.Sprintf("/%d", *r.ProgramNumber)
			}
			if r.StreamType != "" {
				role += "/" + r.StreamType
			}
			roles = append(roles, role)
		}
		got = append(got, fmt.Sprintf("%s %d %d..%d %s", p.PID, p.PacketCount, p.FirstOffsetBytes/packetSize, p.LastOffsetBytes/packetSize, strings.Join(roles, ",")))
	}
	return strings.Join(got, "\n")
}

func checkSummary(t *testing.T, name, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
	}
}

// TestPsiLayoutVersionChangeFixtures checks the layout of the fixtures with a
// PMT change in packet 545 and a PAT change in packet 1131. Packet counts per
// PID equal TSDuck's (tsanalyze --json).
func TestPsiLayoutVersionChangeFixtures(t *testing.T) {
	for _, fixture := range psiVersionChangeFixtures {
		packetSize := int64(188)
		if fixture.m2ts {
			packetSize = 192
		}
		report, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		layout := report.PSILayout
		if layout == nil || report.AnalysisEndOffsetBytes == nil || *report.AnalysisEndOffsetBytes != 1657*packetSize {
			t.Fatalf("%s: %+v", fixture.path, report)
		}
		checkSummary(t, fixture.path+" sections", sectionSummary(layout.Sections, packetSize), strings.Join([]string{
			"0x0000 pat applied 29 1..1093 v0 0/0 cni1",
			"0x1000 pmt applied 14 2..501 v0 0/0 cni1",
			"0x1000 pmt applied 15 545..1094 v1 0/0 cni1",
			"0x0000 pat applied 14 1131..1648 v1 0/0 cni1",
			"0x1100 pmt retransmission 14 1132..1649 v1 0/0 cni1",
		}, "\n"))
		checkSummary(t, fixture.path+" layouts", layoutSummary(layout.Layouts, packetSize), strings.Join([]string{
			"0..546 [] PAT v0@1 | 1 0x1000 v0@27 PCR 0x0100 0x0100:0x02 0x0101:0x03",
			"546..1132 [0] PAT v0@1 | 1 0x1000 v1@545 PCR 0x0200 0x0200:0x02 0x0101:0x04",
			"1132..1657 [1] PAT v1@1131 | 1 0x1100 v1@545 PCR 0x0200 0x0200:0x02 0x0101:0x04",
		}, "\n"))
		checkSummary(t, fixture.path+" PIDs", pidSummary(layout.PIDs, packetSize), strings.Join([]string{
			"0x0000 43 1..1648 pat",
			"0x0011 10 0..1522 unannounced",
			"0x0100 70 3..530 pcr/1,elementary_stream/1/0x02",
			"0x0101 1359 12..1656 elementary_stream/1/0x03,elementary_stream/1/0x04",
			"0x0200 132 546..1621 pcr/1,elementary_stream/1/0x02",
			"0x1000 29 2..1094 pmt/1",
			"0x1100 14 1132..1649 pmt/1",
		}, "\n"))
		for i, tr := range report.PSITransitions {
			if next := layout.Layouts[i+1]; next.OffsetBytes != tr.OffsetBytes+packetSize {
				t.Errorf("%s: layout %d starts at %d, transition at %d", fixture.path, i+1, next.OffsetBytes, tr.OffsetBytes)
			}
		}
	}
}

// TestPsiLayoutRangeNotStartingOnPsi analyzes ranges that start inside a
// packet, between PSI repetitions: the first layout starts at the first
// packet of the range and reports where the first complete PAT and PMT were.
func TestPsiLayoutRangeNotStartingOnPsi(t *testing.T) {
	for _, fixture := range psiVersionChangeFixtures {
		packetSize := int64(188)
		if fixture.m2ts {
			packetSize = 192
		}
		for _, tc := range []struct {
			first, limit  int64
			layouts, pids string
		}{
			{
				first: 110, limit: 300,
				layouts: "111..410 [] PAT v0@145 | 1 0x1000 v0@185 PCR 0x0100 0x0100:0x02 0x0101:0x03",
				pids:    "0x0000 7 145..386 pat\n0x0011 2 168..340 unannounced\n0x0100 36 117..402 pcr/1,elementary_stream/1/0x02\n0x0101 247 111..409 elementary_stream/1/0x03\n0x1000 7 146..387 pmt/1",
			},
			{
				first: 600, limit: 700,
				layouts: "601..1132 [] PAT v0@619 | 1 0x1000 v1@658 PCR 0x0200 0x0200:0x02 0x0101:0x04\n1132..1300 [0] PAT v1@1131 | 1 0x1100 v1@658 PCR 0x0200 0x0200:0x02 0x0101:0x04",
				pids:    "",
			},
		} {
			opts := options.Options{Offset: tc.first*packetSize + 50, Limit: tc.limit * packetSize}
			report, err := decodeRangeAnalysis(t, fixture.path, opts)
			if err != nil {
				t.Fatal(err)
			}
			layout := report.PSILayout
			checkSummary(t, fmt.Sprintf("%s from %d layouts", fixture.path, tc.first), layoutSummary(layout.Layouts, packetSize), tc.layouts)
			if tc.pids != "" {
				checkSummary(t, fmt.Sprintf("%s from %d PIDs", fixture.path, tc.first), pidSummary(layout.PIDs, packetSize), tc.pids)
			}
			var count int64
			for _, pid := range layout.PIDs {
				count += pid.PacketCount
			}
			if end := *report.AnalysisEndOffsetBytes; count != (end-layout.Layouts[0].OffsetBytes)/packetSize || layout.Layouts[len(layout.Layouts)-1].EndOffsetBytes != end {
				t.Errorf("%s from %d: %d packets up to %d", fixture.path, tc.first, count, end)
			}
		}
	}
}

// psiLayoutStream exercises the section log and the layout rebuild:
//   - a damaged packet, a PMT section and other PIDs before the first PAT;
//   - programs 1 and 2 sharing PMT PID 0x1000, program 4 without a PMT;
//   - broken sections, a "next" section and another table on the PMT PID;
//   - both PMTs changing in one packet, then late replays of version 0;
//   - a PAT removing program 2 and adding program 3, then listing program 2
//     again, and an incomplete PAT that is never applied.
func psiLayoutStream() *trackingStream {
	pat := patSectionN(0, 0, 0, [2]uint16{1, 0x1000}, [2]uint16{2, 0x1000}, [2]uint16{4, 0x1004})
	pmt1 := pmtSectionN(1, 0, 0, 0, 0x100, [2]uint16{0x02, 0x100})
	pmt2 := pmtSectionN(2, 0, 0, 0, 0x1FFF, [2]uint16{0x03, 0x101})
	s := newTrackingStream()
	s.packets = append(s.packets, make([]byte, 188))
	s.section(0x1000, pmt1)
	s.packets = append(s.packets, buildStuffingPacket())
	s.raw(0x0011, true, []byte{0x00, 0x42})
	s.section(0x0000, pat).section(0x0000, pat)
	for range 2 {
		s.section(0x1000, pmt1).section(0x1000, pmt2)
	}
	s.pcr(0x100, 0).pes(0x100, 0).pes(0x101, 0)

	badCRC := bytes.Clone(pmt1)
	badCRC[len(badCRC)-1] ^= 0xFF
	malformed := bytes.Clone(pat)
	malformed[1] &^= 0x80
	unparsable := testSection(0x02, 1, 2, 0, 0, true, []byte{0xE1, 0x00, 0xF0, 0x10})
	s.section(0x1000, badCRC).section(0x1000, badCRC).section(0x0000, malformed)
	s.section(0x0000, patSectionN(3, 2, 1, [2]uint16{1, 0x1000})).section(0x1000, unparsable)
	s.section(0x1000, testSection(0x02, 1, 1, 0, 0, false, []byte{0xE1, 0x00, 0xF0, 0x00}))
	s.section(0x1000, longSection(20))

	pmt1v1 := pmtSectionN(1, 1, 0, 0, 0x200, [2]uint16{0x02, 0x200})
	pmt2v1 := pmtSectionN(2, 1, 0, 0, 0x1FFF, [2]uint16{0x04, 0x101})
	s.raw(0x1000, true, append(append([]byte{0x00}, pmt1v1...), pmt2v1...))
	s.pcr(0x200, 40).pes(0x200, 40).pes(0x101, 40)
	s.section(0x1000, pmt1).section(0x1000, pmt1v1)

	s.section(0x0000, patSectionN(1, 0, 0, [2]uint16{1, 0x1000}, [2]uint16{3, 0x1003}))
	s.section(0x0000, patSectionN(2, 0, 0, [2]uint16{1, 0x1000}, [2]uint16{2, 0x1000}))
	s.section(0x0000, patSectionN(5, 0, 1, [2]uint16{1, 0x1000}))
	s.pcr(0x200, 80).pes(0x200, 80)
	return s
}

func TestPsiLayoutSyntheticStream(t *testing.T) {
	stream := psiLayoutStream()
	for _, m2ts := range []bool{false, true} {
		packetSize := int64(188)
		if m2ts {
			packetSize = 192
		}
		report, err := decodeRangeAnalysis(t, stream.write(t, m2ts), options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		layout := report.PSILayout
		if got := transitionSummary(report.PSITransitions); got != "pmt 0->1|pmt 0->1|pat 0->1|pat 1->2" {
			t.Fatalf("m2ts=%v transitions %s", m2ts, got)
		}
		checkSummary(t, "sections", sectionSummary(layout.Sections, packetSize), strings.Join([]string{
			"0x1000 pmt applied 3 1..8 v0 0/0 cni1",
			"0x0000 pat applied 2 4..5 v0 0/0 cni1",
			"0x1000 pmt applied 2 7..9 v0 0/0 cni1",
			"0x1000 pmt invalid 2 13..14 invalid_crc32",
			"0x0000 pat invalid 1 15..15 malformed",
			"0x0000 pat invalid 1 16..16 section_number_beyond_last",
			"0x1000 pmt invalid 1 17..17 parse_error",
			"0x1000 pmt not_applied 1 18..18 v1 0/0 cni0",
			"0x1000 pmt applied 1 20..20 v1 0/0 cni1",
			"0x1000 pmt applied 1 20..20 v1 0/0 cni1",
			"0x1000 pmt retransmission 1 24..24 v0 0/0 cni1",
			"0x1000 pmt retransmission 1 25..25 v1 0/0 cni1",
			"0x0000 pat applied 1 26..26 v1 0/0 cni1",
			"0x0000 pat applied 1 27..27 v2 0/0 cni1",
			"0x0000 pat not_applied 1 28..28 v5 0/1 cni1",
		}, "\n"))
		checkSummary(t, "layouts", layoutSummary(layout.Layouts, packetSize), strings.Join([]string{
			"0..21 [] PAT v0@4 | 1 0x1000 v0@6 PCR 0x0100 0x0100:0x02 | 2 0x1000 v0@7 PCR 0x1fff 0x0101:0x03 | 4 0x1004",
			"21..27 [0 1] PAT v0@4 | 1 0x1000 v1@20 PCR 0x0200 0x0200:0x02 | 2 0x1000 v1@20 PCR 0x1fff 0x0101:0x04 | 4 0x1004",
			"27..28 [2] PAT v1@26 | 1 0x1000 v1@20 PCR 0x0200 0x0200:0x02 | 3 0x1003",
			"28..31 [3] PAT v2@27 | 1 0x1000 v1@20 PCR 0x0200 0x0200:0x02 | 2 0x1000",
		}, "\n"))
		checkSummary(t, "PIDs", pidSummary(layout.PIDs, packetSize), strings.Join([]string{
			"0x0000 7 4..28 pat",
			"0x0011 1 3..3 unannounced",
			"0x0100 2 10..11 pcr/1,elementary_stream/1/0x02",
			"0x0101 2 12..23 elementary_stream/2/0x03,elementary_stream/2/0x04",
			"0x0200 4 21..30 pcr/1,elementary_stream/1/0x02",
			"0x1000 13 1..25 pmt/1,pmt/2",
			"0x1fff 1 2..2 null",
		}, "\n"))
		// Header fields are left out of invalid sections, and the PMT fields
		// of a program without a known PMT.
		if invalid := toJSON(t, layout.Sections[3]); strings.Contains(invalid, "version_number") {
			t.Errorf("invalid section: %s", invalid)
		}
		if unknown := toJSON(t, layout.Layouts[0].Programs[2]); unknown != `{"program_number":4,"pmt_pid":"0x1004","elementary_streams":[]}` {
			t.Errorf("program without PMT: %s", unknown)
		}
	}
}

func TestObserveLeadReadError(t *testing.T) {
	recorder := newPsiLayoutRecorder()
	err := observeLead(&errReadSeeker{data: make([]byte, 376), failAt: 0}, 0, 376, recorder, options.Options{}, 188)
	if err == nil || !strings.Contains(err.Error(), "file read error") {
		t.Errorf("read: err = %v", err)
	}
	err = observeLead(&errReadSeeker{data: make([]byte, 376), failAt: -1, seekFailAt: 1}, 0, 376, recorder, options.Options{}, 188)
	if err == nil || !strings.Contains(err.Error(), "file seek error") {
		t.Errorf("seek: err = %v", err)
	}
}

// TestPsiLayoutPmtBeforeItsPat sends the PMT on a new PID before the PAT that
// announces it, then again; both copies are logged. A table_id 0x02 section on
// a PID no applied PAT announced is not.
func TestPsiLayoutPmtBeforeItsPat(t *testing.T) {
	pmt0 := pmtSectionN(1, 0, 0, 0, 0x100, [2]uint16{0x02, 0x100})
	pmt1 := pmtSectionN(1, 1, 0, 0, 0x100, [2]uint16{0x02, 0x100}, [2]uint16{0x03, 0x101})
	s := newTrackingStream().start(patSectionN(0, 0, 0, [2]uint16{1, 0x1000}), map[uint16][]byte{0x1000: pmt0}, 0x1000)
	s.pcr(0x100, 0).pes(0x100, 0)
	s.section(0x1100, pmt1).section(0x1200, pmt1)
	s.section(0x0000, patSectionN(1, 0, 0, [2]uint16{1, 0x1100}))
	s.section(0x1100, pmt1)
	s.pcr(0x100, 40).pes(0x100, 40)
	for _, m2ts := range []bool{false, true} {
		packetSize := int64(188)
		if m2ts {
			packetSize = 192
		}
		report, err := decodeRangeAnalysis(t, s.write(t, m2ts), options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		checkSummary(t, "sections", sectionSummary(report.PSILayout.Sections, packetSize), strings.Join([]string{
			"0x0000 pat applied 2 0..1 v0 0/0 cni1",
			"0x1000 pmt applied 2 2..3 v0 0/0 cni1",
			"0x1100 pmt applied 2 6..9 v1 0/0 cni1",
			"0x0000 pat applied 1 8..8 v1 0/0 cni1",
		}, "\n"))
		checkSummary(t, "layouts", layoutSummary(report.PSILayout.Layouts, packetSize), strings.Join([]string{
			"0..9 [] PAT v0@0 | 1 0x1000 v0@2 PCR 0x0100 0x0100:0x02",
			"9..10 [0] PAT v1@8 | 1 0x1100 v0@2 PCR 0x0100 0x0100:0x02",
			"10..12 [1] PAT v1@8 | 1 0x1100 v1@9 PCR 0x0100 0x0100:0x02 0x0101:0x03",
		}, "\n"))
	}
}
