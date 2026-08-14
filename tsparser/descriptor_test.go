package tsparser

import "testing"

// buildPmtWithDescriptor builds a valid PMT byte slice carrying a single
// elementary stream whose ES info loop contains descBytes.
func buildPmtWithDescriptor(streamType byte, descBytes []byte) []byte {
	esInfoLen := len(descBytes)
	// Section body after section_length field (program_number .. last program info)
	body := []byte{
		0x00, 0x01, // program_number
		0xC1,       // reserved, version=0, current_next=1
		0x00,       // section_number
		0x00,       // last_section_number
		0xE0, 0x31, // reserved, PCR_PID=0x31
		0xF0, 0x00, // reserved, program_info_length=0
		streamType,
		0xE0, 0x31, // reserved, elementary_PID=0x31
		0xF0 | byte((esInfoLen>>8)&0x0F), byte(esInfoLen & 0xFF), // reserved + ES_info_length (12 bits)
	}
	body = append(body, descBytes...)
	// section_length = len(body) + 4 (CRC)
	sectionLength := len(body) + 4
	header := []byte{0x02, 0xB0 | byte(sectionLength>>8), byte(sectionLength)}
	full := append(header, body...)
	crc := crc32(full)
	return append(full, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
}

func parsePmtWithDescriptor(t *testing.T, streamType byte, descBytes []byte) *Pmt {
	t.Helper()
	pmt := NewPmt()
	pmt.Append(buildPmtWithDescriptor(streamType, descBytes))
	if err := pmt.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if len(pmt.programInfos) != 1 {
		t.Fatalf("expected 1 program info, got %d", len(pmt.programInfos))
	}
	return pmt
}

func TestParseDescriptorsISO639Language(t *testing.T) {
	// tag=0x0A, length=4, "eng" + audio_type 0x00
	desc := []byte{0x0A, 0x04, 'e', 'n', 'g', 0x00}
	pmt := parsePmtWithDescriptor(t, 0x0F, desc)
	ds := pmt.programInfos[0].Descriptors()
	if len(ds) != 1 {
		t.Fatalf("expected 1 descriptor, got %d", len(ds))
	}
	if ds[0].Tag() != 0x0A {
		t.Errorf("expected tag 0x0A, got 0x%02X", ds[0].Tag())
	}
	if string(ds[0].Data()) != "eng\x00" {
		t.Errorf("unexpected data: %q", ds[0].Data())
	}
	pmt.DumpProgramInfos(true) // should not panic
}

func TestParseDescriptorsAVCVideo(t *testing.T) {
	// tag=0x28, length=4, profile_idc=100, constraint=0, level_idc=40, flags=0
	desc := []byte{0x28, 0x04, 100, 0x00, 40, 0x00}
	pmt := parsePmtWithDescriptor(t, 0x1B, desc)
	ds := pmt.programInfos[0].Descriptors()
	if len(ds) != 1 || ds[0].Tag() != 0x28 {
		t.Fatalf("expected one AVC descriptor, got %+v", ds)
	}
	pmt.DumpProgramInfos(true)
}

func TestParseMultipleDescriptors(t *testing.T) {
	// registration "AC-3" followed by ISO 639 language "jpn"
	desc := []byte{
		0x05, 0x04, 'A', 'C', '-', '3',
		0x0A, 0x04, 'j', 'p', 'n', 0x00,
	}
	pmt := parsePmtWithDescriptor(t, 0x06, desc)
	ds := pmt.programInfos[0].Descriptors()
	if len(ds) != 2 {
		t.Fatalf("expected 2 descriptors, got %d", len(ds))
	}
	if ds[0].Tag() != 0x05 || ds[1].Tag() != 0x0A {
		t.Errorf("unexpected tags: 0x%02X, 0x%02X", ds[0].Tag(), ds[1].Tag())
	}
}

func TestDumpAllDescriptorTypes(t *testing.T) {
	cases := [][]byte{
		{0x05, 0x04, 'A', 'C', '-', '3'},  // registration
		{0x0A, 0x04, 'e', 'n', 'g', 0x02}, // ISO 639 language
		{0x28, 0x04, 100, 0x00, 40, 0x00}, // AVC video
		{0x38, 0x0D, 0x21, 0x60, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 120, 0x00}, // HEVC video (14 payload bytes)
		{0x7C, 0x03, 0x50, 0x80, 0x01},                            // AAC audio with AAC_type
		{0x56, 0x05, 'e', 'n', 'g', 0x11, 0x00},                   // teletext
		{0x59, 0x08, 'e', 'n', 'g', 0x10, 0x00, 0x01, 0x00, 0x02}, // subtitling
		{0xFF, 0x02, 0xDE, 0xAD},                                  // unknown
	}
	for _, desc := range cases {
		pmt := parsePmtWithDescriptor(t, 0x06, desc)
		// Exercise Dump paths; must not panic.
		pmt.Dump()
	}
}

func TestParseDescriptorsReadErrors(t *testing.T) {
	// One descriptor: tag, length=1, one payload byte. Fail at each read.
	raw := []byte{0x0A, 0x01, 0xFF}
	for failAt := 1; failAt <= 3; failAt++ {
		m := &mockBitReader{real: newDefaultBitReader(), failAt: failAt}
		m.Set(raw)
		if _, err := parseDescriptors(m, uint16(len(raw))); err == nil {
			t.Errorf("expected error for failAt=%d, got nil", failAt)
		}
	}
	// A single leftover byte (cannot form a descriptor) triggers the trailing Skip.
	ok := &mockBitReader{real: newDefaultBitReader(), failAt: 0}
	ok.Set([]byte{0xFF})
	if _, err := parseDescriptors(ok, 1); err != nil {
		t.Errorf("unexpected error skipping trailing byte: %s", err)
	}
	// Make that trailing Skip fail.
	m := &mockBitReader{real: newDefaultBitReader(), failAt: 1}
	m.Set([]byte{0xFF})
	if _, err := parseDescriptors(m, 1); err == nil {
		t.Error("expected trailing skip error, got nil")
	}
}

// TestPmtParseDescriptorError covers the parseDescriptors error path in Pmt.Parse:
// section_length claims an ES descriptor loop but the buffer ends inside it.
func TestPmtParseDescriptorError(t *testing.T) {
	descBytes := []byte{0x0A, 0x04, 'e', 'n', 'g', 0x00}
	full := buildPmtWithDescriptor(0x0F, descBytes)
	// Drop the descriptor bytes and CRC so es_info_length points past the buffer.
	truncated := full[:len(full)-len(descBytes)-4]
	pmt := NewPmt()
	pmt.Append(truncated)
	if err := pmt.Parse(); err == nil {
		t.Error("expected descriptor parse error, got nil")
	}
}

func TestDescriptorDumpTruncated(t *testing.T) {
	// Each of these hits a "truncated" or optional branch in the dumpers.
	descs := []Descriptor{
		{tag: 0x05, data: []byte{0x41}},                     // registration: len < 4
		{tag: 0x05, data: []byte{'A', 'C', '-', '3', 0x01}}, // registration: additional_info
		{tag: 0x28, data: []byte{0x64}},                     // AVC: len < 3
		{tag: 0x38, data: []byte{0x21}},                     // HEVC: len < 12
		{tag: 0x7C, data: []byte{0x50}},                     // AAC: len < 2
		{tag: 0x7C, data: []byte{0x50, 0x00}},               // AAC: AAC_type_flag = 0
	}
	for _, d := range descs {
		d.Dump() // must not panic
	}
}

func TestDescriptorNameTables(t *testing.T) {
	if printableASCII([]byte{0x00, 'A', 0x7F}) != ".A." {
		t.Errorf("printableASCII: got %q", printableASCII([]byte{0x00, 'A', 0x7F}))
	}
	for _, at := range []uint8{0x00, 0x01, 0x02, 0x03, 0x04} {
		audioTypeName(at)
	}
	for _, p := range []uint8{66, 77, 88, 100, 110, 122, 244, 0} {
		avcProfileName(p)
	}
	for _, p := range []uint8{1, 2, 3, 4, 0} {
		hevcProfileName(p)
	}
	for _, tt := range []uint8{0x01, 0x02, 0x03, 0x04, 0x05, 0x00} {
		teletextTypeName(tt)
	}
	for _, st := range []uint8{0x10, 0x11, 0x20, 0x21, 0x00} {
		subtitlingTypeName(st)
	}
}

func TestParseDescriptorsMalformedLength(t *testing.T) {
	// descriptor claims length 10 but only 1 byte remains in the ES info loop.
	desc := []byte{0x0A, 0x0A, 0xEE}
	pmt := parsePmtWithDescriptor(t, 0x0F, desc)
	ds := pmt.programInfos[0].Descriptors()
	if len(ds) != 1 {
		t.Fatalf("expected 1 descriptor, got %d", len(ds))
	}
	// Length was clamped to the remaining single byte.
	if len(ds[0].Data()) != 1 || ds[0].Data()[0] != 0xEE {
		t.Errorf("expected clamped 1-byte payload 0xEE, got % X", ds[0].Data())
	}
}
