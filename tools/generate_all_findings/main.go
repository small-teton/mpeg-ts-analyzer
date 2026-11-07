// Command generate_all_findings writes a short, deterministic transport stream
// in which every Overview compliance card of the range analysis reports a
// non-OK result. It is intentionally invalid: use it to demonstrate and test
// the verdicts, never as an example of a compliant stream.
//
// The stream has one program (program_number 1, PMT PID 0x1000) with MPEG-2
// video on PID 0x0100, which also carries the PCR, and MPEG-1 audio on PID
// 0x0101. Every packet occupies one 10 ms slot of a constant-rate multiplex
// (150.4 kbit/s). The PAT is sent in slots 0, 1 and 40 and the PMT in slots
// 2, 3 and 41: the analyzer treats a table as complete at the next copy, so
// the analysis starts at slot 4, and the repetition keeps PSI intervals below
// 500 ms. Video PES packets start every 40 ms from slot 6, audio PES packets
// every 80 ms from slot 4, and the remaining slots are null packets. Each PES packet fits in one TS packet;
// the elementary-stream payload is filler. PCR and PTS values follow the slot
// clock (10 s + slot × 10 ms), so byte position is an exact time proxy.
//
// Five deliberate defects, each of which can be left out with -omit:
//
//   - pcr-interval: the video packets in slots 26, 30 and 34 carry no PCR, so
//     the PCRs in slots 22 and 38 are 160 ms apart (limit 100 ms).
//   - pcr-pts-gap: every audio PTS is 1200 ms ahead of the PCR time of its
//     packet (limit 1000 ms) instead of 200 ms.
//   - pts-backward: the video PES in slot 46 has a PTS 40 ms before the
//     previous video PTS instead of 40 ms after it.
//   - cc-gap: the audio packet in slot 44 skips continuity_counter 5 (it
//     carries 6); later audio packets continue from 6, so the error does not
//     cascade.
//   - pcr-jitter: the PCR in slot 58 is 100 µs (2700 ticks of 27 MHz) late
//     relative to its byte position (DVB tolerance ±25 µs).
//
// With -m2ts, each packet gets a 4-byte TP_extra_header whose
// arrival_time_stamp advances by one 10 ms slot per packet, matching the
// intended constant delivery.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

const (
	tsPacketSize = 188
	slots        = 80
	pmtPid       = 0x1000
	videoPid     = 0x0100
	audioPid     = 0x0101
	nullPid      = 0x1FFF
	// slotTicks27 is one 10 ms slot of the 27 MHz system clock.
	slotTicks27 = 270_000
	// baseMs is the PCR time of slot 0.
	baseMs = 10_000
	// esPayload is the filler elementary-stream payload of each PES packet.
	esPayload = 16
)

// Defect names accepted by -omit.
const (
	defectPcrInterval = "pcr-interval"
	defectPcrPtsGap   = "pcr-pts-gap"
	defectPtsBackward = "pts-backward"
	defectCcGap       = "cc-gap"
	defectPcrJitter   = "pcr-jitter"
)

var defects = []string{defectPcrInterval, defectPcrPtsGap, defectPtsBackward, defectCcGap, defectPcrJitter}

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
func slotPts(slot, offsetMs int) uint64 { return uint64(baseMs+slot*10+offsetMs) * 90 }

// generate returns the 188-byte stream without the defects named in omit.
func generate(omit map[string]bool) []byte {
	with := func(defect string) bool { return !omit[defect] }
	var out []byte
	var patCC, pmtCC, videoCC, audioCC byte
	for slot := range slots {
		var p []byte
		switch {
		case slot == 0 || slot == 1 || slot == 40:
			p = psiPacket(0x0000, patCC, patSection())
			patCC++
		case slot == 2 || slot == 3 || slot == 41:
			p = psiPacket(pmtPid, pmtCC, pmtSection())
			pmtCC++
		case slot%4 == 2: // video, every 40 ms
			hasPcr := !with(defectPcrInterval) || (slot != 26 && slot != 30 && slot != 34)
			pcr := slotPcr(slot)
			if with(defectPcrJitter) && slot == 58 {
				pcr += 2700 // 100 µs late
			}
			pts := slotPts(slot, 100)
			if with(defectPtsBackward) && slot == 46 {
				pts = slotPts(slot-4, 100-40)
			}
			p = pesPacket(videoPid, videoCC, 0xE0, pts, pcr, hasPcr)
			videoCC++
		case slot%8 == 4: // audio, every 80 ms
			if with(defectCcGap) && slot == 44 {
				audioCC++ // skip one continuity_counter value
			}
			offsetMs := 200
			if with(defectPcrPtsGap) {
				offsetMs = 1200
			}
			p = pesPacket(audioPid, audioCC, 0xC0, slotPts(slot, offsetMs), 0, false)
			audioCC++
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

func parseOmit(value string) (map[string]bool, error) {
	omit := map[string]bool{}
	if value == "" {
		return omit, nil
	}
	for _, name := range strings.Split(value, ",") {
		if name == "all" {
			for _, d := range defects {
				omit[d] = true
			}
			continue
		}
		known := false
		for _, d := range defects {
			known = known || d == name
		}
		if !known {
			return nil, fmt.Errorf("unknown defect %q (want all or %s)", name, strings.Join(defects, ", "))
		}
		omit[name] = true
	}
	return omit, nil
}

func main() {
	output := flag.String("output", "", "output transport stream")
	asM2TS := flag.Bool("m2ts", false, "write 192-byte M2TS packets")
	omitFlag := flag.String("omit", "", "comma-separated defects to leave out ("+strings.Join(defects, ", ")+", or all)")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "usage: generate_all_findings -output <out.ts> [-m2ts] [-omit defect,...]")
		os.Exit(2)
	}
	omit, err := parseOmit(*omitFlag)
	if err == nil {
		data := generate(omit)
		if *asM2TS {
			data = m2ts(data)
		}
		err = os.WriteFile(*output, data, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
