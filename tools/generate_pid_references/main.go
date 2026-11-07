// Command generate_pid_references writes a deterministic transport stream
// whose PSI layout references PIDs that are absent, silent or intermittent,
// and which carries packets on PIDs that no layout references. It is
// intentionally invalid: use it to test the analyzer's PID reference health
// check, never as an example of a compliant stream.
//
// The stream is 16 s of a constant-rate multiplex: 160 frames of 100 ms, each
// of four 25 ms packet slots (60.16 kbit/s). In every frame, slot 0 is a video
// packet on PID 0x0100 with a PCR and a PTS (so PCRs are 100 ms apart), slot
// 1 an audio packet on PID 0x0101, slot 2 a PSI/SI or other packet and slot 3
// a packet of another stream. Packet times follow the slot clock: the PCR of
// frame f is 10 s + f × 100 ms.
//
// The analysis acquires the stream in the middle: frame 0 starts with video
// and audio packets, the PAT (version 0: program 1 on PMT PID 0x1000 and the
// network_PID 0x0020) is in slot 2 of every frame f with f%4 == 0, and the
// PMT in slot 2 of every frame with f%4 == 2. PMT version 0 has PCR_PID
// 0x0100, a CA_descriptor naming CA_PID 0x0400 and the streams:
//
//   - 0x0100 MPEG-2 video (0x02): every frame;
//   - 0x0101 MPEG-1 audio (0x03): every frame except frames 60 to 119, a
//     silence of more than 5 s;
//   - 0x0102 private PES data (0x06): never sent; it may be idle, so it is not
//     judged;
//   - 0x0103 MPEG-2 audio (0x04): never sent while referenced, which is
//     longer than 5 s;
//   - 0x0106 MPEG-1 audio (0x03): slot 3 of the even frames up to frame 146.
//
// PMT version 1, sent from frame 142, removes 0x0103 and 0x0106 and adds
// 0x0105 MPEG-1 audio (0x03), which is sent in slot 3 of the odd frames from
// frame 139: its first packets precede the PMT announcing it, and 0x0106
// continues for a short time after the PMT removing it, both within the
// 500 ms grace period.
//
// The other packets of slot 2 (frames f%4 == 1 and 3):
//
//   - 0x0011 (SDT PID) in frames f%12 == 1, 0x0020 (the network_PID) in frames
//     f%12 == 5 and 0x0400 (the CA_PID) in frames f%12 == 9: referenced or
//     reserved, so never unreferenced;
//   - 0x0300, which no layout references, in frames 31 to 47 (f%4 == 3);
//   - 0x0500, which no layout references, in frames 155 and 159: it appears
//     less than 500 ms before the end, so the range is too short to judge it.
//
// Slot 3 of frame 61 is a packet on PID 0x0777 with transport_error_indicator
// set, which must not count as an unreferenced PID. Every other free slot is a
// null packet.
//
// With -m2ts, each packet gets a 4-byte TP_extra_header whose
// arrival_time_stamp advances by one 25 ms slot per packet.
package main

import (
	"flag"
	"fmt"
	"os"
)

const (
	tsPacketSize = 188
	frames       = 160
	pmtPid       = 0x1000
	networkPid   = 0x0020
	sdtPid       = 0x0011
	caPid        = 0x0400
	videoPid     = 0x0100
	audioPid     = 0x0101
	privatePid   = 0x0102
	absentPid    = 0x0103
	addedPid     = 0x0105
	removedPid   = 0x0106
	strayPid     = 0x0300
	latePid      = 0x0500
	damagedPid   = 0x0777
	nullPid      = 0x1FFF
	frameMs      = 100
	slotMs       = frameMs / 4
	baseMs       = 10_000
	esPayload    = 16
	pmtV1Frame   = 142 // first frame with PMT version 1
	silentFirst  = 60  // first frame without audio
	silentLast   = 119 // last frame without audio
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
		0x00, 0x00, 0xE0 | networkPid>>8, networkPid & 0xFF, // network_PID
		0x00, 0x01, 0xE0 | pmtPid>>8, pmtPid & 0xFF, // program 1
	})
}

