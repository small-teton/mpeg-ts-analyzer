package tsparser

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/options"
)

// errReadSeeker is a mock io.ReadSeeker that returns errors on specific Read/Seek calls.
type errReadSeeker struct {
	data        []byte
	pos         int64
	failAt      int // fail on the Nth Read call (0-indexed), -1 to disable
	readNum     int
	seekFailAt  int // fail on the Nth Seek call (1-indexed), 0 to disable
	seekNum     int
}

func (r *errReadSeeker) Read(p []byte) (int, error) {
	if r.failAt >= 0 && r.readNum == r.failAt {
		r.readNum++
		return 0, errors.New("mock read error")
	}
	r.readNum++
	if r.pos >= int64(len(r.data)) {
		return 0, errors.New("EOF")
	}
	n := copy(p, r.data[r.pos:])
	r.pos += int64(n)
	return n, nil
}

func (r *errReadSeeker) Seek(offset int64, whence int) (int64, error) {
	r.seekNum++
	if r.seekFailAt > 0 && r.seekNum == r.seekFailAt {
		return 0, errors.New("mock seek error")
	}
	switch whence {
	case 0:
		r.pos = offset
	case 1:
		r.pos += offset
	case 2:
		r.pos = int64(len(r.data)) + offset
	}
	return r.pos, nil
}

func TestParseTsReaderReadError(t *testing.T) {
	r := &errReadSeeker{failAt: 0}
	var opts options.Options
	err := parseTsReader(r, opts)
	if err == nil {
		t.Errorf("expected error from mock reader, got nil")
	}
}

func buildValidStreamBuf() []byte {
	var buf bytes.Buffer
	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	buf.Write(buildTsPacket(0x0000, true, 0, patPayload))
	buf.Write(buildStuffingPacket())
	buf.Write(buildStuffingPacket())
	// Second PAT for BufferPsi termination
	buf.Write(buildTsPacket(0x0000, true, 1, patPayload))
	// PMT (single stream, matching CRC)
	pmtHeader := []byte{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0, 0x00}
	pmtCrc := crc32(pmtHeader)
	pmtPayload := append(pmtHeader, byte(pmtCrc>>24), byte(pmtCrc>>16), byte(pmtCrc>>8), byte(pmtCrc))
	buf.Write(buildTsPacket(0x003F, true, 0, pmtPayload))
	// Second PMT for BufferPsi termination
	buf.Write(buildTsPacket(0x003F, true, 1, pmtPayload))
	// PCR + PES
	buf.Write(buildPcrPacket(0x0031, 13500))
	pesHeader := []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05, 0x21, 0x00, 0x07, 0xD8, 0x61}
	buf.Write(buildTsPacket(0x0031, true, 1, pesHeader))
	for buf.Len() < 65536 {
		buf.WriteByte(0xFF)
	}
	return buf.Bytes()
}

func TestParseTsReaderSeekError1(t *testing.T) {
	// First Seek fails (after findPat, before PAT buffering)
	r := &errReadSeeker{data: buildValidStreamBuf(), failAt: -1, seekFailAt: 1}
	var opts options.Options
	err := parseTsReader(r, opts)
	if err == nil {
		t.Errorf("expected seek error on 1st seek, got nil")
	}
}

func TestParseTsReaderSeekError2(t *testing.T) {
	// Second Seek fails (after PAT parse, before PMT buffering)
	r := &errReadSeeker{data: buildValidStreamBuf(), failAt: -1, seekFailAt: 2}
	var opts options.Options
	err := parseTsReader(r, opts)
	if err == nil {
		t.Errorf("expected seek error on 2nd seek, got nil")
	}
}

func TestParseTsReaderSeekError3(t *testing.T) {
	// Seek after PMT parse - try multiple seek numbers to find the right one
	for seekN := 3; seekN <= 10; seekN++ {
		r := &errReadSeeker{data: buildValidStreamBuf(), failAt: -1, seekFailAt: seekN}
		var opts options.Options
		err := parseTsReader(r, opts)
		if err != nil {
			return // found the right seek number
		}
	}
	t.Errorf("expected seek error on post-PMT seek, none triggered")
}

