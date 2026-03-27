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

func TestPesInitialize(t *testing.T) {
	p1 := NewPes()
	p1.Initialize(1, 2, 3, 4)

	if p1.pid != 1 {
		t.Errorf("actual: 1, But got %d", p1.pid)
	}
	if p1.pos != 2 {
		t.Errorf("actual: 2, But got %d", p1.pos)
	}
	if p1.prevPcr != 3 {
		t.Errorf("actual: 3, But got %d", p1.prevPcr)
	}
	if p1.prevPcrPos != 4 {
		t.Errorf("actual: 4, But got %d", p1.prevPcrPos)
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
	p2.Initialize(1, 2, 3, 4)

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
		0xE0,             // stream_id (video)
		0x00, 0x00,       // pes_packet_length
		0x80,             // '10' + flags=0
		0x80,             // ptsDtsFlags=10, others=0
		0x05,             // pes_header_data_length
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
		0xBC,             // stream_id
		0x00, 0x04,       // pes_packet_length=4
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

func TestPesParseEscr(t *testing.T) {
	// escrFlag=1, escrBase=45000
	// first=0, second=1, third=12232
	// ESCR bits (37): reserved(2)=00, first(3)=000, marker=1, second(15)=000000000000001, marker=1, third(15)=010111111001000
	// Bytes: 0x04, 0x00, 0x0D, 0x7E, 0x40
	data := []byte{
		0x00, 0x00, 0x01, // start code prefix
		0xE0,             // stream_id
		0x00, 0x00,       // pes_packet_length
		0x80,             // '10' + flags=0
		0x20,             // ptsDts=00, escr=1, others=0
		0x05,             // pes_header_data_length
		0x04, 0x00, 0x0D, 0x7E, 0x40, // ESCR data
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
}

func TestPesParseEsRate(t *testing.T) {
	// esRateFlag=1, esRate=50000
	// ES rate: marker(1)=1, rate(22)=50000, marker(1)=1
	// Bytes: 0x81, 0x86, 0xA1
	data := []byte{
		0x00, 0x00, 0x01, // start code prefix
		0xE0,             // stream_id
		0x00, 0x00,       // pes_packet_length
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
	// Should not panic; prevPcrPos==nextPcrPos so no delay calculation
	pes.DumpTimestamp()

	// Test with PCR interpolation (nextPcrPos != prevPcrPos)
	pes2 := NewPes()
	pes2.Append(data)
	pes2.prevPcr = 1000
	pes2.nextPcr = 2000
	pes2.prevPcrPos = 0
	pes2.nextPcrPos = 100
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
	pes.prevPcr = 1000
	pes.nextPcr = 2000
	pes.prevPcrPos = 0
	pes.nextPcrPos = 100
	pes.pos = 50
	if err := pes.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	// ptsDtsFlags=3 so DTS delay path is exercised
	pes.DumpTimestamp()

	// Without PCR interpolation (prevPcrPos == nextPcrPos)
	pes2 := NewPes()
	pes2.Append(data)
	if err := pes2.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	pes2.DumpTimestamp()
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
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x20, 0x05, 0x04, 0x00, 0x0D, 0x7E, 0x40},
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
