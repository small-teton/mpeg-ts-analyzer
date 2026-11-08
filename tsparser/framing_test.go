package tsparser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// withTestTrailers follows every 188-byte packet of data with the 16-byte
// trailer the fixture generators write (-trailer): filler in 0x80..0xBF that
// is never a sync byte.
func withTestTrailers(data []byte) []byte {
	out := make([]byte, 0, len(data)/tsPayloadSize*trailerPacketSize)
	for i := 0; i+tsPayloadSize <= len(data); i += tsPayloadSize {
		out = append(out, data[i:i+tsPayloadSize]...)
		for k := range trailerSize {
			out = append(out, 0x80|byte((i/tsPayloadSize+k)&0x3F))
		}
	}
	return out
}

func writeTestFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFramingHelpers(t *testing.T) {
	packet := make([]byte, trailerPacketSize)
	for i := range packet {
		packet[i] = byte(i)
	}
	for _, tc := range []struct {
		size, offset int
	}{{188, 0}, {192, 4}, {204, 0}} {
		if got := tsOffset(tc.size); got != tc.offset {
			t.Errorf("tsOffset(%d) = %d", tc.size, got)
		}
		ts := tsBytes(packet[:tc.size], tc.size)
		if len(ts) != tsPayloadSize || ts[0] != byte(tc.offset) {
			t.Errorf("tsBytes(%d) = %d bytes from %d", tc.size, len(ts), ts[0])
		}
		if !isPacketSize(tc.size) {
			t.Errorf("isPacketSize(%d) = false", tc.size)
		}
	}
	for _, size := range []int{0, 187, 189, 200, 208} {
		if isPacketSize(size) {
			t.Errorf("isPacketSize(%d) = true", size)
		}
	}
}

// A 204-byte stream is detected from five sync bytes 204 bytes apart; 188-
// and 192-byte streams keep their size even when their payload has sync bytes
// 204 bytes apart, and random data is never taken for 204-byte packets.
func TestDetectPacketSize204(t *testing.T) {
	stream188 := syncTestStream(nil, 20)
	stream204 := withTestTrailers(stream188)
	if got := detectPacketSize(stream204); got != trailerPacketSize {
		t.Errorf("204-byte stream detected as %d", got)
	}
	// Four packets are one fewer than the five sync bytes 204 bytes need.
	if got := detectPacketSize(stream204[:4*trailerPacketSize]); got != tsPayloadSize {
		t.Errorf("four 204-byte packets detected as %d", got)
	}
	if !hasTrailerAlignment(stream204) || hasTrailerAlignment(stream204[:4*trailerPacketSize]) {
		t.Error("hasTrailerAlignment")
	}
	// A false sync byte in every trailer does not hide the alignment.
	falseSyncs := bytes.Clone(stream204)
	for i := tsPayloadSize; i < len(falseSyncs); i += trailerPacketSize {
		falseSyncs[i] = syncByteValue
	}
	if got := detectPacketSize(falseSyncs); got != trailerPacketSize {
		t.Errorf("204-byte stream with false trailer sync bytes detected as %d", got)
	}
	// Regression (PR #195 review): five 204-byte packets, the only 204-byte
	// candidate, whose first trailer byte and byte 172 of the second packet
	// are 0x47. Those are legal trailer and payload values; two sync bytes
	// 188 bytes apart are weaker evidence than five, so the stream stays a
	// 204-byte one.
	five := bytes.Clone(stream204[:5*trailerPacketSize])
	five[tsPayloadSize], five[trailerPacketSize+172] = syncByteValue, syncByteValue
	if got := detectPacketSize(five); got != trailerPacketSize {
		t.Errorf("204-byte packets with 0x47 188 and 376 bytes after a sync byte detected as %d", got)
	}

	// 0x47 bytes 204 bytes apart from a real sync byte, in the payload of
	// the following null packets.
	for _, size := range []int{tsPayloadSize, m2tsPacketSize} {
		var data []byte
		for n := range 20 {
			if size == m2tsPacketSize {
				data = append(data, 0, 0, 0, byte(n))
			}
			data = append(data, nullTestPacket()...)
		}
		sync := tsOffset(size)
		for k := 1; k < strictSyncBytes; k++ {
			data[sync+k*trailerPacketSize] = syncByteValue
		}
		if got := detectPacketSize(data); got != size {
			t.Errorf("%d-byte stream with sync bytes 204 bytes apart detected as %d", size, got)
		}
	}

	rng := rand.New(rand.NewPCG(186, 204))
	buf := make([]byte, 65536)
	for range 200 {
		for i := range buf {
			buf[i] = byte(rng.Uint32())
		}
		if hasTrailerAlignment(buf) {
			t.Fatal("random data has a 204-byte alignment")
		}
	}
}