func TestFindPat_NotFound(t *testing.T) {
	data := make([]byte, 188*3)
	for i := range data {
		data[i] = 0xAA
	}
	_, err := findPat(data, 188)
	if err == nil {
		t.Errorf("expected error for data with no PAT, got nil")
	}
}

func TestFindPat_AtOffset0(t *testing.T) {
	data := buildFindPatData(0)
	pos, err := findPat(data, 188)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if pos != 0 {
		t.Errorf("expected pos=0, got %d", pos)
	}
}

func TestFindPat_AtOffset5(t *testing.T) {
	data := buildFindPatData(5)
	pos, err := findPat(data, 188)
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
	_, err := findPat(data, 188)
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
	_, err := findPat(data, 188)
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
	pos, err := findPat(data, 188)
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

func TestParseTsFile_CorruptPatThenValid(t *testing.T) {
	// First 64KB chunk: corrupt PAT (bad CRC), then stuffing packets
	// Second 64KB chunk: valid PAT/PMT/PCR/PES
	f, err := os.CreateTemp("", "corruptpat*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	// Corrupt PAT: valid structure but tampered CRC bytes
	corruptPatPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0xFF, 0xFF, 0xFF, 0xFF}
	corruptPat := buildTsPacket(0x0000, true, 0, corruptPatPayload)

	// Need 3 consecutive sync bytes for findPat
	f.Write(corruptPat)
	f.Write(buildStuffingPacket())
	f.Write(buildStuffingPacket())
	// Second PAT PUSI to terminate buffering properly
	f.Write(buildTsPacket(0x0000, true, 1, corruptPatPayload))

	// Pad with stuffing packets (not raw 0xFF) so TS parse doesn't fail
	numStuffing := (65536 - 188*4) / 188
	for i := 0; i < numStuffing; i++ {
		f.Write(buildStuffingPacket())
	}

	// Second chunk: valid stream
	writeFullStream(f, 1, []uint64{13500})
	f.Close()

	var opt options.Options
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected recovery from corrupt PAT, got: %s", err)
	}
}

func TestParseTsFile_CorruptPmtThenValid(t *testing.T) {
	// First chunk: valid PAT but corrupt PMT (bad CRC), then stuffing
	// Second chunk: valid PAT/PMT/PCR/PES
	f, err := os.CreateTemp("", "corruptpmt*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	// Valid PAT with termination
	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	f.Write(buildTsPacket(0x0000, true, 0, patPayload))
	f.Write(buildStuffingPacket())
	f.Write(buildStuffingPacket())
	f.Write(buildTsPacket(0x0000, true, 1, patPayload))

	// Corrupt PMT: valid structure but tampered CRC + termination
	corruptPmtPayload := []byte{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0, 0x00, 0xFF, 0xFF, 0xFF, 0xFF}
	f.Write(buildTsPacket(0x003F, true, 0, corruptPmtPayload))
	f.Write(buildTsPacket(0x003F, true, 1, corruptPmtPayload))

	// Pad with stuffing packets
	numStuffing := (65536 - 188*6) / 188
	for i := 0; i < numStuffing; i++ {
		f.Write(buildStuffingPacket())
	}

	// Second chunk: valid stream
	writeFullStream(f, 1, []uint64{13500})
	f.Close()

	var opt options.Options
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected recovery from corrupt PMT, got: %s", err)
	}
}

