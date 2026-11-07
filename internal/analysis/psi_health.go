package analysis

import (
	"cmp"
	"slices"
)

// PSI health: availability and repetition of the PAT and of the PMT of every
// announced program.
//
// What is generic MPEG-2 and what comes from a monitoring profile:
//
//   - ISO/IEC 13818-1 §2.4.4 defines where the tables are (the PAT on PID
//     0x0000 with table_id 0x00, each PMT with table_id 0x02 on the PID the
//     PAT assigns to its program), the section syntax and the CRC_32. A
//     section on PID 0x0000 with another table_id, a section with an invalid
//     CRC_32 or broken syntax, and a table whose sections never all arrive
//     are therefore findings under any profile.
//   - ISO/IEC 13818-1 sets no maximum repetition interval for the PAT or the
//     PMT. The 500 ms limit used here is the one of ETSI TR 101 290 V1.4.1
//     §5.2.1, indicators 1.3.a (PAT_error_2) and 1.5.a (PMT_error_2):
//     "sections with table_id 0x00 [0x02] do not occur at least every 0,5 s".
//     Other broadcast profiles require shorter intervals; this check applies
//     no profile-specific limit beyond this one.
//   - TR 101 290 1.3.a and 1.5.a also list a scrambled PAT or PMT packet as an
//     error. Packets the parser does not trust (transport_error_indicator or
//     a scrambled payload, see TransportTrustCheck) are counted per table as
//     unusable.
//
// Time basis. A file has no packet-arrival times, so the time of a packet is
// interpolated by byte position between the two PCRs around it on one
// reference PCR_PID, as for the PCR-PTS/DTS gap. A table instance is the
// packet that completes the table. Each PCR is classified by PCRClock, the
// same classification as every other PCR-based result: time is measured only
// across comparable steps (continuous or wraparound), and every other step
// (declared, pcr_pid_change, backward, forward_jump) starts a new PCR
// segment. Without two PCRs in one segment there is no time base.
//
// Intervals. Within a segment, the interval between two consecutive instances
// of a table is a complete interval. The time from the start of a segment (or
// from the first PCR after the table was announced) to the first instance,
// and from the last instance to the end of the segment, are open intervals:
// the table may have been sent just outside them, so they are lower bounds of
// the real interval. Both kinds are compared with the limit, since an open
// interval longer than the limit proves a longer real one; only complete
// intervals show that the table repeats, so a table is OK only when at least
// one complete interval was measured. Comparable instances are those of one
// table identity: the PAT on PID 0, or the PMT of one program_number on one
// PID. A version change keeps the identity (the table must still be repeated
// on the same PID); a PMT moved to another PID by a PAT change, or a program
// removed and announced again, starts a new identity. The counts and the
// longest interval of a table cover all its identities, but evidence does not
// carry over: an identity whose intervals were late, or that was followed
// for longer than the limit without any instance, keeps the table late or
// missing, and the table is OK only when its latest identity has a complete
// interval of its own.
//
// State is a fixed-size record per table identity, bounded by the number of
// program_numbers announced in the range (at most 65,535) plus the PAT, never
// by the number of packets.

// PSIRepetitionLimitTicks is the repetition limit in 27 MHz ticks: 500 ms.
const PSIRepetitionLimitTicks = 500 * PCRTicksPerMs

// PSITableKind is the kind of table.
type PSITableKind int

const (
	// PSITablePAT is the program association table.
	PSITablePAT PSITableKind = iota
	// PSITablePMT is the program map table of one program.
	PSITablePMT
)

// PSITable identifies a followed table: the PAT (ProgramNumber 0) or the PMT
// of one program_number.
type PSITable struct {
	Kind          PSITableKind
	ProgramNumber uint16
}

// PATTable is the identity of the PAT.
var PATTable = PSITable{Kind: PSITablePAT}

// PMTTable is the identity of the PMT of programNumber.
func PMTTable(programNumber uint16) PSITable {
	return PSITable{Kind: PSITablePMT, ProgramNumber: programNumber}
}

// PSIProblemKind classifies a problem with the data of a table.
type PSIProblemKind int

