package analysis

import (
	"cmp"
	"slices"
)

// PID reference health: whether the PIDs that the PSI layout references carry
// packets, and whether packets arrive on PIDs that no layout references.
//
// Referenced PIDs. Every program of the applicable PAT references its PMT PID;
// a program whose PMT the analysis follows also references its PCR_PID
// (unless 0x1FFF) and each elementary_PID. Each (program, role, PID) is one
// result row, so a PCR_PID that also carries an elementary stream has a pcr
// row and an elementary_stream row. A row is referenced from the packet at
// which its layout applies (the scan start, the start of the program's
// analysis after its PMT was acquired, or the packet after the one that
// completed a PAT/PMT change) until the layout stops referencing it or the
// scan ends. Its silence is the longest time without a packet of its PID
// while it is referenced, including the time from the start of a reference
// to the first packet and from the last packet to the end of the reference.
//
// The roles judged for absence and silence are the PMT, the PCR_PID, and
// video and audio elementary streams (the stream types labeled video or audio
// in the continuity summary), which carry packets continuously. Other stream
// types (private data, subtitles, metadata, ...) may legitimately be idle for
// a long time, so only their presence is reported. PIDSilenceLimitMs is the
// limit for judged roles; ETSI TR 101 290 (5.2.1, check 1.6 PID_error) leaves
// the period "user specified", and TSDuck's TR 101 290 analyzer uses the same
// 5 s default for video and audio PIDs (ts::tr101290::DEFAULT_MAX_PID_INTERVAL).
//
// Row status:
//
//   - ok: a packet arrived while referenced and, for a judged role, no
//     silence exceeded the limit;
//   - absent: a judged role had no packet while referenced, during a
//     reference longer than the limit;
//   - silent: a judged role had packets, but a silence exceeded the limit;
//   - insufficient_data: no packet arrived while referenced, but the reference
//     was not longer than the limit (reason reference_shorter_than_limit), or
//     the stream type is not judged (intermittent_stream_type); or there is no
//     PCR time base to measure silences (no_pcr_time_base).
//
// Unreferenced PIDs. A packet is unreferenced when its PID is not referenced
// by the applicable layout, and is not the null PID 0x1FFF, one of the PIDs
// 0x0000-0x001F that ISO/IEC 13818-1 (Table 2-3) and ETSI EN 300 468
// reserve for PSI/SI, 0x1FFB (ATSC base PID), the PAT's network_PID or a
// CA_PID named by a PMT. Unreferenced packets are only judged while the
// analysis follows the PMT of every program of the applicable PAT, so that
// the referenced PIDs are fully known. Consecutive unreferenced packets of a
// PID form a run, which ends when a layout references the PID, when the
// layout stops being fully known, or at the end of the scan. ETSI TR 101 290
// (5.2.3, check 3.4 Unreferenced_PID) requires a PID to be referred to by a
// PMT within 0.5 s (UnreferencedGraceMs, TSDuck's MAX_PID_REFERENCE_INTERVAL),
// which tolerates packets of a new stream that precede the PMT announcing it
// and packets of a removed stream that follow the PMT removing it:
//
//   - a run of a PID that no layout referenced before is unreferenced when the
//     reference does not follow within the grace period of its first packet;
//   - a run of a PID that an earlier layout referenced (a removed stream) is
//     unreferenced when its packets continue for longer than the grace period.
//
// A run that the end of the scan (or a layout that is no longer fully known)
// cuts before the grace period could elapse cannot be judged and makes the
// result insufficient_data. The CAT is not parsed, so an EMM PID that only
// the CAT announces is unreferenced.

// PIDSilenceLimitMs is the longest silence of a judged referenced PID.
const PIDSilenceLimitMs = 5000.0

// UnreferencedGraceMs is how long a PID may carry packets without a reference.
const UnreferencedGraceMs = 500.0

