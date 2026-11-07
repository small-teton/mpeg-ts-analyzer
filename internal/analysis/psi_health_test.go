package analysis

import (
	"slices"
	"testing"
)

// The tests use a constant-rate multiplex of one 188-byte packet per
// millisecond, so offset/188 is the time in ms.
const testPacket = 188

func at(ms int64) int64 { return ms * testPacket }

func pcrAt(ms int64) PCR {
	return PCR{PID: 0x100, Value: uint64(10_000+ms) * PCRTicksPerMs, Offset: at(ms)}
}

// feed sends a PCR every 40 ms from 0 to endMs and an instance of each table
// at the given times, in time order.
func feed(c *PSIHealthCheck, endMs int64, instances map[PSITable][]int64) {
	for ms := int64(0); ms <= endMs; ms++ {
		if ms%40 == 0 {
			c.ObservePCR(pcrAt(ms))
		}
		for table, times := range instances {
			if slices.Contains(times, ms) {
				c.ObserveInstance(PSIInstance{Table: table, PID: 0x1000, Offset: at(ms)})
			}
		}
	}
}

func table(r PSIHealthResult, t PSITable) PSITableResult {
	for _, row := range r.Tables {
		if row.Table == t {
			return row
		}
	}
	return PSITableResult{}
}

func toMs(ticks uint64) float64 { return float64(ticks) / PCRTicksPerMs }

func TestPSIHealthRepetition(t *testing.T) {
	c := NewPSIHealthCheck()
	c.ObserveAnnounced(PSIAnnounced{Table: PMTTable(1), PID: 0x1000, Offset: at(1)})
	// Announced again on the same PID: nothing changes.
	c.ObserveAnnounced(PSIAnnounced{Table: PMTTable(1), PID: 0x1000, Offset: at(5)})
	c.ObserveAnnounced(PSIAnnounced{Table: PMTTable(2), PID: 0x1010, Offset: at(1)})
	feed(c, 2000, map[PSITable][]int64{
		// Two PATs between the same PCRs (210 and 230 ms), then 600 ms.
		PATTable:    {10, 210, 230, 400, 1000, 1400, 1800},
		PMTTable(1): {20, 220, 420, 620, 820, 1020, 1220, 1420, 1620, 1820},
	})
	r := c.Result()
	if r.Status != StatusNG || !r.TimeBase || r.Segments != 1 || len(r.Tables) != 3 {
		t.Fatalf("result %+v", r)
	}
	if r.Tables[0].Table != PATTable || r.Tables[1].Table != PMTTable(1) || r.Tables[2].Table != PMTTable(2) {
		t.Errorf("order %+v", r.Tables)
	}
	pat := table(r, PATTable)
	// Complete intervals: 200, 20, 170, 600, 400, 400; the open interval to
	// the last PCR (2000) is 200.
	if pat.Status != StatusNG || !slices.Equal(pat.Reasons, []PSIReason{PSIReasonLate}) || pat.Instances != 7 || pat.FirstInstance != at(10) ||
		pat.Repetition.Status != StatusNG || pat.Repetition.Intervals != 6 || toMs(pat.Repetition.MaxTicks) != 600 ||
		pat.Repetition.MaxEnd != at(1000) || pat.Repetition.Late != 1 || pat.Repetition.FirstLate != at(1000) {
		t.Errorf("PAT %+v", pat)
	}
	pmt := table(r, PMTTable(1))
	if pmt.Status != StatusOK || len(pmt.Reasons) != 0 || pmt.Announced != at(1) || pmt.PID != 0x1000 ||
		pmt.Repetition.Status != StatusOK || pmt.Repetition.Intervals != 9 || toMs(pmt.Repetition.MaxTicks) != 200 {
		t.Errorf("PMT 1 %+v", pmt)
	}
	// Never found while followed for 2000 ms: missing.
	missing := table(r, PMTTable(2))
	if missing.Status != StatusNG || !slices.Equal(missing.Reasons, []PSIReason{PSIReasonMissing}) || missing.Instances != 0 ||
		toMs(missing.Repetition.MaxTicks) != 2000 || missing.Repetition.MaxEnd != at(2000) || missing.Repetition.Intervals != 0 {
		t.Errorf("PMT 2 %+v", missing)
	}
}

