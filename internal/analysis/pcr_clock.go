package analysis

// Time basis of the packet-gap and PID-reference checks.
//
// A transport stream file keeps the order and byte position of its packets
// but not when they arrived. The checks therefore estimate the time of a
// packet from its position between the PCRs of one reference program
// ("pcr_interpolated_file_position", the model of the bitrate and PCR jitter
// estimates): a packet between two PCRs of the same timing segment gets the
// time interpolated linearly from its packet index. The time of a packet is
// only known once the next PCR has arrived, so a stamp taken now is resolved
// at the next PCR; stamps taken after the last PCR are resolved to the last
// PCR when the scan finishes, and stamps taken before the first PCR to the
// first PCR. Durations measured this way are lower bounds: time outside the
// PCR-covered part of the range is not counted, and neither is time across a
// timing-segment boundary. Whether two consecutive PCRs are comparable is the
// PCRStep that PCRClock gives every PCR-based result (pcr.go): a continuous
// or wraparound step adds its interval, and every other step (declared,
// pcr_pid_change, backward, forward_jump) starts a new segment without adding
// time.

// stamp is the position of one packet, or of one event at a packet, and its
// estimated time once resolved. ms is the PCR time elapsed since the first
// PCR, not advancing across timing-segment boundaries.
type stamp struct {
	index    int64 // packet index in the range
	offset   int64 // byte offset of the packet
	seq      int   // PCRs the clock had seen when the stamp was taken
	ms       float64
	resolved bool
	// exact is set when the stamp was interpolated between two PCRs of one
	// timing segment, and segment numbers that segment. A stamp before the
	// first PCR, after the last PCR or in a PCR interval that ends at a
	// segment boundary was clamped instead.
	exact   bool
	segment int
}

// settler is state that holds unresolved stamps. The clock calls resolve for
// each registered settler at the next PCR, and at the end of the scan.
type settler interface {
	resolve(c *pcrClock)
}

// pcrClock turns packet indices into PCR time.
type pcrClock struct {
	seq   int // number of PCRs seen
	has   bool
	index int64   // packet index of the last PCR
	ms    float64 // elapsed time at the last PCR
	// segment numbers the timing segment of the last PCR.
	segment int

	// The PCR being applied while settlers resolve: stamps taken after the
	// last PCR lie between it and this one.
	nextIndex    int64
	nextMs       float64
	nextBoundary bool
	finished     bool

	pending []settler
}

// take returns an unresolved stamp at a packet.
func (c *pcrClock) take(index, offset int64) stamp {
	return stamp{index: index, offset: offset, seq: c.seq}
}

// wait registers s to be resolved at the next PCR. queued is the settler's
// own flag, so a settler is registered at most once per PCR interval and the
// list is bounded by the number of settlers.
func (c *pcrClock) wait(s settler, queued *bool) {
	if !*queued {
		*queued = true
		c.pending = append(c.pending, s)
	}
}

// resolve sets the time of a stamp taken since the last PCR. It is called by
// settlers during observePCR or finish; stamps already resolved, and stamps
// that cannot be resolved yet, are left unchanged.
func (c *pcrClock) resolve(s *stamp) {
	if s.resolved || s.seq != c.seq {
		return
	}
	switch {
	case c.finished:
		if !c.has {
			return // no PCR at all: no time base
		}
		s.ms = c.ms
	case !c.has || c.nextBoundary:
		// Before the first PCR, or across a segment boundary: no time is
		// counted.
		s.ms = c.nextMs
	default:
		s.ms = c.ms + float64(s.index-c.index)/float64(c.nextIndex-c.index)*(c.nextMs-c.ms)
		s.exact = true
	}
	s.segment = c.segment
	s.resolved = true
}

// observePCR applies one PCR of the reference program, carried by the packet
// at index, with its step relative to the program's previous PCR.
func (c *pcrClock) observePCR(index int64, step PCRStep) {
	c.nextIndex, c.nextMs, c.nextBoundary = index, c.ms, !step.Comparable()
	if !c.nextBoundary {
		c.nextMs += float64(step.Ticks) / PCRTicksPerMs
	}
	c.settle()
	if c.has && c.nextBoundary {
		c.segment++
	}
	c.seq++
	c.has, c.index, c.ms = true, index, c.nextMs
}

// finish resolves the stamps taken after the last PCR to its time.
func (c *pcrClock) finish() {
	c.finished = true
	c.settle()
}

func (c *pcrClock) settle() {
	pending := c.pending
	c.pending = nil
	for _, s := range pending {
		s.resolve(c)
	}
}

// interval is a span between two stamps. Its duration is known once both are
// resolved.
type interval struct {
	start, end stamp
}

func (i *interval) resolve(c *pcrClock) {
	c.resolve(&i.start)
	c.resolve(&i.end)
}

func (i interval) known() bool { return i.start.resolved && i.end.resolved }

func (i interval) ms() float64 { return i.end.ms - i.start.ms }

// exact reports whether the duration was interpolated within one timing
// segment, rather than clamped at the edge of the PCR-covered range or at a
// segment boundary.
func (i interval) exact() bool {
	return i.start.exact && i.end.exact && i.start.segment == i.end.segment
}

// longest keeps the longest of a chain of intervals, each starting where the
// previous one ended (the silences of one PID). Their ends are taken now, so
// all intervals offered since the last PCR end in the same PCR interval. At
// most one of them starts before that PCR (straddle); the others lie entirely
// in the PCR interval, where time is proportional to the packet count, so
// only the one spanning the most packets (inner) can be the longest. This
// bounds the state to two candidates whatever the packet rate. The owner
// waits on the clock whenever it offers an interval that is not yet known,
// and calls resolve from its own resolve.
type longest struct {
	best                  interval
	has                   bool
	straddle, inner       interval
	hasStraddle, hasInner bool
	// unsolved is set when the scan finished with a candidate whose time is
	// unknown: there was no PCR at all.
	unsolved bool
}

// offer adds one interval of the chain. Its end was just taken, so it is not
// resolved yet; its start is resolved when it was taken before the last PCR,
// since the owner resolves its stamps at every PCR.
func (l *longest) offer(i interval) {
	switch {
	case i.start.resolved:
		l.straddle, l.hasStraddle = i, true
	case !l.hasInner || i.end.index-i.start.index > l.inner.end.index-l.inner.start.index:
		l.inner, l.hasInner = i, true
	}
}

func (l *longest) consider(i interval) {
	if !l.has || i.ms() > l.best.ms() {
		l.best, l.has = i, true
	}
}

// resolve settles the candidates.
func (l *longest) resolve(c *pcrClock) {
	settle(c, &l.straddle, &l.hasStraddle, l.consider, &l.unsolved)
	settle(c, &l.inner, &l.hasInner, l.consider, &l.unsolved)
}

// settle resolves a pending interval and hands it to done once its duration
// is known. At the end of a scan without any PCR it stays unknown, which sets
// unsolved.
func settle(c *pcrClock, i *interval, pending *bool, done func(interval), unsolved *bool) {
	if !*pending {
		return
	}
	i.resolve(c)
	switch {
	case i.known():
		*pending = false
		done(*i)
	case c.finished:
		*pending = false
		*unsolved = true
	}
}
