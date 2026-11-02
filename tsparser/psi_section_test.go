package tsparser

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// testSection builds a long-form section with any header values.
func testSection(tableID byte, extension uint16, version, number, last byte, current bool, body []byte) []byte {
	length := 5 + len(body) + 4
	flags := 0xC0 | version<<1
	if current {
		flags |= 0x01
	}
	section := []byte{tableID, 0xB0 | byte(length>>8), byte(length), byte(extension >> 8), byte(extension), flags, number, last}
	section = append(section, body...)
	c := crc32(section)
	return append(section, byte(c>>24), byte(c>>16), byte(c>>8), byte(c))
}

// patSectionN is section number of last of a PAT version listing
// (program_number, PMT PID) pairs.
func patSectionN(version, number, last byte, programs ...[2]uint16) []byte {
	var body []byte
	for _, p := range programs {
		body = append(body, byte(p[0]>>8), byte(p[0]), 0xE0|byte(p[1]>>8), byte(p[1]))
	}
	return testSection(0x00, 1, version, number, last, true, body)
}

// pmtSectionN is section number of last of a PMT version listing
// (stream_type, PID) pairs.
func pmtSectionN(programNumber uint16, version, number, last byte, pcrPid uint16, streams ...[2]uint16) []byte {
	body := []byte{0xE0 | byte(pcrPid>>8), byte(pcrPid), 0xF0, 0x00}
	for _, s := range streams {
		body = append(body, byte(s[0]), 0xE0|byte(s[1]>>8), byte(s[1]), 0xF0, 0x00)
	}
	return testSection(0x02, programNumber, version, number, last, true, body)
}

// rawPsiPacket is a payload-only packet whose payload (including the
// pointer_field of a PUSI packet) is given verbatim and padded with 0xFF.
func rawPsiPacket(pid uint16, pusi bool, cc uint8, payload []byte) []byte {
	pkt := bytes.Repeat([]byte{0xFF}, 188)
	pkt[0], pkt[1], pkt[2], pkt[3] = 0x47, byte(pid>>8)&0x1F, byte(pid), 0x10|cc&0x0F
	if pusi {
		pkt[1] |= 0x40
	}
	copy(pkt[4:], payload)
	return pkt
}

func parsedTsPacket(t *testing.T, raw []byte) *TsPacket {
	t.Helper()
	p := NewTsPacket()
	p.Initialize(0, options.Options{})
	p.Append(raw)
	if err := p.Parse(); err != nil {
		t.Fatal(err)
	}
	return p
}

// longSection is a private section (table_id 0x80) of exactly size bytes.
func longSection(size int) []byte {
	return testSection(0x80, 1, 0, 0, 0, true, bytes.Repeat([]byte{0x00}, size-12))
}

func TestPsiSectionReaderSplitsAndJoinsSections(t *testing.T) {
	short := patSectionN(0, 0, 0, [2]uint16{1, 0x100})
	long := longSection(300)
	var r psiSectionReader
	push := func(raw []byte, pos int64) ([]psiSectionData, []string) { return r.push(parsedTsPacket(t, raw), pos) }

	if s, p := push(adaptationOnlyPacket(0, 0), 0); s != nil || p != nil {
		t.Fatalf("adaptation-only packet: %v %v", s, p)
	}
	if s, p := push(rawPsiPacket(0, false, 1, long[:10]), 0); s != nil || p != nil {
		t.Fatalf("continuation before any start: %v %v", s, p)
	}
	first := append([]byte{0x00}, short...)
	first = append(first, long[:184-len(first)]...)
	sections, problems := push(rawPsiPacket(0, true, 2, first), 188)
	if len(sections) != 1 || !bytes.Equal(sections[0].data, short) || sections[0].pos != 188 || problems != nil {
		t.Fatalf("first packet: %v %v", sections, problems)
	}
	used := 184 - 1 - len(short)
	// A repeated packet is skipped, then the continuation completes the section.
	if s, p := push(rawPsiPacket(0, true, 2, first), 376); s != nil || p != nil {
		t.Fatalf("duplicate packet: %v %v", s, p)
	}
	sections, problems = push(rawPsiPacket(0, false, 3, long[used:]), 564)
	if len(sections) != 1 || !bytes.Equal(sections[0].data, long) || sections[0].pos != 188 || problems != nil {
		t.Fatalf("continuation: %v %v", sections, problems)
	}
}

