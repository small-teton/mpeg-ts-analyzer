// Command generate_psi_health writes short, deterministic transport streams
// for the PAT/PMT health check (psi_health). Every scenario except healthy is
// intentionally invalid: use them to test how the analyzer reports the
// availability and repetition of the PAT and the PMTs, never as examples of
// compliant streams.
//
// The base stream has two programs. Program 1 (PMT PID 0x1000) has MPEG-2
// video on PID 0x0100, which carries the PCR. Program 2 (PMT PID 0x1010)
// uses the same PCR_PID and video and adds MPEG-1 audio on PID 0x0101, which
// is never sent; its PMT has two sections. Every packet occupies one 20 ms
// slot of a constant-rate multiplex (75.2 kbit/s); there are 150 slots (3 s).
//
//   - Slot 0 is a null packet.
//   - PAT: slots 1 and 2, then every 200 ms in the slots 10k+9.
//   - PMT of program 1: slots 3 and 4, then the slots 10k+3 from 13.
//   - PMT of program 2: section 0 in the slots 10k+5, section 1 in the slots
//     10k+7.
//   - Video: the even slots from 6, one PES packet per TS packet, each with a
//     PCR (every 40 ms).
//   - The slots 10k+1 from 11 are null packets.
//
// PCR follows the slot clock (10 s + slot × 20 ms), and the video PTS is
// 100 ms ahead of it. The analyzer treats a table as complete at its next
// copy, so the first PAT is acquired in slots 1 and 2, the first PMT of
// program 1 in slots 3 and 4, and that of program 2 in slots 5, 7 and 15.
//
// Scenarios (-scenario):
//
//   - healthy: the base stream.
//   - pat-late: no PAT in the slots 49 to 89, so 1200 ms pass between the
//     PATs in slots 39 and 99.
//   - pat-stops: no PAT after slot 2.
//   - pmt-missing: no PMT of program 2, which the PAT still announces.
//   - incomplete: section 1 of program 2's PMT is never sent.
//   - crc: the PAT in slot 59 and the PMT of program 1 in slot 63 have an
//     invalid CRC_32.
//   - wrong-table-id: the packet in slot 59 carries a PMT section (table_id
//     0x02) on the PAT PID instead of the PAT.
//   - version-change: from slot 73 the PMT of program 1 has version 1, and
//     from slot 79 the PAT has version 1; their content is unchanged.
//   - no-pcr: the video packets carry no PCR.
//   - discontinuity: from slot 70 the PCR and PTS are 10 s later, and the PCR
//     in slot 70 has discontinuity_indicator set.
//   - unusable: the PAT in slot 59 has transport_error_indicator set and the
//     PMT of program 1 in slot 63 is scrambled.
//   - defects: pat-late, crc on the PMT of program 1 only, wrong-table-id,
//     incomplete and version-change of the PMT of program 1 together.
//
// With -m2ts, each packet gets a 4-byte TP_extra_header whose
// arrival_time_stamp advances by one 20 ms slot per packet.
package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
)

const (
	tsPacketSize = 188
	slots        = 150
	pmt1Pid      = 0x1000
	pmt2Pid      = 0x1010
	videoPid     = 0x0100
	audioPid     = 0x0101
	nullPid      = 0x1FFF
	// slotMs is the duration of one slot (one packet).
	slotMs = 20
	// slotTicks27 is one slot of the 27 MHz system clock.
	slotTicks27 = slotMs * 27_000
	// baseMs is the PCR time of slot 0.
	baseMs = 10_000
	// jumpMs is the PCR and PTS jump of the discontinuity scenario.
	jumpMs = 10_000
	// esPayload is the filler elementary-stream payload of each PES packet.
	esPayload = 16
)

var scenarios = []string{"healthy", "pat-late", "pat-stops", "pmt-missing", "incomplete", "crc", "wrong-table-id", "version-change", "no-pcr", "discontinuity", "unusable", "defects"}

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

func patSection(version byte) []byte {
	return section(0x00, []byte{
		0x00, 0x01, // transport_stream_id 1
		0xC1 | version<<1, // version, current_next_indicator 1
		0x00, 0x00,        // section_number, last_section_number
		0x00, 0x01, 0xE0 | pmt1Pid>>8, pmt1Pid & 0xFF, // program 1
		0x00, 0x02, 0xE0 | pmt2Pid>>8, pmt2Pid & 0xFF, // program 2
	})
}

func pmt1Section(version byte) []byte {
	return section(0x02, []byte{
		0x00, 0x01, // program_number 1
		0xC1 | version<<1,
		0x00, 0x00,
		0xE0 | videoPid>>8, videoPid & 0xFF, // PCR_PID
		0xF0, 0x00, // program_info_length 0
		0x02, 0xE0 | videoPid>>8, videoPid & 0xFF, 0xF0, 0x00, // MPEG-2 video
	})
}

