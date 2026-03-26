package tsparser

import (
	"os"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/options"
)

func TestFindPat_NotFound(t *testing.T) {
	data := make([]byte, 188*3)
	for i := range data {
		data[i] = 0xAA
	}
	_, err := findPat(data)
	if err == nil {
		t.Errorf("expected error for data with no PAT, got nil")
	}
}

func TestFindPat_AtOffset0(t *testing.T) {
	data := buildFindPatData(0)
	pos, err := findPat(data)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if pos != 0 {
		t.Errorf("expected pos=0, got %d", pos)
	}
}

func TestFindPat_AtOffset5(t *testing.T) {
	data := buildFindPatData(5)
	pos, err := findPat(data)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if pos != 5 {
		t.Errorf("expected pos=5, got %d", pos)
	}
}

func TestFindPat_SyncButNotPat(t *testing.T) {
	// 3 sync bytes at 188-byte intervals but PID is not 0x0000 (not PAT)
	data := make([]byte, 188*3)
	data[0] = 0x47
	data[188] = 0x47
	data[188*2] = 0x47
	// PID = 0x100, PUSI=1
	data[1] = 0x41
	data[2] = 0x00
	_, err := findPat(data)
	if err == nil {
		t.Errorf("expected error for sync bytes without PAT PID, got nil")
	}
}

func TestFindPat_ShortData(t *testing.T) {
	data := make([]byte, 188*2)
	data[0] = 0x47
	data[1] = 0x40
	data[2] = 0x00
	data[188] = 0x47
	_, err := findPat(data)
	if err == nil {
		t.Errorf("expected error for data shorter than 3 packets, got nil")
	}
}

func TestFindPat_GarbagePrefixThenValid(t *testing.T) {
	garbage := make([]byte, 1000)
	for i := range garbage {
		garbage[i] = 0xFF
	}
	patData := buildFindPatData(0)
	data := append(garbage, patData...)
	pos, err := findPat(data)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if pos != 1000 {
		t.Errorf("expected pos=1000, got %d", pos)
	}
}

func TestParseTsFile_FileNotFound(t *testing.T) {
	var opt options.Options
	err := ParseTsFile("/nonexistent/file.ts", opt)
	if err == nil {
		t.Errorf("expected error for nonexistent file, got nil")
	}
}

func TestParseTsFile_EmptyFile(t *testing.T) {
	f, err := os.CreateTemp("", "empty*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Close()

	var opt options.Options
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected nil error for empty file, got: %s", err)
	}
}

func TestParseTsFile_GarbageOnly(t *testing.T) {
	f, err := os.CreateTemp("", "garbage*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	garbage := make([]byte, 65536)
	for i := range garbage {
		garbage[i] = 0xAA
	}
	f.Write(garbage)
	f.Close()

	var opt options.Options
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected nil error for garbage-only file, got: %s", err)
	}
}

func TestParseTsFile_ValidPatPmt(t *testing.T) {
	f := createValidTsFile(t, 0)
	defer os.Remove(f)

	var opt options.Options
	err := ParseTsFile(f, opt)
	if err != nil {
		t.Errorf("expected successful parse, got: %s", err)
	}
}

func TestParseTsFile_GarbagePrefixBeforePat(t *testing.T) {
	f := createValidTsFile(t, 500)
	defer os.Remove(f)

	var opt options.Options
	err := ParseTsFile(f, opt)
	if err != nil {
		t.Errorf("expected successful parse with garbage prefix, got: %s", err)
	}
}

// buildFindPatData creates data with 3 consecutive sync bytes at 188-byte intervals
// starting at the given offset, with PAT PID (0x0000) and PUSI=1.
func buildFindPatData(offset int) []byte {
	data := make([]byte, offset+188*3)
	data[offset] = 0x47
	data[offset+1] = 0x40 // PUSI=1, PID high=0
	data[offset+2] = 0x00 // PID low=0
	data[offset+188] = 0x47
	data[offset+188*2] = 0x47
	return data
}

// createValidTsFile creates a temp file with optional garbage prefix followed by
// valid PAT, PMT, PCR, and PES packets. Returns the file path.
func createValidTsFile(t *testing.T, garbageLen int) string {
	t.Helper()
	f, err := os.CreateTemp("", "tstest*.ts")
	if err != nil {
		t.Fatal(err)
	}

	if garbageLen > 0 {
		garbage := make([]byte, garbageLen)
		for i := range garbage {
			garbage[i] = 0xFF
		}
		f.Write(garbage)
	}

	// PAT: program 1 -> PMT PID 0x3F
	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	f.Write(buildTsPacket(0x0000, true, 0, patPayload))

	// PMT: PCR PID=0x31, video stream PID=0x31 (type 0x1B)
	pmtPayload := []byte{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0, 0x00, 0xE0, 0x6A, 0x28, 0x6E}
	f.Write(buildTsPacket(0x003F, true, 0, pmtPayload))

	// PCR packet on PID 0x31
	f.Write(buildPcrPacket(0x0031, 13500))

	// PES packet on PID 0x31 (video) with PTS
	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xE0, // start code + stream_id (video)
		0x00, 0x00,             // pes_packet_length=0 (unbounded)
		0x80,                   // '10' marker
		0x80,                   // PTS only
		0x05,                   // pes_header_data_length
		0x21, 0x00, 0x07, 0xD8, 0x61, // PTS = 1000
	}
	f.Write(buildTsPacket(0x0031, true, 1, pesHeader))

	name := f.Name()
	f.Close()
	return name
}

// buildTsPacket creates a 188-byte TS packet with the given PID, PUSI flag,
// continuity counter, and payload.
func buildTsPacket(pid uint16, pusi bool, cc uint8, payload []byte) []byte {
	pkt := make([]byte, 188)
	pkt[0] = 0x47
	if pusi {
		pkt[1] = 0x40 | uint8((pid>>8)&0x1F)
	} else {
		pkt[1] = uint8((pid >> 8) & 0x1F)
	}
	pkt[2] = uint8(pid & 0xFF)
	pkt[3] = 0x10 | (cc & 0x0F) // adaptation_field_control=01 (payload only)

	// pointer_field (0x00) + payload
	pkt[4] = 0x00
	copy(pkt[5:], payload)
	for i := 5 + len(payload); i < 188; i++ {
		pkt[i] = 0xFF
	}
	return pkt
}

// buildPcrPacket creates a 188-byte TS packet with adaptation field containing PCR.
func buildPcrPacket(pid uint16, pcr uint64) []byte {
	pkt := make([]byte, 188)
	pkt[0] = 0x47
	pkt[1] = uint8((pid >> 8) & 0x1F)
	pkt[2] = uint8(pid & 0xFF)
	pkt[3] = 0x20 // adaptation_field_control=10 (AF only)

	pkt[4] = 183  // adaptation_field_length
	pkt[5] = 0x10 // PCR_flag=1

	pcrBase := pcr / 300
	pcrExt := pcr % 300
	pkt[6] = uint8(pcrBase >> 25)
	pkt[7] = uint8(pcrBase >> 17)
	pkt[8] = uint8(pcrBase >> 9)
	pkt[9] = uint8(pcrBase >> 1)
	pkt[10] = uint8((pcrBase&1)<<7) | 0x7E | uint8((pcrExt>>8)&0x01)
	pkt[11] = uint8(pcrExt & 0xFF)

	for i := 12; i < 188; i++ {
		pkt[i] = 0xFF
	}
	return pkt
}
