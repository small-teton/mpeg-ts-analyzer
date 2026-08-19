package tsparser

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
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
	err := BufferPsi(r, &pos, 0x0000, pat, opts, 188, 0)
	if err == nil {
		t.Errorf("expected error from mock reader, got nil")
	}
}

func TestBufferPsiInvalidPointerField(t *testing.T) {
	// PUSI packet whose pointer_field runs past the payload must error, not panic.
	pkt := make([]byte, 188)
	pkt[0] = 0x47
	pkt[1] = 0x40 // PUSI=1, pid=0x0000
	pkt[2] = 0x00
	pkt[3] = 0x10 // adaptation_field_control=01 (payload only), cc=0
	pkt[4] = 200  // pointer_field=200 (payload is only 184 bytes)
	r := &errReader{data: pkt, failAt: -1}
	var pos int64
	pat := NewPat()
	var opts options.Options
	if err := BufferPsi(r, &pos, 0x0000, pat, opts, 188, 0); err == nil {
		t.Error("expected error for invalid pointer_field, got nil")
	}
}

func TestBufferPesReadError(t *testing.T) {
	r := &errReader{failAt: 0}
	var pos int64
	programInfos := []ProgramInfo{{streamType: 0x1B, elementaryPid: 0x31}}
	var opts options.Options
	err := BufferPes(r, &pos, 0x0030, 0x0031, programInfos, opts, 188, 0)
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
	err := BufferPes(r, &pos, 0x0030, 0x0031, programInfos, opts, 188, 0)
	if err == nil {
		t.Errorf("expected error from mock reader, got nil")
	}
}

