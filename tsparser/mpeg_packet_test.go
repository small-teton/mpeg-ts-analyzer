package tsparser

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/options"
)

// errReader is a mock io.Reader that returns an error after N successful reads.
type errReader struct {
	data    []byte
	pos     int
	failAt  int // fail on the Nth Read call (0-indexed)
	readNum int
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.readNum == r.failAt {
		r.readNum++
		return 0, errors.New("mock read error")
	}
	r.readNum++
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func TestBufferPsiReadError(t *testing.T) {
	// First read returns error
	r := &errReader{failAt: 0}
	var pos int64
	pat := NewPat()
	var opts options.Options
	err := BufferPsi(r, &pos, 0x0000, pat, opts, 188)
	if err == nil {
		t.Errorf("expected error from mock reader, got nil")
	}
}

func TestBufferPesReadError(t *testing.T) {
	r := &errReader{failAt: 0}
	var pos int64
	programInfos := []ProgramInfo{{streamType: 0x1B, elementaryPid: 0x31}}
	var opts options.Options
	err := BufferPes(r, &pos, 0x0031, programInfos, opts, 188)
	if err == nil {
		t.Errorf("expected error from mock reader, got nil")
	}
}

func TestBufferPesReadErrorMidStream(t *testing.T) {
	// Build valid packets, then fail on 3rd read
	var buf bytes.Buffer
	buf.Write(buildPcrPacket(0x0031, 13500))
	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05,
		0x21, 0x00, 0x07, 0xD8, 0x61,
	}
	buf.Write(buildTsPacket(0x0031, true, 1, pesHeader))
	// Third read will fail
	r := &errReader{data: buf.Bytes(), failAt: 2}
	var pos int64
	programInfos := []ProgramInfo{{streamType: 0x1B, elementaryPid: 0x31}}
	var opts options.Options
	err := BufferPes(r, &pos, 0x0031, programInfos, opts, 188)
	if err == nil {
		t.Errorf("expected error from mock reader, got nil")
	}
}