var hexOffset = regexp.MustCompile(`\b0x[0-9a-f]{8}\b`)

// normalizeOffsets rewrites the offsets of a document written for a 188-byte
// stream into those of the same packets in the 204-byte stream: every number
// under a key that names an offset is a packet boundary, which moves from
// 188·n to 204·n, and so does every 0x%08x offset in a message.
func normalizeOffsets(t *testing.T, v any, key string) any {
	t.Helper()
	switch v := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, item := range v {
			out[k] = normalizeOffsets(t, item, k)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizeOffsets(t, item, key)
		}
		return out
	case string:
		// Messages give offsets as 0x%08x.
		return hexOffset.ReplaceAllStringFunc(v, func(hex string) string {
			n, _ := strconv.ParseInt(hex[2:], 16, 64)
			return fmt.Sprintf("0x%08x", n/tsPayloadSize*trailerPacketSize)
		})
	case float64:
		if !strings.Contains(key, "offset") {
			return v
		}
		if int64(v)%tsPayloadSize != 0 {
			t.Errorf("%s %v is not a packet boundary", key, v)
		}
		return float64(int64(v) / tsPayloadSize * trailerPacketSize)
	default:
		return v
	}
}

func decodeDocument(t *testing.T, path string, opts options.Options) map[string]any {
	t.Helper()
	opts.Format = "json"
	out := captureStdout(t, func() { _ = ParseTsFile(path, opts) })
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, out)
	}
	input := doc["input"].(map[string]any)
	delete(input, "path")
	delete(input, "file_size_bytes")
	return doc
}

// The first 188 bytes of every 204-byte packet give the same results as the
// 188-byte stream: every committed 188-byte sample whose packets are aligned,
// followed by trailers, has the same analysis and summary documents, with
// the offsets of the same packets and packet_size_bytes 204.
func TestTrailerPacketsMatch188(t *testing.T) {
	paths, err := filepath.Glob("../sample_data/sample_188byte_*.ts")
	if err != nil || len(paths) < 10 {
		t.Fatalf("samples %v %v", paths, err)
	}
	for _, path := range paths {
		if strings.Contains(path, "sync_loss") {
			continue // damaged alignment: tools/generate_sync_loss -trailer
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(data)%tsPayloadSize != 0 {
			t.Fatalf("%s is not whole packets", path)
		}
		path204 := writeTestFile(t, "sample.ts", withTestTrailers(data))
		for _, opts := range []options.Options{
			{Output: "analysis"},
			{Output: "summary", DumpBitrate: true, DumpPcrJitter: true, Program: 1},
		} {
			want := normalizeOffsets(t, decodeDocument(t, path, opts), "").(map[string]any)
			want["input"].(map[string]any)["packet_size_bytes"] = float64(trailerPacketSize)
			got := decodeDocument(t, path204, opts)
			if diff := firstDifference(got, want, ""); diff != "" {
				t.Errorf("%s %s: %s", path, opts.Output, diff)
			}
		}
	}
}

// firstDifference names the first place where got and want differ, or
// returns "" when they are equal.
func firstDifference(got, want any, at string) string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(w) {
			return fmt.Sprintf("%s: got %v, want %v", at, got, want)
		}
		for k := range w {
			if d := firstDifference(g[k], w[k], at+"/"+k); d != "" {
				return d
			}
		}
		return ""
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Sprintf("%s: got %v, want %v", at, got, want)
		}
		for i := range w {
			if d := firstDifference(g[i], w[i], fmt.Sprintf("%s/%d", at, i)); d != "" {
				return d
			}
		}
		return ""
	default:
		if !reflect.DeepEqual(got, want) {
			return fmt.Sprintf("%s: got %v, want %v", at, got, want)
		}
		return ""
	}
}

