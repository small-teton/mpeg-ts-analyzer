package tsparser

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// PAT and PMT version tracking during the analysis pass.
//
// The analysis starts from the first complete PAT and PMT. While it scans, the
// PAT and the PMT of every analyzed program are assembled again from their
// sections, with the same rules as the initial acquisition (psiAssembler). A
// complete table whose version_number differs from the active one is applied
// from the packet after the one that completed it; incomplete tables, tables
// with an invalid section and "next" tables (current_next_indicator 0) are
// never applied. Each applied change is recorded as a psiTransition.
//
// Transition policy:
//
//   - PCR_PID change: the first PCR on the new PCR_PID is a pcr_pid_change
//     discontinuity (analysis.PCRClock), so no PCR interval, jitter estimate,
//     bitrate window or PCR-PTS/DTS gap spans the change (see
//     programAnalyzer.setLayout).
//   - Removed or retyped elementary PID: its buffered PES is finalized at the
//     change and its PES and timestamp state is dropped; packets of a removed
//     PID are ignored from then on.
//   - Added elementary PID, new PCR_PID or new PMT PID: PES and timestamp
//     state starts at its next payload_unit_start_indicator, and continuity
//     state is reset unless another analyzed program already uses the PID.
//   - A PID listed by several layouts keeps one row in the bitrate and
//     continuity summaries, labeled from the latest layout listing it.
//   - PAT change that moves a program's PMT PID: the PMT is read from the new
//     PID. Its first complete table is applied even with the same
//     version_number when its content differs.
//   - Program removed from the PAT: its analysis stops at that packet and is
//     not resumed. Programs added to the PAT are recorded, not analyzed.
//   - A table with the active version_number but different content is not
//     applied (ISO/IEC 13818-1 requires a version change); a warning is given.
//   - A table byte-identical to the one last applied with its version_number
//     is a late retransmission of a superseded version and is ignored, so the
//     layout never rolls back. version_number wraps after 31, so a reused
//     version with different content is applied.
//   - Tables are compared by their section bytes, descriptors included.

// psiTransition is one applied PAT or PMT version change.
type psiTransition struct {
	pos             int64 // offset of the packet that completed the new table
	table           string
	pid             uint16
	previousVersion uint8
	version         uint8
	sections        [][]byte // the new table's sections in section_number order

	// PAT
	programs        []Program
	programsAdded   []Program
	programsRemoved []Program
	pmtPidChanges   []pmtPidChange

	// PMT
	programNumber     uint16
	previousPcrPid    uint16
	pcrPid            uint16
	streams           []ProgramInfo
	streamsAdded      []ProgramInfo
	streamsRemoved    []ProgramInfo
	streamTypeChanges []streamTypeChange
}

type pmtPidChange struct {
	programNumber, previousPmtPid, pmtPid uint16
}

type streamTypeChange struct {
	pid                            uint16
	previousStreamType, streamType uint8
}

// psiTracker follows the PAT and the PMTs of the analyzed programs.
type psiTracker struct {
	options     options.Options
	packetSize  int
	quiet       bool
	continuity  *continuityCounterSummary
	trust       *transportTrustSummary // names PIDs of applied layouts, when set
	patVersion  uint8
	patTable    *patTable
	patPrograms []Program
	patHistory  map[uint8]*patTable // last applied table of each version_number
	patReader   psiSectionReader
	pat         *psiAssembler[*Pat]
	programs    []*trackedProgram
	transitions []psiTransition
	warnings    []psiDiagnostic
	warned      map[string]bool
	layout      *psiLayoutRecorder // records sections and PIDs for --output analysis
	sdt         *sdtCollector
	health      *psiHealthCollector // follows PAT changes for psi_health, when set
}

type trackedProgram struct {
	number    uint16
	analyzer  *programAnalyzer
	pmtPid    uint16
	table     *pmtTable
	history   map[uint8]*pmtTable // last applied table of each version_number
	reader    psiSectionReader
	assembler *psiAssembler[*Pmt]
	moved     bool // the PMT PID changed and no table was completed on it yet
	removed   bool
}

