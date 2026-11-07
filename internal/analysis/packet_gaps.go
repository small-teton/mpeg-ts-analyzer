package analysis

// PacketGaps is the bounded per-PID packet collector of the analysis. For
// every PID it keeps the number of packets, the first and last packet and the
// largest gap between two consecutive packets, and it owns the PCR clock that
// turns packet positions into time (see pcr_clock.go). The PID reference
// check (PIDReferenceCheck) is built on it, and the report of per-PID maximum
// packet gaps (issue #176) presents its PIDs result, so the analysis keeps a
// single per-PID packet history.
//
// Its state is one record per PID (at most 8192) with a fixed number of
// stamps, plus the list of records waiting for the next PCR; it keeps no
// per-packet history.
type PacketGaps struct {
	clock pcrClock
	pids  [nullPID + 1]*pidPackets // indexed by PID; nil until observed
	count int                      // PIDs observed
}

type pidPackets struct {
	pid         uint16
	packets     int64
	first, last stamp
	// worst is the gap spanning the most packets of other PIDs; its time is
	// resolved later. gapPackets is that number of packets.
	worst      interval
	gapPackets int64
	hasGap     bool
	queued     bool
}

// NewPacketGaps returns a collector that has seen no packet and no PCR.
func NewPacketGaps() *PacketGaps {
	return &PacketGaps{}
}

// ObservePacket counts one packet. Packets must be observed in file order,
// with their index in the range.
func (g *PacketGaps) ObservePacket(o Packet) {
	now := g.clock.take(o.Index, o.Offset)
	p := g.pids[o.PID&nullPID]
	if p == nil {
		p = &pidPackets{pid: o.PID, first: now}
		g.pids[o.PID&nullPID] = p
		g.count++
	} else if gap := o.Index - p.last.index - 1; !p.hasGap || gap > p.gapPackets {
		p.worst, p.gapPackets, p.hasGap = interval{start: p.last, end: now}, gap, true
	}
	p.packets++
	p.last = now
	g.clock.wait(p, &p.queued)
}

func (p *pidPackets) resolve(c *pcrClock) {
	p.queued = false
	c.resolve(&p.first)
	c.resolve(&p.last)
	p.worst.resolve(c)
}

// ObservePCR applies one PCR of the reference program; see TimedPCR.
func (g *PacketGaps) ObservePCR(o TimedPCR) {
	g.clock.observePCR(o.Index, o.Step)
}

// Finish ends the scan: times not yet resolved are clamped to the last PCR.
func (g *PacketGaps) Finish() { g.clock.finish() }

// HasTimeBase reports whether at least one PCR of the reference program was
// observed, so that durations can be estimated.
func (g *PacketGaps) HasTimeBase() bool { return g.clock.has }

// PIDPackets is what PacketGaps collected for one PID.
type PIDPackets struct {
	PID         uint16
	Packets     int64
	FirstOffset int64
	LastOffset  int64
	// HasGap is set when the PID has at least two packets. MaxGapPackets is
	// then the largest number of packets of other PIDs between two
	// consecutive packets of the PID (the endpoints are not counted), and
	// MaxGapStartOffset and MaxGapEndOffset are the offsets of those two
	// packets; the first such gap is kept when several are equal.
	HasGap            bool
	MaxGapPackets     int64
	MaxGapStartOffset int64
	MaxGapEndOffset   int64
	// MaxGapMs is the estimated time of that gap, valid when MaxGapTimeKnown.
	// MaxGapExact is set when both packets lie between PCRs of one timing
	// segment; otherwise the time is a lower bound (see pcr_clock.go).
	MaxGapMs        float64
	MaxGapTimeKnown bool
	MaxGapExact     bool
}

// PIDs returns every PID observed so far in ascending PID order. Times are
// final after Finish.
func (g *PacketGaps) PIDs() []PIDPackets {
	result := make([]PIDPackets, 0, g.count)
	for _, p := range g.pids {
		if p != nil {
			result = append(result, p.result())
		}
	}
	return result
}

// PID returns what was collected for one PID, and whether it was observed.
func (g *PacketGaps) PID(pid uint16) (PIDPackets, bool) {
	p := g.pids[pid&nullPID]
	if p == nil {
		return PIDPackets{PID: pid}, false
	}
	return p.result(), true
}

func (p *pidPackets) result() PIDPackets {
	r := PIDPackets{PID: p.pid, Packets: p.packets, FirstOffset: p.first.offset, LastOffset: p.last.offset, HasGap: p.hasGap}
	if p.hasGap {
		r.MaxGapPackets, r.MaxGapStartOffset, r.MaxGapEndOffset = p.gapPackets, p.worst.start.offset, p.worst.end.offset
		if p.worst.known() {
			r.MaxGapMs, r.MaxGapTimeKnown, r.MaxGapExact = p.worst.ms(), true, p.worst.exact()
		}
	}
	return r
}