const (
	// PSICRCError is a section whose CRC_32 does not check.
	PSICRCError PSIProblemKind = iota
	// PSIMalformed is a section that cannot be read: too short, without
	// section_syntax_indicator, a section_number beyond
	// last_section_number, an invalid pointer_field, or fields that do not
	// parse.
	PSIMalformed
	// PSIIncomplete is collected data that had to be discarded before a
	// table completed: a section cut short by the next section start, or the
	// sections of a version discarded because they changed without a version
	// change. A table that never completed while sections of it arrived is
	// also counted once at the end.
	PSIIncomplete
	// PSIWrongTableID is a section with a table_id other than 0x00 on the
	// PAT PID.
	PSIWrongTableID
	// PSIUnusable is a packet of the table's PID that the parser did not
	// read because of transport_error_indicator or a scrambled payload.
	PSIUnusable
	psiProblemKinds
)

// PSIAnnounced is a PMT announced by an applied PAT, from the packet after
// Offset on, on PID. A PMT moved to another PID is announced again.
type PSIAnnounced struct {
	Table  PSITable
	PID    uint16
	Offset int64
}

// PSIWithdrawn is a PMT whose program an applied PAT no longer lists, from
// the packet at Offset.
type PSIWithdrawn struct {
	Table  PSITable
	Offset int64
}

// PSIInstance is one complete current table: every section of one version
// arrived valid. Offset is the packet that completed it.
type PSIInstance struct {
	Table   PSITable
	PID     uint16
	Version uint8
	Offset  int64
}

// PSIProblem is one problem with the data of a table at the packet at Offset.
type PSIProblem struct {
	Table  PSITable
	Kind   PSIProblemKind
	Offset int64
}

// psiClock is the PCR time base: the last PCR and the elapsed time of the
// current segment.
type psiClock struct {
	steps    PCRClock
	running  bool // a segment has started
	offset   int64
	elapsed  uint64 // ticks since the first PCR of the segment
	segments int
	measured bool // some segment had two PCRs
}

// psiPending are the instances of a table since the last PCR, which the next
// PCR of the segment times.
type psiPending struct {
	count               int
	first, last         int64
	maxGapBytes, maxEnd int64
}

type psiTableState struct {
	pid                       uint16
	active                    bool
	announced, withdrawn      int64
	hasAnnounced, isWithdrawn bool
	instances                 int
	firstInstance             int64
	version                   uint8
	versionChanges            int
	problems                  [psiProblemKinds]int
	firstProblem              int64
	anyProblem                bool

	// awaitingStart: the table became active and the next PCR starts its
	// first open interval.
	awaitingStart bool
	hasRef        bool
	refTicks      uint64 // elapsed ticks of the last instance, or of the open start
	refInstance   bool
	pending       psiPending

	intervals int // complete intervals of every identity
	maxTicks  uint64
	maxEnd    int64
	hasMax    bool
	late      int
	// The evidence of the current identity, and what earlier identities left.
	identityInstances, identityIntervals, identityLate int
	missingSeen, lateSeen                              bool
	firstLate                                          int64
}

// PSIHealthCheck follows the PAT and the PMT of every announced program.
type PSIHealthCheck struct {
	clock  psiClock
	tables map[PSITable]*psiTableState
}

// NewPSIHealthCheck returns a check that follows the PAT from the first
// packet of the range.
func NewPSIHealthCheck() *PSIHealthCheck {
	c := &PSIHealthCheck{tables: make(map[PSITable]*psiTableState)}
	c.tables[PATTable] = &psiTableState{active: true, awaitingStart: true}
	return c
}

// ObserveAnnounced starts following a PMT. A PMT already followed on another
// PID, or one withdrawn earlier, starts a new identity: its open interval is
// closed and the count of intervals continues.
func (c *PSIHealthCheck) ObserveAnnounced(o PSIAnnounced) {
	t := c.tables[o.Table]
	if t == nil {
		t = &psiTableState{}
		c.tables[o.Table] = t
	}
	if t.active && t.pid == o.PID {
		return
	}
	if t.active {
		c.close(t)
	}
	t.fold()
	t.pid, t.active, t.isWithdrawn = o.PID, true, false
	t.awaitingStart, t.hasRef, t.pending = true, false, psiPending{}
	if !t.hasAnnounced {
		t.announced, t.hasAnnounced = o.Offset, true
	}
}