func newPsiTracker(pat *patTable, continuity *continuityCounterSummary, options options.Options, packetSize int, quiet bool) *psiTracker {
	first := pat.sections[0]
	return &psiTracker{
		options:     options,
		packetSize:  packetSize,
		quiet:       quiet,
		continuity:  continuity,
		patVersion:  first.versionNumber,
		patTable:    pat,
		patPrograms: pat.Programs(),
		patHistory:  map[uint8]*patTable{first.versionNumber: pat},
		pat:         &psiAssembler[*Pat]{name: "PAT", tableID: 0x00, extension: first.transportStreamID, parse: parsePatSection},
		warned:      make(map[string]bool),
		sdt:         newSdtCollector(),
	}
}

// addProgram follows the PMT of an analyzed program, starting from table.
func (t *psiTracker) addProgram(number, pmtPid uint16, table *pmtTable, analyzer *programAnalyzer) {
	t.programs = append(t.programs, &trackedProgram{
		number: number, analyzer: analyzer, pmtPid: pmtPid, table: table,
		history: map[uint8]*pmtTable{table.versionNumber: table}, assembler: newPmtAssembler(number, pmtPid),
	})
}

// sameSections reports whether two tables consist of the same section bytes.
func sameSections[T *Pat | *Pmt](a, b []T, raw func(T) []byte) bool {
	return slices.EqualFunc(a, b, func(x, y T) bool { return bytes.Equal(raw(x), raw(y)) })
}

// sectionBytes returns the bytes of each section of a table.
func sectionBytes[T *Pat | *Pmt](sections []T) [][]byte {
	raw := make([][]byte, len(sections))
	for i, section := range sections {
		switch s := any(section).(type) {
		case *Pat:
			raw[i] = s.buf
		case *Pmt:
			raw[i] = s.buf
		}
	}
	return raw
}

func samePatTable(a, b *patTable) bool {
	return sameSections(a.sections, b.sections, func(p *Pat) []byte { return p.buf })
}

func samePmtTable(a, b *pmtTable) bool {
	return sameSections(a.sections, b.sections, func(p *Pmt) []byte { return p.buf })
}

// observe passes one packet of the scan at offset pos.
func (t *psiTracker) observe(tsPacket *TsPacket, pos int64, continuity *continuityTracker) {
	if t.layout != nil {
		t.layout.observe(tsPacket, pos)
	}
	t.sdt.observe(tsPacket, pos)
	pid := tsPacket.Pid()
	if pid == patPidValue {
		for _, sections := range collectTables(t, &t.patReader, t.pat, tsPacket, pos) {
			t.applyPat(pos, sections, continuity)
		}
		return
	}
	for _, p := range t.programs {
		if p.removed || pid != p.pmtPid || pos < p.analyzer.start {
			continue
		}
		for _, sections := range collectTables(t, &p.reader, p.assembler, tsPacket, pos) {
			t.applyPmt(p, pos, sections, continuity)
		}
	}
}

// readsPid reports whether the tracker reads the payload of pid at offset pos:
// the PAT, or the PMT PID of an analyzed program that is still in the PAT. A
// nil tracker reads nothing.
func (t *psiTracker) readsPid(pid uint16, pos int64) bool {
	if t == nil {
		return false
	}
	if pid == patPidValue {
		return true
	}
	for _, p := range t.programs {
		if !p.removed && pid == p.pmtPid && pos >= p.analyzer.start {
			return true
		}
	}
	return false
}

// collectTables returns the tables the packet completes. A continuity_counter
// jump inside a section is not reported again: the continuity summary already
// counts it, and the next repetition of the table is used.
func collectTables[T any](t *psiTracker, reader *psiSectionReader, assembler *psiAssembler[T], tsPacket *TsPacket, pos int64) [][]T {
	sections, problems := reader.push(tsPacket, pos)
	for _, problem := range problems {
		if !strings.HasPrefix(problem, "continuity_counter jumped") {
			t.warn(pos, assembler.name+": "+problem)
		}
	}
	var tables [][]T
	for _, section := range sections {
		if table, ok := assembler.add(section.data, section.pos); ok {
			tables = append(tables, table)
		}
	}
	for _, d := range assembler.diagnostics {
		t.warn(d.pos, d.message)
	}
	assembler.diagnostics = nil
	return tables
}

// warn records a problem once per message, so a stream that repeats a broken
// table does not produce one warning per repetition.
func (t *psiTracker) warn(pos int64, message string) {
	if t.warned[message] {
		return
	}
	t.warned[message] = true
	t.warnings = append(t.warnings, psiDiagnostic{pos, message})
	if !t.quiet {
		fmt.Printf("0x%08x %s\n", pos, message)
	}
}