func TestBufferPes(t *testing.T) {
	f, err := os.CreateTemp("", "bufferpes*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	programInfos := []ProgramInfo{
		{streamType: 0x1B, elementaryPid: 0x31, esInfoLength: 0},
	}

	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xE0, // start code + video stream_id
		0x00, 0x00, // pes_packet_length=0
		0x80,                         // '10' marker
		0x80,                         // PTS only
		0x05,                         // header data length
		0x21, 0x00, 0x07, 0xD8, 0x61, // PTS
	}

	// Write PCR packet
	_, _ = f.Write(buildPcrPacket(0x0031, 13500))

	// Write PES start packet (cc=1)
	_, _ = f.Write(buildTsPacket(0x0031, true, 1, pesHeader))

	// Write continuation packet (cc=2)
	_, _ = f.Write(buildTsPacket(0x0031, false, 2, []byte{0x00, 0x01}))

	// Write another PCR packet (different value to trigger interval calc)
	_, _ = f.Write(buildPcrPacket(0x0031, 27000))

	// Write another PES start (triggers parse of previous PES, cc=3)
	_, _ = f.Write(buildTsPacket(0x0031, true, 3, pesHeader))

	_ = f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f2.Close() }()

	var pos int64
	var opts options.Options
	err = BufferPes(f2, &pos, 0x0030, 0x0031, programInfos, opts, 188, 0)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestBufferPesWithTimestamp(t *testing.T) {
	f, err := os.CreateTemp("", "bufferpests*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

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
	_, _ = f.Write(buildPcrPacket(0x0031, 13500))

	// Write PES start packet (cc=1)
	_, _ = f.Write(buildTsPacket(0x0031, true, 1, pesHeader))

	// Write continuation packet (cc=2)
	_, _ = f.Write(buildTsPacket(0x0031, false, 2, []byte{0x00, 0x01}))

	// Write another PCR packet
	_, _ = f.Write(buildPcrPacket(0x0031, 27000))

	// Write another PES start (triggers parse + dump of previous PES)
	_, _ = f.Write(buildTsPacket(0x0031, true, 3, pesHeader))

	_ = f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f2.Close() }()

	var pos int64
	opts := options.Options{DumpTimestamp: true}
	err = BufferPes(f2, &pos, 0x0030, 0x0031, programInfos, opts, 188, 0)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestBufferPesNonPesPacketSkip(t *testing.T) {
	f, err := os.CreateTemp("", "bufferpesnonpes*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	programInfos := []ProgramInfo{
		{streamType: 0x1B, elementaryPid: 0x31, esInfoLength: 0},
	}

	// Write packets on a PID not in programInfos (should be skipped)
	_, _ = f.Write(buildTsPacket(0x0100, true, 0, []byte{0xAA, 0xBB}))
	_, _ = f.Write(buildTsPacket(0x0100, false, 1, []byte{0xCC, 0xDD}))

	_ = f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f2.Close() }()

	var pos int64
	var opts options.Options
	err = BufferPes(f2, &pos, 0x0030, 0x0031, programInfos, opts, 188, 0)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestBufferPesPacketLoss(t *testing.T) {
	f, err := os.CreateTemp("", "bufferpesloss*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

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
	_, _ = f.Write(buildPcrPacket(0x0031, 13500))

	// Write PES start packet (cc=0)
	_, _ = f.Write(buildTsPacket(0x0031, true, 0, pesHeader))

	// Write continuation with cc gap (cc=5, expected 1) -> triggers packet loss printf
	_, _ = f.Write(buildTsPacket(0x0031, false, 5, []byte{0x00, 0x01}))

	_ = f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f2.Close() }()

	var pos int64
	var opts options.Options
	err = BufferPes(f2, &pos, 0x0030, 0x0031, programInfos, opts, 188, 0)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestBufferPsi_192(t *testing.T) {
	f, err := os.CreateTemp("", "bufferpsi192*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	_, _ = f.Write(wrapM2TS(buildTsPacket(0x0000, true, 0, patPayload)))
	_, _ = f.Write(wrapM2TS(buildStuffingPacket()))
	_, _ = f.Write(wrapM2TS(buildStuffingPacket()))
	_, _ = f.Write(wrapM2TS(buildTsPacket(0x0000, true, 1, patPayload)))
	_ = f.Close()

	file, _ := os.Open(f.Name())
	defer func() { _ = file.Close() }()

	var pos int64
	var opt options.Options
	pat := NewPat()
	err = BufferPsi(file, &pos, 0x0, pat, opt, 192, 0)
	if err != nil {
		t.Errorf("expected successful BufferPsi with 192-byte packets, got: %s", err)
	}
}

func TestBufferPes_192(t *testing.T) {
	f, err := os.CreateTemp("", "bufferpes192*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

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

	_, _ = f.Write(wrapM2TS(buildPcrPacket(0x0031, 13500)))
	_, _ = f.Write(wrapM2TS(buildTsPacket(0x0031, true, 1, pesHeader)))
	_, _ = f.Write(wrapM2TS(buildTsPacket(0x0031, false, 2, []byte{0x00, 0x01})))
	_, _ = f.Write(wrapM2TS(buildPcrPacket(0x0031, 27000)))
	_, _ = f.Write(wrapM2TS(buildTsPacket(0x0031, true, 3, pesHeader)))
	_ = f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f2.Close() }()

	var pos int64
	var opts options.Options
	err = BufferPes(f2, &pos, 0x0030, 0x0031, programInfos, opts, 192, 0)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestBufferPsi_EndOffset(t *testing.T) {
	f, err := os.CreateTemp("", "bufpsi_endoff*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	_, _ = f.Write(buildTsPacket(0x0000, true, 0, patPayload))
	_, _ = f.Write(buildStuffingPacket())
	_, _ = f.Write(buildTsPacket(0x0000, true, 1, patPayload))
	_ = f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f2.Close() }()

	var pos int64
	var opts options.Options
	pat := NewPat()
	// endOffset stops reading after 1 packet
	err = BufferPsi(f2, &pos, 0x0000, pat, opts, 188, 188)
	// Should not error, just stop early
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestBufferPes_EndOffset(t *testing.T) {
	f, err := os.CreateTemp("", "bufpes_endoff*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	programInfos := []ProgramInfo{
		{streamType: 0x1B, elementaryPid: 0x31, esInfoLength: 0},
	}
	pesHeader := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05,
		0x21, 0x00, 0x07, 0xD8, 0x61,
	}

	_, _ = f.Write(buildPcrPacket(0x0031, 13500))
	_, _ = f.Write(buildTsPacket(0x0031, true, 1, pesHeader))
	_, _ = f.Write(buildTsPacket(0x0031, false, 2, []byte{0x00, 0x01}))
	_ = f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f2.Close() }()

	var pos int64
	opts := options.Options{DumpTimestamp: true}
	// endOffset stops reading after 2 packets
	err = BufferPes(f2, &pos, 0x0030, 0x0031, programInfos, opts, 188, 188*2)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestBufferPsiSkipsAfOnlyPacket(t *testing.T) {
	// AF-only packet (afc=2) on PAT PID should be skipped (no payload)
	f, err := os.CreateTemp("", "bufpsi_afonly*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	patPayload := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
	// PAT start
	_, _ = f.Write(buildTsPacket(0x0000, true, 0, patPayload))
	// AF-only packet on PAT PID (afc=2, no payload)
	_, _ = f.Write(buildPcrPacket(0x0000, 13500))
	// Second PAT to terminate buffering
	_, _ = f.Write(buildTsPacket(0x0000, true, 1, patPayload))
	_ = f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f2.Close() }()

	var pos int64
	pat := NewPat()
	var opts options.Options
	err = BufferPsi(f2, &pos, 0x0000, pat, opts, 188, 0)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestBufferPesFinalPesMultiPid(t *testing.T) {
	// Two elementary PIDs so the end-of-stream final-PES pass sorts more than one
	// PID (covers the sort comparator). Immediate EOF, no pending data.
	r := &errReader{data: nil, failAt: -1}
	var pos int64
	programInfos := []ProgramInfo{
		{streamType: 0x1B, elementaryPid: 0x31},
		{streamType: 0x0F, elementaryPid: 0x32},
	}
	var opts options.Options
	if err := BufferPes(r, &pos, 0x30, 0x31, programInfos, opts, 188, 0); err != nil {
		t.Errorf("unexpected error: %s", err)
	}
}

// buildPesTsPacket builds a 188-byte TS packet whose payload IS a PES (start
// code at byte 4). Unlike buildTsPacket, it does not prepend a PSI
// pointer_field, so the PES actually parses.
func buildPesTsPacket(pid uint16, cc uint8, payload []byte) []byte {
	pkt := make([]byte, 188)
	pkt[0] = 0x47
	pkt[1] = 0x40 | uint8((pid>>8)&0x1F) // payload_unit_start_indicator = 1
	pkt[2] = uint8(pid & 0xFF)
	pkt[3] = 0x10 | (cc & 0x0F) // payload only
	copy(pkt[4:], payload)
	for i := 4 + len(payload); i < 188; i++ {
		pkt[i] = 0xFF
	}
	return pkt
}

func TestBufferPesAnomalyValidPesChecked(t *testing.T) {
	// A cleanly parsed PES reaches the anomaly check (a single access unit just
	// seeds the per-PID baseline). Exercises the parse-succeeded path.
	f, err := os.CreateTemp("", "pesvalid*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	programInfos := []ProgramInfo{{streamType: 0x1B, elementaryPid: 0x31}}
	// A valid PES header (ptsDtsFlags=3: PTS and DTS present).
	validPes := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x84, 0xC0, 0x0A, 0x31, 0x00, 0x01, 0xC7, 0x3F, 0x11, 0x00,
		0x01, 0xAF, 0xC9, 0x00,
	}

	_, _ = f.Write(buildPesTsPacket(0x0031, 1, validPes))
	_ = f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f2.Close() }()

	var pos int64
	var opts options.Options
	out := captureStdout(t, func() {
		if err := BufferPes(f2, &pos, 0x0030, 0x0031, programInfos, opts, 188, 0); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	})
	if strings.Contains(out, "Timestamp Anomaly Report:") {
		t.Errorf("no anomaly should be reported here:\n%s", out)
	}
}

// encodePtsOnlyPes builds a PES header carrying only a PTS (ptsDtsFlags=2) with
// the given 90 kHz value, for end-to-end anomaly tests.
func encodePtsOnlyPes(pts uint64) []byte {
	return []byte{
		0x00, 0x00, 0x01, 0xE0, // start code + video stream_id
		0x00, 0x00, // pes_packet_length
		0x80, // '10' marker
		0x80, // ptsDtsFlags=10 (PTS only)
		0x05, // pes_header_data_length
		0x21 | byte((pts>>29)&0x0E),
		byte((pts >> 22) & 0xFF),
		0x01 | byte((pts>>14)&0xFE),
		byte((pts >> 7) & 0xFF),
		0x01 | byte((pts<<1)&0xFE),
	}
}

func TestBufferPesEmitsAnomaly(t *testing.T) {
	// Two PES with a decreasing PTS must produce a "went backward" report.
	f, err := os.CreateTemp("", "pesanom*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	programInfos := []ProgramInfo{{streamType: 0x1B, elementaryPid: 0x31}}
	_, _ = f.Write(buildPesTsPacket(0x0031, 1, encodePtsOnlyPes(90000))) // 1000 ms
	_, _ = f.Write(buildPesTsPacket(0x0031, 2, encodePtsOnlyPes(45000))) // 500 ms (backward)
	_ = f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f2.Close() }()

	var pos int64
	var opts options.Options
	out := captureStdout(t, func() {
		if err := BufferPes(f2, &pos, 0x0030, 0x0031, programInfos, opts, 188, 0); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	})
	if !strings.Contains(out, "Timestamp Anomaly Report:") || !strings.Contains(out, "PTS went backward") {
		t.Errorf("expected a backward-PTS anomaly report, got:\n%s", out)
	}
}

func TestBufferPesAnomalyParseErrorSkipped(t *testing.T) {
	// A completed PES whose payload has an invalid packet_start_code_prefix makes
	// pes.Parse() fail; the timestamp anomaly check must be skipped (garbage
	// header) and BufferPes must still return nil.
	f, err := os.CreateTemp("", "pesparseerr*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	programInfos := []ProgramInfo{{streamType: 0x1B, elementaryPid: 0x31}}
	badPayload := []byte{0xFF, 0xFF, 0xFF, 0xE0, 0x00, 0x00} // start code != 0x000001

	_, _ = f.Write(buildPcrPacket(0x0031, 13500))
	_, _ = f.Write(buildTsPacket(0x0031, true, 1, badPayload)) // first PES (bad)
	_, _ = f.Write(buildTsPacket(0x0031, true, 2, badPayload)) // second PUSI parses the first PES -> error
	_ = f.Close()

	f2, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f2.Close() }()

	var pos int64
	var opts options.Options
	out := captureStdout(t, func() {
		if err := BufferPes(f2, &pos, 0x0030, 0x0031, programInfos, opts, 188, 0); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	})
	if strings.Contains(out, "Timestamp Anomaly Report:") {
		t.Errorf("no anomaly should be reported here:\n%s", out)
	}
}