func TestPsiSectionReaderEndsSectionsAtUnitStart(t *testing.T) {
	long := longSection(400)
	next := patSectionN(0, 1, 1, [2]uint16{2, 0x200})
	var r psiSectionReader
	push := func(raw []byte) ([]psiSectionData, []string) { return r.push(parsedTsPacket(t, raw), 0) }

	push(rawPsiPacket(0, true, 0, append([]byte{0x00}, long[:183]...)))
	if s, p := push(rawPsiPacket(0, false, 1, long[183:367])); s != nil || p != nil {
		t.Fatalf("incomplete continuation: %v %v", s, p)
	}
	// The rest of the long section sits before the pointer_field's target.
	tail := long[367:]
	payload := append([]byte{byte(len(tail))}, tail...)
	payload = append(payload, next...)
	sections, problems := push(rawPsiPacket(0, true, 2, payload))
	if len(sections) != 2 || !bytes.Equal(sections[0].data, long) || !bytes.Equal(sections[1].data, next) || problems != nil {
		t.Fatalf("unit start ending a section: %v %v", sections, problems)
	}

	// A unit start before the section is complete discards it.
	push(rawPsiPacket(0, true, 3, append([]byte{0x00}, long[:183]...)))
	sections, problems = push(rawPsiPacket(0, true, 4, append([]byte{0x01, 0xAA}, next...)))
	if len(sections) != 1 || !bytes.Equal(sections[0].data, next) || len(problems) != 1 || !strings.Contains(problems[0], "ended early") {
		t.Fatalf("early unit start: %v %v", sections, problems)
	}
}

func TestPsiSectionReaderDiscardsOnContinuityGap(t *testing.T) {
	long := longSection(400)
	next := patSectionN(0, 0, 0, [2]uint16{1, 0x100})
	var r psiSectionReader
	push := func(raw []byte) ([]psiSectionData, []string) { return r.push(parsedTsPacket(t, raw), 0) }

	push(rawPsiPacket(0, true, 0, append([]byte{0x00}, long[:183]...)))
	sections, problems := push(rawPsiPacket(0, false, 5, long[183:367]))
	if sections != nil || len(problems) != 1 || !strings.Contains(problems[0], "continuity_counter jumped from 0 to 5") {
		t.Fatalf("gap: %v %v", sections, problems)
	}
	if s, p := push(rawPsiPacket(0, false, 6, long[367:])); s != nil || p != nil {
		t.Fatalf("after a discarded section: %v %v", s, p)
	}

	// A gap before a unit start drops the partial section but not the new one.
	push(rawPsiPacket(0, true, 7, append([]byte{0x00}, long[:183]...)))
	sections, problems = push(rawPsiPacket(0, true, 9, append([]byte{0x00}, next...)))
	if len(sections) != 1 || len(problems) != 1 {
		t.Fatalf("gap before a unit start: %v %v", sections, problems)
	}

	sections, problems = push(rawPsiPacket(0, true, 10, []byte{200}))
	if sections != nil || len(problems) != 1 || !strings.Contains(problems[0], "invalid pointer_field 200") {
		t.Fatalf("invalid pointer_field: %v %v", sections, problems)
	}
}

