package tsparser

import (
	"math"
	"reflect"
	"testing"
)

func TestNewPes(t *testing.T) {
	pes := NewPes()
	if _, ok := interface{}(pes).(*Pes); !ok {
		t.Errorf("actual: *tsparser.Pat, But got %s", reflect.TypeOf(pes))
	}
}

func TestPesInitialize(t *testing.T) {
	prev := pcrAnchor{valid: true, pcr: 3, unwrapped: 3, pos: 4}
	p1 := NewPes()
	p1.initialize(1, 2, prev)

	if p1.pid != 1 {
		t.Errorf("actual: 1, But got %d", p1.pid)
	}
	if p1.pos != 2 {
		t.Errorf("actual: 2, But got %d", p1.pos)
	}
	if p1.prevAnchor != prev || p1.nextAnchor.valid {
		t.Errorf("prevAnchor = %+v, nextAnchor = %+v", p1.prevAnchor, p1.nextAnchor)
	}

	data := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x84, 0xC0, 0x0A, 0x31, 0x00, 0x01, 0xC7, 0x3F, 0x11, 0x00,
		0x01, 0xAF, 0xC9, 0x00, 0x00, 0x00, 0x01, 0x09, 0x10, 0x00, 0x00, 0x00, 0x01, 0x67, 0x4D, 0x40,
		0x1F, 0x96, 0x56, 0x05, 0xA1, 0xED, 0x82, 0xA8, 0x40, 0x00, 0x00, 0xFA, 0x40, 0x00, 0x3A, 0x98,
	}
	p2 := NewPes()
	p2.Append(data)
	if err := p2.Parse(); err != nil {
		t.Errorf("Parse error: %s", err)
	}
	p2.nextAnchor = pcrAnchor{valid: true}
	p2.initialize(1, 2, prev)

	if !reflect.DeepEqual(p1, p2) {
		t.Errorf("Failed Initialize. Different in p1 and p2")
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
	err = err || pes.pesExtensionFlag != 0x00
	err = err || pes.pts != 0x639F
	err = err || pes.dts != 0x57E4
	if err {
		t.Errorf("Parse error")
	}
}

func TestPesParsePtsOnly(t *testing.T) {
	// PTS=90000 (1 second at 90kHz), ptsDtsFlags=2
	// PTS: first=0, second=2, third=24464
	// PTS bytes: 0x21, 0x00, 0x05, 0xBF, 0x21
	data := []byte{
		0x00, 0x00, 0x01, // start code prefix
		0xE0,       // stream_id (video)
		0x00, 0x00, // pes_packet_length
		0x80,                         // '10' + flags=0
		0x80,                         // ptsDtsFlags=10, others=0
		0x05,                         // pes_header_data_length
		0x21, 0x00, 0x05, 0xBF, 0x21, // PTS data
	}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if pes.ptsDtsFlags != 2 {
		t.Errorf("expected ptsDtsFlags=2, got %d", pes.ptsDtsFlags)
	}
	if pes.pts != 90000 {
		t.Errorf("expected pts=90000, got %d", pes.pts)
	}
}

func TestPesParseSpecialStreamId(t *testing.T) {
	// stream_id=0xBC (program_stream_map), should skip header parsing
	data := []byte{
		0x00, 0x00, 0x01, // start code prefix
		0xBC,       // stream_id
		0x00, 0x04, // pes_packet_length=4
		0xAA, 0xBB, 0xCC, 0xDD, // data
	}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if pes.streamID != 0xBC {
		t.Errorf("expected streamID=0xBC, got 0x%02X", pes.streamID)
	}
	if pes.pesPacketLength != 4 {
		t.Errorf("expected pesPacketLength=4, got %d", pes.pesPacketLength)
	}
}

func TestPesParseInvalidStartCode(t *testing.T) {
	// packet_start_code_prefix must be 0x000001.
	data := []byte{0x00, 0x00, 0x02, 0xE0, 0x00, 0x00}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err == nil {
		t.Error("expected error for invalid packet_start_code_prefix, got nil")
	}
}

func TestPesParseSpecialStreamIdIgnoresBodyLength(t *testing.T) {
	// A stream without an optional header has nothing after
	// PES_packet_length that the header check reads, so a body shorter than
	// PES_packet_length (the PES was cut off) is not a header problem.
	for _, streamID := range []byte{0xBC, 0xBE, 0xBF, 0xF0, 0xF1, 0xF2, 0xF8, 0xFF} {
		pes := NewPes()
		pes.Append([]byte{0x00, 0x00, 0x01, streamID, 0x00, 0x64}) // length=100, no payload
		if err := pes.Parse(); err != nil {
			t.Errorf("stream_id 0x%02X: Parse() = %v, want nil", streamID, err)
		}
	}
}