// maxClosingRuns bounds the unreferenced runs of one PID that may end within
// one PCR interval while their time is still unknown; a further run in the
// same PCR interval is not judged. Each run ends at a layout change, so this is
// only reached by several PAT/PMT changes within one PCR interval.
const maxClosingRuns = 8

// atscBasePID is the PID of the ATSC PSIP base tables.
const atscBasePID = 0x1FFB

// PIDStatus is the status of a PID reference result.
type PIDStatus string

const (
	PIDStatusOK               PIDStatus = "ok"
	PIDStatusAbsent           PIDStatus = "absent"
	PIDStatusSilent           PIDStatus = "silent"
	PIDStatusUnreferenced     PIDStatus = "unreferenced"
	PIDStatusInsufficientData PIDStatus = "insufficient_data"
	PIDStatusSkipped          PIDStatus = "skipped"
)

// Reasons of PID reference results that are insufficient_data or skipped.
const (
	ReasonNoPCRTimeBase             = "no_pcr_time_base"
	ReasonReferenceShorterThanLimit = "reference_shorter_than_limit"
	ReasonIntermittentStreamType    = "intermittent_stream_type"
	ReasonUnreferencedWithinGrace   = "unreferenced_within_grace"
	ReasonProgramLayoutsUnknown     = "program_layouts_unknown"
)

// ReferenceRole is what a layout references a PID as.
type ReferenceRole int

const (
	RolePMT ReferenceRole = iota
	RolePCR
	RoleElementaryStream
)

func (r ReferenceRole) String() string {
	switch r {
	case RolePMT:
		return "pmt"
	case RolePCR:
		return "pcr"
	default:
		return "elementary_stream"
	}
}

type rowKey struct {
	program uint16
	role    ReferenceRole
	pid     uint16
}

// PIDReferenceCheck evaluates referenced and unreferenced PIDs. It is fed
// every packet and PCR through its PacketGaps collector and the layouts
// through ObserveLayout. Its state is one row per (program, role, PID) ever
// referenced and one record per unreferenced PID, each with a fixed number of
// stamps, so it is bounded by the number of PIDs and programs.
type PIDReferenceCheck struct {
	gaps       *PacketGaps
	rows       map[rowKey]*referenceRow
	active     map[uint16][]*referenceRow // rows referenced now, by PID
	activePID  [nullPID + 1]bool          // active has the PID
	referenced [nullPID + 1]bool          // PIDs referenced by the applicable layout
	ever       [nullPID + 1]bool          // PIDs referenced by any layout so far
	unref      map[uint16]*unreferencedPID
	complete   bool // every program's PMT is followed
	assessed   bool // complete was set at some point
}

// NewPIDReferenceCheck returns a check that has seen no layout.
func NewPIDReferenceCheck() *PIDReferenceCheck {
	return &PIDReferenceCheck{
		gaps:   NewPacketGaps(),
		rows:   make(map[rowKey]*referenceRow),
		active: make(map[uint16][]*referenceRow),
		unref:  make(map[uint16]*unreferencedPID),
	}
}

// Gaps returns the per-PID packet collector the check is built on.
func (c *PIDReferenceCheck) Gaps() *PacketGaps { return c.gaps }

// ObservePCR applies one PCR of the reference program.
func (c *PIDReferenceCheck) ObservePCR(o PCR) { c.gaps.ObservePCR(o) }

// ObservePacket counts one packet; see Packet.
func (c *PIDReferenceCheck) ObservePacket(o Packet) {
	c.gaps.ObservePacket(o)
	pid := o.PID & nullPID
	clock := &c.gaps.clock
	now := clock.take(o.Index, o.Offset)
	if c.activePID[pid] {
		for _, row := range c.active[pid] {
			row.packet(clock, now)
		}
	}
	if c.complete && !c.referenced[pid] && unreferencedCandidate(pid) {
		u, ok := c.unref[pid]
		if !ok {
			u = &unreferencedPID{pid: pid}
			c.unref[pid] = u
		}
		u.packet(clock, now, c.ever[pid])
	}
}

