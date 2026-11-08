package tsparser

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// layoutKinds are the documented bit_layout kinds.
var layoutKinds = map[string]bool{
	"value": true, "flag": true, "reserved": true, "marker": true, "prefix": true,
	"stuffing": true, "payload": true, "unknown": true,
}

// sublayers are the layers that sit inside the payload layer.
var sublayers = map[string]bool{"pes_header": true, "pat": true, "pmt": true, "psi_section": true}

// checkLayout verifies the bit layouts of the decoded layers of raw and
// returns their parts by id:
//   - every layer has a layout whose parts follow each other without gaps or
//     overlap, and the layouts of the framing layers (M2TS header, TS header,
//     adaptation field, payload, trailer) cover every bit of the packet once;
//   - a PES header or section starts where the payload does and stays in it;
//   - ids are unique and end with the part's first bit, kinds are documented,
//     an expected value comes with its validity and rule, and the validity of
//     a fixed value says whether the value is the expected one;
//   - links name an existing field (of the row they name) whose bytes hold
//     the part, and, when wellFormed, the value bits of every decoded field
//     are covered by the parts linked to it. (The field decoder reads some
//     fields where a malformed length field says the structure has ended.)
func checkLayout(t *testing.T, where string, raw []byte, layers []PacketLayer, wellFormed bool) map[string]LayoutPart {
	t.Helper()
	parts := map[string]LayoutPart{}
	framing, payloadStart := 0, -1
	for _, layer := range layers {
		if len(layer.BitLayout) == 0 {
			t.Fatalf("%s: layer %s has no bit layout", where, layer.Name)
		}
		start := layer.BitLayout[0].Bits[0].BitOffset
		switch {
		case layer.Name == "payload":
			payloadStart = start
			fallthrough
		case !sublayers[layer.Name]:
			if start != framing {
				t.Fatalf("%s: layer %s starts at bit %d, want %d", where, layer.Name, start, framing)
			}
		case start != payloadStart:
			t.Fatalf("%s: layer %s starts at bit %d, payload at %d", where, layer.Name, start, payloadStart)
		}
		next := start
		covered := map[string]map[int]bool{}
		for _, part := range layer.BitLayout {
			if len(part.Bits) != 1 || part.Bits[0].BitOffset != next || part.Bits[0].BitLength < 1 {
				t.Fatalf("%s: %s: part %+v does not follow bit %d", where, layer.Name, part, next)
			}
			run := part.Bits[0]
			next = run.BitOffset + run.BitLength
			if _, dup := parts[part.ID]; dup || !strings.HasSuffix(part.ID, fmt.Sprintf("@%d", run.BitOffset)) || !strings.HasPrefix(part.ID, layer.Name+".") {
				t.Fatalf("%s: id %q", where, part.ID)
			}
			parts[part.ID] = part
			if !layoutKinds[part.Kind] || part.Label == "" || part.Value == "" {
				t.Fatalf("%s: part %+v", where, part)
			}
			if (part.Expected == "") != (part.Valid == nil) || (part.Expected == "") != (part.Rule == "") {
				t.Fatalf("%s: expected/valid/rule of %+v", where, part)
			}
			if part.Valid != nil && part.Kind != "stuffing" && *part.Valid != (part.Value == part.Expected) {
				t.Fatalf("%s: validity of %+v", where, part)
			}
			fields := layer.Fields
			if part.Row != nil {
				fields = layer.Rows[*part.Row]
			}
			if part.Field == "" {
				continue
			}
			field, ok := findField(fields, part.Field)
			if !ok || run.BitOffset < field.ByteOffset*8 || next > (field.ByteOffset+field.ByteLength)*8 {
				t.Fatalf("%s: %s links to %+v", where, part.ID, field)
			}
			key := rowKey(part.Row) + part.Field
			if covered[key] == nil {
				covered[key] = map[int]bool{}
			}
			for bit := run.BitOffset; bit < next; bit++ {
				covered[key][bit] = true
			}
		}
		if next > len(raw)*8 {
			t.Fatalf("%s: layer %s ends at bit %d, past the packet", where, layer.Name, next)
		}
		if !sublayers[layer.Name] {
			framing = next
		}
		check := func(row *int, f PacketField) {
			if !wellFormed {
				return
			}
			bits := f.Bits
			if bits == nil {
				bits = []BitRange{bitsAt(f.ByteOffset, 0, f.ByteLength*8)}
			}
			for _, r := range bits {
				for bit := r.BitOffset; bit < r.BitOffset+r.BitLength; bit++ {
					if !covered[rowKey(row)+f.Name][bit] {
						t.Fatalf("%s: %s.%s bit %d has no part linked to it", where, layer.Name, f.Name, bit)
					}
				}
			}
		}
		for _, f := range layer.Fields {
			check(nil, f)
		}
		for i, row := range layer.Rows {
			for _, f := range row {
				check(&i, f)
			}
		}
	}
	if framing != len(raw)*8 {
		t.Fatalf("%s: the framing layers cover %d of %d bits", where, framing, len(raw)*8)
	}
	return parts
}

