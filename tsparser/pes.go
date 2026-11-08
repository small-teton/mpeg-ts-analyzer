package tsparser

import (
	"errors"
	"fmt"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

// maxPesHeaderSize is the largest PES header that ISO/IEC 13818-1 allows: the
// 6 bytes of packet_start_code_prefix, stream_id and PES_packet_length, the 3
// bytes of flags and PES_header_data_length, and at most 255 bytes of optional
// fields and stuffing that PES_header_data_length can announce. A Pes retains
// at most this many leading bytes of each PES packet, however long the packet
// is or however far away the next payload_unit_start_indicator lies.
//
// Parse reads at most 32 bytes (the 9 fixed bytes plus PTS/DTS, ESCR, ES_rate,
// DSM trick mode, additional_copy_info and previous_PES_packet_CRC), so the
// limit never changes what it decodes; it also keeps the complete header
// available for header-level checks, independent of the elementary-stream
// payload size.
const maxPesHeaderSize = 6 + 3 + 255

// Pes Packetized Elementary Stream.
type Pes struct {
	pid               uint16
	continuityCounter uint8
	// buf holds the first bytes of the PES packet, at most maxPesHeaderSize.
	buf []byte
	// received counts every payload byte appended to this PES packet,
	// including the bytes beyond maxPesHeaderSize that are not retained.
	received int64
	pos      int64
	// prevAnchor and nextAnchor are the PCRs before and after the start of
	// the PES packet, which place it on the program's PCR timeline.
	prevAnchor pcrAnchor
	nextAnchor pcrAnchor

	packetStartCodePrefix            uint32
	streamID                         uint8
	pesPacketLength                  uint16
	pesScramblingControl             uint8
	pesPriority                      uint8
	dataAlignmentIndicator           uint8
	copyright                        uint8
	originalOrCopy                   uint8
	ptsDtsFlags                      uint8
	escrFlag                         uint8
	esRateFlag                       uint8
	dsmTrickModeFlag                 uint8
	additionalCopyInfoFlag           uint8
	pesCrcFlag                       uint8
	pesExtensionFlag                 uint8
	pesHeaderDataLength              uint8
	pts                              uint64
	dts                              uint64
	escr                             uint32
	escrBase                         uint64
	escrExtension                    uint16
	esRate                           uint32
	trickModeControl                 uint8
	fieldID                          uint8
	intraSliceRefresh                uint8
	frequencyTruncation              uint8
	repCntrl                         uint8
	additionalCopyInfo               uint8
	previousPesPacketCrc             uint16
	pesPrivateDataFlag               uint8
	packHeaderFieldFlag              uint8
	programPacketSequenceCounterFlag uint8
	pStdBufferFlag                   uint8
	pesExtensionFlag2                uint8
	programPacketSequenceCounter     uint8
	mpeg1Mpeg2Identifier             uint8
	originalStuffLength              uint8
	pStdBufferScale                  uint8
	pStdBufferSize                   uint16
	pesExtensionFieldLength          uint8
}

// NewPes create new PES instance
func NewPes() *Pes {
	pes := new(Pes)
	pes.buf = make([]byte, 0, maxPesHeaderSize)
	return pes
}

// initialize starts a PES packet of pid at the packet at offset pos, after
// the PCR prev (invalid when no PCR is known).
func (p *Pes) initialize(pid uint16, pos int64, prev pcrAnchor) {
	p.pid = pid
	p.continuityCounter = 0
	p.buf = p.buf[0:0]
	p.received = 0
	p.pos = pos
	p.prevAnchor = prev
	p.nextAnchor = pcrAnchor{}

	p.packetStartCodePrefix = 0
	p.streamID = 0
	p.pesPacketLength = 0
	p.pesScramblingControl = 0
	p.pesPriority = 0
	p.dataAlignmentIndicator = 0
	p.copyright = 0
	p.originalOrCopy = 0
	p.ptsDtsFlags = 0
	p.escrFlag = 0
	p.esRateFlag = 0
	p.dsmTrickModeFlag = 0
	p.additionalCopyInfoFlag = 0
	p.pesCrcFlag = 0
	p.pesExtensionFlag = 0
	p.pesHeaderDataLength = 0
	p.pts = 0
	p.dts = 0
	p.escr = 0
	p.escrBase = 0
	p.escrExtension = 0
	p.esRate = 0
	p.trickModeControl = 0
	p.fieldID = 0
	p.intraSliceRefresh = 0
	p.frequencyTruncation = 0
	p.repCntrl = 0
	p.additionalCopyInfo = 0
	p.previousPesPacketCrc = 0
	p.pesPrivateDataFlag = 0
	p.packHeaderFieldFlag = 0
	p.programPacketSequenceCounterFlag = 0
	p.pStdBufferFlag = 0
	p.pesExtensionFlag2 = 0
	p.programPacketSequenceCounter = 0
	p.mpeg1Mpeg2Identifier = 0
	p.originalStuffLength = 0
	p.pStdBufferScale = 0
	p.pStdBufferSize = 0
	p.pesExtensionFieldLength = 0
}

// ContinuityCounter return current continuity_counter of TsPacket.
func (p *Pes) ContinuityCounter() uint8 { return p.continuityCounter }

// SetContinuityCounter set current continuity_counter of TsPacket.
func (p *Pes) SetContinuityCounter(continuityCounter uint8) { p.continuityCounter = continuityCounter }

// Append adds the next TS payload of the PES packet. Only the first
// maxPesHeaderSize bytes are retained, so the memory held per active PES stays
// bounded even when a PID never starts another PES packet; the length of the
// remaining bytes is still counted.
func (p *Pes) Append(buf []byte) {
	p.received += int64(len(buf))
	if room := maxPesHeaderSize - len(p.buf); room > 0 {
		p.buf = append(p.buf, buf[:min(room, len(buf))]...)
	}
}

// PES header checks (ISO/IEC 13818-1, clause 2.4.3.6 and 2.4.3.7).
//
// Parse decodes the header from the retained bytes only (at most
// maxPesHeaderSize, which holds every possible header) and rejects a header
// whose fields cannot be trusted, classified as an analysis.PESHeaderProblem:
//
//   - PESHeaderBadStartCode: packet_start_code_prefix is not 0x000001, or the
//     stream_id is below 0xBC (those values are not PES stream_ids).
//   - PESHeaderBadFlags: the optional header does not start with '10', or
//     PTS_DTS_flags is the forbidden '01'.
//   - PESHeaderInconsistentLength: PES_header_data_length is smaller than the
//     fields its flags announce (PTS 5 bytes, PTS+DTS 10, ESCR 6, ES_rate 3,
//     DSM trick mode 1, additional_copy_info 1, previous_PES_CRC 2, and the
//     PES_extension with the fields its own flags announce), or a non-zero PES_packet_length is smaller than the
//     3 flag bytes plus PES_header_data_length. A PES_packet_length of 0
//     ("unbounded") is accepted for every stream.
//   - PESHeaderBadMarkerBits: a PTS or DTS field does not start with its
//     4-bit prefix ('0010' for a PTS alone, '0011' and '0001' for PTS and
//     DTS) or one of its three marker_bits is 0. Its value would not be
//     trustworthy.
//   - PESHeaderTruncated: the bytes end before the fixed header or before
//     the PES_header_data_length bytes of optional fields and stuffing.
//
// Streams that carry no optional header (program_stream_map, padding,
// private_stream_2, ECM, EMM, DSMCC, H.222.1 type E and the directory) end
// after PES_packet_length. Elementary-stream payload is never read.

// pesHeaderError is a PES header that cannot be trusted.
type pesHeaderError struct {
	problem analysis.PESHeaderProblem
	message string
}

func (e *pesHeaderError) Error() string { return e.message }

func pesError(problem analysis.PESHeaderProblem, format string, args ...any) error {
	return &pesHeaderError{problem: problem, message: fmt.Sprintf(format, args...)}
}

// pesProblem returns the header problem of a Parse error.
func pesProblem(err error) analysis.PESHeaderProblem {
	var headerErr *pesHeaderError
	if errors.As(err, &headerErr) {
		return headerErr.problem
	}
	return analysis.PESHeaderOK
}

// Optional field sizes in bytes, announced by the flags of the PES header.
const (
	pesPtsSize       = 5
	pesEscrSize      = 6
	pesEsRateSize    = 3
	pesTrickModeSize = 1
	pesCopyInfoSize  = 1
	pesCrcSize       = 2
	pesExtensionMin  = 1
)

// Parse decodes the PES header and checks it; see the comment above. It
// returns a *pesHeaderError when the header cannot be trusted.
func (p *Pes) Parse() error {
	b := p.buf
	if len(b) >= 3 {
		p.packetStartCodePrefix = uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
		if p.packetStartCodePrefix != 0x000001 {
			return pesError(analysis.PESHeaderBadStartCode, "invalid pes packet_start_code_prefix: 0x%06x", p.packetStartCodePrefix)
		}
	}
	if len(b) >= 4 {
		p.streamID = b[3]
		if p.streamID < 0xBC {
			return pesError(analysis.PESHeaderBadStartCode, "invalid pes stream_id: 0x%02x", p.streamID)
		}
	}
	if len(b) < 6 {
		return pesError(analysis.PESHeaderTruncated, "pes header truncated: %d bytes before PES_packet_length ends", len(b))
	}
	p.pesPacketLength = uint16(b[4])<<8 | uint16(b[5])
	switch p.streamID {
	case 0xBC, 0xBE, 0xBF, 0xF0, 0xF1, 0xFF, 0xF2, 0xF8:
		// These streams carry no optional header.
		return nil
	}
	if len(b) < 9 {
		return pesError(analysis.PESHeaderTruncated, "pes header truncated: %d bytes of the 9-byte fixed header", len(b))
	}
	if b[6]>>6 != 0x2 {
		return pesError(analysis.PESHeaderBadFlags, "pes optional header starts with '%02b', not '10'", b[6]>>6)
	}
	p.pesScramblingControl = b[6] >> 4 & 0x3
	p.pesPriority = b[6] >> 3 & 0x1
	p.dataAlignmentIndicator = b[6] >> 2 & 0x1
	p.copyright = b[6] >> 1 & 0x1
	p.originalOrCopy = b[6] & 0x1
	p.ptsDtsFlags = b[7] >> 6
	p.escrFlag = b[7] >> 5 & 0x1
	p.esRateFlag = b[7] >> 4 & 0x1
	p.dsmTrickModeFlag = b[7] >> 3 & 0x1
	p.additionalCopyInfoFlag = b[7] >> 2 & 0x1
	p.pesCrcFlag = b[7] >> 1 & 0x1
	p.pesExtensionFlag = b[7] & 0x1
	p.pesHeaderDataLength = b[8]
	if p.ptsDtsFlags == 1 {
		return pesError(analysis.PESHeaderBadFlags, "forbidden pes PTS_DTS_flags '01'")
	}
	required := 0
	switch p.ptsDtsFlags {
	case 2:
		required = pesPtsSize
	case 3:
		required = 2 * pesPtsSize
	}
	required += int(p.escrFlag)*pesEscrSize + int(p.esRateFlag)*pesEsRateSize + int(p.dsmTrickModeFlag)*pesTrickModeSize +
		int(p.additionalCopyInfoFlag)*pesCopyInfoSize + int(p.pesCrcFlag)*pesCrcSize + int(p.pesExtensionFlag)*pesExtensionMin
	if int(p.pesHeaderDataLength) < required {
		return pesError(analysis.PESHeaderInconsistentLength, "pes PES_header_data_length %d is smaller than the %d bytes its flags announce", p.pesHeaderDataLength, required)
	}
	if p.pesPacketLength != 0 && int(p.pesPacketLength) < 3+int(p.pesHeaderDataLength) {
		return pesError(analysis.PESHeaderInconsistentLength, "pes PES_packet_length %d is smaller than its header (%d bytes)", p.pesPacketLength, 3+int(p.pesHeaderDataLength))
	}
	if len(b) < 9+int(p.pesHeaderDataLength) {
		return pesError(analysis.PESHeaderTruncated, "pes header truncated: %d of %d bytes", len(b), 9+int(p.pesHeaderDataLength))
	}
	if p.pesExtensionFlag == 1 {
		extension := b[9+required-pesExtensionMin : 9+int(p.pesHeaderDataLength)]
		if need := pesExtensionSize(extension); need > len(extension) {
			return pesError(analysis.PESHeaderInconsistentLength, "pes PES_extension needs %d bytes, PES_header_data_length leaves %d", need, len(extension))
		}
	}

	o := b[9:]
	var ok bool
	switch p.ptsDtsFlags {
	case 2:
		if p.pts, ok = pesTimestamp(o, 0x2); !ok {
			return pesError(analysis.PESHeaderBadMarkerBits, "invalid pes PTS prefix or marker_bit")
		}
		o = o[pesPtsSize:]
	case 3:
		if p.pts, ok = pesTimestamp(o, 0x3); !ok {
			return pesError(analysis.PESHeaderBadMarkerBits, "invalid pes PTS prefix or marker_bit")
		}
		if p.dts, ok = pesTimestamp(o[pesPtsSize:], 0x1); !ok {
			return pesError(analysis.PESHeaderBadMarkerBits, "invalid pes DTS prefix or marker_bit")
		}
		o = o[2*pesPtsSize:]
	}
	if p.escrFlag == 1 {
		// reserved(2) base[32..30](3) marker base[29..15](15) marker
		// base[14..0](15) marker extension(9) marker
		v := uint64(o[0])<<40 | uint64(o[1])<<32 | uint64(o[2])<<24 | uint64(o[3])<<16 | uint64(o[4])<<8 | uint64(o[5])
		p.escrBase = (v>>43&0x7)<<30 | (v>>27&0x7FFF)<<15 | v>>11&0x7FFF
		p.escrExtension = uint16(v >> 1 & 0x1FF)
		o = o[pesEscrSize:]
	}
	if p.esRateFlag == 1 {
		// marker ES_rate(22) marker
		p.esRate = (uint32(o[0])<<16 | uint32(o[1])<<8 | uint32(o[2])) >> 1 & 0x3FFFFF
		o = o[pesEsRateSize:]
	}
	if p.dsmTrickModeFlag == 1 {
		p.trickModeControl = o[0] >> 5
		switch p.trickModeControl {
		case 0x00, 0x03: // fast_forward, freeze_frame
			p.fieldID = o[0] >> 3 & 0x3
			p.intraSliceRefresh = o[0] >> 2 & 0x1
			p.frequencyTruncation = o[0] & 0x3
		case 0x01: // slow_motion, slow_reverse
			p.repCntrl = o[0] & 0x1F
		}
		o = o[pesTrickModeSize:]
	}
	if p.additionalCopyInfoFlag == 1 {
		p.additionalCopyInfo = o[0] & 0x7F // after a marker_bit
		o = o[pesCopyInfoSize:]
	}
	if p.pesCrcFlag == 1 {
		p.previousPesPacketCrc = uint16(o[0])<<8 | uint16(o[1])
	}
	return nil
}

// pesExtensionSize returns the number of bytes the PES_extension starting at
// b announces: its flag byte, 16 bytes of PES_private_data, the
// length-prefixed pack_header, 2 bytes each of program_packet_sequence_counter
// and P-STD buffer, and the length-prefixed PES_extension_2 data. A length
// byte beyond b counts as needed, so the result exceeds len(b) whenever the
// announced fields do not fit.
func pesExtensionSize(b []byte) int {
	flags, n := b[0], 1
	if flags&0x80 != 0 { // PES_private_data_flag
		n += 16
	}
	if flags&0x40 != 0 { // pack_header_field_flag
		if n >= len(b) {
			return n + 1
		}
		n += 1 + int(b[n])
	}
	if flags&0x20 != 0 { // program_packet_sequence_counter_flag
		n += 2
	}
	if flags&0x10 != 0 { // P-STD_buffer_flag
		n += 2
	}
	if flags&0x01 != 0 { // PES_extension_flag_2
		if n >= len(b) {
			return n + 1
		}
		n += 1 + int(b[n]&0x7F)
	}
	return n
}

// pesTimestamp decodes a 5-byte PTS or DTS field: a 4-bit prefix, then the
// 33-bit value in parts of 3, 15 and 15 bits, each followed by a marker_bit.
// ok is false when the prefix differs or a marker_bit is 0.
func pesTimestamp(b []byte, prefix byte) (uint64, bool) {
	value := uint64(b[0]>>1&0x7)<<30 | uint64(b[1])<<22 | uint64(b[2]>>1)<<15 | uint64(b[3])<<7 | uint64(b[4]>>1)
	return value, b[0]>>4 == prefix && b[0]&b[2]&b[4]&0x1 == 1
}

// DumpTimestamp dump PTS and DTS
func (p *Pes) DumpTimestamp() float64 {
	switch p.ptsDtsFlags {
	case 2:
		return p.dumpTimestamp("PTS", p.pts)
	case 3:
		// ptsDtsFlags == 3 carries both PTS and DTS; the end-to-end delay is
		// measured against the DTS (when the access unit is decoded).
		return p.dumpTimestamp("DTS", p.dts)
	}
	return 0
}

// PCR-to-PTS/DTS delay of a PES packet: its lead, analysis.PES.LeadMs, which
// the mux lead report computes for the same PES packet (analysis.PES.Lead,
// the same lead from the arrival truncated to whole ticks). The PES packet is
// placed on the program's PCR timeline by arrivalAt between prevAnchor and
// nextAnchor, so a bracket across a PCR wraparound is interpolated, a bracket
// across a PCR discontinuity or a PCR_PID change is not measured, and the
// lead is read modulo the 2^33 period of the PTS/DTS and the PCR as the
// signed shortest difference.

// timing returns the PES packet's timestamps and its time on the PCR
// timeline as a PES observation.
func (p *Pes) timing(arrival analysis.ClockTime) analysis.PES {
	return analysis.PES{
		HasPTS: p.ptsDtsFlags >= 2, HasDTS: p.ptsDtsFlags == 3, PTS: p.pts, DTS: p.dts,
		Arrival: arrival,
	}
}

// hasPcrReference reports whether a PCR before the PES packet is known and
// the first PCR after its start, if one has arrived, continues the same
// timing segment.
func (p *Pes) hasPcrReference() bool {
	return p.prevAnchor.valid && (!p.nextAnchor.valid || p.nextAnchor.segment == p.prevAnchor.segment)
}

// TimestampDelay returns the PCR-to-PTS/DTS delay in milliseconds when the PES
// has a timestamp and a preceding PCR reference. DTS is used when both are
// present because it represents the decode timeline. It is the
// BracketedTimestampDelay when the PCR after the start of the PES packet has
// arrived; before that (e.g. the final PES before EOF) the delay is taken
// against the previous PCR alone, since interpolating towards an unknown PCR
// would give a wildly wrong value.
func (p *Pes) TimestampDelay() (float64, bool) {
	if delay, ok := p.BracketedTimestampDelay(); ok {
		return delay, true
	}
	if p.nextAnchor.valid || !p.hasPcrReference() {
		return 0, false
	}
	return p.timing(analysis.ClockTime{Valid: true, Ticks: p.prevAnchor.pcr, Ms: pcrToMs(float64(p.prevAnchor.pcr))}).LeadMs()
}

// BracketedTimestampDelay returns a delay only when PCR observations of one
// timing segment on both sides of the PES position are available. Compliance
// evaluation uses this stricter form because treating the previous PCR value
// as if it occurred at a later PES position overstates the delay near the end
// of a stream.
func (p *Pes) BracketedTimestampDelay() (float64, bool) {
	return p.timing(arrivalAt(p.prevAnchor, p.nextAnchor, p.pos)).LeadMs()
}

// dumpTimestamp prints one PTS/DTS line and returns the PCR-to-timestamp delay
// (the end-to-end buffering delay) in milliseconds.
func (p *Pes) dumpTimestamp(label string, stamp uint64) float64 {
	stampMs := float64(stamp) / 90
	pcrDelay, ok := p.TimestampDelay()
	if !ok {
		fmt.Printf("0x%08x %s: 0x%08x[%012fms] (pid:0x%02x)\n", p.pos, label, stamp, stampMs, p.pid)
		return 0
	}
	fmt.Printf("0x%08x %s: 0x%08x[%012fms] (pid:0x%02x) (delay:%fms)\n", p.pos, label, stamp, stampMs, p.pid, pcrDelay)
	return pcrDelay
}

// pcrToMs converts a 27 MHz PCR value or interval to milliseconds.
func pcrToMs(pcr float64) float64 { return pcr / 27000 }

// Dump PES header detail.
// pesColonColumn is the column where the PES header dump aligns its colons.
const pesColonColumn = 48

// pesField prints one aligned "PES : <label> : <value>" line.
func pesField(label, format string, args ...interface{}) {
	dumpField("PES : ", pesColonColumn, label, format, args...)
}

func (p *Pes) DumpHeader() {
	fmt.Printf("\n===========================================\n")
	fmt.Printf(" PES")
	fmt.Printf("\n===========================================\n")
	pesField("packet_start_code_prefix", "%d", p.packetStartCodePrefix)
	pesField("stream_id", "%d", p.streamID)
	pesField("pes_packet_length", "%d", p.pesPacketLength)
	pesField("pes_scrambling_control", "%d", p.pesScramblingControl)
	pesField("pes_priority", "%d", p.pesPriority)
	pesField("data_alignment_indicator", "%d", p.dataAlignmentIndicator)
	pesField("copyright", "%d", p.copyright)
	pesField("original_or_copy", "%d", p.originalOrCopy)
	pesField("pts_dts_flags", "%d", p.ptsDtsFlags)
	pesField("escr_flag", "%d", p.escrFlag)
	pesField("es_rate_flag", "%d", p.esRateFlag)
	pesField("dsm_trick_mode_flag", "%d", p.dsmTrickModeFlag)
	pesField("additional_copy_info_flag", "%d", p.additionalCopyInfoFlag)
	pesField("pes_crc_flag", "%d", p.pesCrcFlag)
	pesField("pes_extension_flag", "%d", p.pesExtensionFlag)
	pesField("pes_header_data_length", "%d", p.pesHeaderDataLength)
	pesField("pts", "%d", p.pts)
	pesField("dts", "%d", p.dts)
	pesField("escr", "%d", p.escr)
	pesField("escr_base", "%d", p.escrBase)
	pesField("escr_extension", "%d", p.escrExtension)
	pesField("es_rate", "%d", p.esRate)
	pesField("trick_mode_control", "%d", p.trickModeControl)
	pesField("field_id", "%d", p.fieldID)
	pesField("intra_slice_refresh", "%d", p.intraSliceRefresh)
	pesField("frequency_truncation", "%d", p.frequencyTruncation)
	pesField("rep_cntrl", "%d", p.repCntrl)
	pesField("additional_copy_info", "%d", p.additionalCopyInfo)
	pesField("previous_pes_packet_crc", "%d", p.previousPesPacketCrc)
	pesField("pes_private_data_flag", "%d", p.pesPrivateDataFlag)
	pesField("pack_header_field_flag", "%d", p.packHeaderFieldFlag)
	pesField("program_packet_sequence_counter_flag", "%d", p.programPacketSequenceCounterFlag)
	pesField("p-std_buffer_flag", "%d", p.pStdBufferFlag)
	pesField("pes_extension_flag2", "%d", p.pesExtensionFlag2)
	pesField("program_packet_sequence_counter", "%d", p.programPacketSequenceCounter)
	pesField("mpeg1_mpeg2_identifier", "%d", p.mpeg1Mpeg2Identifier)
	pesField("original_stuff_length", "%d", p.originalStuffLength)
	pesField("p-std_buffer_scale", "%d", p.pStdBufferScale)
	pesField("p-std_buffer_size", "%d", p.pStdBufferSize)
	pesField("pes_extension_field_length", "%d", p.pesExtensionFieldLength)
}
