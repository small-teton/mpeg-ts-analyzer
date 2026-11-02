package tsparser

import (
	"encoding/hex"
	"fmt"
	"io"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// PacketDetail is the document written by --output packet: one packet decoded
// field by field, and the PES packet or PSI section it belongs to.
type PacketDetail struct {
	PacketsEnvelope
	Packet   *DecodedPacket  `json:"packet,omitempty"`
	Unit     *PacketUnit     `json:"unit,omitempty"`
	Warnings []ReportWarning `json:"warnings"`
}

// DecodedPacket is the raw packet and its decoded layers.
type DecodedPacket struct {
	OffsetBytes int64         `json:"offset_bytes"`
	Bytes       string        `json:"bytes"`
	Layers      []PacketLayer `json:"layers"`
}

// PacketLayer is one structure in the packet. Rows hold table entries (PAT
// programs, PMT streams). Truncated means the structure continues in
// following packets, so fields beyond this packet are not listed.
type PacketLayer struct {
	Name      string          `json:"name"`
	Fields    []PacketField   `json:"fields"`
	Rows      [][]PacketField `json:"rows,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
}

// PacketField is a decoded value and the bytes of the packet it was read
// from; byte_offset counts from the first byte of "bytes". Bits lists the
// exact bits the value was read from, in wire order; it is omitted when the
// value uses every bit of its byte span.
type PacketField struct {
	Name       string     `json:"name"`
	Value      string     `json:"value"`
	ByteOffset int        `json:"byte_offset"`
	ByteLength int        `json:"byte_length"`
	Bits       []BitRange `json:"bits,omitempty"`
}

// BitRange is a run of packet bits. Bit offset 0 is the most significant bit
// of the first packet byte, so bit 7 of byte i is at offset i*8.
type BitRange struct {
	BitOffset int `json:"bit_offset"`
	BitLength int `json:"bit_length"`
}

// bitsAt is the run of length bits that starts at bit first (0 = most
// significant) of the byte at offset.
func bitsAt(offset, first, length int) BitRange {
	return BitRange{BitOffset: offset*8 + first, BitLength: length}
}

// readBits concatenates the runs of raw, in order, into one value. Bits past
// the end of raw read as 0.
func readBits(raw []byte, runs ...BitRange) uint64 {
	var v uint64
	for _, r := range runs {
		for bit := r.BitOffset; bit < r.BitOffset+r.BitLength; bit++ {
			v <<= 1
			if bit/8 < len(raw) {
				v |= uint64(raw[bit/8]>>(7-bit%8)) & 1
			}
		}
	}
	return v
}

// timestampBits are the 33 value bits of a PTS or DTS whose 5 bytes start at
// offset; the 4-bit prefix and the marker bits are left out.
func timestampBits(offset int) []BitRange {
	return []BitRange{bitsAt(offset, 4, 3), bitsAt(offset+1, 0, 15), bitsAt(offset+3, 0, 15)}
}

// PacketUnit describes the PES packet or PSI section a packet belongs to,
// within the scan range given by --range-offset / --range-limit.
type PacketUnit struct {
	Kind                string   `json:"kind"`
	FirstOffsetBytes    *int64   `json:"first_offset_bytes,omitempty"`
	StartedBeforeRange  bool     `json:"started_before_range,omitempty"`
	Position            int      `json:"position"`
	PacketsInRange      int      `json:"packets_in_range"`
	PreviousOffsetBytes *int64   `json:"previous_offset_bytes,omitempty"`
	NextOffsetBytes     *int64   `json:"next_offset_bytes,omitempty"`
	StreamID            string   `json:"stream_id,omitempty"`
	PTSMs               *float64 `json:"pts_ms,omitempty"`
	DTSMs               *float64 `json:"dts_ms,omitempty"`
	TableID             string   `json:"table_id,omitempty"`
	HeaderPacketOffsets []int64  `json:"header_packet_offsets,omitempty"`
}

// readPacketDetail decodes the packet at --offset.
func readPacketDetail(r io.ReaderAt, size int64, opts options.Options, detail *PacketDetail) error {
	f, err := openPacketFile(r, size, opts.Offset, opts.PacketSize)
	if err != nil {
		return err
	}
	detail.Input.PacketSizeBytes = f.packetSize
	if aligned := f.alignUp(opts.Offset); aligned != opts.Offset {
		return fmt.Errorf("offset %d is not on a packet boundary (next packet at %d)", opts.Offset, aligned)
	}
	rangeStart := f.alignUp(opts.RangeOffset)
	rangeEnd := size
	if opts.RangeLimit > 0 {
		rangeEnd = min(size, opts.RangeOffset+opts.RangeLimit)
	}
	if opts.Offset < rangeStart || opts.Offset >= rangeEnd {
		return fmt.Errorf("offset %d is outside the scan range [%d, %d)", opts.Offset, rangeStart, rangeEnd)
	}
	raw, err := f.readPacket(opts.Offset)
	if err != nil {
		return fmt.Errorf("cannot read the packet at %d: %w", opts.Offset, err)
	}
	tp, err := f.parsePacket(raw, opts.Offset)
	if err != nil {
		return fmt.Errorf("cannot parse the packet at %d: %w", opts.Offset, err)
	}
	detail.Packet = &DecodedPacket{OffsetBytes: opts.Offset, Bytes: hex.EncodeToString(raw), Layers: decodePacketLayers(raw)}
	detail.Unit = findUnit(f, tp, opts.Offset, rangeStart, rangeEnd)
	detail.Status = "complete"
	return nil
}

// fieldBuilder collects fields that lie entirely within the packet bytes.
type fieldBuilder struct {
	raw       []byte
	truncated bool
}

// add appends a field of whole bytes when they are inside the packet and
// reports whether they were.
func (b *fieldBuilder) add(fields *[]PacketField, name string, offset, length int, format string, args ...any) bool {
	return b.addBits(fields, name, []BitRange{bitsAt(offset, 0, length*8)}, format, args...)
}

// addBits appends a field read from runs, given in wire order, when they are
// inside the packet and reports whether they were. The byte span covers the
// runs; the runs are listed only when they leave bits of that span out.
func (b *fieldBuilder) addBits(fields *[]PacketField, name string, runs []BitRange, format string, args ...any) bool {
	last := runs[len(runs)-1]
	offset := runs[0].BitOffset / 8
	end := (last.BitOffset + last.BitLength + 7) / 8
	if offset < 0 || end > len(b.raw) {
		b.truncated = true
		return false
	}
	field := PacketField{Name: name, Value: fmt.Sprintf(format, args...), ByteOffset: offset, ByteLength: end - offset}
	bits := 0
	for _, r := range runs {
		bits += r.BitLength
	}
	if bits != field.ByteLength*8 {
		field.Bits = runs
	}
	*fields = append(*fields, field)
	return true
}

// field appends a field whose value is read from run and formatted with
// format followed by args, and reports whether it was inside the packet. A
// run past the packet reads as 0, but such a field is rejected, so that value
// is never shown.
func (b *fieldBuilder) field(fields *[]PacketField, name, format string, run BitRange, args ...any) bool {
	return b.addBits(fields, name, []BitRange{run}, format, append([]any{b.value(run)}, args...)...)
}

// value reads the concatenation of runs; see field.
func (b *fieldBuilder) value(runs ...BitRange) uint64 { return readBits(b.raw, runs...) }

// decodePacketLayers decodes every structure of the packet whose start lies in it.
func decodePacketLayers(raw []byte) []PacketLayer {
	b := &fieldBuilder{raw: raw}
	t := len(raw) - tsPayloadSize
	var layers []PacketLayer
	if t == tpExtraHeaderSize {
		var fields []PacketField
		b.field(&fields, "copy_permission_indicator", "%d", bitsAt(0, 0, 2))
		b.field(&fields, "arrival_time_stamp", "%d", bitsAt(0, 2, 30))
		layers = append(layers, PacketLayer{Name: "m2ts_header", Fields: fields})
	}

	var header []PacketField
	afcBits, pusiBits := bitsAt(t+3, 2, 2), bitsAt(t+1, 1, 1)
	afc := uint8(b.value(afcBits))
	pusi := b.value(pusiBits) != 0
	b.field(&header, "sync_byte", "0x%02x", bitsAt(t, 0, 8))
	b.field(&header, "transport_error_indicator", "%d", bitsAt(t+1, 0, 1))
	b.field(&header, "payload_unit_start_indicator", "%d", pusiBits)
	b.field(&header, "transport_priority", "%d", bitsAt(t+1, 2, 1))
	b.field(&header, "PID", "0x%04x", bitsAt(t+1, 3, 13))
	b.field(&header, "transport_scrambling_control", "%d", bitsAt(t+3, 0, 2))
	b.field(&header, "adaptation_field_control", "%d (%s)", afcBits, adaptationFieldControlName(afc))
	b.field(&header, "continuity_counter", "%d", bitsAt(t+3, 4, 4))
	layers = append(layers, PacketLayer{Name: "ts_header", Fields: header})

	payload := t + tsHeaderSize
	if afc&0x2 != 0 {
		length := int(raw[t+4])
		layers = append(layers, decodeAdaptationField(b, t+4, length))
		payload += 1 + length
	}
	if afc&0x1 == 0 || payload >= len(raw) {
		return layers
	}
	var body []PacketField
	b.add(&body, "payload", payload, len(raw)-payload, "%d bytes", len(raw)-payload)
	layers = append(layers, PacketLayer{Name: "payload", Fields: body})
	if !pusi {
		return layers
	}
	if payload+3 <= len(raw) && raw[payload] == 0 && raw[payload+1] == 0 && raw[payload+2] == 1 {
		return append(layers, decodePesHeader(&fieldBuilder{raw: raw}, payload))
	}
	return append(layers, decodePsiSection(&fieldBuilder{raw: raw}, payload))
}

func adaptationFieldControlName(afc uint8) string {
	switch afc {
	case 1:
		return "payload only"
	case 2:
		return "adaptation field only"
	case 3:
		return "adaptation field and payload"
	default:
		return "reserved"
	}
}

func decodeAdaptationField(b *fieldBuilder, at, length int) PacketLayer {
	var fields []PacketField
	b.add(&fields, "adaptation_field_length", at, 1, "%d", length)
	if length == 0 {
		return PacketLayer{Name: "adaptation_field", Fields: fields}
	}
	flags := b.raw[at+1]
	for i, name := range []string{
		"discontinuity_indicator", "random_access_indicator", "elementary_stream_priority_indicator", "PCR_flag",
		"OPCR_flag", "splicing_point_flag", "transport_private_data_flag", "adaptation_field_extension_flag",
	} {
		b.field(&fields, name, "%d", bitsAt(at+1, i, 1))
	}
	pos, end := at+2, at+1+length
	clockField := func(name string) {
		if pos+6 <= end {
			// The 33-bit base and the 9-bit extension, around 6 reserved bits.
			base, ext := bitsAt(pos, 0, 33), bitsAt(pos+4, 7, 9)
			clock := b.value(base)*300 + b.value(ext)
			b.addBits(&fields, name, []BitRange{base, ext}, "%d (%.6f s)", clock, float64(clock)/pcrClockHz)
		}
		pos += 6
	}
	if flags&0x10 != 0 {
		clockField("program_clock_reference")
	}
	if flags&0x08 != 0 {
		clockField("original_program_clock_reference")
	}
	if flags&0x04 != 0 && pos < end {
		b.add(&fields, "splice_countdown", pos, 1, "%d", int8(b.raw[pos]))
	}
	return PacketLayer{Name: "adaptation_field", Fields: fields}
}

// streamHasPesHeader reports whether a stream_id carries the optional PES
// header (ISO/IEC 13818-1 Table 2-21).
func streamHasPesHeader(streamID byte) bool {
	switch streamID {
	case 0xBC, 0xBE, 0xBF, 0xF0, 0xF1, 0xF2, 0xF8, 0xFF:
		return false
	}
	return true
}

// decodeTimestamp reads the PTS or DTS whose 5 bytes start at offset of raw.
func decodeTimestamp(raw []byte, offset int) uint64 {
	return readBits(raw, timestampBits(offset)...)
}

func decodePesHeader(b *fieldBuilder, p int) PacketLayer {
	var fields []PacketField
	b.add(&fields, "packet_start_code_prefix", p, 3, "0x000001")
	flagBits := bitsAt(p+7, 0, 2)
	if b.field(&fields, "stream_id", "0x%02x", bitsAt(p+3, 0, 8)) &&
		b.field(&fields, "PES_packet_length", "%d", bitsAt(p+4, 0, 16)) &&
		streamHasPesHeader(b.raw[p+3]) &&
		b.field(&fields, "PES_scrambling_control", "%d", bitsAt(p+6, 2, 2)) &&
		b.field(&fields, "PTS_DTS_flags", "%d", flagBits) &&
		b.field(&fields, "PES_header_data_length", "%d", bitsAt(p+8, 0, 8)) {
		flags := b.value(flagBits)
		if flags&0x2 != 0 {
			b.timestampField(&fields, "PTS", p+9)
		}
		if flags == 0x3 {
			b.timestampField(&fields, "DTS", p+14)
		}
	}
	return PacketLayer{Name: "pes_header", Fields: fields, Truncated: b.truncated}
}

// timestampField appends the PTS or DTS whose 5 bytes start at offset; one
// that continues past the packet marks the layer truncated.
func (b *fieldBuilder) timestampField(fields *[]PacketField, name string, offset int) {
	runs := timestampBits(offset)
	ts := b.value(runs...)
	b.addBits(fields, name, runs, "%d (%.6f s)", ts, float64(ts)/90000)
}

func decodePsiSection(b *fieldBuilder, p int) PacketLayer {
	var fields []PacketField
	pointer := int(b.raw[p])
	b.add(&fields, "pointer_field", p, 1, "%d", pointer)
	s := p + 1 + pointer
	if !b.field(&fields, "table_id", "0x%02x", bitsAt(s, 0, 8)) {
		return PacketLayer{Name: "psi_section", Fields: fields, Truncated: true}
	}
	tableID := b.raw[s]
	name := map[byte]string{0x00: "pat", 0x02: "pmt"}[tableID]
	if name == "" {
		name = "psi_section"
	}
	sectionLength := bitsAt(s+1, 4, 12)
	if !b.field(&fields, "section_syntax_indicator", "%d", bitsAt(s+1, 0, 1)) ||
		!b.field(&fields, "section_length", "%d", sectionLength) {
		return PacketLayer{Name: name, Fields: fields, Truncated: true}
	}
	sectionEnd := s + 3 + int(b.value(sectionLength))
	crcAt := sectionEnd - 4
	var rows [][]PacketField
	if tableID == 0x00 || tableID == 0x02 {
		idName := "transport_stream_id"
		if tableID == 0x02 {
			idName = "program_number"
		}
		if b.field(&fields, idName, "%d", bitsAt(s+3, 0, 16)) &&
			b.field(&fields, "version_number", "%d", bitsAt(s+5, 2, 5)) &&
			b.field(&fields, "current_next_indicator", "%d", bitsAt(s+5, 7, 1)) &&
			b.field(&fields, "section_number", "%d", bitsAt(s+6, 0, 8)) &&
			b.field(&fields, "last_section_number", "%d", bitsAt(s+7, 0, 8)) {
			if tableID == 0x00 {
				rows = patRows(b, s+8, crcAt)
			} else {
				rows = pmtRows(b, s+8, crcAt, &fields)
			}
		}
	}
	if crcAt+4 <= len(b.raw) && crcAt >= s+3 {
		b.field(&fields, "CRC_32", "0x%08x", bitsAt(crcAt, 0, 32))
	} else {
		b.truncated = true
	}
	return PacketLayer{Name: name, Fields: fields, Rows: rows, Truncated: b.truncated}
}

func patRows(b *fieldBuilder, at, end int) [][]PacketField {
	var rows [][]PacketField
	for i := at; i+4 <= end; i += 4 {
		if i+4 > len(b.raw) {
			b.truncated = true
			break
		}
		programNumber := bitsAt(i, 0, 16)
		pidName := "program_map_PID"
		if b.value(programNumber) == 0 {
			pidName = "network_PID"
		}
		var row []PacketField
		b.field(&row, "program_number", "%d", programNumber)
		b.field(&row, pidName, "0x%04x", bitsAt(i+2, 3, 13))
		rows = append(rows, row)
	}
	return rows
}

func pmtRows(b *fieldBuilder, at, end int, fields *[]PacketField) [][]PacketField {
	programInfoLength := bitsAt(at+2, 4, 12)
	if !b.field(fields, "PCR_PID", "0x%04x", bitsAt(at, 3, 13)) ||
		!b.field(fields, "program_info_length", "%d", programInfoLength) {
		return nil
	}
	var rows [][]PacketField
	for i := at + 4 + int(b.value(programInfoLength)); i+5 <= end; {
		var row []PacketField
		if i+5 > len(b.raw) {
			b.truncated = true
			break
		}
		esInfoLengthBits := bitsAt(i+3, 4, 12)
		esInfoLength := int(b.value(esInfoLengthBits))
		b.field(&row, "stream_type", "0x%02x (%s)", bitsAt(i, 0, 8), StreamTypeString(b.raw[i]))
		b.field(&row, "elementary_PID", "0x%04x", bitsAt(i+1, 3, 13))
		b.field(&row, "ES_info_length", "%d", esInfoLengthBits)
		rows = append(rows, row)
		i += 5 + esInfoLength
	}
	return rows
}

// findUnit locates the PES packet or PSI section that contains the packet at
// offset, scanning packets of the same PID within [rangeStart, rangeEnd).
// Packets without payload, such as adaptation-field-only ones, carry no unit
// bytes and belong to no unit.
func findUnit(f *packetFile, tp *TsPacket, offset, rangeStart, rangeEnd int64) *PacketUnit {
	pid := tp.Pid()
	if pid == nullPidValue || len(tp.Payload()) == 0 {
		return &PacketUnit{Kind: "none"}
	}
	ps := int64(f.packetSize)
	same := func(at int64) (*TsPacket, []byte, bool) {
		raw, err := f.readPacket(at)
		if err != nil {
			return nil, nil, false
		}
		other, err := f.parsePacket(raw, at)
		if err != nil || other.Pid() != pid || len(other.Payload()) == 0 {
			return nil, nil, false
		}
		return other, raw, true
	}

	unit := &PacketUnit{Kind: "unknown", Position: 1}
	first := int64(-1)
	if tp.PayloadUnitStartIndicator() {
		first = offset
	} else {
		for at := offset - ps; at >= rangeStart; at -= ps {
			other, _, ok := same(at)
			if !ok {
				continue
			}
			unit.Position++
			if unit.PreviousOffsetBytes == nil {
				previous := at
				unit.PreviousOffsetBytes = &previous
			}
			if other.PayloadUnitStartIndicator() {
				first = at
				break
			}
		}
	}
	after := 0
	for at := offset + ps; at+ps <= rangeEnd; at += ps {
		other, _, ok := same(at)
		if !ok {
			continue
		}
		if other.PayloadUnitStartIndicator() {
			break
		}
		if unit.NextOffsetBytes == nil {
			next := at
			unit.NextOffsetBytes = &next
		}
		after++
	}
	unit.PacketsInRange = unit.Position + after

	if first < 0 {
		unit.StartedBeforeRange = true
		return unit
	}
	unit.FirstOffsetBytes = &first
	firstPacket, _, _ := same(first)
	start := firstPacket.Payload()
	if len(start) >= 3 && start[0] == 0 && start[1] == 0 && start[2] == 1 {
		unit.Kind = "pes"
		describePesUnit(unit, f, same, first, start, ps, rangeEnd)
		return unit
	}
	unit.Kind = "psi"
	if len(start) > 0 && 1+int(start[0]) < len(start) {
		unit.TableID = fmt.Sprintf("0x%02x", start[1+int(start[0])])
	}
	return unit
}

// describePesUnit decodes the PES header of a unit, gathering the payload of
// its first packets when the header spans more than one.
func describePesUnit(unit *PacketUnit, f *packetFile, same func(int64) (*TsPacket, []byte, bool), first int64, start []byte, ps, rangeEnd int64) {
	header := append([]byte(nil), start...)
	unit.HeaderPacketOffsets = []int64{first}
	// The fixed part is 6 bytes; streams with the optional header need 9 more
	// bytes' worth of flags and lengths, then PES_header_data_length bytes.
	need := func() int {
		switch {
		case len(header) < 6 || !streamHasPesHeader(header[3]):
			return 6
		case len(header) < 9:
			return 9
		default:
			return 9 + int(header[8])
		}
	}
	for at := first + ps; len(header) < need() && at+ps <= rangeEnd; at += ps {
		other, _, ok := same(at)
		if !ok {
			continue
		}
		if other.PayloadUnitStartIndicator() {
			break
		}
		header = append(header, other.Payload()...)
		unit.HeaderPacketOffsets = append(unit.HeaderPacketOffsets, at)
	}
	if len(header) < 6 {
		return
	}
	unit.StreamID = fmt.Sprintf("0x%02x", header[3])
	if len(header) < 9 || !streamHasPesHeader(header[3]) {
		return
	}
	flags := header[7] >> 6
	if flags&0x2 != 0 && len(header) >= 14 {
		unit.PTSMs = optionalFloat(float64(decodeTimestamp(header, 9))/90, true)
	}
	if flags == 0x3 && len(header) >= 19 {
		unit.DTSMs = optionalFloat(float64(decodeTimestamp(header, 14))/90, true)
	}
}
