package tsparser

import (
	"errors"
	"fmt"
)

// Pat Program Association Table.
type Pat struct {
	// startFlag      bool
	continuityCounter uint8
	buf               []byte
	pmtPid            uint16
	newBitReader      func() bitReader

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

// Program is one PAT entry for an actual program (program_number != 0, which is
// the network PID).
type Program struct {
	ProgramNumber uint16
	PmtPid        uint16
}

// Programs returns every program listed in the PAT. A multi-program transport
// stream — e.g. a raw ISDB-T capture carrying a main service plus 1seg — has
// more than one.
func (p *Pat) Programs() []Program {
	programs := make([]Program, 0, len(p.programInfo))
	for _, info := range p.programInfo {
		if info.programNumber != 0 {
			programs = append(programs, Program{ProgramNumber: info.programNumber, PmtPid: info.programMapPid})
		}
	}
	return programs
}

// Append append ts payload data for buffer.
func (p *Pat) Append(buf []byte) {
	p.buf = append(p.buf, buf...)
}

// Parse PAT data.
func (p *Pat) Parse() error {
	var bb bitReader
	if p.newBitReader != nil {
		bb = p.newBitReader()
	} else {
		bb = newDefaultBitReader()
	}
	bb.Set(p.buf)

	var err error
	if p.tableID, err = bb.ReadUint8(8); err != nil {
		return fmt.Errorf("failed to read pat table_id: %w", err)
	}
	if p.tableID != 0x00 {
		return fmt.Errorf("invalid pat table_id: 0x%02x", p.tableID)
	}
	if p.sectionSyntaxIndicator, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to peek pat section_syntax_indicator: %w", err)
	}
	if err = bb.Skip(1); err != nil {
		return fmt.Errorf("failed to skip in pat: (): %w", err)
	} // ()
	if err = bb.Skip(2); err != nil {
		return fmt.Errorf("failed to skip in pat: reserved: %w", err)
	} // reserved
	if p.sectionLength, err = bb.ReadUint16(12); err != nil {
		return fmt.Errorf("failed to peek pat section_length: %w", err)
	}
	if p.transportStreamID, err = bb.ReadUint16(16); err != nil {
		return fmt.Errorf("failed to peek pat transport_stream_id: %w", err)
	}
	if err = bb.Skip(2); err != nil {
		return fmt.Errorf("failed to skip in pat: reserved: %w", err)
	} // reserved
	if p.versionNumber, err = bb.ReadUint8(5); err != nil {
		return fmt.Errorf("failed to peek pat transport_stream_id: %w", err)
	}
	if p.currentNextIndicator, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to peek pat current_next_indicator: %w", err)
	}
	if p.sectionNumber, err = bb.ReadUint8(8); err != nil {
		return fmt.Errorf("failed to peek pat section_number: %w", err)
	}
	if p.lastSectionNumber, err = bb.ReadUint8(8); err != nil {
		return fmt.Errorf("failed to peek pat last_section_number: %w", err)
	}

	for i := 0; i < ((int(p.sectionLength) - 9) / 4); i++ {
		var patProgramInfo PatProgramInfo
		if patProgramInfo.programNumber, err = bb.ReadUint16(16); err != nil {
			return fmt.Errorf("failed to peek pat program info: program_number: %w", err)
		}
		if err = bb.Skip(3); err != nil {
			return fmt.Errorf("failed to skip in pat program info: reserved: %w", err)
		} // reserved
		if patProgramInfo.programNumber == 0 {
			if patProgramInfo.networkPid, err = bb.ReadUint16(13); err != nil {
				return fmt.Errorf("failed to peek pat program info: network_pid: %w", err)
			}
		} else {
			if patProgramInfo.programMapPid, err = bb.ReadUint16(13); err != nil {
				return fmt.Errorf("failed to peek pat program info: program_map_pid: %w", err)
			}
			p.pmtPid = patProgramInfo.programMapPid
		}
		p.programInfo = append(p.programInfo, patProgramInfo)
	}
	if p.crc32, err = bb.ReadUint32(32); err != nil {
		return fmt.Errorf("failed to peek pat crc32: %w", err)
	}

	if len(p.buf) >= int(3+p.sectionLength-4) && p.crc32 != crc32(p.buf[0:3+p.sectionLength-4]) {
		return errors.New("PAT CRC32 is invalid")
	}

	return nil
}

// patColonColumn is the column where the PAT dump aligns its "key : value" colons.
const patColonColumn = 32

// patField prints one aligned "PAT : <label> : <value>" line.
func patField(label, format string, args ...interface{}) {
	dumpField("PAT : ", patColonColumn, label, format, args...)
}

// Dump PAT detail.
func (p *Pat) Dump() {
	fmt.Printf("\n===========================================\n")
	fmt.Printf(" PAT")
	fmt.Printf("\n===========================================\n")
	patField("table_id", "0x%x", p.tableID)
	patField("section_syntax_indicator", "%d", p.sectionSyntaxIndicator)
	patField("section_length", "%d", p.sectionLength)
	patField("transport_stream_id", "%d", p.transportStreamID)
	patField("version_number", "%d", p.versionNumber)
	patField("current_next_indicator", "%d", p.currentNextIndicator)
	patField("section_number", "%d", p.sectionNumber)
	patField("last_section_number", "%d", p.lastSectionNumber)

	for _, val := range p.programInfo {
		patField("program_number", "%d", val.programNumber)
		if val.programNumber == 0 {
			patField("network_PID", "0x%x", val.networkPid)
		} else {
			patField("program_map_PID", "0x%x", val.programMapPid)
		}
	}
	patField("CRC_32", "%x", p.crc32)
}
