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

// The PSI health fixtures come from tools/generate_psi_health: 150 packets of
// one 20 ms slot each, two programs, PAT every 200 ms. Packet n is slot n.
var psiHealthFixtures = []struct {
	path       string
	packetSize int64
	m2ts       bool
}{
	{"../sample_data/sample_188byte_psi_health.ts", 188, false},
	{"../sample_data/sample_192byte_psi_health.ts", 192, true},
}

// psiTableWant pins one table of a scenario. Offsets are slots.
type psiTableWant struct {
	status                                             string
	reasons                                            []string
	instances, versionChanges                          int
	crc, malformed, incomplete, wrongTableID, unusable int
	firstProblem                                       int64 // slot, or -1 without problem
	repetition                                         string
	intervals, late                                    int
	maxMs                                              float64
	maxEnd                                             int64 // slot, or -1 without interval
}

var (
	healthyPAT  = psiTableWant{status: "ok", reasons: []string{}, instances: 17, firstProblem: -1, repetition: "ok", intervals: 13, maxMs: 200, maxEnd: 19}
	healthyPMT1 = psiTableWant{status: "ok", reasons: []string{}, instances: 16, firstProblem: -1, repetition: "ok", intervals: 13, maxMs: 200, maxEnd: 23}
	healthyPMT2 = psiTableWant{status: "ok", reasons: []string{}, instances: 15, firstProblem: -1, repetition: "ok", intervals: 14, maxMs: 200, maxEnd: 17}
	// Followed from the first PCR (slot 6) to the last (slot 148) and never
	// found.
	missingPMT2 = psiTableWant{status: "ng", reasons: []string{"missing"}, firstProblem: -1, repetition: "late", late: 1, maxMs: 2840, maxEnd: 148}
	latePAT     = psiTableWant{status: "ng", reasons: []string{"late"}, instances: 12, firstProblem: -1, repetition: "late", intervals: 8, late: 1, maxMs: 1200, maxEnd: 99}
)

// with returns a copy of w changed by f.
func (w psiTableWant) with(f func(*psiTableWant)) psiTableWant {
	f(&w)
	return w
}