func TestPSIHealthShortRangeAndNoTimeBase(t *testing.T) {
	// One instance in a 300 ms range: no complete interval.
	c := NewPSIHealthCheck()
	feed(c, 300, map[PSITable][]int64{PATTable: {150}})
	r := c.Result()
	pat := r.Tables[0]
	if r.Status != StatusInsufficientData || pat.Status != StatusInsufficientData || pat.Repetition.Status != StatusInsufficientData ||
		toMs(pat.Repetition.MaxTicks) != 150 || pat.Repetition.Intervals != 0 {
		t.Errorf("short range %+v", r)
	}

	// Instances before the first PCR, and a single Value: no time base.
	c = NewPSIHealthCheck()
	c.ObserveInstance(PSIInstance{Table: PATTable, Offset: 0})
	c.ObservePCR(pcrAt(10))
	c.ObserveInstance(PSIInstance{Table: PATTable, Offset: at(20)})
	r = c.Result()
	if r.TimeBase || r.Segments != 1 || r.Tables[0].Status != StatusInsufficientData || r.Tables[0].Repetition.Status != StatusSkipped ||
		r.Tables[0].Repetition.HasMax || r.Tables[0].Instances != 2 {
		t.Errorf("no time base %+v", r)
	}

	// A problem makes the table NG even without a time base.
	c.ObserveProblem(PSIProblem{Table: PATTable, Kind: PSICRCError, Offset: at(30)})
	if r = c.Result(); r.Status != StatusNG || !slices.Equal(r.Tables[0].Reasons, []PSIReason{PSIReasonCRCError}) {
		t.Errorf("CRC without time base %+v", r)
	}
}

func TestPSIHealthProblems(t *testing.T) {
	c := NewPSIHealthCheck()
	for i, kind := range []PSIProblemKind{PSIUnusable, PSIWrongTableID, PSIIncomplete, PSIMalformed, PSICRCError} {
		c.ObserveProblem(PSIProblem{Table: PATTable, Kind: kind, Offset: at(int64(50 - i))})
	}
	c.ObserveProblem(PSIProblem{Table: PATTable, Kind: PSICRCError, Offset: at(60)})
	// Tables that are not followed are ignored.
	c.ObserveProblem(PSIProblem{Table: PMTTable(9), Kind: PSICRCError, Offset: 0})
	c.ObserveInstance(PSIInstance{Table: PMTTable(9), Offset: 0})
	// An instance ends the table's open interval; without one, the end of the
	// range counts once as incomplete.
	c.ObserveNeverCompleted(PATTable, at(70))
	c.ObserveNeverCompleted(PMTTable(9), at(70))
	r := c.Result()
	pat := r.Tables[0]
	want := []PSIReason{PSIReasonCRCError, PSIReasonMalformed, PSIReasonIncomplete, PSIReasonWrongTableID, PSIReasonUnusable}
	if len(r.Tables) != 1 || pat.Status != StatusNG || !slices.Equal(pat.Reasons, want) || pat.FirstProblem != at(46) ||
		pat.Problems != [psiProblemKinds]int{2, 1, 2, 1, 1} {
		t.Errorf("problems %+v", r)
	}

	c = NewPSIHealthCheck()
	c.ObserveInstance(PSIInstance{Table: PATTable, Offset: 0})
	c.ObserveNeverCompleted(PATTable, at(70))
	if r = c.Result(); r.Tables[0].Problems[PSIIncomplete] != 0 {
		t.Errorf("a completed table counted as never completed: %+v", r)
	}
}

