package analysis

import (
	"testing"
)

// The streams below have 10 packets per 100 ms PCR interval: packet i is at
// i × 10 ms. tick advances one interval with the PCR first.

const (
	pcrPID   = 0x0100
	audioPID = 0x0101
	dataPID  = 0x0102
	pmtPID   = 0x1000
)

// program is program 1 with its PMT, the PCR on the video PID and the given
// extra streams.
func program(streams ...ElementaryStream) ReferencedProgram {
	all := append([]ElementaryStream{{PID: pcrPID, StreamType: 0x02}}, streams...)
	return ReferencedProgram{Number: 1, PMTPID: pmtPID, Known: true, PCRPID: pcrPID, Streams: all}
}

// tick observes one PCR interval: the PCR, then the given packets, then null
// packets up to 10 packets.
func (s *stream) tick(pids ...uint16) {
	s.pcr(float64(s.index)*10, false)
	s.packets(pids...)
	s.fill(nullPID, 9-len(pids))
}

func rowOf(t *testing.T, r PIDReferenceResult, program uint16, role ReferenceRole, pid uint16) ReferencedPID {
	t.Helper()
	for _, row := range r.Referenced.PIDs {
		if row.Program == program && row.Role == role && row.PID == pid {
			return row
		}
	}
	t.Fatalf("no row %d %v 0x%04x in %+v", program, role, pid, r.Referenced.PIDs)
	return ReferencedPID{}
}

func TestReferencedPIDStatuses(t *testing.T) {
	s := newStream()
	s.layout(program(
		ElementaryStream{PID: audioPID, StreamType: 0x03}, // silent
		ElementaryStream{PID: dataPID, StreamType: 0x06},  // never sent, not judged
		ElementaryStream{PID: 0x0103, StreamType: 0x04},   // absent
		ElementaryStream{PID: 0x0104, StreamType: 0x06},   // private data with packets
		ElementaryStream{PID: nullPID, StreamType: 0x03},  // the null PID is never a reference
		ElementaryStream{PID: 0x0105, StreamType: 0x1B},   // removed after 1 s without packets
		ElementaryStream{PID: 0x0106, StreamType: 0x0F},   // removed after its last packet
	))
	for i := range 80 { // 8 s
		switch {
		case i < 10:
			s.tick(pmtPID, audioPID, 0x0104, 0x0106)
		case i < 70: // audio silent for 6 s
			s.tick(pmtPID)
		default:
			s.tick(pmtPID, audioPID)
		}
		if i == 9 {
			s.layout(program(
				ElementaryStream{PID: audioPID, StreamType: 0x03},
				ElementaryStream{PID: dataPID, StreamType: 0x06},
				ElementaryStream{PID: 0x0103, StreamType: 0x04},
				ElementaryStream{PID: 0x0104, StreamType: 0x06},
			))
		}
	}
	s.finish()
	r := s.check.Result()
	if r.Referenced.Status != PIDStatusAbsent {
		t.Errorf("status %v", r.Referenced.Status)
	}
	end := s.offset()

	pmt := rowOf(t, r, 1, RolePMT, pmtPID)
	if pmt.Status != PIDStatusOK || !pmt.Judged || pmt.Packets != 80 || pmt.ReferencedPackets != 80 || pmt.ReferencedFrom != 0 || pmt.ReferencedUntil != end ||
		!pmt.MaxSilenceKnown || !pmt.MaxSilenceExact || !near(pmt.MaxSilenceMs, 100) {
		t.Errorf("PMT %+v", pmt)
	}
	if pcr := rowOf(t, r, 1, RolePCR, pcrPID); pcr.Status != PIDStatusOK || pcr.Packets != 80 {
		t.Errorf("PCR %+v", pcr)
	}
	audio := rowOf(t, r, 1, RoleElementaryStream, audioPID)
	// Last audio packet in interval 9 (index 92), next in interval 70 (index 702).
	if audio.Status != PIDStatusSilent || audio.StreamType != 0x03 || !near(audio.MaxSilenceMs, 6100) ||
		audio.MaxSilenceStartOffset != 92*188 || audio.MaxSilenceEndOffset != 702*188 || audio.ReferencedFirstOffset != 2*188 || audio.ReferencedLastOffset != 792*188 {
		t.Errorf("audio %+v", audio)
	}
	if data := rowOf(t, r, 1, RoleElementaryStream, dataPID); data.Status != PIDStatusInsufficientData || data.Reason != ReasonIntermittentStreamType || data.Judged {
		t.Errorf("data %+v", data)
	}
	if private := rowOf(t, r, 1, RoleElementaryStream, 0x0104); private.Status != PIDStatusOK || private.Judged || !near(private.MaxSilenceMs, 6970) || private.MaxSilenceStartOffset != 93*188 {
		t.Errorf("private %+v", private)
	}
	absent := rowOf(t, r, 1, RoleElementaryStream, 0x0103)
	if absent.Status != PIDStatusAbsent || absent.Packets != 0 || absent.FirstOffset != 0 || !near(absent.MaxSilenceMs, 7900) {
		t.Errorf("absent %+v", absent)
	}
	// Referenced for 1 s only, without packets.
	short := rowOf(t, r, 1, RoleElementaryStream, 0x0105)
	if short.Status != PIDStatusInsufficientData || short.Reason != ReasonReferenceShorterThanLimit || short.ReferencedUntil != 100*188 {
		t.Errorf("short %+v", short)
	}
	if removed := rowOf(t, r, 1, RoleElementaryStream, 0x0106); removed.Status != PIDStatusOK || !near(removed.MaxSilenceMs, 100) {
		t.Errorf("removed %+v", removed)
	}
	for _, row := range r.Referenced.PIDs {
		if row.PID == nullPID {
			t.Errorf("null PID row %+v", row)
		}
	}
	if r.Unreferenced.Status != PIDStatusOK {
		t.Errorf("unreferenced %+v", r.Unreferenced)
	}
}