// pmt2Section is section number of program 2's two-section PMT.
func pmt2Section(number byte) []byte {
	stream := []byte{0x02, 0xE0 | videoPid>>8, videoPid & 0xFF, 0xF0, 0x00}
	if number == 1 {
		stream = []byte{0x03, 0xE0 | audioPid>>8, audioPid & 0xFF, 0xF0, 0x00}
	}
	body := []byte{
		0x00, 0x02, // program_number 2
		0xC1,
		number, 0x01, // section_number, last_section_number 1
		0xE0 | videoPid>>8, videoPid & 0xFF,
		0xF0, 0x00,
	}
	return section(0x02, append(body, stream...))
}

// breakCRC flips the last bit of the CRC_32.
func breakCRC(s []byte) []byte {
	s[len(s)-1] ^= 0x01
	return s
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

// videoPacket is one TS packet holding a whole video PES packet with a PTS,
// padded with adaptation-field stuffing, and a PCR when hasPcr is set.
func videoPacket(cc byte, pts, pcr uint64, hasPcr, discontinuity bool) []byte {
	pes := []byte{0x00, 0x00, 0x01, 0xE0}
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
	if discontinuity {
		af[1] |= 0x80
	}
	for len(af) < 1+afLength {
		af = append(af, 0xFF)
	}
	p := append(header(videoPid, true, 3, cc), af...)
	return append(p, pes...)
}

// generate returns the 188-byte stream of a scenario.
func generate(scenario string) []byte {
	has := func(names ...string) bool { return slices.Contains(names, scenario) }
	var out []byte
	var patCC, pmt1CC, pmt2CC, videoCC byte
	for slot := range slots {
		if has("wrong-table-id", "defects") && slot == 59 {
			out = append(out, psiPacket(0x0000, patCC, pmt1Section(0))...)
			patCC++
			continue
		}
		var p []byte
		switch {
		case slot == 1 || slot == 2 || slot%10 == 9:
			if has("pat-late", "defects") && slot >= 49 && slot <= 89 || has("pat-stops") && slot > 2 {
				p = nullPacket()
				break
			}
			var version byte
			if has("version-change") && slot >= 79 {
				version = 1
			}
			s := patSection(version)
			if has("crc") && slot == 59 {
				s = breakCRC(s)
			}
			p = psiPacket(0x0000, patCC, s)
			if has("unusable") && slot == 59 {
				p[1] |= 0x80 // transport_error_indicator
			}
			patCC++
		case slot == 3 || slot == 4 || slot >= 13 && slot%10 == 3:
			var version byte
			if has("version-change", "defects") && slot >= 73 {
				version = 1
			}
			s := pmt1Section(version)
			if has("crc", "defects") && slot == 63 {
				s = breakCRC(s)
			}
			p = psiPacket(pmt1Pid, pmt1CC, s)
			if has("unusable") && slot == 63 {
				// transport_scrambling_control 2 and a payload that no
				// longer reads as a section.
				p[3] = p[3]&0x3F | 0x80
				for i := 4; i < len(p); i++ {
					p[i] ^= 0x5A
				}
			}
			pmt1CC++
		case slot%10 == 5 || slot%10 == 7:
			number := byte(0)
			if slot%10 == 7 {
				number = 1
			}
			if has("pmt-missing") || has("incomplete", "defects") && number == 1 {
				p = nullPacket()
				break
			}
			p = psiPacket(pmt2Pid, pmt2CC, pmt2Section(number))
			pmt2CC++
		case slot >= 6 && slot%2 == 0:
			clockMs := baseMs + slot*slotMs
			if has("discontinuity") && slot >= 70 {
				clockMs += jumpMs
			}
			pcr := uint64(clockMs) * 27_000
			pts := uint64(clockMs+100) * 90
			p = videoPacket(videoCC, pts, pcr, !has("no-pcr"), has("discontinuity") && slot == 70)
			videoCC++
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
	scenario := flag.String("scenario", "defects", fmt.Sprintf("one of %v", scenarios))
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "usage: generate_psi_health -output <out.ts> [-scenario name] [-m2ts]")
		os.Exit(2)
	}
	if !slices.Contains(scenarios, *scenario) {
		fmt.Fprintf(os.Stderr, "unknown scenario %q (expected one of %v)\n", *scenario, scenarios)
		os.Exit(2)
	}
	data := generate(*scenario)
	if *asM2TS {
		data = m2ts(data)
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
