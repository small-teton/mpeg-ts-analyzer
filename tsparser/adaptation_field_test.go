package tsparser

import (
	"reflect"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/options"
)

func TestNewAdaptationField(t *testing.T) {
	af := NewAdaptationField()
	if _, ok := interface{}(af).(*AdaptationField); !ok {
		t.Errorf("actual: *tsparser.Pat, But got %s", reflect.TypeOf(af))
	}
}

func TestAdaptationFieldInitialize(t *testing.T) {
	var options options.Options
	options.DumpHeader = true
	af1 := NewAdaptationField()
	af1.Initialize(1, options)

	if af1.pos != 1 {
		t.Errorf("actual: 1, But got %d", af1.pos)
	}
	if !af1.options.DumpHeader {
		t.Errorf("actual: true, But got false")
	}

	data := []byte{0x07, 0x70, 0x00, 0x00, 0x00, 0x42, 0x7E, 0x6F}

	af2 := NewAdaptationField()
	af2.Append(data)
	if len, err := af2.Parse(); len != 7 || err != nil {
		t.Errorf("Parse error: %s", err)
	}
	af2.Initialize(1, options)

	if !reflect.DeepEqual(af1, af2) {
		t.Errorf("Failed Initialize. Different in af1 and af2")
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

func TestAdaptationFieldParseOpcr(t *testing.T) {
	// oPcrFlag=1, base=100, ext=50
	// OPCR 48 bits: base(33)=100, reserved(6)=0x3F, ext(9)=50
	// Bytes: 0x00, 0x00, 0x00, 0x32, 0x7E, 0x32
	data := []byte{0x07, 0x08, 0x00, 0x00, 0x00, 0x32, 0x7E, 0x32}

	af := NewAdaptationField()
	af.Append(data)
	if _, err := af.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if af.oPcrFlag != 1 {
		t.Errorf("expected oPcrFlag=1, got %d", af.oPcrFlag)
	}
	if af.originalProgramClockReferenceBase != 100 {
		t.Errorf("expected originalProgramClockReferenceBase=100, got %d", af.originalProgramClockReferenceBase)
	}
	if af.originalProgramClockReferenceExtension != 50 {
		t.Errorf("expected originalProgramClockReferenceExtension=50, got %d", af.originalProgramClockReferenceExtension)
	}
}

func TestAdaptationFieldParseSpliceCountdown(t *testing.T) {
	// splicingPointFlag=1, splice_countdown=0xAB
	data := []byte{0x02, 0x04, 0xAB}

	af := NewAdaptationField()
	af.Append(data)
	if _, err := af.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if af.splicingPointFlag != 1 {
		t.Errorf("expected splicingPointFlag=1, got %d", af.splicingPointFlag)
	}
	if af.spliceCountdown != 0xAB {
		t.Errorf("expected spliceCountdown=0xAB, got 0x%02X", af.spliceCountdown)
	}
}

func TestAdaptationFieldParsePrivateData(t *testing.T) {
	// transportPrivateDataFlag=1, private_data_length=3, data=0xDE,0xAD,0xFF
	data := []byte{0x05, 0x02, 0x03, 0xDE, 0xAD, 0xFF}

	af := NewAdaptationField()
	af.Append(data)
	if _, err := af.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if af.transportPrivateDataFlag != 1 {
		t.Errorf("expected transportPrivateDataFlag=1, got %d", af.transportPrivateDataFlag)
	}
	if af.transportPrivateDataLength != 3 {
		t.Errorf("expected transportPrivateDataLength=3, got %d", af.transportPrivateDataLength)
	}
	expected := []byte{0xDE, 0xAD, 0xFF}
	if !reflect.DeepEqual(af.privateDataByte, expected) {
		t.Errorf("expected privateDataByte=%v, got %v", expected, af.privateDataByte)
	}
}

func TestAdaptationFieldParseExtensionLtw(t *testing.T) {
	// adaptationFieldExtensionFlag=1, ltw=1
	// Extension: ext_len=3, flags=0x80(ltw=1,piecewise=0,seamless=0,reserved=00000)
	// LTW: valid(1)=1, offset(15)=0x1234
	//   1_001001000110100 -> bytes: 0x92, 0x34
	data := []byte{0x05, 0x01, 0x03, 0x80, 0x92, 0x34}

	af := NewAdaptationField()
	af.Append(data)
	if _, err := af.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if af.adaptationFieldExtensionFlag != 1 {
		t.Errorf("expected adaptationFieldExtensionFlag=1, got %d", af.adaptationFieldExtensionFlag)
	}
	if af.ltwFlag != 1 {
		t.Errorf("expected ltwFlag=1, got %d", af.ltwFlag)
	}
	if af.ltwValidFlag != 1 {
		t.Errorf("expected ltwValidFlag=1, got %d", af.ltwValidFlag)
	}
	if af.ltwOffset != 0x1234 {
		t.Errorf("expected ltwOffset=0x1234, got 0x%04X", af.ltwOffset)
	}
}

func TestAdaptationFieldParseExtensionPiecewiseRate(t *testing.T) {
	// adaptationFieldExtensionFlag=1, piecewiseRate=1
	// Extension: ext_len=4, flags=0x40(ltw=0,piecewise=1,seamless=0,reserved=00000)
	// Piecewise: reserved(2)+rate(22)=24 bits; rate=100000
	// 100000 in 22 bits: 0000011000011010100000 (padded)
	// Full 24 bits with reserved=00: 00_0000011000011010100000
	// Bytes: 0x01, 0x86, 0xA0
	data := []byte{0x06, 0x01, 0x04, 0x40, 0x01, 0x86, 0xA0}

	af := NewAdaptationField()
	af.Append(data)
	if _, err := af.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if af.piecewiseRateFlag != 1 {
		t.Errorf("expected piecewiseRateFlag=1, got %d", af.piecewiseRateFlag)
	}
	if af.piecewiseRate != 100000 {
		t.Errorf("expected piecewiseRate=100000, got %d", af.piecewiseRate)
	}
}

func TestAdaptationFieldParseExtensionSeamlessSplice(t *testing.T) {
	// adaptationFieldExtensionFlag=1, seamlessSplice=1
	// Extension: ext_len=6, flags=0x20(ltw=0,piecewise=0,seamless=1,reserved=00000)
	// Seamless splice: spliceType(4)=3, first(3)=2, marker, second(15)=100, marker, third(15)=200, marker
	// dtsNextAu = 2<<30 | 100<<15 | 200 = 0x803200C8
	// Bits: 0011 010 1 000000001100100 1 000000011001000 1
	// Bytes: 0x35, 0x00, 0xC9, 0x01, 0x91
	data := []byte{0x08, 0x01, 0x06, 0x20, 0x35, 0x00, 0xC9, 0x01, 0x91}

	af := NewAdaptationField()
	af.Append(data)
	if _, err := af.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if af.seamlessSpliceFlag != 1 {
		t.Errorf("expected seamlessSpliceFlag=1, got %d", af.seamlessSpliceFlag)
	}
	if af.spliceType != 3 {
		t.Errorf("expected spliceType=3, got %d", af.spliceType)
	}
	if af.dtsNextAu != 0x803200C8 {
		t.Errorf("expected dtsNextAu=0x803200C8, got 0x%08X", af.dtsNextAu)
	}
}

func TestAdaptationFieldDumpPcr(t *testing.T) {
	// Parse AF with PCR, then call DumpPcr with prevPcr=0 and prevPcr!=0
	data := []byte{0x07, 0x10, 0x00, 0x00, 0x00, 0x32, 0x7E, 0x32}
	af := NewAdaptationField()
	af.Append(data)
	if _, err := af.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	// Should not panic
	af.DumpPcr(0)
	af.DumpPcr(1000)
}

func TestAdaptationFieldDump(t *testing.T) {
	// Parse AF with multiple flags set and call Dump to verify no panic
	// Flags byte: disc=0,random=0,priority=0,pcr=1,opcr=0,splicing=0,private=0,ext=0 = 0x10
	// Just use a simple PCR-only AF for the Dump test
	data := []byte{
		0x07,                                     // adaptation_field_length=7
		0x10,                                     // flags: pcrFlag=1
		0x00, 0x00, 0x00, 0x32, 0x7E, 0x32,      // PCR data (base=100, ext=50)
	}
	af := NewAdaptationField()
	af.Append(data)
	if _, err := af.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	// Should not panic
	af.Dump()

	// Also test Dump with adaptationFieldLength=0
	af2 := NewAdaptationField()
	af2.Append([]byte{0x00})
	if _, err := af2.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	af2.Dump()
}

func TestAdaptationFieldDumpAllFlags(t *testing.T) {
	data := []byte{
		0x1C,                                           // adaptation_field_length=28
		0xFF,                                           // all flags: disc=1,random=1,priority=1,pcr=1,opcr=1,splice=1,private=1,ext=1
		0x00, 0x00, 0x00, 0x32, 0x7E, 0x32,            // PCR base=100, ext=50
		0x00, 0x00, 0x00, 0x32, 0x7E, 0x32,            // OPCR base=100, ext=50
		0xAB,                                           // splice_countdown
		0x01, 0xDD,                                     // private: length=1, data=0xDD
		0x0B,                                           // extension length=11
		0xE0,                                           // ext flags: ltw=1, piecewise=1, seamless=1, reserved=00000
		0x92, 0x34,                                     // LTW: valid=1, offset=0x1234
		0x01, 0x86, 0xA0,                               // piecewise: reserved=00, rate=100000
		0x35, 0x00, 0xC9, 0x01, 0x91,                   // seamless: spliceType=3, dtsNextAu parts
	}
	af := NewAdaptationField()
	af.Append(data)
	if _, err := af.Parse(); err != nil {
		t.Fatalf("full buffer parse should succeed: %s", err)
	}
	// Call Dump to cover all Dump branches (PCR, OPCR, splice, private, extension with LTW+piecewise+seamless)
	af.Dump()
}

func TestAdaptationFieldParseErrors(t *testing.T) {
	valid := []byte{
		0x1C,                                           // adaptation_field_length=28
		0xFF,                                           // all flags
		0x00, 0x00, 0x00, 0x32, 0x7E, 0x32,            // PCR
		0x00, 0x00, 0x00, 0x32, 0x7E, 0x32,            // OPCR
		0xAB,                                           // splice_countdown
		0x01, 0xDD,                                     // private data
		0x0B,                                           // extension length
		0xE0,                                           // ext flags
		0x92, 0x34,                                     // LTW
		0x01, 0x86, 0xA0,                               // piecewise
		0x35, 0x00, 0xC9, 0x01, 0x91,                   // seamless
	}
	// Full parse should succeed
	af := NewAdaptationField()
	af.Append(valid)
	if _, err := af.Parse(); err != nil {
		t.Fatalf("full buffer parse should succeed: %s", err)
	}
	// Truncated parses should fail (i=0: empty buf, i=1: flags byte missing, etc.)
	for i := 0; i < len(valid); i++ {
		af := NewAdaptationField()
		af.Append(valid[:i])
		_, err := af.Parse()
		if err == nil {
			t.Errorf("expected error for truncated buffer of length %d, got nil", i)
		}
	}
}