func TestBufferPes(t *testing.T) {
	f, err := os.CreateTemp("", "bufferpes*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	programInfos := []ProgramInfo{
		{streamType: 0x1B, elementaryPid: 0x31, esInfoLength: 0},
	}

	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xE0, // start code + video stream_id
		0x00, 0x00, // pes_packet_length=0
		0x80,       // '10' marker
		0x80,       // PTS only
		0x05,       // header data length
		0x21, 0x00, 0x07, 0xD8, 0x61, // PTS
	}

	// Write PCR packet
	f.Write(buildPcrPacket(0x0031, 13500))

	// Write PES start packet (cc=1)
	f.Write(buildTsPacket(0x0031, true, 1, pesHeader))

	// Write continuation packet (cc=2)
	f.Write(buildTsPacket(0x0031, false, 2, []byte{0x00, 0x01}))

	// Write another PCR packet (different value to trigger interval calc)
	f.Write(buildPcrPacket(0x0031, 27000))

	// Write another PES start (triggers parse of previous PES, cc=3)
	f.Write(buildTsPacket(0x0031, true, 3, pesHeader))

	f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	var pos int64
	var opts options.Options
	err = BufferPes(f2, &pos, 0x0031, programInfos, opts, 188)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestBufferPesWithTimestamp(t *testing.T) {
	f, err := os.CreateTemp("", "bufferpests*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	programInfos := []ProgramInfo{
		{streamType: 0x1B, elementaryPid: 0x31, esInfoLength: 0},
	}

	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x00,
		0x80,
		0x80,
		0x05,
		0x21, 0x00, 0x07, 0xD8, 0x61,
	}

	// Write PCR packet
	f.Write(buildPcrPacket(0x0031, 13500))

	// Write PES start packet (cc=1)
	f.Write(buildTsPacket(0x0031, true, 1, pesHeader))

	// Write continuation packet (cc=2)
	f.Write(buildTsPacket(0x0031, false, 2, []byte{0x00, 0x01}))

	// Write another PCR packet
	f.Write(buildPcrPacket(0x0031, 27000))

	// Write another PES start (triggers parse + dump of previous PES)
	f.Write(buildTsPacket(0x0031, true, 3, pesHeader))

	f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	var pos int64
	opts := options.Options{DumpTimestamp: true}
	err = BufferPes(f2, &pos, 0x0031, programInfos, opts, 188)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestBufferPesNonPesPacketSkip(t *testing.T) {
	f, err := os.CreateTemp("", "bufferpesnonpes*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	programInfos := []ProgramInfo{
		{streamType: 0x1B, elementaryPid: 0x31, esInfoLength: 0},
	}

	// Write packets on a PID not in programInfos (should be skipped)
	f.Write(buildTsPacket(0x0100, true, 0, []byte{0xAA, 0xBB}))
	f.Write(buildTsPacket(0x0100, false, 1, []byte{0xCC, 0xDD}))

	f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	var pos int64
	var opts options.Options
	err = BufferPes(f2, &pos, 0x0031, programInfos, opts, 188)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestBufferPesPacketLoss(t *testing.T) {
	f, err := os.CreateTemp("", "bufferpesloss*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	programInfos := []ProgramInfo{
		{streamType: 0x1B, elementaryPid: 0x31, esInfoLength: 0},
	}

	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x00,
		0x80,
		0x80,
		0x05,
		0x21, 0x00, 0x07, 0xD8, 0x61,
	}

	// Write PCR packet
	f.Write(buildPcrPacket(0x0031, 13500))

	// Write PES start packet (cc=0)
	f.Write(buildTsPacket(0x0031, true, 0, pesHeader))

	// Write continuation with cc gap (cc=5, expected 1) -> triggers packet loss printf
	f.Write(buildTsPacket(0x0031, false, 5, []byte{0x00, 0x01}))

	f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	var pos int64
	var opts options.Options
	err = BufferPes(f2, &pos, 0x0031, programInfos, opts, 188)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestBufferPsi_192(t *testing.T) {
	f, err := os.CreateTemp("", "bufferpsi192*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	f.Write(wrapM2TS(buildTsPacket(0x0000, true, 0, patPayload)))
	f.Write(wrapM2TS(buildStuffingPacket()))
	f.Write(wrapM2TS(buildStuffingPacket()))
	f.Write(wrapM2TS(buildTsPacket(0x0000, true, 1, patPayload)))
	f.Close()

	file, _ := os.Open(f.Name())
	defer file.Close()

	var pos int64
	var opt options.Options
	pat := NewPat()
	err = BufferPsi(file, &pos, 0x0, pat, opt, 192)
	if err != nil {
		t.Errorf("expected successful BufferPsi with 192-byte packets, got: %s", err)
	}
}

func TestBufferPes_192(t *testing.T) {
	f, err := os.CreateTemp("", "bufferpes192*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	programInfos := []ProgramInfo{
		{streamType: 0x1B, elementaryPid: 0x31, esInfoLength: 0},
	}

	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xE0,
		0x00, 0x00,
		0x80,
		0x80,
		0x05,
		0x21, 0x00, 0x07, 0xD8, 0x61,
	}

	f.Write(wrapM2TS(buildPcrPacket(0x0031, 13500)))
	f.Write(wrapM2TS(buildTsPacket(0x0031, true, 1, pesHeader)))
	f.Write(wrapM2TS(buildTsPacket(0x0031, false, 2, []byte{0x00, 0x01})))
	f.Write(wrapM2TS(buildPcrPacket(0x0031, 27000)))
	f.Write(wrapM2TS(buildTsPacket(0x0031, true, 3, pesHeader)))
	f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	var pos int64
	var opts options.Options
	err = BufferPes(f2, &pos, 0x0031, programInfos, opts, 192)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}