// fold ends the current identity: its late or missing verdict is kept, and the
// next identity starts without evidence.
func (t *psiTableState) fold() {
	missing, late := t.identityVerdict()
	t.missingSeen, t.lateSeen = t.missingSeen || missing, t.lateSeen || late
	t.identityInstances, t.identityIntervals, t.identityLate = 0, 0, 0
}

// identityVerdict reports whether the current identity was followed for
// longer than the limit without any instance (missing) or had an interval
// over the limit between or after instances (late).
func (t *psiTableState) identityVerdict() (missing, late bool) {
	return t.identityLate > 0 && t.identityInstances == 0, t.identityLate > 0 && t.identityInstances > 0
}

// ObserveWithdrawn stops following a PMT.
func (c *PSIHealthCheck) ObserveWithdrawn(o PSIWithdrawn) {
	t := c.tables[o.Table]
	if t == nil || !t.active {
		return
	}
	c.close(t)
	t.active, t.withdrawn, t.isWithdrawn = false, o.Offset, true
}

// ObserveInstance counts one complete table.
func (c *PSIHealthCheck) ObserveInstance(o PSIInstance) {
	t := c.tables[o.Table]
	if t == nil || !t.active {
		return
	}
	t.pid = o.PID
	if t.instances == 0 {
		t.firstInstance = o.Offset
	} else if o.Version != t.version {
		t.versionChanges++
	}
	t.instances++
	t.identityInstances++
	t.version = o.Version
	if !c.clock.running {
		return
	}
	p := &t.pending
	if p.count == 0 {
		p.first = o.Offset
	} else if gap := o.Offset - p.last; gap > p.maxGapBytes {
		p.maxGapBytes, p.maxEnd = gap, o.Offset
	}
	p.last = o.Offset
	p.count++
}

// ObserveProblem counts one problem with the data of a table.
func (c *PSIHealthCheck) ObserveProblem(o PSIProblem) {
	t := c.tables[o.Table]
	if t == nil || !t.active {
		return
	}
	t.problems[o.Kind]++
	if !t.anyProblem || o.Offset < t.firstProblem {
		t.firstProblem = o.Offset
	}
	t.anyProblem = true
}

// ObservePCR advances the time base with one PCR of the reference PCR_PID,
// as the parser gives it to PCRCheck.
func (c *PSIHealthCheck) ObservePCR(o PCR) {
	clock := &c.clock
	if step := clock.steps.Observe(o); step.Comparable() {
		c.advance(o.Offset, step.Ticks)
		return
	}
	// A new segment: close every open interval at the end of the previous
	// one, then start the next at this PCR.
	if clock.running {
		for _, t := range c.tables {
			if t.active {
				c.close(t)
			}
		}
	}
	clock.running, clock.offset, clock.elapsed, clock.segments = true, o.Offset, 0, clock.segments+1
	for _, t := range c.tables {
		if t.active {
			t.awaitingStart = false
			t.hasRef, t.refTicks, t.refInstance = true, 0, false
			t.pending = psiPending{}
		}
	}
}

