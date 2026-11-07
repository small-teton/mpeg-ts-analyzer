// Command generate_transport_errors writes a short, deterministic transport
// stream with packets marked by transport_error_indicator (TEI) and packets
// with a non-zero transport_scrambling_control on the PAT, PMT, PCR,
// elementary-stream, unreferenced and null PIDs. It is intentionally invalid:
// use it to test how the analyzer counts these packets and keeps their data
// out of the analysis, never as an example of a compliant stream.
//
// The stream has one program (program_number 1, PMT PID 0x1000) with MPEG-2
// video on PID 0x0100, which also carries the PCR, and MPEG-1 audio on PID
// 0x0101. Every packet occupies one 20 ms slot of a constant-rate multiplex
// (75.2 kbit/s); there are 100 slots (2 s). Slot 0 is a null packet, the PAT
// is sent in slots 1 and 2 and the PMT in slots 3 and 4: the analyzer treats a
// table as complete at its next copy, so slots 0 to 4 are read while the PAT
// and PMT are acquired and the analysis starts at slot 5. The PAT is repeated
// in slots 19, 39, 59, 79 and 99 and the PMT in slots 23, 43, 63 and 83. Video
// PES packets take the even slots from 6 (every 40 ms, each with a PCR), audio
// PES packets the slots 4k+1 from 5 (every 80 ms), and the remaining slots are
// null packets. Each PES packet fits in one TS packet. PCR and PTS follow the
// slot clock (10 s + slot × 20 ms; video PTS +100 ms, audio PTS +200 ms).
//
// The affected packets, by slot (TEI: transport_error_indicator 1; scrambled:
// transport_scrambling_control 2 or 3, payload bytes XORed with 0x5A):
//
//   - 0: null packet with TEI, read while acquiring the PAT.
//   - 7: a scrambled packet on PID 0x0200, which no PMT references.
//   - 25: audio with TEI and a PTS 1 s too early.
//   - 30: video with TEI, a PTS 1 s too early and a PCR 1 s too late.
//   - 39: PAT with TEI announcing PMT PID 0x1001 in version 1 (valid CRC).
//   - 43: PMT with TEI announcing stream_type 0x1B for PID 0x0100 in version
//     1 (valid CRC).
//   - 45: scrambled audio (control 3).
//   - 50: scrambled video (control 2) with a correct PCR in its clear
//     adaptation field.
//   - 59: scrambled PAT; 63: scrambled PMT.
//   - 70: video with TEI and scrambled (control 2).
//
// Every affected packet keeps the continuity_counter sequence of its PID. If
// the analyzer used their payload or the PCR of a TEI packet, it would report
// PSI transitions, PTS/DTS anomalies, PES parse warnings or a PCR jump. With
// -clean the same stream is written without the affected packets: each is a
// normal packet of its slot (slot 0 a null packet, slot 7 a null packet).
//
// With -malformed, every TEI packet is also damaged where a parser that read
// past the fixed 4-byte header would fail, while its PID, continuity counter
// sequence and payload presence stay as in the default stream:
//
//   - 0 (null): adaptation_field_control 0 (reserved).
//   - 25 (audio): adaptation_field_length 200, past the end of the packet.
//   - 30 (video): transport_private_data_flag set, with a
//     transport_private_data_length (200) past the adaptation field.
//   - 39 (PAT): adaptation_field_control 0, with the PAT's previous
//     continuity_counter, since a packet with control 0 does not increment it.
//   - 43 (PMT): adaptation_field_control 3 with adaptation_field_length 255.
//   - 70 (video, also scrambled): OPCR_flag and transport_private_data_flag
//     set, with a transport_private_data_length (200) past the adaptation
//     field.
//
// The analysis of this stream must equal that of the default stream: every
// TEI packet is counted with the PID of its header and nothing past that
// header is used. -malformed and -clean cannot be combined.
//
// With -m2ts, each packet gets a 4-byte TP_extra_header whose
// arrival_time_stamp advances by one 20 ms slot per packet.
package main

import (
	"flag"
	"fmt"
	"os"
)

