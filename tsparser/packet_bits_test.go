package tsparser

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// packetBitString spells raw as a string of '0' and '1', most significant bit
// of each byte first, so string index i is packet bit offset i.
func packetBitString(raw []byte) string {
	var sb strings.Builder
	for _, b := range raw {
		fmt.Fprintf(&sb, "%08b", b)
	}
	return sb.String()
}

// leadingNumber parses the number a field value starts with ("0x0100",
// "3 (adaptation field and payload)", "144000 (1.600000 s)").
func leadingNumber(t *testing.T, value string) uint64 {
	t.Helper()
	token, _, _ := strings.Cut(value, " ")
	base := 10
	if hexDigits, ok := strings.CutPrefix(token, "0x"); ok {
		token, base = hexDigits, 16
	}
	n, err := strconv.ParseUint(token, base, 64)
	if err != nil {
		t.Fatalf("value %q is not numeric: %v", value, err)
	}
	return n
}

// checkFieldBits verifies that the bit ranges of f lie in its byte span, in
// wire order without overlap, leave some bit of the span out, and reproduce
// the decoded value from the raw bits. It reports the bits of each range.
func checkFieldBits(t *testing.T, where string, f PacketField, bits string) []string {
	t.Helper()
	spanStart, spanEnd := f.ByteOffset*8, (f.ByteOffset+f.ByteLength)*8
	next, count := spanStart, 0
	var parts []string
	for _, r := range f.Bits {
		if r.BitLength < 1 || r.BitOffset < next || r.BitOffset+r.BitLength > spanEnd {
			t.Fatalf("%s: range %+v outside the span [%d, %d) or before %d", where, r, spanStart, spanEnd, next)
		}
		parts = append(parts, bits[r.BitOffset:r.BitOffset+r.BitLength])
		next, count = r.BitOffset+r.BitLength, count+r.BitLength
	}
	// The span is the tightest one: it starts and ends in the ranges' bytes.
	if f.Bits[0].BitOffset/8 != f.ByteOffset || (next+7)/8*8 != spanEnd || count == f.ByteLength*8 {
		t.Fatalf("%s: ranges %+v do not match the span %d+%d", where, f.Bits, f.ByteOffset, f.ByteLength)
	}
	return parts
}

func parseBits(t *testing.T, s string) uint64 {
	t.Helper()
	n, err := strconv.ParseUint(s, 2, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPacketFieldBitsReproduceValues(t *testing.T) {
	for _, tc := range []struct {
		path       string
		packetSize int
	}{
		{sample188Path, 188}, {sample192Path, 192}, {twoPrograms188Path, 188}, {twoPrograms192Path, 192},
	} {
		data, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for at := 0; at+tc.packetSize <= len(data); at += tc.packetSize {
			raw := data[at : at+tc.packetSize]
			bits := packetBitString(raw)
			for _, layer := range decodePacketLayers(raw) {
				for _, f := range append(layer.Fields, flatten(layer.Rows)...) {
					if f.Bits == nil {
						continue
					}
					where := fmt.Sprintf("%s@%d %s.%s", tc.path, at, layer.Name, f.Name)
					parts := checkFieldBits(t, where, f, bits)
					want := leadingNumber(t, f.Value)
					var got uint64
					switch f.Name {
					case "program_clock_reference", "original_program_clock_reference":
						// The 33-bit base and the 9-bit extension.
						if len(parts) != 2 || len(parts[0]) != 33 || len(parts[1]) != 9 {
							t.Fatalf("%s: ranges %+v", where, f.Bits)
						}
						got = parseBits(t, parts[0])*300 + parseBits(t, parts[1])
					default:
						got = parseBits(t, strings.Join(parts, ""))
					}
					if got != want {
						t.Errorf("%s: bits %v give %d, value is %q", where, parts, got, f.Value)
					}
					seen[layer.Name+"."+f.Name] = true
				}
			}
		}
		for _, name := range []string{
			"ts_header.PID", "ts_header.payload_unit_start_indicator", "ts_header.continuity_counter",
			"adaptation_field.PCR_flag", "adaptation_field.program_clock_reference",
			"pes_header.PES_scrambling_control", "pes_header.PTS", "pes_header.DTS",
			"pat.section_length", "pat.version_number", "pat.current_next_indicator", "pat.program_map_PID",
			"pmt.PCR_PID", "pmt.program_info_length", "pmt.elementary_PID", "pmt.ES_info_length",
		} {
			if !seen[name] {
				t.Errorf("%s: no %s field with bits", tc.path, name)
			}
		}
		if tc.packetSize == 192 && !seen["m2ts_header.arrival_time_stamp"] {
			t.Errorf("%s: no arrival_time_stamp bits", tc.path)
		}
	}
}

func TestPacketFieldBitRanges(t *testing.T) {
	for _, tc := range []struct {
		path   string
		offset int64
		shift  int
	}{
		{twoPrograms188Path, 29892, 0},
		{twoPrograms192Path, 29892 / 188 * 192, 32},
	} {
		detail, err := decodePacketDetail(t, tc.path, options.Options{Offset: tc.offset})
		if err != nil {
			t.Fatal(err)
		}
		header, _ := layerFields(detail, "ts_header")
		for name, want := range map[string]BitRange{
			"PID":                          {11, 13},
			"payload_unit_start_indicator": {9, 1},
			"continuity_counter":           {28, 4},
		} {
			want.BitOffset += tc.shift
			if got := header[name].Bits; !reflect.DeepEqual(got, []BitRange{want}) {
				t.Errorf("%s: %s bits = %+v, want %+v", tc.path, name, got, want)
			}
		}
		if header["sync_byte"].Bits != nil {
			t.Errorf("%s: whole-byte sync_byte has bits %+v", tc.path, header["sync_byte"].Bits)
		}
		// PTS starts at byte 21 (+4): 3 bits after the prefix, then two
		// 15-bit runs, each followed by a marker bit.
		pes, _ := layerFields(detail, "pes_header")
		p := 21*8 + tc.shift
		if want := []BitRange{{p + 4, 3}, {p + 8, 15}, {p + 24, 15}}; !reflect.DeepEqual(pes["PTS"].Bits, want) {
			t.Errorf("%s: PTS bits = %+v, want %+v", tc.path, pes["PTS"].Bits, want)
		}
		af, _ := layerFields(detail, "adaptation_field")
		p = 6*8 + tc.shift
		if want := []BitRange{{p, 33}, {p + 39, 9}}; !reflect.DeepEqual(af["program_clock_reference"].Bits, want) {
			t.Errorf("%s: PCR bits = %+v, want %+v", tc.path, af["program_clock_reference"].Bits, want)
		}
	}
}