var psiHealthScenarios = []struct {
	scenario string
	status   string
	segments int
	tables   [3]psiTableWant // PAT, PMT of program 1, PMT of program 2
}{
	{"healthy", "ok", 1, [3]psiTableWant{healthyPAT, healthyPMT1, healthyPMT2}},
	{"pat-late", "ng", 1, [3]psiTableWant{latePAT, healthyPMT1, healthyPMT2}},
	{"pat-stops", "ng", 1, [3]psiTableWant{
		{status: "ng", reasons: []string{"late"}, instances: 2, firstProblem: -1, repetition: "late", late: 1, maxMs: 2840, maxEnd: 148},
		healthyPMT1, healthyPMT2,
	}},
	{"pmt-missing", "ng", 1, [3]psiTableWant{healthyPAT, healthyPMT1, missingPMT2}},
	{"incomplete", "ng", 1, [3]psiTableWant{healthyPAT, healthyPMT1, missingPMT2.with(func(w *psiTableWant) {
		w.reasons, w.incomplete, w.firstProblem = []string{"missing", "incomplete"}, 1, 149
	})}},
	{"crc", "ng", 1, [3]psiTableWant{
		healthyPAT.with(func(w *psiTableWant) {
			w.status, w.reasons, w.instances, w.crc, w.firstProblem, w.intervals, w.maxMs, w.maxEnd = "ng", []string{"crc_error"}, 16, 1, 59, 12, 400, 69
		}),
		healthyPMT1.with(func(w *psiTableWant) {
			w.status, w.reasons, w.instances, w.crc, w.firstProblem, w.intervals, w.maxMs, w.maxEnd = "ng", []string{"crc_error"}, 15, 1, 63, 12, 400, 73
		}),
		healthyPMT2,
	}},
	{"wrong-table-id", "ng", 1, [3]psiTableWant{
		healthyPAT.with(func(w *psiTableWant) {
			w.status, w.reasons, w.instances, w.wrongTableID, w.firstProblem, w.intervals, w.maxMs, w.maxEnd = "ng", []string{"wrong_table_id"}, 16, 1, 59, 12, 400, 69
		}),
		healthyPMT1, healthyPMT2,
	}},
	{"version-change", "ok", 1, [3]psiTableWant{
		healthyPAT.with(func(w *psiTableWant) { w.versionChanges = 1 }),
		healthyPMT1.with(func(w *psiTableWant) { w.versionChanges = 1 }),
		healthyPMT2,
	}},
	{"no-pcr", "insufficient_data", 0, [3]psiTableWant{
		{status: "insufficient_data", reasons: []string{}, instances: 17, firstProblem: -1, repetition: "skipped", maxEnd: -1},
		{status: "insufficient_data", reasons: []string{}, instances: 16, firstProblem: -1, repetition: "skipped", maxEnd: -1},
		{status: "insufficient_data", reasons: []string{}, instances: 15, firstProblem: -1, repetition: "skipped", maxEnd: -1},
	}},
	// No interval spans the discontinuity at slot 70.
	{"discontinuity", "ok", 2, [3]psiTableWant{
		healthyPAT.with(func(w *psiTableWant) { w.intervals = 11 }),
		healthyPMT1.with(func(w *psiTableWant) { w.intervals = 12 }),
		healthyPMT2.with(func(w *psiTableWant) { w.intervals = 13 }),
	}},
	{"unusable", "ng", 1, [3]psiTableWant{
		healthyPAT.with(func(w *psiTableWant) {
			w.status, w.reasons, w.instances, w.unusable, w.firstProblem, w.intervals, w.maxMs, w.maxEnd = "ng", []string{"unusable_packets"}, 16, 1, 59, 12, 400, 69
		}),
		healthyPMT1.with(func(w *psiTableWant) {
			w.status, w.reasons, w.instances, w.unusable, w.firstProblem, w.intervals, w.maxMs, w.maxEnd = "ng", []string{"unusable_packets"}, 15, 1, 63, 12, 400, 73
		}),
		healthyPMT2,
	}},
	{"defects", "ng", 1, [3]psiTableWant{
		latePAT.with(func(w *psiTableWant) {
			w.reasons, w.wrongTableID, w.firstProblem = []string{"late", "wrong_table_id"}, 1, 59
		}),
		healthyPMT1.with(func(w *psiTableWant) {
			w.status, w.reasons, w.instances, w.versionChanges, w.crc, w.firstProblem, w.intervals, w.maxMs, w.maxEnd = "ng", []string{"crc_error"}, 15, 1, 1, 63, 12, 400, 73
		}),
		missingPMT2.with(func(w *psiTableWant) {
			w.reasons, w.incomplete, w.firstProblem = []string{"missing", "incomplete"}, 1, 149
		}),
	}},
}

// checkPsiHealth compares a psi_health result with a scenario.
func checkPsiHealth(t *testing.T, name string, h *ReportPSIHealth, packetSize int64, status string, segments int, tables [3]psiTableWant) {
	t.Helper()
	timeBase := "pcr"
	if segments == 0 {
		timeBase = "none"
	}
	if h == nil || h.Status != status || h.RepetitionLimitMs != 500 || h.TimeBase != (ReportPSITimeBase{Status: timeBase, ProgramNumber: 1, PCRPID: "0x0100", SegmentCount: segments}) || len(h.Tables) != 3 {
		t.Fatalf("%s: %s", name, toJSON(t, h))
	}
	offset := func(slot int64) *int64 {
		if slot < 0 {
			return nil
		}
		o := slot * packetSize
		return &o
	}
	for i, w := range tables {
		got := h.Tables[i]
		want := ReportPSITableHealth{
			Table: "pat", PID: "0x0000", Status: w.status, Reasons: w.reasons,
			InstanceCount: w.instances, VersionChangeCount: w.versionChanges,
			CRCErrorCount: w.crc, MalformedSectionCount: w.malformed, IncompleteCount: w.incomplete, WrongTableIDCount: w.wrongTableID, UnusablePacketCount: w.unusable,
			FirstProblemOffsetBytes: offset(w.firstProblem),
			Repetition:              ReportPSIRepetition{Status: w.repetition, IntervalCount: w.intervals, LateIntervalCount: w.late},
		}
		if w.instances > 0 {
			want.FirstInstanceOffsetBytes = offset(map[int]int64{0: 1, 1: 3, 2: 7}[i])
		}
		if i > 0 {
			number := uint16(i)
			want.Table, want.ProgramNumber, want.PID = "pmt", &number, []string{"", "0x1000", "0x1010"}[i]
			// Announced by the first PAT, completed in slot 1.
			want.AnnouncedOffsetBytes = offset(1)
		}
		switch w.repetition {
		case "skipped":
			want.Repetition.Reason = "no_pcr_time_base"
		case "insufficient_data":
			want.Repetition.Reason = "no_complete_interval"
		}
		if w.maxEnd >= 0 {
			want.Repetition.MaxIntervalMs = &tables[i].maxMs
			want.Repetition.MaxIntervalEndOffsetBytes = offset(w.maxEnd)
		}
		if w.late > 0 {
			want.Repetition.FirstLateEndOffsetBytes = offset(w.maxEnd)
		}
		if !jsonEqual(t, got, want) {
			t.Errorf("%s: table %d:\n got %s\nwant %s", name, i, toJSON(t, got), toJSON(t, want))
		}
	}
}

