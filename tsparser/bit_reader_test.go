package tsparser

import (
	"errors"
	"testing"
)

// mockBitReader delegates to a real bitReader but fails on the Nth Read/Skip call.
type mockBitReader struct {
	real    bitReader
	callNum int
	failAt  int // 1-indexed: fail on this call number
}

func newMockFactory(failAt int) func() bitReader {
	return func() bitReader {
		return &mockBitReader{real: newDefaultBitReader(), failAt: failAt}
	}
}

func (m *mockBitReader) Set(b []byte)          { m.real.Set(b) }
func (m *mockBitReader) Skip(n uint32) error {
	m.callNum++
	if m.callNum == m.failAt {
		return errors.New("mock error")
	}
	return m.real.Skip(n)
}
func (m *mockBitReader) ReadUint8(n uint32) (uint8, error) {
	m.callNum++
	if m.callNum == m.failAt {
		return 0, errors.New("mock error")
	}
	return m.real.ReadUint8(n)
}
func (m *mockBitReader) ReadUint16(n uint32) (uint16, error) {
	m.callNum++
	if m.callNum == m.failAt {
		return 0, errors.New("mock error")
	}
	return m.real.ReadUint16(n)
}
func (m *mockBitReader) ReadUint32(n uint32) (uint32, error) {
	m.callNum++
	if m.callNum == m.failAt {
		return 0, errors.New("mock error")
	}
	return m.real.ReadUint32(n)
}
func (m *mockBitReader) ReadUint64(n uint32) (uint64, error) {
	m.callNum++
	if m.callNum == m.failAt {
		return 0, errors.New("mock error")
	}
	return m.real.ReadUint64(n)
}

// TestAdaptationFieldParseMockErrors tests all error paths in AF.Parse using mock BitReader.
// With all flags set, there are 34 Read/Skip calls.
func TestAdaptationFieldParseMockErrors(t *testing.T) {
	data := []byte{
		0x1C, 0xFF,
		0x00, 0x00, 0x00, 0x32, 0x7E, 0x32, // PCR
		0x00, 0x00, 0x00, 0x32, 0x7E, 0x32, // OPCR
		0xAB,                               // splice
		0x01, 0xDD,                         // private data
		0x0B, 0xE0,                         // ext length + flags
		0x92, 0x34,                         // LTW
		0x01, 0x86, 0xA0,                   // piecewise
		0x35, 0x00, 0xC9, 0x01, 0x91,       // seamless
	}

	// Verify full parse succeeds
	af := NewAdaptationField()
	af.Append(data)
	if _, err := af.Parse(); err != nil {
		t.Fatalf("full parse should succeed: %s", err)
	}

	for failAt := 1; failAt <= 34; failAt++ {
		af := NewAdaptationField()
		af.Append(data)
		af.newBitReader = newMockFactory(failAt)
		_, err := af.Parse()
		if err == nil {
			t.Errorf("expected error for failAt=%d, got nil", failAt)
		}
	}
}

// TestPatParseMockErrors tests all error paths in Pat.Parse.
// Single program PAT: 15 Read/Skip calls.
func TestPatParseMockErrors(t *testing.T) {
	data := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}

	pat := NewPat()
	pat.Append(data)
	if err := pat.Parse(); err != nil {
		t.Fatalf("full parse should succeed: %s", err)
	}

	for failAt := 1; failAt <= 15; failAt++ {
		pat := NewPat()
		pat.Append(data)
		pat.newBitReader = newMockFactory(failAt)
		if err := pat.Parse(); err == nil {
			t.Errorf("expected error for failAt=%d, got nil", failAt)
		}
	}
}

// TestPatParseMockErrorsNetworkPid tests error path for network PID read.
// PAT with programNumber=0: call 14 is ReadUint16(13) for networkPid.
func TestPatParseMockErrorsNetworkPid(t *testing.T) {
	header := []byte{
		0x00, 0xB0, 0x11, 0x00, 0x3F, 0xC1, 0x00, 0x00,
		0x00, 0x00, 0xE0, 0x10,
		0x00, 0x01, 0xE0, 0x3F,
	}
	crc := crc32(header)
	data := append(header, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))

	pat := NewPat()
	pat.Append(data)
	if err := pat.Parse(); err != nil {
		t.Fatalf("full parse should succeed: %s", err)
	}

	// Call 14 is the networkPid ReadUint16(13) for programNumber=0
	pat2 := NewPat()
	pat2.Append(data)
	pat2.newBitReader = newMockFactory(14)
	if err := pat2.Parse(); err == nil {
		t.Errorf("expected error for networkPid read, got nil")
	}
}

