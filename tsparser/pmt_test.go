package tsparser

import (
	"reflect"
	"testing"
)

func TestNewPmt(t *testing.T) {
	pmt := NewPmt()
	if _, ok := interface{}(pmt).(*Pmt); !ok {
		t.Errorf("actual: *tsparser.Pmt, But got %s", reflect.TypeOf(pmt))
	}
}

func TestPmtContinuityCounter(t *testing.T) {
	pmt := NewPmt()

	var actual uint8 = 0x1
	pmt.continuityCounter = actual
	retVal := pmt.ContinuityCounter()
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}

	actual = 0x5
	pmt.continuityCounter = actual
	retVal = pmt.ContinuityCounter()
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}
}

func TestPmtSetContinuityCounter(t *testing.T) {
	pmt := NewPmt()

	var actual uint8 = 0x1
	pmt.SetContinuityCounter(actual)
	retVal := pmt.continuityCounter
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}

	actual = 0x5
	pmt.SetContinuityCounter(actual)
	retVal = pmt.continuityCounter
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}
}

func TestPcrPid(t *testing.T) {
	pmt := NewPmt()

	var actual uint16 = 0xABCD
	pmt.pcrPid = actual
	retVal := pmt.PcrPid()
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}

	actual = 0xFFFF
	pmt.pcrPid = actual
	retVal = pmt.PcrPid()
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}
}

func TestProgramInfos(t *testing.T) {
	pmt := NewPmt()

	input := make([]ProgramInfo, 3)
	input[0].streamType = 0x1B
	input[0].elementaryPid = 0x31
	input[0].esInfoLength = 0x00
	input[1].streamType = 0x0F
	input[1].elementaryPid = 0x64
	input[1].esInfoLength = 0x00
	input[2].streamType = 0x0F
	input[2].elementaryPid = 0x98
	input[2].esInfoLength = 0x00
	pmt.programInfos = input

	output := pmt.ProgramInfos()
	err := false
	err = err || len(output) != 3
	err = err || output[0].streamType != 0x1B
	err = err || output[0].elementaryPid != 0x31
	err = err || output[0].esInfoLength != 0x00
	err = err || output[1].streamType != 0x0F
	err = err || output[1].elementaryPid != 0x64
	err = err || output[1].esInfoLength != 0x00
	err = err || output[2].streamType != 0x0F
	err = err || output[2].elementaryPid != 0x98
	err = err || output[2].esInfoLength != 0x00
	if err {
		t.Errorf("Parse error")
	}
}

func TestPmtAppend(t *testing.T) {
	data1 := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}
	data2 := []byte{0x19, 0xed, 0x5d, 0xda, 0x57, 0x4b, 0xa0, 0x22, 0x2b, 0x1e, 0xf7, 0xb1, 0x66, 0xf6, 0x2b, 0x29, 0x43}

	pmt := NewPmt()
	pmt.Append(data1)

	if len(pmt.buf) != len(data1) {
		t.Errorf("length is different: actual %d, But got %d", len(data1), len(pmt.buf))
	}
	for i, val := range data1 {
		if pmt.buf[i] != val {
			t.Errorf("actual: %x, But got %x", val, pmt.buf[i])
		}
	}

	pmt.Append(data2)
	if len(pmt.buf) != len(data1)+len(data2) {
		t.Errorf("length is different: actual %d, But got %d", len(data1)+len(data2), len(pmt.buf))
	}
	offset := len(data1)
	for i, val := range data2 {
		if pmt.buf[offset+i] != val {
			t.Errorf("actual: %x, But got %x", val, pmt.buf[offset+i])
		}
	}
}

func TestPmtParse(t *testing.T) {
	data := []byte{0x02, 0xB0, 0x1C, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0, 0x00, 0x0F, 0xE0, 0x64, 0xF0, 0x00, 0x0F, 0xE0, 0x98, 0xF0, 0x00, 0x3D, 0xFE, 0xAE, 0x61, 0xFF}
	pmt := NewPmt()
	pmt.Append(data)
	if err := pmt.Parse(); err != nil {
		t.Errorf("Parse error: %s", err)
	}
	err := false
	err = err || pmt.tableID != 0x02
	err = err || pmt.sectionSyntaxIndicator != 0x01
	err = err || pmt.sectionLength != 28
	err = err || pmt.programNumber != 0x01
	err = err || pmt.versionNumber != 0x00
	err = err || pmt.currentNextIndicator != 0x01
	err = err || pmt.sectionNumber != 0x00
	err = err || pmt.lastSectionNumber != 0x00
	err = err || pmt.pcrPid != 0x31
	err = err || pmt.programInfoLength != 0x00
	err = err || len(pmt.programInfos) != 3
	err = err || pmt.programInfos[0].streamType != 0x1B
	err = err || pmt.programInfos[0].elementaryPid != 0x31
	err = err || pmt.programInfos[0].esInfoLength != 0x00
	err = err || pmt.programInfos[1].streamType != 0x0F
	err = err || pmt.programInfos[1].elementaryPid != 0x64
	err = err || pmt.programInfos[1].esInfoLength != 0x00
	err = err || pmt.programInfos[2].streamType != 0x0F
	err = err || pmt.programInfos[2].elementaryPid != 0x98
	err = err || pmt.programInfos[2].esInfoLength != 0x00
	err = err || pmt.crc32 != 0x3DFEAE61
	if err {
		t.Errorf("Parse error")
	}

	data[5] = 0xFF
	pmt = NewPmt()
	pmt.Append(data)
	if err := pmt.Parse(); err == nil {
		t.Errorf("Cannot detect parse error")
	}
}