func TestReferencedPIDOrderAndSharedRoles(t *testing.T) {
	s := newStream()
	second := ReferencedProgram{Number: 2, PMTPID: 0x1001, Known: true, PCRPID: 0x0200, Streams: []ElementaryStream{{PID: 0x0200, StreamType: 0x1B}}}
	noPCR := ReferencedProgram{Number: 3, PMTPID: 0x1002, Known: true, PCRPID: nullPID}
	unsetPCR := ReferencedProgram{Number: 4, PMTPID: 0x1003, Known: true}
	s.layout(noPCR, second, program(), unsetPCR)
	s.tick(0x0200, pmtPID, 0x1001, 0x1002, 0x1003)
	s.finish()
	var got []rowKey
	for _, row := range s.check.Result().Referenced.PIDs {
		got = append(got, rowKey{row.Program, row.Role, row.PID})
	}
	want := []rowKey{
		{1, RolePMT, pmtPID}, {1, RolePCR, pcrPID}, {1, RoleElementaryStream, pcrPID},
		{2, RolePMT, 0x1001}, {2, RolePCR, 0x0200}, {2, RoleElementaryStream, 0x0200},
		{3, RolePMT, 0x1002}, {4, RolePMT, 0x1003},
	}
	if len(got) != len(want) {
		t.Fatalf("rows %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %v, want %v", i, got[i], want[i])
		}
	}
	if RolePMT.String() != "pmt" || RolePCR.String() != "pcr" || RoleElementaryStream.String() != "elementary_stream" {
		t.Error("role names")
	}
}