func TestParseTsFile_PesPacketLossThenValid(t *testing.T) {
	// First chunk: valid PAT/PMT/PCR, then PES with continuity counter gap
	// Second chunk: valid PAT/PMT/PCR/PES
	f, err := os.CreateTemp("", "pesloss*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	pmtPayload := []byte{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0, 0x00, 0xB5, 0x9E, 0xA0, 0xB0}
	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x00, 0x80, 0x80, 0x05,
		0x21, 0x00, 0x07, 0xD8, 0x61,
	}

	// Valid PAT with termination
	f.Write(buildTsPacket(0x0000, true, 0, patPayload))
	f.Write(buildStuffingPacket())
	f.Write(buildStuffingPacket())
	f.Write(buildTsPacket(0x0000, true, 1, patPayload))

	// Valid PMT with termination
	f.Write(buildTsPacket(0x003F, true, 0, pmtPayload))
	f.Write(buildTsPacket(0x003F, true, 1, pmtPayload))

	// PCR
	f.Write(buildPcrPacket(0x0031, 13500))

	// PES start packet with cc=1
	f.Write(buildTsPacket(0x0031, true, 1, pesHeader))

	// Continuation packet with cc=5 (gap: expected 2) -> triggers packet loss in PES
	f.Write(buildTsPacket(0x0031, false, 5, []byte{0x00, 0x00}))

	// Pad with stuffing packets
	numStuffing := (65536 - 188*9) / 188
	for i := 0; i < numStuffing; i++ {
		f.Write(buildStuffingPacket())
	}

	// Second chunk: valid stream
	writeFullStream(f, 1, []uint64{13500})
	f.Close()

	var opt options.Options
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected recovery from PES packet loss, got: %s", err)
	}
}

// writeFullStream writes a proper stream where PAT and PMT buffering terminates
// correctly by including second PUSI packets for each PSI table. This ensures
// BufferPsi completes without reading to EOF, so the file position is correct
// for the subsequent PMT/PES parsing phases.
func writeFullStream(f *os.File, pesPackets int, pcrs []uint64) {
	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	pmtPayload := []byte{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0, 0x00, 0xB5, 0x9E, 0xA0, 0xB0}
	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x00, 0x80, 0x80, 0x05,
		0x21, 0x00, 0x07, 0xD8, 0x61,
	}

	// PAT packet + second PAT (terminates PAT buffering)
	f.Write(buildTsPacket(0x0000, true, 0, patPayload))
	f.Write(buildStuffingPacket())
	f.Write(buildStuffingPacket())
	f.Write(buildTsPacket(0x0000, true, 1, patPayload)) // second PAT PUSI terminates buffering

	// PMT packet + second PMT (terminates PMT buffering)
	f.Write(buildTsPacket(0x003F, true, 0, pmtPayload))
	f.Write(buildTsPacket(0x003F, true, 1, pmtPayload)) // second PMT PUSI terminates buffering

	// Write PCR and PES packets
	pcrIdx := 0
	cc := uint8(1)
	for i := 0; i < pesPackets; i++ {
		if pcrIdx < len(pcrs) {
			f.Write(buildPcrPacket(0x0031, pcrs[pcrIdx]))
			pcrIdx++
		}
		f.Write(buildTsPacket(0x0031, true, cc, pesHeader))
		cc++
		// Insert continuation packet + PCR between first and second PES
		if i == 0 && pcrIdx < len(pcrs) {
			f.Write(buildPcrPacket(0x0031, pcrs[pcrIdx]))
			pcrIdx++
			f.Write(buildTsPacket(0x0031, false, cc, []byte{0x00, 0x00}))
			cc++
			if pcrIdx < len(pcrs) {
				f.Write(buildPcrPacket(0x0031, pcrs[pcrIdx]))
				pcrIdx++
			}
		}
	}
}

// writeValidTsStream writes a complete valid PAT+PMT+PCR+PES sequence to a file.
func writeValidTsStream(f *os.File) {
	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	f.Write(buildTsPacket(0x0000, true, 0, patPayload))

	// Stuffing packets for findPat sync
	f.Write(buildStuffingPacket())
	f.Write(buildStuffingPacket())

	pmtPayload := []byte{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0, 0x00, 0xE0, 0x6A, 0x28, 0x6E}
	f.Write(buildTsPacket(0x003F, true, 0, pmtPayload))

	f.Write(buildPcrPacket(0x0031, 13500))

	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x00,
		0x80,
		0x80,
		0x05,
		0x21, 0x00, 0x07, 0xD8, 0x61,
	}
	f.Write(buildTsPacket(0x0031, true, 1, pesHeader))
}