// advance times the instances since the previous PCR with the new PCR of the
// same segment.
func (c *PSIHealthCheck) advance(offset int64, delta uint64) {
	clock := &c.clock
	prevElapsed, prevOffset := clock.elapsed, clock.offset
	bytes := offset - prevOffset
	at := func(offset int64) uint64 {
		return prevElapsed + uint64(float64(delta)*float64(offset-prevOffset)/float64(bytes))
	}
	for _, t := range c.tables {
		if !t.active {
			continue
		}
		if t.awaitingStart {
			// Announced since the previous PCR: the open interval starts here.
			t.awaitingStart = false
			t.hasRef, t.refTicks, t.refInstance = true, prevElapsed+delta, false
		}
		p := t.pending
		if p.count == 0 {
			continue
		}
		first := at(p.first)
		if t.hasRef && first >= t.refTicks {
			t.record(first-t.refTicks, p.first, t.refInstance)
		}
		if p.count > 1 {
			t.intervals += p.count - 2
			t.identityIntervals += p.count - 2
			t.record(uint64(float64(delta)*float64(p.maxGapBytes)/float64(bytes)), p.maxEnd, true)
		}
		t.hasRef, t.refTicks, t.refInstance = true, at(p.last), true
		t.pending = psiPending{}
	}
	clock.offset, clock.elapsed = offset, prevElapsed+delta
	clock.measured = true
}

// record keeps an interval of ticks that ends at offset; complete tells a
// complete interval from an open one.
func (t *psiTableState) record(ticks uint64, end int64, complete bool) {
	if complete {
		t.intervals++
		t.identityIntervals++
	}
	if !t.hasMax || ticks > t.maxTicks {
		t.maxTicks, t.maxEnd, t.hasMax = ticks, end, true
	}
	if ticks > PSIRepetitionLimitTicks {
		if t.late == 0 {
			t.firstLate = end
		}
		t.late++
		t.identityLate++
	}
}

// close ends the table's open interval at the last PCR of the segment.
func (c *PSIHealthCheck) close(t *psiTableState) {
	if ticks, end, ok := c.tail(t); ok {
		t.record(ticks, end, false)
	}
	t.hasRef, t.pending, t.awaitingStart = false, psiPending{}, false
}

// tail is the open interval of a table from its last timed instance (or the
// start of its open interval) to the last PCR of the segment, without
// changing the state. Instances after the last PCR end the interval there:
// the real interval to them is at least as long.
func (c *PSIHealthCheck) tail(t *psiTableState) (uint64, int64, bool) {
	if !c.clock.running || !t.hasRef || c.clock.elapsed <= t.refTicks {
		return 0, 0, false
	}
	end := c.clock.offset
	if t.pending.count > 0 {
		end = t.pending.first
	}
	return c.clock.elapsed - t.refTicks, end, true
}

// PSIRepetition is the repetition result of one table.
type PSIRepetition struct {
	// Status is StatusOK when the latest identity has at least one complete
	// interval and no interval of any identity exceeded the limit, StatusNG
	// when one did, StatusInsufficientData when none did but the latest
	// identity has no complete interval, and StatusSkipped without a time
	// base.
	Status Status
	// Intervals is the number of complete intervals of every identity.
	Intervals int
	// MaxTicks is the longest interval, complete or open, in 27 MHz ticks;
	// MaxEnd is the offset where it ends. Both are set when HasMax.
	MaxTicks uint64
	MaxEnd   int64
	HasMax   bool
	// Late is the number of intervals longer than the limit; FirstLate is
	// where the first one ends.
	Late      int
	FirstLate int64
}

// PSITableResult is the result of one table.
type PSITableResult struct {
	Table PSITable
	// PID is the table's PID (the latest one for a moved PMT).
	PID uint16
	// Status is StatusNG with at least one reason, StatusInsufficientData when
	// there is no reason but the repetition is not OK, or StatusOK.
	Status  Status
	Reasons []PSIReason
	// Announced is where a PMT was first announced and Withdrawn where it
	// was last withdrawn, when IsWithdrawn.
	Announced   int64
	Withdrawn   int64
	IsWithdrawn bool
	Instances   int
	// FirstInstance is the offset of the first instance when Instances > 0.
	FirstInstance  int64
	VersionChanges int
	// Problems counts each PSIProblemKind; FirstProblem is the offset of the
	// earliest one when any count is not zero.
	Problems     [psiProblemKinds]int
	FirstProblem int64
	Repetition   PSIRepetition
}

// PSIReason is why a table is NG.
type PSIReason int

