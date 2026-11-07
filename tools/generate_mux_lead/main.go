// Command generate_mux_lead writes a short, deterministic transport stream
// with known mux leads: a remux with a large audio preload, across a PCR and
// PTS wraparound and a declared PCR discontinuity. It is intentionally unusual
// (filler elementary-stream payload, a transport error, a PES packet
// multiplexed after its PTS): use it to test the mux lead report, never as an
// example of a compliant stream.
//
// The stream has one program (program_number 1, PMT PID 0x1000) with MPEG-2
// video (stream_type 0x02) on PID 0x0100, which also carries the PCR, MPEG-1
// audio (0x03) on PID 0x0101, AAC (0x0F) on PID 0x0102 and PES private data
// (0x06) on PID 0x0103. Every packet occupies one 20 ms slot of a
// constant-rate multiplex (75.2 kbit/s); there are 240 slots (4.8 s). Slot 0 is
// a null packet, the PAT is sent in slots 1 and 2 and the PMT in slots 3 and 4
// (the analysis starts at slot 5); the PAT repeats in the slots 20k+19 and the
// PMT in the slots 20k+3 from 23. From slot 6, video PES packets take the even
// slots (every 40 ms, each packet with a PCR), audio PES packets the slots
// 4k+1 from 5 (every 80 ms) and AAC PES packets the slots 8k+3 from 11 that PSI
// does not use; PID 0x0103 carries one PES packet in slot 15. The remaining
// slots are null packets.
//
// Clock: the 90 kHz system clock of slot s is 2^33 - 100 × 1800 + s × 1800
// (modulo 2^33), so the PCR wraps at slot 100; from slot 150 the clock is 10 s
// later, and the PCR of slot 150 carries discontinuity_indicator. Every
// PTS/DTS is the clock of its slot plus its lead, modulo 2^33:
//
//   - video: DTS lead 700 + 2 × ((s / 2) mod 5) ms (700 to 708 ms), PTS 40 ms
//     after the DTS;
//   - audio: PTS lead 1500 ms (a large audio preload);
//   - AAC: PTS lead 650 ms, except slot 51 (-20 ms: multiplexed after its
//     PTS) and slot 67 (no PTS);
//   - private data: PTS lead 300 ms.
//
// The audio PES of slot 61 has transport_error_indicator set. With -m2ts,
// each packet gets a 4-byte TP_extra_header whose arrival_time_stamp advances
// by one 20 ms slot per packet. With -trailer, each packet is followed by a
// 16-byte trailer, making 204-byte packets; the trailer is deterministic
// filler that is never a sync byte.
package main

import (
	"flag"
	"fmt"
	"os"
)

const (
	tsPacketSize = 188
	slots        = 240
	pmtPid       = 0x1000
	videoPid     = 0x0100
	audioPid     = 0x0101
	aacPid       = 0x0102
	privatePid   = 0x0103
	nullPid      = 0x1FFF
	// slotMs is the duration of one slot (one packet).
	slotMs = 20
	// slot90 is one slot of the 90 kHz clock.
	slot90 = slotMs * 90
	// wrap90 is the period of the 33-bit 90 kHz clock.
	wrap90 = uint64(1) << 33
	// wrapSlot is the slot at which the clock wraps.
	wrapSlot = 100
	// jumpSlot is the slot of the declared discontinuity, jumpMs its size.
	jumpSlot = 150
	jumpMs   = 10_000
	// teiSlot is the audio packet with transport_error_indicator set.
	teiSlot = 61
	// esPayload is the filler elementary-stream payload of each PES packet.
	esPayload = 16
)

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
	body := []byte{
		0x00, 0x01, // program_number 1
		0xC1,
		0x00, 0x00,
		0xE0 | videoPid>>8, videoPid & 0xFF, // PCR_PID
		0xF0, 0x00, // program_info_length 0
	}
	for _, es := range []struct {
		streamType byte
		pid        uint16
	}{{0x02, videoPid}, {0x03, audioPid}, {0x0F, aacPid}, {0x06, privatePid}} {
		body = append(body, es.streamType, 0xE0|byte(es.pid>>8), byte(es.pid), 0xF0, 0x00)
	}
	return section(0x02, body)
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

// timestampBytes encodes a 90 kHz PTS or DTS with a 4-bit prefix and its
// three marker_bits.
func timestampBytes(prefix byte, ts uint64) []byte {
	return []byte{
		prefix<<4 | byte(ts>>29)&0x0E | 0x01,
		byte(ts >> 22), byte(ts>>14) | 0x01,
		byte(ts >> 7), byte(ts<<1) | 0x01,
	}
}

// pesHeader returns a PES header with the given optional fields. length is
// PES_packet_length.
func pesHeader(streamID byte, length int, flags2 byte, fields []byte) []byte {
	h := []byte{0x00, 0x00, 0x01, streamID, byte(length >> 8), byte(length), 0x80, flags2, byte(len(fields))}
	return append(h, fields...)
}

// esBytes is the filler elementary-stream payload.
func esBytes() []byte {
	es := make([]byte, esPayload)
	for i := range es {
		es[i] = byte(0xA0 + i)
	}
	return es
}

// boundedPes returns a PES packet with a PTS alone (none when untimed) and a
// PES_packet_length that covers it.
func boundedPes(streamID byte, pts uint64, untimed bool) []byte {
	if untimed {
		return append(pesHeader(streamID, 3+esPayload, 0x00, nil), esBytes()...)
	}
	fields := timestampBytes(0x2, pts)
	return append(pesHeader(streamID, 3+len(fields)+esPayload, 0x80, fields), esBytes()...)
}