func TestPesParseEscr(t *testing.T) {
	// escrFlag=1, escrBase=45000 (first=0, second=1, third=12232), escrExtension=100.
	// ESCR is 48 bits: reserved(2) base(3)+m base(15)+m base(15)+m extension(9)+m.
	// Bytes: 0x04, 0x00, 0x0D, 0x7E, 0x44, 0xC9
	data := []byte{
		0x00, 0x00, 0x01, // start code prefix
		0xE0,       // stream_id
		0x00, 0x00, // pes_packet_length
		0x80,                               // '10' + flags=0
		0x20,                               // ptsDts=00, escr=1, others=0
		0x06,                               // pes_header_data_length
		0x04, 0x00, 0x0D, 0x7E, 0x44, 0xC9, // ESCR data (6 bytes)
	}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if pes.escrFlag != 1 {
		t.Errorf("expected escrFlag=1, got %d", pes.escrFlag)
	}
	if pes.escrBase != 45000 {
		t.Errorf("expected escrBase=45000, got %d", pes.escrBase)
	}
	if pes.escrExtension != 100 {
		t.Errorf("expected escrExtension=100, got %d", pes.escrExtension)
	}
}

func TestPesParseEsRate(t *testing.T) {
	// esRateFlag=1, esRate=50000
	// ES rate: marker(1)=1, rate(22)=50000, marker(1)=1
	// Bytes: 0x81, 0x86, 0xA1
	data := []byte{
		0x00, 0x00, 0x01, // start code prefix
		0xE0,       // stream_id
		0x00, 0x00, // pes_packet_length
		0x80,             // '10' + flags=0
		0x10,             // ptsDts=00, escr=0, esRate=1, others=0
		0x03,             // pes_header_data_length
		0x81, 0x86, 0xA1, // ES rate data
	}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if pes.esRateFlag != 1 {
		t.Errorf("expected esRateFlag=1, got %d", pes.esRateFlag)
	}
	if pes.esRate != 50000 {
		t.Errorf("expected esRate=50000, got %d", pes.esRate)
	}
}

func TestPesParseTrickModeFastForward(t *testing.T) {
	// dsmTrickModeFlag=1, control=0x00 (fast_forward)
	// Trick byte: 000_01_1_10 = 0x0E
	// fieldID=1, intraSliceRefresh=1, frequencyTruncation=2
	data := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x08, 0x01, 0x0E,
	}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if pes.trickModeControl != 0x00 {
		t.Errorf("expected trickModeControl=0, got %d", pes.trickModeControl)
	}
	if pes.fieldID != 1 {
		t.Errorf("expected fieldID=1, got %d", pes.fieldID)
	}
	if pes.intraSliceRefresh != 1 {
		t.Errorf("expected intraSliceRefresh=1, got %d", pes.intraSliceRefresh)
	}
	if pes.frequencyTruncation != 2 {
		t.Errorf("expected frequencyTruncation=2, got %d", pes.frequencyTruncation)
	}
}

func TestPesParseTrickModeSlowMotion(t *testing.T) {
	// dsmTrickModeFlag=1, control=0x01 (slow_motion)
	// Trick byte: 001_10101 = 0x35, repCntrl=21
	data := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x08, 0x01, 0x35,
	}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if pes.trickModeControl != 1 {
		t.Errorf("expected trickModeControl=1, got %d", pes.trickModeControl)
	}
	if pes.repCntrl != 21 {
		t.Errorf("expected repCntrl=21, got %d", pes.repCntrl)
	}
}

func TestPesParseTrickModeDefault(t *testing.T) {
	// dsmTrickModeFlag=1, control=0x02 (default path, skip 5 bits)
	// Trick byte: 010_00000 = 0x40
	data := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x08, 0x01, 0x40,
	}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if pes.trickModeControl != 2 {
		t.Errorf("expected trickModeControl=2, got %d", pes.trickModeControl)
	}
}