func rowKey(row *int) string {
	if row == nil {
		return "."
	}
	return fmt.Sprintf("%d.", *row)
}

// TestPacketLayoutCoversEveryBit checks the layout invariants on every packet
// of every sample (188-, 192- and 204-byte), and that the samples that are
// not made to fail health checks have no expected-value mismatch.
func TestPacketLayoutCoversEveryBit(t *testing.T) {
	paths, err := filepath.Glob("../sample_data/sample_*byte_*.ts")
	if err != nil || len(paths) < 30 {
		t.Fatalf("samples: %v %v", paths, err)
	}
	kinds := map[string]bool{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, err := openPacketFile(bytes.NewReader(data), int64(len(data)), 0, 0)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		size := f.packetSize
		// Samples made to fail health checks break fixed bits on purpose.
		malformed := strings.Contains(path, "all_findings") || strings.Contains(path, "pes_health") ||
			strings.Contains(path, "malformed") || strings.Contains(path, "sync_loss")
		for at := f.alignUp(0); at+int64(size) <= int64(len(data)) && at < 4000*int64(size); at += int64(size) {
			raw := data[at : at+int64(size)]
			if raw[tsOffset(size)] != 0x47 {
				continue
			}
			where := fmt.Sprintf("%s@%d", path, at)
			for _, part := range checkLayout(t, where, raw, decodePacketLayers(raw), !malformed) {
				kinds[part.Kind] = true
				if part.Valid != nil && !*part.Valid && !malformed {
					t.Errorf("%s: %s = %s, expected %s", where, part.ID, part.Value, part.Expected)
				}
			}
		}
	}
	for kind := range layoutKinds {
		if !kinds[kind] && kind != "unknown" {
			t.Errorf("no sample has a %s part", kind)
		}
	}
}

// part is the id, kind, bits and value of a layout part, as listed in the
// fixed expectations below.
type part struct {
	id     string
	kind   string
	offset int
	length int
	value  string
}

func layoutOf(t *testing.T, path string, offset int64, layer string) []part {
	t.Helper()
	detail, err := decodePacketDetail(t, path, options.Options{Offset: offset})
	if err != nil {
		t.Fatal(err)
	}
	_, l := layerFields(detail, layer)
	var got []part
	for _, p := range l.BitLayout {
		got = append(got, part{p.ID, p.Kind, p.Bits[0].BitOffset, p.Bits[0].BitLength, p.Value})
	}
	return got
}

// shifted moves expectations by bits, for the same structure in an M2TS
// packet, and renames the ids accordingly.
func shifted(parts []part, bits int) []part {
	out := make([]part, len(parts))
	for i, p := range parts {
		name, at, _ := strings.Cut(p.id, "@")
		offset, err := strconv.Atoi(at)
		if err != nil {
			panic(err)
		}
		p.id, p.offset = fmt.Sprintf("%s@%d", name, offset+bits), p.offset+bits
		out[i] = p
	}
	return out
}

