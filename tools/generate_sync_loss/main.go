// Command generate_sync_loss writes a short, deterministic transport stream
// whose packet alignment is damaged in several places, to test and
// demonstrate the analyzer's synchronization check. It is intentionally
// invalid: use it to test sync-loss detection and recovery, never as an
// example of a compliant stream.
//
// The underlying stream has one program (program_number 1, PMT PID 0x1000)
// with MPEG-2 video on PID 0x0100, which also carries the PCR. Every packet
// occupies one 10 ms slot of a constant-rate multiplex (150.4 kbit/s); the
// 200 slots last 2 s. The PAT is sent in slots 0 and 1 and the PMT in slots 2
// and 3, so the analysis starts at slot 4; both are repeated every 500 ms
// (PAT in slots 25, 75, ..., PMT in slots 27, 77, ...). From slot 4, the
// other even slots carry one video PES packet each with a PCR and a PTS
// (100 ms ahead of the PCR), and the other odd slots are null packets.
//
// Damage, by slot of the underlying stream; each can be left out with -omit:
//
//   - sync-byte: the sync byte of video slot 40 is 0x00; the alignment is
//     unchanged, so that packet is skipped.
//   - insert-byte: one 0xFF byte is inserted after byte 100 of null slot 61,
//     so slot 62 and every later packet start one byte late.
//   - delete-byte: byte 100 of null slot 81 is deleted, so the boundary
//     expected for slot 82 falls on its second byte and slot 82 is lost.
//   - garbage: 1000 bytes are inserted between slots 120 and 121. They are
//     zero except for false sync bytes (0x47) one packet apart at four
//     positions, one fewer than a new alignment requires.
//   - truncated-tail: the first 100 bytes of a null packet follow the last
//     slot.
//
// -unrecoverable additionally sets the sync byte of slot 197 to 0x00, so
// fewer than five aligned packets follow the last loss before the input
// ends.
//
// With -m2ts, each packet gets a 4-byte TP_extra_header whose
// arrival_time_stamp advances by one 10 ms slot per packet; the damage is
// applied to the 192-byte packets at the same byte positions within the
// 188-byte packet, so the inserted and deleted bytes are in the same places
// relative to the sync byte.
//
// With -trailer, each packet is followed by a 16-byte trailer, making 204-byte
// packets: deterministic filler (not Reed-Solomon parity) in 0x80..0xBF, except
// that byte 5 of the trailers of slots 82 to 85 is a false sync byte (0x47),
// four 204 bytes apart, one fewer than a new alignment requires. The
// resynchronization after the delete-byte damage scans across them. The
// damage is at the same positions relative to the sync byte.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const (
	tsPacketSize = 188
	slots        = 200
	pmtPid       = 0x1000
	videoPid     = 0x0100
	nullPid      = 0x1FFF
	// slotMs is the duration of one slot (one packet).
	slotMs = 10
	// slotTicks27 is one slot of the 27 MHz system clock.
	slotTicks27 = slotMs * 27_000
	// baseMs is the PCR time of slot 0.
	baseMs = 10_000
	// esPayload is the filler elementary-stream payload of each PES packet.
	esPayload = 16
	// garbageBytes is the length of the garbage block.
	garbageBytes = 1000
	// tailBytes is the length of the truncated packet at the end.
	tailBytes = 100
	// trailerSize is the length of the trailer of a 204-byte packet.
	trailerSize = 16
)

// Damage names accepted by -omit.
const (
	damageSyncByte      = "sync-byte"
	damageInsertByte    = "insert-byte"
	damageDeleteByte    = "delete-byte"
	damageGarbage       = "garbage"
	damageTruncatedTail = "truncated-tail"
)

var damages = []string{damageSyncByte, damageInsertByte, damageDeleteByte, damageGarbage, damageTruncatedTail}