// unreferencedCandidate reports whether packets of pid can be unreferenced:
// it is not the null PID, a reserved PSI/SI PID or the ATSC base PID.
func unreferencedCandidate(pid uint16) bool {
	return pid > 0x001F && pid != atscBasePID && pid != nullPID
}

// ObserveLayout applies the layout that holds from its packet on.
func (c *PIDReferenceCheck) ObserveLayout(l ReferenceLayout) {
	clock := &c.gaps.clock
	now := clock.take(l.Index, l.Offset)
	want := make(map[rowKey]uint8)
	var referenced [nullPID + 1]bool
	complete := true
	add := func(key rowKey, streamType uint8) {
		key.pid &= nullPID // PIDs are 13 bits
		if key.pid != nullPID {
			want[key] = streamType
			referenced[key.pid] = true
		}
	}
	for _, p := range l.Programs {
		add(rowKey{p.Number, RolePMT, p.PMTPID}, 0)
		if !p.Known {
			complete = false
			continue
		}
		if p.PCRPID != 0 {
			add(rowKey{p.Number, RolePCR, p.PCRPID}, 0)
		}
		for _, s := range p.Streams {
			add(rowKey{p.Number, RoleElementaryStream, s.PID}, s.StreamType)
		}
		for _, pid := range p.CAPIDs {
			referenced[pid&nullPID] = true
		}
	}
	if l.NetworkPID != 0 {
		referenced[l.NetworkPID&nullPID] = true
	}
	for pid, rows := range c.active {
		kept := rows[:0]
		for _, row := range rows {
			if _, ok := want[row.key]; ok {
				kept = append(kept, row)
			} else {
				row.unwatch(clock, now)
			}
		}
		if len(kept) == 0 {
			delete(c.active, pid)
			c.activePID[pid] = false
		} else {
			c.active[pid] = kept
		}
	}
	for key, streamType := range want {
		row, ok := c.rows[key]
		if !ok {
			row = &referenceRow{key: key}
			c.rows[key] = row
		}
		row.streamType = streamType
		if !row.active {
			row.watch(clock, now)
			c.active[key.pid] = append(c.active[key.pid], row)
			c.activePID[key.pid] = true
		}
	}
	for pid, u := range c.unref {
		if u.open && (referenced[pid] || !complete) {
			u.close(clock, now, referenced[pid])
		}
	}
	for pid, yes := range referenced {
		c.ever[pid] = c.ever[pid] || yes
	}
	c.referenced, c.complete = referenced, complete
	c.assessed = c.assessed || complete
}

// Finish ends the scan at the packet index and byte offset just after its
// last packet: every reference and unreferenced run ends there, and times
// still unknown are clamped to the last PCR.
func (c *PIDReferenceCheck) Finish(index, offset int64) {
	clock := &c.gaps.clock
	now := clock.take(index, offset)
	for _, rows := range c.active {
		for _, row := range rows {
			row.unwatch(clock, now)
		}
	}
	c.active = make(map[uint16][]*referenceRow)
	c.activePID = [nullPID + 1]bool{}
	for _, u := range c.unref {
		if u.open {
			u.close(clock, now, false)
		}
	}
	c.gaps.Finish()
}

// referenceRow is one (program, role, PID) and its silences while
// referenced.
type referenceRow struct {
	key        rowKey
	streamType uint8
	active     bool
	since      stamp // the last packet while referenced, or the reference start
	silence    longest
	packets    int64
	first      int64 // offset of the first packet while referenced
	last       int64
	from       int64 // offset where the row was first referenced
	until      int64 // offset where its last reference ended
	referenced bool  // from is set
	queued     bool
}

func (r *referenceRow) watch(clock *pcrClock, now stamp) {
	r.active, r.since = true, now
	if !r.referenced {
		r.from, r.referenced = now.offset, true
	}
	clock.wait(r, &r.queued)
}

