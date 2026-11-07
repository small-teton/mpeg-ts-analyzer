package tsparser

import (
	"fmt"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

// Trust policy for packets marked with transport_error_indicator (TEI) or a
// non-zero transport_scrambling_control.
//
// A TEI packet has at least one uncorrectable bit error somewhere in it, so
// nothing beyond the fields needed to count it is used:
//
//   - PSI: the packet is ignored and the section in progress on its PID is
//     discarded (psiSectionReader), so no PAT or PMT is built from it.
//   - PES: the payload is not used. Since it may continue the PES packet in
//     progress or start the next one, the PES in progress ends at the packet
//     and is parsed from the trusted packets received before it; the PID then
//     resumes at its next unaffected payload_unit_start_indicator. No PES
//     header, PTS/DTS, timestamp check or PCR-PTS/DTS gap comes from the
//     affected payload.
//   - PCR: a PCR in its adaptation field is ignored; PCR intervals, jitter,
//     bitrate windows and PCR brackets use the surrounding trusted PCRs.
//
// A scrambled packet has a clear header and adaptation field (ISO/IEC 13818-1
// never scrambles them), but its payload cannot be read without decryption:
//
//   - PSI and PES: as for TEI, when the packet has a payload. A scrambled
//     packet without payload leaves the section or PES in progress intact.
//   - PCR: used, since the adaptation field is clear.
//
// In both cases the packet is still counted for the continuity counter check
// (with the continuity_counter of its header) and for the bitrate, since it
// occupies the multiplex. Packets are counted on every PID, including the null
// PID; whether data of a packet was left out of the analysis ("excluded") is
// decided here, from the PIDs the analysis reads at that point.

// transportError reports whether the packet's transport_error_indicator is set.
func (tp *TsPacket) transportError() bool { return tp.transportErrorIndicator != 0 }

// scrambled reports whether the packet's transport_scrambling_control is not
// zero.
func (tp *TsPacket) scrambled() bool { return tp.transportScramblingControl != 0 }

// payloadUntrusted reports whether the packet's payload must not be parsed:
// it has a transport error, or it is scrambled and has a payload.
func (tp *TsPacket) payloadUntrusted() bool {
	return tp.transportError() || tp.scrambled() && len(tp.Payload()) > 0
}

// transportTrustSummary feeds the packets with a transport error or a
// scrambled payload to the transport trust check of the analysis package, and
// renders its result as the end-of-run summary.
type transportTrustSummary struct {
	check *analysis.TransportTrustCheck
}

func newTransportTrustSummary() *transportTrustSummary {
	return &transportTrustSummary{check: analysis.NewTransportTrustCheck()}
}

// addProgramLabels names the PIDs of one program in the summary.
func (s *transportTrustSummary) addProgramLabels(pmtPid, pcrPid uint16, programInfos []ProgramInfo) {
	s.check.ObserveLayout(programLayout(pmtPid, pcrPid, programInfos))
}

// observeLead counts a packet read before the analysis started, while the
// first PAT and PMT were acquired. Acquisition reports what it discarded as
// warnings, so no data of these packets counts as excluded.
func (s *transportTrustSummary) observeLead(tsPacket *TsPacket, pos int64) {
	s.observe(tsPacket, pos, nil, nil)
}

// observe counts the packet at pos if it is affected. A packet is excluded when
// the analysis reads what it no longer trusts on the packet's PID: the payload
// of the PAT, of the PMT of an analyzed program or of one of its elementary
// streams, or, for a transport error, the PCR on an analyzed PCR_PID.
func (s *transportTrustSummary) observe(tsPacket *TsPacket, pos int64, analyzers []*programAnalyzer, psi *psiTracker) {
	transportError, scrambled := tsPacket.transportError(), tsPacket.scrambled()
	if !transportError && !scrambled {
		return
	}
	pid := tsPacket.Pid()
	readsPayload, readsPcr := psi.readsPid(pid, pos), false
	for _, a := range analyzers {
		if pos < a.start || a.stopped {
			continue
		}
		_, stream := a.pesMap[pid]
		readsPayload = readsPayload || stream
		readsPcr = readsPcr || (a.pcrPid != 0 && a.pcrPid != nullPidValue && pid == a.pcrPid)
	}
	if transportError {
		s.check.ObserveTransportError(analysis.TransportError{PID: pid, Offset: pos, Excluded: readsPayload || readsPcr})
	}
	if scrambled {
		s.check.ObserveScrambled(analysis.Scrambled{PID: pid, Offset: pos, Excluded: readsPayload && len(tsPacket.Payload()) > 0})
	}
}

// Dump prints the summary of both conditions. A healthy stream still gets one
// line for each, because they are always-on health checks.
func (s *transportTrustSummary) Dump() {
	fmt.Println("-----------------------------")
	result := s.check.Result()
	dumpAffected("Transport Error Indicator", "no packets with transport_error_indicator set", result.TransportErrors)
	dumpAffected("Transport Scrambling", "no scrambled packets", result.Scrambled)
}

func dumpAffected(title, healthy string, r analysis.AffectedPackets) {
	if r.Status == analysis.StatusOK {
		fmt.Printf("%s: %s\n", title, healthy)
		return
	}
	fmt.Printf("%s: %d %s in %d %s, first at 0x%08x, %d excluded from analysis [%s]\n", title, r.Packets, packetWord(r.Packets), len(r.PIDs), pidWord(len(r.PIDs)), r.FirstOffset, r.Excluded, affectedStatusText(r.Status))
	for _, pid := range r.PIDs {
		fmt.Printf("  PID 0x%04x (%s) : %d %s, first at 0x%08x, %d excluded\n", pid.PID, pid.Label, pid.Packets, packetWord(pid.Packets), pid.FirstOffset, pid.Excluded)
	}
}

func affectedStatusText(status analysis.Status) string {
	if status == analysis.StatusDataExcluded {
		return "DATA EXCLUDED"
	}
	return "AFFECTED"
}

func packetWord(count int) string {
	if count == 1 {
		return "packet"
	}
	return "packets"
}

// ReportAffectedPID is the count of affected packets of one PID.
type ReportAffectedPID struct {
	PID                 string `json:"pid"`
	Label               string `json:"label"`
	PacketCount         int    `json:"packet_count"`
	ExcludedPacketCount int    `json:"excluded_packet_count"`
	FirstOffsetBytes    int64  `json:"first_offset_bytes"`
}

// ReportAffectedPackets is the transport_errors or scrambled_packets result:
// status ok, affected or data_excluded, the totals, the offset of the first
// affected packet (absent when there is none) and the PIDs in numeric order.
type ReportAffectedPackets struct {
	Status              string              `json:"status"`
	PacketCount         int                 `json:"packet_count"`
	ExcludedPacketCount int                 `json:"excluded_packet_count"`
	FirstOffsetBytes    *int64              `json:"first_offset_bytes,omitempty"`
	PIDs                []ReportAffectedPID `json:"pids"`
}

// reportTransportTrust returns the transport_errors and scrambled_packets
// results.
func reportTransportTrust(s *transportTrustSummary) (*ReportAffectedPackets, *ReportAffectedPackets) {
	result := s.check.Result()
	return reportAffected(result.TransportErrors), reportAffected(result.Scrambled)
}

func reportAffected(r analysis.AffectedPackets) *ReportAffectedPackets {
	report := &ReportAffectedPackets{Status: "ok", PacketCount: r.Packets, ExcludedPacketCount: r.Excluded, PIDs: []ReportAffectedPID{}}
	switch r.Status {
	case analysis.StatusAffected:
		report.Status = "affected"
	case analysis.StatusDataExcluded:
		report.Status = "data_excluded"
	}
	if r.Packets > 0 {
		first := r.FirstOffset
		report.FirstOffsetBytes = &first
	}
	for _, pid := range r.PIDs {
		report.PIDs = append(report.PIDs, ReportAffectedPID{PID: reportPID(pid.PID), Label: pid.Label, PacketCount: pid.Packets, ExcludedPacketCount: pid.Excluded, FirstOffsetBytes: pid.FirstOffset})
	}
	return report
}