func (t *psiTracker) applyPat(pos int64, sections []*Pat, continuity *continuityTracker) {
	table, conflicts := newPatTable(sections)
	version := sections[0].versionNumber
	if version == t.patVersion {
		if !samePatTable(table, t.patTable) {
			t.warn(pos, fmt.Sprintf("PAT changed without a version change (version %d); change ignored", version))
		}
		return
	}
	if previous, ok := t.patHistory[version]; ok && samePatTable(previous, table) {
		return
	}
	t.patHistory[version] = table
	for _, conflict := range conflicts {
		t.warn(pos, conflict)
	}
	tr := psiTransition{pos: pos, table: "pat", pid: patPidValue, previousVersion: t.patVersion, version: version, sections: sectionBytes(table.sections), programs: table.Programs()}
	for _, program := range table.Programs() {
		idx := slices.IndexFunc(t.patPrograms, func(p Program) bool { return p.ProgramNumber == program.ProgramNumber })
		if idx < 0 {
			tr.programsAdded = append(tr.programsAdded, program)
		} else if previous := t.patPrograms[idx].PmtPid; previous != program.PmtPid {
			tr.pmtPidChanges = append(tr.pmtPidChanges, pmtPidChange{program.ProgramNumber, previous, program.PmtPid})
		}
	}
	for _, program := range t.patPrograms {
		if !slices.ContainsFunc(table.Programs(), func(p Program) bool { return p.ProgramNumber == program.ProgramNumber }) {
			tr.programsRemoved = append(tr.programsRemoved, program)
		}
	}
	t.patVersion, t.patTable, t.patPrograms = version, table, table.Programs()
	if t.health != nil {
		t.health.patApplied(pos, t.patPrograms)
	}

	for _, p := range t.programs {
		if p.removed {
			continue
		}
		idx := slices.IndexFunc(t.patPrograms, func(program Program) bool { return program.ProgramNumber == p.number })
		if idx < 0 {
			p.removed = true
			p.analyzer.stop(pos + int64(t.packetSize))
			continue
		}
		if pmtPid := t.patPrograms[idx].PmtPid; pmtPid != p.pmtPid {
			t.setLayout(p, pmtPid, p.table, continuity)
			p.reader, p.assembler, p.moved = psiSectionReader{}, newPmtAssembler(p.number, pmtPid), true
		}
	}
	t.record(tr, table.Dump)
}

func (t *psiTracker) applyPmt(p *trackedProgram, pos int64, sections []*Pmt, continuity *continuityTracker) {
	table, conflicts := newPmtTable(sections)
	moved := p.moved
	p.moved = false
	same := samePmtTable(table, p.table)
	if table.versionNumber == p.table.versionNumber && (same || !moved) {
		if !same {
			t.warn(pos, fmt.Sprintf("%s changed without a version change (version %d); change ignored", p.assembler.name, table.versionNumber))
		}
		return
	}
	if previous, ok := p.history[table.versionNumber]; ok && samePmtTable(previous, table) {
		return
	}
	p.history[table.versionNumber] = table
	for _, conflict := range conflicts {
		t.warn(pos, conflict)
	}
	previous := p.table
	tr := psiTransition{
		pos: pos, table: "pmt", pid: p.pmtPid, previousVersion: previous.versionNumber, version: table.versionNumber, sections: sectionBytes(table.sections),
		programNumber: p.number, previousPcrPid: previous.pcrPid, pcrPid: table.pcrPid, streams: table.programInfos,
	}
	previousTypes := make(map[uint16]uint8, len(previous.programInfos))
	for _, info := range previous.programInfos {
		previousTypes[info.elementaryPid] = info.streamType
	}
	types := make(map[uint16]uint8, len(table.programInfos))
	for _, info := range table.programInfos {
		types[info.elementaryPid] = info.streamType
		if streamType, ok := previousTypes[info.elementaryPid]; !ok {
			tr.streamsAdded = append(tr.streamsAdded, info)
		} else if streamType != info.streamType {
			tr.streamTypeChanges = append(tr.streamTypeChanges, streamTypeChange{info.elementaryPid, streamType, info.streamType})
		}
	}
	for _, info := range previous.programInfos {
		if _, ok := types[info.elementaryPid]; !ok {
			tr.streamsRemoved = append(tr.streamsRemoved, info)
		}
	}
	t.setLayout(p, p.pmtPid, table, continuity)
	t.record(tr, table.Dump)
}