func (r *referenceRow) packet(clock *pcrClock, now stamp) {
	r.silence.offer(interval{start: r.since, end: now})
	if r.packets == 0 {
		r.first = now.offset
	}
	r.packets++
	r.last = now.offset
	r.since = now
	clock.wait(r, &r.queued)
}

func (r *referenceRow) unwatch(clock *pcrClock, now stamp) {
	r.silence.offer(interval{start: r.since, end: now})
	r.active, r.until = false, now.offset
	clock.wait(r, &r.queued)
}

func (r *referenceRow) resolve(c *pcrClock) {
	r.queued = false
	c.resolve(&r.since)
	r.silence.resolve(c)
}

// judged reports whether absence and silence are judged for the row's role.
func (r *referenceRow) judged() bool {
	if r.key.role != RoleElementaryStream {
		return true
	}
	label := streamTypeLabel(r.streamType)
	return label == "video" || label == "audio"
}

// unreferencedPID holds the unreferenced runs of one PID.
type unreferencedPID struct {
	pid     uint16
	open    bool
	run     unreferencedRun
	closing []unreferencedRun // ended runs whose time is still unknown
	queued  bool

	// Totals over the runs found unreferenced, and over the runs that could
	// not be judged.
	found, undetermined runTotals
}

type runTotals struct {
	runs        int
	packets     int64
	first, last int64
	longestMs   float64
}

func (t *runTotals) add(run unreferencedRun, ms float64) {
	if t.runs == 0 {
		t.first = run.start.offset
	}
	t.runs++
	t.packets += run.packets
	t.last = max(t.last, run.last.offset)
	t.longestMs = max(t.longestMs, ms)
}

type unreferencedRun struct {
	start, last, end stamp
	packets          int64
	// referencedBefore is set when an earlier layout referenced the PID.
	referencedBefore bool
	// byReference is set when the run ended because a layout referenced the
	// PID, rather than at the end of the scan or of a fully known layout.
	byReference bool
}

func (u *unreferencedPID) packet(clock *pcrClock, now stamp, referencedBefore bool) {
	if !u.open {
		u.open = true
		u.run = unreferencedRun{start: now, referencedBefore: referencedBefore}
	}
	u.run.last = now
	u.run.packets++
	clock.wait(u, &u.queued)
}

func (u *unreferencedPID) close(clock *pcrClock, now stamp, byReference bool) {
	u.open = false
	run := u.run
	run.end, run.byReference = now, byReference
	// The end was just taken, so the run is judged once the next PCR (or the
	// end of the scan) resolves it.
	if len(u.closing) < maxClosingRuns {
		u.closing = append(u.closing, run)
	}
	clock.wait(u, &u.queued)
}

func (r *unreferencedRun) resolve(c *pcrClock) {
	c.resolve(&r.start)
	c.resolve(&r.last)
	c.resolve(&r.end)
}

func (r *unreferencedRun) known() bool {
	return r.start.resolved && r.last.resolved && r.end.resolved
}

func (u *unreferencedPID) resolve(c *pcrClock) {
	u.queued = false
	if u.open {
		c.resolve(&u.run.start)
		c.resolve(&u.run.last)
	}
	// Every stamp of an ended run was taken since the last PCR, so the runs
	// are known now, unless the scan finished without any PCR.
	for _, run := range u.closing {
		run.resolve(c)
		if run.known() {
			u.judge(run)
		}
	}
	u.closing = u.closing[:0]
}

// judge decides an ended run whose time is known.
func (u *unreferencedPID) judge(run unreferencedRun) {
	untilEnd := run.end.ms - run.start.ms
	duration := untilEnd
	if run.referencedBefore {
		duration = run.last.ms - run.start.ms
	}
	switch {
	case duration > UnreferencedGraceMs:
		u.found.add(run, duration)
	case !run.byReference && untilEnd <= UnreferencedGraceMs:
		u.undetermined.add(run, duration)
	}
}

