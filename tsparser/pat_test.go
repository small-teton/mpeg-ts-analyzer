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

func TestPatParseNetworkPid(t *testing.T) {
	// PAT with programNumber=0 (network PID) + programNumber=1 (PMT PID)
	header := []byte{
		0x00,       // table_id
		0xB0, 0x11, // ssi=1, section_length=17
		0x00, 0x3F, // transport_stream_id
		0xC1,       // reserved=11, version=0, cni=1
		0x00,       // section_number
		0x00,       // last_section_number
		0x00, 0x00, // program_number=0
		0xE0, 0x10, // reserved=111, network_pid=0x10
		0x00, 0x01, // program_number=1
		0xE0, 0x3F, // reserved=111, program_map_pid=0x3F
	}
	crc := crc32(header)
	data := append(header, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))

	pat := NewPat()
	pat.Append(data)
	if err := pat.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	if len(pat.programInfo) != 2 {
		t.Fatalf("expected 2 program infos, got %d", len(pat.programInfo))
	}
	// Network PID entry
	if pat.programInfo[0].programNumber != 0 {
		t.Errorf("expected programNumber=0, got %d", pat.programInfo[0].programNumber)
	}
	if pat.programInfo[0].networkPid != 0x10 {
		t.Errorf("expected networkPid=0x10, got 0x%04X", pat.programInfo[0].networkPid)
	}
	// Program entry
	if pat.programInfo[1].programNumber != 1 {
		t.Errorf("expected programNumber=1, got %d", pat.programInfo[1].programNumber)
	}
	if pat.programInfo[1].programMapPid != 0x3F {
		t.Errorf("expected programMapPid=0x3F, got 0x%04X", pat.programInfo[1].programMapPid)
	}
	if pat.pmtPid != 0x3F {
		t.Errorf("expected pmtPid=0x3F, got 0x%04X", pat.pmtPid)
	}
}

func TestPatDump(t *testing.T) {
	data := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53, 0xFF}
	pat := NewPat()
	pat.Append(data)
	pat.Parse()
	// Should not panic
	pat.Dump()
}

func TestPatDumpNetworkPid(t *testing.T) {
	header := []byte{
		0x00, 0xB0, 0x11, 0x00, 0x3F, 0xC1, 0x00, 0x00,
		0x00, 0x00, 0xE0, 0x10, // program_number=0, network_pid=0x10
		0x00, 0x01, 0xE0, 0x3F, // program_number=1, program_map_pid=0x3F
	}
	crc := crc32(header)
	data := append(header, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
	pat := NewPat()
	pat.Append(data)
	if err := pat.Parse(); err != nil {
		t.Fatalf("Parse error: %s", err)
	}
	// Should not panic; covers the programNumber==0 branch in Dump
	pat.Dump()
}

func TestPatParseErrors(t *testing.T) {
	valid := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	// Full parse should succeed
	pat := NewPat()
	pat.Append(valid)
	if err := pat.Parse(); err != nil {
		t.Fatalf("full buffer parse should succeed: %s", err)
	}
	// Truncated parses should fail (i=0: empty buf covers tableID read error)
	for i := 0; i < len(valid); i++ {
		pat := NewPat()
		pat.Append(valid[:i])
		if err := pat.Parse(); err == nil {
			t.Errorf("expected error for truncated buffer of length %d", i)
		}
	}
}
