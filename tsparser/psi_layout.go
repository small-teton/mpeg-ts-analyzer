package tsparser

import (
	"bytes"
	"fmt"
	"io"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// PSI layout of an analyzed range (--output analysis).
//
// psiLayoutRecorder sees every packet of the range once, from the first packet
// of the range to the end of the scan. It counts the packets of each PID and
// logs the PAT sections on PID 0 and the table_id 0x02 sections on any other
// PID, from the first packet of that PID whose first section starts with
// table_id 0x02. Which PIDs are PMT PIDs is only known at the end, because a
// PMT may be sent on a PID before a later PAT announces it, so the report
// keeps the PMT sections of the PIDs an applied PAT announced.
//
// A section identical to the previous one with the same PID, table_id,
// table_id_extension, current_next_indicator and section_number extends that
// entry (a run) instead of adding one, so the log grows with table changes and
// PSI errors, not with retransmissions or the range length. Runs stay
// unambiguous per PID: an invalid section, or data the section reader had to
// drop, closes every valid run of the PID, so the next valid copy starts a new
// run; and consecutive invalid copies with the same fault and header form one
// invalid run, which anything else on the PID ends. For a packet that starts a
// section, its PID, the section header and its offset therefore match at most
// one entry: the one whose [pos, last] contains the offset.
//
// The layouts are not decoded again from the log: they are the first complete
// PAT/PMT the analysis started from and the changes psiTracker applied, so they
// always agree with psi_transitions.

// psiSectionFault values of a section that cannot be used.
const (
	psiFaultMalformed  = "malformed"
	psiFaultCRC        = "invalid_crc32"
	psiFaultBeyondLast = "section_number_beyond_last"
	psiFaultParse      = "parse_error"
)

type pidObservation struct {
	count       int64
	first, last int64
}

// psiRunKey identifies the valid sections a run can extend.
type psiRunKey struct {
	pid           uint16
	tableID       uint8
	extension     uint16
	currentNext   uint8
	sectionNumber uint8
}

// psiSectionRun is one logged section and its identical repetitions, or a
// series of consecutive invalid copies.
type psiSectionRun struct {
	key       psiRunKey
	fault     string // "" for a valid section
	pos, last int64  // offsets of the packets where the first and last copies start
	count     int
	data      []byte // the first copy
	epoch     int    // the PID's epoch when the run started
	status    string
}

type psiLayoutRecorder struct {
	pids    [nullPidValue + 1]pidObservation
	readers [nullPidValue + 1]*psiSectionReader // nil for PIDs that carry no PAT or PMT
	// epoch counts the invalid sections and dropped data of each PID; a valid
	// run is only extended within its epoch.
	epoch   [nullPidValue + 1]int
	latest  [nullPidValue + 1]*psiSectionRun // the run of the PID's previous section
	runs    []*psiSectionRun
	byKey   map[psiRunKey][]*psiSectionRun // valid runs
	applied map[string]bool                // section bytes of every applied table
}

func newPsiLayoutRecorder() *psiLayoutRecorder {
	r := &psiLayoutRecorder{byKey: make(map[psiRunKey][]*psiSectionRun), applied: make(map[string]bool)}
	r.readers[patPidValue] = &psiSectionReader{}
	return r
}

// observe passes one packet of the range at offset pos.
func (r *psiLayoutRecorder) observe(tsPacket *TsPacket, pos int64) {
	pid := tsPacket.Pid()
	o := &r.pids[pid]
	if o.count == 0 {
		o.first = pos
	}
	o.count++
	o.last = pos
	reader := r.readers[pid]
	if reader == nil {
		if !startsPmtSection(tsPacket) {
			return
		}
		reader = &psiSectionReader{}
		r.readers[pid] = reader
	}
	// Damaged packets are reported by the discovery and the tracker; here they
	// only end the runs of the PID, as an invalid section does.
	sections, problems := reader.push(tsPacket, pos)
	if len(problems) > 0 {
		r.epoch[pid]++
		r.latest[pid] = nil
	}
	for _, section := range sections {
		r.addSection(pid, section)
	}
}

func (r *psiLayoutRecorder) addSection(pid uint16, section psiSectionData) {
	data := section.data
	tableID := uint8(0x02)
	if pid == patPidValue {
		tableID = 0x00
	}
	// Other tables on these PIDs are not part of the PSI layout, but they
	// end a run of invalid copies: an invalid run covers only consecutive
	// sections of the PID. Valid runs may still span them.
	if data[0] != tableID {
		r.latest[pid] = nil
		return
	}
	if fault := psiSectionFault(data); fault != "" {
		r.epoch[pid]++
		if run := r.latest[pid]; run != nil && run.fault == fault && sameSectionHeader(run.data, data) {
			run.count++
			run.last = section.pos
			return
		}
		run := &psiSectionRun{key: psiRunKey{pid: pid, tableID: tableID}, fault: fault, pos: section.pos, last: section.pos, count: 1, data: data}
		r.runs = append(r.runs, run)
		r.latest[pid] = run
		return
	}
	key := psiRunKey{pid: pid, tableID: tableID, extension: uint16(data[3])<<8 | uint16(data[4]), currentNext: data[5] & 0x01, sectionNumber: data[6]}
	runs := r.byKey[key]
	if n := len(runs); n > 0 && runs[n-1].epoch == r.epoch[pid] && bytes.Equal(runs[n-1].data, data) {
		run := runs[n-1]
		run.count++
		run.last = section.pos
		r.latest[pid] = run
		return
	}
	run := &psiSectionRun{key: key, pos: section.pos, last: section.pos, count: 1, data: data, epoch: r.epoch[pid]}
	r.runs = append(r.runs, run)
	r.byKey[key] = append(runs, run)
	r.latest[pid] = run
}

// sectionHeaderReadable reports whether a section has the long-form header
// fields: table_id_extension, version_number, current_next_indicator,
// section_number and last_section_number.
func sectionHeaderReadable(data []byte) bool {
	return len(data) >= 8 && data[1]&0x80 != 0
}

// sameSectionHeader reports whether two sections have the same header fields,
// or both lack them.
func sameSectionHeader(a, b []byte) bool {
	if sectionHeaderReadable(a) != sectionHeaderReadable(b) {
		return false
	}
	return !sectionHeaderReadable(a) || bytes.Equal(a[3:8], b[3:8])
}

// startsPmtSection reports whether the first section starting in a packet has
// table_id 0x02. PES packets never do: their payload starts with 0x00 0x00 0x01.
func startsPmtSection(tsPacket *TsPacket) bool {
	payload := tsPacket.Payload()
	if !tsPacket.PayloadUnitStartIndicator() || len(payload) == 0 {
		return false
	}
	at := 1 + int(payload[0])
	return at < len(payload) && payload[at] == 0x02
}

// psiSectionFault returns why a section cannot be used, or "" for a valid one.
func psiSectionFault(data []byte) string {
	switch {
	case len(data) < 12 || data[1]&0x80 == 0:
		return psiFaultMalformed
	case crc32(data) != 0:
		return psiFaultCRC
	case data[6] > data[7]:
		return psiFaultBeyondLast
	}
	var err error
	if data[0] == 0x00 {
		_, err = parsePatSection(data)
	} else {
		_, err = parsePmtSection(data)
	}
	if err != nil {
		return psiFaultParse
	}
	return ""
}

// markApplied records a table the analysis applied from the packet at pos.
// The run of each of its sections that was current at pos is the one that
// was applied.
func (r *psiLayoutRecorder) markApplied(pos int64, pid uint16, sections [][]byte) {
	for _, data := range sections {
		r.applied[string(data)] = true
		key := psiRunKey{pid: pid, tableID: data[0], extension: uint16(data[3])<<8 | uint16(data[4]), currentNext: 1, sectionNumber: data[6]}
		var current *psiSectionRun
		for _, run := range r.byKey[key] {
			if run.pos <= pos {
				current = run
			}
		}
		if current != nil && bytes.Equal(current.data, data) {
			current.status = "applied"
		}
	}
}

// classifyPsiSections marks the sections of the first complete PAT/PMT and of
// every applied change, then classifies the rest.
func (a *rangeAnalysis) classifyPsiSections() {
	layout := a.psi.layout
	layout.markApplied(a.pat.pos, patPidValue, sectionBytes(a.pat.sections))
	for _, entry := range a.programs {
		if entry.table != nil {
			layout.markApplied(entry.table.pos, entry.pmtPid, sectionBytes(entry.table.sections))
		}
	}
	for _, tr := range a.psi.transitions {
		layout.markApplied(tr.pos, tr.pid, tr.sections)
	}
	layout.classify()
}

// classify gives every run that was not applied its status: a repetition of
// an applied section (including a late replay of a superseded version, which
// the tracker ignores), a valid section that was never applied, or an invalid
// section.
func (r *psiLayoutRecorder) classify() {
	for _, run := range r.runs {
		switch {
		case run.status != "":
		case run.fault != "":
			run.status = "invalid"
		case r.applied[string(run.data)]:
			run.status = "retransmission"
		default:
			run.status = "not_applied"
		}
	}
}

// observeLead passes the packets from the first packet of the range up to the
// start of the scan, which PAT/PMT discovery read before the scan, to the
// recorder. A packet that cannot be parsed there is skipped.
func observeLead(reader io.ReadSeeker, start, end int64, recorder *psiLayoutRecorder, options options.Options, packetSize int) error {
	if _, err := reader.Seek(start, io.SeekStart); err != nil {
		return fmt.Errorf("file seek error: %w", err)
	}
	tsBuffer := make([]byte, packetSize)
	tsPacket := NewTsPacket()
	for pos := start; pos < end; pos += int64(packetSize) {
		if _, err := io.ReadFull(reader, tsBuffer); err != nil {
			return fmt.Errorf("file read error: %w", err)
		}
		tsPacket.Initialize(pos, options)
		tsPacket.Append(tsBuffer[packetSize-tsPayloadSize:])
		if tsPacket.Parse() == nil {
			recorder.observe(tsPacket, pos)
		}
	}
	return nil
}

// ReportPSILayout is the PSI layout of an analyzed range: the PAT and PMT
// sections seen, the layouts in effect and the PIDs observed.
type ReportPSILayout struct {
	Sections []ReportPSISection     `json:"sections"`
	Layouts  []ReportPSILayoutEntry `json:"layouts"`
	PIDs     []ReportObservedPID    `json:"pids"`
}

// ReportPSISection is a section and its identical repetitions. The header
// fields are absent for an invalid section.
type ReportPSISection struct {
	OffsetBytes     int64  `json:"offset_bytes"`
	LastOffsetBytes int64  `json:"last_offset_bytes"`
	Count           int    `json:"count"`
	PID             string `json:"pid"`
	Table           string `json:"table"`
	Status          string `json:"status"`
	InvalidReason   string `json:"invalid_reason,omitempty"`
	*ReportPSISectionHeader
}

type ReportPSISectionHeader struct {
	TransportStreamID    *uint16 `json:"transport_stream_id,omitempty"`
	ProgramNumber        *uint16 `json:"program_number,omitempty"`
	VersionNumber        uint8   `json:"version_number"`
	SectionNumber        uint8   `json:"section_number"`
	LastSectionNumber    uint8   `json:"last_section_number"`
	CurrentNextIndicator uint8   `json:"current_next_indicator"`
}

// ReportPSILayoutEntry is a layout and the packets it applies to.
type ReportPSILayoutEntry struct {
	OffsetBytes       int64                 `json:"offset_bytes"`
	EndOffsetBytes    int64                 `json:"end_offset_bytes"`
	TransitionIndices []int                 `json:"transition_indices"`
	PATVersion        uint8                 `json:"pat_version"`
	PATOffsetBytes    int64                 `json:"pat_offset_bytes"`
	Programs          []ReportLayoutProgram `json:"programs"`
}

// ReportLayoutProgram is a program of a layout. The PMT fields are absent when
// no PMT of the program is known (it was not found, or the program was added
// by a later PAT and is not analyzed).
type ReportLayoutProgram struct {
	ProgramNumber     uint16                   `json:"program_number"`
	PMTPID            string                   `json:"pmt_pid"`
	PMTVersion        *uint8                   `json:"pmt_version,omitempty"`
	PMTOffsetBytes    *int64                   `json:"pmt_offset_bytes,omitempty"`
	PCRPID            string                   `json:"pcr_pid,omitempty"`
	ElementaryStreams []ReportElementaryStream `json:"elementary_streams"`
}

// ReportObservedPID is a PID seen in the range and what the layouts say it
// carries.
type ReportObservedPID struct {
	PID              string          `json:"pid"`
	PacketCount      int64           `json:"packet_count"`
	FirstOffsetBytes int64           `json:"first_offset_bytes"`
	LastOffsetBytes  int64           `json:"last_offset_bytes"`
	Roles            []ReportPIDRole `json:"roles"`
}

type ReportPIDRole struct {
	Kind                  string  `json:"kind"`
	ProgramNumber         *uint16 `json:"program_number,omitempty"`
	StreamType            string  `json:"stream_type,omitempty"`
	StreamTypeDescription string  `json:"stream_type_description,omitempty"`
}

// layoutProgram is a program of the layout being rebuilt.
type layoutProgram struct {
	number, pmtPid uint16
	known          bool
	version        uint8
	pmtPos         int64
	pcrPid         uint16
	streams        []ProgramInfo
}

type pidRole struct {
	kind       string
	program    uint16
	streamType int // -1 when not an elementary stream
}

// reportPsiLayout rebuilds the layouts from the first complete PAT/PMT and the
// applied changes, and attaches the section log and the observed PIDs.
func reportPsiLayout(analysis *rangeAnalysis) *ReportPSILayout {
	r := &ReportPSILayout{Sections: []ReportPSISection{}, Layouts: []ReportPSILayoutEntry{}, PIDs: []ReportObservedPID{}}
	// Sections other than PAT sections are kept for the PMT PIDs of the PATs
	// the analysis applied.
	announced := map[uint16]bool{patPidValue: true}
	pats := [][]Program{analysis.pat.Programs()}
	for _, tr := range analysis.psi.transitions {
		pats = append(pats, tr.programs)
	}
	for _, programs := range pats {
		for _, program := range programs {
			announced[program.PmtPid] = true
		}
	}
	for _, run := range analysis.psi.layout.runs {
		if announced[run.key.pid] {
			r.Sections = append(r.Sections, reportPsiSection(run))
		}
	}

	patVersion, patPos := analysis.pat.sections[0].versionNumber, analysis.pat.pos
	var programs []layoutProgram
	for _, entry := range analysis.programs {
		p := layoutProgram{number: entry.program.ProgramNumber, pmtPid: entry.pmtPid}
		if t := entry.table; t != nil {
			p.known, p.version, p.pmtPos, p.pcrPid, p.streams = true, t.versionNumber, t.pos, t.pcrPid, t.programInfos
		}
		programs = append(programs, p)
	}
	roles := make(map[uint16][]pidRole)
	start, indices := analysis.leadStart, []int{}
	emit := func(end int64) {
		r.Layouts = append(r.Layouts, reportLayout(start, end, indices, patVersion, patPos, programs))
		addLayoutRoles(roles, programs)
	}
	transitions := analysis.psi.transitions
	for i, tr := range transitions {
		// Changes completed by the same packet start one layout.
		if i == 0 || tr.pos != transitions[i-1].pos {
			next := tr.pos + int64(analysis.packetSize)
			emit(next)
			start, indices = next, []int{}
		}
		indices = append(indices, i)
		if tr.table == "pat" {
			patVersion, patPos = tr.version, tr.pos
			programs = applyPatToLayout(programs, tr.programs)
			continue
		}
		for idx := range programs {
			if p := &programs[idx]; p.number == tr.programNumber {
				p.pmtPid, p.known, p.version, p.pmtPos, p.pcrPid, p.streams = tr.pid, true, tr.version, tr.pos, tr.pcrPid, tr.streams
			}
		}
	}
	emit(analysis.scanEnd)

	for pid, o := range analysis.psi.layout.pids {
		if o.count == 0 {
			continue
		}
		observed := ReportObservedPID{PID: reportPID(uint16(pid)), PacketCount: o.count, FirstOffsetBytes: o.first, LastOffsetBytes: o.last, Roles: []ReportPIDRole{}}
		pidRoles := roles[uint16(pid)]
		if len(pidRoles) == 0 {
			pidRoles = []pidRole{{kind: "unannounced", streamType: -1}}
		}
		for _, role := range pidRoles {
			observed.Roles = append(observed.Roles, reportPidRole(role))
		}
		r.PIDs = append(r.PIDs, observed)
	}
	return r
}

// applyPatToLayout returns the programs of a new PAT. A program that stays
// keeps its PMT, also when its PMT PID moves, until a PMT change replaces it,
// as in psiTracker; a removed program is not resumed when a later PAT lists it
// again, and an added program has no known PMT.
func applyPatToLayout(previous []layoutProgram, programs []Program) []layoutProgram {
	var next []layoutProgram
	for _, program := range programs {
		p := layoutProgram{number: program.ProgramNumber}
		for _, old := range previous {
			if old.number == program.ProgramNumber {
				p = old
			}
		}
		p.pmtPid = program.PmtPid
		next = append(next, p)
	}
	return next
}

func reportLayout(start, end int64, indices []int, patVersion uint8, patPos int64, programs []layoutProgram) ReportPSILayoutEntry {
	layout := ReportPSILayoutEntry{OffsetBytes: start, EndOffsetBytes: end, TransitionIndices: indices, PATVersion: patVersion, PATOffsetBytes: patPos, Programs: []ReportLayoutProgram{}}
	for _, p := range programs {
		program := ReportLayoutProgram{ProgramNumber: p.number, PMTPID: reportPID(p.pmtPid), ElementaryStreams: []ReportElementaryStream{}}
		if p.known {
			version, pos := p.version, p.pmtPos
			program.PMTVersion, program.PMTOffsetBytes, program.PCRPID = &version, &pos, reportPID(p.pcrPid)
			program.ElementaryStreams = reportElementaryStreams(p.streams)
		}
		layout.Programs = append(layout.Programs, program)
	}
	return layout
}

// addLayoutRoles adds what each PID of a layout carries, once per role.
func addLayoutRoles(roles map[uint16][]pidRole, programs []layoutProgram) {
	add := func(pid uint16, role pidRole) {
		for _, known := range roles[pid] {
			if known == role {
				return
			}
		}
		roles[pid] = append(roles[pid], role)
	}
	add(patPidValue, pidRole{kind: "pat", streamType: -1})
	add(nullPidValue, pidRole{kind: "null", streamType: -1})
	for _, p := range programs {
		add(p.pmtPid, pidRole{kind: "pmt", program: p.number, streamType: -1})
		if !p.known {
			continue
		}
		if p.pcrPid != nullPidValue {
			add(p.pcrPid, pidRole{kind: "pcr", program: p.number, streamType: -1})
		}
		for _, info := range p.streams {
			add(info.elementaryPid, pidRole{kind: "elementary_stream", program: p.number, streamType: int(info.streamType)})
		}
	}
}

func reportPidRole(role pidRole) ReportPIDRole {
	r := ReportPIDRole{Kind: role.kind}
	if role.kind == "pmt" || role.kind == "pcr" || role.kind == "elementary_stream" {
		program := role.program
		r.ProgramNumber = &program
	}
	if role.streamType >= 0 {
		r.StreamType = fmt.Sprintf("0x%02x", role.streamType)
		r.StreamTypeDescription = StreamTypeString(uint8(role.streamType))
	}
	return r
}

func reportPsiSection(run *psiSectionRun) ReportPSISection {
	table := "pmt"
	if run.key.tableID == 0x00 {
		table = "pat"
	}
	s := ReportPSISection{OffsetBytes: run.pos, LastOffsetBytes: run.last, Count: run.count, PID: reportPID(run.key.pid), Table: table, Status: run.status, InvalidReason: run.fault}
	data := run.data
	if !sectionHeaderReadable(data) {
		return s
	}
	header := &ReportPSISectionHeader{VersionNumber: data[5] >> 1 & 0x1F, SectionNumber: data[6], LastSectionNumber: data[7], CurrentNextIndicator: data[5] & 0x01}
	extension := uint16(data[3])<<8 | uint16(data[4])
	if table == "pat" {
		header.TransportStreamID = &extension
	} else {
		header.ProgramNumber = &extension
	}
	s.ReportPSISectionHeader = header
	return s
}