func esEntry(streamType byte, pid uint16) []byte {
	return []byte{streamType, 0xE0 | byte(pid>>8), byte(pid), 0xF0, 0x00}
}

func pmtSection(version byte) []byte {
	body := []byte{
		0x00, 0x01, // program_number 1
		0xC1 | version<<1,
		0x00, 0x00,
		0xE0 | videoPid>>8, videoPid & 0xFF, // PCR_PID
		// program_info_length 6: CA_descriptor, CA_system_ID 0x0B00
		0xF0, 0x06, 0x09, 0x04, 0x0B, 0x00, 0xE0 | caPid>>8, caPid & 0xFF,
	}
	body = append(body, esEntry(0x02, videoPid)...)
	body = append(body, esEntry(0x03, audioPid)...)
	body = append(body, esEntry(0x06, privatePid)...)
	if version == 0 {
		body = append(body, esEntry(0x04, absentPid)...)
		body = append(body, esEntry(0x03, removedPid)...)
	} else {
		body = append(body, esEntry(0x03, addedPid)...)
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

// slotMsAt is the time of a slot on the PCR clock, in milliseconds.
func slotMsAt(frame, slot int) uint64 { return uint64(baseMs + frame*frameMs + slot*slotMs) }

// generate returns the 188-byte stream.
func generate() []byte {
	cc := make(map[uint16]byte)
	next := func(pid uint16) byte {
		c := cc[pid]
		cc[pid] = c + 1
		return c
	}
	pes := func(pid uint16, streamID byte, frame, slot int) []byte {
		return pesPacket(pid, next(pid), streamID, (slotMsAt(frame, slot)+200)*90, 0, false)
	}
	var out []byte
	for f := range frames {
		// Slot 0: video with PCR.
		t := slotMsAt(f, 0)
		out = append(out, pesPacket(videoPid, next(videoPid), 0xE0, (t+100)*90, t*27_000, true)...)
		// Slot 1: audio, silent for frames 60 to 119.
		if f < silentFirst || f > silentLast {
			out = append(out, pes(audioPid, 0xC0, f, 1)...)
		} else {
			out = append(out, payloadPacket(nullPid, 0)...)
		}
		// Slot 2: PSI/SI and other PIDs.
		var p []byte
		switch {
		case f%4 == 0:
			p = psiPacket(0x0000, next(0x0000), patSection())
		case f%4 == 2:
			version := byte(0)
			if f >= pmtV1Frame {
				version = 1
			}
			p = psiPacket(pmtPid, next(pmtPid), pmtSection(version))
		case f%12 == 1:
			p = payloadPacket(sdtPid, next(sdtPid))
		case f%12 == 5:
			p = payloadPacket(networkPid, next(networkPid))
		case f%12 == 9:
			p = payloadPacket(caPid, next(caPid))
		case f >= 31 && f <= 47:
			p = payloadPacket(strayPid, next(strayPid))
		case f >= 155:
			p = payloadPacket(latePid, next(latePid))
		default:
			p = payloadPacket(nullPid, 0)
		}
		out = append(out, p...)
		// Slot 3: the streams added and removed by PMT version 1.
		switch {
		case f%2 == 0 && f <= 146:
			p = pes(removedPid, 0xC1, f, 3)
		case f%2 == 1 && f >= 139:
			p = pes(addedPid, 0xC2, f, 3)
		case f == 61:
			p = payloadPacket(damagedPid, 0)
			p[1] |= 0x80 // transport_error_indicator
		default:
			p = payloadPacket(nullPid, 0)
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
		fmt.Fprintln(os.Stderr, "usage: generate_pid_references -output <out.ts> [-m2ts]")
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