// buildPsiHealthGenerator builds tools/generate_psi_health once per test.
func buildPsiHealthGenerator(t *testing.T) func(args ...string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "generate_psi_health")
	if output, err := exec.Command("go", "build", "-o", bin, "../tools/generate_psi_health").CombinedOutput(); err != nil {
		t.Fatalf("build generator: %v\n%s", err, output)
	}
	count := 0
	return func(args ...string) string {
		count++
		path := filepath.Join(dir, fmt.Sprintf("fixture%d.ts", count))
		if output, err := exec.Command(bin, append([]string{"-output", path}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("generate %v: %v\n%s", args, err, output)
		}
		return path
	}
}

func TestPsiHealthScenarios(t *testing.T) {
	generate := buildPsiHealthGenerator(t)
	for _, sc := range psiHealthScenarios {
		for _, m2ts := range []bool{false, true} {
			args := []string{"-scenario", sc.scenario}
			packetSize := int64(188)
			if m2ts {
				args, packetSize = append(args, "-m2ts"), 192
			}
			name := fmt.Sprintf("%s/%d", sc.scenario, packetSize)
			report, err := decodeRangeAnalysis(t, generate(args...), options.Options{})
			if err != nil || report.Status != "complete" || report.Input.PacketSizeBytes != int(packetSize) {
				t.Fatalf("%s: %v %s", name, err, toJSON(t, report.Input))
			}
			checkPsiHealth(t, name, report.PSIHealth, packetSize, sc.status, sc.segments, sc.tables)
		}
	}
}

// The committed fixtures are the defects scenario. The summary of a single
// program reports the same tables, including the PMT of the other program.
func TestPsiHealthFixtures(t *testing.T) {
	generate := buildPsiHealthGenerator(t)
	defects := psiHealthScenarios[len(psiHealthScenarios)-1]
	for _, fixture := range psiHealthFixtures {
		var args []string
		if fixture.m2ts {
			args = append(args, "-m2ts")
		}
		generated, err := os.ReadFile(generate(args...))
		if err != nil {
			t.Fatal(err)
		}
		committed, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, committed) || len(committed) >= 32*1024 {
			t.Errorf("%s does not match the generator output (%d bytes)", fixture.path, len(committed))
		}

		analysis, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		checkPsiHealth(t, fixture.path+" analysis", analysis.PSIHealth, fixture.packetSize, defects.status, defects.segments, defects.tables)
		report := summary(t, fixture.path, options.Options{Program: 1})
		checkPsiHealth(t, fixture.path+" summary", report.PSIHealth, fixture.packetSize, defects.status, defects.segments, defects.tables)
		// The tracker warns about the CRC error once.
		if report.Status != "complete" || len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0].Message, "invalid CRC32") {
			t.Errorf("%s: %s", fixture.path, toJSON(t, report.Warnings))
		}
	}
}