// ReferencedPID is the result of one (program, role, PID).
type ReferencedPID struct {
	Program uint16
	Role    ReferenceRole
	PID     uint16
	// StreamType is the stream_type of an elementary stream (the latest one
	// when a PMT change retyped it); 0 for the other roles.
	StreamType uint8
	Status     PIDStatus
	Reason     string
	// Judged is set when absence and silence are judged for the role.
	Judged bool
	// Packets counts the packets of the PID in the whole range (from the
	// first packet of the range, without packets with a transport error);
	// FirstOffset and LastOffset are the first and last of them.
	Packets     int64
	FirstOffset int64
	LastOffset  int64
	// ReferencedPackets counts the packets while the row was referenced;
	// ReferencedFirstOffset and ReferencedLastOffset are valid when it is not
	// zero.
	ReferencedPackets     int64
	ReferencedFirstOffset int64
	ReferencedLastOffset  int64
	// ReferencedFrom is the offset of the packet from which the row was first
	// referenced, ReferencedUntil the offset where its last reference ended
	// (the end of the scan when it was still referenced).
	ReferencedFrom  int64
	ReferencedUntil int64
	// MaxSilenceMs is the longest time without a packet while referenced,
	// valid when MaxSilenceKnown; MaxSilenceStartOffset is the offset of the
	// packet (or reference start) before it and MaxSilenceEndOffset the
	// offset of the packet (or reference end) that ended it. It is a lower
	// bound when MaxSilenceExact is not set (see pcr_clock.go).
	MaxSilenceMs          float64
	MaxSilenceKnown       bool
	MaxSilenceExact       bool
	MaxSilenceStartOffset int64
	MaxSilenceEndOffset   int64
}

// ReferencedResult is the result for referenced PIDs: the most severe row
// status (absent, then silent, then insufficient_data, then ok) and the rows
// ordered by program number, role (PMT, PCR, elementary stream) and PID.
type ReferencedResult struct {
	Status PIDStatus
	PIDs   []ReferencedPID
}

// UnreferencedPID is one PID that carried unreferenced packets.
type UnreferencedPID struct {
	PID uint16
	// Status is unreferenced when a run was found unreferenced, otherwise
	// insufficient_data: a run could not be judged.
	Status PIDStatus
	// Packets counts the packets of the runs with that status, FirstOffset
	// is the first packet of the first such run and LastOffset the last
	// packet of the last one. LongestMs is the longest of those runs, as
	// judged: up to its last packet for a removed stream, otherwise up to
	// the end of the run.
	Packets     int64
	FirstOffset int64
	LastOffset  int64
	LongestMs   float64
}

// UnreferencedResult is the result for unreferenced PIDs: unreferenced when a
// PID is, insufficient_data when a run could not be judged
// (unreferenced_within_grace) or there is no PCR time base, skipped when the
// layout was never fully known (program_layouts_unknown), ok otherwise. PIDs
// are in ascending order.
type UnreferencedResult struct {
	Status PIDStatus
	Reason string
	PIDs   []UnreferencedPID
}

// PIDReferenceResult is the result of the check.
type PIDReferenceResult struct {
	Referenced   ReferencedResult
	Unreferenced UnreferencedResult
}

// Result returns the result. It is final after Finish.
func (c *PIDReferenceCheck) Result() PIDReferenceResult {
	timeBase := c.gaps.HasTimeBase()
	referenced := ReferencedResult{Status: PIDStatusOK, PIDs: make([]ReferencedPID, 0, len(c.rows))}
	for _, row := range c.rows {
		r := c.referencedPID(row, timeBase)
		referenced.PIDs = append(referenced.PIDs, r)
		if severity(r.Status) > severity(referenced.Status) {
			referenced.Status = r.Status
		}
	}
	slices.SortFunc(referenced.PIDs, func(a, b ReferencedPID) int {
		return cmp.Or(cmp.Compare(a.Program, b.Program), cmp.Compare(a.Role, b.Role), cmp.Compare(a.PID, b.PID))
	})
	return PIDReferenceResult{Referenced: referenced, Unreferenced: c.unreferencedResult(timeBase)}
}

