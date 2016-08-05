package tsparser

import (
	"fmt"

	"github.com/small-teton/MpegTsAnalyzer/bitbuffer"
)

// Pmt Progran Map Table
type Pmt struct {
	startFlag         bool
	continuityCounter uint8
	buf               []byte

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
	programInfos           []ProgramInfo
	crc32                  uint32
}

// ProgramInfo Program info
type ProgramInfo struct {
	streamType    uint8
	elementaryPid uint16
	esInfoLength  uint16
}

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
	bb := new(bitbuffer.BitBuffer)
	bb.Set(p.buf)

	var err error
	if p.tableID, err = bb.PeekUint8(8); err != nil {
		return err
	}
	if p.sectionSyntaxIndicator, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if err := bb.Skip(1); err != nil {
		return err
	} // ()
	if err := bb.Skip(2); err != nil {
		return err
	} // reserved
	if p.sectionLength, err = bb.PeekUint16(12); err != nil {
		return err
	}
	if p.programNumber, err = bb.PeekUint16(16); err != nil {
		return err
	}
	if err := bb.Skip(2); err != nil {
		return err
	} // reserved
	if p.versionNumber, err = bb.PeekUint8(5); err != nil {
		return err
	}
	if p.currentNextIndicator, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if p.sectionNumber, err = bb.PeekUint8(8); err != nil {
		return err
	}
	if p.lastSectionNumber, err = bb.PeekUint8(8); err != nil {
		return err
	}
	if err := bb.Skip(3); err != nil {
		return err
	} // reserved
	if p.pcrPid, err = bb.PeekUint16(13); err != nil {
		return err
	}
	if err := bb.Skip(4); err != nil {
		return err
	} // reserved
	if p.programInfoLength, err = bb.PeekUint16(12); err != nil {
		return err
	}
	if err := bb.Skip(8 * p.programInfoLength); err != nil {
		return err
	}
	remainLength := int32(p.sectionLength - 9 - 4)
	for remainLength > 0 {
		var info ProgramInfo
		if info.streamType, err = bb.PeekUint8(8); err != nil {
			return err
		}
		if err := bb.Skip(3); err != nil {
			return err
		} // reserved
		if info.elementaryPid, err = bb.PeekUint16(13); err != nil {
			return err
		}
		if err := bb.Skip(4); err != nil {
			return err
		} // reserved
		if info.esInfoLength, err = bb.PeekUint16(12); err != nil {
			return err
		}
		if err := bb.Skip(8 * info.esInfoLength); err != nil {
			return err
		}
		remainLength = remainLength - 5 - int32(info.esInfoLength)
		p.programInfos = append(p.programInfos, info)
	}
	if p.crc32, err = bb.PeekUint32(32); err != nil {
		return err
	}

	if len(p.buf) >= int(3+p.sectionLength-4) && p.crc32 != crc32(p.buf[0:3+p.sectionLength-4]) {
		return fmt.Errorf("PAT CRC32 is invalidate")
	}

	return nil
}

// DumpProgramInfos Dump Program info
func (p *Pmt) DumpProgramInfos() {
	for _, val := range p.programInfos {
		var streamType string
		switch val.streamType {
		case 0x00:
			streamType = "reserved"
		case 0x01:
			streamType = "11172 video"
		case 0x02:
			streamType = "13818-2 video or 11172-2 constrained parameter video stream"
		case 0x03:
			streamType = "11172 audio"
		case 0x04:
			streamType = "13818-3 audio"
		case 0x05:
			streamType = "13818-1 private sections"
		case 0x06:
			streamType = "13818-1 PES packet containing private data"
		case 0x07:
			streamType = "13522 MHEG"
		case 0x08:
			streamType = "13818-1 annex A DSM-CC"
		case 0x09:
			streamType = "H.222.1"
		case 0x0A:
			streamType = "13818-6 type A"
		case 0x0B:
			streamType = "13818-6 type B"
		case 0x0C:
			streamType = "13818-6 type C"
		case 0x0D:
			streamType = "13818-6 type D"
		case 0x0E:
			streamType = "13818-1 auxiliary"
		case 0x0F:
			streamType = "13818-7 audio with ADTS transport syntax"
		case 0x10:
			streamType = "14496-2 visual"
		case 0x11:
			streamType = "14496-3 audio with LATM transport syntax as defined in ISO/IEC 14496-3 / AMD 1"
		case 0x12:
			streamType = "14496-1 SL-packetized stream or FlexMux stream carried in PES packet"
		case 0x13:
			streamType = "14496-1 SL-packetized stream or FlexMux stream carrried in 14496 sections"
		case 0x14:
			streamType = "13818-6 synchronized download protocol"
		case 0x15:
			streamType = "Metadata carried in PES packets"
		case 0x16:
			streamType = "Metadata carried in metadata_sections"
		case 0x17:
			streamType = "Metadata carried in ISO/IEC 13818-6 Data Carousel"
		case 0x18:
			streamType = "Metadata carried in ISO/IEC 13818-6 Object Carousel"
		case 0x19:
			streamType = "Metadata carried in ISO/IEC 13818-6 Synchronized Download Protocol"
		case 0x1A:
			streamType = "IPMP stream (defined in ISO/IEC 13818-11, MPEG2IPMP)"
		case 0x1B:
			streamType = "AVC video stream as defined in ITU-T Rec. H.264|ISO/IEC 14496-10 Video"
		case 0x7F:
			streamType = "IPMP stream"
		default:
			if val.streamType <= 0x7E {
				streamType = " 13818-1 reserved"
			} else {
				streamType = "user private"
			}
		}
		fmt.Printf("PMT : Program Info : elementary_PID	: 0x%02x, stream_type : 0x%02x (%s)\n", val.elementaryPid, val.streamType, streamType)
	}
}

// Dump PMT detail.
func (p *Pmt) Dump() {
	fmt.Printf("\n===========================================\n")
	fmt.Printf(" PMT")
	fmt.Printf("\n===========================================\n")
	fmt.Printf("PMT : table_id			: 0x%x\n", p.tableID)
	fmt.Printf("PMT : section_syntax_indicator	: %d\n", p.sectionSyntaxIndicator)
	fmt.Printf("PMT : section_length		: %d\n", p.sectionLength)
	fmt.Printf("PMT : program_number		: %d\n", p.programNumber)
	fmt.Printf("PMT : version_number		: %d\n", p.versionNumber)
	fmt.Printf("PMT : current_next_indicator	: %d\n", p.currentNextIndicator)
	fmt.Printf("PMT : section_number		: %d\n", p.sectionNumber)
	fmt.Printf("PMT : last_section_number	: %d\n", p.lastSectionNumber)
	fmt.Printf("PMT : PCR_PID			: 0x%x\n", p.pcrPid)
	fmt.Printf("PMT : program_info_length	: %d\n", p.programInfoLength)
	p.DumpProgramInfos()
	fmt.Printf("PMT : CRC_32			: %x\n", p.crc32)
}