// A range whose --offset and --limit cut through physical packets starts at
// the next packet boundary and ends with the last complete packet, as for
// 188-byte packets.
func TestTrailerPacketsRange(t *testing.T) {
	data, err := os.ReadFile(sample188Path)
	if err != nil {
		t.Fatal(err)
	}
	path204 := writeTestFile(t, "sample.ts", withTestTrailers(data))
	cut := func(ps int64) options.Options {
		return options.Options{Output: "analysis", Offset: 100*ps + 50, Limit: 900*ps + 77}
	}
	want := normalizeOffsets(t, func() map[string]any {
		doc := decodeDocument(t, sample188Path, cut(tsPayloadSize))
		delete(doc, "input")
		return doc
	}(), "")
	got := decodeDocument(t, path204, cut(trailerPacketSize))
	if got["input"].(map[string]any)["packet_size_bytes"] != float64(trailerPacketSize) || int64(got["analysis_start_offset_bytes"].(float64))%trailerPacketSize != 0 {
		t.Errorf("range: %v", got)
	}
	delete(got, "input")
	if diff := firstDifference(got, want, ""); diff != "" {
		t.Error(diff)
	}
}

// Packet pages and packet detail step by 204 bytes, keep the trailer out of
// the decoded TS packet and list it as a final "trailer" layer.
func TestTrailerPacketsPagesAndDetail(t *testing.T) {
	const path = "../sample_data/sample_204byte_all_findings.ts"
	for _, hint := range []int{0, trailerPacketSize} {
		page, err := decodePacketsPage(t, path, options.Options{Offset: 10*trailerPacketSize + 60, Count: 3, PacketSize: hint})
		if err != nil || page.Input.PacketSizeBytes != trailerPacketSize || page.TotalPackets != 120 || *page.FirstOffsetBytes != 11*trailerPacketSize ||
			*page.NextOffsetBytes != 14*trailerPacketSize || len(page.Packets) != 3 || page.Packets[1].OffsetBytes != 12*trailerPacketSize || page.Packets[1].PID != "0x0100" {
			t.Errorf("hint %d: page %s %v", hint, toJSON(t, page), err)
		}
	}

	// The packet in slot 12 is the video packet with a PCR that starts a PES
	// packet; it decodes as in the 188-byte fixture.
	const slot = 12
	detail, err := decodePacketDetail(t, path, options.Options{Offset: slot * trailerPacketSize})
	if err != nil {
		t.Fatal(err)
	}
	want, err := decodePacketDetail(t, "../sample_data/sample_188byte_all_findings.ts", options.Options{Offset: slot * tsPayloadSize})
	if err != nil {
		t.Fatal(err)
	}
	layers := detail.Packet.Layers
	trailer := layers[len(layers)-1]
	if detail.Input.PacketSizeBytes != trailerPacketSize || len(detail.Packet.Bytes) != 2*trailerPacketSize ||
		detail.Packet.Bytes[:2*tsPayloadSize] != want.Packet.Bytes || detail.Packet.Bytes[2*tsPayloadSize:] != "8c8d8e8f909192939495969798999a9b" {
		t.Errorf("bytes %s", detail.Packet.Bytes)
	}
	if toJSON(t, trailer) != `{"name":"trailer","fields":[{"name":"trailer_bytes","value":"16 bytes","byte_offset":188,"byte_length":16}],`+
		`"bit_layout":[{"id":"trailer.trailer_bytes@1504","label":"trailer_bytes","kind":"unknown","bits":[{"bit_offset":1504,"bit_length":128}],"value":"16 bytes","field":"trailer_bytes"}]}` {
		t.Errorf("trailer %s", toJSON(t, trailer))
	}
	if toJSON(t, layers[:len(layers)-1]) != toJSON(t, want.Packet.Layers) {
		t.Errorf("layers\n got %s\nwant %s", toJSON(t, layers), toJSON(t, want.Packet.Layers))
	}
	if detail.Unit.Kind != "pes" || *detail.Unit.FirstOffsetBytes != slot*trailerPacketSize || detail.Unit.PacketsInRange != want.Unit.PacketsInRange || *detail.Unit.PTSMs != *want.Unit.PTSMs {
		t.Errorf("unit %s, want %s", toJSON(t, detail.Unit), toJSON(t, want.Unit))
	}

	// A PSI packet continues in the payload, not in the trailer: the PMT of
	// slot 3 has no field past the TS packet.
	pmt, err := decodePacketDetail(t, path, options.Options{Offset: 3 * trailerPacketSize})
	if err != nil {
		t.Fatal(err)
	}
	for _, layer := range pmt.Packet.Layers[:len(pmt.Packet.Layers)-1] {
		for _, field := range layer.Fields {
			if field.ByteOffset+field.ByteLength > tsPayloadSize {
				t.Errorf("%s/%s reaches the trailer: %s", layer.Name, field.Name, toJSON(t, field))
			}
		}
	}

	if _, err := decodePacketDetail(t, path, options.Options{Offset: slot*trailerPacketSize + tsPayloadSize}); err == nil {
		t.Error("an offset inside a trailer was accepted")
	}
}
