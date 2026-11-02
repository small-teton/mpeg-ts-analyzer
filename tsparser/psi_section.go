package tsparser

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// psiSectionData is one complete PSI section and the offset of the packet in
// which it starts.
type psiSectionData struct {
	data []byte
	pos  int64
}

// psiSectionReader extracts complete sections from the packets of one PID. A
// section may span packets, and one packet may carry the end of a section and
// the start of others. It keeps no other state, so packets can be fed from any
// point of a scan.
type psiSectionReader struct {
	buf        []byte
	startPos   int64 // offset of the packet in which the buffered section starts
	collecting bool
	lastCC     uint8
}

// psiSectionSize returns the size of the section at the start of data, or
// false when data is too short to hold its section_length.
func psiSectionSize(data []byte) (int, bool) {
	if len(data) < 3 {
		return 0, false
	}
	return 3 + (int(data[1]&0x0F)<<8 | int(data[2])), true
}

// push adds one packet of the PID found at offset pos. It returns the sections
// the packet completes and a description of each problem that made it drop
// data; a problem never stops the reader.
func (r *psiSectionReader) push(tsPacket *TsPacket, pos int64) ([]psiSectionData, []string) {
	if tsPacket.transportErrorIndicator != 0 {
		// The packet has an uncorrectable error: neither its header nor its
		// payload can be trusted, so nothing it carries is used, and the
		// section in progress may be missing data.
		r.collecting, r.buf = false, nil
		return nil, []string{"transport_error_indicator set; packet ignored and any partial section discarded"}
	}
	payload := tsPacket.Payload()
	if len(payload) == 0 {
		return nil, nil
	}
	cc := tsPacket.ContinuityCounter()
	expected := (r.lastCC + 1) & 0xF
	duplicate := cc == r.lastCC
	r.lastCC = cc
	var problems []string
	if r.collecting && duplicate {
		// A repeated packet (same continuity_counter) carries the same bytes.
		return nil, nil
	}
	if r.collecting && cc != expected {
		problems = append(problems, fmt.Sprintf("continuity_counter jumped from %d to %d inside a section; partial section discarded", (expected-1)&0xF, cc))
		r.collecting, r.buf = false, nil
	}

	if !tsPacket.PayloadUnitStartIndicator() {
		if !r.collecting {
			return nil, problems
		}
		r.buf = append(r.buf, payload...)
		if size, ok := psiSectionSize(r.buf); ok && len(r.buf) >= size {
			section := psiSectionData{data: r.buf[:size], pos: r.startPos}
			r.collecting, r.buf = false, nil
			return []psiSectionData{section}, problems
		}
		return nil, problems
	}

	// payload[0] is the pointer_field: the bytes before the first new section
	// end the section in progress.
	pointer := int(payload[0])
	if 1+pointer > len(payload) {
		r.collecting, r.buf = false, nil
		return nil, append(problems, fmt.Sprintf("invalid pointer_field %d; packet ignored", pointer))
	}
	var sections []psiSectionData
	if r.collecting {
		r.buf = append(r.buf, payload[1:1+pointer]...)
		if size, ok := psiSectionSize(r.buf); ok && len(r.buf) >= size {
			sections = append(sections, psiSectionData{data: r.buf[:size], pos: r.startPos})
		} else {
			problems = append(problems, "section ended early by the next payload_unit_start_indicator; partial section discarded")
		}
		r.collecting, r.buf = false, nil
	}
	// Further sections may follow each other; 0xFF starts the stuffing.
	rest := payload[1+pointer:]
	for len(rest) > 0 && rest[0] != 0xFF {
		size, ok := psiSectionSize(rest)
		if !ok || len(rest) < size {
			r.buf, r.startPos, r.collecting = bytes.Clone(rest), pos, true
			break
		}
		sections = append(sections, psiSectionData{data: bytes.Clone(rest[:size]), pos: pos})
		rest = rest[size:]
	}
	return sections, problems
}

// psiDiagnostic is a problem found while assembling a table. The table is not
// built from the affected section, so the analysis never runs on a silently
// partial table.
type psiDiagnostic struct {
	pos     int64
	message string
}

// psiTableKey identifies one version of a table. Sections of different keys are
// never combined.
type psiTableKey struct {
	tableID   uint8
	extension uint16 // transport_stream_id for a PAT, program_number for a PMT
	version   uint8
}

// psiSectionSet collects the sections of one table version.
type psiSectionSet[T any] struct {
	last     uint8
	raw      [][]byte // indexed by section_number; nil until received
	parsed   []T
	received int
}

