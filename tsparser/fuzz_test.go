package tsparser

import (
	"bytes"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

const (
	maxFuzzSectionSize = 4096
	maxFuzzPacketData  = 3*(tsPayloadSize+tpExtraHeaderSize) + 64
	maxFuzzStreamData  = maxFuzzSectionSize + 3*(tsPayloadSize+tpExtraHeaderSize)
)

func FuzzTsPacketAndAdaptationField(f *testing.F) {
	validPacket := make([]byte, tsPayloadSize)
	validPacket[0] = 0x47
	validPacket[3] = 0x10
	validPacket[4] = 0x00
	oversizedAdaptationField := make([]byte, tsPayloadSize)
	oversizedAdaptationField[0] = 0x47
	oversizedAdaptationField[3] = 0x30
	oversizedAdaptationField[4] = 0xFF
	f.Add(validPacket)
	f.Add([]byte{0x00})
	f.Add([]byte{0x07, 0x10, 0x00, 0x00, 0x00, 0x32, 0x7E, 0x32})
	f.Add(oversizedAdaptationField)
	f.Add([]byte{0x47, 0x00, 0x00, 0x30, 0x01, 0x10})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > tsPayloadSize {
			return
		}

		packet := NewTsPacket()
		packet.Initialize(0, options.Options{})
		packet.Append(data)
		_ = packet.Parse()

		// Exercise adaptation-field parsing directly as well as through a TS
		// packet. Its length byte and every optional-field flag are untrusted.
		if len(data) <= tsPayloadSize-tsHeaderSize {
			field := NewAdaptationField()
			field.Initialize(0, options.Options{})
			field.Append(data)
			_, _ = field.Parse()
		}
	})
}

func FuzzPsiSections(f *testing.F) {
	validPAT := []byte{
		0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00,
		0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53,
	}
	validPMT := []byte{
		0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00,
		0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0,
		0x00, 0x15, 0xBD, 0x4D, 0x56,
	}
	f.Add(validPAT)
	f.Add(validPMT)
	f.Add(validPAT[:8])
	f.Add(validPMT[:12])
	f.Add([]byte{0x00, 0xBF, 0xFF})
	f.Add([]byte{0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x31, 0xF0, 0x06})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzSectionSize {
			return
		}

		pat := NewPat()
		pat.Append(data)
		_ = pat.Parse()

		pmt := NewPmt()
		pmt.Append(data)
		_ = pmt.Parse()
	})
}

func FuzzPsiBuffering(f *testing.F) {
	validPAT := []byte{
		0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00,
		0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53,
	}
	validPMT := []byte{
		0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00,
		0xE0, 0x31, 0xF0, 0x00, 0x1B, 0xE0, 0x31, 0xF0,
		0x00, 0x15, 0xBD, 0x4D, 0x56,
	}
	f.Add(fuzzPSIStream(tsPayloadSize, 0x0000, validPAT), false)
	f.Add(fuzzPSIStream(tsPayloadSize+tpExtraHeaderSize, 0x0000, validPAT), true)
	f.Add(fuzzPSIStream(tsPayloadSize, 0x003F, validPMT), false)
	f.Add(fuzzPSIStream(tsPayloadSize+tpExtraHeaderSize, 0x003F, validPMT), true)
	invalidPointer := make([]byte, tsPayloadSize)
	invalidPointer[0] = 0x47
	invalidPointer[1] = 0x40
	invalidPointer[3] = 0x10
	invalidPointer[4] = 0xFF
	f.Add(invalidPointer, false)

	f.Fuzz(func(t *testing.T, data []byte, m2ts bool) {
		if len(data) > maxFuzzStreamData {
			return
		}

		packetSize := tsPayloadSize
		if m2ts {
			packetSize += tpExtraHeaderSize
		}
		for _, target := range []struct {
			pid    uint16
			packet MpegPacket
		}{
			{pid: 0x0000, packet: NewPat()},
			{pid: 0x003F, packet: NewPmt()},
		} {
			pos := int64(0)
			_ = BufferPsi(bytes.NewReader(data), &pos, target.pid, target.packet, options.Options{}, packetSize, 0)
			_ = target.packet.Parse()
		}
	})
}

func FuzzDescriptors(f *testing.F) {
	f.Add([]byte{0x0A, 0x04, 'e', 'n', 'g', 0x00})
	f.Add([]byte{0x05, 0xFF, 'H', 'D', 'M', 'V'})
	f.Add([]byte{0x7C})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzSectionSize {
			return
		}

		reader := newDefaultBitReader()
		reader.Set(data)
		_, _ = parseDescriptors(reader, uint16(len(data)))
	})
}

func FuzzPesHeader(f *testing.F) {
	validPTS := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80,
		0x05, 0x21, 0x00, 0x07, 0xD8, 0x61,
	}
	validPTSDTS := []byte{
		0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x84, 0xC0,
		0x0A, 0x31, 0x00, 0x01, 0xC7, 0x3F, 0x11, 0x00,
		0x01, 0xAF, 0xC9, 0x00,
	}
	f.Add(validPTS)
	f.Add(validPTSDTS)
	f.Add(validPTS[:9])
	f.Add([]byte{0x00, 0x00, 0x01, 0xBE, 0x00, 0x00})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzSectionSize {
			return
		}

		pes := NewPes()
		pes.Initialize(0x100, 0, 0, 0)
		pes.Append(data)
		_ = pes.Parse()
	})
}

func FuzzPacketDiscovery(f *testing.F) {
	packet188 := fuzzPATPackets(tsPayloadSize)
	packet192 := fuzzPATPackets(tsPayloadSize + tpExtraHeaderSize)
	f.Add(packet188)
	f.Add(packet192)
	f.Add(packet188[:2*tsPayloadSize])
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzPacketData {
			return
		}

		packetSize := detectPacketSize(data)
		_, _ = findPat(data, packetSize)
	})
}

func fuzzPATPackets(packetSize int) []byte {
	data := make([]byte, 3*packetSize)
	syncOffset := packetSize - tsPayloadSize
	for offset := syncOffset; offset < len(data); offset += packetSize {
		data[offset] = 0x47
		data[offset+1] = 0x40
		data[offset+2] = 0x00
		data[offset+3] = 0x10
	}
	return data
}

func fuzzPSIStream(packetSize int, pid uint16, section []byte) []byte {
	data := make([]byte, 2*packetSize)
	syncOffset := packetSize - tsPayloadSize
	for packet := 0; packet < 2; packet++ {
		offset := packet*packetSize + syncOffset
		for i := offset; i < offset+tsPayloadSize; i++ {
			data[i] = 0xFF
		}
		data[offset] = 0x47
		data[offset+1] = 0x40 | byte(pid>>8)
		data[offset+2] = byte(pid)
		data[offset+3] = 0x10 | byte(packet)
		data[offset+4] = 0x00
		copy(data[offset+5:], section)
	}
	return data
}
