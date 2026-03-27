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

func TestPmtParseWithDescriptors(t *testing.T) {
	// PMT with programInfoLength > 0 and ES info descriptors
	header := []byte{
		0x02,       // table_id
		0xB0, 0x1C, // ssi=1, section_length=28
		0x00, 0x01, // program_number=1
		0xC1,       // reserved=11, version=0, cni=1
		0x00,       // section_number
		0x00,       // last_section_number
		0xE0, 0x31, // reserved=111, PCR_PID=0x31
		0xF0, 0x02, // reserved=1111, program_info_length=2
		0xAA, 0xBB, // 2 bytes of program descriptor (skipped)
		0x1B,       // stream_type=0x1B (H.264)
		0xE0, 0x31, // reserved=111, elementary_PID=0x31
		0xF0, 0x03, // reserved=1111, ES_info_length=3
		0xCC, 0xDD, 0xEE, // 3 bytes of ES descriptor (skipped)
		0x0F,       // stream_type=0x0F (AAC)
		0xE0, 0x64, // reserved=111, elementary_PID=0x64
		0xF0, 0x00, // reserved=1111, ES_info_length=0
	}
	crc := crc32(header)
	data := append(header, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))

	pmt := NewPmt()
	pmt.Append(data)
	if err := pmt.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if pmt.programInfoLength != 2 {
		t.Errorf("expected programInfoLength=2, got %d", pmt.programInfoLength)
	}
	if len(pmt.programInfos) != 2 {
		t.Fatalf("expected 2 program infos, got %d", len(pmt.programInfos))
	}
	if pmt.programInfos[0].streamType != 0x1B {
		t.Errorf("expected streamType=0x1B, got 0x%02X", pmt.programInfos[0].streamType)
	}
	if pmt.programInfos[0].esInfoLength != 3 {
		t.Errorf("expected esInfoLength=3, got %d", pmt.programInfos[0].esInfoLength)
	}
	if pmt.programInfos[1].streamType != 0x0F {
		t.Errorf("expected streamType=0x0F, got 0x%02X", pmt.programInfos[1].streamType)
	}
	if pmt.programInfos[1].elementaryPid != 0x64 {
		t.Errorf("expected elementaryPid=0x64, got 0x%04X", pmt.programInfos[1].elementaryPid)
	}
}

func TestPmtDumpProgramInfos(t *testing.T) {
	pmt := NewPmt()
	// Add various stream types to cover the switch cases
	pmt.programInfos = []ProgramInfo{
		{streamType: 0x00, elementaryPid: 0x10, esInfoLength: 0}, // reserved
		{streamType: 0x01, elementaryPid: 0x11, esInfoLength: 0}, // 11172 video
		{streamType: 0x02, elementaryPid: 0x12, esInfoLength: 0}, // 13818-2 video
		{streamType: 0x03, elementaryPid: 0x13, esInfoLength: 0}, // 11172 audio
		{streamType: 0x0F, elementaryPid: 0x14, esInfoLength: 0}, // AAC
		{streamType: 0x1B, elementaryPid: 0x15, esInfoLength: 0}, // H.264
		{streamType: 0x7F, elementaryPid: 0x16, esInfoLength: 0}, // IPMP
		{streamType: 0x50, elementaryPid: 0x17, esInfoLength: 0}, // reserved (<=0x7E)
		{streamType: 0x90, elementaryPid: 0x18, esInfoLength: 0}, // user private (>0x7E)
	}
	// Should not panic
	pmt.DumpProgramInfos()
}

func TestPmtDump(t *testing.T) {
	data := []byte{0x02, 0xB0, 0x1C, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0, 0x00, 0x0F, 0xE0, 0x64, 0xF0, 0x00, 0x0F, 0xE0, 0x98, 0xF0, 0x00, 0x3D, 0xFE, 0xAE, 0x61, 0xFF}
	pmt := NewPmt()
	pmt.Append(data)
	pmt.Parse()
	// Should not panic
	pmt.Dump()
}

func TestPmtDumpProgramInfosAllTypes(t *testing.T) {
	pmt := NewPmt()
	pmt.programInfos = []ProgramInfo{
		{streamType: 0x04, elementaryPid: 0x20, esInfoLength: 0},
		{streamType: 0x05, elementaryPid: 0x21, esInfoLength: 0},
		{streamType: 0x06, elementaryPid: 0x22, esInfoLength: 0},
		{streamType: 0x07, elementaryPid: 0x23, esInfoLength: 0},
		{streamType: 0x08, elementaryPid: 0x24, esInfoLength: 0},
		{streamType: 0x09, elementaryPid: 0x25, esInfoLength: 0},
		{streamType: 0x0A, elementaryPid: 0x26, esInfoLength: 0},
		{streamType: 0x0B, elementaryPid: 0x27, esInfoLength: 0},
		{streamType: 0x0C, elementaryPid: 0x28, esInfoLength: 0},
		{streamType: 0x0D, elementaryPid: 0x29, esInfoLength: 0},
		{streamType: 0x0E, elementaryPid: 0x2A, esInfoLength: 0},
		{streamType: 0x10, elementaryPid: 0x2B, esInfoLength: 0},
		{streamType: 0x11, elementaryPid: 0x2C, esInfoLength: 0},
		{streamType: 0x12, elementaryPid: 0x2D, esInfoLength: 0},
		{streamType: 0x13, elementaryPid: 0x2E, esInfoLength: 0},
		{streamType: 0x14, elementaryPid: 0x2F, esInfoLength: 0},
		{streamType: 0x15, elementaryPid: 0x30, esInfoLength: 0},
		{streamType: 0x16, elementaryPid: 0x31, esInfoLength: 0},
		{streamType: 0x17, elementaryPid: 0x32, esInfoLength: 0},
		{streamType: 0x18, elementaryPid: 0x33, esInfoLength: 0},
		{streamType: 0x19, elementaryPid: 0x34, esInfoLength: 0},
		{streamType: 0x1A, elementaryPid: 0x35, esInfoLength: 0},
	}
	// Should not panic; covers all stream type switch cases 0x04-0x1A
	pmt.DumpProgramInfos()
}

func TestPmtParseErrors(t *testing.T) {
	valid := []byte{0x02, 0xB0, 0x1C, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0, 0x00, 0x0F, 0xE0, 0x64, 0xF0, 0x00, 0x0F, 0xE0, 0x98, 0xF0, 0x00, 0x3D, 0xFE, 0xAE, 0x61}
	// Full parse should succeed
	pmt := NewPmt()
	pmt.Append(valid)
	if err := pmt.Parse(); err != nil {
		t.Fatalf("full buffer parse should succeed: %s", err)
	}
	// Truncated parses should fail (i=0: empty buf covers tableID read error)
	for i := 0; i < len(valid); i++ {
		pmt := NewPmt()
		pmt.Append(valid[:i])
		if err := pmt.Parse(); err == nil {
			t.Errorf("expected error for truncated buffer of length %d", i)
		}
	}
}