// videoPes returns a video PES with PTS and DTS and PES_packet_length 0.
func videoPes(pts, dts uint64) []byte {
	fields := append(timestampBytes(0x3, pts), timestampBytes(0x1, dts)...)
	return append(pesHeader(0xE0, 0, 0xC0, fields), esBytes()...)
}

// pesPacket is one TS packet with payload_unit_start_indicator holding data,
// padded with adaptation-field stuffing. pcr is written when hasPcr is set,
// with discontinuity_indicator when discontinuity is set.
func pesPacket(pid uint16, cc byte, data []byte, pcr uint64, hasPcr, discontinuity bool) []byte {
	afLength := tsPacketSize - 4 - 1 - len(data)
	af := []byte{byte(afLength), 0x00}
	if hasPcr {
		af[1] = 0x10 // PCR_flag
		af = append(af, pcrBytes(pcr)...)
	}
	if discontinuity {
		af[1] |= 0x80
	}
	for len(af) < 1+afLength {
		af = append(af, 0xFF)
	}
	p := append(header(pid, true, 3, cc), af...)
	return append(p, data...)
}

// clock90 is the 90 kHz clock of a slot plus leadMs, modulo 2^33.
func clock90(slot, leadMs int) uint64 {
	t := int64(wrap90) - wrapSlot*slot90 + int64(slot)*slot90 + int64(leadMs)*90
	if slot >= jumpSlot {
		t += jumpMs * 90
	}
	return uint64(t) % wrap90
}

// videoLeadMs is the DTS lead of the video PES of a slot.
func videoLeadMs(slot int) int { return 700 + 2*(slot/2%5) }

// aacLeadMs is the PTS lead of the AAC PES of a slot.
func aacLeadMs(slot int) int {
	if slot == 51 {
		return -20
	}
	return 650
}

func generate() []byte {
	var out []byte
	var patCC, pmtCC, videoCC, audioCC, aacCC, privateCC byte
	for slot := range slots {
		var p []byte
		switch {
		case slot == 1 || slot == 2 || slot%20 == 19:
			p = psiPacket(0x0000, patCC, patSection())
			patCC++
		case slot == 3 || slot == 4 || slot > 20 && slot%20 == 3:
			p = psiPacket(pmtPid, pmtCC, pmtSection())
			pmtCC++
		case slot >= 6 && slot%2 == 0: // video, every 40 ms
			lead := videoLeadMs(slot)
			data := videoPes(clock90(slot, lead+40), clock90(slot, lead))
			p = pesPacket(videoPid, videoCC, data, clock90(slot, 0)*300, true, slot == jumpSlot)
			videoCC++
		case slot >= 5 && slot%4 == 1: // audio, every 80 ms
			p = pesPacket(audioPid, audioCC, boundedPes(0xC0, clock90(slot, 1500), false), 0, false, false)
			if slot == teiSlot {
				p[1] |= 0x80 // transport_error_indicator
			}
			audioCC++
		case slot >= 11 && slot%8 == 3: // AAC, every 160 ms
			p = pesPacket(aacPid, aacCC, boundedPes(0xC1, clock90(slot, aacLeadMs(slot)), slot == 67), 0, false, false)
			aacCC++
		case slot == 15:
			p = pesPacket(privatePid, privateCC, boundedPes(0xBD, clock90(slot, 300), false), 0, false, false)
			privateCC++
		default:
			p = nullPacket()
		}
		out = append(out, p...)
	}
	return out
}

// m2ts prefixes every 188-byte packet with a TP_extra_header whose
// arrival_time_stamp advances by one slot per packet.
func m2ts(data []byte) []byte {
	out := make([]byte, 0, len(data)/tsPacketSize*(tsPacketSize+4))
	for i := 0; i+tsPacketSize <= len(data); i += tsPacketSize {
		ats := uint32(i/tsPacketSize*slot90*300) & 0x3FFFFFFF
		out = append(out, byte(ats>>24), byte(ats>>16), byte(ats>>8), byte(ats))
		out = append(out, data[i:i+tsPacketSize]...)
	}
	return out
}

// withTrailers follows every 188-byte packet with a 16-byte trailer, making
// 204-byte packets. The trailer of packet n is deterministic filler in
// 0x80..0xBF: never a sync byte, and not Reed-Solomon parity.
func withTrailers(data []byte) []byte {
	out := make([]byte, 0, len(data)/tsPacketSize*(tsPacketSize+16))
	for i := 0; i+tsPacketSize <= len(data); i += tsPacketSize {
		out = append(out, data[i:i+tsPacketSize]...)
		for k := range 16 {
			out = append(out, 0x80|byte((i/tsPacketSize+k)&0x3F))
		}
	}
	return out
}

func main() {
	output := flag.String("output", "", "output transport stream")
	asM2TS := flag.Bool("m2ts", false, "write 192-byte M2TS packets")
	withTrailer := flag.Bool("trailer", false, "write 204-byte packets: each TS packet followed by a 16-byte trailer")
	flag.Parse()
	if *output == "" || *asM2TS && *withTrailer {
		fmt.Fprintln(os.Stderr, "usage: generate_mux_lead -output <out.ts> [-m2ts | -trailer]")
		os.Exit(2)
	}
	data := generate()
	if *asM2TS {
		data = m2ts(data)
	}
	if *withTrailer {
		data = withTrailers(data)
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
