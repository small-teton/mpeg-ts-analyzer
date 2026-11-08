package tsparser

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

// LayoutPart classifies one run of packet bits in a layer's bit_layout: what
// the bits are on the wire (a value, a flag, reserved or marker bits, a fixed
// prefix, stuffing, payload, or bits this decoder does not interpret), whether
// or not they contribute to a decoded field. The parts of a layer are in wire
// order and do not overlap; together they cover the layer's span in the packet.
type LayoutPart struct {
	// ID is unique within the packet and stable for the same bytes: the layer,
	// the structure the part sits in and the part's first bit.
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	// Bits are packet-relative, as in PacketField.Bits.
	Bits  []BitRange `json:"bits"`
	Value string     `json:"value"`
	// Expected and Valid are present only where the specification fixes the
	// bits and Rule cites where.
	Expected string `json:"expected,omitempty"`
	Valid    *bool  `json:"valid,omitempty"`
	Rule     string `json:"rule,omitempty"`
	// Field names the decoded field of the layer (of Rows[Row] when Row is
	// set) the bits belong to; several parts can share one, such as the
	// prefix, value runs and marker bits of a PTS.
	Field string `json:"field,omitempty"`
	// Row is the table entry of the layer (PAT program, PMT stream) the bits
	// belong to.
	Row *int `json:"row,omitempty"`
}

// Layout part kinds.
const (
	partValue    = "value"
	partFlag     = "flag"
	partReserved = "reserved"
	partMarker   = "marker"
	partPrefix   = "prefix"
	partStuffing = "stuffing"
	partPayload  = "payload"
	partUnknown  = "unknown"
)

// The specification rules behind the expected values of layout parts.
const (
	ruleSyncByte      = "ISO/IEC 13818-1 Table 2-2: sync_byte is 0x47"
	ruleReserved      = "ISO/IEC 13818-1 2.1 (reserved): reserved bits are set to '1' unless otherwise specified; receivers ignore them"
	ruleAFStuffing    = "ISO/IEC 13818-1 Table 2-6: stuffing_byte is '1111 1111'"
	rulePesStartCode  = "ISO/IEC 13818-1 Table 2-21: packet_start_code_prefix is 0x000001"
	rulePesHeaderBits = "ISO/IEC 13818-1 Table 2-21: the optional PES header starts with '10'"
	ruleMarkerBit     = "ISO/IEC 13818-1 Table 2-21: marker_bit is '1'"
	rulePesStuffing   = "ISO/IEC 13818-1 Table 2-21: stuffing_byte is '1111 1111'"
)

// Value formats of a layout part.
const (
	formatDecimal = iota
	formatBinary
	formatHex
	formatCount
)

// notDecoded labels bits of a structure that no part describes.
const notDecoded = "not decoded"

// noEnd is the end of a structure whose length is not known yet.
const noEnd = math.MaxInt32

type partOption func(*partSpec)

type partSpec struct {
	name, ctx, field string
	format, digits   int
	expected, rule   string
}

// withField links the part to a decoded field other than its label.
func withField(name string) partOption { return func(s *partSpec) { s.field = name } }

// withID sets the part's id segment instead of one made from its label, and
// the structure it is in.
func withID(ctx, name string) partOption {
	return func(s *partSpec) { s.ctx, s.name = ctx, name }
}

// inside sets the structure the part is in, for its id.
func inside(ctx string) partOption { return func(s *partSpec) { s.ctx = ctx } }

func asBinary() partOption { return func(s *partSpec) { s.format = formatBinary } }

func asHex(digits int) partOption {
	return func(s *partSpec) { s.format, s.digits = formatHex, digits }
}

// expect sets the bits the specification fixes, as the value is formatted.
func expect(value, rule string) partOption {
	return func(s *partSpec) { s.expected, s.rule = value, rule }
}

