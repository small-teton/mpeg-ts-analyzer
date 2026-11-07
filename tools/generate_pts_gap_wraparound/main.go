// Command generate_pts_gap_wraparound writes a short, deterministic transport
// stream whose PCR-to-PTS/DTS leads cross the PCR and PTS/DTS wraparounds in
// both orders: a PTS/DTS that wraps before the PCR (positive leads) and a PTS
// that wraps after the PCR (a negative lead), with PES packets whose
// bracketing PCRs span the PCR wraparound. It is intentionally unusual
// (filler elementary-stream payload, a PES packet multiplexed after its PTS):
// use it to test the PCR-PTS/DTS gap check, --dump-timestamp and the mux lead
// across a wraparound, never as an example of a compliant stream.
//
// The stream has one program (program_number 1, PMT PID 0x1000) with MPEG-2
// video (stream_type 0x02) on PID 0x0100, which also carries the PCR, MPEG-1
// audio (0x03) on PID 0x0101 and AAC (0x0F) on PID 0x0102. Every packet
// occupies one 20 ms slot of a constant-rate multiplex (75.2 kbit/s); there
// are 200 slots (4 s). Slot 0 is a null packet, the PAT is sent in slots 1 and
// 2 and the PMT in slots 3 and 4; the PAT repeats in the slots 20k and the PMT
// in the slots 20k+8. Video PES packets take the slots 4k+2 from 6 (every
// 80 ms, each packet with a PCR), AAC PES packets the slots 4k+1 from 5 and
// audio PES packets the slots 4k+3 from 7. The remaining slots are null
// packets.
//
// Clock: the 90 kHz system clock of slot s is 2^33 - 100 × 1800 + s × 1800
// (modulo 2^33), so the PCR wraps at slot 100, between the PCRs of slots 98
// and 102. Every PTS/DTS is the clock of its slot plus its lead, modulo 2^33:
//
//   - video: DTS lead 700 ms, PTS 40 ms after the DTS. The DTS wraps at slot
//     65, before the PCR;
//   - audio: PTS lead 30 ms. The PTS wraps between slots 98 and 99, so the
//     PES packet of slot 99 has a wrapped PTS and a PCR bracket (98 to 102)
//     across the PCR wrap;
//   - AAC: PTS lead -150 ms (multiplexed after its PTS). The PTS wraps at
//     slot 107.5, after the PCR: the PES packets of slots 101 and 105 have a
//     PCR that has wrapped and a PTS that has not.
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
	slots        = 200
	pmtPid       = 0x1000
	videoPid     = 0x0100
	audioPid     = 0x0101
	aacPid       = 0x0102
	nullPid      = 0x1FFF
	// slotMs is the duration of one slot (one packet).
	slotMs = 20
	// slot90 is one slot of the 90 kHz clock.
	slot90 = slotMs * 90
	// wrap90 is the period of the 33-bit 90 kHz clock.
	wrap90 = uint64(1) << 33
	// wrapSlot is the slot at which the clock wraps.
	wrapSlot = 100
	// The leads of the elementary streams.
	videoLeadMs = 700
	audioLeadMs = 30
	aacLeadMs   = -150
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
	}{{0x02, videoPid}, {0x03, audioPid}, {0x0F, aacPid}} {
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

// boundedPes returns a PES packet with a PTS alone and a PES_packet_length
// that covers it.
func boundedPes(streamID byte, pts uint64) []byte {
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
	return uint64(t) % wrap90
}

func generate() []byte {
	var out []byte
	var patCC, pmtCC, videoCC, audioCC, aacCC byte
	for slot := range slots {
		var p []byte
		switch {
		case slot == 1 || slot == 2 || slot > 0 && slot%20 == 0:
			p = psiPacket(0x0000, patCC, patSection())
			patCC++
		case slot == 3 || slot == 4 || slot%20 == 8:
			p = psiPacket(pmtPid, pmtCC, pmtSection())
			pmtCC++
		case slot >= 6 && slot%4 == 2: // video, every 80 ms, with the PCR
			data := videoPes(clock90(slot, videoLeadMs+40), clock90(slot, videoLeadMs))
			p = pesPacket(videoPid, videoCC, data, clock90(slot, 0)*300, true, false)
			videoCC++
		case slot >= 5 && slot%4 == 1: // AAC, every 80 ms
			p = pesPacket(aacPid, aacCC, boundedPes(0xC1, clock90(slot, aacLeadMs)), 0, false, false)
			aacCC++
		case slot >= 7 && slot%4 == 3: // audio, every 80 ms
			p = pesPacket(audioPid, audioCC, boundedPes(0xC0, clock90(slot, audioLeadMs)), 0, false, false)
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
		ats := uint32(i/tsPacketSize*slot90*300) & 0x3FFFFFFF
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
		fmt.Fprintln(os.Stderr, "usage: generate_pts_gap_wraparound -output <out.ts> [-m2ts]")
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
