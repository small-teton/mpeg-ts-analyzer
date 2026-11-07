package analysis

import (
	"math"
	"testing"
)

// stream feeds a check packet by packet: each packet has the next index and
// the offset index × 188.
type stream struct {
	check *PIDReferenceCheck
	index int64
	clock PCRClock // classifies the PCRs as the parser's PCRCheck does
}

func newStream() *stream { return &stream{check: NewPIDReferenceCheck()} }

func (s *stream) offset() int64 { return s.index * 188 }

// packets observes one packet of each pid in order.
func (s *stream) packets(pids ...uint16) {
	for _, pid := range pids {
		s.check.ObservePacket(Packet{PID: pid, Index: s.index, Offset: s.offset()})
		s.index++
	}
}

// fill observes n packets of pid.
func (s *stream) fill(pid uint16, n int) {
	for range n {
		s.packets(pid)
	}
}

// pcr applies a PCR at ms milliseconds carried by the next packet, which it
// observes on PID 0x0100.
func (s *stream) pcr(ms float64, discontinuity bool) {
	step := s.clock.Observe(PCR{PID: 0x0100, Offset: s.offset(), Value: uint64(ms * PCRTicksPerMs), DiscontinuityIndicator: discontinuity})
	s.check.ObservePCR(TimedPCR{Index: s.index, Offset: s.offset(), Step: step})
	s.packets(0x0100)
}

func (s *stream) layout(programs ...ReferencedProgram) {
	s.check.ObserveLayout(ReferenceLayout{Index: s.index, Offset: s.offset(), Programs: programs})
}

func (s *stream) finish() { s.check.Finish(s.index, s.offset()) }

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPacketGapsInterpolatesBetweenPCRs(t *testing.T) {
	s := newStream()
	// PCRs at indices 0 (0 ms) and 10 (100 ms): 10 ms per packet.
	s.pcr(0, false)           // index 0
	s.packets(0x0200)         // 1
	s.fill(0x1FFF, 5)         // 2-6
	s.packets(0x0200)         // 7: gap of 5 packets, 60 ms
	s.fill(0x1FFF, 2)         // 8-9
	s.pcr(100, false)         // 10
	s.packets(0x0200, 0x0200) // 11, 12: gap of 0 packets
	s.fill(0x1FFF, 7)         // 13-19
	s.pcr(200, false)         // 20
	s.finish()

	gaps := s.check.Gaps()
	if !gaps.HasTimeBase() {
		t.Fatal("no time base")
	}
	p, ok := gaps.PID(0x0200)
	want := PIDPackets{PID: 0x0200, Packets: 4, FirstOffset: 188, LastOffset: 12 * 188, HasGap: true, MaxGapPackets: 5, MaxGapStartOffset: 188, MaxGapEndOffset: 7 * 188, MaxGapTimeKnown: true, MaxGapExact: true}
	if !ok || !near(p.MaxGapMs, 60) {
		t.Errorf("gap %+v", p)
	}
	p.MaxGapMs = 0
	if p != want {
		t.Errorf("PID 0x0200 = %+v, want %+v", p, want)
	}
	if _, ok := gaps.PID(0x0300); ok {
		t.Error("PID 0x0300 was never observed")
	}
	pids := gaps.PIDs()
	if len(pids) != 3 || pids[0].PID != 0x0100 || pids[1].PID != 0x0200 || pids[2].PID != 0x1FFF {
		t.Errorf("PIDs %+v", pids)
	}
	if pids[0].HasGap != true || pids[0].MaxGapPackets != 9 {
		t.Errorf("PCR PID %+v", pids[0])
	}
}

