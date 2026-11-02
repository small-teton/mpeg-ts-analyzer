// Command generate_psi_version_change rewrites a single-program transport
// stream so that its PMT and then its PAT change version mid-stream, for
// cross-checking PAT/PMT version tracking with an independent analyzer.
//
// From the PMT packet one third into the stream, the PMT is sent as version 1:
// the video PID 0x0100 becomes 0x0200 (also the new PCR_PID) and the audio PID
// 0x0101 changes stream_type from 0x03 to 0x04. Video packets move to PID
// 0x0200 from the first video payload_unit_start after that PMT packet, so a
// few packets of the old PID still follow the change. From the first PAT packet
// after the PMT packet two thirds into the stream, the PAT is sent as version
// 1 and moves the PMT to PID 0x1100; later PMT packets use that PID and keep
// version 1.
//
// Packet count, positions, continuity counters, PCRs and timestamps stay as
// in the input, which must carry each PAT and PMT section in one packet. With
// -m2ts, each output packet gets a 4-byte TP_extra_header whose
// arrival_time_stamp advances by a constant step, as for a 500 kbit/s stream.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

const (
	tsPacketSize = 188
	patPid       = 0x0000
	pmtPid       = 0x1000
	movedPmtPid  = 0x1100
	videoPid     = 0x0100
	newVideoPid  = 0x0200
	audioPid     = 0x0101
	newAudioType = 0x04
	// atsStep is the 27 MHz arrival_time_stamp step of one packet at 500 kbit/s.
	atsStep = 27_000_000 * tsPacketSize * 8 / 500_000
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

// withVersion returns a copy of a section with a new version_number and CRC32,
// after edit has changed its body (the bytes between header and CRC32).
func withVersion(section []byte, version byte, edit func(body []byte)) []byte {
	s := append([]byte{}, section...)
	s[5] = s[5]&0xC1 | version<<1
	edit(s[8 : len(s)-4])
	crc := crc32MPEG(s[:len(s)-4])
	s[len(s)-4], s[len(s)-3], s[len(s)-2], s[len(s)-1] = byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc)
	return s
}

func setPid(b []byte, pid uint16) {
	b[0] = b[0]&0xE0 | byte(pid>>8)
	b[1] = byte(pid)
}

func getPid(b []byte) uint16 { return uint16(b[0]&0x1F)<<8 | uint16(b[1]) }

// pmtVersion1 moves the video stream and the PCR to newVideoPid and retypes
// the audio stream.
func pmtVersion1(pmt []byte) ([]byte, error) {
	var err error
	section := withVersion(pmt, 1, func(body []byte) {
		if getPid(body[0:2]) != videoPid {
			err = errors.New("PCR_PID is not the video PID 0x0100")
			return
		}
		setPid(body[0:2], newVideoPid)
		found := 0
		programInfoLength := int(body[2]&0x0F)<<8 | int(body[3])
		for rest := body[4+programInfoLength:]; len(rest) >= 5; {
			switch getPid(rest[1:3]) {
			case videoPid:
				setPid(rest[1:3], newVideoPid)
				found++
			case audioPid:
				rest[0] = newAudioType
				found++
			}
			rest = rest[5+(int(rest[3]&0x0F)<<8|int(rest[4])):]
		}
		if found != 2 {
			err = errors.New("the PMT must list video PID 0x0100 and audio PID 0x0101")
		}
	})
	return section, err
}

// patVersion1 moves the PMT of the only program to movedPmtPid.
func patVersion1(pat []byte) ([]byte, error) {
	var err error
	section := withVersion(pat, 1, func(body []byte) {
		if len(body) != 4 || getPid(body[2:4]) != pmtPid {
			err = errors.New("the PAT must list one program on PMT PID 0x1000")
			return
		}
		setPid(body[2:4], movedPmtPid)
	})
	return section, err
}