func TestPsiSectionReaderSplitHeader(t *testing.T) {
	// A section of 181 bytes leaves two bytes, too few for the next section's
	// section_length, at the end of the packet.
	first := longSection(181)
	next := patSectionN(0, 0, 0, [2]uint16{1, 0x100})
	payload := append([]byte{0x00}, first...)
	payload = append(payload, next[:2]...)
	var r psiSectionReader
	sections, _ := r.push(parsedTsPacket(t, rawPsiPacket(0, true, 0, payload)), 0)
	if len(sections) != 1 || len(first) != 181 {
		t.Fatalf("first packet: %v", sections)
	}
	sections, _ = r.push(parsedTsPacket(t, rawPsiPacket(0, false, 1, next[2:])), 188)
	if len(sections) != 1 || !bytes.Equal(sections[0].data, next) || sections[0].pos != 0 {
		t.Fatalf("split header: %v", sections)
	}
}

func newPatAssembler() *psiAssembler[*Pat] {
	return &psiAssembler[*Pat]{name: "PAT", tableID: 0x00, anyExtension: true, parse: parsePatSection}
}

func diagnosticMessages(diagnostics []psiDiagnostic) string {
	var messages []string
	for _, d := range diagnostics {
		messages = append(messages, d.message)
	}
	return strings.Join(messages, "\n")
}

func TestPsiAssemblerCompletesTables(t *testing.T) {
	a := newPatAssembler()
	s0 := patSectionN(0, 0, 1, [2]uint16{1, 0x100})
	s1 := patSectionN(0, 1, 1, [2]uint16{2, 0x200})
	if _, ok := a.add(s1, 10); ok {
		t.Fatal("one section completed a two-section table")
	}
	if _, ok := a.add(s1, 20); ok {
		t.Fatal("a retransmission completed the table")
	}
	sections, ok := a.add(s0, 30)
	if !ok || len(sections) != 2 || sections[0].sectionNumber != 0 || sections[1].sectionNumber != 1 || len(a.diagnostics) != 0 {
		t.Fatalf("sections = %v, ok = %v, diagnostics = %v", sections, ok, a.diagnostics)
	}
	// Collection starts over, so the next repetition completes it again.
	a.add(s0, 40)
	if _, ok := a.add(s1, 50); !ok {
		t.Error("the repeated table did not complete again")
	}
}

func TestPsiAssemblerIgnoresOtherTables(t *testing.T) {
	a := newPatAssembler()
	ignored := [][]byte{
		pmtSectionN(1, 0, 0, 0, 0x100),                                       // another table_id
		testSection(0x00, 1, 0, 0, 0, false, []byte{0x00, 0x01, 0xE0, 0x10}), // next, not current
	}
	for _, section := range ignored {
		if _, ok := a.add(section, 0); ok {
			t.Fatalf("%x completed a table", section)
		}
	}
	pmts := &psiAssembler[*Pmt]{name: "PMT", tableID: 0x02, extension: 1, parse: parsePmtSection}
	if _, ok := pmts.add(pmtSectionN(2, 0, 0, 0, 0x100), 0); ok {
		t.Fatal("another program's PMT completed the table")
	}
	if len(a.diagnostics) != 0 || len(pmts.diagnostics) != 0 {
		t.Fatalf("diagnostics: %v %v", a.diagnostics, pmts.diagnostics)
	}
	if err := a.incomplete(); err == nil || err.Error() != "PAT: no valid section found" {
		t.Errorf("incomplete() = %v", err)
	}
}

