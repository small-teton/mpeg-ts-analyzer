package tsparser

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

func decodePacketsPage(t *testing.T, path string, opts options.Options) (PacketsPage, error) {
	t.Helper()
	opts.Format, opts.Output = "json", "packets"
	var runErr error
	out := captureStdout(t, func() { runErr = ParseTsFile(path, opts) })
	var page PacketsPage
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatalf("packets output did not decode: %v\n%s", err, out)
	}
	return page, runErr
}

func decodePacketDetail(t *testing.T, path string, opts options.Options) (PacketDetail, error) {
	t.Helper()
	opts.Format, opts.Output = "json", "packet"
	var runErr error
	out := captureStdout(t, func() { runErr = ParseTsFile(path, opts) })
	var detail PacketDetail
	if err := json.Unmarshal([]byte(out), &detail); err != nil {
		t.Fatalf("packet output did not decode: %v\n%s", err, out)
	}
	return detail, runErr
}

func TestPacketsPagingVisitsEveryPacketOnce(t *testing.T) {
	for _, tc := range []struct {
		path       string
		packetSize int
	}{
		{sample188Path, 188}, {sample192Path, 192}, {twoPrograms188Path, 188}, {twoPrograms192Path, 192},
	} {
		info, _ := os.Stat(tc.path)
		offset, visited := int64(0), int64(0)
		for pages := 0; ; pages++ {
			page, err := decodePacketsPage(t, tc.path, options.Options{Offset: offset, Count: 500})
			if err != nil {
				t.Fatalf("%s: %v", tc.path, err)
			}
			if page.Status != "complete" || page.Input.PacketSizeBytes != tc.packetSize || page.Input.FileSizeBytes != info.Size() || *page.FirstOffsetBytes != offset {
				t.Fatalf("%s: page header %+v", tc.path, page.PacketsEnvelope)
			}
			for i, p := range page.Packets {
				if p.OffsetBytes != offset+int64(i*tc.packetSize) || !p.Valid {
					t.Fatalf("%s: packet %d = %+v", tc.path, i, p)
				}
			}
			visited += int64(len(page.Packets))
			if page.EndOfFile {
				if page.NextOffsetBytes != nil || visited != page.TotalPackets || visited != info.Size()/int64(tc.packetSize) {
					t.Errorf("%s: visited %d of %d packets, next %v", tc.path, visited, page.TotalPackets, page.NextOffsetBytes)
				}
				break
			}
			offset = *page.NextOffsetBytes
		}
	}
}