// buildStuffingPacket creates a 188-byte null/stuffing TS packet (PID 0x1FFF).
func buildStuffingPacket() []byte {
	pkt := make([]byte, 188)
	pkt[0] = 0x47
	pkt[1] = 0x1F
	pkt[2] = 0xFF
	pkt[3] = 0x10
	for i := 4; i < 188; i++ {
		pkt[i] = 0xFF
	}
	return pkt
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

func TestParseTsFile_DumpPsiOption(t *testing.T) {
	f, err := os.CreateTemp("", "dumppsi*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	writeFullStream(f, 1, []uint64{13500})
	f.Close()

	var opt options.Options
	opt.DumpPsi = true
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected successful parse with DumpPsi, got: %s", err)
	}
}

func TestParseTsFile_DumpTimestampOption(t *testing.T) {
	f := createValidTsFile(t, 0)
	defer os.Remove(f)

	var opt options.Options
	opt.DumpTimestamp = true
	err := ParseTsFile(f, opt)
	if err != nil {
		t.Errorf("expected successful parse with DumpTimestamp, got: %s", err)
	}
}

func TestParseTsFile_DumpPesHeaderOption(t *testing.T) {
	f, err := os.CreateTemp("", "dumppes*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	writeFullStream(f, 2, []uint64{13500, 27000})
	f.Close()

	var opt options.Options
	opt.DumpPesHeader = true
	opt.DumpTimestamp = true
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected successful parse with DumpPesHeader, got: %s", err)
	}
}

func TestParseTsFile_MultiplePcrAndPes(t *testing.T) {
	// Tests the nextPcr update path in BufferPes (pes.nextPcr == 0 && lastPcr > pes.prevPcr)
	f, err := os.CreateTemp("", "multipcr*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	writeFullStream(f, 2, []uint64{13500, 27000, 40500})
	f.Close()

	var opt options.Options
	opt.DumpTimestamp = true
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected successful parse with multiple PCRs, got: %s", err)
	}
}

func TestBufferPsi_MultiPacket(t *testing.T) {
	// Tests the pointer_field buffering path in BufferPsi where isBuffering=true
	// and a second PUSI packet arrives.
	f, err := os.CreateTemp("", "multipsi*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	// First PAT packet (PUSI=1, cc=0) with pointer_field=0
	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	f.Write(buildTsPacket(0x0000, true, 0, patPayload))
	f.Write(buildStuffingPacket())
	f.Write(buildStuffingPacket())

	// Second PAT packet (PUSI=1, cc=1) - triggers "isBuffering" break path
	f.Write(buildTsPacket(0x0000, true, 1, patPayload))
	f.Close()

	file, _ := os.Open(f.Name())
	defer file.Close()

	var pos int64
	var opt options.Options
	pat := NewPat()
	err = BufferPsi(file, &pos, 0x0, pat, opt, 188, 0)
	if err != nil {
		t.Errorf("expected successful BufferPsi, got: %s", err)
	}
}

func TestBufferPsi_Continuation(t *testing.T) {
	// Tests the successful continuation path in BufferPsi (non-PUSI with correct CC)
	f, err := os.CreateTemp("", "psicont*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}

	// First PAT (PUSI=1, cc=0) - starts buffering
	f.Write(buildTsPacket(0x0000, true, 0, patPayload))
	// Continuation PAT (PUSI=0, cc=1) - hits the continuation path
	f.Write(buildTsPacket(0x0000, false, 1, patPayload))
	// Second PAT PUSI (cc=2) - terminates buffering
	f.Write(buildTsPacket(0x0000, true, 2, patPayload))
	f.Close()

	file, _ := os.Open(f.Name())
	defer file.Close()

	var pos int64
	var opt options.Options
	pat := NewPat()
	err = BufferPsi(file, &pos, 0x0, pat, opt, 188, 0)
	if err != nil {
		t.Errorf("expected successful BufferPsi with continuation, got: %s", err)
	}
}

func TestBufferPsi_PacketLoss(t *testing.T) {
	// Tests the packet loss detection path in BufferPsi
	f, err := os.CreateTemp("", "psiloss*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	// First PAT packet (PUSI=1, cc=0)
	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	f.Write(buildTsPacket(0x0000, true, 0, patPayload))

	// Continuation packet with cc=5 (gap: expected 1 but got 5) -> packet loss
	f.Write(buildTsPacket(0x0000, false, 5, patPayload))
	f.Close()

	file, _ := os.Open(f.Name())
	defer file.Close()

	var pos int64
	var opt options.Options
	pat := NewPat()
	err = BufferPsi(file, &pos, 0x0, pat, opt, 188, 0)
	if err == nil {
		t.Errorf("expected packet loss error, got nil")
	}
}

func TestParseTsFile_PatBufferingErrorThenValid(t *testing.T) {
	// PAT buffering error (packet loss during PAT multi-packet) then valid stream.
	f, err := os.CreateTemp("", "patbuferr*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}

	// First: a PAT PUSI (cc=0) followed by a PAT continuation with wrong cc (cc=5)
	// This triggers packet loss in BufferPsi -> PAT buffering error
	f.Write(buildTsPacket(0x0000, true, 0, patPayload))
	f.Write(buildStuffingPacket())
	f.Write(buildStuffingPacket())
	// Continuation packet for PAT with wrong cc -> triggers packet loss error
	f.Write(buildTsPacket(0x0000, false, 5, patPayload))

	// Pad to fill the first 64KB buffer
	padLen := 65536 - 188*4
	pad := make([]byte, padLen)
	for i := range pad {
		pad[i] = 0xFF
	}
	f.Write(pad)

	// Second chunk: valid stream
	writeFullStream(f, 1, []uint64{13500})
	f.Close()

	var opt options.Options
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected recovery from PAT buffering error, got: %s", err)
	}
}

func TestParseTsFile_PmtBufferingErrorThenValid(t *testing.T) {
	// Valid PAT, then PMT with packet loss during buffering, then valid stream.
	f, err := os.CreateTemp("", "pmtbuferr*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	pmtPayload := []byte{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0, 0x00, 0xB5, 0x9E, 0xA0, 0xB0}

	// Valid PAT + second PAT for termination
	f.Write(buildTsPacket(0x0000, true, 0, patPayload))
	f.Write(buildStuffingPacket())
	f.Write(buildStuffingPacket())
	f.Write(buildTsPacket(0x0000, true, 1, patPayload))

	// PMT PUSI (cc=0) then PMT continuation with wrong cc (cc=5)
	f.Write(buildTsPacket(0x003F, true, 0, pmtPayload))
	f.Write(buildTsPacket(0x003F, false, 5, pmtPayload))

	// Pad to fill the first 64KB buffer
	padLen := 65536 - 188*6
	pad := make([]byte, padLen)
	for i := range pad {
		pad[i] = 0xFF
	}
	f.Write(pad)

	// Second chunk: valid stream
	writeFullStream(f, 1, []uint64{13500})
	f.Close()

	var opt options.Options
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected recovery from PMT buffering error, got: %s", err)
	}
}

func TestParseTsFile_PesErrorThenValid(t *testing.T) {
	// Valid PAT+PMT, then PES read error, then valid stream in next chunk.
	f, err := os.CreateTemp("", "peserr*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	pmtPayload := []byte{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0, 0x00, 0xB5, 0x9E, 0xA0, 0xB0}

	// Valid PAT with termination
	f.Write(buildTsPacket(0x0000, true, 0, patPayload))
	f.Write(buildStuffingPacket())
	f.Write(buildStuffingPacket())
	f.Write(buildTsPacket(0x0000, true, 1, patPayload))

	// Valid PMT with termination
	f.Write(buildTsPacket(0x003F, true, 0, pmtPayload))
	f.Write(buildTsPacket(0x003F, true, 1, pmtPayload))

	// Write exactly 1 non-188-byte-aligned chunk to trigger BufferPes read error
	// Actually, BufferPes only returns fmt.Errorf for read errors. To trigger that,
	// we need the file to have non-188-byte-aligned data after PMT.
	// Write 100 bytes of garbage (not 188 aligned) after the valid PMT to trigger
	// a short read in BufferPes.
	garbage := make([]byte, 100)
	for i := range garbage {
		garbage[i] = 0xAA
	}
	f.Write(garbage)

	// Pad to fill the first 64KB buffer
	written := 188*6 + 100
	padLen := 65536 - written
	pad := make([]byte, padLen)
	for i := range pad {
		pad[i] = 0xFF
	}
	f.Write(pad)

	// Second chunk: valid stream
	writeFullStream(f, 1, []uint64{13500})
	f.Close()

	var opt options.Options
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected recovery from PES error, got: %s", err)
	}
}

func TestMaxInt64(t *testing.T) {
	if maxInt64(1, 2) != 2 {
		t.Errorf("expected 2")
	}
	if maxInt64(3, 1) != 3 {
		t.Errorf("expected 3")
	}
	if maxInt64(5, 5) != 5 {
		t.Errorf("expected 5")
	}
}

func TestParseTsFile_WithLimit(t *testing.T) {
	f, err := os.CreateTemp("", "limit*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	writeFullStream(f, 2, []uint64{13500, 27000, 40500})
	f.Close()

	var opt options.Options
	opt.Limit = 188 * 20
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected successful parse with limit, got: %s", err)
	}
}

func TestParseTsFile_WithOffset(t *testing.T) {
	f, err := os.CreateTemp("", "offset*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	// Write garbage prefix, then two full streams
	garbage := make([]byte, 188*10)
	for i := range garbage {
		garbage[i] = 0xFF
	}
	f.Write(garbage)
	writeFullStream(f, 1, []uint64{13500})
	f.Close()

	var opt options.Options
	opt.Offset = int64(len(garbage))
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected successful parse with offset, got: %s", err)
	}
}

func TestParseTsFile_WithOffsetAndLimit(t *testing.T) {
	f, err := os.CreateTemp("", "offsetlimit*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	garbage := make([]byte, 188*10)
	for i := range garbage {
		garbage[i] = 0xFF
	}
	f.Write(garbage)
	writeFullStream(f, 2, []uint64{13500, 27000, 40500})
	f.Close()

	var opt options.Options
	opt.Offset = int64(len(garbage))
	opt.Limit = 188 * 20
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected successful parse with offset+limit, got: %s", err)
	}
}

func TestParseTsReader_LimitZeroRemaining(t *testing.T) {
	// Limit of 1 byte means on second iteration remaining <= 0 after first read
	r := &errReadSeeker{data: buildValidStreamBuf(), failAt: -1}
	opts := options.Options{Limit: 1}
	// Should exit cleanly without error
	err := parseTsReader(r, opts)
	if err != nil {
		t.Errorf("expected nil error with tiny limit, got: %s", err)
	}
}

func TestParseTsReader_OffsetSeekError(t *testing.T) {
	r := &errReadSeeker{data: buildValidStreamBuf(), failAt: -1, seekFailAt: 1}
	opts := options.Options{Offset: 100}
	err := parseTsReader(r, opts)
	if err == nil {
		t.Errorf("expected seek error for offset, got nil")
	}
}

// wrapM2TS prepends a 4-byte TP_extra_header (zeroed) to a 188-byte TS packet.
func wrapM2TS(tsPacket []byte) []byte {
	m2ts := make([]byte, 192)
	copy(m2ts[4:], tsPacket)
	return m2ts
}

func TestDetectPacketSize_188(t *testing.T) {
	data := buildFindPatData(0) // 188-byte aligned
	if got := detectPacketSize(data); got != 188 {
		t.Errorf("expected 188, got %d", got)
	}
}

func TestDetectPacketSize_192(t *testing.T) {
	// Build 3 consecutive 192-byte packets with sync at offset 4
	data := make([]byte, 192*3)
	for i := 0; i < 3; i++ {
		data[i*192+4] = 0x47 // sync byte at offset 4
	}
	if got := detectPacketSize(data); got != 192 {
		t.Errorf("expected 192, got %d", got)
	}
}

func TestDetectPacketSize_ShortData(t *testing.T) {
	data := []byte{0x47}
	if got := detectPacketSize(data); got != 188 {
		t.Errorf("expected default 188, got %d", got)
	}
}

func TestDetectPacketSize_NoSync(t *testing.T) {
	data := make([]byte, 1000)
	for i := range data {
		data[i] = 0xAA
	}
	if got := detectPacketSize(data); got != 188 {
		t.Errorf("expected default 188, got %d", got)
	}
}

func TestFindPat_192(t *testing.T) {
	// Build 192-byte packets: PAT + 2 stuffing
	pat := wrapM2TS(buildTsPacket(0x0000, true, 0, []byte{0x00, 0xB0, 0x0D}))
	stuff1 := wrapM2TS(buildStuffingPacket())
	stuff2 := wrapM2TS(buildStuffingPacket())
	data := append(pat, stuff1...)
	data = append(data, stuff2...)

	pos, err := findPat(data, 192)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if pos != 0 {
		t.Errorf("expected pos=0, got %d", pos)
	}
}

func TestFindPat_192_WithOffset(t *testing.T) {
	garbage := make([]byte, 192) // one packet of garbage
	for i := range garbage {
		garbage[i] = 0xFF
	}
	pat := wrapM2TS(buildTsPacket(0x0000, true, 0, []byte{0x00, 0xB0, 0x0D}))
	stuff1 := wrapM2TS(buildStuffingPacket())
	stuff2 := wrapM2TS(buildStuffingPacket())
	data := append(garbage, pat...)
	data = append(data, stuff1...)
	data = append(data, stuff2...)

	pos, err := findPat(data, 192)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if pos != 192 {
		t.Errorf("expected pos=192, got %d", pos)
	}
}

func TestParseTsFile_192BytePackets(t *testing.T) {
	f, err := os.CreateTemp("", "m2ts*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	f.Write(wrapM2TS(buildTsPacket(0x0000, true, 0, patPayload)))
	f.Write(wrapM2TS(buildStuffingPacket()))
	f.Write(wrapM2TS(buildStuffingPacket()))
	// Second PAT for BufferPsi termination
	f.Write(wrapM2TS(buildTsPacket(0x0000, true, 1, patPayload)))

	// PMT with correct CRC
	pmtHeader := []byte{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0, 0x00}
	pmtCrc := crc32(pmtHeader)
	pmtPayload := append(pmtHeader, byte(pmtCrc>>24), byte(pmtCrc>>16), byte(pmtCrc>>8), byte(pmtCrc))
	f.Write(wrapM2TS(buildTsPacket(0x003F, true, 0, pmtPayload)))
	f.Write(wrapM2TS(buildTsPacket(0x003F, true, 1, pmtPayload)))

	// PCR + PES
	f.Write(wrapM2TS(buildPcrPacket(0x0031, 13500)))
	pesHeader := []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05, 0x21, 0x00, 0x07, 0xD8, 0x61}
	f.Write(wrapM2TS(buildTsPacket(0x0031, true, 1, pesHeader)))

	f.Close()

	var opt options.Options
	err = ParseTsFile(f.Name(), opt)
	if err != nil {
		t.Errorf("expected successful parse of 192-byte stream, got: %s", err)
	}
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