// psiAssembler builds complete tables of one table_id from sections fed in any
// order. Only current sections (current_next_indicator 1) whose CRC32 and
// syntax are valid are collected; a table is complete when every section from
// 0 to last_section_number of one version has arrived. Exact retransmissions
// are ignored. Each problem is recorded as a diagnostic.
type psiAssembler[T any] struct {
	name         string
	tableID      uint8
	extension    uint16
	anyExtension bool
	parse        func([]byte) (T, error)
	sets         map[psiTableKey]*psiSectionSet[T]
	latest       psiTableKey // the set that received the last section
	hasLatest    bool
	diagnostics  []psiDiagnostic
}

func (a *psiAssembler[T]) diagnose(pos int64, format string, args ...any) {
	a.diagnostics = append(a.diagnostics, psiDiagnostic{pos, a.name + " " + fmt.Sprintf(format, args...)})
}

// add feeds one section found at offset pos. It returns the parsed sections in
// section_number order when the section completes a table.
func (a *psiAssembler[T]) add(data []byte, pos int64) ([]T, bool) {
	// Other tables may share the PID (for a PMT PID, also other programs'
	// PMTs); they are not part of this table.
	if data[0] != a.tableID {
		return nil, false
	}
	if len(data) < 12 || data[1]&0x80 == 0 {
		a.diagnose(pos, "section is malformed (too short or section_syntax_indicator 0); section ignored")
		return nil, false
	}
	key := psiTableKey{tableID: data[0], extension: uint16(data[3])<<8 | uint16(data[4]), version: data[5] >> 1 & 0x1F}
	if !a.anyExtension && key.extension != a.extension {
		return nil, false
	}
	number, last := data[6], data[7]
	if crc32(data) != 0 {
		a.diagnose(pos, "section %d (version %d) has an invalid CRC32; section ignored", number, key.version)
		return nil, false
	}
	if data[5]&0x01 == 0 {
		// A "next" table (current_next_indicator 0) is not applicable yet.
		return nil, false
	}
	if number > last {
		a.diagnose(pos, "section %d (version %d) is beyond last_section_number %d; section ignored", number, key.version, last)
		return nil, false
	}
	parsed, err := a.parse(data)
	if err != nil {
		a.diagnose(pos, "section %d (version %d) cannot be parsed: %s; section ignored", number, key.version, err)
		return nil, false
	}
	// A transport stream has one PAT identity: the first valid section fixes
	// it, so sections of other transport_stream_ids cannot pile up.
	if a.anyExtension {
		a.extension, a.anyExtension = key.extension, false
	}

	set := a.sets[key]
	if set != nil && set.last != last {
		a.diagnose(pos, "version %d changed last_section_number from %d to %d; discarding %d collected section(s)", key.version, set.last, last, set.received)
		set = nil
	} else if set != nil && set.raw[number] != nil {
		if bytes.Equal(set.raw[number], data) {
			return nil, false
		}
		a.diagnose(pos, "section %d changed without a version change (version %d); discarding %d collected section(s)", number, key.version, set.received)
		set = nil
	}
	if set == nil {
		set = &psiSectionSet[T]{last: last, raw: make([][]byte, int(last)+1), parsed: make([]T, int(last)+1)}
		if a.sets == nil {
			a.sets = make(map[psiTableKey]*psiSectionSet[T])
		}
		a.sets[key] = set
	}
	set.raw[number], set.parsed[number] = data, parsed
	set.received++
	a.latest, a.hasLatest = key, true
	if set.received < len(set.raw) {
		return nil, false
	}
	// Collecting starts over, so a later repetition completes the table again.
	delete(a.sets, key)
	return set.parsed, true
}

// incomplete describes why no table was completed.
func (a *psiAssembler[T]) incomplete() error {
	if !a.hasLatest {
		return fmt.Errorf("%s: no valid section found", a.name)
	}
	set := a.sets[a.latest]
	return fmt.Errorf("incomplete %s (version %d): missing section(s) %s of 0..%d", a.name, a.latest.version, set.missing(), set.last)
}

// missing lists the section numbers that have not arrived.
func (s *psiSectionSet[T]) missing() string {
	var numbers []string
	for number, raw := range s.raw {
		if raw == nil {
			numbers = append(numbers, strconv.Itoa(number))
		}
	}
	return strings.Join(numbers, ", ")
}

// pending describes every incomplete set of a version other than active, in
// version order. These are candidate tables that were never applied.
func (a *psiAssembler[T]) pending(active uint8) []string {
	var keys []psiTableKey
	for key := range a.sets {
		if key.version != active {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(x, y psiTableKey) int { return cmp.Compare(x.version, y.version) })
	var messages []string
	for _, key := range keys {
		set := a.sets[key]
		messages = append(messages, fmt.Sprintf("incomplete %s (version %d) at the end of the analysis: missing section(s) %s of 0..%d; not applied", a.name, key.version, set.missing(), set.last))
	}
	return messages
}