func TestPesParseAdditionalCopyInfo(t *testing.T) {
	// additionalCopyInfoFlag=1, info=0x55
	// Byte: marker(1)=1, info(7)=0x55=1010101 -> 1_1010101 = 0xD5
	data := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x04, 0x01, 0xD5,
	}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if pes.additionalCopyInfoFlag != 1 {
		t.Errorf("expected additionalCopyInfoFlag=1, got %d", pes.additionalCopyInfoFlag)
	}
	if pes.additionalCopyInfo != 0x55 {
		t.Errorf("expected additionalCopyInfo=0x55, got 0x%02X", pes.additionalCopyInfo)
	}
}

func TestPesParsePesCrc(t *testing.T) {
	// pesCrcFlag=1, CRC=0xABCD
	data := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x02, 0x02, 0xAB, 0xCD,
	}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if pes.pesCrcFlag != 1 {
		t.Errorf("expected pesCrcFlag=1, got %d", pes.pesCrcFlag)
	}
	if pes.previousPesPacketCrc != 0xABCD {
		t.Errorf("expected previousPesPacketCrc=0xABCD, got 0x%04X", pes.previousPesPacketCrc)
	}
}

func TestPesDumpTimestamp(t *testing.T) {
	// Test DumpTimestamp with ptsDtsFlags=2 (PTS only), no PCR interpolation
	data := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05,
		0x21, 0x00, 0x05, 0xBF, 0x21, // PTS=90000
	}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	// Should not panic; no PCR reference so no delay calculation
	pes.DumpTimestamp()

	// Test with PCR interpolation
	pes2 := NewPes()
	pes2.Append(data)
	pes2.prevAnchor = pcrAnchor{valid: true, pcr: 1000, unwrapped: 1000, pos: 0}
	pes2.nextAnchor = pcrAnchor{valid: true, pcr: 2000, unwrapped: 2000, pos: 100}
	pes2.pos = 50
	if err := pes2.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	pes2.DumpTimestamp()
}

func TestPesDumpTimestampPtsDtsWithPcr(t *testing.T) {
	// PES with PTS+DTS (ptsDtsFlags=3)
	data := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x84, 0xC0, 0x0A, 0x31, 0x00, 0x01, 0xC7, 0x3F, 0x11, 0x00,
		0x01, 0xAF, 0xC9, 0x00, 0x00, 0x00, 0x01, 0x09, 0x10, 0x00, 0x00, 0x00, 0x01, 0x67, 0x4D, 0x40,
		0x1F, 0x96, 0x56, 0x05, 0xA1, 0xED, 0x82, 0xA8, 0x40, 0x00, 0x00, 0xFA, 0x40, 0x00, 0x3A, 0x98,
	}
	// With PCR interpolation
	pes := NewPes()
	pes.Append(data)
	pes.prevAnchor = pcrAnchor{valid: true, pcr: 1000, unwrapped: 1000, pos: 0}
	pes.nextAnchor = pcrAnchor{valid: true, pcr: 2000, unwrapped: 2000, pos: 100}
	pes.pos = 50
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	// ptsDtsFlags=3 so DTS delay path is exercised
	pes.DumpTimestamp()

	// Without a PCR reference
	pes2 := NewPes()
	pes2.Append(data)
	if err := pes2.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	pes2.DumpTimestamp()
}

// TestPesDumpTimestampNoFollowingPcr guards the fix for a spurious PCR-PTS gap:
// when a PES (e.g. the final one before EOF) has no following PCR, the delay
// must fall back to the most recent PCR rather than interpolate against an
// unknown PCR, which used to yield a wildly wrong value.
func TestPesDumpTimestampNoFollowingPcr(t *testing.T) {
	// PTS=90000 (1000ms). prevPcr=24300000 (900ms), no following PCR.
	data := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05,
		0x21, 0x00, 0x05, 0xBF, 0x21,
	}
	pes := NewPes()
	pes.Append(data)
	pes.prevAnchor = pcrAnchor{valid: true, pcr: 24300000, unwrapped: 24300000, pos: 500}
	pes.pos = 600 // no following PCR captured
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	got := pes.DumpTimestamp()
	if math.Abs(got-100.0) > 1e-6 {
		t.Errorf("delay: expected fallback to prevPcr (100ms), got %fms", got)
	}
}

// TestPesDumpTimestampNoPtsDts covers a PES that carries neither PTS nor DTS.
func TestPesDumpTimestampNoPtsDts(t *testing.T) {
	// ptsDtsFlags=0, pes_header_data_length=0.
	data := []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if got := pes.DumpTimestamp(); got != 0 {
		t.Errorf("delay: expected 0 for a PES without PTS/DTS, got %fms", got)
	}
}