func TestPacketsPageFields(t *testing.T) {
	// From an unaligned offset the page starts at the next packet boundary.
	page, err := decodePacketsPage(t, twoPrograms188Path, options.Options{Offset: 29892 - 100, Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	p := page.Packets[0]
	if *page.FirstOffsetBytes != 29892 || p.PID != "0x0100" || !p.PUSI || p.AdaptationFieldControl != 3 || p.PCRMs == nil || *p.PCRMs != 860 || *page.NextOffsetBytes != 29892+188 {
		t.Errorf("page = %+v / %+v", page.PacketsEnvelope, p)
	}
	// The packet-size hint selects the size; a page ending exactly at EOF has no next offset.
	info, _ := os.Stat(sample192Path)
	page, err = decodePacketsPage(t, sample192Path, options.Options{Offset: info.Size() - 192, Count: 1, PacketSize: 192})
	if err != nil || !page.EndOfFile || page.NextOffsetBytes != nil || len(page.Packets) != 1 {
		t.Errorf("last page = %+v %v", page, err)
	}
}

func TestPacketsPageInvalidAndTruncatedPackets(t *testing.T) {
	data := bytes.Repeat(buildStuffingPacket(), 4)
	data = append(data, make([]byte, 188)...) // no sync byte
	data = append(data, buildStuffingPacket()...)
	data = append(data, 0x47, 0x1F) // truncated tail
	path := t.TempDir() + "/packets.ts"
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	page, err := decodePacketsPage(t, path, options.Options{Count: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Packets) != 6 || page.Packets[4].Valid || page.Packets[4].PID != "" || !page.Packets[5].Valid || !page.EndOfFile || len(page.Warnings) != 1 || !strings.Contains(page.Warnings[0].Message, "ignored 2 trailing bytes") {
		t.Errorf("page = %+v", page)
	}
}

func TestPacketsOutputErrors(t *testing.T) {
	for _, tc := range []struct {
		opts   options.Options
		path   string
		reason string
		want   string
	}{
		{options.Options{Count: 0}, sample188Path, "analysis_failed", "--count must be between 1 and 10000"},
		{options.Options{Count: 10001}, sample188Path, "analysis_failed", "--count must be between"},
		{options.Options{Count: 1, PacketSize: 204}, sample188Path, "analysis_failed", "unsupported packet size 204"},
		{options.Options{Count: 1, Offset: 1 << 40}, sample188Path, "analysis_failed", "outside the file"},
		{options.Options{Count: 1}, "/no/such/file.ts", "input_open_failed", "file open error"},
	} {
		page, err := decodePacketsPage(t, tc.path, tc.opts)
		if err == nil || page.Status != "failed" || page.Reason != tc.reason || !strings.Contains(page.Error, tc.want) {
			t.Errorf("%+v: page %+v err %v", tc.opts, page.PacketsEnvelope, err)
		}
	}
	// No sync byte anywhere.
	path := t.TempDir() + "/garbage.ts"
	_ = os.WriteFile(path, bytes.Repeat([]byte{0x00}, 1000), 0o600)
	if page, err := decodePacketsPage(t, path, options.Options{Count: 1}); err == nil || !strings.Contains(page.Error, "no TS sync byte") {
		t.Errorf("garbage: %+v %v", page, err)
	}
	if err := ParseTsFile(sample188Path, options.Options{Output: "packets"}); err == nil || !strings.Contains(err.Error(), "--output packets requires --format json") {
		t.Errorf("text mode: %v", err)
	}
}

// failingReaderAt fails every read after the first n bytes.
type failingReaderAt struct {
	data []byte
	n    int64
}

func (r failingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= r.n {
		return 0, errors.New("mock read error")
	}
	return bytes.NewReader(r.data[:r.n]).ReadAt(p, off)
}

func TestPacketsReadErrors(t *testing.T) {
	data := bytes.Repeat(buildStuffingPacket(), 10)
	page := &PacketsPage{}
	err := readPacketsPage(failingReaderAt{data, 188 * 3}, int64(len(data)), options.Options{Count: 10}, page)
	if err == nil || !strings.Contains(err.Error(), "file read error") {
		t.Errorf("err = %v", err)
	}
	if _, err := openPacketFile(failingReaderAt{data, 0}, int64(len(data)), 0, 0); err == nil {
		t.Error("expected a probe read error")
	}
}

// layerFields maps a layer's field names to values.
func layerFields(d PacketDetail, name string) (map[string]PacketField, PacketLayer) {
	for _, layer := range d.Packet.Layers {
		if layer.Name == name {
			fields := map[string]PacketField{}
			for _, f := range layer.Fields {
				fields[f.Name] = f
			}
			return fields, layer
		}
	}
	return nil, PacketLayer{}
}

func TestPacketDetailMatchesParsers(t *testing.T) {
	detail, err := decodePacketDetail(t, twoPrograms188Path, options.Options{Offset: 29892})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := hex.DecodeString(detail.Packet.Bytes)
	tp := NewTsPacket()
	tp.Initialize(29892, options.Options{})
	tp.Append(raw)
	if err := tp.Parse(); err != nil {
		t.Fatal(err)
	}
	header, _ := layerFields(detail, "ts_header")
	af, _ := layerFields(detail, "adaptation_field")
	pes, pesLayer := layerFields(detail, "pes_header")
	if header["PID"].Value != reportPID(tp.Pid()) || header["continuity_counter"].Value != "1" || header["payload_unit_start_indicator"].Value != "1" {
		t.Errorf("TS header = %+v", header)
	}
	if !strings.HasPrefix(af["program_clock_reference"].Value, "23220000 ") || tp.Pcr() != 23220000 || af["program_clock_reference"].ByteOffset != 6 {
		t.Errorf("PCR = %+v (parser %d)", af["program_clock_reference"], tp.Pcr())
	}
	if pes["PTS"].Value != "144000 (1.600000 s)" || pes["DTS"].Value != "140400 (1.560000 s)" || pesLayer.Truncated {
		t.Errorf("PES header = %+v", pes)
	}
	// Every span points at the bytes its value came from.
	for _, layer := range detail.Packet.Layers {
		for _, f := range append(layer.Fields, flatten(layer.Rows)...) {
			if f.ByteOffset < 0 || f.ByteOffset+f.ByteLength > len(raw) || f.ByteLength == 0 {
				t.Errorf("%s.%s span %d+%d out of the packet", layer.Name, f.Name, f.ByteOffset, f.ByteLength)
			}
		}
	}
	unit := detail.Unit
	if unit.Kind != "pes" || *unit.FirstOffsetBytes != 29892 || unit.Position != 1 || unit.StreamID != "0xe0" || *unit.PTSMs != 1600 || *unit.DTSMs != 1560 {
		t.Errorf("unit = %+v", unit)
	}
}

func flatten(rows [][]PacketField) []PacketField {
	var out []PacketField
	for _, row := range rows {
		out = append(out, row...)
	}
	return out
}

func TestPacketDetailPsiTables(t *testing.T) {
	for _, tc := range []struct {
		path   string
		offset int64
		layer  string
		rows   int
	}{
		{twoPrograms188Path, 25004, "pat", 2},
		{twoPrograms188Path, 376, "pmt", 2},
		{twoPrograms192Path, 25004 / 188 * 192, "pat", 2},
	} {
		detail, err := decodePacketDetail(t, tc.path, options.Options{Offset: tc.offset})
		if err != nil {
			t.Fatalf("%s@%d: %v", tc.path, tc.offset, err)
		}
		fields, layer := layerFields(detail, tc.layer)
		if len(layer.Rows) != tc.rows || fields["CRC_32"].ByteLength != 4 || detail.Unit.Kind != "psi" {
			t.Errorf("%s@%d: %s layer %+v unit %+v", tc.path, tc.offset, tc.layer, layer, detail.Unit)
		}
		if strings.Contains(tc.path, "192") {
			if m2ts, _ := layerFields(detail, "m2ts_header"); m2ts["arrival_time_stamp"].ByteLength != 4 {
				t.Errorf("M2TS header = %+v", m2ts)
			}
		}
	}
}

func TestPacketDetailUnitMembership(t *testing.T) {
	// The second packet of the video PES at 29892.
	detail, err := decodePacketDetail(t, twoPrograms188Path, options.Options{Offset: 30080})
	if err != nil {
		t.Fatal(err)
	}
	u := detail.Unit
	if u.Kind != "pes" || *u.FirstOffsetBytes != 29892 || u.Position != 2 || *u.PreviousOffsetBytes != 29892 || u.NextOffsetBytes == nil || u.PacketsInRange < 3 {
		t.Errorf("unit = %+v", u)
	}
	// With the range starting after the PES start, the unit started before it.
	detail, err = decodePacketDetail(t, twoPrograms188Path, options.Options{Offset: 30080, RangeOffset: 30000, RangeLimit: 30080 + 188 - 30000})
	if err != nil {
		t.Fatal(err)
	}
	if u := detail.Unit; !u.StartedBeforeRange || u.FirstOffsetBytes != nil || u.Kind != "unknown" || u.Position != 1 || u.PacketsInRange != 1 || u.NextOffsetBytes != nil {
		t.Errorf("unit in a narrow range = %+v", u)
	}
}

// writeDetailFixture writes packets to a file and decodes the packet at offset.
func writeDetailFixture(t *testing.T, packets [][]byte, opts options.Options) PacketDetail {
	t.Helper()
	path := t.TempDir() + "/detail.ts"
	if err := os.WriteFile(path, bytes.Join(packets, nil), 0o600); err != nil {
		t.Fatal(err)
	}
	detail, err := decodePacketDetail(t, path, opts)
	if err != nil {
		t.Fatal(err)
	}
	return detail
}

func TestPacketDetailSplitPesHeader(t *testing.T) {
	// An adaptation field leaves 10 payload bytes, so the PTS/DTS continue in
	// the next packet of the PID.
	first := bytes.Repeat([]byte{0xFF}, 188)
	first[0], first[1], first[2], first[3] = 0x47, 0x41, 0x00, 0x30
	first[4] = 173 // adaptation_field_length: 4+1+173 = 178, 10 payload bytes left
	first[5] = 0x00
	header := []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0xC0, 0x0A,
		0x31, 0x00, 0x05, 0xBF, 0x21, 0x11, 0x00, 0x05, 0x9F, 0x41}
	copy(first[178:], header[:10])
	second := bytes.Repeat([]byte{0xFF}, 188)
	second[0], second[1], second[2], second[3] = 0x47, 0x01, 0x00, 0x11
	copy(second[4:], header[10:])
	other := buildStuffingPacket()
	detail := writeDetailFixture(t, [][]byte{first, other, second, other, other}, options.Options{Offset: 0})
	pes, layer := layerFields(detail, "pes_header")
	if !layer.Truncated || pes["PES_header_data_length"].Value != "10" || pes["PTS"].Name != "" {
		t.Errorf("split PES header layer = %+v", layer)
	}
	u := detail.Unit
	if u.Kind != "pes" || len(u.HeaderPacketOffsets) != 2 || u.HeaderPacketOffsets[1] != 376 || u.PTSMs == nil || u.DTSMs == nil {
		t.Errorf("unit = %+v", u)
	}
}

func TestPacketDetailAdaptationFieldVariants(t *testing.T) {
	pkt := bytes.Repeat([]byte{0xFF}, 188)
	pkt[0], pkt[1], pkt[2], pkt[3] = 0x47, 0x01, 0x00, 0x20 // AF only
	pkt[4] = 183
	pkt[5] = 0x1C // PCR, OPCR and splicing point
	pkt[18] = 0xFE
	detail := writeDetailFixture(t, [][]byte{pkt, buildStuffingPacket(), buildStuffingPacket()}, options.Options{Offset: 0})
	af, _ := layerFields(detail, "adaptation_field")
	if af["program_clock_reference"].ByteOffset != 6 || af["original_program_clock_reference"].ByteOffset != 12 || af["splice_countdown"].Value != "-2" {
		t.Errorf("adaptation field = %+v", af)
	}
	if _, payload := layerFields(detail, "payload"); payload.Name != "" {
		t.Errorf("AF-only packet has a payload layer: %+v", payload)
	}

	empty := bytes.Repeat([]byte{0xFF}, 188)
	empty[0], empty[1], empty[2], empty[3] = 0x47, 0x01, 0x00, 0x30
	empty[4] = 0 // zero-length adaptation field
	detail = writeDetailFixture(t, [][]byte{empty, buildStuffingPacket(), buildStuffingPacket()}, options.Options{Offset: 0})
	if af, layer := layerFields(detail, "adaptation_field"); len(layer.Fields) != 1 || af["adaptation_field_length"].Value != "0" {
		t.Errorf("empty adaptation field = %+v", layer)
	}
	if got := adaptationFieldControlName(0); got != "reserved" {
		t.Errorf("afc 0 = %q", got)
	}
}

func TestPacketDetailSectionsAndStreams(t *testing.T) {
	// A PMT whose section continues past the packet, with a descriptor loop.
	streams := make([][2]uint16, 40)
	for i := range streams {
		streams[i] = [2]uint16{0x0F, uint16(0x200 + i)}
	}
	long := buildTsPacket(0x1000, true, 0, pmtSection(1, 0x100, streams...))
	detail := writeDetailFixture(t, [][]byte{long, buildStuffingPacket(), buildStuffingPacket()}, options.Options{Offset: 0})
	if fields, layer := layerFields(detail, "pmt"); !layer.Truncated || len(layer.Rows) == 0 || fields["CRC_32"].Name != "" {
		t.Errorf("long PMT = %+v", layer)
	}
	// An unknown table and a pointer_field that points past the packet.
	other := buildTsPacket(0x0011, true, 0, psiSection(0x42, 1, []byte{0x00, 0x01, 0xFF}))
	pointer := bytes.Repeat([]byte{0xFF}, 188)
	pointer[0], pointer[1], pointer[2], pointer[3], pointer[4] = 0x47, 0x40, 0x12, 0x10, 0xFE
	detail = writeDetailFixture(t, [][]byte{other, pointer, buildStuffingPacket()}, options.Options{Offset: 0})
	if fields, layer := layerFields(detail, "psi_section"); layer.Truncated || fields["CRC_32"].ByteLength != 4 {
		t.Errorf("SDT section = %+v", layer)
	}
	detail = writeDetailFixture(t, [][]byte{other, pointer, buildStuffingPacket()}, options.Options{Offset: 188})
	if _, layer := layerFields(detail, "psi_section"); !layer.Truncated || len(layer.Fields) != 1 {
		t.Errorf("pointer past the packet = %+v", layer)
	}
	// A PES stream without the optional header (padding_stream).
	padding := bytes.Repeat([]byte{0xFF}, 188)
	padding[0], padding[1], padding[2], padding[3] = 0x47, 0x40, 0x30, 0x10
	copy(padding[4:], []byte{0x00, 0x00, 0x01, 0xBE, 0x00, 0x10})
	detail = writeDetailFixture(t, [][]byte{padding, buildStuffingPacket(), buildStuffingPacket()}, options.Options{Offset: 0})
	if fields, layer := layerFields(detail, "pes_header"); len(layer.Fields) != 3 || fields["PES_packet_length"].Value != "16" || detail.Unit.StreamID != "0xbe" || detail.Unit.PTSMs != nil {
		t.Errorf("padding stream = %+v / %+v", layer, detail.Unit)
	}
	// Null packets belong to no unit.
	detail = writeDetailFixture(t, [][]byte{buildStuffingPacket(), buildStuffingPacket(), buildStuffingPacket()}, options.Options{Offset: 188})
	if detail.Unit.Kind != "none" {
		t.Errorf("null packet unit = %+v", detail.Unit)
	}
}

func TestPacketDetailErrors(t *testing.T) {
	for _, tc := range []struct {
		opts options.Options
		want string
	}{
		{options.Options{Offset: 100}, "not on a packet boundary"},
		{options.Options{Offset: 188, RangeOffset: 376}, "outside the scan range"},
		{options.Options{Offset: 376, RangeLimit: 188}, "outside the scan range"},
		{options.Options{Offset: 0, PacketSize: 7}, "unsupported packet size"},
	} {
		detail, err := decodePacketDetail(t, sample188Path, tc.opts)
		if err == nil || detail.Status != "failed" || !strings.Contains(detail.Error, tc.want) || detail.Packet != nil {
			t.Errorf("%+v: %+v %v", tc.opts, detail.PacketsEnvelope, err)
		}
	}
	// A packet without a sync byte cannot be decoded.
	data := append(bytes.Repeat(buildStuffingPacket(), 3), make([]byte, 188)...)
	data = append(data, bytes.Repeat(buildStuffingPacket(), 3)...)
	detail := &PacketDetail{}
	if err := readPacketDetail(bytes.NewReader(data), int64(len(data)), options.Options{Offset: 564}, detail); err == nil || !strings.Contains(err.Error(), "cannot parse") {
		t.Errorf("no sync: %v", err)
	}
	// A truncated packet at the offset cannot be read.
	data = append(bytes.Repeat(buildStuffingPacket(), 3), 0x47, 0x1F, 0xFF)
	if err := readPacketDetail(bytes.NewReader(data), int64(len(data)), options.Options{Offset: 564}, detail); err == nil || !strings.Contains(err.Error(), "cannot read") {
		t.Errorf("truncated: %v", err)
	}
}

func TestPacketsPageAfterGarbagePrefix(t *testing.T) {
	// Packet boundaries start 100 bytes into the file.
	data := append(bytes.Repeat([]byte{0x00}, 100), bytes.Repeat(buildStuffingPacket(), 4)...)
	page := &PacketsPage{}
	if err := readPacketsPage(bytes.NewReader(data), int64(len(data)), options.Options{Count: 2}, page); err != nil {
		t.Fatal(err)
	}
	if *page.FirstOffsetBytes != 100 || page.TotalPackets != 4 || *page.NextOffsetBytes != 476 {
		t.Errorf("page = %+v", page)
	}
}

func TestWritePacketsDocumentWriteError(t *testing.T) {
	err := writePacketsDocument(sample188Path, options.Options{Output: "packets", Count: 1}, failingWriter{})
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Errorf("err = %v", err)
	}
}

// packetWithPayload builds a packet on pid whose payload is payload, after an
// adaptation field that pads the packet so the payload ends it.
func packetWithPayload(pid uint16, pusi bool, cc uint8, payload []byte) []byte {
	pkt := bytes.Repeat([]byte{0xFF}, 188)
	pkt[0], pkt[1], pkt[2], pkt[3] = 0x47, byte(pid>>8), byte(pid), 0x30|cc
	if pusi {
		pkt[1] |= 0x40
	}
	pkt[4] = byte(188 - 5 - len(payload))
	pkt[5] = 0x00
	copy(pkt[188-len(payload):], payload)
	return pkt
}

func TestPacketDetailTablesCutByPacketEnd(t *testing.T) {
	// A PAT with a network entry and more programs than fit in one packet.
	programs := [][2]uint16{{0, 0x0010}}
	for i := uint16(1); i <= 50; i++ {
		programs = append(programs, [2]uint16{i, 0x1000 + i})
	}
	pat := buildTsPacket(0x0000, true, 0, patSection(programs...))
	detail := writeDetailFixture(t, [][]byte{pat, buildStuffingPacket(), buildStuffingPacket()}, options.Options{Offset: 0})
	_, layer := layerFields(detail, "pat")
	if !layer.Truncated || layer.Rows[0][1].Name != "network_PID" || len(layer.Rows) != 43 {
		t.Errorf("long PAT: %d rows, truncated %v, first %+v", len(layer.Rows), layer.Truncated, layer.Rows[0])
	}
	// A PMT starting so late that PCR_PID does not fit.
	pmt := packetWithPayload(0x1000, true, 0, append([]byte{0x00}, pmtSection(1, 0x100, [2]uint16{0x02, 0x100})[:8]...))
	detail = writeDetailFixture(t, [][]byte{pmt, buildStuffingPacket(), buildStuffingPacket()}, options.Options{Offset: 0})
	if fields, layer := layerFields(detail, "pmt"); !layer.Truncated || fields["last_section_number"].Name == "" || fields["PCR_PID"].Name != "" {
		t.Errorf("late PMT = %+v", layer)
	}
	// A section whose table_id is the last byte of the packet.
	late := packetWithPayload(0x0012, true, 0, []byte{0x00, 0x42})
	detail = writeDetailFixture(t, [][]byte{late, buildStuffingPacket(), buildStuffingPacket()}, options.Options{Offset: 0})
	if fields, layer := layerFields(detail, "psi_section"); !layer.Truncated || fields["table_id"].Value != "0x42" || fields["section_length"].Name != "" {
		t.Errorf("late section = %+v", layer)
	}
}

func TestPacketDetailIncompletePesUnits(t *testing.T) {
	other := buildStuffingPacket()
	// The PES header continues, but the next packet of the PID starts a new unit.
	start := packetWithPayload(0x0030, true, 0, []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80})
	restart := packetWithPayload(0x0030, true, 1, []byte{0x00, 0x00, 0x01, 0xE0})
	detail := writeDetailFixture(t, [][]byte{start, other, restart, other}, options.Options{Offset: 0})
	if u := detail.Unit; u.StreamID != "0xe0" || u.PTSMs != nil || len(u.HeaderPacketOffsets) != 1 || u.PacketsInRange != 1 {
		t.Errorf("cut header unit = %+v", u)
	}
	// Only the start code fits and nothing follows.
	tiny := packetWithPayload(0x0030, true, 0, []byte{0x00, 0x00, 0x01})
	detail = writeDetailFixture(t, [][]byte{tiny, other, other}, options.Options{Offset: 0})
	if u := detail.Unit; u.Kind != "pes" || u.StreamID != "" {
		t.Errorf("tiny PES unit = %+v", u)
	}
	// Searching back past other PIDs to the unit start.
	cont := packetWithPayload(0x0030, false, 1, []byte{0xAA})
	detail = writeDetailFixture(t, [][]byte{start, other, cont, other}, options.Options{Offset: 376})
	if u := detail.Unit; *u.FirstOffsetBytes != 0 || u.Position != 2 || *u.PreviousOffsetBytes != 0 {
		t.Errorf("continuation unit = %+v", u)
	}
}

