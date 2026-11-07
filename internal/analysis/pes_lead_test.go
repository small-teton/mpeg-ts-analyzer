package analysis

import (
	"math"
	"testing"
)

// TestPESLead covers the lead of a PES packet in both units, across both
// orders of the PTS/DTS and PCR wraparounds.
func TestPESLead(t *testing.T) {
	const wrap90 = uint64(1) << 33
	at := func(ticks uint64, fraction float64) ClockTime {
		return ClockTime{Valid: true, Ticks: ticks, Ms: (float64(ticks) + fraction) / PCRTicksPerMs}
	}
	for _, tc := range []struct {
		name    string
		o       PES
		ticks   int64
		ms      float64
		measure bool
	}{
		{"no PTS", PES{Arrival: at(0, 0)}, 0, 0, false},
		{"no arrival", PES{HasPTS: true, PTS: 90_000}, 0, 0, false},
		{"PTS", PES{HasPTS: true, PTS: 90_000, Arrival: at(9*ms, 0)}, 991 * ms, 991, true},
		{"DTS before PTS", PES{HasPTS: true, PTS: 99_000, HasDTS: true, DTS: 90_000, Arrival: at(9*ms, 0)}, 991 * ms, 991, true},
		{"sub-tick arrival", PES{HasPTS: true, PTS: 90_000, Arrival: at(9*ms, 0.5)}, 991 * ms, 991 - 0.5/PCRTicksPerMs, true},
		{"PTS wrapped, PCR not", PES{HasPTS: true, PTS: 690 * 90, Arrival: at(PCRWrapTicks-10*ms, 0)}, 700 * ms, 700, true},
		{"PCR wrapped, PTS not", PES{HasPTS: true, PTS: wrap90 - 20*90, Arrival: at(30*ms, 0)}, -50 * ms, -50, true},
		{"unwrapped arrival, PTS not wrapped", PES{HasPTS: true, PTS: wrap90 - 20*90, Arrival: at(PCRWrapTicks+30*ms, 0)}, -50 * ms, -50, true},
		{"unwrapped arrival twice, PTS wrapped", PES{HasPTS: true, PTS: 700 * 90, Arrival: at(2*PCRWrapTicks+10*ms, 0)}, 690 * ms, 690, true},
	} {
		ticks, ok := tc.o.Lead()
		if ok != tc.measure || ticks != tc.ticks {
			t.Errorf("%s: Lead() = %d, %v; want %d, %v", tc.name, ticks, ok, tc.ticks, tc.measure)
		}
		got, ok := tc.o.LeadMs()
		if ok != tc.measure || math.Abs(got-tc.ms) > 1e-6 {
			t.Errorf("%s: LeadMs() = %f, %v; want %f, %v", tc.name, got, ok, tc.ms, tc.measure)
		}
	}
	// Without a wraparound, LeadMs is exactly the timestamp minus the
	// arrival, the computation of the PCR-PTS/DTS gap before the lead was
	// made wraparound-aware.
	arrival := ClockTime{Valid: true, Ticks: 1_234_567, Ms: 45.72470370370371}
	o := PES{HasPTS: true, PTS: 90_000, Arrival: arrival}
	if got, _ := o.LeadMs(); got != float64(90_000)/90-arrival.Ms {
		t.Errorf("LeadMs() = %v, want %v", got, float64(90_000)/90-arrival.Ms)
	}
}