const (
	// PSIReasonMissing: no instance, and the table was followed with a time
	// base for longer than the limit.
	PSIReasonMissing PSIReason = iota
	// PSIReasonLate: an interval was longer than the limit.
	PSIReasonLate
	// PSIReasonCRCError and the following reasons: at least one problem of
	// the PSIProblemKind of the same name.
	PSIReasonCRCError
	PSIReasonMalformed
	PSIReasonIncomplete
	PSIReasonWrongTableID
	PSIReasonUnusable
)

// PSIHealthResult is the result of every table: the PAT first, then the PMTs
// in program_number order. Status is the worst table status (NG before
// insufficient data before OK). Segments is the number of PCR segments and
// TimeBase reports that at least one had two PCRs.
type PSIHealthResult struct {
	Status   Status
	TimeBase bool
	Segments int
	Tables   []PSITableResult
}

// ObserveNeverCompleted counts one PSIIncomplete problem at offset, the end
// of the range, for a table without any instance whose sections were still
// being collected there. A table with instances is not affected: its sections
// at the end of the range are only the next repetition in progress.
func (c *PSIHealthCheck) ObserveNeverCompleted(table PSITable, offset int64) {
	if t := c.tables[table]; t != nil && t.instances == 0 {
		c.ObserveProblem(PSIProblem{Table: table, Kind: PSIIncomplete, Offset: offset})
	}
}

// Result returns the result of the range observed so far: every open
// interval ends at the last PCR.
func (c *PSIHealthCheck) Result() PSIHealthResult {
	r := PSIHealthResult{Status: StatusOK, TimeBase: c.clock.measured, Segments: c.clock.segments}
	for table, t := range c.tables {
		r.Tables = append(r.Tables, c.tableResult(table, t))
	}
	slices.SortFunc(r.Tables, func(a, b PSITableResult) int {
		return cmp.Or(cmp.Compare(a.Table.Kind, b.Table.Kind), cmp.Compare(a.Table.ProgramNumber, b.Table.ProgramNumber))
	})
	for _, t := range r.Tables {
		switch {
		case t.Status == StatusNG:
			r.Status = StatusNG
		case t.Status == StatusInsufficientData && r.Status == StatusOK:
			r.Status = StatusInsufficientData
		}
	}
	return r
}

func (c *PSIHealthCheck) tableResult(table PSITable, t *psiTableState) PSITableResult {
	final := *t
	if final.active {
		if ticks, end, ok := c.tail(&final); ok {
			final.record(ticks, end, false)
		}
	}
	r := PSITableResult{
		Table: table, PID: t.pid, Reasons: []PSIReason{},
		Announced: t.announced, Withdrawn: t.withdrawn, IsWithdrawn: t.isWithdrawn,
		Instances: t.instances, FirstInstance: t.firstInstance, VersionChanges: t.versionChanges,
		Problems: t.problems, FirstProblem: t.firstProblem,
		Repetition: PSIRepetition{
			Intervals: final.intervals, MaxTicks: final.maxTicks, MaxEnd: final.maxEnd, HasMax: final.hasMax,
			Late: final.late, FirstLate: final.firstLate,
		},
	}
	switch {
	case !c.clock.measured:
		r.Repetition.Status = StatusSkipped
	case final.late > 0:
		r.Repetition.Status = StatusNG
	case final.identityIntervals > 0:
		r.Repetition.Status = StatusOK
	default:
		r.Repetition.Status = StatusInsufficientData
	}
	missing, late := final.identityVerdict()
	if missing || final.missingSeen {
		r.Reasons = append(r.Reasons, PSIReasonMissing)
	}
	if late || final.lateSeen {
		r.Reasons = append(r.Reasons, PSIReasonLate)
	}
	for kind, reason := range []PSIReason{PSIReasonCRCError, PSIReasonMalformed, PSIReasonIncomplete, PSIReasonWrongTableID, PSIReasonUnusable} {
		if t.problems[kind] > 0 {
			r.Reasons = append(r.Reasons, reason)
		}
	}
	switch {
	case len(r.Reasons) > 0:
		r.Status = StatusNG
	case r.Repetition.Status != StatusOK:
		r.Status = StatusInsufficientData
	default:
		r.Status = StatusOK
	}
	return r
}