func crc32MPEG(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc ^= uint32(b) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// section completes a PSI section: it fills in section_length and appends the
// CRC32. body is everything after the 3-byte header.
func section(tableID byte, body []byte) []byte {
	length := len(body) + 4
	s := []byte{tableID, 0xB0 | byte(length>>8), byte(length)}
	s = append(s, body...)
	crc := crc32MPEG(s)
	return append(s, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
}

func patSection() []byte {
	return section(0x00, []byte{
		0x00, 0x01, // transport_stream_id 1
		0xC1,       // version 0, current_next_indicator 1
		0x00, 0x00, // section_number, last_section_number
		0x00, 0x01, 0xE0 | pmtPid>>8, pmtPid & 0xFF, // program 1 -> PMT PID
	})
}

func pmtSection() []byte {
	return section(0x02, []byte{
		0x00, 0x01, // program_number 1
		0xC1,
		0x00, 0x00,
		0xE0 | videoPid>>8, videoPid & 0xFF, // PCR_PID
		0xF0, 0x00, // program_info_length 0
		0x02, 0xE0 | videoPid>>8, videoPid & 0xFF, 0xF0, 0x00, // MPEG-2 video
	})
}

func header(pid uint16, pusi bool, afc, cc byte) []byte {
	b1 := byte(pid >> 8)
	if pusi {
		b1 |= 0x40
	}
	return []byte{0x47, b1, byte(pid), afc<<4 | cc&0x0F}
}

func psiPacket(pid uint16, cc byte, s []byte) []byte {
	p := append(header(pid, true, 1, cc), 0x00) // pointer_field
	p = append(p, s...)
	for len(p) < tsPacketSize {
		p = append(p, 0xFF)
	}
	return p
}

func nullPacket() []byte {
	p := header(nullPid, false, 1, 0)
	for len(p) < tsPacketSize {
		p = append(p, 0xFF)
	}
	return p
}

// pcrBytes encodes a 27 MHz PCR as program_clock_reference_base (33 bits),
// 6 reserved bits and program_clock_reference_extension (9 bits).
func pcrBytes(pcr uint64) []byte {
	base, ext := pcr/300, pcr%300
	return []byte{
		byte(base >> 25), byte(base >> 17), byte(base >> 9), byte(base >> 1),
		byte(base<<7) | 0x7E | byte(ext>>8), byte(ext),
	}
}

// ptsBytes encodes a 90 kHz PTS with the '0010' prefix of a PTS-only header.
func ptsBytes(pts uint64) []byte {
	return []byte{
		0x21 | byte(pts>>29)&0x0E,
		byte(pts >> 22), byte(pts>>14) | 0x01,
		byte(pts >> 7), byte(pts<<1) | 0x01,
	}
}

// videoPacket is one TS packet holding a whole PES packet with a PTS and a
// PCR, padded with adaptation-field stuffing.
func videoPacket(cc byte, pts, pcr uint64) []byte {
	pes := []byte{0x00, 0x00, 0x01, 0xE0}
	pesLength := 3 + 5 + esPayload
	pes = append(pes, byte(pesLength>>8), byte(pesLength), 0x80, 0x80, 0x05)
	pes = append(pes, ptsBytes(pts)...)
	for i := range esPayload {
		pes = append(pes, byte(0xA0+i))
	}
	afLength := tsPacketSize - 4 - 1 - len(pes)
	af := []byte{byte(afLength), 0x10} // PCR_flag
	af = append(af, pcrBytes(pcr)...)
	for len(af) < 1+afLength {
		af = append(af, 0xFF)
	}
	p := append(header(videoPid, true, 3, cc), af...)
	return append(p, pes...)
}

// packets returns the 188-byte packets of the undamaged stream, one per slot.
func packets() [][]byte {
	var out [][]byte
	var patCC, pmtCC, videoCC byte
	for slot := range slots {
		var p []byte
		switch {
		case slot == 0 || slot == 1 || slot%50 == 25:
			p = psiPacket(0x0000, patCC, patSection())
			patCC++
		case slot == 2 || slot == 3 || slot%50 == 27:
			p = psiPacket(pmtPid, pmtCC, pmtSection())
			pmtCC++
		case slot%2 == 0:
			pcr := uint64(baseMs*27_000) + uint64(slot)*slotTicks27
			pts := uint64(baseMs+slot*slotMs+100) * 90
			p = videoPacket(videoCC, pts, pcr)
			videoCC++
		default:
			p = nullPacket()
		}
		out = append(out, p)
	}
	return out
}

// m2ts prefixes a 188-byte packet of slot with a TP_extra_header whose
// arrival_time_stamp advances by one slot per packet.
func m2ts(slot int, p []byte) []byte {
	ats := uint32(slot*slotTicks27) & 0x3FFFFFFF
	return append([]byte{byte(ats >> 24), byte(ats >> 16), byte(ats >> 8), byte(ats)}, p...)
}

// trailer returns the 16-byte trailer that follows the packet of slot in a
// 204-byte stream: filler in 0x80..0xBF, never a sync byte, except for the
// false sync bytes in slots 82 to 85.
func trailer(slot int) []byte {
	t := make([]byte, trailerSize)
	for k := range t {
		t[k] = 0x80 | byte((slot+k)&0x3F)
	}
	if slot >= 82 && slot <= 85 {
		t[5] = 0x47
	}
	return t
}

// generate returns the damaged stream. omit leaves damage out; unrecoverable
// adds the final loss.
func generate(omit map[string]bool, asM2TS, withTrailer, unrecoverable bool) []byte {
	with := func(damage string) bool { return !omit[damage] }
	header := 0
	if asM2TS {
		header = 4
	}
	var out []byte
	for slot, p := range packets() {
		if asM2TS {
			p = m2ts(slot, p)
		}
		if withTrailer {
			p = append(p, trailer(slot)...)
		}
		switch {
		case slot == 40 && with(damageSyncByte), slot == 197 && unrecoverable:
			p[header] = 0x00
		case slot == 61 && with(damageInsertByte):
			at := header + 100
			p = append(p[:at:at], append([]byte{0xFF}, p[at:]...)...)
		case slot == 81 && with(damageDeleteByte):
			at := header + 100
			p = append(p[:at:at], p[at+1:]...)
		case slot == 121 && with(damageGarbage):
			garbage := make([]byte, garbageBytes)
			for k := range 4 {
				garbage[10+k*len(p)] = 0x47
			}
			out = append(out, garbage...)
		}
		out = append(out, p...)
	}
	if with(damageTruncatedTail) {
		tail := nullPacket()
		if asM2TS {
			tail = m2ts(slots, tail)
		}
		if withTrailer {
			tail = append(tail, trailer(slots)...)
		}
		out = append(out, tail[:tailBytes]...)
	}
	return out
}

func parseOmit(value string) (map[string]bool, error) {
	omit := map[string]bool{}
	if value == "" {
		return omit, nil
	}
	for _, name := range strings.Split(value, ",") {
		if name == "all" {
			for _, d := range damages {
				omit[d] = true
			}
			continue
		}
		known := false
		for _, d := range damages {
			known = known || d == name
		}
		if !known {
			return nil, fmt.Errorf("unknown damage %q (want all or %s)", name, strings.Join(damages, ", "))
		}
		omit[name] = true
	}
	return omit, nil
}

func main() {
	output := flag.String("output", "", "output transport stream")
	asM2TS := flag.Bool("m2ts", false, "write 192-byte M2TS packets")
	withTrailer := flag.Bool("trailer", false, "write 204-byte packets: each TS packet followed by a 16-byte trailer")
	omitFlag := flag.String("omit", "", "comma-separated damage to leave out ("+strings.Join(damages, ", ")+", or all)")
	unrecoverable := flag.Bool("unrecoverable", false, "also damage slot 197 so that the last loss is not recovered")
	flag.Parse()
	if *output == "" || *asM2TS && *withTrailer {
		fmt.Fprintln(os.Stderr, "usage: generate_sync_loss -output <out.ts> [-m2ts | -trailer] [-omit damage,...] [-unrecoverable]")
		os.Exit(2)
	}
	omit, err := parseOmit(*omitFlag)
	if err == nil {
		err = os.WriteFile(*output, generate(omit, *asM2TS, *withTrailer, *unrecoverable), 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
