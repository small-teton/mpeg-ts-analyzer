package tsparser

import (
	"fmt"

	"github.com/small-teton/MpegTsAnalyzer/bitbuffer"
	"github.com/small-teton/MpegTsAnalyzer/options"
)

const tsHeaderSize = 4

// TsPacket is mpeg2-ts packet. It has fixed size(188byte).
type TsPacket struct {
	pos     int64
	prevPcr *uint64
	options options.Options
	data    []byte
	payload []byte

	syncByte                   uint8
	transportErrorIndicator    uint8
	payloadUnitStartIndicator  uint8
	transportPriority          uint8
	pid                        uint16
	transportScramblingControl uint8
	adaptationFieldControl     uint8
	continuityCounter          uint8

	adaptationField AdaptationField
}

// NewTsPacket create new TsPacket instance.
func NewTsPacket(buf []byte, pos int64, prevPcr *uint64, options options.Options) *TsPacket {
	tp := new(TsPacket)
	tp.pos = pos
	tp.prevPcr = prevPcr
	tp.options = options
	tp.data = make([]byte, tsPacketSize)
	copy(tp.data, buf[:tsPacketSize])
	return tp
}

// HasAf return whether this TsPacket has adaptation_field.
func (tp *TsPacket) HasAf() bool {
	return tp.adaptationFieldControl == 2 || tp.adaptationFieldControl == 3
}

// Pcr return this TsPacket PCR.
func (tp *TsPacket) Pcr() uint64 { return tp.adaptationField.Pcr() }

// Parse parse TsPacket header.
func (tp *TsPacket) Parse() error {

	bb := new(bitbuffer.BitBuffer)
	bb.Set(tp.data)

	var err error
	if tp.syncByte, err = bb.PeekUint8(8); err != nil {
		return err
	}
	if tp.transportErrorIndicator, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if tp.payloadUnitStartIndicator, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if tp.transportPriority, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if tp.pid, err = bb.PeekUint16(13); err != nil {
		return err
	}
	if tp.transportScramblingControl, err = bb.PeekUint8(2); err != nil {
		return err
	}
	if tp.adaptationFieldControl, err = bb.PeekUint8(2); err != nil {
		return err
	}
	if tp.continuityCounter, err = bb.PeekUint8(4); err != nil {
		return err
	}

	var afLength uint8
	if tp.adaptationFieldControl == 2 || tp.adaptationFieldControl == 3 {
		if afLength, err = tp.adaptationField.Parse(tp.data[tsHeaderSize:], tp.pos, tp.prevPcr); err != nil {
			return err
		}
		if tp.adaptationField.PcrFlag() {
			tp.adaptationField.DumpPcr()
		}
		if tp.options.DumpAdaptationField() {
			tp.adaptationField.Dump()
		}
	}
	if tp.adaptationFieldControl == 1 {
		tp.payload = tp.data[tsHeaderSize:]
	}
	if tp.adaptationFieldControl == 3 {
		tp.payload = tp.data[tsHeaderSize+afLength+1:]
	}
	return nil
}

// Payload return this TsPacket payload data.
func (tp *TsPacket) Payload() []byte {
	return tp.payload
}

// PayloadUnitStartIndicator return this TsPacket payload_unit_start_indicator.
func (tp *TsPacket) PayloadUnitStartIndicator() bool {
	return tp.payloadUnitStartIndicator == 0x1
}

// Pid return this TsPacket pid.
func (tp *TsPacket) Pid() uint16 { return tp.pid }

// PayloadUnitStartIndicator return this TsPacket payload_unit_start_indicator.

// ContinuityCounter return this TsPacket payload_unit_start_indicator.
func (tp *TsPacket) ContinuityCounter() uint8 { return tp.continuityCounter }

// DumpHeader print this TsPacket header detail.
func (tp *TsPacket) DumpHeader() {
	fmt.Printf("===============================================================\n")
	fmt.Printf(" TS Header\n")
	fmt.Printf("===============================================================\n")

	fmt.Printf("transport_error_indicator	: %d\n", tp.transportErrorIndicator)
	fmt.Printf("payload_unit_start_indicator	: %d\n", tp.payloadUnitStartIndicator)
	fmt.Printf("transport_priority		: %d\n", tp.transportPriority)
	fmt.Printf("pid				: 0x%x\n", tp.pid)

	fmt.Printf("transport_scrambling_control	: %x\n", tp.transportScramblingControl)
	fmt.Printf("adaptation_field_control	: %x\n", tp.adaptationFieldControl)
	fmt.Printf("continuity_counter			: %x\n", tp.continuityCounter)
}

// DumpData print this TsPacket payload binary.
func (tp *TsPacket) DumpData() {
	fmt.Printf("===============================================================\n")
	fmt.Printf(" Dump TS Data\n")
	fmt.Printf("===============================================================\n")
	for i, val := range tp.data {
		if (i%20 == 0) || (i == 0) {
			fmt.Printf("\n%2d: ", (i+1)/20+1)
		}
		fmt.Printf("%02x ", val)
	}
	fmt.Printf("\n")
}
