package analysis

// PCR-to-PTS/DTS lead of a PES packet.
//
// The lead of a PES packet is how far ahead of its decode time it was
// multiplexed: its DTS (its PTS when it has no DTS, since the DTS is then
// equal to the PTS) minus its time on the program's PCR timeline. The time is
// the PCR value at the first byte of the packet that carries the PES packet's
// payload_unit_start_indicator, interpolated by byte position between the PCRs
// before and after it (PES.Arrival, see ClockTime). It is defined once, here,
// and used by every result that reports it: the PCR-PTS/DTS gap check and
// --dump-timestamp in tsparser and the mux lead report (mux_lead.go), so they
// give the same lead for the same PES packet.
//
//   - Bracket: the time is known only between two PCRs of one timing segment
//     of the PCR timeline, whose segments are those of the PCR step
//     classification (pcr.go). A continuous or wraparound step stays in the
//     segment, so a PES packet between the last PCR before a PCR wraparound
//     and the first PCR after it is measured by interpolating across the
//     wrap. Every other step (declared, pcr_pid_change, backward,
//     forward_jump) starts a new segment, and a PES packet whose PCRs lie in
//     different segments has no lead.
//   - Wraparound: the PTS/DTS is a 33-bit count of the 90 kHz clock and the
//     PCR a count of the 27 MHz clock modulo 2^33*300; both wrap after the
//     same 2^33/90000 s (about 26.5 hours), but at different points of the
//     stream, since one leads the other. The lead is therefore read modulo
//     that period as the signed value nearest to zero (LeadTicks): it is
//     correct whichever of the two wrapped first, as long as its magnitude is
//     below half the period (about 13 hours).

// LeadTicks returns the lead of a 90 kHz timestamp over a 27 MHz PCR time,
// both modulo their periods, as the signed value nearest to zero in 27 MHz
// ticks. arrival may be a PCR value or a time unwrapped past the period.
func LeadTicks(stamp, arrival uint64) int64 {
	d := (stamp*300%PCRWrapTicks + PCRWrapTicks - arrival%PCRWrapTicks) % PCRWrapTicks
	if d >= PCRWrapTicks/2 {
		return int64(d) - int64(PCRWrapTicks)
	}
	return int64(d)
}

// Lead returns the lead of the PES packet in 27 MHz ticks: its DTS, or its
// PTS when it has no DTS, minus its Arrival.Ticks, by LeadTicks. ok is false
// when the packet has no PTS or its Arrival is not known.
func (o PES) Lead() (ticks int64, ok bool) {
	if !o.HasPTS || !o.Arrival.Valid {
		return 0, false
	}
	return LeadTicks(o.stamp(), o.Arrival.Ticks), true
}

// LeadMs returns the same lead in milliseconds, from Arrival.Ms instead of
// the whole ticks of Arrival.Ticks, so it differs from Lead by less than one
// 27 MHz tick. The timestamp is first unwrapped to the period of the arrival,
// as Lead decided, so the lead is the signed shortest difference whichever
// clock wrapped first; when neither wrapped, it is exactly the timestamp
// minus the arrival.
func (o PES) LeadMs() (ms float64, ok bool) {
	ticks, ok := o.Lead()
	if !ok {
		return 0, false
	}
	stamp := o.stamp()
	// stamp*300 + wraps*2^33*300 is Arrival.Ticks + ticks exactly.
	wraps := (int64(o.Arrival.Ticks) + ticks - int64(stamp*300)) / int64(PCRWrapTicks)
	return float64(int64(stamp)+wraps<<33)/90 - o.Arrival.Ms, true
}

// stamp is the timestamp the lead is taken of: the DTS, or the PTS when the
// packet has no DTS.
func (o PES) stamp() uint64 {
	if o.HasDTS {
		return o.DTS
	}
	return o.PTS
}
