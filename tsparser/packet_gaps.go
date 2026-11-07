package tsparser

import (
	"fmt"
	"strings"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

// Per-PID maximum packet gaps (report-only multiplexing diagnostics).
//
// The gaps come from the packet-gap collector of the PID reference check
// (analysis.PIDReferenceCheck.PacketGaps), fed by pidReferenceSummary: the
// same packets (every packet of the range from its first packet, without
// packets with transport_error_indicator set, which still occupy their packet
// index), the same reference-program PCRs and the same layouts. The result
// carries no verdict and never affects the exit status.

// ReportMultiplexing holds the report-only multiplexing diagnostics.
type ReportMultiplexing struct {
	PacketGaps ReportPacketGaps `json:"packet_gaps"`
}

// ReportPacketGaps is the maximum packet gap of every non-NULL PID.
type ReportPacketGaps struct {
	TimeBasis string               `json:"time_basis"`
	PIDs      []ReportPacketGapPID `json:"pids"`
}

// ReportPacketGapPID is the maximum packet gap of one PID.
type ReportPacketGapPID struct {
	PID                    string               `json:"pid"`
	References             []ReportPIDReference `json:"references"`
	PacketCount            int64                `json:"packet_count"`
	Status                 string               `json:"status"`
	Reason                 string               `json:"reason,omitempty"`
	MaxGapPackets          *int64               `json:"max_gap_packets,omitempty"`
	MaxGapStartOffsetBytes *int64               `json:"max_gap_start_offset_bytes,omitempty"`
	MaxGapEndOffsetBytes   *int64               `json:"max_gap_end_offset_bytes,omitempty"`
	MaxGapMs               *float64             `json:"max_gap_ms,omitempty"`
	MaxGapExact            *bool                `json:"max_gap_exact,omitempty"`
}

// ReportPIDReference is one program and role under which a layout referenced
// a PID.
type ReportPIDReference struct {
	ProgramNumber uint16 `json:"program_number"`
	Role          string `json:"role"`
	StreamType    string `json:"stream_type,omitempty"`
}

// reportMultiplexing returns the multiplexing result.
func reportMultiplexing(s *pidReferenceSummary) *ReportMultiplexing {
	r := &ReportMultiplexing{PacketGaps: ReportPacketGaps{TimeBasis: pidReferenceTimeBasis, PIDs: []ReportPacketGapPID{}}}
	for _, gap := range s.check.PacketGaps() {
		row := ReportPacketGapPID{PID: reportPID(gap.PID), References: []ReportPIDReference{}, PacketCount: gap.Packets, Status: string(gap.Status), Reason: gap.Reason}
		for _, ref := range gap.References {
			reference := ReportPIDReference{ProgramNumber: ref.Program, Role: ref.Role.String()}
			if ref.Role == analysis.RoleElementaryStream {
				reference.StreamType = fmt.Sprintf("0x%02x", ref.StreamType)
			}
			row.References = append(row.References, reference)
		}
		if gap.HasGap {
			packets, start, end := gap.MaxGapPackets, gap.MaxGapStartOffset, gap.MaxGapEndOffset
			row.MaxGapPackets, row.MaxGapStartOffsetBytes, row.MaxGapEndOffsetBytes = &packets, &start, &end
		}
		if gap.HasGap && gap.MaxGapTimeKnown {
			ms, exact := roundMicro(gap.MaxGapMs), gap.MaxGapExact
			row.MaxGapMs, row.MaxGapExact = &ms, &exact
		}
		r.PacketGaps.PIDs = append(r.PacketGaps.PIDs, row)
	}
	return r
}

// dumpPacketGaps prints one line per non-NULL PID.
func dumpPacketGaps(s *pidReferenceSummary) {
	fmt.Println("-----------------------------")
	gaps := s.check.PacketGaps()
	fmt.Printf("Max packet gaps (report only): %d %s, time basis %s\n", len(gaps), pidWord(len(gaps)), pidReferenceTimeBasis)
	for _, gap := range gaps {
		fmt.Printf("  PID 0x%04x%s : %d %s, %s\n", gap.PID, gapReferences(gap.References), gap.Packets, packetWord(int(gap.Packets)), gapDetail(gap))
	}
}

// gapReferences names the programs and roles that referenced a PID.
func gapReferences(references []analysis.PIDReference) string {
	if len(references) == 0 {
		return ""
	}
	parts := make([]string, 0, len(references))
	for _, ref := range references {
		part := fmt.Sprintf("program %d %s", ref.Program, roleText(ref.Role))
		if ref.Role == analysis.RoleElementaryStream {
			part += fmt.Sprintf(" stream_type 0x%02x", ref.StreamType)
		}
		parts = append(parts, part)
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

func gapDetail(gap analysis.PacketGapPID) string {
	if !gap.HasGap {
		return fmt.Sprintf("no gap (%s)", gap.Reason)
	}
	detail := fmt.Sprintf("max gap %d %s", gap.MaxGapPackets, packetWord(int(gap.MaxGapPackets)))
	switch {
	case !gap.MaxGapTimeKnown:
		detail += fmt.Sprintf(" (%s)", gap.Reason)
	case gap.MaxGapExact:
		detail += fmt.Sprintf(", %.3f ms", gap.MaxGapMs)
	default:
		detail += fmt.Sprintf(", at least %.3f ms", gap.MaxGapMs)
	}
	return detail + fmt.Sprintf(" from 0x%08x to 0x%08x", gap.MaxGapStartOffset, gap.MaxGapEndOffset)
}