// TestPacketLayoutFixedRanges pins the layout of representative packets to
// bit positions taken from ISO/IEC 13818-1 Tables 2-2, 2-6 and 2-21 and the
// PAT/PMT syntax, written out by hand rather than computed.
func TestPacketLayoutFixedRanges(t *testing.T) {
	tsHeader := []part{
		{"ts_header.sync_byte@0", "prefix", 0, 8, "0x47"},
		{"ts_header.transport_error_indicator@8", "flag", 8, 1, "0"},
		{"ts_header.payload_unit_start_indicator@9", "flag", 9, 1, "1"},
		{"ts_header.transport_priority@10", "flag", 10, 1, "0"},
		{"ts_header.pid@11", "value", 11, 13, "0x0100"},
		{"ts_header.transport_scrambling_control@24", "value", 24, 2, "0"},
		{"ts_header.adaptation_field_control@26", "value", 26, 2, "3 (adaptation field and payload)"},
		{"ts_header.continuity_counter@28", "value", 28, 4, "1"},
	}
	adaptation := []part{
		{"adaptation_field.adaptation_field_length@32", "value", 32, 8, "7"},
		{"adaptation_field.discontinuity_indicator@40", "flag", 40, 1, "0"},
		{"adaptation_field.random_access_indicator@41", "flag", 41, 1, "0"},
		{"adaptation_field.elementary_stream_priority_indicator@42", "flag", 42, 1, "0"},
		{"adaptation_field.pcr_flag@43", "flag", 43, 1, "1"},
		{"adaptation_field.opcr_flag@44", "flag", 44, 1, "0"},
		{"adaptation_field.splicing_point_flag@45", "flag", 45, 1, "0"},
		{"adaptation_field.transport_private_data_flag@46", "flag", 46, 1, "0"},
		{"adaptation_field.adaptation_field_extension_flag@47", "flag", 47, 1, "0"},
		{"adaptation_field.program_clock_reference.base@48", "value", 48, 33, "77400"},
		{"adaptation_field.program_clock_reference.reserved@81", "reserved", 81, 6, "111111"},
		{"adaptation_field.program_clock_reference.extension@87", "value", 87, 9, "0"},
	}
	pes := []part{
		{"pes_header.packet_start_code_prefix@96", "prefix", 96, 24, "0x000001"},
		{"pes_header.stream_id@120", "value", 120, 8, "0xe0"},
		{"pes_header.pes_packet_length@128", "value", 128, 16, "0"},
		{"pes_header.fixed_10@144", "prefix", 144, 2, "10"},
		{"pes_header.pes_scrambling_control@146", "value", 146, 2, "0"},
		{"pes_header.pes_priority@148", "flag", 148, 1, "0"},
		{"pes_header.data_alignment_indicator@149", "flag", 149, 1, "0"},
		{"pes_header.copyright@150", "flag", 150, 1, "0"},
		{"pes_header.original_or_copy@151", "flag", 151, 1, "0"},
		{"pes_header.pts_dts_flags@152", "value", 152, 2, "3"},
		{"pes_header.escr_flag@154", "flag", 154, 1, "0"},
		{"pes_header.es_rate_flag@155", "flag", 155, 1, "0"},
		{"pes_header.dsm_trick_mode_flag@156", "flag", 156, 1, "0"},
		{"pes_header.additional_copy_info_flag@157", "flag", 157, 1, "0"},
		{"pes_header.pes_crc_flag@158", "flag", 158, 1, "0"},
		{"pes_header.pes_extension_flag@159", "flag", 159, 1, "0"},
		{"pes_header.pes_header_data_length@160", "value", 160, 8, "10"},
		{"pes_header.pts.prefix@168", "prefix", 168, 4, "0011"},
		{"pes_header.pts.bits_32_30@172", "value", 172, 3, "000"},
		{"pes_header.pts.marker_bit@175", "marker", 175, 1, "1"},
		{"pes_header.pts.bits_29_15@176", "value", 176, 15, "000000000000100"},
		{"pes_header.pts.marker_bit@191", "marker", 191, 1, "1"},
		{"pes_header.pts.bits_14_0@192", "value", 192, 15, "011001010000000"},
		{"pes_header.pts.marker_bit@207", "marker", 207, 1, "1"},
		{"pes_header.dts.prefix@208", "prefix", 208, 4, "0001"},
		{"pes_header.dts.bits_32_30@212", "value", 212, 3, "000"},
		{"pes_header.dts.marker_bit@215", "marker", 215, 1, "1"},
		{"pes_header.dts.bits_29_15@216", "value", 216, 15, "000000000000100"},
		{"pes_header.dts.marker_bit@231", "marker", 231, 1, "1"},
		{"pes_header.dts.bits_14_0@232", "value", 232, 15, "010010001110000"},
		{"pes_header.dts.marker_bit@247", "marker", 247, 1, "1"},
	}
	pmt := []part{
		{"pmt.pointer_field@32", "value", 32, 8, "0"},
		{"pmt.table_id@40", "value", 40, 8, "0x02"},
		{"pmt.section_syntax_indicator@48", "flag", 48, 1, "1"},
		{"pmt.zero@49", "marker", 49, 1, "0"},
		{"pmt.section_length.reserved@50", "reserved", 50, 2, "11"},
		{"pmt.section_length@52", "value", 52, 12, "23"},
		{"pmt.program_number@64", "value", 64, 16, "2"},
		{"pmt.version_number.reserved@80", "reserved", 80, 2, "11"},
		{"pmt.version_number@82", "value", 82, 5, "0"},
		{"pmt.current_next_indicator@87", "flag", 87, 1, "1"},
		{"pmt.section_number@88", "value", 88, 8, "0"},
		{"pmt.last_section_number@96", "value", 96, 8, "0"},
		{"pmt.pcr_pid.reserved@104", "reserved", 104, 3, "111"},
		{"pmt.pcr_pid@107", "value", 107, 13, "0x0102"},
		{"pmt.program_info_length.reserved@120", "reserved", 120, 4, "1111"},
		{"pmt.program_info_length@124", "value", 124, 12, "0"},
		{"pmt.stream[0].stream_type@136", "value", 136, 8, "0x02 (13818-2 video or 11172-2 constrained parameter video stream)"},
		{"pmt.stream[0].elementary_pid.reserved@144", "reserved", 144, 3, "111"},
		{"pmt.stream[0].elementary_pid@147", "value", 147, 13, "0x0102"},
		{"pmt.stream[0].es_info_length.reserved@160", "reserved", 160, 4, "1111"},
		{"pmt.stream[0].es_info_length@164", "value", 164, 12, "0"},
		{"pmt.stream[1].stream_type@176", "value", 176, 8, "0x03 (11172 audio)"},
		{"pmt.stream[1].elementary_pid.reserved@184", "reserved", 184, 3, "111"},
		{"pmt.stream[1].elementary_pid@187", "value", 187, 13, "0x0103"},
		{"pmt.stream[1].es_info_length.reserved@200", "reserved", 200, 4, "1111"},
		{"pmt.stream[1].es_info_length@204", "value", 204, 12, "0"},
		{"pmt.crc_32@216", "value", 216, 32, "0x1058355a"},
	}
	pat := []part{
		{"pat.pointer_field@32", "value", 32, 8, "0"},
		{"pat.table_id@40", "value", 40, 8, "0x00"},
		{"pat.section_syntax_indicator@48", "flag", 48, 1, "1"},
		{"pat.zero@49", "marker", 49, 1, "0"},
		{"pat.section_length.reserved@50", "reserved", 50, 2, "11"},
		{"pat.section_length@52", "value", 52, 12, "17"},
		{"pat.transport_stream_id@64", "value", 64, 16, "1"},
		{"pat.version_number.reserved@80", "reserved", 80, 2, "11"},
		{"pat.version_number@82", "value", 82, 5, "0"},
		{"pat.current_next_indicator@87", "flag", 87, 1, "1"},
		{"pat.section_number@88", "value", 88, 8, "0"},
		{"pat.last_section_number@96", "value", 96, 8, "0"},
		{"pat.program[0].program_number@104", "value", 104, 16, "1"},
		{"pat.program[0].program_map_pid.reserved@120", "reserved", 120, 3, "111"},
		{"pat.program[0].program_map_pid@123", "value", 123, 13, "0x1000"},
		{"pat.program[1].program_number@136", "value", 136, 16, "2"},
		{"pat.program[1].program_map_pid.reserved@152", "reserved", 152, 3, "111"},
		{"pat.program[1].program_map_pid@155", "value", 155, 13, "0x1001"},
		{"pat.crc_32@168", "value", 168, 32, "0x20827a4d"},
	}
	m2ts := []part{
		{"m2ts_header.copy_permission_indicator@0", "value", 0, 2, "0"},
	}
	for _, tc := range []struct {
		path   string
		offset int64
		layer  string
		want   []part
	}{
		{twoPrograms188Path, 29892, "ts_header", tsHeader},
		{twoPrograms188Path, 29892, "adaptation_field", adaptation},
		{twoPrograms188Path, 29892, "pes_header", pes},
		{twoPrograms188Path, 29892, "payload", []part{{"payload.payload@96", "payload", 96, 1408, "176 bytes"}}},
		{twoPrograms188Path, 564, "pmt", pmt},
		{twoPrograms188Path, 188, "pat", pat},
		{twoPrograms192Path, 29892 / 188 * 192, "ts_header", shifted(tsHeader, 32)},
		{twoPrograms192Path, 29892 / 188 * 192, "adaptation_field", shifted(adaptation, 32)},
		{twoPrograms192Path, 29892 / 188 * 192, "pes_header", shifted(pes, 32)},
		{twoPrograms192Path, 576, "pmt", shifted(pmt, 32)},
		{twoPrograms192Path, 192, "pat", shifted(pat, 32)},
		{allFindingsFixtures[2].path, 12 * 204, "trailer", []part{{"trailer.trailer_bytes@1504", "unknown", 1504, 128, "16 bytes"}}},
		{allFindingsFixtures[2].path, 12 * 204, "payload", []part{{"payload.payload@1264", "payload", 1264, 240, "30 bytes"}}},
	} {
		got := layoutOf(t, tc.path, tc.offset, tc.layer)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s@%d %s:\n got %v\nwant %v", tc.path, tc.offset, tc.layer, got, tc.want)
		}
	}
	got := layoutOf(t, twoPrograms192Path, 576, "m2ts_header")
	if len(got) != 2 || !reflect.DeepEqual(got[0], m2ts[0]) || got[1].id != "m2ts_header.arrival_time_stamp@2" || got[1].offset != 2 || got[1].length != 30 {
		t.Errorf("m2ts_header = %v", got)
	}
	// The 204-byte packet's AF ends with 146 stuffing bytes before the PES.
	af := layoutOf(t, allFindingsFixtures[2].path, 12*204, "adaptation_field")
	if last := af[len(af)-1]; last != (part{"adaptation_field.stuffing_byte@96", "stuffing", 96, 1168, "0xff × 146"}) {
		t.Errorf("204-byte AF stuffing = %v", last)
	}
}

