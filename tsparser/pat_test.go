package tsparser

import (
	"reflect"
	"testing"
)

func TestNewPat(t *testing.T) {
	pat := NewPat()
	if _, ok := interface{}(pat).(*Pat); !ok {
		t.Errorf("actual: *tsparser.Pat, But got %s", reflect.TypeOf(pat))
	}
}

func TestPatContinuityCounter(t *testing.T) {
	pat := NewPat()

	var actual uint8 = 0x1
	pat.continuityCounter = actual
	retVal := pat.ContinuityCounter()
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}

	actual = 0x5
	pat.continuityCounter = actual
	retVal = pat.ContinuityCounter()
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}
}

func TestPatSetContinuityCounter(t *testing.T) {
	pat := NewPat()

	var actual uint8 = 0x1
	pat.SetContinuityCounter(actual)
	retVal := pat.continuityCounter
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}

	actual = 0x5
	pat.SetContinuityCounter(actual)
	retVal = pat.continuityCounter
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}
}

func TestPmtPid(t *testing.T) {
	pat := NewPat()

	var actual uint16 = 0xABCD
	pat.pmtPid = actual
	retVal := pat.PmtPid()
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}

	actual = 0xFFFF
	pat.pmtPid = actual
	retVal = pat.PmtPid()
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}
}

func TestPatAppend(t *testing.T) {
	data1 := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}
	data2 := []byte{0x19, 0xed, 0x5d, 0xda, 0x57, 0x4b, 0xa0, 0x22, 0x2b, 0x1e, 0xf7, 0xb1, 0x66, 0xf6, 0x2b, 0x29, 0x43}

	pat := NewPat()
	pat.Append(data1)

	if len(pat.buf) != len(data1) {
		t.Errorf("length is different: actual %d, But got %d", len(data1), len(pat.buf))
	}
	for i, val := range data1 {
		if pat.buf[i] != val {
			t.Errorf("actual: %x, But got %x", val, pat.buf[i])
		}
	}

	pat.Append(data2)
	if len(pat.buf) != len(data1)+len(data2) {
		t.Errorf("length is different: actual %d, But got %d", len(data1)+len(data2), len(pat.buf))
	}
	offset := len(data1)
	for i, val := range data2 {
		if pat.buf[offset+i] != val {
			t.Errorf("actual: %x, But got %x", val, pat.buf[offset+i])
		}
	}
}

func TestPatParse(t *testing.T) {
	data := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53, 0xFF}
	pat := NewPat()
	pat.Append(data)
	if err := pat.Parse(); err != nil {
		t.Errorf("Parse error: %s", err)
	}
	err := false
	err = err || pat.tableID != 0x00
	err = err || pat.sectionSyntaxIndicator != 0x01
	err = err || pat.sectionLength != 13
	err = err || pat.transportStreamID != 0x3F
	err = err || pat.versionNumber != 0x00
	err = err || pat.currentNextIndicator != 0x01
	err = err || pat.sectionNumber != 0x00
	err = err || pat.lastSectionNumber != 0x00
	err = err || len(pat.programInfo) != 1
	err = err || pat.programInfo[0].programNumber != 0x01
	err = err || pat.programInfo[0].programMapPid != 0x3F
	err = err || pat.programInfo[0].networkPid != 0x00
	err = err || pat.crc32 != 0x2DBCB053
	if err {
		t.Errorf("Parse error")
	}

	data[5] = 0xFF
	pat = NewPat()
	pat.Append(data)
	if err := pat.Parse(); err == nil {
		t.Errorf("Cannot detect parse error")
	}
}
