// Command generate_multi_section_psi rewrites a transport stream so that its
// PAT and every PMT are split into several sections, for cross-checking
// multi-section table assembly with an independent analyzer.
//
// The PAT gets one section per program and each PMT one section per
// elementary stream (section 0 also carries PCR_PID and the program
// descriptors). Each PSI packet of the input carries one section, sent from
// the last section to section 0, so tables arrive out of order. Only PSI
// payloads change: packet count, positions, PCRs and timestamps stay as in the
// input, which must carry each PAT and PMT section in one packet.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

const tsPacketSize = 188

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

// section builds a long-form section with section_syntax_indicator 1.
func section(tableID byte, extension uint16, version, number, last byte, body []byte) []byte {
	length := 5 + len(body) + 4
	s := []byte{tableID, 0xB0 | byte(length>>8), byte(length), byte(extension >> 8), byte(extension), 0xC1 | version<<1, number, last}
	s = append(s, body...)
	crc := crc32MPEG(s)
	return append(s, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
}

type packet struct {
	ts      []byte // the 188-byte TS packet
	pid     uint16
	pusi    bool
	payload []byte
}

func packets(data []byte, packetSize int) ([]packet, error) {
	if len(data)%packetSize != 0 {
		return nil, fmt.Errorf("input size %d is not a multiple of %d", len(data), packetSize)
	}
	var out []packet
	for offset := 0; offset < len(data); offset += packetSize {
		ts := data[offset+packetSize-tsPacketSize : offset+packetSize]
		if ts[0] != 0x47 {
			return nil, fmt.Errorf("no sync byte at offset %d", offset)
		}
		p := packet{ts: ts, pid: uint16(ts[1]&0x1F)<<8 | uint16(ts[2]), pusi: ts[1]&0x40 != 0}
		if ts[3]&0x30 == 0x10 {
			p.payload = ts[4:]
		}
		out = append(out, p)
	}
	return out, nil
}

// firstSection returns the first complete section on pid, which must start
// right after the pointer_field of a payload_unit_start packet.
func firstSection(all []packet, pid uint16) ([]byte, error) {
	for _, p := range all {
		if p.pid != pid || !p.pusi || len(p.payload) < 4 || p.payload[0] != 0 {
			continue
		}
		s := p.payload[1:]
		length := 3 + (int(s[1]&0x0F)<<8 | int(s[2]))
		if length > len(s) {
			return nil, fmt.Errorf("section on PID 0x%04x spans packets", pid)
		}
		if crc32MPEG(s[:length]) != 0 {
			return nil, fmt.Errorf("section on PID 0x%04x has an invalid CRC32", pid)
		}
		return s[:length], nil
	}
	return nil, fmt.Errorf("no section on PID 0x%04x", pid)
}

// splitPat returns one section per program; network entries go to section 0.
func splitPat(pat []byte) ([][]byte, []uint16, error) {
	entries := pat[8 : len(pat)-4]
	var network, programs [][]byte
	var pmtPids []uint16
	for i := 0; i+4 <= len(entries); i += 4 {
		entry := entries[i : i+4]
		if entry[0] == 0 && entry[1] == 0 {
			network = append(network, entry)
			continue
		}
		programs = append(programs, entry)
		pmtPids = append(pmtPids, uint16(entry[2]&0x1F)<<8|uint16(entry[3]))
	}
	if len(programs) < 2 {
		return nil, nil, errors.New("the PAT needs at least two programs")
	}
	extension, version := uint16(pat[3])<<8|uint16(pat[4]), pat[5]>>1&0x1F
	var sections [][]byte
	for number, program := range programs {
		var body []byte
		if number == 0 {
			for _, entry := range network {
				body = append(body, entry...)
			}
		}
		body = append(body, program...)
		sections = append(sections, section(0x00, extension, version, byte(number), byte(len(programs)-1), body))
	}
	return sections, pmtPids, nil
}

// splitPmt returns one section per elementary stream.
func splitPmt(pmt []byte) ([][]byte, error) {
	programInfoLength := int(pmt[10]&0x0F)<<8 | int(pmt[11])
	header := pmt[8:12] // PCR_PID and program_info_length
	descriptors := pmt[12 : 12+programInfoLength]
	var streams [][]byte
	for rest := pmt[12+programInfoLength : len(pmt)-4]; len(rest) >= 5; {
		size := 5 + (int(rest[3]&0x0F)<<8 | int(rest[4]))
		streams = append(streams, rest[:size])
		rest = rest[size:]
	}
	if len(streams) < 2 {
		return nil, errors.New("each PMT needs at least two elementary streams")
	}
	extension, version := uint16(pmt[3])<<8|uint16(pmt[4]), pmt[5]>>1&0x1F
	var sections [][]byte
	for number, stream := range streams {
		body := []byte{header[0], header[1], 0xF0, 0x00} // program_info_length 0
		if number == 0 {
			body = append(append([]byte{}, header...), descriptors...)
		}
		body = append(body, stream...)
		sections = append(sections, section(0x02, extension, version, byte(number), byte(len(streams)-1), body))
	}
	return sections, nil
}

func generate(data []byte) ([]byte, error) {
	packetSize := tsPacketSize
	if len(data) >= 3*192 && (data[0] != 0x47 || data[188] != 0x47 || data[376] != 0x47) {
		packetSize = 192
	}
	all, err := packets(data, packetSize)
	if err != nil {
		return nil, err
	}
	pat, err := firstSection(all, 0x0000)
	if err != nil {
		return nil, err
	}
	patSections, pmtPids, err := splitPat(pat)
	if err != nil {
		return nil, err
	}
	replacements := map[uint16][][]byte{0x0000: patSections}
	for _, pid := range pmtPids {
		pmt, err := firstSection(all, pid)
		if err != nil {
			return nil, err
		}
		if replacements[pid], err = splitPmt(pmt); err != nil {
			return nil, err
		}
	}

	out := make([]byte, len(data))
	copy(out, data)
	sent := map[uint16]int{}
	for i, p := range all {
		sections, ok := replacements[p.pid]
		if !ok {
			continue
		}
		if !p.pusi || len(p.payload) != tsPacketSize-4 {
			return nil, fmt.Errorf("packet %d on PID 0x%04x is not a payload-only section start", i, p.pid)
		}
		s := sections[len(sections)-1-sent[p.pid]%len(sections)]
		sent[p.pid]++
		payload := out[i*packetSize+packetSize-tsPacketSize+4 : (i+1)*packetSize]
		payload[0] = 0x00 // pointer_field
		copy(payload[1:], s)
		for j := 1 + len(s); j < len(payload); j++ {
			payload[j] = 0xFF
		}
	}
	return out, nil
}

func main() {
	input := flag.String("input", "", "input transport stream (188-byte TS or 192-byte M2TS)")
	output := flag.String("output", "", "output transport stream")
	flag.Parse()
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: generate_multi_section_psi -input <in.ts> -output <out.ts>")
		os.Exit(2)
	}
	data, err := os.ReadFile(*input)
	if err == nil {
		data, err = generate(data)
	}
	if err == nil {
		err = os.WriteFile(*output, data, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