func TestFindUnitReadErrors(t *testing.T) {
	data := bytes.Repeat(packetWithPayload(0x0030, false, 0, []byte{0xAA}), 6)
	f := &packetFile{r: failingReaderAt{data, 188 * 3}, size: int64(len(data)), packetSize: 188}
	tp, err := f.parsePacket(data[376:564], 376)
	if err != nil {
		t.Fatal(err)
	}
	// Packets after the third cannot be read and are skipped.
	if u := findUnit(f, tp, 376, 0, int64(len(data))); !u.StartedBeforeRange || u.Position != 3 || u.NextOffsetBytes != nil {
		t.Errorf("unit = %+v", u)
	}
}

func TestPacketsPageAtTheEndOfTheFile(t *testing.T) {
	info, _ := os.Stat(sample192Path)
	size := info.Size()
	// Without a hint, the packet size is still detected from the last packet.
	page, err := decodePacketsPage(t, sample192Path, options.Options{Offset: size - 192, Count: 1})
	if err != nil || page.Input.PacketSizeBytes != 192 || *page.FirstOffsetBytes != size-192 || page.TotalPackets != size/192 || len(page.Packets) != 1 || !page.Packets[0].Valid {
		t.Errorf("last packet page = %+v %v", page, err)
	}
	// The terminal cursor gives an empty page.
	page, err = decodePacketsPage(t, sample192Path, options.Options{Offset: size, Count: 1})
	if err != nil || page.Status != "complete" || !page.EndOfFile || len(page.Packets) != 0 || page.FirstOffsetBytes != nil || page.NextOffsetBytes != nil || page.TotalPackets != size/192 {
		t.Errorf("page at EOF = %+v %v", page, err)
	}
}