func TestPsiAssemblerDiagnostics(t *testing.T) {
	badCRC := patSectionN(0, 0, 0, [2]uint16{1, 0x100})
	badCRC[len(badCRC)-1] ^= 0xFF
	noSyntax := patSectionN(0, 0, 0, [2]uint16{1, 0x100})
	noSyntax[1] &^= 0x80
	// program_info_length runs past the end of the section.
	unparsable := testSection(0x02, 1, 0, 0, 0, true, []byte{0xE1, 0x00, 0xF0, 0x20})

	a := newPatAssembler()
	a.add([]byte{0x00, 0xB0, 0x01, 0x00}, 1)
	a.add(noSyntax, 2)
	a.add(badCRC, 3)
	a.add(patSectionN(0, 2, 1, [2]uint16{1, 0x100}), 4)
	pmts := &psiAssembler[*Pmt]{name: "PMT", tableID: 0x02, extension: 1, parse: parsePmtSection}
	pmts.add(unparsable, 5)

	// Inconsistent sets of one version are discarded.
	a.add(patSectionN(0, 0, 1, [2]uint16{1, 0x100}), 6)
	a.add(patSectionN(0, 1, 2, [2]uint16{2, 0x200}), 7)
	a.add(patSectionN(0, 1, 2, [2]uint16{3, 0x300}), 8)
	want := []psiDiagnostic{
		{1, "PAT section is malformed (too short or section_syntax_indicator 0); section ignored"},
		{2, "PAT section is malformed (too short or section_syntax_indicator 0); section ignored"},
		{3, "PAT section 0 (version 0) has an invalid CRC32; section ignored"},
		{4, "PAT section 2 (version 0) is beyond last_section_number 1; section ignored"},
		{7, "PAT version 0 changed last_section_number from 1 to 2; discarding 1 collected section(s)"},
		{8, "PAT section 1 changed without a version change (version 0); discarding 1 collected section(s)"},
	}
	if !reflect.DeepEqual(a.diagnostics, want) {
		t.Errorf("diagnostics:\n%s", diagnosticMessages(a.diagnostics))
	}
	if len(pmts.diagnostics) != 1 || !strings.HasPrefix(pmts.diagnostics[0].message, "PMT section 0 (version 0) cannot be parsed: ") {
		t.Errorf("PMT diagnostics: %v", pmts.diagnostics)
	}
	if err := a.incomplete(); err == nil || err.Error() != "incomplete PAT (version 0): missing section(s) 0, 2 of 0..2" {
		t.Errorf("incomplete() = %v", err)
	}
}

func TestPsiAssemblerKeepsVersionsAndIdentitiesApart(t *testing.T) {
	a := newPatAssembler()
	a.add(patSectionN(0, 0, 1, [2]uint16{1, 0x100}), 0)
	a.add(patSectionN(1, 1, 1, [2]uint16{2, 0x200}), 1)
	otherStream := testSection(0x00, 2, 0, 1, 1, true, []byte{0x00, 0x02, 0xE2, 0x00})
	if _, ok := a.add(otherStream, 2); ok {
		t.Fatal("sections of different transport_stream_id were combined")
	}
	if err := a.incomplete(); err == nil || err.Error() != "incomplete PAT (version 1): missing section(s) 0 of 0..1" {
		t.Errorf("incomplete() = %v", err)
	}
	if _, ok := a.add(patSectionN(1, 0, 1, [2]uint16{1, 0x100}), 3); !ok {
		t.Error("version 1 did not complete")
	}
}

// Only the first valid PAT identity is collected, so incomplete sections of
// many transport_stream_ids cannot accumulate.
func TestPsiAssemblerLocksThePatIdentity(t *testing.T) {
	a := newPatAssembler()
	for extension := uint16(1); extension <= 1000; extension++ {
		a.add(testSection(0x00, extension, 0, 0, 255, true, []byte{0x00, 0x01, 0xE1, 0x00}), int64(extension))
	}
	if len(a.sets) != 1 || a.extension != 1 {
		t.Errorf("collected %d identities; locked to %d", len(a.sets), a.extension)
	}
}

// A packet with transport_error_indicator set contributes nothing, and a
// section it interrupts is discarded.
func TestPsiSectionReaderDropsTransportErrors(t *testing.T) {
	section := patSectionN(0, 0, 0, [2]uint16{1, 0x100})
	long := longSection(300)
	var r psiSectionReader
	push := func(raw []byte) ([]psiSectionData, []string) { return r.push(parsedTsPacket(t, raw), 0) }
	withError := func(raw []byte) []byte {
		raw[1] |= 0x80
		return raw
	}

	sections, problems := push(withError(rawPsiPacket(0, true, 0, append([]byte{0x00}, section...))))
	if sections != nil || len(problems) != 1 || !strings.Contains(problems[0], "transport_error_indicator") {
		t.Fatalf("standalone section: %v %v", sections, problems)
	}
	push(rawPsiPacket(0, true, 1, append([]byte{0x00}, long[:183]...)))
	sections, problems = push(withError(rawPsiPacket(0, false, 2, long[183:])))
	if sections != nil || len(problems) != 1 {
		t.Fatalf("mid-section error: %v %v", sections, problems)
	}
	// The next continuation has nothing to join.
	if sections, _ = push(rawPsiPacket(0, false, 3, long[183:])); sections != nil {
		t.Fatalf("section completed after a transport error: %v", sections)
	}
}

