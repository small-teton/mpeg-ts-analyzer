package main

import (
	"fmt"
)

const tsHeaderSize = 4

type TsPacket struct {
	pos     int64
	prevPcr *uint64
	data    []byte
	body    []byte

	sync_byte                    uint8
	transport_error_indicator    uint8
	payload_unit_start_indicator uint8
	transport_priority           uint8
	pid                          uint16
	transport_scrambling_control uint8
	adaptation_field_control     uint8
	cyclicValue                  uint8

	adaptationField AdaptationField
}

func NewTsPacket(pos int64, prevPcr *uint64) *TsPacket {
	tp := new(TsPacket)
	tp.pos = pos
	tp.prevPcr = prevPcr
	tp.data = make([]byte, tsPacketSize)
	return tp
}

func (this *TsPacket) HasAf() bool {
	return this.adaptation_field_control == 2 || this.adaptation_field_control == 3
}
func (this *TsPacket) Pcr() uint64 { return this.adaptationField.Pcr() }

func (this *TsPacket) Parse(buf []byte) error {
	copy(this.data, buf[:tsPacketSize])

	bb := new(BitBuffer)
	bb.Set(this.data)

	var err error
	if this.sync_byte, err = bb.PeekUint8(8); err != nil {
		return err
	}
	if this.transport_error_indicator, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.payload_unit_start_indicator, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.transport_priority, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.pid, err = bb.PeekUint16(13); err != nil {
		return err
	}
	if this.transport_scrambling_control, err = bb.PeekUint8(2); err != nil {
		return err
	}
	if this.adaptation_field_control, err = bb.PeekUint8(2); err != nil {
		return err
	}
	if this.cyclicValue, err = bb.PeekUint8(4); err != nil {
		return err
	}

	var afLength uint8
	if this.adaptation_field_control == 2 || this.adaptation_field_control == 3 {
		if afLength, err = this.adaptationField.Parse(this.data[tsHeaderSize:], this.pos, this.prevPcr); err != nil {
			return err
		}
		if *dAf {
			this.adaptationField.Dump()
		}
	}
	if this.adaptation_field_control == 1 {
		this.body = this.data[tsHeaderSize:]
	}
	if this.adaptation_field_control == 3 {
		this.body = this.data[tsHeaderSize+afLength+1:]
	}
	return nil
}

func (this *TsPacket) Body() []byte {
	return this.body
}

func (this *TsPacket) PayloadUnitStartIndicator() bool {
	return this.payload_unit_start_indicator == 0x1
}
func (this *TsPacket) Pid() uint16                   { return this.pid }
func (this *TsPacket) AdaptationFieldControl() uint8 { return this.adaptation_field_control }
func (this *TsPacket) CyclicValue() uint8            { return this.cyclicValue }

func (this *TsPacket) DumpTsHeader() {
	fmt.Printf("===============================================================\n")
	fmt.Printf(" TS Header\n")
	fmt.Printf("===============================================================\n")

	fmt.Printf("transport_error_indicator	: %d\n", this.transport_error_indicator)
	fmt.Printf("payload_unit_start_indicator	: %d\n", this.payload_unit_start_indicator)
	fmt.Printf("transport_priority		: %d\n", this.transport_priority)
	fmt.Printf("pid				: 0x%x\n", this.pid)

	fmt.Printf("transport_scrambling_control	: %x\n", this.transport_scrambling_control)
	fmt.Printf("adaptation_field_control	: %x\n", this.adaptation_field_control)
	fmt.Printf("cyclicValue			: %x\n", this.cyclicValue)
}

func (this *TsPacket) DumpTsData() {
	fmt.Printf("===============================================================\n")
	fmt.Printf(" Dump TS Data\n")
	fmt.Printf("===============================================================\n")
	for i, val := range this.data {
		if (i%20 == 0) || (i == 0) {
			fmt.Printf("\n%2d: ", (i+1)/20+1)
		}
		fmt.Printf("%02x ", val)
	}
	fmt.Printf("\n")
}