func TestPacketGapsSingleAndClampedPackets(t *testing.T) {
	s := newStream()
	s.packets(0x0200) // 0: before the first PCR
	s.pcr(1000, false)
	s.fill(0x1FFF, 3)
	s.pcr(1040, false) // index 5
	s.packets(0x0300)  // 6: one packet only
	s.packets(0x0200)  // 7: after the last PCR
	s.finish()
	p, _ := s.check.Gaps().PID(0x0200)
	// Index 0 is clamped to the first PCR and index 7 to the last: 40 ms,
	// not exact.
	if !p.MaxGapTimeKnown || p.MaxGapExact || !near(p.MaxGapMs, 40) || p.MaxGapPackets != 6 {
		t.Errorf("clamped %+v", p)
	}
	single, _ := s.check.Gaps().PID(0x0300)
	if single.HasGap || single.Packets != 1 || single.FirstOffset != 6*188 || single.LastOffset != 6*188 {
		t.Errorf("single %+v", single)
	}
}

func TestPCRClockSegments(t *testing.T) {
	for _, tc := range []struct {
		name          string
		second        float64
		discontinuity bool
	}{
		{"declared discontinuity", 140, true},
		{"forward jump beyond 1 s", 2000, false},
		{"backward", 50, false},
	} {
		s := newStream()
		s.pcr(100, false) // 0
		s.packets(0x0200) // 1
		s.fill(0x1FFF, 2)
		s.pcr(tc.second, tc.discontinuity) // 4: new segment, no time counted
		s.packets(0x0200)                  // 5
		s.fill(0x1FFF, 2)
		s.pcr(tc.second+30, false) // 8
		s.finish()
		p, _ := s.check.Gaps().PID(0x0200)
		// Only the 1 packet after the boundary PCR counts: 30 ms / 4 packets.
		if !p.MaxGapTimeKnown || p.MaxGapExact || !near(p.MaxGapMs, 7.5) {
			t.Errorf("%s: %+v", tc.name, p)
		}
	}

	// A wraparound is a normal interval: time keeps counting across it.
	s := newStream()
	top := float64(PCRWrapTicks-PCRTicksPerMs*10) / PCRTicksPerMs // 10 ms before the wrap
	s.pcr(top, false)
	s.packets(0x0200)
	s.fill(0x1FFF, 2)
	s.pcr(30, false) // 40 ms later across the wrap
	s.packets(0x0200)
	s.fill(0x1FFF, 2)
	s.pcr(70, false)
	s.finish()
	if p, _ := s.check.Gaps().PID(0x0200); !p.MaxGapExact || !near(p.MaxGapMs, 40) {
		t.Errorf("wraparound: %+v", p)
	}
}

func TestPacketGapsWithoutPCR(t *testing.T) {
	s := newStream()
	s.packets(0x0200, 0x1FFF, 0x0200)
	s.finish()
	if s.check.Gaps().HasTimeBase() {
		t.Error("time base without PCR")
	}
	p, _ := s.check.Gaps().PID(0x0200)
	if !p.HasGap || p.MaxGapPackets != 1 || p.MaxGapTimeKnown {
		t.Errorf("no PCR %+v", p)
	}
}

func TestLongestKeepsBoundedCandidates(t *testing.T) {
	var c pcrClock
	var l longest
	c.observePCR(0, PCRStep{Kind: PCRStepFirst})
	a := c.take(1, 1)
	b := c.take(2, 2)
	d := c.take(9, 9)
	// Two inner intervals of the same PCR interval: the wider one is kept.
	l.offer(interval{a, b})
	l.offer(interval{b, d})
	l.offer(interval{a, b})
	if !l.hasInner || l.inner.start.index != 2 {
		t.Fatalf("inner %+v", l.inner)
	}
	c.nextIndex, c.nextMs, c.nextBoundary = 10, 100, false
	l.resolve(&c)
	if !l.has || !near(l.best.ms(), 70) || l.hasInner {
		t.Fatalf("best %+v", l.best)
	}
	// A straddling interval from a resolved stamp; a shorter one later does
	// not replace the best.
	c.seq++
	c.index, c.ms = 10, 100
	e := c.take(11, 11)
	l.offer(interval{l.best.end, e})
	c.nextIndex, c.nextMs = 20, 200
	l.resolve(&c)
	if !near(l.best.ms(), 70) || l.hasStraddle {
		t.Errorf("straddle replaced the best: %+v", l.best)
	}
}
