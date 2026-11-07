package analysis

import (
	"reflect"
	"testing"
)

func gapProgram(streams ...ElementaryStream) ReferencedProgram {
	return ReferencedProgram{Number: 1, PMTPID: 0x1000, Known: true, PCRPID: 0x0100, Streams: streams}
}

// A PID removed by a layout change and re-added later gets no gap across its
// absence; a PID referenced for the first time keeps the gap from the packets
// read before the first layout.
func TestPacketGapsRestartAtLayoutChanges(t *testing.T) {
	s := newStream()
	video := ElementaryStream{PID: 0x0100, StreamType: 0x02}
	audio := ElementaryStream{PID: 0x0200, StreamType: 0x03}
	s.packets(0x0200) // 0: before the first layout
	s.fill(0x1FFF, 3) // 1-3
	s.layout(gapProgram(video, audio))
	s.pcr(0, false)                    // 4
	s.packets(0x0200)                  // 5: a gap of 4 packets across the first layout
	s.fill(0x1FFF, 2)                  // 6-7
	s.packets(0x0200)                  // 8
	s.layout(gapProgram(video))        // removed from index 9
	s.packets(0x0200)                  // 9: after the removal, a new chain
	s.fill(0x1FFF, 10)                 // 10-19
	s.pcr(100, false)                  // 20
	s.packets(0x0200)                  // 21: unreferenced, same chain as packet 9: gap 11
	s.fill(0x1FFF, 20)                 // 22-41
	s.layout(gapProgram(video, audio)) // re-added from index 42
	s.packets(0x0200)                  // 42: new chain, no gap of 20
	s.fill(0x1FFF, 2)                  // 43-44
	s.packets(0x0200)                  // 45
	s.pcr(200, false)                  // 46
	s.packets(0x1000)                  // 47: one packet only
	s.finish()

	gaps := s.check.PacketGaps()
	var pids []uint16
	for _, g := range gaps {
		pids = append(pids, g.PID)
	}
	if !reflect.DeepEqual(pids, []uint16{0x0100, 0x0200, 0x1000}) {
		t.Fatalf("PIDs %v (the null PID is excluded)", pids)
	}
	pcr, audioGap, pmt := gaps[0], gaps[1], gaps[2]
	want := []PIDReference{{1, RolePCR, 0}, {1, RoleElementaryStream, 0x02}}
	if !reflect.DeepEqual(pcr.References, want) || pcr.Status != GapStatusInfo || pcr.Reason != "" || pcr.MaxGapPackets != 25 {
		t.Errorf("PCR PID %+v", pcr)
	}
	if audioGap.Packets != 7 || audioGap.MaxGapPackets != 11 || audioGap.MaxGapStartOffset != 9*188 || audioGap.MaxGapEndOffset != 21*188 ||
		!audioGap.MaxGapTimeKnown || !audioGap.MaxGapExact || audioGap.Status != GapStatusInfo {
		t.Errorf("audio %+v", audioGap)
	}
	if !reflect.DeepEqual(audioGap.References, []PIDReference{{1, RoleElementaryStream, 0x03}}) {
		t.Errorf("audio references %+v", audioGap.References)
	}
	if pmt.HasGap || pmt.Status != GapStatusInsufficientData || pmt.Reason != ReasonNoConsecutivePackets || !reflect.DeepEqual(pmt.References, []PIDReference{{1, RolePMT, 0}}) {
		t.Errorf("PMT %+v", pmt)
	}
}

// Two packets around a removal are not consecutive; without any PCR a gap
// has no time.
func TestPacketGapsStatuses(t *testing.T) {
	s := newStream()
	s.layout(gapProgram(ElementaryStream{PID: 0x0200, StreamType: 0x03}))
	s.packets(0x0200, 0x0300, 0x0011)
	s.layout(gapProgram())
	s.packets(0x0200, 0x0300, 0x0300)
	s.finish()
	gaps := s.check.PacketGaps()
	if len(gaps) != 3 {
		t.Fatalf("gaps %+v", gaps)
	}
	if g := gaps[0]; g.PID != 0x0011 || g.Status != GapStatusInsufficientData || len(g.References) != 0 {
		t.Errorf("SDT %+v", g)
	}
	if g := gaps[1]; g.PID != 0x0200 || g.Packets != 2 || g.HasGap || g.Status != GapStatusInsufficientData || g.Reason != ReasonNoConsecutivePackets {
		t.Errorf("removed %+v", g)
	}
	if g := gaps[2]; g.PID != 0x0300 || g.MaxGapPackets != 2 || g.MaxGapTimeKnown || g.Status != GapStatusInfo || g.Reason != ReasonNoPCRTimeBase {
		t.Errorf("no time base %+v", g)
	}
	// A restart of a PID that was never observed is a no-op.
	s.check.Gaps().restart(0x0400)
	if _, ok := s.check.Gaps().PID(0x0400); ok {
		t.Error("restart observed a PID")
	}
}

// Of equal gaps the first after the first PCR is kept: one before it could
// only have a clamped time.
func TestPacketGapsPreferEqualGapsAfterTheFirstPCR(t *testing.T) {
	s := newStream()
	s.packets(0x0200, 0x1FFF, 0x0200) // 0-2: a gap of 1 before the first PCR
	s.pcr(0, false)                   // 3
	s.packets(0x0200)                 // 4: a gap of 1 from packet 2, which precedes the PCR
	s.fill(0x1FFF, 1)                 // 5
	s.packets(0x0200)                 // 6: a gap of 1 after the first PCR
	s.fill(0x1FFF, 1)                 // 7
	s.packets(0x0200)                 // 8: equal again, not kept
	s.pcr(90, false)                  // 9
	s.finish()
	p, _ := s.check.Gaps().PID(0x0200)
	if p.MaxGapPackets != 1 || p.MaxGapStartOffset != 4*188 || p.MaxGapEndOffset != 6*188 || !p.MaxGapExact || !near(p.MaxGapMs, 30) {
		t.Errorf("gap %+v", p)
	}
}