// TestPmtParseMockErrors tests all error paths in Pmt.Parse.
// PMT with 3 streams, no descriptors: 16 + 6*3 + 1 = 35 calls.
func TestPmtParseMockErrors(t *testing.T) {
	data := []byte{0x02, 0xB0, 0x1C, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00,
		0x1B, 0xE0, 0x31, 0xF0, 0x00,
		0x0F, 0xE0, 0x64, 0xF0, 0x00,
		0x0F, 0xE0, 0x98, 0xF0, 0x00,
		0x3D, 0xFE, 0xAE, 0x61}

	pmt := NewPmt()
	pmt.Append(data)
	if err := pmt.Parse(); err != nil {
		t.Fatalf("full parse should succeed: %s", err)
	}

	for failAt := 1; failAt <= 35; failAt++ {
		pmt := NewPmt()
		pmt.Append(data)
		pmt.newBitReader = newMockFactory(failAt)
		if err := pmt.Parse(); err == nil {
			t.Errorf("expected error for failAt=%d, got nil", failAt)
		}
	}
}

// TestPesParseMockErrors tests error paths in Pes.Parse for various flag combinations.
func TestPesParseMockErrors(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		maxCalls int
	}{
		{
			"ptsDtsFlags=3",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x84, 0xC0, 0x0A,
				0x31, 0x00, 0x01, 0xC7, 0x3F, 0x11, 0x00, 0x01, 0xAF, 0xC9},
			31, // 17 header + 14 PTS/DTS
		},
		{
			"ptsDtsFlags=2",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05,
				0x21, 0x00, 0x05, 0xBF, 0x21},
			24, // 17 header + 7 PTS
		},
		{
			"escr",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x20, 0x05,
				0x04, 0x00, 0x0D, 0x7E, 0x40},
			23, // 17 header + 6 ESCR
		},
		{
			"esRate",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x10, 0x03,
				0x81, 0x86, 0xA1},
			20, // 17 header + 3 ES rate
		},
		{
			"trickFastForward",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x08, 0x01, 0x0E},
			21, // 17 header + 4 trick mode
		},
		{
			"trickSlow",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x08, 0x01, 0x35},
			19, // 17 header + 2 trick mode
		},
		{
			"trickDefault",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x08, 0x01, 0x40},
			19, // 17 header + 2 trick mode (control + skip)
		},
		{
			"additionalCopyInfo",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x04, 0x01, 0xD5},
			19, // 17 header + 2 copy info
		},
		{
			"pesCrc",
			[]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x02, 0x02, 0xAB, 0xCD},
			18, // 17 header + 1 CRC
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify full parse succeeds
			pes := NewPes()
			pes.Append(tt.data)
			if err := pes.Parse(); err != nil {
				t.Fatalf("full parse should succeed: %s", err)
			}
			for failAt := 1; failAt <= tt.maxCalls; failAt++ {
				pes := NewPes()
				pes.Append(tt.data)
				pes.newBitReader = newMockFactory(failAt)
				if err := pes.Parse(); err == nil {
					t.Errorf("expected error for failAt=%d, got nil", failAt)
				}
			}
		})
	}
}

// TestTsPacketParseMockErrors tests error paths in TsPacket.Parse.
// TS header: 8 Read calls.
func TestTsPacketParseMockErrors(t *testing.T) {
	data := make([]byte, 188)
	data[0] = 0x47
	data[1] = 0x40
	data[2] = 0x00
	data[3] = 0x10
	for i := 4; i < 188; i++ {
		data[i] = 0xFF
	}

	tp := NewTsPacket()
	tp.Append(data)
	if err := tp.Parse(); err != nil {
		t.Fatalf("full parse should succeed: %s", err)
	}

	for failAt := 1; failAt <= 8; failAt++ {
		tp := NewTsPacket()
		tp.Append(data)
		tp.newBitReader = newMockFactory(failAt)
		if err := tp.Parse(); err == nil {
			t.Errorf("expected error for failAt=%d, got nil", failAt)
		}
	}
}
