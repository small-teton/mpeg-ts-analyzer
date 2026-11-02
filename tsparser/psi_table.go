package tsparser

import (
	"fmt"
	"io"
	"slices"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// patTable is a complete PAT: every section of one version.
type patTable struct {
	sections []*Pat
	programs []Program
}

// newPatTable merges the programs of every section. A program listed more than
// once keeps its first PMT PID in section order; the list is ordered by
// program_number so it does not depend on the order the sections arrived in.
func newPatTable(sections []*Pat) (*patTable, []string) {
	t := &patTable{sections: sections}
	var conflicts []string
	for _, section := range sections {
		for _, program := range section.Programs() {
			idx := slices.IndexFunc(t.programs, func(p Program) bool { return p.ProgramNumber == program.ProgramNumber })
			if idx < 0 {
				t.programs = append(t.programs, program)
			} else if t.programs[idx].PmtPid != program.PmtPid {
				conflicts = append(conflicts, fmt.Sprintf("PAT lists program %d with PMT PIDs 0x%x and 0x%x; using 0x%x", program.ProgramNumber, t.programs[idx].PmtPid, program.PmtPid, t.programs[idx].PmtPid))
			}
		}
	}
	slices.SortFunc(t.programs, func(a, b Program) int { return int(a.ProgramNumber) - int(b.ProgramNumber) })
	return t, conflicts
}

// Programs returns every program of the table (program_number 0, the network
// PID, excluded), ordered by program_number.
func (t *patTable) Programs() []Program { return t.programs }

// Dump prints every section of the table in section_number order.
func (t *patTable) Dump() {
	for _, section := range t.sections {
		section.Dump()
	}
}

// pmtTable is a complete PMT: every section of one version. The embedded Pmt
// is the merged view the analysis uses: the header and PCR_PID of section 0,
// the program descriptors of every section, and every elementary stream once.
type pmtTable struct {
	*Pmt
	sections []*Pmt
}

func newPmtTable(sections []*Pmt) (*pmtTable, []string) {
	merged := *sections[0]
	merged.descriptors, merged.programInfos = nil, nil
	var conflicts []string
	for number, section := range sections {
		if section.pcrPid != merged.pcrPid {
			conflicts = append(conflicts, fmt.Sprintf("PMT of program %d has PCR_PID 0x%x in section 0 and 0x%x in section %d; using 0x%x", merged.programNumber, merged.pcrPid, section.pcrPid, number, merged.pcrPid))
		}
		merged.descriptors = append(merged.descriptors, section.descriptors...)
		for _, info := range section.programInfos {
			idx := slices.IndexFunc(merged.programInfos, func(i ProgramInfo) bool { return i.elementaryPid == info.elementaryPid })
			if idx < 0 {
				merged.programInfos = append(merged.programInfos, info)
			} else if merged.programInfos[idx].streamType != info.streamType {
				conflicts = append(conflicts, fmt.Sprintf("PMT of program %d lists elementary PID 0x%x with stream types 0x%02x and 0x%02x; using 0x%02x", merged.programNumber, info.elementaryPid, merged.programInfos[idx].streamType, info.streamType, merged.programInfos[idx].streamType))
			}
		}
	}
	return &pmtTable{Pmt: &merged, sections: sections}, conflicts
}

// Dump prints every section of the table in section_number order.
func (t *pmtTable) Dump() {
	for _, section := range t.sections {
		section.Dump()
	}
}

func parsePatSection(data []byte) (*Pat, error) {
	pat := NewPat()
	pat.Append(data)
	return pat, pat.Parse()
}

func parsePmtSection(data []byte) (*Pmt, error) {
	pmt := NewPmt()
	pmt.Append(data)
	return pmt, pmt.Parse()
}

// acquirePat reads packets from *pos until a complete PAT has been collected.
// Diagnostics are returned even when no table is found.
func acquirePat(reader io.Reader, pos *int64, options options.Options, packetSize int, endOffset int64) (*patTable, []psiDiagnostic, error) {
	assembler := &psiAssembler[*Pat]{name: "PAT", tableID: 0x00, anyExtension: true, parse: parsePatSection}
	sections, err := acquireTable(reader, pos, 0x0000, assembler, options, packetSize, endOffset)
	if err != nil {
		return nil, assembler.diagnostics, err
	}
	table, conflicts := newPatTable(sections)
	return table, appendConflicts(assembler.diagnostics, *pos, conflicts), nil
}

// acquirePmt reads packets from *pos until a complete PMT of programNumber has
// been collected on pid.
func acquirePmt(reader io.Reader, pos *int64, pid, programNumber uint16, options options.Options, packetSize int, endOffset int64) (*pmtTable, []psiDiagnostic, error) {
	name := fmt.Sprintf("PMT of program %d on PID 0x%04x", programNumber, pid)
	assembler := &psiAssembler[*Pmt]{name: name, tableID: 0x02, extension: programNumber, parse: parsePmtSection}
	sections, err := acquireTable(reader, pos, pid, assembler, options, packetSize, endOffset)
	if err != nil {
		return nil, assembler.diagnostics, err
	}
	table, conflicts := newPmtTable(sections)
	return table, appendConflicts(assembler.diagnostics, *pos, conflicts), nil
}

func appendConflicts(diagnostics []psiDiagnostic, pos int64, conflicts []string) []psiDiagnostic {
	for _, message := range conflicts {
		diagnostics = append(diagnostics, psiDiagnostic{pos, message})
	}
	return diagnostics
}

// acquireTable feeds the sections on pid to the assembler until it completes a
// table. Reading then continues up to the first packet on pid that starts a
// new payload unit after the start of the completing section, which is where
// a single-section table has always been considered complete; the analysis
// that follows starts there. Problems with packets or sections become
// diagnostics; a table still incomplete at the end of the input or range is an
// error.
func acquireTable[T any](reader io.Reader, pos *int64, pid uint16, assembler *psiAssembler[T], options options.Options, packetSize int, endOffset int64) ([]T, error) {
	tsBuffer := make([]byte, packetSize)
	tsPacket := NewTsPacket()
	var sectionReader psiSectionReader
	var table []T
	var completedAt int64

	for endOffset <= 0 || *pos+int64(packetSize) <= endOffset {
		size, err := io.ReadFull(reader, tsBuffer)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			// A trailing partial packet cannot carry a usable section; stop as at EOF.
			break
		} else if err != nil {
			return nil, fmt.Errorf("%s buffering error: file read error: %w", assembler.name, err)
		}

		tsData := tsBuffer
		if packetSize > tsPayloadSize {
			tsData = tsBuffer[packetSize-tsPayloadSize:]
		}
		packetPos := *pos
		tsPacket.Initialize(packetPos, options)
		tsPacket.Append(tsData)
		if err := tsPacket.Parse(); err != nil {
			return nil, fmt.Errorf("%s buffering error: failed to parse TS packet: %w", assembler.name, err)
		}
		*pos += int64(size)

		if tsPacket.Pid() != pid || len(tsPacket.Payload()) == 0 {
			continue
		}
		if table != nil {
			if tsPacket.PayloadUnitStartIndicator() {
				return table, nil
			}
			continue
		}
		sections, problems := sectionReader.push(tsPacket, packetPos)
		for _, problem := range problems {
			assembler.diagnostics = append(assembler.diagnostics, psiDiagnostic{packetPos, assembler.name + ": " + problem})
		}
		for _, section := range sections {
			if parsed, ok := assembler.add(section.data, section.pos); ok {
				table, completedAt = parsed, section.pos
				break
			}
		}
		if table != nil && tsPacket.PayloadUnitStartIndicator() && completedAt != packetPos {
			return table, nil
		}
	}
	if table != nil {
		return table, nil
	}
	return nil, assembler.incomplete()
}
