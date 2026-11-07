// Command generate_pes_health writes a short, deterministic transport stream
// with malformed PES headers and an excessive PTS repetition interval. It is
// intentionally invalid: use it to test the PES header and PTS repetition
// checks, never as an example of a compliant stream.
//
// The stream has one program (program_number 1, PMT PID 0x1000) with MPEG-2
// video (stream_type 0x02) on PID 0x0100, which also carries the PCR, MPEG-1
// audio (0x03) on PID 0x0101, PES private data (0x06) on PID 0x0102, MPEG-2
// audio (0x04) on PID 0x0103 and AVC video (0x1B) on PID 0x0104. Every packet
// occupies one 20 ms slot of a constant-rate multiplex (75.2 kbit/s); there
// are 200 slots (4 s). Slot 0 is a null packet, the PAT is sent in slots 1 and
// 2 and the PMT in slots 3 and 4 (the analyzer treats a table as complete at
// its next copy, so the analysis starts at slot 5); the PAT repeats in the
// slots 20k+19 and the PMT in the slots 20k+3 from 23. From slot 6, video PES
// packets take the even slots (every 40 ms, each packet with a PCR), audio
// PES packets the slots 4k+1 from 5 (every 80 ms) and private PES packets the
// slots 8k+3 from 11 that PSI does not use; PID 0x0103 carries one PES packet
// in slot 15 and PID 0x0104 none. The remaining slots are null packets. PCR,
// PTS and DTS follow the slot clock (10 s + slot × 20 ms; video DTS +100 ms
// and PTS +140 ms, audio PTS +200 ms, private PTS +300 ms).
//
// Valid headers: video PES packets have PTS and DTS and PES_packet_length 0
// (unbounded, as video in a transport stream may), audio and private PES
// packets a PTS alone and a PES_packet_length. The video PES of slot 10 is
// split: its PUSI packet carries only the first 12 header bytes and the
// packet in slot 12 the rest.
//
// The defects, by slot:
//
//   - 27 (private): packet_start_code_prefix 0x000002 (bad_start_code).
//   - 35 (private): the PUSI packet carries only the first 8 header bytes
//     and the next private packet (slot 51) starts a new PES (truncated).
//   - 61 (audio): the last marker_bit of the PTS is 0 (bad_marker_bits).
//   - 67 (private): PTS_DTS_flags '01' (bad_flags).
//   - 75 (private): the optional header starts with '01' (bad_flags).
//   - 91 (private): PTS_DTS_flags '10' with PES_header_data_length 3
//     (inconsistent_length).
//   - 107 (private): PES_packet_length 4, shorter than its 8-byte header
//     (inconsistent_length).
//   - 115 (private): the PTS prefix is '0011' although no DTS follows
//     (bad_marker_bits).
//   - 100 to 150 (video): the PES packets carry no PTS or DTS, so the PTS of
//     slot 98 is followed by the PTS of slot 152, 1080 ms later (limit
//     700 ms).
//
// With -clean the same stream is written without the defects. With -m2ts,
// each packet gets a 4-byte TP_extra_header whose arrival_time_stamp advances
// by one 20 ms slot per packet.
package main

import (
	"flag"
	"fmt"
	"os"
)

