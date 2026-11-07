// Command generate_pcr_discontinuities writes a short, deterministic transport
// stream whose PCR passes through every kind of PCR step the analyzer
// distinguishes: a 33-bit wraparound, a declared discontinuity, a
// discontinuity declared on an earlier packet of the PCR_PID, an undeclared
// backward step, an undeclared forward jump and a PCR_PID change. It is
// intentionally invalid (the undeclared steps violate ISO/IEC 13818-1,
// 2.4.3.5): use it to test PCR timing, never as an example of a compliant
// stream.
//
// The stream has one program (program_number 1, PMT PID 0x1000) with MPEG-2
// video announced on PID 0x0100. Every packet occupies one 20 ms slot of a
// constant-rate multiplex (75.2 kbit/s); there are 260 slots (5.2 s). Slot 0
// is a null packet, the PAT is sent in slots 1 and 2 and the PMT in slots 3
// and 4 (the analyzer treats a table as complete at its next copy, so the
// analysis starts at slot 5); the PAT repeats in slots 19, 39, ... and the PMT
// in slots 23, 43, .... From slot 6 on, every even slot carries a PCR in an
// adaptation-field-only packet (no PES): every 40 ms. The other slots are null
// packets.
//
// The PCR follows the slot clock, starting 1 s before the 2^33*300 wrap, so
// that the PCR of slot 50 is 0 (a wraparound). From the events below on, the
// clock is offset:
//
//   - slot 80: +5000 ms with discontinuity_indicator on the PCR packet
//     (declared).
//   - slot 109: an adaptation-field-only packet of PID 0x0100 with
//     discontinuity_indicator and no PCR; slot 110: -3000 ms (declared by the
//     earlier packet).
//   - slot 140: -2000 ms without discontinuity_indicator (backward).
//   - slot 170: +4000 ms without discontinuity_indicator (forward jump).
//   - slots 201 and 203: PMT version 1 with PCR_PID 0x0101, applied from slot
//     202; from slot 202 on the PCR is carried on PID 0x0101 with the same
//     clock (PCR_PID change).
//
// Every PCR interval is 40 ms. With -clean the same stream is written without
// the events: one continuous clock through the wraparound, on PID 0x0100.
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
	slots        = 260
	pmtPid       = 0x1000
	videoPid     = 0x0100
	newPcrPid    = 0x0101
	nullPid      = 0x1FFF
	// slotMs is the duration of one slot (one packet).
	slotMs = 20
	// ticksPerMs is one millisecond of the 27 MHz system clock.
	ticksPerMs = 27_000
	// wrapTicks is the PCR modulus.
	wrapTicks = (uint64(1) << 33) * 300
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

func pmtSection(version byte, pcrPid uint16) []byte {
	return section(0x02, []byte{
		0x00, 0x01, // program_number 1
		0xC1 | version<<1,
		0x00, 0x00,
		0xE0 | byte(pcrPid>>8), byte(pcrPid), // PCR_PID
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

// adaptationPacket is an adaptation-field-only packet (no payload, so the
// continuity_counter does not advance) with discontinuity_indicator when
// discontinuity is set and a PCR when hasPcr is set.
func adaptationPacket(pid uint16, discontinuity, hasPcr bool, pcr uint64) []byte {
	af := []byte{tsPacketSize - 5, 0x00}
	if discontinuity {
		af[1] |= 0x80
	}
	if hasPcr {
		af[1] |= 0x10
		af = append(af, pcrBytes(pcr)...)
	}
	for len(af) < tsPacketSize-4 {
		af = append(af, 0xFF)
	}
	return append(header(pid, false, 2, 0), af...)
}

// clockOffsetMs is the offset of the PCR from the slot clock at slot.
func clockOffsetMs(slot int, clean bool) int64 {
	if clean {
		return 0
	}
	var offset int64
	for _, event := range []struct {
		slot     int
		offsetMs int64
	}{{80, 5000}, {110, -3000}, {140, -2000}, {170, 4000}} {
		if slot >= event.slot {
			offset += event.offsetMs
		}
	}
	return offset
}

// slotPcr is the PCR of slot: the slot clock, starting 1 s before the wrap,
// plus the offset of the events so far, modulo the wrap.
func slotPcr(slot int, clean bool) uint64 {
	ms := int64(slot*slotMs) - 1000 + clockOffsetMs(slot, clean)
	ticks := (int64(wrapTicks) + ms*ticksPerMs) % int64(wrapTicks)
	return uint64(ticks)
}

// generate returns the 188-byte stream, with the events unless clean is set.
func generate(clean bool) []byte {
	var out []byte
	var patCC, pmtCC byte
	for slot := range slots {
		var p []byte
		switch {
		case slot == 1 || slot == 2 || slot%20 == 19:
			p = psiPacket(0x0000, patCC, patSection())
			patCC++
		case slot == 3 || slot == 4 || slot > 20 && slot%20 == 3:
			s := pmtSection(0, videoPid)
			if !clean && slot >= 201 {
				s = pmtSection(1, newPcrPid)
			}
			p = psiPacket(pmtPid, pmtCC, s)
			pmtCC++
		case !clean && slot == 201:
			p = psiPacket(pmtPid, pmtCC, pmtSection(1, newPcrPid))
			pmtCC++
		case slot >= 6 && slot%2 == 0:
			pid := uint16(videoPid)
			if !clean && slot >= 202 {
				pid = newPcrPid
			}
			declared := !clean && slot == 80
			p = adaptationPacket(pid, declared, true, slotPcr(slot, clean))
		case !clean && slot == 109:
			p = adaptationPacket(videoPid, true, false, 0)
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
		ats := uint32(i/tsPacketSize*slotMs*ticksPerMs) & 0x3FFFFFFF
		out = append(out, byte(ats>>24), byte(ats>>16), byte(ats>>8), byte(ats))
		out = append(out, data[i:i+tsPacketSize]...)
	}
	return out
}

func main() {
	output := flag.String("output", "", "output transport stream")
	asM2TS := flag.Bool("m2ts", false, "write 192-byte M2TS packets")
	clean := flag.Bool("clean", false, "write the stream without the PCR events")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "usage: generate_pcr_discontinuities -output <out.ts> [-m2ts] [-clean]")
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