// An adaptation-only packet with transport_error_indicator set between two
// packets of a section also discards it and is reported, during acquisition.
func TestAcquireTableDropsSectionsAcrossAnErroneousAdaptationOnlyPacket(t *testing.T) {
	var programs [][2]uint16
	for number := uint16(1); number <= 60; number++ {
		programs = append(programs, [2]uint16{number, 0x100 + number})
	}
	long := patSectionN(0, 0, 0, programs...)
	erroneous := adaptationOnlyPacket(0, 1)
	erroneous[1] |= 0x80
	stream := psiStream(
		rawPsiPacket(0, true, 0, append([]byte{0x00}, long[:183]...)),
		erroneous,
		rawPsiPacket(0, false, 1, long[183:]),
	)
	pos := int64(0)
	assembler := newPatAssembler()
	_, _, err := acquireTable(bytes.NewReader(stream), &pos, 0, assembler, options.Options{}, 188, 0)
	if err == nil {
		t.Fatal("a section was completed across an erroneous packet")
	}
	if !strings.Contains(diagnosticMessages(assembler.diagnostics), "transport_error_indicator set") {
		t.Errorf("diagnostics = %s", diagnosticMessages(assembler.diagnostics))
	}
}

func TestNewPatTableMergesPrograms(t *testing.T) {
	var sections []*Pat
	for _, raw := range [][]byte{
		patSectionN(0, 0, 1, [2]uint16{0, 0x10}, [2]uint16{3, 0x300}, [2]uint16{2, 0x200}),
		patSectionN(0, 1, 1, [2]uint16{2, 0x200}, [2]uint16{1, 0x100}, [2]uint16{3, 0x301}),
	} {
		pat, err := parsePatSection(raw)
		if err != nil {
			t.Fatal(err)
		}
		sections = append(sections, pat)
	}
	table, conflicts := newPatTable(sections)
	want := []Program{{1, 0x100}, {2, 0x200}, {3, 0x300}}
	if !reflect.DeepEqual(table.Programs(), want) {
		t.Errorf("programs = %v", table.Programs())
	}
	if len(conflicts) != 1 || conflicts[0] != "PAT lists program 3 with PMT PIDs 0x300 and 0x301; using 0x300" {
		t.Errorf("conflicts = %v", conflicts)
	}
	out := captureStdout(t, table.Dump)
	if strings.Count(out, " PAT\n") != 2 || !strings.Contains(out, "last_section_number") {
		t.Errorf("dump:\n%s", out)
	}
}

