// Command generate_packet_gaps writes a deterministic, poorly interleaved
// transport stream: a bursty remux whose audio arrives in blocks, with a PCR
// discontinuity inside the longest audio gap and a PMT change that removes a
// stream and a later one that re-adds it. Use it to test the analyzer's
// per-PID maximum packet gaps (a report-only diagnostic); it is not an
// example of good multiplexing.
//
// The stream is 4 s of a constant-rate multiplex: 100 frames of 40 ms, each
// of eight 5 ms packet slots (300.8 kbit/s). In every frame f:
//
//   - slot 0 is a PCR-only packet (adaptation field only) on the PCR_PID
//     0x01FF, so PCRs are 40 ms apart;
//   - slots 1 to 3 are video packets on PID 0x0100, each a whole PES packet
//     with a PTS;
//   - slot 4 carries PSI/SI: the PAT (program 1 on PMT PID 0x1000) when
//     f%4 == 0, the PMT when f%4 == 2, an SDT packet on 0x0011 when
//     f%8 == 1, and the only packet of PID 0x0012 in frame 43; otherwise a
//     null packet;
//   - slots 5 to 7 carry the bursts: three audio packets on PID 0x0101 in
//     the frames f%8 == 0, except frame 56, which is skipped, and a private
//     data packet on PID 0x0102 in slot 5 of the frames f%8 == 4 while the
//     PMT references it; otherwise null packets.
//
// Packet times follow the slot clock: slot s of frame f is at
// 10 s + f × 40 ms + s × 5 ms, plus 10 s from frame 50 on, where the PCR jumps
// with discontinuity_indicator set (a declared discontinuity). The PTS of a
// packet is its time plus 100 ms. The gap of 125 packets between the audio
// bursts of frames 48 and 64 therefore crosses the discontinuity; every other
// audio gap is 61 packets.
//
// PMT version 0 (frames 0 to 29) references the video 0x0100 (MPEG-2 video),
// the audio 0x0101 (MPEG-1 audio) and the private data 0x0102 (PES private
// data); version 1 (from frame 30) removes 0x0102 and version 2 (from frame
// 70) adds it again. Its packets are in frames 4, 12, 20, 28 and 76, 84, 92:
// 63 packets apart within each stretch, 383 across its absence.
//
// The analysis acquires the stream in the middle: frame 0 starts with the
// PCR and video packets before the first PAT.
//
// With -m2ts, each packet gets a 4-byte TP_extra_header whose
// arrival_time_stamp advances by one 5 ms slot per packet.
package main

import (
	"flag"
	"fmt"
	"os"
)

const (
	tsPacketSize   = 188
	frames         = 100
	slots          = 8
	pmtPid         = 0x1000
	pcrPid         = 0x01FF
	videoPid       = 0x0100
	audioPid       = 0x0101
	privatePid     = 0x0102
	sdtPid         = 0x0011
	eitPid         = 0x0012
	nullPid        = 0x1FFF
	frameMs        = 40
	slotMs         = frameMs / slots
	baseMs         = 10_000
	jumpMs         = 10_000
	jumpFrame      = 50 // first frame after the PCR discontinuity
	skippedBurst   = 56 // audio burst left out
	eitFrame       = 43
	pmtV1Frame     = 30 // removes the private data stream
	pmtV2Frame     = 70 // adds it again
	esPayload      = 16
	burstEvery     = 8
	privateInBurst = 4
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
		0x00, 0x01, 0xE0 | pmtPid>>8, pmtPid & 0xFF, // program 1
	})
}

func esEntry(streamType byte, pid uint16) []byte {
	return []byte{streamType, 0xE0 | byte(pid>>8), byte(pid), 0xF0, 0x00}
}

// privateReferenced reports whether the PMT sent in frame f references the
// private data stream.
func privateReferenced(f int) bool { return f < pmtV1Frame || f >= pmtV2Frame }

func pmtVersion(f int) byte {
	switch {
	case f >= pmtV2Frame:
		return 2
	case f >= pmtV1Frame:
		return 1
	default:
		return 0
	}
}

