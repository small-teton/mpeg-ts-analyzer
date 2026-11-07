package analysis

import "slices"

// TransportTrustCheck counts, per PID, the packets marked with
// transport_error_indicator and the packets with a non-zero
// transport_scrambling_control, and how many of them the parser left out of
// the analysis. Its state is one counter pair and one label per PID for each
// condition, so it is bounded by the 8192 possible PIDs regardless of the
// input length.
type TransportTrustCheck struct {
	labels         pidLabels
	transportError map[uint16]*affectedPID
	scrambled      map[uint16]*affectedPID
}

type affectedPID struct {
	packets, excluded int
	first             int64
}

// NewTransportTrustCheck returns a check with no affected packets and no named
// PIDs.
func NewTransportTrustCheck() *TransportTrustCheck {
	return &TransportTrustCheck{
		labels:         make(pidLabels),
		transportError: make(map[uint16]*affectedPID),
		scrambled:      make(map[uint16]*affectedPID),
	}
}

// ObserveLayout names the PIDs of an applied program layout in the result, as
// ContinuityCheck.ObserveLayout does.
func (c *TransportTrustCheck) ObserveLayout(layout ProgramLayout) { c.labels.observe(layout) }

// ObserveTransportError counts one packet with transport_error_indicator set.
func (c *TransportTrustCheck) ObserveTransportError(o TransportError) {
	count(c.transportError, o.PID, o.Offset, o.Excluded)
}

// ObserveScrambled counts one packet with a non-zero
// transport_scrambling_control.
func (c *TransportTrustCheck) ObserveScrambled(o Scrambled) {
	count(c.scrambled, o.PID, o.Offset, o.Excluded)
}

func count(pids map[uint16]*affectedPID, pid uint16, offset int64, excluded bool) {
	p, ok := pids[pid]
	if !ok {
		p = &affectedPID{first: offset}
		pids[pid] = p
	}
	p.packets++
	p.first = min(p.first, offset)
	if excluded {
		p.excluded++
	}
}

// AffectedPID is the count of affected packets of one PID.
type AffectedPID struct {
	PID uint16
	// Label names the PID's role as ContinuityPID.Label does, with "null" for
	// the null PID 0x1FFF when no layout named it.
	Label    string
	Packets  int
	Excluded int
	// FirstOffset is the byte offset of the PID's first affected packet.
	FirstOffset int64
}

// AffectedPackets is the result for one condition: StatusOK with no PIDs when
// no packet had it, StatusDataExcluded when the parser left data of at least
// one of the packets out of the analysis, and StatusAffected otherwise. PIDs
// are in ascending PID order; FirstOffset is the smallest offset of all PIDs
// and is meaningful only when Packets is not zero.
type AffectedPackets struct {
	Status      Status
	Packets     int
	Excluded    int
	FirstOffset int64
	PIDs        []AffectedPID
}

// TransportTrustResult holds the result of each condition.
type TransportTrustResult struct {
	TransportErrors AffectedPackets
	Scrambled       AffectedPackets
}

// Result returns the summary of the packets observed so far.
func (c *TransportTrustCheck) Result() TransportTrustResult {
	return TransportTrustResult{TransportErrors: c.result(c.transportError), Scrambled: c.result(c.scrambled)}
}

func (c *TransportTrustCheck) result(pids map[uint16]*affectedPID) AffectedPackets {
	r := AffectedPackets{Status: StatusOK, PIDs: make([]AffectedPID, 0, len(pids))}
	for pid, p := range pids {
		fallback := "unknown"
		if pid == nullPID {
			fallback = "null"
		}
		r.PIDs = append(r.PIDs, AffectedPID{PID: pid, Label: c.labels.label(pid, fallback), Packets: p.packets, Excluded: p.excluded, FirstOffset: p.first})
		if r.Packets == 0 || p.first < r.FirstOffset {
			r.FirstOffset = p.first
		}
		r.Packets += p.packets
		r.Excluded += p.excluded
	}
	slices.SortFunc(r.PIDs, func(a, b AffectedPID) int { return int(a.PID) - int(b.PID) })
	switch {
	case r.Excluded > 0:
		r.Status = StatusDataExcluded
	case r.Packets > 0:
		r.Status = StatusAffected
	}
	return r
}
