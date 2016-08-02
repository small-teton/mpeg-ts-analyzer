package main

import (
	"fmt"
)

type Pat struct {
	startFlag   bool
	cyclicValue uint8
	buf         []byte
	pmtPid      uint16

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

type PatProgramInfo struct {
	programNumber uint16
	networkPid    uint16
	programMapPid uint16
}

func NewPat() *Pat { return new(Pat) }

func (this *Pat) CyclicValue() uint8               { return this.cyclicValue }
func (this *Pat) SetCyclicValue(cyclicValue uint8) { this.cyclicValue = cyclicValue }

func (this *Pat) PmtPid() uint16 { return this.pmtPid }

func (this *Pat) Append(buf []byte) {
	this.buf = append(this.buf, buf...)
}

func (this *Pat) Parse() error {
	bb := new(BitBuffer)
	bb.Set(this.buf)

	var err error
	if this.tableID, err = bb.PeekUint8(8); err != nil {
		return err
	}
	if this.sectionSyntaxIndicator, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if err = bb.Skip(1); err != nil {
		return err
	} // ()
	if err = bb.Skip(2); err != nil {
		return err
	} // reserved
	if this.sectionLength, err = bb.PeekUint16(12); err != nil {
		return err
	}
	if this.transportStreamID, err = bb.PeekUint16(16); err != nil {
		return err
	}
	if err = bb.Skip(2); err != nil {
		return err
	} // reserved
	if this.versionNumber, err = bb.PeekUint8(5); err != nil {
		return err
	}
	if this.currentNextIndicator, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.sectionNumber, err = bb.PeekUint8(8); err != nil {
		return err
	}
	if this.lastSectionNumber, err = bb.PeekUint8(8); err != nil {
		return err
	}

	for i := 0; i < ((int(this.sectionLength) - 9) / 4); i++ {
		var patProgramInfo PatProgramInfo
		if patProgramInfo.programNumber, err = bb.PeekUint16(16); err != nil {
			return err
		}
		if err = bb.Skip(3); err != nil {
			return err
		} // reserved
		if patProgramInfo.programNumber == 0 {
			if patProgramInfo.networkPid, err = bb.PeekUint16(13); err != nil {
				return err
			}
		} else {
			if patProgramInfo.programMapPid, err = bb.PeekUint16(13); err != nil {
				return err
			}
			this.pmtPid = patProgramInfo.programMapPid
		}
		this.programInfo = append(this.programInfo, patProgramInfo)
	}
	if this.crc32, err = bb.PeekUint32(32); err != nil {
		return err
	}
	return nil
}

func (this *Pat) Dump() {
	fmt.Printf("\n===========================================\n")
	fmt.Printf(" PAT")
	fmt.Printf("\n===========================================\n")
	fmt.Printf("PAT : table_id			: 0x%x\n", this.tableID)
	fmt.Printf("PAT : section_syntax_indicator	: %d\n", this.sectionSyntaxIndicator)
	fmt.Printf("PAT : section_length		: %d\n", this.sectionLength)
	fmt.Printf("PAT : transport_stream_id	: %d\n", this.transportStreamID)
	fmt.Printf("PAT : version_number		: %d\n", this.versionNumber)
	fmt.Printf("PAT : current_next_indicator	: %d\n", this.currentNextIndicator)
	fmt.Printf("PAT : section_number		: %d\n", this.sectionNumber)
	fmt.Printf("PAT : last_section_number	: %d\n", this.lastSectionNumber)

	for _, val := range this.programInfo {
		fmt.Printf("PAT : program_number		: %d\n", val.programNumber)
		if val.programNumber == 0 {
			fmt.Printf("PAT : network_PID		: 0x%x\n", val.networkPid)
		} else {
			fmt.Printf("PAT : program_map_PID		: 0x%x\n", val.programMapPid)
		}
	}
	fmt.Printf("PAT : CRC_32			: %x\n", this.crc32)
}