type packet struct {
	ts      []byte // the 188-byte TS packet within the output
	pid     uint16
	pusi    bool
	section []byte // the section after a zero pointer_field, or nil
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
		p := packet{ts: ts, pid: getPid(ts[1:3]), pusi: ts[1]&0x40 != 0}
		if p.pusi && ts[3]&0x30 == 0x10 && ts[4] == 0 {
			s := ts[5:]
			if length := 3 + (int(s[1]&0x0F)<<8 | int(s[2])); length <= len(s) {
				p.section = s[:length]
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// replaceSection writes section into a packet that carries one section.
func replaceSection(p packet, section []byte) error {
	if p.section == nil || len(section) != len(p.section) || crc32MPEG(p.section) != 0 {
		return fmt.Errorf("packet on PID 0x%04x does not carry one valid section", p.pid)
	}
	copy(p.section, section)
	return nil
}

func generate(data []byte) ([]byte, error) {
	packetSize := tsPacketSize
	if len(data) >= 3*192 && (data[0] != 0x47 || data[188] != 0x47 || data[376] != 0x47) {
		packetSize = 192
	}
	out := append([]byte{}, data...)
	all, err := packets(out, packetSize)
	if err != nil {
		return nil, err
	}
	var pmtPackets, patPackets []int
	for i, p := range all {
		switch {
		case p.pid == pmtPid && p.pusi:
			pmtPackets = append(pmtPackets, i)
		case p.pid == patPid && p.pusi:
			patPackets = append(patPackets, i)
		}
	}
	if len(pmtPackets) < 6 || len(patPackets) < 6 {
		return nil, errors.New("the input needs at least six PAT and six PMT packets")
	}
	pmtChange := pmtPackets[len(pmtPackets)/3]
	patChange := -1
	for _, i := range patPackets {
		if i > pmtPackets[2*len(pmtPackets)/3] {
			patChange = i
			break
		}
	}
	if patChange < 0 {
		return nil, errors.New("no PAT packet after the PMT change")
	}
	pmt1, err := pmtVersion1(all[pmtChange].section)
	if err != nil {
		return nil, err
	}
	pat1, err := patVersion1(all[patChange].section)
	if err != nil {
		return nil, err
	}

	videoMoved := false
	for i, p := range all {
		switch {
		case p.pid == pmtPid && i >= pmtChange:
			if err := replaceSection(p, pmt1); err != nil {
				return nil, err
			}
			if i > patChange {
				setPid(p.ts[1:3], movedPmtPid)
			}
		case p.pid == patPid && i >= patChange:
			if err := replaceSection(p, pat1); err != nil {
				return nil, err
			}
		case p.pid == videoPid && i > pmtChange && (videoMoved || p.pusi):
			videoMoved = true
			setPid(p.ts[1:3], newVideoPid)
		}
	}
	return out, nil
}

// m2ts prefixes every 188-byte packet with a TP_extra_header.
func m2ts(data []byte) []byte {
	out := make([]byte, 0, len(data)/tsPacketSize*(tsPacketSize+4))
	for i := 0; i+tsPacketSize <= len(data); i += tsPacketSize {
		ats := uint32(i/tsPacketSize*atsStep) & 0x3FFFFFFF
		out = append(out, byte(ats>>24), byte(ats>>16), byte(ats>>8), byte(ats))
		out = append(out, data[i:i+tsPacketSize]...)
	}
	return out
}

func main() {
	input := flag.String("input", "", "input transport stream (188-byte TS or 192-byte M2TS)")
	output := flag.String("output", "", "output transport stream")
	asM2TS := flag.Bool("m2ts", false, "write 192-byte M2TS packets (the input must be 188-byte TS)")
	flag.Parse()
	if *input == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: generate_psi_version_change -input <in.ts> -output <out.ts> [-m2ts]")
		os.Exit(2)
	}
	data, err := os.ReadFile(*input)
	if err == nil {
		data, err = generate(data)
	}
	if err == nil && *asM2TS {
		if len(data) > 0 && data[0] != 0x47 {
			err = errors.New("-m2ts needs a 188-byte TS input")
		} else {
			data = m2ts(data)
		}
	}
	if err == nil {
		err = os.WriteFile(*output, data, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