func TestReferencedPIDWithoutTimeBase(t *testing.T) {
	s := newStream()
	s.layout(program(ElementaryStream{PID: audioPID, StreamType: 0x03}))
	s.packets(pmtPID, pcrPID, nullPID)
	s.finish()
	r := s.check.Result()
	for _, row := range r.Referenced.PIDs {
		if row.Status != PIDStatusInsufficientData || row.Reason != ReasonNoPCRTimeBase || row.MaxSilenceKnown {
			t.Errorf("row %+v", row)
		}
	}
	if r.Referenced.Status != PIDStatusInsufficientData {
		t.Errorf("status %v", r.Referenced.Status)
	}
	if r.Unreferenced.Status != PIDStatusInsufficientData || r.Unreferenced.Reason != ReasonNoPCRTimeBase {
		t.Errorf("unreferenced %+v", r.Unreferenced)
	}
}

func TestReferencedSeverity(t *testing.T) {
	if !(severity(PIDStatusAbsent) > severity(PIDStatusSilent) && severity(PIDStatusSilent) > severity(PIDStatusInsufficientData) &&
		severity(PIDStatusInsufficientData) > severity(PIDStatusOK)) {
		t.Error("severity order")
	}
	// Silent without absent.
	s := newStream()
	s.layout(program(ElementaryStream{PID: audioPID, StreamType: 0x03}))
	s.tick(pmtPID, audioPID)
	for range 60 {
		s.tick(pmtPID)
	}
	s.tick(pmtPID, audioPID)
	s.finish()
	if r := s.check.Result(); r.Referenced.Status != PIDStatusSilent {
		t.Errorf("status %v", r.Referenced.Status)
	}
}

func unreferencedOf(r PIDReferenceResult, pid uint16) (UnreferencedPID, bool) {
	for _, u := range r.Unreferenced.PIDs {
		if u.PID == pid {
			return u, true
		}
	}
	return UnreferencedPID{}, false
}

func TestUnreferencedPIDs(t *testing.T) {
	s := newStream()
	// Packets before the first layout (acquisition) are never unreferenced.
	s.packets(0x0700)
	layout := func(streams ...ElementaryStream) {
		p := program(streams...)
		p.CAPIDs = []uint16{0x0400}
		s.check.ObserveLayout(ReferenceLayout{Index: s.index, Offset: s.offset(), NetworkPID: 0x0020, Programs: []ReferencedProgram{p}})
	}
	layout(ElementaryStream{PID: 0x0106, StreamType: 0x03})
	for i := range 30 { // 3 s
		var pids []uint16
		switch {
		case i < 10:
			// Reserved, network, CA and ATSC base PIDs are never
			// unreferenced; 0x0300 is, for 1 s.
			pids = []uint16{0x0011, 0x0020, 0x0400, atscBasePID, 0x0300, 0x0106}
		case i == 10:
			// 0x0105 precedes the layout that adds it by 200 ms.
			pids = []uint16{0x0105, 0x0106}
		case i == 11:
			pids = []uint16{0x0105, 0x0106}
		case i < 14:
			// 0x0106 continues 300 ms after the layout that removes it.
			pids = []uint16{0x0105, 0x0106}
		case i < 20:
			pids = []uint16{0x0105, 0x0107} // 0x0107: removed stream continuing
		default:
			pids = []uint16{0x0105, 0x0107}
		}
		s.tick(pids...)
		if i == 11 {
			layout(ElementaryStream{PID: 0x0105, StreamType: 0x03}, ElementaryStream{PID: 0x0107, StreamType: 0x03})
		}
		if i == 14 {
			layout(ElementaryStream{PID: 0x0105, StreamType: 0x03})
		}
	}
	// 0x0500 appears 300 ms before the end: too short to judge.
	s.tick(0x0500)
	s.tick(0x0500)
	s.tick()
	s.finish()
	r := s.check.Result()
	if r.Unreferenced.Status != PIDStatusUnreferenced || r.Unreferenced.Reason != "" {
		t.Errorf("status %+v", r.Unreferenced)
	}
	stray, ok := unreferencedOf(r, 0x0300)
	// Never referenced: from its first packet (index 6) to the end of the
	// scan, clamped to the last PCR (index 321).
	if !ok || stray.Status != PIDStatusUnreferenced || stray.Packets != 10 || stray.FirstOffset != 6*188 || stray.LastOffset != 96*188 || !near(stray.LongestMs, 3150) {
		t.Errorf("0x0300 %+v (end %d)", stray, s.index)
	}
	removed, ok := unreferencedOf(r, 0x0107)
	// Removed at interval 15 and continuing to interval 29: from its first
	// unreferenced packet to its last.
	if !ok || removed.Status != PIDStatusUnreferenced || removed.Packets != 15 || !near(removed.LongestMs, 1400) {
		t.Errorf("0x0107 %+v", removed)
	}
	late, ok := unreferencedOf(r, 0x0500)
	if !ok || late.Status != PIDStatusInsufficientData || late.Packets != 2 {
		t.Errorf("0x0500 %+v", late)
	}
	for _, pid := range []uint16{0x0011, 0x0020, 0x0400, atscBasePID, 0x0105, 0x0106, 0x0700, nullPID} {
		if u, ok := unreferencedOf(r, pid); ok {
			t.Errorf("0x%04x unreferenced: %+v", pid, u)
		}
	}
	if len(r.Unreferenced.PIDs) != 3 || r.Unreferenced.PIDs[0].PID != 0x0107 {
		t.Errorf("order %+v", r.Unreferenced.PIDs)
	}
}