func TestPacketsPageOfAnEmptyFile(t *testing.T) {
	path := t.TempDir() + "/empty.ts"
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	page, err := decodePacketsPage(t, path, options.Options{Count: 100})
	if err != nil || page.Status != "complete" || !page.EndOfFile || len(page.Packets) != 0 || page.TotalPackets != 0 || page.Input.PacketSizeBytes != 0 {
		t.Errorf("empty file page = %+v %v", page, err)
	}
	if page, err := decodePacketsPage(t, path, options.Options{Count: 1, PacketSize: 204}); err == nil || !strings.Contains(page.Error, "unsupported packet size 204") {
		t.Errorf("empty file with a bad hint = %+v %v", page, err)
	}
	if page, err := decodePacketDetail(t, path, options.Options{}); err == nil || !strings.Contains(page.Error, "outside the file") {
		t.Errorf("detail of an empty file = %+v %v", page, err)
	}
}

// adaptationOnlyPacket builds an adaptation_field_control 2 packet on pid.
func adaptationOnlyPacket(pid uint16, cc uint8) []byte {
	pkt := bytes.Repeat([]byte{0xFF}, 188)
	pkt[0], pkt[1], pkt[2], pkt[3] = 0x47, byte(pid>>8), byte(pid), 0x20|cc
	pkt[4], pkt[5] = 183, 0x00
	return pkt
}

func TestPacketDetailSkipsPacketsWithoutPayload(t *testing.T) {
	pesStart := []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00}
	packets := [][]byte{
		packetWithPayload(0x100, true, 0, pesStart),
		adaptationOnlyPacket(0x100, 0),
		packetWithPayload(0x100, false, 1, []byte{0xAA}),
		packetWithPayload(0x100, true, 2, pesStart),
	}
	if u := writeDetailFixture(t, packets, options.Options{Offset: 188}).Unit; u.Kind != "none" || u.PacketsInRange != 0 {
		t.Errorf("unit of the adaptation-only packet = %+v", u)
	}
	u := writeDetailFixture(t, packets, options.Options{Offset: 2 * 188}).Unit
	if u.Kind != "pes" || u.Position != 2 || u.PacketsInRange != 2 || *u.PreviousOffsetBytes != 0 || !reflect.DeepEqual(u.HeaderPacketOffsets, []int64{0}) {
		t.Errorf("unit of the continuation packet = %+v", u)
	}
	u = writeDetailFixture(t, packets, options.Options{Offset: 0}).Unit
	if u.PacketsInRange != 2 || *u.NextOffsetBytes != 2*188 {
		t.Errorf("unit of the first packet = %+v", u)
	}
}