func TestPsiHealthText(t *testing.T) {
	out := captureStdout(t, func() {
		if err := ParseTsFile(psiHealthFixtures[0].path, options.Options{Program: 1}); err != nil {
			t.Error(err)
		}
	})
	want := strings.Join([]string{
		"-----------------------------",
		"PAT/PMT health: NG (repetition limit 500 ms, ETSI TR 101 290 1.3.a/1.5.a; time base: PCR PID 0x0100 of program 1)",
		"  PAT PID 0x0000 : 12 tables, 8 intervals, max 1200.000 ms ending at 0x000048b4, first problem at 0x00002b54 [NG: late x1, wrong_table_id x1]",
		"  PMT program 1 PID 0x1000 : 15 tables, 12 intervals, max 400.000 ms ending at 0x0000359c, first problem at 0x00002e44 [NG: crc_error x1]",
		"  PMT program 2 PID 0x1010 : not found, 0 intervals, max 2840.000 ms ending at 0x00006cb0, first problem at 0x00006d6c [NG: missing, incomplete x1]",
		"",
	}, "\n")
	if !strings.HasSuffix(out, want) {
		t.Errorf("text summary:\n%s\nwant suffix:\n%s", out, want)
	}

	path := buildPsiHealthGenerator(t)("-scenario", "no-pcr")
	out = captureStdout(t, func() {
		if err := ParseTsFile(path, options.Options{Program: 2}); err != nil {
			t.Error(err)
		}
	})
	want = strings.Join([]string{
		"PAT/PMT health: INSUFFICIENT DATA (repetition limit 500 ms, ETSI TR 101 290 1.3.a/1.5.a; time base: no PCR time base)",
		"  PAT PID 0x0000 : 17 tables, no pcr time base [INSUFFICIENT DATA]",
		"  PMT program 1 PID 0x1000 : 16 tables, no pcr time base [INSUFFICIENT DATA]",
		"  PMT program 2 PID 0x1010 : 15 tables, no pcr time base [INSUFFICIENT DATA]",
		"",
	}, "\n")
	if !strings.HasSuffix(out, want) {
		t.Errorf("no PCR:\n%s", out)
	}

	// One table, and a short range without a complete interval.
	path = buildPsiHealthGenerator(t)("-scenario", "pat-stops")
	out = captureStdout(t, func() {
		if err := ParseTsFile(path, options.Options{Program: 1, Limit: 30 * 188}); err != nil {
			t.Error(err)
		}
	})
	for _, line := range []string{
		"  PAT PID 0x0000 : 2 tables, 0 intervals, max 440.000 ms ending at 0x00001490 [INSUFFICIENT DATA]\n",
		"  PMT program 1 PID 0x1000 : 4 tables, 1 interval, max 200.000 ms ending at 0x000010e4 [OK]\n",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("short range: want %q in\n%s", line, out)
		}
	}
	if !slices.ContainsFunc(strings.Split(out, "\n"), func(l string) bool { return strings.HasPrefix(l, "PAT/PMT health: INSUFFICIENT DATA") }) {
		t.Errorf("short range: %s", out)
	}
}

// psiChangeStream sends PCRs every 40 ms on 0x0100 and repeats the PAT and the
// PMTs every 200 ms. After 800 ms a PAT version 1 moves program 1's PMT to
// 0x1001, removes program 2 and adds program 3. Program 2's PMT is sent once
// more on its old PID, which is no longer read.
func psiChangeStream() (*trackingStream, int) {
	pat0 := patSectionN(0, 0, 0, [2]uint16{1, 0x1000}, [2]uint16{2, 0x1010})
	pat1 := patSectionN(1, 0, 0, [2]uint16{1, 0x1001}, [2]uint16{3, 0x1030})
	pmt := func(number uint16) []byte { return pmtSectionN(number, 0, 0, 0, 0x100, [2]uint16{0x02, 0x100}) }
	s := newTrackingStream().start(pat0, map[uint16][]byte{0x1000: pmt(1), 0x1010: pmt(2)}, 0x1000, 0x1010)
	changeAt := 0
	for i := range uint64(41) {
		s.pcr(0x100, i*40)
		switch {
		case i == 20:
			changeAt = len(s.packets)
			s.section(0x0000, pat1)
		case i%5 == 0 && i < 20:
			s.section(0x0000, pat0)
		case i%5 == 0:
			s.section(0x0000, pat1)
		case i%5 == 1 && i < 20:
			s.section(0x1000, pmt(1))
		case i%5 == 1:
			s.section(0x1001, pmt(1))
		case i%5 == 2 && i < 20:
			s.section(0x1010, pmt(2))
		case i == 22:
			s.section(0x1010, pmt(2))
			s.section(0x1030, pmt(3))
		case i%5 == 2:
			s.section(0x1030, pmt(3))
		}
	}
	return s, changeAt
}