func TestNewPmtTableMergesStreams(t *testing.T) {
	withDescriptor := func(raw []byte) []byte {
		// Insert a 2-byte descriptor into program_info and fix length and CRC.
		body := append([]byte{}, raw[8:len(raw)-4]...)
		body[2], body[3] = 0xF0, 0x02
		body = append(body[:4], append([]byte{0x05, 0x00}, body[4:]...)...)
		return testSection(raw[0], uint16(raw[3])<<8|uint16(raw[4]), raw[5]>>1&0x1F, raw[6], raw[7], true, body)
	}
	var sections []*Pmt
	for _, raw := range [][]byte{
		withDescriptor(pmtSectionN(1, 0, 0, 1, 0x100, [2]uint16{0x02, 0x100})),
		withDescriptor(pmtSectionN(1, 0, 1, 1, 0x101, [2]uint16{0x02, 0x100}, [2]uint16{0x1B, 0x101}, [2]uint16{0x03, 0x100})),
	} {
		pmt, err := parsePmtSection(raw)
		if err != nil {
			t.Fatal(err)
		}
		sections = append(sections, pmt)
	}
	table, conflicts := newPmtTable(sections)
	if table.PcrPid() != 0x100 || len(table.ProgramInfos()) != 2 || table.ProgramInfos()[1].elementaryPid != 0x101 || len(table.descriptors) != 2 {
		t.Errorf("merged PMT = %+v", table.Pmt)
	}
	want := []string{
		"PMT of program 1 has PCR_PID 0x100 in section 0 and 0x101 in section 1; using 0x100",
		"PMT of program 1 lists elementary PID 0x100 with stream types 0x02 and 0x03; using 0x02",
	}
	if !reflect.DeepEqual(conflicts, want) {
		t.Errorf("conflicts = %v", conflicts)
	}
	if out := captureStdout(t, table.Dump); strings.Count(out, " PMT\n") != 2 {
		t.Errorf("dump:\n%s", out)
	}
}

func psiStream(packets ...[]byte) []byte { return bytes.Join(packets, nil) }

func TestAcquireTableStopsAtTheNextUnitStart(t *testing.T) {
	s0 := patSectionN(0, 0, 1, [2]uint16{1, 0x100})
	s1 := patSectionN(0, 1, 1, [2]uint16{2, 0x200})
	long := longSection(250)
	data := psiStream(
		buildTsPacket(0x0000, true, 0, s1),
		buildStuffingPacket(),
		buildTsPacket(0x0000, true, 1, s0),     // completes the table
		rawPsiPacket(0x0000, false, 2, s0[:4]), // not a unit start
		buildTsPacket(0x0000, true, 3, s1),     // ends the acquisition
		buildStuffingPacket(),
	)
	pos := int64(0)
	table, diagnostics, err := acquirePat(bytes.NewReader(data), &pos, options.Options{}, 188, 0)
	if err != nil || len(diagnostics) != 0 || len(table.Programs()) != 2 || pos != 5*188 {
		t.Fatalf("table %v, diagnostics %v, err %v, pos %d", table, diagnostics, err, pos)
	}

	// A section completed by the bytes before a pointer_field ends the
	// acquisition in that packet: the next unit has already started.
	var programs [][2]uint16
	for number := uint16(100); number < 160; number++ {
		programs = append(programs, [2]uint16{number, 0x1000 + number})
	}
	big := patSectionN(0, 0, 1, programs...)
	tail := big[183:]
	data = psiStream(
		buildTsPacket(0x0000, true, 0, s1),
		rawPsiPacket(0x0000, true, 1, append([]byte{0x00}, big[:183]...)),
		rawPsiPacket(0x0000, true, 2, append(append([]byte{byte(len(tail))}, tail...), long[:10]...)),
		buildStuffingPacket(),
	)
	pos = 0
	if table, _, err := acquirePat(bytes.NewReader(data), &pos, options.Options{}, 188, 0); err != nil || pos != 3*188 || len(table.Programs()) != 61 {
		t.Fatalf("err %v, pos %d", err, pos)
	}

	// The end of the input completes nothing more but keeps the table.
	pos = 0
	data = psiStream(buildTsPacket(0x0000, true, 0, s1), buildTsPacket(0x0000, true, 1, s0))
	if table, _, err := acquirePat(bytes.NewReader(data), &pos, options.Options{}, 188, 0); err != nil || len(table.Programs()) != 2 || pos != 2*188 {
		t.Fatalf("table %v, err %v, pos %d", table, err, pos)
	}
}