func TestPSIHealthSegments(t *testing.T) {
	c := NewPSIHealthCheck()
	for _, step := range []struct {
		ms       int64
		instance bool
		pcr      *PCR
	}{
		{ms: 0, pcr: &PCR{PID: 0x100, Value: 1000 * PCRTicksPerMs, Offset: at(0)}},
		{ms: 100, instance: true},
		{ms: 300, pcr: &PCR{PID: 0x100, Value: 1300 * PCRTicksPerMs, Offset: at(300)}},
		// Declared discontinuity: the PCR jumps; the open interval 100..300
		// closes and a new segment starts.
		{ms: 400, pcr: &PCR{PID: 0x100, Value: 900_000 * PCRTicksPerMs, DiscontinuityIndicator: true, Offset: at(400)}},
		{ms: 450, instance: true},
		// Undeclared backward step: another segment.
		{ms: 500, pcr: &PCR{PID: 0x100, Value: 800_000 * PCRTicksPerMs, Offset: at(500)}},
		// A forward step over 1 s: another segment, in which a wraparound
		// within 1 s is measured.
		{ms: 600, pcr: &PCR{PID: 0x100, Value: PCRWrapTicks - 50*PCRTicksPerMs, Offset: at(600)}},
		{ms: 700, pcr: &PCR{PID: 0x100, Value: 50 * PCRTicksPerMs, Offset: at(700)}},
		{ms: 750, instance: true},
		// A forward step over 1 s: another segment.
		{ms: 800, pcr: &PCR{PID: 0x100, Value: 5000 * PCRTicksPerMs, Offset: at(800)}},
		// Another PCR_PID: another segment.
		{ms: 900, pcr: &PCR{PID: 0x200, Value: 5100 * PCRTicksPerMs, Offset: at(900)}},
		{ms: 950, instance: true},
		{ms: 1500, pcr: &PCR{PID: 0x200, Value: 5700 * PCRTicksPerMs, Offset: at(1500)}},
	} {
		if step.pcr != nil {
			c.ObservePCR(*step.pcr)
		}
		if step.instance {
			c.ObserveInstance(PSIInstance{Table: PATTable, Offset: at(step.ms)})
		}
	}
	r := c.Result()
	pat := r.Tables[0]
	// Segments: [0,300] with the open intervals 0..100 and 100..300;
	// [400] and [500], where nothing is timed; [600,700], where the
	// instance at 750 comes after the last PCR, so the open interval
	// 600..700 ends there; [800]; [900,1500] with the open intervals 0..50
	// and 50..600, which is late.
	if r.Segments != 6 || pat.Repetition.Intervals != 0 || pat.Repetition.Late != 1 || toMs(pat.Repetition.MaxTicks) != 600-50 ||
		pat.Repetition.MaxEnd != at(1500) || pat.Repetition.FirstLate != at(1500) || pat.Status != StatusNG ||
		!slices.Equal(pat.Reasons, []PSIReason{PSIReasonLate}) || pat.Repetition.Status != StatusNG {
		t.Errorf("segments %+v", r)
	}
}

func TestPSIHealthAnnouncements(t *testing.T) {
	c := NewPSIHealthCheck()
	// Withdrawing a PMT that was never announced does nothing.
	c.ObserveWithdrawn(PSIWithdrawn{Table: PMTTable(1), Offset: 0})
	c.ObservePCR(pcrAt(0))
	// Announced between two PCRs with an instance before the next Value: the
	// open interval starts at the next PCR, after the instance, so the
	// instance only starts the first complete interval.
	c.ObserveAnnounced(PSIAnnounced{Table: PMTTable(1), PID: 0x1000, Offset: at(5)})
	c.ObserveInstance(PSIInstance{Table: PMTTable(1), PID: 0x1000, Version: 3, Offset: at(10)})
	c.ObservePCR(pcrAt(40))
	c.ObservePCR(pcrAt(80))
	c.ObserveInstance(PSIInstance{Table: PMTTable(1), PID: 0x1000, Version: 4, Offset: at(100)})
	c.ObservePCR(pcrAt(120))
	// Moved to another PID: the open interval 100..120 closes.
	c.ObserveAnnounced(PSIAnnounced{Table: PMTTable(1), PID: 0x1001, Offset: at(130)})
	c.ObservePCR(pcrAt(160))
	c.ObserveInstance(PSIInstance{Table: PMTTable(1), PID: 0x1001, Version: 4, Offset: at(170)})
	c.ObservePCR(pcrAt(200))
	// Withdrawn, withdrawn again (ignored), announced again: the first
	// announcement is kept; the withdrawal is cleared.
	c.ObserveWithdrawn(PSIWithdrawn{Table: PMTTable(1), Offset: at(210)})
	c.ObserveWithdrawn(PSIWithdrawn{Table: PMTTable(1), Offset: at(220)})
	c.ObservePCR(pcrAt(240))
	r := c.Result()
	pmt := table(r, PMTTable(1))
	// Intervals: 10..100 complete (90), 100..120 open, 160..170 open,
	// 170..200 open. The identity on 0x1001 has no complete interval of its
	// own, so the repetition is not shown.
	if pmt.Announced != at(5) || !pmt.IsWithdrawn || pmt.Withdrawn != at(210) || pmt.PID != 0x1001 || pmt.Instances != 3 ||
		pmt.VersionChanges != 1 || pmt.Repetition.Intervals != 1 || toMs(pmt.Repetition.MaxTicks) != 90 || pmt.Status != StatusInsufficientData {
		t.Errorf("announcements %+v", pmt)
	}
	c.ObserveAnnounced(PSIAnnounced{Table: PMTTable(1), PID: 0x1001, Offset: at(230)})
	if pmt = table(c.Result(), PMTTable(1)); pmt.Announced != at(5) || pmt.IsWithdrawn {
		t.Errorf("announced again %+v", pmt)
	}
}