const (
	tsPacketSize = 188
	slots        = 100
	pmtPid       = 0x1000
	videoPid     = 0x0100
	audioPid     = 0x0101
	strayPid     = 0x0200
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

func patSection(version byte, pmt uint16) []byte {
	return section(0x00, []byte{
		0x00, 0x01, // transport_stream_id 1
		0xC1 | version<<1, // version, current_next_indicator 1
		0x00, 0x00,        // section_number, last_section_number
		0x00, 0x01, 0xE0 | byte(pmt>>8), byte(pmt), // program 1 -> PMT PID
	})
}

func pmtSection(version, videoType byte) []byte {
	return section(0x02, []byte{
		0x00, 0x01, // program_number 1
		0xC1 | version<<1,
		0x00, 0x00,
		0xE0 | videoPid>>8, videoPid & 0xFF, // PCR_PID
		0xF0, 0x00, // program_info_length 0
		videoType, 0xE0 | videoPid>>8, videoPid & 0xFF, 0xF0, 0x00, // video
		0x03, 0xE0 | audioPid>>8, audioPid & 0xFF, 0xF0, 0x00, // MPEG-1 audio
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

func payloadPacket(pid uint16, cc byte) []byte {
	p := header(pid, false, 1, cc)
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

// pesPacket is one TS packet holding a whole PES packet with a PTS, padded
// with adaptation-field stuffing. pcr is written when hasPcr is set.
func pesPacket(pid uint16, cc byte, streamID byte, pts uint64, pcr uint64, hasPcr bool) []byte {
	pes := []byte{0x00, 0x00, 0x01, streamID}
	pesLength := 3 + 5 + esPayload
	pes = append(pes, byte(pesLength>>8), byte(pesLength), 0x80, 0x80, 0x05)
	pes = append(pes, ptsBytes(pts)...)
	for i := range esPayload {
		pes = append(pes, byte(0xA0+i))
	}
	afLength := tsPacketSize - 4 - 1 - len(pes)
	af := []byte{byte(afLength), 0x00}
	if hasPcr {
		af[1] = 0x10 // PCR_flag
		af = append(af, pcrBytes(pcr)...)
	}
	for len(af) < 1+afLength {
		af = append(af, 0xFF)
	}
	p := append(header(pid, true, 3, cc), af...)
	return append(p, pes...)
}

// slotPcr is the 27 MHz PCR value of a slot's clock.
func slotPcr(slot int) uint64 { return uint64(baseMs*27_000) + uint64(slot)*slotTicks27 }

// slotPts is the 90 kHz value of a slot's clock plus offsetMs.
func slotPts(slot, offsetMs int) uint64 { return uint64(baseMs+slot*slotMs+offsetMs) * 90 }

// markError sets transport_error_indicator.
func markError(p []byte) []byte {
	p[1] |= 0x80
	return p
}

// scramble sets transport_scrambling_control and XORs the payload, which
// starts after the adaptation field when there is one.
func scramble(p []byte, control byte) []byte {
	p[3] = p[3]&0x3F | control<<6
	start := 4
	if p[3]&0x20 != 0 {
		start += 1 + int(p[4])
	}
	for i := start; i < len(p); i++ {
		p[i] ^= 0x5A
	}
	return p
}

// malformPrivateData sets transport_private_data_flag and the extra flags in
// the adaptation field of p, and writes a transport_private_data_length of
// 200, which runs past the adaptation field, after the PCR and OPCR. The
// adaptation field of p must hold a PCR.
func malformPrivateData(p []byte, flags byte) []byte {
	p[5] |= 0x02 | flags
	at := 6 + 6 // after the PCR
	if p[5]&0x08 != 0 {
		at += 6 // OPCR
	}
	p[at] = 200
	return p
}

// malform damages the TEI packet of slot as listed in the package comment.
func malform(slot int, p []byte) []byte {
	switch slot {
	case 0, 39:
		p[3] &^= 0x30 // adaptation_field_control 0
	case 25:
		p[4] = 200
	case 30:
		p = malformPrivateData(p, 0)
	case 43:
		p[3] |= 0x30
		p[4] = 255
	case 70:
		p = malformPrivateData(p, 0x08) // OPCR_flag
	}
	return p
}

// generate returns the 188-byte stream, with the affected packets unless
// clean is set; with malformed, the TEI packets are also malformed.
func generate(clean, malformed bool) []byte {
	affected := func(slot int) bool {
		if clean {
			return false
		}
		switch slot {
		case 0, 7, 25, 30, 39, 43, 45, 50, 59, 63, 70:
			return true
		}
		return false
	}
	var out []byte
	var patCC, pmtCC, videoCC, audioCC byte
	for slot := range slots {
		var p []byte
		switch {
		case slot == 1 || slot == 2 || slot%20 == 19:
			s := patSection(0, pmtPid)
			if affected(slot) && slot == 39 {
				s = patSection(1, 0x1001)
			}
			if malformed && slot == 39 {
				// adaptation_field_control 0 does not increment the counter.
				p = psiPacket(0x0000, patCC-1, s)
				break
			}
			p = psiPacket(0x0000, patCC, s)
			patCC++
		case slot == 3 || slot == 4 || slot > 20 && slot%20 == 3:
			s := pmtSection(0, 0x02)
			if affected(slot) && slot == 43 {
				s = pmtSection(1, 0x1B)
			}
			p = psiPacket(pmtPid, pmtCC, s)
			pmtCC++
		case slot >= 6 && slot%2 == 0: // video, every 40 ms
			pts, pcr := slotPts(slot, 100), slotPcr(slot)
			if affected(slot) && slot == 30 {
				pts, pcr = slotPts(slot, 100-1000), pcr+27_000_000
			}
			p = pesPacket(videoPid, videoCC, 0xE0, pts, pcr, true)
			videoCC++
		case slot >= 5 && slot%4 == 1: // audio, every 80 ms
			pts := slotPts(slot, 200)
			if affected(slot) && slot == 25 {
				pts = slotPts(slot, 200-1000)
			}
			p = pesPacket(audioPid, audioCC, 0xC0, pts, 0, false)
			audioCC++
		case affected(slot) && slot == 7:
			p = payloadPacket(strayPid, 0)
		default:
			p = payloadPacket(nullPid, 0)
		}
		if affected(slot) {
			switch slot {
			case 0, 25, 30, 39, 43:
				p = markError(p)
			case 45:
				p = scramble(p, 3)
			case 70:
				p = scramble(markError(p), 2)
			default:
				p = scramble(p, 2)
			}
			if malformed && p[1]&0x80 != 0 {
				p = malform(slot, p)
			}
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
	clean := flag.Bool("clean", false, "write the stream without the affected packets")
	malformed := flag.Bool("malformed", false, "also damage the header or adaptation field of each TEI packet")
	flag.Parse()
	if *output == "" || *clean && *malformed {
		fmt.Fprintln(os.Stderr, "usage: generate_transport_errors -output <out.ts> [-m2ts] [-clean | -malformed]")
		os.Exit(2)
	}
	data := generate(*clean, *malformed)
	if *asM2TS {
		data = m2ts(data)
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