func TestAcquireTableErrors(t *testing.T) {
	s0 := patSectionN(0, 0, 1, [2]uint16{1, 0x100})
	data := psiStream(buildTsPacket(0x0000, true, 0, s0), buildTsPacket(0x0000, true, 1, s0), buildStuffingPacket())

	pos := int64(0)
	_, _, err := acquirePat(&errReader{data: data, failAt: 1}, &pos, options.Options{}, 188, 0)
	if err == nil || err.Error() != "PAT buffering error: file read error: mock read error" {
		t.Errorf("read error: %v", err)
	}
	pos = 0
	broken := append(bytes.Clone(data[:188]), make([]byte, 188)...)
	if _, _, err := acquirePat(bytes.NewReader(broken), &pos, options.Options{}, 188, 0); err == nil || !strings.HasPrefix(err.Error(), "PAT buffering error: failed to parse TS packet") {
		t.Errorf("broken packet: %v", err)
	}
	// The range ends before the second section.
	pos = 0
	if _, _, err := acquirePat(bytes.NewReader(data), &pos, options.Options{}, 188, 188); err == nil || err.Error() != "incomplete PAT (version 0): missing section(s) 1 of 0..1" || pos != 188 {
		t.Errorf("limited range: %v, pos %d", err, pos)
	}

	pos = 0
	_, diagnostics, err := acquirePmt(bytes.NewReader(psiStream(rawPsiPacket(0x1000, true, 0, []byte{200}))), &pos, 0x1000, 1, options.Options{}, 188, 0)
	if err == nil || err.Error() != "PMT of program 1 on PID 0x1000: no valid section found" || len(diagnostics) != 1 ||
		diagnostics[0].message != "PMT of program 1 on PID 0x1000: invalid pointer_field 200; packet ignored" {
		t.Errorf("PMT: %v %v", err, diagnostics)
	}
}

func TestAcquireTableM2TS(t *testing.T) {
	data := psiStream(
		wrapM2TS(buildTsPacket(0x1000, true, 0, pmtSectionN(1, 0, 1, 1, 0x100, [2]uint16{0x03, 0x101}))),
		wrapM2TS(buildTsPacket(0x1000, true, 1, pmtSectionN(2, 0, 0, 0, 0x200, [2]uint16{0x02, 0x200}))),
		wrapM2TS(buildTsPacket(0x1000, true, 2, pmtSectionN(1, 0, 0, 1, 0x100, [2]uint16{0x02, 0x100}))),
		wrapM2TS(buildTsPacket(0x1000, true, 3, pmtSectionN(1, 0, 1, 1, 0x100, [2]uint16{0x03, 0x101}))),
	)
	pos := int64(0)
	table, _, err := acquirePmt(bytes.NewReader(data), &pos, 0x1000, 1, options.Options{}, 192, 0)
	if err != nil || pos != 4*192 || table.PcrPid() != 0x100 || len(table.ProgramInfos()) != 2 || table.ProgramInfos()[0].elementaryPid != 0x100 {
		t.Fatalf("table %+v, err %v, pos %d", table, err, pos)
	}
}

func TestAcquireTableReportsConflicts(t *testing.T) {
	pat := psiStream(
		buildTsPacket(0x0000, true, 0, patSectionN(0, 0, 1, [2]uint16{1, 0x100})),
		buildTsPacket(0x0000, true, 1, patSectionN(0, 1, 1, [2]uint16{1, 0x101})),
	)
	pos := int64(0)
	_, diagnostics, err := acquirePat(bytes.NewReader(pat), &pos, options.Options{}, 188, 0)
	if err != nil || len(diagnostics) != 1 || diagnostics[0].pos != pos {
		t.Errorf("PAT: %v %v", diagnostics, err)
	}
	pmt := psiStream(
		buildTsPacket(0x1000, true, 0, pmtSectionN(1, 0, 0, 1, 0x100, [2]uint16{0x02, 0x100})),
		buildTsPacket(0x1000, true, 1, pmtSectionN(1, 0, 1, 1, 0x101, [2]uint16{0x03, 0x101})),
	)
	pos = 0
	if _, diagnostics, err = acquirePmt(bytes.NewReader(pmt), &pos, 0x1000, 1, options.Options{}, 188, 0); err != nil || len(diagnostics) != 1 {
		t.Errorf("PMT: %v %v", diagnostics, err)
	}
}