// A moved or re-announced PMT starts without the evidence of its earlier
// identity, and keeps the late or missing verdict of that identity.
func TestPSIHealthIdentityEvidence(t *testing.T) {
	pmt := PMTTable(1)
	healthyOldIdentity := func() *PSIHealthCheck {
		c := NewPSIHealthCheck()
		c.ObserveAnnounced(PSIAnnounced{Table: pmt, PID: 0x1000, Offset: at(0)})
		c.ObservePCR(pcrAt(0))
		c.ObserveInstance(PSIInstance{Table: pmt, PID: 0x1000, Offset: at(100)})
		c.ObservePCR(pcrAt(200))
		c.ObserveInstance(PSIInstance{Table: pmt, PID: 0x1000, Offset: at(300)})
		c.ObservePCR(pcrAt(400))
		if got := table(c.Result(), pmt); got.Status != StatusOK {
			t.Fatalf("old identity: %+v", got)
		}
		return c
	}

	// One instance on the new PID: no complete interval of its own.
	c := healthyOldIdentity()
	c.ObserveAnnounced(PSIAnnounced{Table: pmt, PID: 0x1001, Offset: at(450)})
	c.ObservePCR(pcrAt(480))
	c.ObserveInstance(PSIInstance{Table: pmt, PID: 0x1001, Offset: at(500)})
	c.ObservePCR(pcrAt(600))
	if got := table(c.Result(), pmt); got.Status != StatusInsufficientData || got.Repetition.Status != StatusInsufficientData ||
		got.Instances != 3 || got.Repetition.Intervals != 1 {
		t.Errorf("new identity reused old evidence: %+v", got)
	}

	// Nothing on the new PID for more than the limit: missing, although the
	// table had instances before.
	c = healthyOldIdentity()
	c.ObserveAnnounced(PSIAnnounced{Table: pmt, PID: 0x1001, Offset: at(450)})
	for ms := int64(480); ms <= 1200; ms += 40 {
		c.ObservePCR(pcrAt(ms))
	}
	if got := table(c.Result(), pmt); got.Status != StatusNG || !slices.Equal(got.Reasons, []PSIReason{PSIReasonMissing}) {
		t.Errorf("missing after the move: %+v", got)
	}

	// A late identity keeps the table late after a healthy re-announcement;
	// a missing one keeps it missing.
	c = NewPSIHealthCheck()
	c.ObserveAnnounced(PSIAnnounced{Table: pmt, PID: 0x1000, Offset: at(0)})
	c.ObservePCR(pcrAt(0))
	c.ObserveInstance(PSIInstance{Table: pmt, PID: 0x1000, Offset: at(10)})
	c.ObservePCR(pcrAt(600))
	c.ObserveInstance(PSIInstance{Table: pmt, PID: 0x1000, Offset: at(700)})
	c.ObservePCR(pcrAt(720))
	c.ObserveWithdrawn(PSIWithdrawn{Table: pmt, Offset: at(730)})
	c.ObserveAnnounced(PSIAnnounced{Table: pmt, PID: 0x1000, Offset: at(740)})
	for ms := int64(760); ms <= 1000; ms += 40 {
		c.ObservePCR(pcrAt(ms))
		if ms%200 == 0 {
			c.ObserveInstance(PSIInstance{Table: pmt, PID: 0x1000, Offset: at(ms + 1)})
		}
	}
	if got := table(c.Result(), pmt); got.Status != StatusNG || !slices.Equal(got.Reasons, []PSIReason{PSIReasonLate}) || got.Repetition.Status != StatusNG {
		t.Errorf("late identity: %+v", got)
	}
	c.ObserveAnnounced(PSIAnnounced{Table: pmt, PID: 0x1002, Offset: at(1010)})
	for ms := int64(1040); ms <= 1800; ms += 40 {
		c.ObservePCR(pcrAt(ms))
	}
	c.ObserveAnnounced(PSIAnnounced{Table: pmt, PID: 0x1003, Offset: at(1810)})
	if got := table(c.Result(), pmt); !slices.Equal(got.Reasons, []PSIReason{PSIReasonMissing, PSIReasonLate}) {
		t.Errorf("missing identity: %+v", got)
	}
}
