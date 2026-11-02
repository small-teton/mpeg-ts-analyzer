package tsparser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// trackingStream builds synthetic streams for PAT/PMT version tracking. Each
// PID gets its own continuity_counter sequence; PCR packets carry no payload,
// so they repeat the counter of the PID's previous packet.
type trackingStream struct {
	packets [][]byte
	last    map[uint16]uint8
}

func newTrackingStream() *trackingStream { return &trackingStream{last: map[uint16]uint8{}} }

func (s *trackingStream) next(pid uint16) uint8 {
	cc, seen := s.last[pid]
	if seen {
		cc = (cc + 1) & 0x0F
	}
	s.last[pid] = cc
	return cc
}

func (s *trackingStream) section(pid uint16, section []byte) *trackingStream {
	s.packets = append(s.packets, buildTsPacket(pid, true, s.next(pid), section))
	return s
}

func (s *trackingStream) pcr(pid uint16, ms uint64) *trackingStream {
	packet := buildPcrPacket(pid, 1000*uint64(pcrTicksPerMs)+ms*uint64(pcrTicksPerMs))
	cc := s.last[pid]
	s.last[pid] = cc
	packet[3] |= cc
	s.packets = append(s.packets, packet)
	return s
}

func (s *trackingStream) pes(pid uint16, ptsMs uint64) *trackingStream {
	s.packets = append(s.packets, pesStartPacket(pid, s.next(pid), 90*(1700+ptsMs)))
	return s
}

// skipCC makes the next packet of pid jump its continuity_counter.
func (s *trackingStream) skipCC(pid uint16) *trackingStream {
	s.last[pid] = (s.last[pid] + 5) & 0x0F
	return s
}

func (s *trackingStream) offset(index int, m2ts bool) int64 {
	if m2ts {
		return int64(index) * 192
	}
	return int64(index) * 188
}

