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
// from; byte_offset counts from the first byte of "bytes".
type PacketField struct {
	Name       string `json:"name"`
	Value      string `json:"value"`
	ByteOffset int    `json:"byte_offset"`
	ByteLength int    `json:"byte_length"`
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

// add appends a field when its bytes are inside the packet and reports
// whether it was.
func (b *fieldBuilder) add(fields *[]PacketField, name string, offset, length int, format string, args ...any) bool {
	if offset < 0 || offset+length > len(b.raw) {
		b.truncated = true
		return false
	}
	*fields = append(*fields, PacketField{Name: name, Value: fmt.Sprintf(format, args...), ByteOffset: offset, ByteLength: length})
	return true
}

func (b *fieldBuilder) u16(offset int) uint16 {
	return uint16(b.raw[offset])<<8 | uint16(b.raw[offset+1])
}

func (b *fieldBuilder) pid(offset int) uint16 { return b.u16(offset) & 0x1FFF }

// byteAt and u16At read values for fields that may lie past the end of the
// packet; add then rejects those fields, so the zero value is never shown.
func (b *fieldBuilder) byteAt(offset int) byte {
	if offset < len(b.raw) {
		return b.raw[offset]
	}
	return 0
}

func (b *fieldBuilder) u16At(offset int) uint16 {
	return uint16(b.byteAt(offset))<<8 | uint16(b.byteAt(offset+1))
}

// decodePacketLayers decodes every structure of the packet whose start lies in it.
func decodePacketLayers(raw []byte) []PacketLayer {
	b := &fieldBuilder{raw: raw}
	t := len(raw) - tsPayloadSize
	var layers []PacketLayer
	if t == tpExtraHeaderSize {
		var fields []PacketField
		b.add(&fields, "copy_permission_indicator", 0, 1, "%d", raw[0]>>6)
		ats := uint32(raw[0]&0x3F)<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
		b.add(&fields, "arrival_time_stamp", 0, 4, "%d", ats)
		layers = append(layers, PacketLayer{Name: "m2ts_header", Fields: fields})
	}

	var header []PacketField
	afc := (raw[t+3] >> 4) & 0x3
	pusi := raw[t+1]&0x40 != 0
	b.add(&header, "sync_byte", t, 1, "0x%02x", raw[t])
	b.add(&header, "transport_error_indicator", t+1, 1, "%d", raw[t+1]>>7)
	b.add(&header, "payload_unit_start_indicator", t+1, 1, "%d", (raw[t+1]>>6)&1)
	b.add(&header, "transport_priority", t+1, 1, "%d", (raw[t+1]>>5)&1)
	b.add(&header, "PID", t+1, 2, "0x%04x", b.pid(t+1))
	b.add(&header, "transport_scrambling_control", t+3, 1, "%d", raw[t+3]>>6)
	b.add(&header, "adaptation_field_control", t+3, 1, "%d (%s)", afc, adaptationFieldControlName(afc))
	b.add(&header, "continuity_counter", t+3, 1, "%d", raw[t+3]&0x0F)
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
		b.add(&fields, name, at+1, 1, "%d", (flags>>(7-i))&1)
	}
	pos, end := at+2, at+1+length
	clockField := func(name string) {
		if pos+6 <= end {
			base := uint64(b.raw[pos])<<25 | uint64(b.raw[pos+1])<<17 | uint64(b.raw[pos+2])<<9 | uint64(b.raw[pos+3])<<1 | uint64(b.raw[pos+4])>>7
			ext := uint64(b.raw[pos+4]&0x01)<<8 | uint64(b.raw[pos+5])
			clock := base*300 + ext
			b.add(&fields, name, pos, 6, "%d (%.6f s)", clock, float64(clock)/pcrClockHz)
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

func decodeTimestamp(b []byte) uint64 {
	return uint64(b[0]>>1&0x07)<<30 | uint64(b[1])<<22 | uint64(b[2]>>1)<<15 | uint64(b[3])<<7 | uint64(b[4]>>1)
}

func decodePesHeader(b *fieldBuilder, p int) PacketLayer {
	var fields []PacketField
	b.add(&fields, "packet_start_code_prefix", p, 3, "0x000001")
	if b.add(&fields, "stream_id", p+3, 1, "0x%02x", b.byteAt(p+3)) &&
		b.add(&fields, "PES_packet_length", p+4, 2, "%d", b.u16At(p+4)) &&
		streamHasPesHeader(b.raw[p+3]) &&
		b.add(&fields, "PES_scrambling_control", p+6, 1, "%d", (b.byteAt(p+6)>>4)&0x3) &&
		b.add(&fields, "PTS_DTS_flags", p+7, 1, "%d", b.byteAt(p+7)>>6) &&
		b.add(&fields, "PES_header_data_length", p+8, 1, "%d", b.byteAt(p+8)) {
		flags := b.raw[p+7] >> 6
		if flags&0x2 != 0 && p+14 <= len(b.raw) {
			pts := decodeTimestamp(b.raw[p+9 : p+14])
			b.add(&fields, "PTS", p+9, 5, "%d (%.6f s)", pts, float64(pts)/90000)
		} else if flags&0x2 != 0 {
			b.truncated = true
		}
		if flags == 0x3 && p+19 <= len(b.raw) {
			dts := decodeTimestamp(b.raw[p+14 : p+19])
			b.add(&fields, "DTS", p+14, 5, "%d (%.6f s)", dts, float64(dts)/90000)
		} else if flags == 0x3 {
			b.truncated = true
		}
	}
	return PacketLayer{Name: "pes_header", Fields: fields, Truncated: b.truncated}
}

func decodePsiSection(b *fieldBuilder, p int) PacketLayer {
	var fields []PacketField
	pointer := int(b.raw[p])
	b.add(&fields, "pointer_field", p, 1, "%d", pointer)
	s := p + 1 + pointer
	if !b.add(&fields, "table_id", s, 1, "0x%02x", b.byteAt(s)) {
		return PacketLayer{Name: "psi_section", Fields: fields, Truncated: true}
	}
	tableID := b.raw[s]
	name := map[byte]string{0x00: "pat", 0x02: "pmt"}[tableID]
	if name == "" {
		name = "psi_section"
	}
	if !b.add(&fields, "section_syntax_indicator", s+1, 1, "%d", b.byteAt(s+1)>>7) ||
		!b.add(&fields, "section_length", s+1, 2, "%d", b.u16At(s+1)&0x0FFF) {
		return PacketLayer{Name: name, Fields: fields, Truncated: true}
	}
	sectionEnd := s + 3 + int(b.u16(s+1)&0x0FFF)
	crcAt := sectionEnd - 4
	var rows [][]PacketField
	if tableID == 0x00 || tableID == 0x02 {
		idName := "transport_stream_id"
		if tableID == 0x02 {
			idName = "program_number"
		}
		if b.add(&fields, idName, s+3, 2, "%d", b.u16At(s+3)) &&
			b.add(&fields, "version_number", s+5, 1, "%d", (b.byteAt(s+5)>>1)&0x1F) &&
			b.add(&fields, "current_next_indicator", s+5, 1, "%d", b.byteAt(s+5)&1) &&
			b.add(&fields, "section_number", s+6, 1, "%d", b.byteAt(s+6)) &&
			b.add(&fields, "last_section_number", s+7, 1, "%d", b.byteAt(s+7)) {
			if tableID == 0x00 {
				rows = patRows(b, s+8, crcAt)
			} else {
				rows = pmtRows(b, s+8, crcAt, &fields)
			}
		}
	}
	if crcAt+4 <= len(b.raw) && crcAt >= s+3 {
		crc := uint32(b.u16(crcAt))<<16 | uint32(b.u16(crcAt+2))
		b.add(&fields, "CRC_32", crcAt, 4, "0x%08x", crc)
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
		pidName := "program_map_PID"
		if b.u16(i) == 0 {
			pidName = "network_PID"
		}
		var row []PacketField
		b.add(&row, "program_number", i, 2, "%d", b.u16(i))
		b.add(&row, pidName, i+2, 2, "0x%04x", b.pid(i+2))
		rows = append(rows, row)
	}
	return rows
}

func pmtRows(b *fieldBuilder, at, end int, fields *[]PacketField) [][]PacketField {
	if !b.add(fields, "PCR_PID", at, 2, "0x%04x", b.u16At(at)&0x1FFF) ||
		!b.add(fields, "program_info_length", at+2, 2, "%d", b.u16At(at+2)&0x0FFF) {
		return nil
	}
	var rows [][]PacketField
	for i := at + 4 + int(b.u16(at+2)&0x0FFF); i+5 <= end; {
		var row []PacketField
		if i+5 > len(b.raw) {
			b.truncated = true
			break
		}
		streamType := b.raw[i]
		esInfoLength := int(b.u16(i+3) & 0x0FFF)
		b.add(&row, "stream_type", i, 1, "0x%02x (%s)", streamType, StreamTypeString(streamType))
		b.add(&row, "elementary_PID", i+1, 2, "0x%04x", b.pid(i+1))
		b.add(&row, "ES_info_length", i+3, 2, "%d", esInfoLength)
		rows = append(rows, row)
		i += 5 + esInfoLength
	}
	return rows
}

// findUnit locates the PES packet or PSI section that contains the packet at
// offset, scanning packets of the same PID within [rangeStart, rangeEnd).
func findUnit(f *packetFile, tp *TsPacket, offset, rangeStart, rangeEnd int64) *PacketUnit {
	pid := tp.Pid()
	if pid == nullPidValue {
		return &PacketUnit{Kind: "none"}
	}
	ps := int64(f.packetSize)
	same := func(at int64) (*TsPacket, []byte, bool) {
		raw, err := f.readPacket(at)
		if err != nil {
			return nil, nil, false
		}
		other, err := f.parsePacket(raw, at)
		if err != nil || other.Pid() != pid {
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
		unit.PTSMs = optionalFloat(float64(decodeTimestamp(header[9:14]))/90, true)
	}
	if flags == 0x3 && len(header) >= 19 {
		unit.DTSMs = optionalFloat(float64(decodeTimestamp(header[14:19]))/90, true)
	}
}
