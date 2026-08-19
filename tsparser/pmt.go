package tsparser

import (
	"errors"
	"fmt"
)

// Pmt Program Map Table
type Pmt struct {
	// startFlag         bool
	continuityCounter uint8
	buf               []byte
	newBitReader      func() bitReader

	tableID                uint8
	sectionSyntaxIndicator uint8
	sectionLength          uint16
	programNumber          uint16
	versionNumber          uint8
	currentNextIndicator   uint8
	sectionNumber          uint8
	lastSectionNumber      uint8
	pcrPid                 uint16
	programInfoLength      uint16
	descriptors            []Descriptor // program-level (whole-program) descriptors
	programInfos           []ProgramInfo
	crc32                  uint32
}

// ProgramInfo Program info
type ProgramInfo struct {
	streamType    uint8
	elementaryPid uint16
	esInfoLength  uint16
	descriptors   []Descriptor
}

// Descriptors return the ES info descriptors of this program info.
func (pi ProgramInfo) Descriptors() []Descriptor { return pi.descriptors }

// NewPmt create new PMT instance
func NewPmt() *Pmt {
	return new(Pmt)
}

// ContinuityCounter return current continuity_counter of TsPacket.
func (p *Pmt) ContinuityCounter() uint8 { return p.continuityCounter }

// SetContinuityCounter set current continuity_counter of TsPacket.
func (p *Pmt) SetContinuityCounter(continuityCounter uint8) { p.continuityCounter = continuityCounter }

// PcrPid return PCR_PID.
func (p *Pmt) PcrPid() uint16 { return p.pcrPid }

// ProgramInfos return ProgramInfos.
func (p *Pmt) ProgramInfos() []ProgramInfo { return p.programInfos }

// Append append ts payload data for buffer.
func (p *Pmt) Append(buf []byte) {
	p.buf = append(p.buf, buf...)
}

// Parse PMT data.
func (p *Pmt) Parse() error {
	var bb bitReader
	if p.newBitReader != nil {
		bb = p.newBitReader()
	} else {
		bb = newDefaultBitReader()
	}
	bb.Set(p.buf)

	var err error
	if p.tableID, err = bb.ReadUint8(8); err != nil {
		return fmt.Errorf("failed to read pmt table_id: %w", err)
	}
	if p.tableID != 0x02 {
		return fmt.Errorf("invalid pmt table_id: 0x%02x", p.tableID)
	}
	if p.sectionSyntaxIndicator, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to read pmt section_syntax_indicator: %w", err)
	}
	if err := bb.Skip(1); err != nil {
		return fmt.Errorf("failed to skip in pmt: (): %w", err)
	} // ()
	if err := bb.Skip(2); err != nil {
		return fmt.Errorf("failed to skip in pmt: reserved: %w", err)
	} // reserved
	if p.sectionLength, err = bb.ReadUint16(12); err != nil {
		return fmt.Errorf("failed to read pmt section_length: %w", err)
	}
	if p.programNumber, err = bb.ReadUint16(16); err != nil {
		return fmt.Errorf("failed to read pmt program_number: %w", err)
	}
	if err := bb.Skip(2); err != nil {
		return fmt.Errorf("failed to skip in pmt: reserved: %w", err)
	} // reserved
	if p.versionNumber, err = bb.ReadUint8(5); err != nil {
		return fmt.Errorf("failed to read pmt version_number: %w", err)
	}
	if p.currentNextIndicator, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to read pmt current_next_indicator: %w", err)
	}
	if p.sectionNumber, err = bb.ReadUint8(8); err != nil {
		return fmt.Errorf("failed to read pmt section_number: %w", err)
	}
	if p.lastSectionNumber, err = bb.ReadUint8(8); err != nil {
		return fmt.Errorf("failed to read pmt last_section_number: %w", err)
	}
	if err := bb.Skip(3); err != nil {
		return fmt.Errorf("failed to skip in pmt: reserved: %w", err)
	} // reserved
	if p.pcrPid, err = bb.ReadUint16(13); err != nil {
		return fmt.Errorf("failed to read pmt pcr_pid: %w", err)
	}
	if err := bb.Skip(4); err != nil {
		return fmt.Errorf("failed to skip in pmt reserved: %w", err)
	} // reserved
	if p.programInfoLength, err = bb.ReadUint16(12); err != nil {
		return fmt.Errorf("failed to read pmt program_info_length: %w", err)
	}
	if p.descriptors, err = parseDescriptors(bb, p.programInfoLength); err != nil {
		return fmt.Errorf("failed to parse pmt program descriptors: %w", err)
	}
	remainLength := int32(p.sectionLength) - 9 - 4 - int32(p.programInfoLength)
	for remainLength > 0 {
		var info ProgramInfo
		if info.streamType, err = bb.ReadUint8(8); err != nil {
			return fmt.Errorf("failed to read pmt program info: stream_type: %w", err)
		}
		if err := bb.Skip(3); err != nil {
			return fmt.Errorf("failed to skip in pmt program info: reserved: %w", err)
		} // reserved
		if info.elementaryPid, err = bb.ReadUint16(13); err != nil {
			return fmt.Errorf("failed to read pmt program info: elementary_pid: %w", err)
		}
		if err := bb.Skip(4); err != nil {
			return fmt.Errorf("failed to skip in pmt program info: reserved: %w", err)
		} // reserved
		if info.esInfoLength, err = bb.ReadUint16(12); err != nil {
			return fmt.Errorf("failed to read pmt program info: es_info_length: %w", err)
		}
		if info.descriptors, err = parseDescriptors(bb, info.esInfoLength); err != nil {
			return fmt.Errorf("failed to parse pmt program info descriptors: %w", err)
		}
		remainLength = remainLength - 5 - int32(info.esInfoLength)
		p.programInfos = append(p.programInfos, info)
	}
	if p.crc32, err = bb.ReadUint32(32); err != nil {
		return fmt.Errorf("failed to read pmt crc32: %w", err)
	}

	if len(p.buf) >= int(3+p.sectionLength-4) && p.crc32 != crc32(p.buf[0:3+p.sectionLength-4]) {
		return errors.New("PMT CRC32 is invalid")
	}

	return nil
}