// decodedParts decodes raw and checks its layout.
func decodedParts(t *testing.T, where string, raw []byte) map[string]LayoutPart {
	t.Helper()
	return checkLayout(t, where, raw, decodePacketLayers(raw), false)
}

// wantParts checks the kind, bits, value and validity of parts by id; a
// validity of "" means the part has no expected value.
func wantParts(t *testing.T, where string, parts map[string]LayoutPart, want map[string][4]string) {
	t.Helper()
	for id, w := range want {
		p, ok := parts[id]
		if !ok {
			t.Errorf("%s: no part %s", where, id)
			continue
		}
		valid := ""
		if p.Valid != nil {
			valid = fmt.Sprint(*p.Valid)
		}
		got := [4]string{p.Kind, fmt.Sprintf("%d+%d", p.Bits[0].BitOffset, p.Bits[0].BitLength), p.Value, valid}
		if got != w {
			t.Errorf("%s: %s = %v, want %v (%+v)", where, id, got, w, p)
		}
	}
}

func tsPacket(header [4]byte, body ...byte) []byte {
	pkt := bytes.Repeat([]byte{0xFF}, 188)
	copy(pkt, header[:])
	copy(pkt[4:], body)
	return pkt
}

// TestPacketLayoutMismatches flags fixed bits that differ from the
// specification, and only those.
func TestPacketLayoutMismatches(t *testing.T) {
	// A PES whose '10', PTS prefix and marker bits are all wrong, followed by
	// a stuffing byte that is not 0xFF.
	pes := tsPacket([4]byte{0x47, 0x41, 0x00, 0x10},
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x40, 0xC0, 0x0C,
		0x20, 0x00, 0x00, 0x00, 0x00, 0x11, 0x00, 0x01, 0x00, 0x01, 0xFF, 0x12)
	parts := decodedParts(t, "bad PES", pes)
	wantParts(t, "bad PES", parts, map[string][4]string{
		"pes_header.fixed_10@80":                 {"prefix", "80+2", "01", "false"},
		"pes_header.pts.prefix@104":              {"prefix", "104+4", "0010", "false"},
		"pes_header.pts.marker_bit@111":          {"marker", "111+1", "0", "false"},
		"pes_header.pts.marker_bit@127":          {"marker", "127+1", "0", "false"},
		"pes_header.pts.marker_bit@143":          {"marker", "143+1", "0", "false"},
		"pes_header.dts.prefix@144":              {"prefix", "144+4", "0001", "true"},
		"pes_header.dts.marker_bit@151":          {"marker", "151+1", "1", "true"},
		"pes_header.stuffing_byte@184":           {"stuffing", "184+8", "0xff × 1", "true"},
		"pes_header.stuffing_byte@192":           {"stuffing", "192+8", "0x12", "false"},
		"pes_header.packet_start_code_prefix@32": {"prefix", "32+24", "0x000001", "true"},
	})
	var invalid []string
	for id, p := range parts {
		if p.Valid != nil && !*p.Valid {
			invalid = append(invalid, id)
		}
	}
	if len(invalid) != 6 {
		t.Errorf("invalid parts %v, want the 6 above", invalid)
	}

	// A PMT with its reserved bits cleared and a wrong fixed '0', and a TS
	// header without its sync byte.
	section := pmtSection(1, 0x0100, [2]uint16{0x1B, 0x0101})
	section[1] = 0xC0 | section[1]&0x0F // '0' set, reserved '00'
	section[8] &^= 0xE0
	section[13] &^= 0xE0
	pmtPacket := buildTsPacket(0x1000, true, 0, section)
	pmtPacket[0] = 0x46
	parts = decodedParts(t, "bad PMT", pmtPacket)
	wantParts(t, "bad PMT", parts, map[string][4]string{
		"ts_header.sync_byte@0":                     {"prefix", "0+8", "0x46", "false"},
		"pmt.zero@49":                               {"marker", "49+1", "1", "false"},
		"pmt.section_length.reserved@50":            {"reserved", "50+2", "00", "false"},
		"pmt.pcr_pid.reserved@104":                  {"reserved", "104+3", "000", "false"},
		"pmt.stream[0].elementary_pid.reserved@144": {"reserved", "144+3", "000", "false"},
		"pmt.program_info_length.reserved@120":      {"reserved", "120+4", "1111", "true"},
	})
	if p := parts["pmt.pcr_pid.reserved@104"]; p.Rule != ruleReserved || p.Row != nil || p.Field != "" {
		t.Errorf("PCR_PID reserved = %+v", p)
	}
}

