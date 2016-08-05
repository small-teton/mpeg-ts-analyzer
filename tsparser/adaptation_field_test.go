package tsparser

import (
	"reflect"
	"testing"

	"github.com/small-teton/MpegTsAnalyzer/options"
)

func TestNewAdaptationField(t *testing.T) {
	af := NewAdaptationField()
	if _, ok := interface{}(af).(*AdaptationField); !ok {
		t.Errorf("actual: *tsparser.Pat, But got %s", reflect.TypeOf(af))
	}
}

func TestAdaptationFieldnitialize(t *testing.T) {
	var prevPcr uint64 = 0x1FFFFFFF
	var options options.Options
	options.SetDumpHeader(true)
	af := NewAdaptationField()
	af.Initialize(1, &prevPcr, options)

	if af.pos != 1 {
		t.Errorf("actual: 1, But got %s", af.pos)
	}
	if *af.prevPcr != 0x1FFFFFFF {
		t.Errorf("actual: 0x1FFFFFFF, But got %s", *af.prevPcr)
	}
	if !af.options.DumpHeader() {
		t.Errorf("actual: true, But got false")
	}
}

func TestAdaptationFieldAppend(t *testing.T) {
	data1 := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}
	data2 := []byte{0x19, 0xed, 0x5d, 0xda, 0x57, 0x4b, 0xa0, 0x22, 0x2b, 0x1e, 0xf7, 0xb1, 0x66, 0xf6, 0x2b, 0x29, 0x43}

	af := NewAdaptationField()
	af.Append(data1)

	if len(af.buf) != len(data1) {
		t.Errorf("length is different: actual %d, But got %d", len(data1), len(af.buf))
	}
	for i, val := range data1 {
		if af.buf[i] != val {
			t.Errorf("actual: %x, But got %x", val, af.buf[i])
		}
	}

	af.Append(data2)
	if len(af.buf) != len(data1)+len(data2) {
		t.Errorf("length is different: actual %d, But got %d", len(data1)+len(data2), len(af.buf))
	}
	offset := len(data1)
	for i, val := range data2 {
		if af.buf[offset+i] != val {
			t.Errorf("actual: %x, But got %x", val, af.buf[offset+i])
		}
	}
}

func TestAdaptationFieldPcrFlag(t *testing.T) {
	af := NewAdaptationField()

	var actual uint8 = 0x1
	af.pcrFlag = actual
	retVal := af.PcrFlag()
	if !retVal {
		t.Errorf("actual: 0x1, But got 0x0")
	}

	actual = 0x5
	af.pcrFlag = actual
	retVal = af.PcrFlag()
	if retVal {
		t.Errorf("actual: 0x0, But got 0x1")
	}
}

func TestAdaptationFieldPcr(t *testing.T) {
	af := NewAdaptationField()

	var actual uint64 = 0x1
	af.pcr = actual
	retVal := af.Pcr()
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}

	actual = 0x5
	af.pcr = actual
	retVal = af.Pcr()
	if retVal != actual {
		t.Errorf("actual: %x, But got %d", actual, retVal)
	}
}

func TestAdaptationFieldParse(t *testing.T) {
	data := []byte{0x07, 0x70, 0x00, 0x00, 0x00, 0x42, 0x7E, 0x6F}

	af := NewAdaptationField()
	af.Append(data)
	if len, err := af.Parse(); len != 7 || err != nil {
		t.Errorf("Parse error: %s", err)
	}
	err := false
	err = err || af.adaptationFieldLength != 7
	err = err || af.discontinuityIndicator != 0x00
	err = err || af.randomAccessIndicator != 0x01
	err = err || af.elementaryStreamPriorityIndicator != 0x01
	err = err || af.pcrFlag != 0x01
	err = err || af.oPcrFlag != 0x00
	err = err || af.transportPrivateDataFlag != 0x00
	err = err || af.programClockReferenceBase != 0x84
	err = err || af.programClockReferenceExtension != 0x6F
	if err {
		t.Errorf("Parse error")
	}
}