// pmtColonColumn is the column where every "key : value" colon in the PMT dump
// lines up. It is wide enough to clear the longest label,
// "PMT : Program Info : elementary_PID" (35 columns), so header fields, the
// per-stream Program Info lines and the descriptor detail lines (see descField)
// all share one column.
const pmtColonColumn = 40

// pmtField prints one aligned "PMT : <label> : <value>" header/stream line.
func pmtField(label, format string, args ...interface{}) {
	dumpField("PMT : ", pmtColonColumn, label, format, args...)
}

// StreamTypeString returns the human-readable name of an ISO/IEC 13818-1
// stream_type value. It is shared by the PMT dump and the bitrate summary so
// both label elementary streams identically.
func StreamTypeString(streamType uint8) string {
	switch streamType {
	case 0x00:
		return "reserved"
	case 0x01:
		return "11172 video"
	case 0x02:
		return "13818-2 video or 11172-2 constrained parameter video stream"
	case 0x03:
		return "11172 audio"
	case 0x04:
		return "13818-3 audio"
	case 0x05:
		return "13818-1 private sections"
	case 0x06:
		return "13818-1 PES packet containing private data"
	case 0x07:
		return "13522 MHEG"
	case 0x08:
		return "13818-1 annex A DSM-CC"
	case 0x09:
		return "H.222.1"
	case 0x0A:
		return "13818-6 type A"
	case 0x0B:
		return "13818-6 type B"
	case 0x0C:
		return "13818-6 type C"
	case 0x0D:
		return "13818-6 type D"
	case 0x0E:
		return "13818-1 auxiliary"
	case 0x0F:
		return "13818-7 audio with ADTS transport syntax"
	case 0x10:
		return "14496-2 visual"
	case 0x11:
		return "14496-3 audio with LATM transport syntax as defined in ISO/IEC 14496-3 / AMD 1"
	case 0x12:
		return "14496-1 SL-packetized stream or FlexMux stream carried in PES packet"
	case 0x13:
		return "14496-1 SL-packetized stream or FlexMux stream carrried in 14496 sections"
	case 0x14:
		return "13818-6 synchronized download protocol"
	case 0x15:
		return "Metadata carried in PES packets"
	case 0x16:
		return "Metadata carried in metadata_sections"
	case 0x17:
		return "Metadata carried in ISO/IEC 13818-6 Data Carousel"
	case 0x18:
		return "Metadata carried in ISO/IEC 13818-6 Object Carousel"
	case 0x19:
		return "Metadata carried in ISO/IEC 13818-6 Synchronized Download Protocol"
	case 0x1A:
		return "IPMP stream (defined in ISO/IEC 13818-11, MPEG2IPMP)"
	case 0x1B:
		return "AVC video stream as defined in ITU-T Rec. H.264|ISO/IEC 14496-10 Video"
	case 0x7F:
		return "IPMP stream"
	default:
		if streamType <= 0x7E {
			return " 13818-1 reserved"
		}
		return "user private"
	}
}

// DumpProgramInfos Dump Program info. When dumpDescriptors is true, the parsed
// ES info descriptors are printed under each program info line.
func (p *Pmt) DumpProgramInfos(dumpDescriptors bool) {
	for _, val := range p.programInfos {
		streamType := StreamTypeString(val.streamType)
		pmtField("Program Info : elementary_PID", "0x%02x, stream_type : 0x%02x (%s)", val.elementaryPid, val.streamType, streamType)
		if dumpDescriptors {
			for _, d := range val.descriptors {
				d.Dump()
			}
		}
	}
}

// Dump PMT detail.
func (p *Pmt) Dump() {
	fmt.Printf("\n===========================================\n")
	fmt.Printf(" PMT")
	fmt.Printf("\n===========================================\n")
	pmtField("table_id", "0x%x", p.tableID)
	pmtField("section_syntax_indicator", "%d", p.sectionSyntaxIndicator)
	pmtField("section_length", "%d", p.sectionLength)
	pmtField("program_number", "%d", p.programNumber)
	pmtField("version_number", "%d", p.versionNumber)
	pmtField("current_next_indicator", "%d", p.currentNextIndicator)
	pmtField("section_number", "%d", p.sectionNumber)
	pmtField("last_section_number", "%d", p.lastSectionNumber)
	pmtField("PCR_PID", "0x%x", p.pcrPid)
	pmtField("program_info_length", "%d", p.programInfoLength)
	for _, d := range p.descriptors {
		d.Dump()
	}
	p.DumpProgramInfos(true)
	pmtField("CRC_32", "%x", p.crc32)
}