// TestPacketLayoutAdaptationFieldVariants covers the optional adaptation
// field members, stuffing and adaptation fields that do not fit.
func TestPacketLayoutAdaptationFieldVariants(t *testing.T) {
	// Private data and an extension, then three stuffing bytes before the payload.
	af := tsPacket([4]byte{0x47, 0x01, 0x00, 0x30}, 9, 0x03, 2, 0xAA, 0xBB, 1, 0x00)
	parts := decodedParts(t, "private data", af)
	wantParts(t, "private data", parts, map[string][4]string{
		"adaptation_field.transport_private_data_length@48":            {"value", "48+8", "2", ""},
		"adaptation_field.transport_private_data.private_data_byte@56": {"unknown", "56+16", "2 bytes", ""},
		"adaptation_field.adaptation_field_extension_length@72":        {"value", "72+8", "1", ""},
		"adaptation_field.adaptation_field_extension@80":               {"unknown", "80+8", "1 byte", ""},
		"adaptation_field.stuffing_byte@88":                            {"stuffing", "88+24", "0xff × 3", "true"},
		"payload.payload@112":                                          {"payload", "112+1392", "174 bytes", ""},
	})

	// An adaptation-field-only packet whose PCR does not fit its length: the
	// PCR bits it has are unknown, and so are the bytes after it.
	short := tsPacket([4]byte{0x47, 0x01, 0x00, 0x20}, 3, 0x10, 0x00, 0x00)
	parts = decodedParts(t, "short AF", short)
	wantParts(t, "short AF", parts, map[string][4]string{
		"adaptation_field.program_clock_reference.base@48":     {"unknown", "48+16", "2 bytes", ""},
		"adaptation_field.bytes_after_the_adaptation_field@64": {"unknown", "64+1440", "180 bytes", ""},
	})
	if p := parts["adaptation_field.program_clock_reference.base@48"]; p.Label != "program_clock_reference_base (cut off by its length field)" || p.Field != "" {
		t.Errorf("cut PCR = %+v", p)
	}

	// An adaptation_field_length past the packet; PCR, OPCR and a splice
	// countdown that fit.
	long := tsPacket([4]byte{0x47, 0x01, 0x00, 0x20}, 200, 0x1C)
	long[18] = 0xFE
	parts = decodedParts(t, "long AF", long)
	wantParts(t, "long AF", parts, map[string][4]string{
		"adaptation_field.original_program_clock_reference.reserved@129": {"reserved", "129+6", "111111", "true"},
		"adaptation_field.splice_countdown@144":                          {"value", "144+8", "-2", ""},
		"adaptation_field.stuffing_byte@152":                             {"stuffing", "152+1352", "0xff × 169", "true"},
	})
	// Empty private data has no part: the stuffing follows its length.
	empty := tsPacket([4]byte{0x47, 0x01, 0x00, 0x30}, 3, 0x02, 0)
	wantParts(t, "empty private data", decodedParts(t, "empty private data", empty), map[string][4]string{
		"adaptation_field.transport_private_data_length@48": {"value", "48+8", "0", ""},
		"adaptation_field.stuffing_byte@56":                 {"stuffing", "56+8", "0xff × 1", "true"},
	})
	// An adaptation field of length 0, then the payload.
	parts = decodedParts(t, "empty AF", tsPacket([4]byte{0x47, 0x01, 0x00, 0x30}, 0))
	if len(parts) != 10 || parts["payload.payload@40"].Kind != "payload" {
		t.Errorf("empty AF = %v", parts)
	}
}

