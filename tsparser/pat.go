package tsparser

import (
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/small-teton/mpeg-ts-analyzer/bitbuffer"
)

// Pat Program Map Table.
type Pat struct {
	// startFlag      bool
	continuityCounter uint8
	buf               []byte
	pmtPid            uint16

	tableID                uint8
	sectionSyntaxIndicator uint8
	sectionLength          uint16
	transportStreamID      uint16
	versionNumber          uint8
	currentNextIndicator   uint8
	sectionNumber          uint8
	lastSectionNumber      uint8
	programInfo            []PatProgramInfo
	crc32                  uint32
}

// PatProgramInfo Program Info of mpeg.
type PatProgramInfo struct {
	programNumber uint16
	networkPid    uint16
	programMapPid uint16
}

// NewPat create new PAT instance
func NewPat() *Pat { return new(Pat) }

// ContinuityCounter return current continuity_counter of TsPacket.
func (p *Pat) ContinuityCounter() uint8 { return p.continuityCounter }

// SetContinuityCounter set current continuity_counter of TsPacket.
func (p *Pat) SetContinuityCounter(continuityCounter uint8) { p.continuityCounter = continuityCounter }

// PmtPid return PMT pid.
func (p *Pat) PmtPid() uint16 { return p.pmtPid }

// Append append ts payload data for buffer.
func (p *Pat) Append(buf []byte) {
	p.buf = append(p.buf, buf...)
}

// Parse PAT data.
func (p *Pat) Parse() error {
	bb := new(bitbuffer.BitBuffer)
	bb.Set(p.buf)

	var err error
	if p.tableID, err = bb.PeekUint8(8); err != nil {
		return errors.Wrap(err, "failed peek pat table_id")
	}
	if p.sectionSyntaxIndicator, err = bb.PeekUint8(1); err != nil {
		return errors.Wrap(err, "failed to peek pat section_syntax_indicator")
	}
	if err = bb.Skip(1); err != nil {
		return errors.Wrap(err, "failed to skip in pat: ()")
	} // ()
	if err = bb.Skip(2); err != nil {
		return errors.Wrap(err, "failed to skip in pat: reserved")
	} // reserved
	if p.sectionLength, err = bb.PeekUint16(12); err != nil {
		return errors.Wrap(err, "failed to peek pat section_length")
	}
	if p.transportStreamID, err = bb.PeekUint16(16); err != nil {
		return errors.Wrap(err, "failed to peek pat transport_stream_id")
	}
	if err = bb.Skip(2); err != nil {
		return errors.Wrap(err, "failed to skip in pat: reserved")
	} // reserved
	if p.versionNumber, err = bb.PeekUint8(5); err != nil {
		return errors.Wrap(err, "failed to peek pat transport_stream_id")
	}
	if p.currentNextIndicator, err = bb.PeekUint8(1); err != nil {
		return errors.Wrap(err, "failed to peek pat current_next_indicator")
	}
	if p.sectionNumber, err = bb.PeekUint8(8); err != nil {
		return errors.Wrap(err, "failed to peek pat section_number")
	}
	if p.lastSectionNumber, err = bb.PeekUint8(8); err != nil {
		return errors.Wrap(err, "failed to peek pat last_section_number")
	}

	for i := 0; i < ((int(p.sectionLength) - 9) / 4); i++ {
		var patProgramInfo PatProgramInfo
		if patProgramInfo.programNumber, err = bb.PeekUint16(16); err != nil {
			return errors.Wrap(err, "failed to peek pat program info: program_number")
		}
		if err = bb.Skip(3); err != nil {
			return errors.Wrap(err, "failed to skip in pat program info: reserved")
		} // reserved
		if patProgramInfo.programNumber == 0 {
			if patProgramInfo.networkPid, err = bb.PeekUint16(13); err != nil {
				return errors.Wrap(err, "failed to peek pat program info: network_pid")
			}
		} else {
			if patProgramInfo.programMapPid, err = bb.PeekUint16(13); err != nil {
				return errors.Wrap(err, "failed to peek pat program info: program_map_pid")
			}
			p.pmtPid = patProgramInfo.programMapPid
		}
		p.programInfo = append(p.programInfo, patProgramInfo)
	}
	if p.crc32, err = bb.PeekUint32(32); err != nil {
		return errors.Wrap(err, "failed to peek pat crc32")
	}

	if len(p.buf) >= int(3+p.sectionLength-4) && p.crc32 != crc32(p.buf[0:3+p.sectionLength-4]) {
		return errors.New("PAT CRC32 is invalidate")
	}

	return nil
}

// Dump PAT detail.
func (p *Pat) Dump() {
	fmt.Printf("\n===========================================\n")
	fmt.Printf(" PAT")
	fmt.Printf("\n===========================================\n")
	fmt.Printf("PAT : table_id			: 0x%x\n", p.tableID)
	fmt.Printf("PAT : section_syntax_indicator	: %d\n", p.sectionSyntaxIndicator)
	fmt.Printf("PAT : section_length		: %d\n", p.sectionLength)
	fmt.Printf("PAT : transport_stream_id	: %d\n", p.transportStreamID)
	fmt.Printf("PAT : version_number		: %d\n", p.versionNumber)
	fmt.Printf("PAT : current_next_indicator	: %d\n", p.currentNextIndicator)
	fmt.Printf("PAT : section_number		: %d\n", p.sectionNumber)
	fmt.Printf("PAT : last_section_number	: %d\n", p.lastSectionNumber)

	for _, val := range p.programInfo {
		fmt.Printf("PAT : program_number		: %d\n", val.programNumber)
		if val.programNumber == 0 {
			fmt.Printf("PAT : network_PID		: 0x%x\n", val.networkPid)
		} else {
			fmt.Printf("PAT : program_map_PID		: 0x%x\n", val.programMapPid)
		}
	}
	fmt.Printf("PAT : CRC_32			: %x\n", p.crc32)
}