func TestUnreferencedWithinGraceOnly(t *testing.T) {
	s := newStream()
	s.layout(program())
	s.tick()
	s.tick(0x0500)
	s.tick(0x0600)
	s.finish()
	r := s.check.Result()
	if r.Unreferenced.Status != PIDStatusInsufficientData || r.Unreferenced.Reason != ReasonUnreferencedWithinGrace || len(r.Unreferenced.PIDs) != 2 {
		t.Errorf("%+v", r.Unreferenced)
	}
}

func TestUnreferencedNeedsKnownLayouts(t *testing.T) {
	s := newStream()
	other := ReferencedProgram{Number: 2, PMTPID: 0x1001}
	s.layout(program(), other)
	for range 20 {
		s.tick(0x0300)
	}
	s.finish()
	r := s.check.Result()
	if r.Unreferenced.Status != PIDStatusSkipped || r.Unreferenced.Reason != ReasonProgramLayoutsUnknown || len(r.Unreferenced.PIDs) != 0 {
		t.Errorf("%+v", r.Unreferenced)
	}
	if pmt := rowOf(t, r, 2, RolePMT, 0x1001); pmt.Status != PIDStatusInsufficientData {
		t.Errorf("program 2 PMT %+v", pmt)
	}

	// A layout that stops being fully known ends a run: it is judged up to
	// that point, here too short.
	s = newStream()
	s.layout(program())
	s.tick(0x0300)
	s.layout(program(), other)
	for range 20 {
		s.tick(0x0300)
	}
	s.finish()
	r = s.check.Result()
	if u, ok := unreferencedOf(r, 0x0300); r.Unreferenced.Status != PIDStatusInsufficientData || !ok || u.Packets != 1 {
		t.Errorf("incomplete: %+v", r.Unreferenced)
	}
}

func TestUnreferencedClosingRunsAreBounded(t *testing.T) {
	s := newStream()
	referencing := program(ElementaryStream{PID: 0x0300, StreamType: 0x06})
	s.layout(program())
	s.pcr(0, false)
	// More runs end within one PCR interval than are kept.
	for range maxClosingRuns + 2 {
		s.packets(0x0300)
		s.layout(referencing)
		s.layout(program())
	}
	s.fill(nullPID, 5)
	s.pcr(1000, false)
	s.finish()
	u := s.check.unref[0x0300]
	if u.undetermined.runs != 0 || u.found.runs != 0 || len(u.closing) != 0 {
		t.Errorf("runs %+v", u)
	}
}