// TestPacketLayoutPesVariants covers the optional PES header members, PES
// headers without the optional header and headers cut by the packet.
func TestPacketLayoutPesVariants(t *testing.T) {
	// PTS, ESCR, ES rate, trick mode, copy info and CRC, then a stuffing byte.
	all := tsPacket([4]byte{0x47, 0x41, 0x00, 0x10},
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0xBE, 0x13,
		0x21, 0x00, 0x01, 0x00, 0x01,
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 0xFF)
	parts := decodedParts(t, "optional fields", all)
	wantParts(t, "optional fields", parts, map[string][4]string{
		"pes_header.escr@144":                 {"unknown", "144+48", "6 bytes", ""},
		"pes_header.es_rate@192":              {"unknown", "192+24", "3 bytes", ""},
		"pes_header.dsm_trick_mode@216":       {"unknown", "216+8", "1 byte", ""},
		"pes_header.additional_copy_info@224": {"unknown", "224+8", "1 byte", ""},
		"pes_header.previous_pes_crc@232":     {"unknown", "232+16", "2 bytes", ""},
		"pes_header.stuffing_byte@248":        {"stuffing", "248+8", "0xff × 1", "true"},
	})
	// A PES extension: the rest of the header is not decoded.
	ext := tsPacket([4]byte{0x47, 0x41, 0x00, 0x10}, 0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x41, 0x03, 1, 2, 3)
	wantParts(t, "extension", decodedParts(t, "extension", ext), map[string][4]string{
		"pes_header.pes_extension_and_stuffing@104": {"unknown", "104+24", "3 bytes", ""},
	})
	// A padding stream has no optional header.
	padding := tsPacket([4]byte{0x47, 0x41, 0x00, 0x10}, 0x00, 0x00, 0x01, 0xBE, 0x00, 0x10)
	if parts := decodedParts(t, "padding", padding); len(parts) != 8+1+3 {
		t.Errorf("padding stream = %v", parts)
	}
	// Headers cut by the end of the packet, after the start code and after
	// the flags; and a PTS that continues in the next packet.
	for _, tc := range []struct {
		header []byte
		last   string
		label  string
	}{
		{[]byte{0x00, 0x00, 0x01}, "pes_header.packet_start_code_prefix@1480", "packet_start_code_prefix"},
		{[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80}, "pes_header.pes_header_data_length@1504", ""},
		{[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05, 0x21, 0x00}, "pes_header.pts.bits_29_15@1496", "PTS[29..15] (continues in the next packet)"},
	} {
		pkt := packetWithPayload(0x0100, true, 0, tc.header)
		parts := decodedParts(t, tc.last, pkt)
		if tc.label == "" {
			if _, ok := parts[tc.last]; ok {
				t.Errorf("%s: a part past the packet", tc.last)
			}
			continue
		}
		if p, ok := parts[tc.last]; !ok || p.Label != tc.label {
			t.Errorf("%s: %+v", tc.last, p)
		}
	}
}