func (s *trackingStream) write(t *testing.T, m2ts bool) string {
	t.Helper()
	var buf bytes.Buffer
	for _, packet := range s.packets {
		if m2ts {
			packet = wrapM2TS(packet)
		}
		buf.Write(packet)
	}
	path := filepath.Join(t.TempDir(), "tracking.ts")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// start sends the initial PAT and PMT twice, so acquisition ends before the
// analyzed packets.
func (s *trackingStream) start(pat []byte, pmts map[uint16][]byte, order ...uint16) *trackingStream {
	s.section(0x0000, pat).section(0x0000, pat)
	for range 2 {
		for _, pid := range order {
			s.section(pid, pmts[pid])
		}
	}
	return s
}

func summary(t *testing.T, path string, opts options.Options) AnalysisReport {
	t.Helper()
	opts.Format = "json"
	report, err := decodeJSONReport(t, func() error { return ParseTsFile(path, opts) })
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func toJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func esJSON(pid uint16, streamType uint8) string {
	return fmt.Sprintf(`{"pid":"0x%04x","stream_type":"0x%02x","stream_type_description":%q}`, pid, streamType, StreamTypeString(streamType))
}

// layoutChangeStream changes the PMT of program 1 to version 1 after six PCR
// periods: the PCR moves from 0x0100 to 0x0200, 0x0102 changes stream_type,
// 0x0103 is added and 0x0104 removed. The old PCR PID and the removed PID keep
// sending packets, and 0x0102 restarts its timestamps.
func layoutChangeStream() (*trackingStream, int) {
	s := newTrackingStream().start(patSection([2]uint16{1, 0x1000}), map[uint16][]byte{
		0x1000: pmtSectionN(1, 0, 0, 0, 0x100, [2]uint16{0x02, 0x101}, [2]uint16{0x03, 0x102}, [2]uint16{0x06, 0x104}),
	}, 0x1000)
	for i := range uint64(6) {
		s.pcr(0x100, i*40).pes(0x101, i*40).pes(0x102, i*40).pes(0x104, i*40)
	}
	changeAt := len(s.packets)
	s.section(0x1000, pmtSectionN(1, 1, 0, 0, 0x200, [2]uint16{0x02, 0x101}, [2]uint16{0x04, 0x102}, [2]uint16{0x03, 0x103}))
	for i := range uint64(6) {
		ms := 240 + i*40
		s.pcr(0x100, ms+5).pcr(0x200, ms).pes(0x101, ms).pes(0x102, i*40).pes(0x103, ms).pes(0x104, ms)
	}
	return s, changeAt
}

func TestPsiTrackingAppliesPmtChange(t *testing.T) {
	stream, changeAt := layoutChangeStream()
	for _, m2ts := range []bool{false, true} {
		path := stream.write(t, m2ts)
		report := summary(t, path, options.Options{DumpBitrate: true, DumpPcrJitter: true})
		if report.Status != "complete" || len(report.PSITransitions) != 1 || len(report.Warnings) != 0 {
			t.Fatalf("m2ts=%v report: %+v", m2ts, report)
		}
		want := fmt.Sprintf(`{"offset_bytes":%d,"table":"pmt","pid":"0x1000","previous_version":0,"version":1,"program_number":1,"previous_pcr_pid":"0x0100","pcr_pid":"0x0200",`+
			`"elementary_streams":[%s,%s,%s],"elementary_streams_added":[%s],"elementary_streams_removed":[%s],`+
			`"stream_type_changes":[{"pid":"0x0102","previous_stream_type":"0x03","stream_type":"0x04","stream_type_description":%q}]}`,
			stream.offset(changeAt, m2ts), esJSON(0x101, 0x02), esJSON(0x102, 0x04), esJSON(0x103, 0x03), esJSON(0x103, 0x03), esJSON(0x104, 0x06), StreamTypeString(0x04))
		if got := toJSON(t, report.PSITransitions[0]); got != want {
			t.Errorf("m2ts=%v transition:\n got %s\nwant %s", m2ts, got, want)
		}
		// Five intervals on each PCR PID; none across the change.
		if c := report.Compliance.MaxPCRInterval; c.Observations != 10 || *c.ValueMs != 40 {
			t.Errorf("m2ts=%v PCR interval: %+v", m2ts, c)
		}
		jitter := report.PCRJitter
		if jitter.SampleCount != 12 || jitter.SegmentCount != 2 || len(jitter.Discontinuities) != 1 || jitter.Discontinuities[0].OffsetBytes != stream.offset(changeAt+2, m2ts) {
			t.Errorf("m2ts=%v jitter: %+v", m2ts, jitter)
		}
		bitrate := report.Bitrate
		if bitrate.SegmentCount != 2 || bitrate.DurationSeconds != 0.4 {
			t.Errorf("m2ts=%v bitrate: %+v", m2ts, bitrate)
		}
		labels := map[string]string{}
		for _, row := range bitrate.PIDs {
			labels[row.PID] = row.Label
		}
		if labels["0x0100"] != "PCR" || labels["0x0102"] != StreamTypeString(0x04) || labels["0x0104"] != StreamTypeString(0x06) || labels["0x0103"] != StreamTypeString(0x03) || labels["0x0200"] != "PCR" {
			t.Errorf("m2ts=%v bitrate labels: %v", m2ts, labels)
		}
		// 0x0102 restarted its timestamps with the new stream type: no
		// backward step is reported across the change.
		if report.ContinuityCounter.Status != "ok" || report.TimestampAnomalies.Status != "ok" {
			t.Errorf("m2ts=%v continuity %+v anomalies %+v", m2ts, report.ContinuityCounter, report.TimestampAnomalies)
		}

		analysis, err := decodeRangeAnalysis(t, path, options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if toJSON(t, analysis.PSITransitions) != toJSON(t, report.PSITransitions) || analysis.Programs[0].AnalysisEndOffsetBytes != nil {
			t.Errorf("m2ts=%v analysis: %+v", m2ts, analysis)
		}
		var breaks int
		for _, p := range analysis.Programs[0].Series.PCR {
			if p.Discontinuity {
				breaks++
			}
		}
		if breaks != 1 {
			t.Errorf("m2ts=%v PCR series breaks = %d", m2ts, breaks)
		}
	}
}

func TestPsiTrackingTextOutput(t *testing.T) {
	stream, changeAt := layoutChangeStream()
	path := stream.write(t, false)
	text := captureStdout(t, func() {
		if err := ParseTsFile(path, options.Options{DumpPsi: true}); err != nil {
			t.Error(err)
		}
	})
	want := fmt.Sprintf("0x%08x PMT of program 1 on PID 0x1000 version 0 -> 1: PCR_PID 0x0100 -> 0x0200, added 0x0103 (stream_type 0x03), removed 0x0104 (stream_type 0x06), stream_type of 0x0102 0x03 -> 0x04\n", stream.offset(changeAt, false))
	if !strings.Contains(text, want) || strings.Count(text, "PMT : version_number                    : 1") != 1 {
		t.Errorf("missing transition or new PMT dump in:\n%s", text)
	}
	if text = captureStdout(t, func() { _ = ParseTsFile(path, options.Options{}) }); !strings.Contains(text, want) || strings.Contains(text, "PMT : version_number") {
		t.Errorf("text without --dump-psi:\n%s", text)
	}
}

// moveStream lists programs 1 and 2, then a PAT version 1 moves program 1's
// PMT to 0x1100, removes program 2 and adds program 3. On 0x1100 the PMT keeps
// version 1 but adds a stream; the old PMT PID then sends a version that must
// be ignored. A PAT version 2 changes nothing.
func moveStream() (s *trackingStream, patChange, pmtChange, pat2 int) {
	pmt1 := pmtSectionN(1, 0, 0, 0, 0x100, [2]uint16{0x02, 0x100})
	pmt2 := pmtSectionN(2, 0, 0, 0, 0x200, [2]uint16{0x02, 0x200})
	s = newTrackingStream().start(patSection([2]uint16{1, 0x1000}, [2]uint16{2, 0x1001}), map[uint16][]byte{0x1000: pmt1, 0x1001: pmt2}, 0x1000, 0x1001)
	for i := range uint64(4) {
		s.pcr(0x100, i*40).pes(0x100, i*40).pcr(0x200, i*40).pes(0x200, i*40)
	}
	// Program 1 changes while program 2 still uses 0x0200, so that PID
	// keeps its continuity state.
	s.section(0x1000, pmtSectionN(1, 1, 0, 0, 0x100, [2]uint16{0x02, 0x100}, [2]uint16{0x02, 0x200}))
	patChange = len(s.packets)
	s.section(0x0000, patSectionN(1, 0, 0, [2]uint16{1, 0x1100}, [2]uint16{3, 0x1003}))
	s.section(0x1001, pmt2)
	pmtChange = len(s.packets)
	s.section(0x1100, pmtSectionN(1, 1, 0, 0, 0x100, [2]uint16{0x02, 0x100}, [2]uint16{0x02, 0x200}, [2]uint16{0x03, 0x101}))
	s.section(0x1100, pmtSectionN(1, 1, 0, 0, 0x100, [2]uint16{0x02, 0x100}, [2]uint16{0x02, 0x200}, [2]uint16{0x03, 0x101}))
	s.section(0x1000, pmtSectionN(1, 7, 0, 0, 0x1FFF))
	for i := range uint64(3) {
		s.pcr(0x100, 160+i*40).pes(0x100, 160+i*40).pcr(0x200, 160+i*40).pes(0x200, 160+i*40)
	}
	pat2 = len(s.packets)
	s.section(0x0000, patSectionN(2, 0, 0, [2]uint16{1, 0x1100}, [2]uint16{3, 0x1003}))
	return s, patChange, pmtChange, pat2
}

func TestPsiTrackingFollowsPat(t *testing.T) {
	stream, patChange, pmtChange, pat2 := moveStream()
	for _, m2ts := range []bool{false, true} {
		path := stream.write(t, m2ts)
		analysis, err := decodeRangeAnalysis(t, path, options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, tr := range analysis.PSITransitions {
			got = append(got, fmt.Sprintf("%s@%d %d->%d", tr.Table, tr.OffsetBytes, tr.PreviousVersion, tr.Version))
		}
		want := []string{
			fmt.Sprintf("pmt@%d 0->1", stream.offset(patChange-1, m2ts)),
			fmt.Sprintf("pat@%d 0->1", stream.offset(patChange, m2ts)),
			fmt.Sprintf("pmt@%d 1->1", stream.offset(pmtChange, m2ts)),
			fmt.Sprintf("pat@%d 1->2", stream.offset(pat2, m2ts)),
		}
		if strings.Join(got, "|") != strings.Join(want, "|") || len(analysis.Warnings) != 0 {
			t.Fatalf("m2ts=%v transitions %v, want %v; warnings %v", m2ts, got, want, analysis.Warnings)
		}
		pat := toJSON(t, analysis.PSITransitions[1])
		if !strings.Contains(pat, `"programs":[{"program_number":1,"pmt_pid":"0x1100"},{"program_number":3,"pmt_pid":"0x1003"}],"programs_added":[{"program_number":3,"pmt_pid":"0x1003"}],"programs_removed":[{"program_number":2,"pmt_pid":"0x1001"}],"pmt_pid_changes":[{"program_number":1,"previous_pmt_pid":"0x1000","pmt_pid":"0x1100"}]`) {
			t.Errorf("m2ts=%v PAT transition: %s", m2ts, pat)
		}
		if moved := analysis.PSITransitions[2]; moved.PID != "0x1100" || moved.PreviousPCRPID != "0x0100" || len(moved.ElementaryStreamsAdded) != 1 {
			t.Errorf("m2ts=%v moved PMT: %+v", m2ts, moved.ReportPMTTransition)
		}
		if p := analysis.Programs[1]; p.AnalysisEndOffsetBytes == nil || *p.AnalysisEndOffsetBytes != stream.offset(patChange+1, m2ts) {
			t.Errorf("m2ts=%v program 2: %+v", m2ts, p)
		}
		if analysis.ContinuityCounter.Status != "ok" {
			t.Errorf("m2ts=%v continuity: %+v", m2ts, analysis.ContinuityCounter)
		}

		selected := summary(t, path, options.Options{Program: 2})
		if len(selected.PSITransitions) != 2 || selected.PSITransitions[0].Table != "pat" || selected.Status != "complete" {
			t.Errorf("m2ts=%v --program 2: %+v", m2ts, selected.PSITransitions)
		}
		text := captureStdout(t, func() { _ = ParseTsFile(path, options.Options{Program: 2}) })
		for _, line := range []string{
			fmt.Sprintf("0x%08x PAT version 0 -> 1: program 1 PMT PID 0x1000 -> 0x1100, added program 3 (PMT PID 0x1003), removed program 2 (PMT PID 0x1001)\n", stream.offset(patChange, m2ts)),
			fmt.Sprintf("0x%08x PAT version 1 -> 2: no program change\n", stream.offset(pat2, m2ts)),
		} {
			if !strings.Contains(text, line) {
				t.Errorf("m2ts=%v missing %q in:\n%s", m2ts, line, text)
			}
		}
	}
}

// candidateStream sends PMT and PAT candidates that must not be applied, then
// a valid two-section PMT version 4 out of order with a retransmission.
func candidateStream(firstSection, secondSection byte) (*trackingStream, int) {
	pmt := func(version, number, last byte, pcrPid uint16, streams ...[2]uint16) []byte {
		return pmtSectionN(1, version, number, last, pcrPid, streams...)
	}
	s := newTrackingStream().start(patSection([2]uint16{1, 0x1000}), map[uint16][]byte{0x1000: pmt(0, 0, 0, 0x100, [2]uint16{0x02, 0x100})}, 0x1000)
	badCRC := pmt(3, 0, 0, 0x100, [2]uint16{0x02, 0x100})
	badCRC[len(badCRC)-1] ^= 0xFF
	s.pcr(0x100, 0).pes(0x100, 0)
	s.section(0x1000, pmt(1, 1, 1, 0x100, [2]uint16{0x03, 0x101}))                                                              // incomplete
	s.section(0x1000, testSection(0x02, 1, 2, 0, 0, false, []byte{0xE1, 0x01, 0xF0, 0x00}))                                     // next table
	s.section(0x1000, badCRC).section(0x1000, badCRC)                                                                           // invalid
	s.section(0x1000, pmt(0, 0, 0, 0x100, [2]uint16{0x02, 0x101})).section(0x1000, pmt(0, 0, 0, 0x100, [2]uint16{0x02, 0x101})) // same version, new content
	s.section(0x0000, patSectionN(0, 0, 0, [2]uint16{1, 0x1000}, [2]uint16{9, 0x1009}))                                         // same version, new content
	s.section(0x0000, patSectionN(1, 0, 1, [2]uint16{1, 0x1000}))                                                               // incomplete PAT
	s.pcr(0x100, 40).pes(0x100, 40)
	sections := map[byte][]byte{
		0: pmt(4, 0, 1, 0x100, [2]uint16{0x02, 0x100}),
		1: pmt(4, 1, 1, 0x101, [2]uint16{0x03, 0x101}),
	}
	s.section(0x1000, sections[firstSection]).section(0x1000, sections[firstSection])
	completedAt := len(s.packets)
	s.section(0x1000, sections[secondSection])
	s.section(0x1000, sections[1]).section(0x1000, sections[0])
	s.pcr(0x100, 80).pes(0x100, 80).pes(0x101, 80).pcr(0x100, 120)
	// A second incomplete PMT version, and a PAT version 2 whose sections
	// disagree about program 1.
	s.section(0x1000, pmt(5, 1, 1, 0x100, [2]uint16{0x03, 0x101}))
	s.section(0x0000, patSectionN(2, 1, 1, [2]uint16{1, 0x1001})).section(0x0000, patSectionN(2, 0, 1, [2]uint16{1, 0x1000}))
	return s, completedAt
}

func TestPsiTrackingIgnoresCandidates(t *testing.T) {
	var transitions []string
	for _, order := range [][2]byte{{1, 0}, {0, 1}} {
		stream, completedAt := candidateStream(order[0], order[1])
		for _, m2ts := range []bool{false, true} {
			path := stream.write(t, m2ts)
			report := summary(t, path, options.Options{})
			if len(report.PSITransitions) != 2 || report.PSITransitions[0].OffsetBytes != stream.offset(completedAt, m2ts) || report.PSITransitions[1].Table != "pat" {
				t.Fatalf("order %v m2ts=%v transitions: %+v", order, m2ts, report.PSITransitions)
			}
			tr := report.PSITransitions[0]
			tr.OffsetBytes = 0
			transitions = append(transitions, toJSON(t, tr))
			end := stream.offset(len(stream.packets), m2ts)
			want := []string{
				"PMT of program 1 on PID 0x1000 section 0 (version 3) has an invalid CRC32; section ignored",
				"PMT of program 1 on PID 0x1000 changed without a version change (version 0); change ignored",
				"PAT changed without a version change (version 0); change ignored",
				"PMT of program 1 has PCR_PID 0x100 in section 0 and 0x101 in section 1; using 0x100",
				"PAT lists program 1 with PMT PIDs 0x1000 and 0x1001; using 0x1000",
				"incomplete PAT (version 1) at the end of the analysis: missing section(s) 1 of 0..1; not applied",
				"incomplete PMT of program 1 on PID 0x1000 (version 1) at the end of the analysis: missing section(s) 0 of 0..1; not applied",
				"incomplete PMT of program 1 on PID 0x1000 (version 5) at the end of the analysis: missing section(s) 0 of 0..1; not applied",
			}
			if got := warningMessages(report.Warnings); strings.Join(got, "\n") != strings.Join(want, "\n") || report.Warnings[7].OffsetBytes != end {
				t.Errorf("order %v m2ts=%v warnings:\n%s", order, m2ts, strings.Join(got, "\n"))
			}
		}
	}
	stream, _ := candidateStream(1, 0)
	text := captureStdout(t, func() { _ = ParseTsFile(stream.write(t, false), options.Options{}) })
	for _, want := range []string{
		"0x000005e0 PMT of program 1 on PID 0x1000 section 0 (version 3) has an invalid CRC32; section ignored\n",
		"PAT version 0 -> 2: no program change\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	for _, tr := range transitions[1:] {
		if tr != transitions[0] {
			t.Errorf("transitions differ:\n%s\n%s", transitions[0], tr)
		}
	}
	if !strings.Contains(transitions[0], `"previous_version":0,"version":4`) || !strings.Contains(transitions[0], `"elementary_streams_added":[`+esJSON(0x101, 0x03)+`]`) {
		t.Errorf("transition: %s", transitions[0])
	}
}

// TestPsiTrackingRestartsContinuity removes 0x0103, adds it back with a
// continuity_counter that does not follow, and moves the PCR to "no PCR".
func TestPsiTrackingRestartsContinuity(t *testing.T) {
	s := newTrackingStream().start(patSection([2]uint16{1, 0x1000}), map[uint16][]byte{
		0x1000: pmtSectionN(1, 0, 0, 0, 0x100, [2]uint16{0x02, 0x100}, [2]uint16{0x03, 0x103}),
	}, 0x1000)
	s.pcr(0x100, 0).pes(0x100, 0).pes(0x103, 0)
	s.section(0x1000, pmtSectionN(1, 1, 0, 0, 0x100, [2]uint16{0x02, 0x100}))
	s.pcr(0x100, 40).pes(0x100, 40)
	s.section(0x1000, pmtSectionN(1, 2, 0, 0, 0x1FFF, [2]uint16{0x02, 0x100}, [2]uint16{0x03, 0x103}))
	s.skipCC(0x103).pes(0x103, 80).pes(0x103, 120).pcr(0x100, 80)
	s.section(0x1000, pmtSectionN(1, 3, 0, 0, 0x1FFF, [2]uint16{0x02, 0x100}, [2]uint16{0x03, 0x103}))
	for _, m2ts := range []bool{false, true} {
		report := summary(t, s.write(t, m2ts), options.Options{DumpBitrate: true})
		if len(report.PSITransitions) != 3 || report.ContinuityCounter.Status != "ok" || report.Compliance.MaxPCRInterval.Observations != 1 {
			t.Errorf("m2ts=%v report: %+v", m2ts, report)
		}
	}
	text := captureStdout(t, func() { _ = ParseTsFile(s.write(t, false), options.Options{}) })
	if !strings.Contains(text, "version 2 -> 3: no stream change\n") {
		t.Errorf("text:\n%s", text)
	}
}
