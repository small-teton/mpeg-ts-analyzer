package analysis

import (
	"cmp"
	"slices"
)

// Per-PID maximum packet gaps (issue #176): a report-only multiplexing
// diagnostic presented from the shared PacketGaps collector of the PID
// reference check, so the analysis keeps a single per-PID packet history.
//
// For every PID observed in the range except the null PID it reports the
// largest number of packets of other PIDs between two consecutive packets of
// the PID (the endpoints are not counted), the offsets of those two packets
// and the estimated time of the gap on the time basis of pcr_clock.go. The
// largest gap is chosen by packets, which is exact; its time is an estimate,
// and a lower bound when it is not exact. The time before the first packet and
// after the last packet of a PID is not a gap: that is the absence and silence
// that the PID reference check judges. A PAT/PMT change that stops or resumes
// referencing a PID starts a new chain (see PacketGaps), so a stream removed
// and later re-added gets no gap across its absence; its packet count and the
// largest gap so far are kept.
//
// The result carries no verdict: a PID is info when a gap was measured and
// insufficient_data otherwise (no_consecutive_packets). An info PID without a
// PCR time base has no time (reason no_pcr_time_base).

// GapStatus is the status of a PID's maximum packet gap.
type GapStatus string

const (
	GapStatusInfo             GapStatus = "info"
	GapStatusInsufficientData GapStatus = "insufficient_data"
)

// ReasonNoConsecutivePackets: the PID has fewer than two packets in one
// stretch of the layout, so no gap could be measured.
const ReasonNoConsecutivePackets = "no_consecutive_packets"

// PIDReference is one way a layout referenced a PID: the program, the role
// and, for an elementary stream, the stream_type.
type PIDReference struct {
	Program    uint16
	Role       ReferenceRole
	StreamType uint8
}

// PacketGapPID is the maximum packet gap of one PID.
type PacketGapPID struct {
	PIDPackets
	// References lists every (program, role, stream_type) under which any
	// layout of the range referenced the PID, ordered as the referenced
	// rows of PIDReferenceResult; it is empty for a PID no layout referenced
	// (PSI/SI PIDs and unreferenced PIDs).
	References []PIDReference
	Status     GapStatus
	Reason     string
}

// PacketGaps returns the maximum packet gap of every observed PID except the
// null PID, in ascending PID order. It is final after Finish.
func (c *PIDReferenceCheck) PacketGaps() []PacketGapPID {
	references := make(map[uint16][]PIDReference)
	for key := range c.rows {
		references[key.pid] = append(references[key.pid], PIDReference{Program: key.program, Role: key.role, StreamType: key.streamType})
	}
	timeBase := c.gaps.HasTimeBase()
	var result []PacketGapPID
	for _, p := range c.gaps.PIDs() {
		if p.PID == nullPID {
			continue
		}
		refs := references[p.PID]
		slices.SortFunc(refs, func(a, b PIDReference) int {
			return cmp.Or(cmp.Compare(a.Program, b.Program), cmp.Compare(a.Role, b.Role), cmp.Compare(a.StreamType, b.StreamType))
		})
		r := PacketGapPID{PIDPackets: p, References: refs, Status: GapStatusInfo}
		switch {
		case !p.HasGap:
			r.Status, r.Reason = GapStatusInsufficientData, ReasonNoConsecutivePackets
		case !timeBase:
			r.Reason = ReasonNoPCRTimeBase
		}
		result = append(result, r)
	}
	return result
}