// layoutBuilder lays parts one after another from a starting bit. A part that
// does not fit before the end of the structure or of the packet becomes an
// unknown part up to there, and nothing more is laid out in that structure.
type layoutBuilder struct {
	raw   []byte
	layer string
	pos   int
	// end is the end of the structure (or of a region of it) being laid out,
	// limit the end of the bits the packet has.
	end, limit int
	// overflow describes a structure that goes on past limit.
	overflow string
	// full: limit was reached; cut: end was.
	full, cut bool
	row       *int
	rowCtx    string
	parts     []LayoutPart
}

func newLayout(raw []byte, layer string, start, end int, overflow string) *layoutBuilder {
	return &layoutBuilder{raw: raw, layer: layer, pos: start, end: end, limit: len(raw) * 8, overflow: overflow}
}

// setRow makes the following parts belong to table entry index, or to none.
func (l *layoutBuilder) setRow(ctx string, index int) {
	if ctx == "" {
		l.row, l.rowCtx = nil, ""
		return
	}
	l.row, l.rowCtx = &index, fmt.Sprintf("%s[%d]", ctx, index)
}

func slug(label string) string {
	var sb strings.Builder
	under := false
	for _, r := range strings.ToLower(label) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if under && sb.Len() > 0 {
				sb.WriteByte('_')
			}
			sb.WriteRune(r)
			under = false
		} else {
			under = true
		}
	}
	return sb.String()
}

// add lays the next n bits out as a part and returns it, or nil when there
// are none, they do not fit (see layoutBuilder) or nothing more fits.
func (l *layoutBuilder) add(kind, label string, n int, opts ...partOption) *LayoutPart {
	// An empty member, such as private data of length 0, has no bits.
	if l.full || l.cut || n == 0 {
		return nil
	}
	spec := partSpec{}
	switch kind {
	case partValue, partFlag:
		spec.field = label
	case partPrefix:
		spec.field, spec.format = label, formatBinary
	case partPayload, partUnknown, partStuffing:
		spec.format = formatCount
	default:
		spec.format = formatBinary
	}
	for _, opt := range opts {
		opt(&spec)
	}
	if stop := min(l.end, l.limit); l.pos+n > stop {
		reason := "cut off by its length field"
		if l.end > l.limit {
			reason, l.full = l.overflow, true
		} else {
			l.cut = true
		}
		if l.pos < stop {
			name := spec.name
			if name == "" {
				name = slug(label)
			}
			l.append(partUnknown, fmt.Sprintf("%s (%s)", label, reason), stop-l.pos, partSpec{name: name, ctx: spec.ctx, field: spec.field, format: formatCount})
		}
		return nil
	}
	return l.append(kind, label, n, spec)
}

func (l *layoutBuilder) append(kind, label string, n int, spec partSpec) *LayoutPart {
	name := spec.name
	if name == "" {
		name = slug(label)
	}
	path := name
	if spec.ctx != "" {
		path = spec.ctx + "." + path
	}
	if l.rowCtx != "" {
		path = l.rowCtx + "." + path
	}
	run := BitRange{BitOffset: l.pos, BitLength: n}
	part := LayoutPart{
		ID:    fmt.Sprintf("%s.%s@%d", l.layer, path, l.pos),
		Label: label,
		Kind:  kind,
		Bits:  []BitRange{run},
		Value: l.format(run, spec),
		Field: spec.field,
		Row:   l.row,
	}
	if spec.expected != "" {
		valid := part.Value == spec.expected
		part.Expected, part.Valid, part.Rule = spec.expected, &valid, spec.rule
	}
	l.parts = append(l.parts, part)
	l.pos += n
	return &l.parts[len(l.parts)-1]
}

