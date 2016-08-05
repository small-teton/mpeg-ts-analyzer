package tsparser

import (
	"reflect"
	"testing"
)

func TestNewPes(t *testing.T) {
	pes := NewPes()
	if _, ok := interface{}(pes).(*Pes); !ok {
		t.Errorf("actual: *tsparser.Pat, But got %s", reflect.TypeOf(pes))
	}
}

func TestPesContinuityCounter(t *testing.T) {
	pes := NewPes()

	var actual uint8 = 0x1
	pes.continuityCounter = actual
	retVal := pes.ContinuityCounter()
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}

	actual = 0x5
	pes.continuityCounter = actual
	retVal = pes.ContinuityCounter()
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}
}

func TestPesSetContinuityCounter(t *testing.T) {
	pes := NewPes()

	var actual uint8 = 0x1
	pes.SetContinuityCounter(actual)
	retVal := pes.continuityCounter
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}

	actual = 0x5
	pes.SetContinuityCounter(actual)
	retVal = pes.continuityCounter
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}
}

func TestPesAppend(t *testing.T) {
	data1 := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}
	data2 := []byte{0x19, 0xed, 0x5d, 0xda, 0x57, 0x4b, 0xa0, 0x22, 0x2b, 0x1e, 0xf7, 0xb1, 0x66, 0xf6, 0x2b, 0x29, 0x43}

	pes := NewPes()
	pes.Append(data1)

	if len(pes.buf) != len(data1) {
		t.Errorf("length is different: actual %d, But got %d", len(data1), len(pes.buf))
	}
	for i, val := range data1 {
		if pes.buf[i] != val {
			t.Errorf("actual: %x, But got %x", val, pes.buf[i])
		}
	}

	pes.Append(data2)
	if len(pes.buf) != len(data1)+len(data2) {
		t.Errorf("length is different: actual %d, But got %d", len(data1)+len(data2), len(pes.buf))
	}
	offset := len(data1)
	for i, val := range data2 {
		if pes.buf[offset+i] != val {
			t.Errorf("actual: %x, But got %x", val, pes.buf[offset+i])
		}
	}
}

func TestPesParse(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x84, 0xC0, 0x0A, 0x31, 0x00, 0x01, 0xC7, 0x3F, 0x11, 0x00,
		0x01, 0xAF, 0xC9, 0x00, 0x00, 0x00, 0x01, 0x09, 0x10, 0x00, 0x00, 0x00, 0x01, 0x67, 0x4D, 0x40,
		0x1F, 0x96, 0x56, 0x05, 0xA1, 0xED, 0x82, 0xA8, 0x40, 0x00, 0x00, 0xFA, 0x40, 0x00, 0x3A, 0x98,
	}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Errorf("Parse error: %s", err)
	}
	err := false
	err = err || pes.packetStartCodePrefix != 0x000001
	err = err || pes.streamID != 0xE0
	err = err || pes.pesPacketLength != 0
	err = err || pes.pesScramblingControl != 0x00
	err = err || pes.pesPriority != 0x00
	err = err || pes.dataAlignmentIndicator != 0x01
	err = err || pes.copyright != 0x00
	err = err || pes.originalOrCopy != 0x00
	err = err || pes.ptsDtsFlags != 0x03
	err = err || pes.escrFlag != 0x00
	err = err || pes.esRateFlag != 0x00
	err = err || pes.dsmTrickModeFlag != 0x00
	err = err || pes.additionalCopyInfoFlag != 0x00
	err = err || pes.pesCrcFlag != 0x00
	err = err || pes.pesExtentionFlag != 0x00
	err = err || pes.pts != 0x639F
	err = err || pes.dts != 0x57E4
	if err {
		t.Errorf("Parse error")
	}
}