func TestPesTimestampDelay(t *testing.T) {
	pes := &Pes{ptsDtsFlags: 2, pts: 90_000, prevAnchor: pcrAnchor{valid: true, pcr: 24_300_000, unwrapped: 24_300_000}}
	if got, ok := pes.TimestampDelay(); !ok || math.Abs(got-100) > 1e-6 {
		t.Errorf("PTS delay = %f, %v; want 100ms, true", got, ok)
	}
	pes.ptsDtsFlags = 3
	pes.dts = 99_000
	if got, ok := pes.TimestampDelay(); !ok || math.Abs(got-200) > 1e-6 {
		t.Errorf("DTS delay = %f, %v; want 200ms, true", got, ok)
	}
	pes.ptsDtsFlags = 0
	if _, ok := pes.TimestampDelay(); ok {
		t.Error("PES without PTS/DTS must not produce a delay")
	}
	pes.ptsDtsFlags = 2
	pes.prevAnchor.valid = false
	if _, ok := pes.TimestampDelay(); ok {
		t.Error("PES without a PCR reference must not produce a delay")
	}
	if _, ok := pes.BracketedTimestampDelay(); ok {
		t.Error("PES without bracketing PCR observations must not produce a compliance delay")
	}
	pes.prevAnchor = pcrAnchor{valid: true, pcr: 24_300_000, unwrapped: 24_300_000, pos: 0}
	pes.nextAnchor = pcrAnchor{valid: true, pcr: 27_000_000, unwrapped: 27_000_000, pos: 100}
	pes.pos = 50
	if got, ok := pes.BracketedTimestampDelay(); !ok || math.Abs(got-50) > 1e-6 {
		t.Errorf("bracketed delay = %f, %v; want 50ms, true", got, ok)
	}
	if got, ok := pes.TimestampDelay(); !ok || math.Abs(got-50) > 1e-6 {
		t.Errorf("delay with a following PCR = %f, %v; want the bracketed 50ms, true", got, ok)
	}
	// The PCR after the start of the PES packet starts a new timing segment
	// (a discontinuity or a PCR_PID change): no delay, not even against the
	// previous PCR alone.
	pes.nextAnchor.segment = 1
	if _, ok := pes.TimestampDelay(); ok {
		t.Error("PES whose PCRs lie in different segments must not produce a delay")
	}
	if pes.hasPcrReference() {
		t.Error("PES whose PCRs lie in different segments has no PCR reference")
	}
}

// TestPesTimestampDelayWraparound covers the delay across the 2^33 periods of
// the PTS/DTS and the PCR, in both orders of the wraps, bracketed across a PCR
// wraparound and against the previous PCR alone.
func TestPesTimestampDelayWraparound(t *testing.T) {
	const wrap90 = uint64(1) << 33
	const ms = 27_000 // 27 MHz ticks
	// The PCR before the PES packet is 30 ms before the wrap, the PCR after
	// it 10 ms after the wrap (unwrapped within the segment), and the PES
	// packet lies halfway between them: 10 ms before the wrap.
	prev := pcrAnchor{valid: true, pcr: pcrWrapTicks - 30*ms, unwrapped: pcrWrapTicks - 30*ms, pos: 0}
	next := pcrAnchor{valid: true, pcr: 10 * ms, unwrapped: pcrWrapTicks + 10*ms, pos: 1000}
	for _, tc := range []struct {
		name       string
		next       pcrAnchor
		stamp      uint64
		wantMs     float64
		bracketed  bool
		wantPrevMs float64
	}{
		{"PTS wrapped, PCR not, across the PCR wrap", next, 690 * 90, 700, true, 0},
		{"neither wrapped, across the PCR wrap", next, wrap90 - 5*90, 5, true, 0},
		{"PCR wrapped first, negative lead, across the PCR wrap", next, wrap90 - 15*90, -5, true, 0},
		{"PTS wrapped, PCR not, previous PCR only", pcrAnchor{}, 690 * 90, 720, false, 720},
		{"PCR wrapped first, previous PCR only", pcrAnchor{}, wrap90 - 40*90, -10, false, -10},
	} {
		pes := &Pes{ptsDtsFlags: 2, pts: tc.stamp, pos: 500, prevAnchor: prev, nextAnchor: tc.next}
		got, ok := pes.BracketedTimestampDelay()
		if ok != tc.bracketed || tc.bracketed && math.Abs(got-tc.wantMs) > 1e-6 {
			t.Errorf("%s: bracketed delay = %f, %v; want %f, %v", tc.name, got, ok, tc.wantMs, tc.bracketed)
		}
		if got, ok := pes.TimestampDelay(); !ok || math.Abs(got-tc.wantMs) > 1e-6 {
			t.Errorf("%s: delay = %f, %v; want %f, true", tc.name, got, ok, tc.wantMs)
		}
	}
	// After the PCR wrap, a PTS that has not wrapped yet (negative lead) is a
	// small negative delay, not a delay of about +26.5 hours.
	after := pcrAnchor{valid: true, pcr: 10 * ms, unwrapped: pcrWrapTicks + 10*ms, pos: 1000}
	later := pcrAnchor{valid: true, pcr: 50 * ms, unwrapped: pcrWrapTicks + 50*ms, pos: 2000}
	pes := &Pes{ptsDtsFlags: 3, pts: 30 * 90, dts: wrap90 - 20*90, pos: 1500, prevAnchor: after, nextAnchor: later}
	if got, ok := pes.BracketedTimestampDelay(); !ok || math.Abs(got+50) > 1e-6 {
		t.Errorf("PCR wrapped before DTS: delay = %f, %v; want -50ms, true", got, ok)
	}
}