const (
	tsPacketSize = 188
	slots        = 200
	pmtPid       = 0x1000
	videoPid     = 0x0100
	audioPid     = 0x0101
	privatePid   = 0x0102
	audio2Pid    = 0x0103
	avcPid       = 0x0104
	nullPid      = 0x1FFF
	// slotMs is the duration of one slot (one packet).
	slotMs = 20
	// slotTicks27 is one slot of the 27 MHz system clock.
	slotTicks27 = slotMs * 27_000
	// baseMs is the PCR time of slot 0.
	baseMs = 10_000
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
	}{{0x02, videoPid}, {0x03, audioPid}, {0x06, privatePid}, {0x04, audio2Pid}, {0x1B, avcPid}} {
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
func pesHeader(streamID byte, length int, flags1, flags2 byte, fields []byte) []byte {
	h := []byte{0x00, 0x00, 0x01, streamID, byte(length >> 8), byte(length), flags1, flags2, byte(len(fields))}
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

// boundedPes returns a PES packet with a PTS alone and a PES_packet_length that
// covers it.
func boundedPes(streamID byte, pts uint64) []byte {
	fields := timestampBytes(0x2, pts)
	return append(pesHeader(streamID, 3+len(fields)+esPayload, 0x80, 0x80, fields), esBytes()...)
}

// pesPacket is one TS packet with payload_unit_start_indicator holding data,
// padded with adaptation-field stuffing. pcr is written when hasPcr is set.
func pesPacket(pid uint16, pusi bool, cc byte, data []byte, pcr uint64, hasPcr bool) []byte {
	afLength := tsPacketSize - 4 - 1 - len(data)
	af := []byte{byte(afLength), 0x00}
	if hasPcr {
		af[1] = 0x10 // PCR_flag
		af = append(af, pcrBytes(pcr)...)
	}
	for len(af) < 1+afLength {
		af = append(af, 0xFF)
	}
	p := append(header(pid, pusi, 3, cc), af...)
	return append(p, data...)
}

// slotPcr is the 27 MHz PCR value of a slot's clock.
func slotPcr(slot int) uint64 { return uint64(baseMs*27_000) + uint64(slot)*slotTicks27 }

// slotTs is the 90 kHz value of a slot's clock plus offsetMs.
func slotTs(slot, offsetMs int) uint64 { return uint64(baseMs+slot*slotMs+offsetMs) * 90 }

// videoPes returns the video PES of a slot: PTS and DTS unless untimed.
func videoPes(slot int, untimed bool) []byte {
	if untimed {
		return append(pesHeader(0xE0, 0, 0x80, 0x00, nil), esBytes()...)
	}
	fields := append(timestampBytes(0x3, slotTs(slot, 140)), timestampBytes(0x1, slotTs(slot, 100))...)
	return append(pesHeader(0xE0, 0, 0x80, 0xC0, fields), esBytes()...)
}

// privatePes returns the private-data PES of a slot, with its defect unless
// clean is set.
func privatePes(slot int, clean bool) []byte {
	pts := slotTs(slot, 300)
	if clean {
		return boundedPes(0xBD, pts)
	}
	ptsField := timestampBytes(0x2, pts)
	length := 3 + len(ptsField) + esPayload
	switch slot {
	case 27:
		p := boundedPes(0xBD, pts)
		p[2] = 0x02
		return p
	case 35:
		return boundedPes(0xBD, pts)[:8]
	case 67:
		return append(pesHeader(0xBD, length, 0x80, 0x40, ptsField), esBytes()...)
	case 75:
		return append(pesHeader(0xBD, length, 0x40, 0x80, ptsField), esBytes()...)
	case 91:
		return append(pesHeader(0xBD, length, 0x80, 0x80, ptsField[:3]), esBytes()...)
	case 107:
		return append(pesHeader(0xBD, 4, 0x80, 0x80, ptsField), esBytes()...)
	case 115:
		return append(pesHeader(0xBD, length, 0x80, 0x80, timestampBytes(0x3, pts)), esBytes()...)
	}
	return boundedPes(0xBD, pts)
}

// generate returns the 188-byte stream, with the defects unless clean is set.
func generate(clean bool) []byte {
	var out []byte
	var patCC, pmtCC, videoCC, audioCC, privateCC, audio2CC byte
	// splitRest holds the second part of the video PES of slot 10.
	var splitRest []byte
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
			data, pusi := videoPes(slot, !clean && slot >= 100 && slot <= 150), true
			switch slot {
			case 10:
				data, splitRest = data[:12], data[12:]
			case 12:
				data, pusi = splitRest, false
			}
			p = pesPacket(videoPid, pusi, videoCC, data, slotPcr(slot), true)
			videoCC++
		case slot >= 5 && slot%4 == 1: // audio, every 80 ms
			data := boundedPes(0xC0, slotTs(slot, 200))
			if !clean && slot == 61 {
				data[13] &^= 0x01 // last PTS marker_bit
			}
			p = pesPacket(audioPid, true, audioCC, data, 0, false)
			audioCC++
		case slot >= 11 && slot%8 == 3: // private data, every 160 ms
			p = pesPacket(privatePid, true, privateCC, privatePes(slot, clean), 0, false)
			privateCC++
		case slot == 15:
			p = pesPacket(audio2Pid, true, audio2CC, boundedPes(0xC1, slotTs(slot, 200)), 0, false)
			audio2CC++
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
		ats := uint32(i/tsPacketSize*slotTicks27) & 0x3FFFFFFF
		out = append(out, byte(ats>>24), byte(ats>>16), byte(ats>>8), byte(ats))
		out = append(out, data[i:i+tsPacketSize]...)
	}
	return out
}

func main() {
	output := flag.String("output", "", "output transport stream")
	asM2TS := flag.Bool("m2ts", false, "write 192-byte M2TS packets")
	clean := flag.Bool("clean", false, "write the stream without the defects")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "usage: generate_pes_health -output <out.ts> [-m2ts] [-clean]")
		os.Exit(2)
	}
	data := generate(*clean)
	if *asM2TS {
		data = m2ts(data)
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