// TestPacketLayoutSectionVariants covers pointer fields, sections this
// decoder does not read, loops that do not fill their space, descriptors and
// section lengths too short for the section header.
func TestPacketLayoutSectionVariants(t *testing.T) {
	// The end of a previous section, then a PMT with program and stream
	// descriptors.
	body := []byte{0xE1, 0x00, 0xF0, 0x03, 0x09, 0x01, 0x00, 0x1B, 0xE1, 0x01, 0xF0, 0x02, 0x0A, 0x00}
	pmt := tsPacket([4]byte{0x47, 0x50, 0x00, 0x10}, append([]byte{2, 0xAB, 0xCD}, psiSection(0x02, 1, body)...)...)
	parts := decodedParts(t, "descriptors", pmt)
	wantParts(t, "descriptors", parts, map[string][4]string{
		"pmt.previous_section@40":               {"payload", "40+16", "2 bytes", ""},
		"pmt.program_info.descriptors@152":      {"unknown", "152+24", "3 bytes", ""},
		"pmt.stream[0].es_info.descriptors@216": {"unknown", "216+16", "2 bytes", ""},
		"pmt.crc_32@232":                        {"value", "232+32", parts["pmt.crc_32@232"].Value, ""},
	})
	if p := parts["pmt.stream[0].es_info.descriptors@216"]; p.Row == nil || *p.Row != 0 {
		t.Errorf("stream descriptors = %+v", p)
	}
	// Program descriptors longer than the section: cut by section_length.
	cut := []byte{0xE1, 0x00, 0xF0, 0x40, 0x09}
	parts = decodedParts(t, "long descriptors", buildTsPacket(0x1000, true, 0, psiSection(0x02, 1, cut)))
	if p := parts["pmt.program_info.descriptors@136"]; p.Label != "descriptors (cut off by its length field)" || p.Kind != "unknown" {
		t.Errorf("long descriptors = %+v", p)
	}

	// A PAT with two bytes after its last program, and one whose
	// section_length is too short for its header.
	pat := psiSection(0x00, 1, []byte{0x00, 0x01, 0xE1, 0x00, 0x12, 0x34})
	parts = decodedParts(t, "PAT leftover", buildTsPacket(0x0000, true, 0, pat))
	wantParts(t, "PAT leftover", parts, map[string][4]string{
		"pat.not_decoded@136": {"unknown", "136+16", "2 bytes", ""},
	})
	tiny := buildTsPacket(0x0000, true, 0, []byte{0x00, 0xB0, 0x03, 0x00, 0x01, 0xC1})
	parts = decodedParts(t, "tiny PAT", tiny)
	wantParts(t, "tiny PAT", parts, map[string][4]string{
		"pat.crc_32@64": {"unknown", "64+24", "3 bytes", ""},
	})
	if len(parts) != 8+1+7 {
		t.Errorf("tiny PAT = %v", parts)
	}

	// A section this decoder does not read: its body is not decoded.
	sdt := buildTsPacket(0x0011, true, 0, psiSection(0x42, 1, []byte{0x00, 0x01, 0xFF}))
	parts = decodedParts(t, "SDT", sdt)
	wantParts(t, "SDT", parts, map[string][4]string{
		"psi_section.section_syntax_indicator@48": {"flag", "48+1", "1", ""},
		"psi_section.private_indicator@49":        {"unknown", "49+1", "1 bit", ""},
		"psi_section.section_length.reserved@50":  {"reserved", "50+2", "11", ""},
		"psi_section.not_decoded@64":              {"unknown", "64+64", "8 bytes", ""},
	})

	// A short-form private section, such as a TDT: all section_length bytes
	// are private data, with no CRC_32, whatever the last four bytes hold.
	tdt := buildTsPacket(0x0014, true, 0, []byte{0x70, 0x70, 0x05, 0xE6, 0x40, 0x12, 0x34, 0x56})
	parts = decodedParts(t, "TDT", tdt)
	wantParts(t, "TDT", parts, map[string][4]string{
		"psi_section.section_syntax_indicator@48": {"flag", "48+1", "0", ""},
		"psi_section.not_decoded@64":              {"unknown", "64+40", "5 bytes", ""},
	})
	for id, p := range parts {
		if p.Label == "CRC_32" || strings.HasPrefix(id, "psi_section.crc_32") {
			t.Errorf("short-form section has a CRC part: %+v", p)
		}
	}
	// A short-form section that continues in the next packet.
	long := packetWithPayload(0x0014, true, 0, []byte{0x00, 0x70, 0x70, 0x20, 0xE6, 0x40})
	if p := decodedParts(t, "long TDT", long)["psi_section.not_decoded@1488"]; p.Label != "not decoded (continues in the next packet)" {
		t.Errorf("short-form section past the packet = %+v", p)
	}
	// A PAT with a zero section_syntax_indicator keeps the long form, and the
	// zero bit is a mismatch.
	zeroPAT := patSection([2]uint16{1, 0x1000})
	zeroPAT[1] &^= 0x80
	parts = decodedParts(t, "PAT with SSI 0", buildTsPacket(0x0000, true, 0, zeroPAT))
	wantParts(t, "PAT with SSI 0", parts, map[string][4]string{
		"pat.section_syntax_indicator@48": {"flag", "48+1", "0", "false"},
	})
	if p := parts["pat.crc_32@136"]; p.Kind != "value" {
		t.Errorf("PAT with SSI 0 CRC_32 = %+v", p)
	}
	// A long-form private section keeps its CRC_32.
	if p := decodedParts(t, "SDT CRC", sdt)["psi_section.crc_32@128"]; p.Kind != "value" || p.Field != "CRC_32" {
		t.Errorf("long-form CRC_32 = %+v", p)
	}

	// Sections cut by the packet: a PAT with more programs than fit, a
	// pointer_field past the packet, and a section_length that does not fit.
	programs := [][2]uint16{{0, 0x0010}}
	for i := uint16(1); i <= 50; i++ {
		programs = append(programs, [2]uint16{i, 0x1000 + i})
	}
	parts = decodedParts(t, "long PAT", buildTsPacket(0x0000, true, 0, patSection(programs...)))
	if p := parts["pat.program[43].program_number@1480"]; p.Row != nil || p.Field != "" || p.Kind != "value" {
		t.Errorf("program cut by the packet = %+v", p)
	}
	if p := parts["pat.program[0].network_pid@123"]; p.Value != "0x0010" || p.Field != "network_PID" {
		t.Errorf("network PID = %+v", p)
	}
	pointer := tsPacket([4]byte{0x47, 0x40, 0x12, 0x10}, 0xFE)
	if p := decodedParts(t, "pointer", pointer)["psi_section.previous_section@40"]; p.Label != "end of the previous section (continues in the next packet)" {
		t.Errorf("pointer past the packet = %+v", p)
	}
	late := packetWithPayload(0x0012, true, 0, []byte{0x00, 0x42, 0xB0})
	if p := decodedParts(t, "late", late)["psi_section.section_length@1500"]; p.Kind != "unknown" || p.Value != "4 bits" {
		t.Errorf("section_length cut by the packet = %+v", p)
	}
	last := packetWithPayload(0x0012, true, 0, []byte{0x00})
	if p := decodedParts(t, "last", last)["psi_section.pointer_field@1496"]; p.Kind != "value" {
		t.Errorf("pointer_field alone = %+v", p)
	}
}

func TestLayoutHelpers(t *testing.T) {
	if got := slug("'0' (x)"); got != "0_x" {
		t.Errorf("slug = %q", got)
	}
	if got := plural(1, "byte") + "/" + plural(3, "bit"); got != "1 byte/3 bits" {
		t.Errorf("plural = %q", got)
	}
}