func (l *layoutBuilder) format(run BitRange, spec partSpec) string {
	switch spec.format {
	case formatCount:
		if run.BitLength%8 == 0 {
			return plural(run.BitLength/8, "byte")
		}
		return plural(run.BitLength, "bit")
	case formatBinary:
		return fmt.Sprintf("%0*b", run.BitLength, readBits(l.raw, run))
	case formatHex:
		return fmt.Sprintf("0x%0*x", spec.digits, readBits(l.raw, run))
	default:
		return fmt.Sprint(readBits(l.raw, run))
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// rest lays the bits up to the end of the structure out as one part, unless
// they are past the packet.
func (l *layoutBuilder) rest(kind, label string) {
	if l.full {
		return
	}
	if stop := min(l.end, l.limit); l.pos < stop {
		l.append(kind, label, stop-l.pos, partSpec{format: formatCount})
	}
	l.full = l.end > l.limit
}

// stuffing lays the bytes up to the end of the structure out as stuffing,
// one part per run of bytes that are, or are not, the expected 0xFF.
func (l *layoutBuilder) stuffing(rule string) {
	for !l.full && l.pos < min(l.end, l.limit) {
		first := l.pos / 8
		stop := min(l.end, l.limit) / 8
		valid := l.raw[first] == 0xFF
		last := first + 1
		for last < stop && (l.raw[last] == 0xFF) == valid {
			last++
		}
		value := fmt.Sprintf("0xff × %d", last-first)
		if !valid {
			value = "0x" + hex.EncodeToString(l.raw[first:last])
		}
		part := l.append(partStuffing, "stuffing_byte", (last-first)*8, partSpec{})
		part.Value, part.Expected, part.Valid, part.Rule = value, "0xff", &valid, rule
	}
	l.full = l.full || l.end > l.limit
}

// region lays out body as a part of the structure that ends at end; bits of
// it that body leaves out become one unknown part.
func (l *layoutBuilder) region(end int, body func()) {
	outer := l.end
	l.end = min(end, outer)
	body()
	l.rest(partUnknown, notDecoded)
	l.end, l.cut = outer, false
}

// finish covers what is left of the structure and returns the parts.
func (l *layoutBuilder) finish() []LayoutPart {
	l.rest(partUnknown, notDecoded)
	return l.parts
}

// withLayout gives a layer its bit layout and links the parts to the layer's
// decoded fields: a link is kept only when the field exists and its bytes hold
// the part, and a part with exactly the field's bits takes the field's value.
func withLayout(layer PacketLayer, parts []LayoutPart) PacketLayer {
	for i := range parts {
		part := &parts[i]
		fields := layer.Fields
		if part.Row != nil {
			if *part.Row >= len(layer.Rows) {
				part.Row, part.Field = nil, ""
				continue
			}
			fields = layer.Rows[*part.Row]
		}
		if part.Field == "" {
			continue
		}
		field, ok := findField(fields, part.Field)
		run := part.Bits[0]
		if !ok || run.BitOffset < field.ByteOffset*8 || run.BitOffset+run.BitLength > (field.ByteOffset+field.ByteLength)*8 {
			part.Field = ""
			continue
		}
		bits := field.Bits
		if bits == nil {
			bits = []BitRange{bitsAt(field.ByteOffset, 0, field.ByteLength*8)}
		}
		if len(bits) == 1 && bits[0] == run {
			part.Value = field.Value
		}
	}
	layer.BitLayout = parts
	return layer
}

func findField(fields []PacketField, name string) (PacketField, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return PacketField{}, false
}

// m2tsHeaderLayout is the 4-byte header of a 192-byte packet.
func m2tsHeaderLayout(raw []byte) []LayoutPart {
	l := newLayout(raw, "m2ts_header", 0, 32, "")
	l.add(partValue, "copy_permission_indicator", 2)
	l.add(partValue, "arrival_time_stamp", 30)
	return l.finish()
}

// tsHeaderLayout is the 4-byte TS header at byte t (ISO/IEC 13818-1 Table 2-2).
// With the reserved adaptation_field_control '00' nothing follows the header,
// so the rest of the TS packet is listed as unknown.
func tsHeaderLayout(raw []byte, t int, afc uint8) []LayoutPart {
	l := newLayout(raw, "ts_header", t*8, (t+4)*8, "")
	l.add(partPrefix, "sync_byte", 8, asHex(2), expect("0x47", ruleSyncByte))
	l.add(partFlag, "transport_error_indicator", 1)
	l.add(partFlag, "payload_unit_start_indicator", 1)
	l.add(partFlag, "transport_priority", 1)
	l.add(partValue, "PID", 13, asHex(4))
	l.add(partValue, "transport_scrambling_control", 2)
	l.add(partValue, "adaptation_field_control", 2)
	l.add(partValue, "continuity_counter", 4)
	l.finish()
	if afc == 0 {
		l.end = l.limit
		l.rest(partUnknown, "packet body (adaptation_field_control '00' is reserved)")
	}
	return l.parts
}

// adaptationFieldLayout is the adaptation field at byte at (ISO/IEC 13818-1
// Table 2-6). In a packet without payload, the bytes after it, which
// adaptation_field_length should leave none of, are listed as unknown.
func adaptationFieldLayout(raw []byte, at, length int, payload bool) []LayoutPart {
	end := (at + 1 + length) * 8
	l := newLayout(raw, "adaptation_field", at*8, end, "runs past the end of the packet")
	l.add(partValue, "adaptation_field_length", 8)
	if length > 0 {
		flags := raw[at+1]
		for _, name := range []string{
			"discontinuity_indicator", "random_access_indicator", "elementary_stream_priority_indicator", "PCR_flag",
			"OPCR_flag", "splicing_point_flag", "transport_private_data_flag", "adaptation_field_extension_flag",
		} {
			l.add(partFlag, name, 1)
		}
		if flags&0x10 != 0 {
			l.clockReference("program_clock_reference")
		}
		if flags&0x08 != 0 {
			l.clockReference("original_program_clock_reference")
		}
		if flags&0x04 != 0 {
			l.add(partValue, "splice_countdown", 8)
		}
		if flags&0x02 != 0 {
			if part := l.add(partValue, "transport_private_data_length", 8); part != nil {
				l.add(partUnknown, "private_data_byte", int(raw[part.Bits[0].BitOffset/8])*8, inside("transport_private_data"))
			}
		}
		if flags&0x01 != 0 {
			if part := l.add(partValue, "adaptation_field_extension_length", 8); part != nil {
				l.add(partUnknown, "adaptation_field_extension", int(raw[part.Bits[0].BitOffset/8])*8)
			}
		}
		l.stuffing(ruleAFStuffing)
	}
	l.finish()
	if !payload && !l.full {
		l.end = l.limit
		l.rest(partUnknown, "bytes after the adaptation field")
	}
	return l.parts
}

// clockReference lays out a PCR or OPCR: the 33-bit base, 6 reserved bits and
// the 9-bit extension.
func (l *layoutBuilder) clockReference(name string) {
	l.add(partValue, name+"_base", 33, withField(name), withID(name, "base"))
	l.add(partReserved, "reserved", 6, withField(name), inside(name), expect("111111", ruleReserved))
	l.add(partValue, name+"_extension", 9, withField(name), withID(name, "extension"))
}

// payloadLayout is the TS payload from byte at to the end of the TS packet.
func payloadLayout(raw []byte, at int) []LayoutPart {
	l := newLayout(raw, "payload", at*8, len(raw)*8, "")
	l.add(partPayload, "payload", (len(raw)-at)*8, withField("payload"))
	return l.finish()
}

// trailerLayout is the trailer after the TS packet of a 204-byte packet, which
// is not decoded.
func trailerLayout(raw []byte, at int) []LayoutPart {
	l := newLayout(raw, "trailer", at*8, len(raw)*8, "")
	l.add(partUnknown, "trailer_bytes", (len(raw)-at)*8, withField("trailer_bytes"))
	return l.finish()
}

// pesHeaderLayout is the PES header that starts at byte p (ISO/IEC 13818-1
// Table 2-21), up to the end of PES_header_data_length.
func pesHeaderLayout(raw []byte, p int) []LayoutPart {
	l := newLayout(raw, "pes_header", p*8, noEnd, "continues in the next packet")
	l.add(partPrefix, "packet_start_code_prefix", 24, asHex(6), expect("0x000001", rulePesStartCode))
	if l.add(partValue, "stream_id", 8, asHex(2)) == nil || l.add(partValue, "PES_packet_length", 16) == nil {
		return l.finish()
	}
	if !streamHasPesHeader(raw[p+3]) {
		return l.parts
	}
	l.add(partPrefix, "'10'", 2, withID("", "fixed_10"), expect("10", rulePesHeaderBits))
	l.add(partValue, "PES_scrambling_control", 2)
	for _, name := range []string{"PES_priority", "data_alignment_indicator", "copyright", "original_or_copy"} {
		l.add(partFlag, name, 1)
	}
	l.add(partValue, "PTS_DTS_flags", 2)
	for _, name := range []string{"ESCR_flag", "ES_rate_flag", "DSM_trick_mode_flag", "additional_copy_info_flag", "PES_CRC_flag", "PES_extension_flag"} {
		l.add(partFlag, name, 1)
	}
	if l.add(partValue, "PES_header_data_length", 8) == nil {
		return l.finish()
	}
	l.end = (p + 9 + int(raw[p+8])) * 8
	flags := raw[p+7]
	switch flags >> 6 {
	case 2:
		l.timestamp("PTS", "0010")
	case 3:
		l.timestamp("PTS", "0011")
		l.timestamp("DTS", "0001")
	}
	for _, optional := range []struct {
		flag  byte
		label string
		bits  int
	}{
		{0x20, "ESCR", 48}, {0x10, "ES_rate", 24}, {0x08, "DSM_trick_mode", 8},
		{0x04, "additional_copy_info", 8}, {0x02, "previous_PES_CRC", 16},
	} {
		if flags&optional.flag != 0 {
			l.add(partUnknown, optional.label, optional.bits)
		}
	}
	if flags&0x01 != 0 {
		l.rest(partUnknown, "PES extension and stuffing")
	} else {
		l.stuffing(rulePesStuffing)
	}
	return l.finish()
}

// timestamp lays out a PTS or DTS: its 4-bit prefix, then 3, 15 and 15 value
// bits, each followed by a marker bit.
func (l *layoutBuilder) timestamp(name, prefix string) {
	ctx := strings.ToLower(name)
	rule := fmt.Sprintf("ISO/IEC 13818-1 Table 2-21: with PTS_DTS_flags '%s', the %s starts with '%s'", map[string]string{"0010": "10", "0011": "11", "0001": "11"}[prefix], name, prefix)
	l.add(partPrefix, "prefix", 4, withField(name), inside(ctx), expect(prefix, rule))
	for _, run := range []struct {
		label string
		bits  int
	}{{"[32..30]", 3}, {"[29..15]", 15}, {"[14..0]", 15}} {
		l.add(partValue, name+run.label, run.bits, withField(name), withID(ctx, "bits_"+slug(run.label)), asBinary())
		l.add(partMarker, "marker_bit", 1, withField(name), inside(ctx), expect("1", ruleMarkerBit))
	}
}

// sectionRules are the fixed bits of the sections this decoder reads.
type sectionRules struct {
	syntax string
	zero   string
}

var knownSections = map[byte]sectionRules{
	0x00: {
		"ISO/IEC 13818-1 program association section: section_syntax_indicator is '1'",
		"ISO/IEC 13818-1 program association section: the bit after section_syntax_indicator is '0'",
	},
	0x02: {
		"ISO/IEC 13818-1 TS program map section: section_syntax_indicator is '1'",
		"ISO/IEC 13818-1 TS program map section: the bit after section_syntax_indicator is '0'",
	},
}

// psiSectionLayout is the pointer_field at byte p and the section it points
// to, up to the end of the section.
func psiSectionLayout(raw []byte, layer string, p int) []LayoutPart {
	l := newLayout(raw, layer, p*8, noEnd, "continues in the next packet")
	l.add(partValue, "pointer_field", 8)
	if pointer := int(raw[p]); pointer > 0 {
		l.add(partPayload, "end of the previous section", pointer*8, withID("", "previous_section"))
	}
	s := p + 1 + int(raw[p])
	if l.add(partValue, "table_id", 8, asHex(2)) == nil {
		return l.finish()
	}
	rules, known := knownSections[raw[s]]
	if known {
		l.add(partFlag, "section_syntax_indicator", 1, expect("1", rules.syntax))
		l.add(partMarker, "'0'", 1, withID("", "zero"), expect("0", rules.zero))
		l.add(partReserved, "reserved", 2, inside("section_length"), expect("11", ruleReserved))
	} else {
		l.add(partFlag, "section_syntax_indicator", 1)
		l.add(partUnknown, "private_indicator", 1)
		l.add(partReserved, "reserved", 2, inside("section_length"))
	}
	if l.add(partValue, "section_length", 12) == nil {
		return l.finish()
	}
	sectionEnd := s + 3 + int(readBits(raw, bitsAt(s+1, 4, 12)))
	l.end = sectionEnd * 8
	l.region((sectionEnd-4)*8, func() {
		switch raw[s] {
		case 0x00:
			l.longSectionHeader("transport_stream_id")
			l.patPrograms()
		case 0x02:
			l.longSectionHeader("program_number")
			l.pmtStreams()
		}
	})
	l.add(partValue, "CRC_32", 32, asHex(8))
	return l.finish()
}

// longSectionHeader lays out the fields from the table_id extension, named
// idName, to last_section_number.
func (l *layoutBuilder) longSectionHeader(idName string) {
	l.add(partValue, idName, 16)
	l.add(partReserved, "reserved", 2, inside("version_number"), expect("11", ruleReserved))
	l.add(partValue, "version_number", 5)
	l.add(partFlag, "current_next_indicator", 1)
	l.add(partValue, "section_number", 8)
	l.add(partValue, "last_section_number", 8)
}

// patPrograms lays out the program loop of a PAT, one row per program.
func (l *layoutBuilder) patPrograms() {
	for i := 0; !l.full && !l.cut && l.pos+32 <= l.end; i++ {
		l.setRow("program", i)
		number := l.add(partValue, "program_number", 16)
		pidName := "program_map_PID"
		if number != nil && readBits(l.raw, number.Bits[0]) == 0 {
			pidName = "network_PID"
		}
		l.add(partReserved, "reserved", 3, inside(slug(pidName)), expect("111", ruleReserved))
		l.add(partValue, pidName, 13, asHex(4))
	}
	l.setRow("", 0)
}

// pmtStreams lays out a PMT from PCR_PID: the program descriptors, then one
// row per elementary stream.
func (l *layoutBuilder) pmtStreams() {
	l.add(partReserved, "reserved", 3, inside("pcr_pid"), expect("111", ruleReserved))
	l.add(partValue, "PCR_PID", 13, asHex(4))
	l.add(partReserved, "reserved", 4, inside("program_info_length"), expect("1111", ruleReserved))
	if length := l.add(partValue, "program_info_length", 12); length != nil {
		if n := int(readBits(l.raw, length.Bits[0])); n > 0 {
			l.add(partUnknown, "descriptors", n*8, inside("program_info"))
		}
	}
	for i := 0; !l.full && !l.cut && l.pos+40 <= l.end; i++ {
		l.setRow("stream", i)
		l.add(partValue, "stream_type", 8, asHex(2))
		l.add(partReserved, "reserved", 3, inside("elementary_pid"), expect("111", ruleReserved))
		l.add(partValue, "elementary_PID", 13, asHex(4))
		l.add(partReserved, "reserved", 4, inside("es_info_length"), expect("1111", ruleReserved))
		if length := l.add(partValue, "ES_info_length", 12); length != nil {
			if n := int(readBits(l.raw, length.Bits[0])); n > 0 {
				l.add(partUnknown, "descriptors", n*8, inside("es_info"))
			}
		}
	}
	l.setRow("", 0)
}
