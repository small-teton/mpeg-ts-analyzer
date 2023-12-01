package tsparser

import (
	"fmt"

	"github.com/small-teton/mpeg-ts-analyzer/bitbuffer"
	"github.com/small-teton/mpeg-ts-analyzer/options"
)

const tsHeaderSize = 4

// TsPacket is mpeg2-ts packet. It has fixed size(188byte).
type TsPacket struct {
	pos     int64
	options options.Options
	buf     []byte
	payload []byte

	syncByte                   uint8
	transportErrorIndicator    uint8
	payloadUnitStartIndicator  uint8
	transportPriority          uint8
	pid                        uint16
	transportScramblingControl uint8
	adaptationFieldControl     uint8
	continuityCounter          uint8

	adaptationField *AdaptationField
}

// NewTsPacket create new TsPacket instance.
func NewTsPacket() *TsPacket {
	tp := new(TsPacket)
	tp.buf = make([]byte, 0, tsPacketSize)
	tp.adaptationField = NewAdaptationField()
	return tp
}

// Initialize Set Params for TsPacket
func (tp *TsPacket) Initialize(pos int64, options options.Options) {
	tp.pos = pos
	tp.options = options

	tp.buf = tp.buf[0:0]
	tp.payload = tp.buf[0:0]
	tp.syncByte = 0
	tp.transportErrorIndicator = 0
	tp.payloadUnitStartIndicator = 0
	tp.transportPriority = 0
	tp.pid = 0
	tp.transportScramblingControl = 0
	tp.adaptationFieldControl = 0
	tp.continuityCounter = 0
	tp.adaptationField.Initialize(tp.pos, tp.options)
}

// Append append ts payload data for buffer.
func (tp *TsPacket) Append(buf []byte) {
	tp.buf = append(tp.buf, buf...)
}

// HasAf return whether this TsPacket has adaptation_field.
func (tp *TsPacket) HasAf() bool {
	return tp.adaptationFieldControl == 2 || tp.adaptationFieldControl == 3
}

// Pcr return this TsPacket PCR.
func (tp *TsPacket) Pcr() uint64 { return tp.adaptationField.Pcr() }

// Parse parse TsPacket header.
func (tp *TsPacket) Parse() error {
	if len(tp.buf) < 188 {
		return fmt.Errorf("Buffer is short of length: %d", len(tp.buf))
	}
	bb := new(bitbuffer.BitBuffer)
	bb.Set(tp.buf)

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
		tp.adaptationField.Initialize(tp.pos, tp.options)
		tp.adaptationField.Append(tp.buf[tsHeaderSize:])
		if afLength, err = tp.adaptationField.Parse(); err != nil {
			return err
		}
		if tp.options.DumpAdaptationField() {
			tp.adaptationField.Dump()
		}
	}
	if tp.adaptationFieldControl == 1 {
		tp.payload = tp.buf[tsHeaderSize:]
	}
	if tp.adaptationFieldControl == 3 {
		tp.payload = tp.buf[tsHeaderSize+afLength+1:]
	}

	if tp.options.DumpHeader() {
		tp.DumpHeader()
	}
	if tp.options.DumpPayload() {
		tp.DumpPayload()
	}

	return nil
}

// Payload return this TsPacket payload data.
func (tp *TsPacket) Payload() []byte {
	return tp.payload
}

// PayloadUnitStartIndicator return this TsPacket payload_unit_start_indicator.
func (tp *TsPacket) PayloadUnitStartIndicator() bool { return tp.payloadUnitStartIndicator == 0x1 }

// Pid return this TsPacket pid.
func (tp *TsPacket) Pid() uint16 { return tp.pid }

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
	fmt.Printf("continuity_counter		: %x\n", tp.continuityCounter)
}

// DumpData print this TsPacket payload binary.
func (tp *TsPacket) DumpPayload() {
	fmt.Printf("===============================================================\n")
	fmt.Printf(" Dump TS Data\n")
	fmt.Printf("===============================================================\n")
	for i, val := range tp.buf {
		if (i%20 == 0) || (i == 0) {
			fmt.Printf("\n%2d: ", (i+1)/20+1)
		}
		fmt.Printf("%02x ", val)
	}
	fmt.Printf("\n")
}