// layoutPids are the PIDs a program layout uses besides the PAT.
func layoutPids(pmtPid uint16, table *pmtTable) []uint16 {
	pids := []uint16{pmtPid}
	if table.pcrPid != 0 && table.pcrPid != nullPidValue {
		pids = append(pids, table.pcrPid)
	}
	for _, info := range table.programInfos {
		pids = append(pids, info.elementaryPid)
	}
	return pids
}

// setLayout makes p follow a new PMT PID or PMT. A PID new to every analyzed
// program starts a fresh continuity sequence, so a PID that returns after a
// pause is not reported as a continuity error.
func (t *psiTracker) setLayout(p *trackedProgram, pmtPid uint16, table *pmtTable, continuity *continuityTracker) {
	known := make(map[uint16]bool)
	for _, other := range t.programs {
		if !other.removed {
			for _, pid := range layoutPids(other.pmtPid, other.table) {
				known[pid] = true
			}
		}
	}
	for _, pid := range layoutPids(pmtPid, table) {
		if !known[pid] {
			delete(continuity.states, pid)
		}
	}
	p.pmtPid, p.table = pmtPid, table
	t.continuity.addProgramLabels(pmtPid, table.pcrPid, table.programInfos)
	if t.trust != nil {
		t.trust.addProgramLabels(pmtPid, table.pcrPid, table.programInfos)
	}
	p.analyzer.setLayout(pmtPid, table.pcrPid, table.programInfos)
}

func (t *psiTracker) record(tr psiTransition, dump func()) {
	t.transitions = append(t.transitions, tr)
	if t.quiet {
		return
	}
	fmt.Printf("0x%08x %s\n", tr.pos, tr.String())
	if t.options.DumpPsi {
		dump()
	}
}

// finish reports candidate tables that were still incomplete at the end of
// the scan, which ends at pos.
func (t *psiTracker) finish(pos int64) {
	for _, message := range t.pat.pending(t.patVersion) {
		t.warn(pos, message)
	}
	for _, p := range t.programs {
		if !p.removed {
			for _, message := range p.assembler.pending(p.table.versionNumber) {
				t.warn(pos, message)
			}
		}
	}
}

// String describes the transition for text output.
func (tr psiTransition) String() string {
	var changes []string
	if tr.table == "pat" {
		for _, c := range tr.pmtPidChanges {
			changes = append(changes, fmt.Sprintf("program %d PMT PID 0x%04x -> 0x%04x", c.programNumber, c.previousPmtPid, c.pmtPid))
		}
		for _, p := range tr.programsAdded {
			changes = append(changes, fmt.Sprintf("added program %d (PMT PID 0x%04x)", p.ProgramNumber, p.PmtPid))
		}
		for _, p := range tr.programsRemoved {
			changes = append(changes, fmt.Sprintf("removed program %d (PMT PID 0x%04x)", p.ProgramNumber, p.PmtPid))
		}
		if len(changes) == 0 {
			changes = append(changes, "no program change")
		}
		return fmt.Sprintf("PAT version %d -> %d: %s", tr.previousVersion, tr.version, strings.Join(changes, ", "))
	}
	if tr.previousPcrPid != tr.pcrPid {
		changes = append(changes, fmt.Sprintf("PCR_PID 0x%04x -> 0x%04x", tr.previousPcrPid, tr.pcrPid))
	}
	for _, info := range tr.streamsAdded {
		changes = append(changes, fmt.Sprintf("added 0x%04x (stream_type 0x%02x)", info.elementaryPid, info.streamType))
	}
	for _, info := range tr.streamsRemoved {
		changes = append(changes, fmt.Sprintf("removed 0x%04x (stream_type 0x%02x)", info.elementaryPid, info.streamType))
	}
	for _, c := range tr.streamTypeChanges {
		changes = append(changes, fmt.Sprintf("stream_type of 0x%04x 0x%02x -> 0x%02x", c.pid, c.previousStreamType, c.streamType))
	}
	if len(changes) == 0 {
		changes = append(changes, "no stream change")
	}
	return fmt.Sprintf("PMT of program %d on PID 0x%04x version %d -> %d: %s", tr.programNumber, tr.pid, tr.previousVersion, tr.version, strings.Join(changes, ", "))
}