func TestPsiHealthFollowsPatChanges(t *testing.T) {
	stream, changeAt := psiChangeStream()
	for _, m2ts := range []bool{false, true} {
		path := stream.write(t, m2ts)
		report, err := decodeRangeAnalysis(t, path, options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		h := report.PSIHealth
		change := stream.offset(changeAt, m2ts)
		if h == nil || h.Status != "ok" || len(h.Tables) != 4 || len(report.PSITransitions) != 1 {
			t.Fatalf("m2ts=%v: %s", m2ts, toJSON(t, h))
		}
		for i, want := range []struct {
			number    uint16
			pid       string
			withdrawn bool
			announced int64
		}{{1, "0x1001", false, stream.offset(0, m2ts)}, {2, "0x1010", true, stream.offset(0, m2ts)}, {3, "0x1030", false, change}} {
			got := h.Tables[i+1]
			if *got.ProgramNumber != want.number || got.PID != want.pid || got.Status != "ok" || *got.AnnouncedOffsetBytes != want.announced ||
				(got.WithdrawnOffsetBytes != nil) != want.withdrawn || want.withdrawn && *got.WithdrawnOffsetBytes != change {
				t.Errorf("m2ts=%v program %d: %s", m2ts, want.number, toJSON(t, got))
			}
		}
		if pat := h.Tables[0]; pat.VersionChangeCount != 1 || pat.Status != "ok" {
			t.Errorf("m2ts=%v PAT: %s", m2ts, toJSON(t, pat))
		}
	}
}

// A multi-section PMT whose last_section_number changes inside a version is
// discarded: incomplete for its program. A table seen once prints "1 table".
func TestPsiHealthDiscardedSections(t *testing.T) {
	pat := patSectionN(0, 0, 0, [2]uint16{1, 0x1000}, [2]uint16{2, 0x1010})
	pmt1 := pmtSectionN(1, 0, 0, 0, 0x100, [2]uint16{0x02, 0x100})
	s := newTrackingStream().start(pat, map[uint16][]byte{0x1000: pmt1}, 0x1000)
	s.pcr(0x100, 0).section(0x1010, pmtSectionN(2, 0, 0, 0, 0x100))
	s.pcr(0x100, 40).section(0x1010, pmtSectionN(2, 1, 0, 1, 0x100))
	s.pcr(0x100, 80).section(0x1010, pmtSectionN(2, 1, 1, 2, 0x100))
	s.pcr(0x100, 120)
	path := s.write(t, false)
	report := summary(t, path, options.Options{Program: 1})
	pmt2 := report.PSIHealth.Tables[2]
	if pmt2.InstanceCount != 1 || pmt2.IncompleteCount != 1 || !slices.Equal(pmt2.Reasons, []string{"incomplete"}) || *pmt2.FirstProblemOffsetBytes != s.offset(len(s.packets)-2, false) {
		t.Errorf("PMT 2: %s", toJSON(t, pmt2))
	}
	out := captureStdout(t, func() {
		if err := ParseTsFile(path, options.Options{Program: 1}); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "  PMT program 2 PID 0x1010 : 1 table, ") {
		t.Errorf("text: %s", out)
	}
}

// The time base splits at the same PCR steps as every other PCR-based result:
// on the PCR-discontinuities sample (a wraparound, two declared
// discontinuities, a PCR_PID change, a backward step and a forward jump), the
// segments equal those of the jitter estimate and the bitrate.
func TestPsiHealthFollowsPcrSteps(t *testing.T) {
	for _, fixture := range pcrDiscontinuitiesFixtures {
		report, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		p := report.Programs[0]
		if base := report.PSIHealth.TimeBase; base.Status != "pcr" || base.SegmentCount != 6 ||
			base.SegmentCount != p.PCRJitter.Estimate.SegmentCount || base.SegmentCount != p.Bitrate.SegmentCount {
			t.Errorf("%s: time base %s, jitter segments %d, bitrate segments %d", fixture.path, toJSON(t, base), p.PCRJitter.Estimate.SegmentCount, p.Bitrate.SegmentCount)
		}
	}
}
