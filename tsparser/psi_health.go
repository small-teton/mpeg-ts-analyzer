package tsparser

import (
	"fmt"
	"slices"
	"strings"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

// PAT and PMT health (psi_health).
//
// psiHealthCollector sees every packet of the range once, from its first
// packet, and reports to analysis.PSIHealthCheck:
//
//   - each complete current PAT on PID 0 and each complete current PMT of
//     every program the applied PAT announces, whether or not the program is
//     analyzed, as one instance at the packet that completes it. Sections are
//     read with the same psiSectionReader and psiAssembler as the analysis
//     itself, so a table counts exactly when the analysis could use it;
//   - each section the analysis cannot use: a table_id other than 0x00 on PID
//     0, an invalid CRC_32, a malformed section (psiSectionFault), data
//     dropped before a table completed, and each packet of a table's PID that
//     the trust policy of transport_trust.go does not read (transport error
//     or scrambled payload). A continuity_counter jump inside a section is
//     left to the continuity counter check, as in psiTracker; its effect, a
//     missing table, shows in the intervals;
//   - which programs are announced: the programs of the first complete PAT
//     from its packet on, then those of every PAT change psiTracker applies.
//     A PMT is read from the packet after its announcement;
//   - the PCRs of one reference PCR_PID, which give the time base: that of
//     the selected program, or with --output analysis of the first analyzed
//     program (in program_number order) that has a PCR_PID. They are the
//     analysis.PCR values programAnalyzer gives PCRCheck (no PCR of a packet
//     with transport_error_indicator; discontinuity_indicator on a packet
//     without PCR declares the next one; a PCR_PID change marks it), from the
//     first packet of the range, so the segments are those of every other
//     PCR-based result.
//
// The collector keeps one section reader per PMT PID and one assembler per
// announced program, so its state is bounded by the PIDs and programs of the
// PAT, not by the number of packets.
type psiHealthCollector struct {
	check     *analysis.PSIHealthCheck
	patReader psiSectionReader
	pat       *psiAssembler[*Pat]
	programs  map[uint16]*healthProgram    // announced programs by program_number
	readers   map[uint16]*psiSectionReader // by PMT PID
	initial   *patTable                    // announced when the scan reaches its packet
	refNumber uint16                       // program_number of the reference program
	pcrPid    uint16                       // its PCR_PID while ref is unset
	ref       *programAnalyzer             // the reference program, once analyzed
	end       int64                        // offset of the last packet seen
	finished  bool
	// lastPcrPid is the reference PCR_PID at the previous packet, so a change
	// marks the next PCR as a pcr_pid_change; discontinuityPending is set by a
	// packet of the PCR_PID with discontinuity_indicator and no PCR, which
	// makes the next PCR declared, as programAnalyzer.observe does.
	lastPcrPid    uint16
	pcrPidChanged bool
	// reportedPcrPid is the last non-zero reference PCR_PID, which the
	// result names even after the reference program was removed.
	reportedPcrPid       uint16
	discontinuityPending bool
}

type healthProgram struct {
	pid       uint16
	assembler *psiAssembler[*Pmt]
}

// newPsiHealthCollector follows the range whose first complete PAT is pat.
// refNumber and pcrPid name the reference program and its PCR_PID.
func newPsiHealthCollector(pat *patTable, refNumber, pcrPid uint16) *psiHealthCollector {
	h := &psiHealthCollector{
		check:     analysis.NewPSIHealthCheck(),
		pat:       &psiAssembler[*Pat]{name: "PAT", tableID: 0x00, anyExtension: true, parse: parsePatSection},
		programs:  make(map[uint16]*healthProgram),
		readers:   make(map[uint16]*psiSectionReader),
		initial:   pat,
		refNumber: refNumber,
		pcrPid:    pcrPid,
	}
	h.reportedPcrPid = h.referencePcrPid()
	return h
}

// referencePcrPid is the PCR_PID that gives the time base at this point, or
// 0 when there is none.
func (h *psiHealthCollector) referencePcrPid() uint16 {
	pid := h.pcrPid
	if h.ref != nil {
		pid = h.ref.pcrPid
		if h.ref.stopped {
			pid = 0
		}
	}
	if pid == nullPidValue {
		return 0
	}
	return pid
}

// observe passes one packet of the range at offset pos.
func (h *psiHealthCollector) observe(tsPacket *TsPacket, pos int64) {
	h.end = pos
	pid := tsPacket.Pid()
	pcrPid := h.referencePcrPid()
	if pcrPid != 0 {
		h.reportedPcrPid = pcrPid
	}
	if pcrPid != h.lastPcrPid {
		h.lastPcrPid, h.pcrPidChanged, h.discontinuityPending = pcrPid, true, false
	}
	if pcrPid != 0 && pid == pcrPid && tsPacket.HasAf() && !tsPacket.transportError() {
		af := tsPacket.adaptationField
		if !af.PcrFlag() {
			h.discontinuityPending = h.discontinuityPending || af.DiscontinuityIndicator()
		} else {
			h.check.ObservePCR(analysis.PCR{PID: pid, Offset: pos, Value: tsPacket.Pcr(), DiscontinuityIndicator: af.DiscontinuityIndicator() || h.discontinuityPending, PCRPIDChanged: h.pcrPidChanged})
			h.pcrPidChanged, h.discontinuityPending = false, false
		}
	}
	if pid == patPidValue {
		h.observePat(tsPacket, pos)
	} else if reader := h.readers[pid]; reader != nil {
		h.observePmt(reader, pid, tsPacket, pos)
	}
	if h.initial != nil && pos >= h.initial.pos {
		h.patApplied(pos, h.initial.Programs())
		h.initial = nil
	}
}

// read pushes a packet of tables' PID to reader and counts what made data
// unusable.
func (h *psiHealthCollector) read(reader *psiSectionReader, tables []analysis.PSITable, tsPacket *TsPacket, pos int64) []psiSectionData {
	sections, problems := reader.push(tsPacket, pos)
	if tsPacket.payloadUntrusted() {
		h.problem(tables, analysis.PSIUnusable, pos)
		return nil
	}
	for _, problem := range problems {
		switch {
		case strings.HasPrefix(problem, "continuity_counter jumped"):
		case strings.HasPrefix(problem, "invalid pointer_field"):
			h.problem(tables, analysis.PSIMalformed, pos)
		default:
			h.problem(tables, analysis.PSIIncomplete, pos)
		}
	}
	return sections
}

func (h *psiHealthCollector) problem(tables []analysis.PSITable, kind analysis.PSIProblemKind, pos int64) {
	for _, table := range tables {
		h.check.ObserveProblem(analysis.PSIProblem{Table: table, Kind: kind, Offset: pos})
	}
}

// fault counts the problem of a section that cannot be used.
func (h *psiHealthCollector) fault(tables []analysis.PSITable, fault string, pos int64) {
	kind := analysis.PSIMalformed
	if fault == psiFaultCRC {
		kind = analysis.PSICRCError
	}
	h.problem(tables, kind, pos)
}

// assemble adds a valid section to a table's assembler and reports whether it
// completed the table; data the assembler discarded counts as incomplete.
func assemble[T any](h *psiHealthCollector, table analysis.PSITable, assembler *psiAssembler[T], section psiSectionData) ([]T, bool) {
	sections, ok := assembler.add(section.data, section.pos)
	for range assembler.diagnostics {
		h.problem([]analysis.PSITable{table}, analysis.PSIIncomplete, section.pos)
	}
	assembler.diagnostics = nil
	return sections, ok
}

func (h *psiHealthCollector) observePat(tsPacket *TsPacket, pos int64) {
	tables := []analysis.PSITable{analysis.PATTable}
	for _, section := range h.read(&h.patReader, tables, tsPacket, pos) {
		if section.data[0] != 0x00 {
			h.problem(tables, analysis.PSIWrongTableID, section.pos)
			continue
		}
		if fault := psiSectionFault(section.data); fault != "" {
			h.fault(tables, fault, section.pos)
			continue
		}
		if sections, ok := assemble(h, analysis.PATTable, h.pat, section); ok {
			h.check.ObserveInstance(analysis.PSIInstance{Table: analysis.PATTable, PID: patPidValue, Version: sections[0].versionNumber, Offset: pos})
		}
	}
}

// programsOn returns the announced programs on pid in program_number order.
func (h *psiHealthCollector) programsOn(pid uint16) []uint16 {
	var numbers []uint16
	for number, p := range h.programs {
		if p.pid == pid {
			numbers = append(numbers, number)
		}
	}
	slices.Sort(numbers)
	return numbers
}

func (h *psiHealthCollector) observePmt(reader *psiSectionReader, pid uint16, tsPacket *TsPacket, pos int64) {
	numbers := h.programsOn(pid)
	tables := make([]analysis.PSITable, len(numbers))
	for i, number := range numbers {
		tables[i] = analysis.PMTTable(number)
	}
	for _, section := range h.read(reader, tables, tsPacket, pos) {
		data := section.data
		// Other tables may share a PMT PID; only PMT sections are read.
		if data[0] != 0x02 {
			continue
		}
		fault := psiSectionFault(data)
		owners := tables
		if len(data) >= 5 {
			if number := uint16(data[3])<<8 | uint16(data[4]); slices.Contains(numbers, number) {
				owners = []analysis.PSITable{analysis.PMTTable(number)}
			} else if fault == "" {
				// The valid PMT of a program not announced on this PID.
				continue
			}
		}
		if fault != "" {
			// A faulty section is counted for its program when its
			// program_number names one on this PID, and otherwise for every
			// program on the PID.
			h.fault(owners, fault, section.pos)
			continue
		}
		number := owners[0].ProgramNumber
		if sections, ok := assemble(h, owners[0], h.programs[number].assembler, section); ok {
			h.check.ObserveInstance(analysis.PSIInstance{Table: owners[0], PID: pid, Version: sections[0].versionNumber, Offset: pos})
		}
	}
}

// patApplied announces the programs of a PAT that applies from the packet
// after pos: new programs and programs whose PMT PID changed start a new
// identity, and programs it no longer lists are withdrawn.
func (h *psiHealthCollector) patApplied(pos int64, programs []Program) {
	listed := make(map[uint16]uint16, len(programs))
	for _, program := range programs {
		listed[program.ProgramNumber] = program.PmtPid
	}
	for number := range h.programs {
		if _, ok := listed[number]; !ok {
			h.check.ObserveWithdrawn(analysis.PSIWithdrawn{Table: analysis.PMTTable(number), Offset: pos})
			delete(h.programs, number)
		}
	}
	for _, program := range programs {
		if p := h.programs[program.ProgramNumber]; p != nil && p.pid == program.PmtPid {
			continue
		}
		h.programs[program.ProgramNumber] = &healthProgram{pid: program.PmtPid, assembler: newPmtAssembler(program.ProgramNumber, program.PmtPid)}
		h.check.ObserveAnnounced(analysis.PSIAnnounced{Table: analysis.PMTTable(program.ProgramNumber), PID: program.PmtPid, Offset: pos})
		if h.readers[program.PmtPid] == nil {
			h.readers[program.PmtPid] = &psiSectionReader{}
		}
	}
	for pid := range h.readers {
		if len(h.programsOn(pid)) == 0 {
			delete(h.readers, pid)
		}
	}
}

// result reports, once, the tables that never completed while sections of
// them were being collected at the end of the range, then returns the
// result.
func (h *psiHealthCollector) result() analysis.PSIHealthResult {
	if !h.finished {
		h.finished = true
		if len(h.pat.sets) > 0 {
			h.check.ObserveNeverCompleted(analysis.PATTable, h.end)
		}
		for number, p := range h.programs {
			if len(p.assembler.sets) > 0 {
				h.check.ObserveNeverCompleted(analysis.PMTTable(number), h.end)
			}
		}
	}
	return h.check.Result()
}

// ReportPSIHealth is the psi_health result: the availability and repetition of
// the PAT and of the PMT of every announced program.
type ReportPSIHealth struct {
	Status            string                 `json:"status"`
	RepetitionLimitMs float64                `json:"repetition_limit_ms"`
	TimeBase          ReportPSITimeBase      `json:"time_base"`
	Tables            []ReportPSITableHealth `json:"tables"`
}

// ReportPSITimeBase names the PCR that times the intervals. Status is pcr
// when at least two PCRs of one segment were found and none otherwise.
type ReportPSITimeBase struct {
	Status        string `json:"status"`
	ProgramNumber uint16 `json:"program_number"`
	PCRPID        string `json:"pcr_pid,omitempty"`
	SegmentCount  int    `json:"segment_count"`
}

// ReportPSITableHealth is the result of the PAT or of one program's PMT.
type ReportPSITableHealth struct {
	Table                    string              `json:"table"`
	ProgramNumber            *uint16             `json:"program_number,omitempty"`
	PID                      string              `json:"pid"`
	Status                   string              `json:"status"`
	Reasons                  []string            `json:"reasons"`
	AnnouncedOffsetBytes     *int64              `json:"announced_offset_bytes,omitempty"`
	WithdrawnOffsetBytes     *int64              `json:"withdrawn_offset_bytes,omitempty"`
	InstanceCount            int                 `json:"instance_count"`
	FirstInstanceOffsetBytes *int64              `json:"first_instance_offset_bytes,omitempty"`
	VersionChangeCount       int                 `json:"version_change_count"`
	CRCErrorCount            int                 `json:"crc_error_count"`
	MalformedSectionCount    int                 `json:"malformed_section_count"`
	IncompleteCount          int                 `json:"incomplete_count"`
	WrongTableIDCount        int                 `json:"wrong_table_id_count"`
	UnusablePacketCount      int                 `json:"unusable_packet_count"`
	FirstProblemOffsetBytes  *int64              `json:"first_problem_offset_bytes,omitempty"`
	Repetition               ReportPSIRepetition `json:"repetition"`
}

// ReportPSIRepetition is the repetition of one table. The interval fields are
// absent when no interval was measured.
type ReportPSIRepetition struct {
	Status                    string   `json:"status"`
	Reason                    string   `json:"reason,omitempty"`
	IntervalCount             int      `json:"interval_count"`
	MaxIntervalMs             *float64 `json:"max_interval_ms,omitempty"`
	MaxIntervalEndOffsetBytes *int64   `json:"max_interval_end_offset_bytes,omitempty"`
	LateIntervalCount         int      `json:"late_interval_count"`
	FirstLateEndOffsetBytes   *int64   `json:"first_late_end_offset_bytes,omitempty"`
}

var psiReasonNames = map[analysis.PSIReason]string{
	analysis.PSIReasonMissing:      "missing",
	analysis.PSIReasonLate:         "late",
	analysis.PSIReasonCRCError:     "crc_error",
	analysis.PSIReasonMalformed:    "malformed_section",
	analysis.PSIReasonIncomplete:   "incomplete",
	analysis.PSIReasonWrongTableID: "wrong_table_id",
	analysis.PSIReasonUnusable:     "unusable_packets",
}

func psiStatusName(status analysis.Status) string {
	switch status {
	case analysis.StatusNG:
		return "ng"
	case analysis.StatusInsufficientData:
		return "insufficient_data"
	case analysis.StatusSkipped:
		return "skipped"
	}
	return "ok"
}

func ticksToMs(ticks uint64) float64 { return float64(ticks) / float64(pcrTicksPerMs) }

func optionalOffset(offset int64, ok bool) *int64 {
	if !ok {
		return nil
	}
	return &offset
}

// reportPsiHealth returns the psi_health result.
func reportPsiHealth(h *psiHealthCollector) *ReportPSIHealth {
	result := h.result()
	r := &ReportPSIHealth{
		Status:            psiStatusName(result.Status),
		RepetitionLimitMs: ticksToMs(analysis.PSIRepetitionLimitTicks),
		TimeBase:          ReportPSITimeBase{Status: "none", ProgramNumber: h.refNumber, SegmentCount: result.Segments},
		Tables:            []ReportPSITableHealth{},
	}
	if h.reportedPcrPid != 0 {
		r.TimeBase.PCRPID = reportPID(h.reportedPcrPid)
	}
	if result.TimeBase {
		r.TimeBase.Status = "pcr"
	}
	for _, t := range result.Tables {
		row := ReportPSITableHealth{
			Table: "pat", PID: reportPID(t.PID), Status: psiStatusName(t.Status), Reasons: []string{},
			InstanceCount: t.Instances, FirstInstanceOffsetBytes: optionalOffset(t.FirstInstance, t.Instances > 0), VersionChangeCount: t.VersionChanges,
			CRCErrorCount: t.Problems[analysis.PSICRCError], MalformedSectionCount: t.Problems[analysis.PSIMalformed], IncompleteCount: t.Problems[analysis.PSIIncomplete],
			WrongTableIDCount: t.Problems[analysis.PSIWrongTableID], UnusablePacketCount: t.Problems[analysis.PSIUnusable],
		}
		if t.Table.Kind == analysis.PSITablePMT {
			number := t.Table.ProgramNumber
			row.Table, row.ProgramNumber = "pmt", &number
			row.AnnouncedOffsetBytes = optionalOffset(t.Announced, true)
			row.WithdrawnOffsetBytes = optionalOffset(t.Withdrawn, t.IsWithdrawn)
		}
		for _, reason := range t.Reasons {
			row.Reasons = append(row.Reasons, psiReasonNames[reason])
		}
		problems := 0
		for _, count := range t.Problems {
			problems += count
		}
		row.FirstProblemOffsetBytes = optionalOffset(t.FirstProblem, problems > 0)
		rep := t.Repetition
		row.Repetition = ReportPSIRepetition{Status: psiStatusName(rep.Status), IntervalCount: rep.Intervals, LateIntervalCount: rep.Late}
		switch rep.Status {
		case analysis.StatusNG:
			row.Repetition.Status = "late"
		case analysis.StatusSkipped:
			row.Repetition.Reason = "no_pcr_time_base"
		case analysis.StatusInsufficientData:
			row.Repetition.Reason = "no_complete_interval"
		}
		if rep.HasMax {
			ms := ticksToMs(rep.MaxTicks)
			row.Repetition.MaxIntervalMs = &ms
			row.Repetition.MaxIntervalEndOffsetBytes = optionalOffset(rep.MaxEnd, true)
		}
		row.Repetition.FirstLateEndOffsetBytes = optionalOffset(rep.FirstLate, rep.Late > 0)
		r.Tables = append(r.Tables, row)
	}
	return r
}

// dumpPsiHealth prints the text summary: one line for the check and one per
// table.
func dumpPsiHealth(r *ReportPSIHealth) {
	fmt.Println("-----------------------------")
	timeBase := "no PCR time base"
	if r.TimeBase.Status == "pcr" {
		timeBase = fmt.Sprintf("PCR PID %s of program %d", r.TimeBase.PCRPID, r.TimeBase.ProgramNumber)
	}
	fmt.Printf("PAT/PMT health: %s (repetition limit %.0f ms, ETSI TR 101 290 1.3.a/1.5.a; time base: %s)\n", psiStatusText(r.Status), r.RepetitionLimitMs, timeBase)
	for _, t := range r.Tables {
		name := "PAT"
		if t.ProgramNumber != nil {
			name = fmt.Sprintf("PMT program %d", *t.ProgramNumber)
		}
		var parts []string
		switch t.InstanceCount {
		case 0:
			parts = append(parts, "not found")
		case 1:
			parts = append(parts, "1 table")
		default:
			parts = append(parts, fmt.Sprintf("%d tables", t.InstanceCount))
		}
		rep := t.Repetition
		if rep.MaxIntervalMs != nil {
			intervals := "intervals"
			if rep.IntervalCount == 1 {
				intervals = "interval"
			}
			parts = append(parts, fmt.Sprintf("%d %s, max %.3f ms ending at 0x%08x", rep.IntervalCount, intervals, *rep.MaxIntervalMs, *rep.MaxIntervalEndOffsetBytes))
		} else if rep.Reason != "" {
			parts = append(parts, strings.ReplaceAll(rep.Reason, "_", " "))
		}
		if t.FirstProblemOffsetBytes != nil {
			parts = append(parts, fmt.Sprintf("first problem at 0x%08x", *t.FirstProblemOffsetBytes))
		}
		verdict := psiStatusText(t.Status)
		if len(t.Reasons) > 0 {
			counts := map[string]int{"crc_error": t.CRCErrorCount, "malformed_section": t.MalformedSectionCount, "incomplete": t.IncompleteCount, "wrong_table_id": t.WrongTableIDCount, "unusable_packets": t.UnusablePacketCount, "late": rep.LateIntervalCount}
			var reasons []string
			for _, reason := range t.Reasons {
				if count, ok := counts[reason]; ok {
					reason = fmt.Sprintf("%s x%d", reason, count)
				}
				reasons = append(reasons, reason)
			}
			verdict += ": " + strings.Join(reasons, ", ")
		}
		fmt.Printf("  %s PID %s : %s [%s]\n", name, t.PID, strings.Join(parts, ", "), verdict)
	}
}

func psiStatusText(status string) string {
	return strings.ToUpper(strings.ReplaceAll(status, "_", " "))
}