func severity(status PIDStatus) int {
	switch status {
	case PIDStatusAbsent:
		return 3
	case PIDStatusSilent:
		return 2
	case PIDStatusInsufficientData:
		return 1
	default:
		return 0
	}
}

func (c *PIDReferenceCheck) referencedPID(row *referenceRow, timeBase bool) ReferencedPID {
	r := ReferencedPID{
		Program: row.key.program, Role: row.key.role, PID: row.key.pid, StreamType: row.streamType, Judged: row.judged(),
		ReferencedPackets: row.packets, ReferencedFrom: row.from, ReferencedUntil: row.until,
	}
	if p, ok := c.gaps.PID(row.key.pid); ok {
		r.Packets, r.FirstOffset, r.LastOffset = p.Packets, p.FirstOffset, p.LastOffset
	}
	if row.packets > 0 {
		r.ReferencedFirstOffset, r.ReferencedLastOffset = row.first, row.last
	}
	if s := row.silence; s.has && timeBase {
		r.MaxSilenceMs, r.MaxSilenceKnown, r.MaxSilenceExact = s.best.ms(), true, s.best.exact()
		r.MaxSilenceStartOffset, r.MaxSilenceEndOffset = s.best.start.offset, s.best.end.offset
	}
	exceeded := r.MaxSilenceKnown && r.MaxSilenceMs > PIDSilenceLimitMs
	switch {
	case !r.Judged && row.packets == 0:
		r.Status, r.Reason = PIDStatusInsufficientData, ReasonIntermittentStreamType
	case !r.Judged:
		r.Status = PIDStatusOK
	case !timeBase:
		r.Status, r.Reason = PIDStatusInsufficientData, ReasonNoPCRTimeBase
	case row.packets == 0 && exceeded:
		r.Status = PIDStatusAbsent
	case row.packets == 0:
		r.Status, r.Reason = PIDStatusInsufficientData, ReasonReferenceShorterThanLimit
	case exceeded:
		r.Status = PIDStatusSilent
	default:
		r.Status = PIDStatusOK
	}
	return r
}

func (c *PIDReferenceCheck) unreferencedResult(timeBase bool) UnreferencedResult {
	r := UnreferencedResult{Status: PIDStatusOK, PIDs: []UnreferencedPID{}}
	switch {
	case !c.assessed:
		r.Status, r.Reason = PIDStatusSkipped, ReasonProgramLayoutsUnknown
		return r
	case !timeBase:
		r.Status, r.Reason = PIDStatusInsufficientData, ReasonNoPCRTimeBase
		return r
	}
	for _, u := range c.unref {
		switch {
		case u.found.runs > 0:
			r.PIDs = append(r.PIDs, u.found.result(u.pid, PIDStatusUnreferenced))
			r.Status, r.Reason = PIDStatusUnreferenced, ""
		case u.undetermined.runs > 0:
			r.PIDs = append(r.PIDs, u.undetermined.result(u.pid, PIDStatusInsufficientData))
			if r.Status == PIDStatusOK {
				r.Status, r.Reason = PIDStatusInsufficientData, ReasonUnreferencedWithinGrace
			}
		}
	}
	slices.SortFunc(r.PIDs, func(a, b UnreferencedPID) int { return cmp.Compare(a.PID, b.PID) })
	return r
}

func (t runTotals) result(pid uint16, status PIDStatus) UnreferencedPID {
	return UnreferencedPID{PID: pid, Status: status, Packets: t.packets, FirstOffset: t.first, LastOffset: t.last, LongestMs: t.longestMs}
}