func pmtSection(f int) []byte {
	body := []byte{
		0x00, 0x01, // program_number 1
		0xC1 | pmtVersion(f)<<1,
		0x00, 0x00,
		0xE0 | pcrPid>>8, pcrPid & 0xFF, // PCR_PID
		0xF0, 0x00, // program_info_length 0
	}
	body = append(body, esEntry(0x02, videoPid)...)
	body = append(body, esEntry(0x03, audioPid)...)
	if privateReferenced(f) {
		body = append(body, esEntry(0x06, privatePid)...)
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

// pcrPacket is an adaptation-field-only packet with a PCR; discontinuity
// sets discontinuity_indicator. The continuity counter does not advance on
// a packet without payload.
func pcrPacket(cc byte, pcr uint64, discontinuity bool) []byte {
	af := []byte{tsPacketSize - 5, 0x10} // PCR_flag
	if discontinuity {
		af[1] |= 0x80
	}
	af = append(af, pcrBytes(pcr)...)
	for len(af) < tsPacketSize-4 {
		af = append(af, 0xFF)
	}
	return append(header(pcrPid, false, 2, cc), af...)
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
// with adaptation-field stuffing.
func pesPacket(pid uint16, cc byte, streamID byte, pts uint64) []byte {
	pes := []byte{0x00, 0x00, 0x01, streamID}
	pesLength := 3 + 5 + esPayload
	pes = append(pes, byte(pesLength>>8), byte(pesLength), 0x80, 0x80, 0x05)
	pes = append(pes, ptsBytes(pts)...)
	for i := range esPayload {
		pes = append(pes, byte(0xA0+i))
	}
	afLength := tsPacketSize - 4 - 1 - len(pes)
	af := []byte{byte(afLength), 0x00}
	for len(af) < 1+afLength {
		af = append(af, 0xFF)
	}
	p := append(header(pid, true, 3, cc), af...)
	return append(p, pes...)
}

// slotMsAt is the time of a slot on the PCR clock, in milliseconds.
func slotMsAt(frame, slot int) uint64 {
	t := uint64(baseMs + frame*frameMs + slot*slotMs)
	if frame >= jumpFrame {
		t += jumpMs
	}
	return t
}

// generate returns the 188-byte stream.
func generate() []byte {
	cc := make(map[uint16]byte)
	next := func(pid uint16) byte {
		c := cc[pid]
		cc[pid] = c + 1
		return c
	}
	pes := func(pid uint16, streamID byte, frame, slot int) []byte {
		return pesPacket(pid, next(pid), streamID, (slotMsAt(frame, slot)+100)*90)
	}
	var out []byte
	for f := range frames {
		out = append(out, pcrPacket(0, slotMsAt(f, 0)*27_000, f == jumpFrame)...)
		for slot := 1; slot <= 3; slot++ {
			out = append(out, pes(videoPid, 0xE0, f, slot)...)
		}
		var p []byte
		switch {
		case f%4 == 0:
			p = psiPacket(0x0000, next(0x0000), patSection())
		case f%4 == 2:
			p = psiPacket(pmtPid, next(pmtPid), pmtSection(f))
		case f%8 == 1:
			p = payloadPacket(sdtPid, next(sdtPid))
		case f == eitFrame:
			p = payloadPacket(eitPid, next(eitPid))
		default:
			p = payloadPacket(nullPid, 0)
		}
		out = append(out, p...)
		for slot := 5; slot < slots; slot++ {
			switch {
			case f%burstEvery == 0 && f != skippedBurst:
				p = pes(audioPid, 0xC0, f, slot)
			case f%burstEvery == privateInBurst && slot == 5 && privateReferenced(f):
				p = pes(privatePid, 0xBD, f, slot)
			default:
				p = payloadPacket(nullPid, 0)
			}
			out = append(out, p...)
		}
	}
	return out
}

// m2ts prefixes every 188-byte packet with a TP_extra_header whose
// arrival_time_stamp advances by one slot per packet.
func m2ts(data []byte) []byte {
	out := make([]byte, 0, len(data)/tsPacketSize*(tsPacketSize+4))
	for i := 0; i+tsPacketSize <= len(data); i += tsPacketSize {
		ats := uint32(i/tsPacketSize*slotMs*27_000) & 0x3FFFFFFF
		out = append(out, byte(ats>>24), byte(ats>>16), byte(ats>>8), byte(ats))
		out = append(out, data[i:i+tsPacketSize]...)
	}
	return out
}

func main() {
	output := flag.String("output", "", "output transport stream")
	asM2TS := flag.Bool("m2ts", false, "write 192-byte M2TS packets")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "usage: generate_packet_gaps -output <out.ts> [-m2ts]")
		os.Exit(2)
	}
	data := generate()
	if *asM2TS {
		data = m2ts(data)
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
