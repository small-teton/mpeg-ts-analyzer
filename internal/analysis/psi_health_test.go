package analysis

import (
	"slices"
	"testing"
)

// The tests use a constant-rate multiplex of one 188-byte packet per
// millisecond, so offset/188 is the time in ms.
const testPacket = 188

func at(ms int64) int64 { return ms * testPacket }

func pcrAt(ms int64) PCRTime {
	return PCRTime{PID: 0x100, PCR: uint64(10_000+ms) * ticksPerMs, Offset: at(ms)}
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

func ms(ticks uint64) float64 { return float64(ticks) / ticksPerMs }

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
		pat.Repetition.Status != StatusNG || pat.Repetition.Intervals != 6 || ms(pat.Repetition.MaxTicks) != 600 ||
		pat.Repetition.MaxEnd != at(1000) || pat.Repetition.Late != 1 || pat.Repetition.FirstLate != at(1000) {
		t.Errorf("PAT %+v", pat)
	}
	pmt := table(r, PMTTable(1))
	if pmt.Status != StatusOK || len(pmt.Reasons) != 0 || pmt.Announced != at(1) || pmt.PID != 0x1000 ||
		pmt.Repetition.Status != StatusOK || pmt.Repetition.Intervals != 9 || ms(pmt.Repetition.MaxTicks) != 200 {
		t.Errorf("PMT 1 %+v", pmt)
	}
	// Never found while followed for 2000 ms: missing.
	missing := table(r, PMTTable(2))
	if missing.Status != StatusNG || !slices.Equal(missing.Reasons, []PSIReason{PSIReasonMissing}) || missing.Instances != 0 ||
		ms(missing.Repetition.MaxTicks) != 2000 || missing.Repetition.MaxEnd != at(2000) || missing.Repetition.Intervals != 0 {
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
		ms(pat.Repetition.MaxTicks) != 150 || pat.Repetition.Intervals != 0 {
		t.Errorf("short range %+v", r)
	}

	// Instances before the first PCR, and a single PCR: no time base.
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
		pcr      *PCRTime
	}{
		{ms: 0, pcr: &PCRTime{PID: 0x100, PCR: 1000 * ticksPerMs, Offset: at(0)}},
		{ms: 100, instance: true},
		{ms: 300, pcr: &PCRTime{PID: 0x100, PCR: 1300 * ticksPerMs, Offset: at(300)}},
		// Declared discontinuity: the PCR jumps; the open interval 100..300
		// closes and a new segment starts.
		{ms: 400, pcr: &PCRTime{PID: 0x100, PCR: 900_000 * ticksPerMs, Discontinuity: true, Offset: at(400)}},
		{ms: 450, instance: true},
		// Undeclared backward step: another segment.
		{ms: 500, pcr: &PCRTime{PID: 0x100, PCR: 800_000 * ticksPerMs, Offset: at(500)}},
		// A forward step over 1 s: another segment, in which a wraparound
		// within 1 s is measured.
		{ms: 600, pcr: &PCRTime{PID: 0x100, PCR: psiClockWrap - 50*ticksPerMs, Offset: at(600)}},
		{ms: 700, pcr: &PCRTime{PID: 0x100, PCR: 50 * ticksPerMs, Offset: at(700)}},
		{ms: 750, instance: true},
		// A forward step over 1 s: another segment.
		{ms: 800, pcr: &PCRTime{PID: 0x100, PCR: 5000 * ticksPerMs, Offset: at(800)}},
		// Another PCR_PID: another segment.
		{ms: 900, pcr: &PCRTime{PID: 0x200, PCR: 5100 * ticksPerMs, Offset: at(900)}},
		{ms: 950, instance: true},
		{ms: 1500, pcr: &PCRTime{PID: 0x200, PCR: 5700 * ticksPerMs, Offset: at(1500)}},
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
	if r.Segments != 6 || pat.Repetition.Intervals != 0 || pat.Repetition.Late != 1 || ms(pat.Repetition.MaxTicks) != 600-50 ||
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
	// Announced between two PCRs with an instance before the next PCR: the
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
	// 170..200 open.
	if pmt.Announced != at(5) || !pmt.IsWithdrawn || pmt.Withdrawn != at(210) || pmt.PID != 0x1001 || pmt.Instances != 3 ||
		pmt.VersionChanges != 1 || pmt.Repetition.Intervals != 1 || ms(pmt.Repetition.MaxTicks) != 90 || pmt.Status != StatusOK {
		t.Errorf("announcements %+v", pmt)
	}
	c.ObserveAnnounced(PSIAnnounced{Table: PMTTable(1), PID: 0x1001, Offset: at(230)})
	if pmt = table(c.Result(), PMTTable(1)); pmt.Announced != at(5) || pmt.IsWithdrawn {
		t.Errorf("announced again %+v", pmt)
	}
}