func TestPesParseErrors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			"ptsDtsFlags=3",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x84, 0xC0, 0x0A, 0x31, 0x00, 0x01, 0xC7, 0x3F, 0x11, 0x00, 0x01, 0xAF, 0xC9},
		},
		{
			"ptsDtsFlags=2",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05, 0x21, 0x00, 0x05, 0xBF, 0x21},
		},
		{
			"escr",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x20, 0x06, 0x04, 0x00, 0x0D, 0x7E, 0x44, 0xC9},
		},
		{
			"esRate",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x10, 0x03, 0x81, 0x86, 0xA1},
		},
		{
			"trickMode",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x08, 0x01, 0x0E},
		},
		{
			"additionalCopyInfo",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x04, 0x01, 0xD5},
		},
		{
			"pesCrc",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x02, 0x02, 0xAB, 0xCD},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Full parse should succeed
			pes := NewPes()
			pes.Append(tt.data)
			if err := pes.Parse(); err != nil {
				t.Fatalf("full buffer parse should succeed: %s", err)
			}
			// Truncated parses should return errors (i=0: empty buf covers first read error)
			for i := 0; i < len(tt.data); i++ {
				pes := NewPes()
				pes.Append(tt.data[:i])
				if err := pes.Parse(); err == nil {
					t.Errorf("expected error for truncated %s buffer of length %d", tt.name, i)
				}
			}
		})
	}
}

func TestPesDumpHeader(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05,
		0x21, 0x00, 0x05, 0xBF, 0x21,
	}
	pes := NewPes()
	pes.Append(data)
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	// Should not panic
	pes.DumpHeader()
}

func TestPesAppendRetainsAtMostTheHeader(t *testing.T) {
	// A PES longer than the largest possible header keeps only its first
	// maxPesHeaderSize bytes but counts every byte it received.
	payload := make([]byte, tsPayloadSize-tsHeaderSize)
	for i := range payload {
		payload[i] = byte(i)
	}
	pes := NewPes()
	pes.initialize(0x100, 0, pcrAnchor{})
	const chunks = 10
	for range chunks {
		pes.Append(payload)
	}
	if len(pes.buf) != maxPesHeaderSize || cap(pes.buf) != maxPesHeaderSize {
		t.Errorf("len/cap(buf) = %d/%d, want %d", len(pes.buf), cap(pes.buf), maxPesHeaderSize)
	}
	if want := int64(chunks * len(payload)); pes.received != want {
		t.Errorf("received = %d, want %d", pes.received, want)
	}
	for i, got := range pes.buf {
		if want := payload[i%len(payload)]; got != want {
			t.Fatalf("buf[%d] = 0x%02x, want 0x%02x", i, got, want)
		}
	}
	pes.initialize(0x100, 0, pcrAnchor{})
	if len(pes.buf) != 0 || pes.received != 0 {
		t.Errorf("after Initialize len(buf) = %d, received = %d", len(pes.buf), pes.received)
	}
}
