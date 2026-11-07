package tsparser

import (
	"fmt"
	"math"
	"strings"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

// PID reference health (referenced, missing and unreferenced PIDs).
//
// pidReferenceSummary feeds analysis.PIDReferenceCheck during the scan: every
// packet of the range from its first packet (the packets read while the
// first PAT and PMT were acquired included), the PCRs of the reference
// program, and the layout of the PAT and of the followed PMTs whenever it
// changes. The decisions the parser makes here are:
//
//   - A packet with transport_error_indicator set is not observed: its PID
//     may itself be corrupted, so it neither proves that a referenced PID is
//     present nor makes a PID unreferenced (the packet is counted by the
//     transport error check). It still occupies its packet index.
//   - A scrambled packet is observed: its header, and therefore its PID, is
//     clear, so it proves that the PID carries packets.
//   - The reference program, whose PCRs give the time basis, is the first
//     analyzed program (in PAT order) whose PMT has a PCR_PID. Its PCRs are
//     the ones the program analysis uses: not from a packet with a transport
//     error, and a PCR_PID change starts a new timing segment. All program
//     clocks run at 27 MHz within 30 ppm (ISO/IEC 13818-1, 2.4.2.1), so one
//     program's PCRs time the packets of every PID.
//   - Layouts come from psiTracker: the programs of the applicable PAT, the
//     followed PMTs (from the packet at which each program's analysis
//     starts), the PAT's network_PID and the CA_PIDs of CA_descriptors.
type pidReferenceSummary struct {
	check      *analysis.PIDReferenceCheck
	leadStart  int64
	packetSize int64
	layoutSet  bool
	generation int // psiTracker.generation of the layout reported last
	started    int // analyzers started when it was reported
	// next is the index of the next scanned packet. Lead packets are aligned
	// to the first packet of the range; the scan counts its packets, since a
	// synchronization recovery may skip bytes that are not whole packets.
	next int64
}

func newPidReferenceSummary(leadStart int64, packetSize int) *pidReferenceSummary {
	return &pidReferenceSummary{check: analysis.NewPIDReferenceCheck(), leadStart: leadStart, packetSize: int64(packetSize)}
}

// index is the index of the packet at pos in the lead, or of the first
// packet of the scan.
func (s *pidReferenceSummary) index(pos int64) int64 { return (pos - s.leadStart) / s.packetSize }

// scanIndex is the index of the packet of the scan at pos.
func (s *pidReferenceSummary) scanIndex(pos int64) int64 {
	if !s.layoutSet {
		return s.index(pos)
	}
	return s.next
}

// observeLead counts a packet read before the scan, while the first PAT and
// PMT were acquired. No layout applies yet.
func (s *pidReferenceSummary) observeLead(tsPacket *TsPacket, pos int64) {
	s.count(tsPacket, s.index(pos), pos)
}

func (s *pidReferenceSummary) count(tsPacket *TsPacket, index, pos int64) {
	if !tsPacket.transportError() {
		s.check.ObservePacket(analysis.Packet{PID: tsPacket.Pid(), Index: index, Offset: pos})
	}
	s.next = index + 1
}

// observe counts a packet of the scan, after reporting the layout that
// applies to it when it changed.
func (s *pidReferenceSummary) observe(tsPacket *TsPacket, pos int64, analyzers []*programAnalyzer, psi *psiTracker) {
	started := 0
	for _, a := range analyzers {
		if pos >= a.start {
			started++
		}
	}
	index := s.scanIndex(pos)
	if !s.layoutSet || psi.generation != s.generation || started != s.started {
		s.check.ObserveLayout(referenceLayout(index, pos, psi))
		s.layoutSet, s.generation, s.started = true, psi.generation, started
	}
	s.count(tsPacket, index, pos)
}

// observePcr applies a PCR of the reference program carried by the packet at
// pos.
func (s *pidReferenceSummary) observePcr(pos int64, pcr uint64, discontinuity bool) {
	s.check.ObservePCR(analysis.PCR{Index: s.scanIndex(pos), Offset: pos, PCR: pcr, Discontinuity: discontinuity})
}

// finish ends the scan at pos, the offset after its last packet.
func (s *pidReferenceSummary) finish(pos int64) { s.check.Finish(s.scanIndex(pos), pos) }

// usePcrOf makes the first analyzer with a PCR_PID the reference program.
func (s *pidReferenceSummary) usePcrOf(analyzers []*programAnalyzer) {
	for _, a := range analyzers {
		if a.pcrPid != 0 && a.pcrPid != nullPidValue {
			a.onPcr = s.observePcr
			return
		}
	}
}

// referenceLayout is the layout psi applies at pos: every program of its
// PAT, with the PMT of each program whose analysis has started and that is
// still in the PAT.
func referenceLayout(index, pos int64, psi *psiTracker) analysis.ReferenceLayout {
	layout := analysis.ReferenceLayout{Index: index, Offset: pos, NetworkPID: psi.patTable.networkPid()}
	for _, program := range psi.patPrograms {
		p := analysis.ReferencedProgram{Number: program.ProgramNumber, PMTPID: program.PmtPid}
		for _, tracked := range psi.programs {
			if tracked.number != program.ProgramNumber || tracked.removed || pos < tracked.analyzer.start {
				continue
			}
			table := tracked.table
			p.Known, p.PMTPID, p.PCRPID = true, tracked.pmtPid, table.pcrPid
			p.CAPIDs = caPids(table.descriptors)
			for _, info := range table.programInfos {
				p.Streams = append(p.Streams, analysis.ElementaryStream{PID: info.elementaryPid, StreamType: info.streamType})
				p.CAPIDs = append(p.CAPIDs, caPids(info.descriptors)...)
			}
		}
		layout.Programs = append(layout.Programs, p)
	}
	return layout
}

// networkPid returns the network_PID of the PAT's program_number 0 entry, or
// 0 when there is none.
func (t *patTable) networkPid() uint16 {
	for _, section := range t.sections {
		for _, info := range section.programInfo {
			if info.programNumber == 0 {
				return info.networkPid
			}
		}
	}
	return 0
}

// caDescriptorTag is the tag of the CA_descriptor (ISO/IEC 13818-1, 2.6.16).
const caDescriptorTag = 0x09

// caPids returns the CA_PID of each CA_descriptor: CA_system_ID (16 bits),
// reserved (3 bits), CA_PID (13 bits), private data.
func caPids(descriptors []Descriptor) []uint16 {
	var pids []uint16
	for _, d := range descriptors {
		if d.tag == caDescriptorTag && len(d.data) >= 4 {
			pids = append(pids, uint16(d.data[2]&0x1F)<<8|uint16(d.data[3]))
		}
	}
	return pids
}

// Dump prints the summary. A healthy stream gets one line for referenced and
// one for unreferenced PIDs; a problem lists the PIDs concerned.
func (s *pidReferenceSummary) Dump() {
	fmt.Println("-----------------------------")
	result := s.check.Result()
	referenced := result.Referenced
	fmt.Printf("Referenced PIDs: %d %s, silence limit %.0f ms [%s]\n", len(referenced.PIDs), referenceWord(len(referenced.PIDs)), analysis.PIDSilenceLimitMs, pidStatusText(referenced.Status))
	for _, pid := range referenced.PIDs {
		if pid.Status == analysis.PIDStatusOK {
			continue
		}
		fmt.Printf("  program %d %s PID 0x%04x%s : %s\n", pid.Program, roleText(pid.Role), pid.PID, streamTypeSuffix(pid), referencedDetail(pid))
	}
	unreferenced := result.Unreferenced
	switch unreferenced.Status {
	case analysis.PIDStatusOK:
		fmt.Println("Unreferenced PIDs: none [OK]")
	case analysis.PIDStatusSkipped:
		fmt.Printf("Unreferenced PIDs: not checked (%s) [SKIPPED]\n", unreferenced.Reason)
	default:
		fmt.Printf("Unreferenced PIDs: %d %s, grace %.0f ms [%s]\n", len(unreferenced.PIDs), pidWord(len(unreferenced.PIDs)), analysis.UnreferencedGraceMs, pidStatusText(unreferenced.Status))
		if unreferenced.Reason != "" {
			fmt.Printf("  reason: %s\n", unreferenced.Reason)
		}
		for _, pid := range unreferenced.PIDs {
			fmt.Printf("  PID 0x%04x : %s, %d %s from 0x%08x to 0x%08x, %.1f ms\n", pid.PID, pidStatusLower(pid.Status), pid.Packets, packetWord(int(pid.Packets)), pid.FirstOffset, pid.LastOffset, pid.LongestMs)
		}
	}
}

func referenceWord(count int) string {
	if count == 1 {
		return "reference"
	}
	return "references"
}

func roleText(role analysis.ReferenceRole) string {
	switch role {
	case analysis.RolePMT:
		return "PMT"
	case analysis.RolePCR:
		return "PCR"
	default:
		return "ES"
	}
}

func streamTypeSuffix(pid analysis.ReferencedPID) string {
	if pid.Role != analysis.RoleElementaryStream {
		return ""
	}
	return fmt.Sprintf(" (stream_type 0x%02x)", pid.StreamType)
}

func referencedDetail(pid analysis.ReferencedPID) string {
	switch pid.Status {
	case analysis.PIDStatusAbsent:
		return fmt.Sprintf("absent, no packet for %.1f ms from 0x%08x to 0x%08x", pid.MaxSilenceMs, pid.MaxSilenceStartOffset, pid.MaxSilenceEndOffset)
	case analysis.PIDStatusSilent:
		return fmt.Sprintf("silent, no packet for %.1f ms from 0x%08x to 0x%08x", pid.MaxSilenceMs, pid.MaxSilenceStartOffset, pid.MaxSilenceEndOffset)
	default:
		return fmt.Sprintf("insufficient data (%s)", pid.Reason)
	}
}

// pidStatusText is a status in upper case with spaces, as in the other
// summaries.
func pidStatusText(status analysis.PIDStatus) string {
	return strings.ToUpper(pidStatusLower(status))
}

func pidStatusLower(status analysis.PIDStatus) string {
	if status == analysis.PIDStatusInsufficientData {
		return "insufficient data"
	}
	return string(status)
}

// pidReferenceTimeBasis names the time model of the durations.
const pidReferenceTimeBasis = "pcr_interpolated_file_position"

// ReportPIDReferences is the pid_references result: the referenced PIDs of
// the PSI layouts and the PIDs that carried unreferenced packets.
type ReportPIDReferences struct {
	TimeBasis           string                 `json:"time_basis"`
	SilenceLimitMs      float64                `json:"silence_limit_ms"`
	UnreferencedGraceMs float64                `json:"unreferenced_grace_ms"`
	Referenced          ReportReferencedPIDs   `json:"referenced"`
	Unreferenced        ReportUnreferencedPIDs `json:"unreferenced"`
}

type ReportReferencedPIDs struct {
	Status string                `json:"status"`
	PIDs   []ReportReferencedPID `json:"pids"`
}

// ReportReferencedPID is one (program, role, PID) of the layouts.
type ReportReferencedPID struct {
	ProgramNumber              uint16   `json:"program_number"`
	Role                       string   `json:"role"`
	PID                        string   `json:"pid"`
	StreamType                 string   `json:"stream_type,omitempty"`
	Status                     string   `json:"status"`
	Reason                     string   `json:"reason,omitempty"`
	SilenceJudged              bool     `json:"silence_judged"`
	PacketCount                int64    `json:"packet_count"`
	FirstOffsetBytes           *int64   `json:"first_offset_bytes,omitempty"`
	LastOffsetBytes            *int64   `json:"last_offset_bytes,omitempty"`
	ReferencedPacketCount      int64    `json:"referenced_packet_count"`
	ReferencedFromOffsetBytes  int64    `json:"referenced_from_offset_bytes"`
	ReferencedUntilOffsetBytes int64    `json:"referenced_until_offset_bytes"`
	MaxSilenceMs               *float64 `json:"max_silence_ms,omitempty"`
	MaxSilenceExact            *bool    `json:"max_silence_exact,omitempty"`
	MaxSilenceStartOffsetBytes *int64   `json:"max_silence_start_offset_bytes,omitempty"`
	MaxSilenceEndOffsetBytes   *int64   `json:"max_silence_end_offset_bytes,omitempty"`
}

type ReportUnreferencedPIDs struct {
	Status string                  `json:"status"`
	Reason string                  `json:"reason,omitempty"`
	PIDs   []ReportUnreferencedPID `json:"pids"`
}

// ReportUnreferencedPID is one PID that carried unreferenced packets.
type ReportUnreferencedPID struct {
	PID              string  `json:"pid"`
	Status           string  `json:"status"`
	PacketCount      int64   `json:"packet_count"`
	FirstOffsetBytes int64   `json:"first_offset_bytes"`
	LastOffsetBytes  int64   `json:"last_offset_bytes"`
	LongestRunMs     float64 `json:"longest_run_ms"`
}

// reportPidReferences returns the pid_references result.
func reportPidReferences(s *pidReferenceSummary) *ReportPIDReferences {
	result := s.check.Result()
	r := &ReportPIDReferences{
		TimeBasis: pidReferenceTimeBasis, SilenceLimitMs: analysis.PIDSilenceLimitMs, UnreferencedGraceMs: analysis.UnreferencedGraceMs,
		Referenced:   ReportReferencedPIDs{Status: string(result.Referenced.Status), PIDs: []ReportReferencedPID{}},
		Unreferenced: ReportUnreferencedPIDs{Status: string(result.Unreferenced.Status), Reason: result.Unreferenced.Reason, PIDs: []ReportUnreferencedPID{}},
	}
	for _, pid := range result.Referenced.PIDs {
		row := ReportReferencedPID{
			ProgramNumber: pid.Program, Role: pid.Role.String(), PID: reportPID(pid.PID), Status: string(pid.Status), Reason: pid.Reason,
			SilenceJudged: pid.Judged, PacketCount: pid.Packets, ReferencedPacketCount: pid.ReferencedPackets,
			ReferencedFromOffsetBytes: pid.ReferencedFrom, ReferencedUntilOffsetBytes: pid.ReferencedUntil,
		}
		if pid.Role == analysis.RoleElementaryStream {
			row.StreamType = fmt.Sprintf("0x%02x", pid.StreamType)
		}
		if pid.Packets > 0 {
			first, last := pid.FirstOffset, pid.LastOffset
			row.FirstOffsetBytes, row.LastOffsetBytes = &first, &last
		}
		if pid.MaxSilenceKnown {
			ms, exact, start, end := roundMicro(pid.MaxSilenceMs), pid.MaxSilenceExact, pid.MaxSilenceStartOffset, pid.MaxSilenceEndOffset
			row.MaxSilenceMs, row.MaxSilenceExact, row.MaxSilenceStartOffsetBytes, row.MaxSilenceEndOffsetBytes = &ms, &exact, &start, &end
		}
		r.Referenced.PIDs = append(r.Referenced.PIDs, row)
	}
	for _, pid := range result.Unreferenced.PIDs {
		r.Unreferenced.PIDs = append(r.Unreferenced.PIDs, ReportUnreferencedPID{
			PID: reportPID(pid.PID), Status: string(pid.Status), PacketCount: pid.Packets,
			FirstOffsetBytes: pid.FirstOffset, LastOffsetBytes: pid.LastOffset, LongestRunMs: roundMicro(pid.LongestMs),
		})
	}
	return r
}

// roundMicro rounds a duration in milliseconds to the microsecond, so the
// JSON does not carry interpolation noise.
func roundMicro(ms float64) float64 { return math.Round(ms*1000) / 1000 }
