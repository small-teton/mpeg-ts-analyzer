package tsparser

import (
	"bytes"
	"testing"
	"unicode/utf8"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

const (
	maxFuzzSectionSize = 4096
	// maxFuzzPacketData holds six 204-byte packets: 204-byte detection needs
	// five sync bytes.
	maxFuzzPacketData = 6*trailerPacketSize + 64
	maxFuzzStreamData = maxFuzzSectionSize + 3*trailerPacketSize
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
	packetWithPCR := make([]byte, tsPayloadSize)
	copy(packetWithPCR, []byte{
		0x47, 0x01, 0x00, 0x30, 0x07, 0x10,
		0x00, 0x00, 0x00, 0x00, 0x7E, 0x00,
	})
	f.Add(validPacket)
	f.Add([]byte{0x00})
	f.Add([]byte{0x07, 0x10, 0x00, 0x00, 0x00, 0x32, 0x7E, 0x32})
	f.Add(packetWithPCR)
	f.Add(oversizedAdaptationField)
	f.Add([]byte{0x47, 0x00, 0x00, 0x30, 0x01, 0x10})
	f.Add([]byte{})
	f.Add(rawPsiPacket(0x1000, true, 0, append([]byte{0x00}, pmtSectionN(1, 0, 0, 0, 0x100, [2]uint16{0x02, 0x100})...)))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > tsPayloadSize {
			return
		}

		packet := NewTsPacket()
		packet.Initialize(0, options.Options{})
		packet.Append(data)
		if packet.Parse() == nil {
			_ = packetSectionStarts(packet)
		}

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
		0x00, 0xB5, 0x9E, 0xA0, 0xB0,
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

		_, _ = parseSdtSection(data)
	})
}

func FuzzDVBText(f *testing.F) {
	f.Add([]byte("Service01"))
	f.Add([]byte{0xC2, 'e', 0xE0, 0x8A})
	f.Add([]byte{0xC2})
	f.Add([]byte{0x01, 0xB0, 0x86})
	f.Add([]byte{0x10, 0x00, 0x02, 0xA1})
	f.Add([]byte{0x15, 0xEE, 0x82, 0x8A})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		// A DVB string has a one-byte length.
		if len(data) > 255 {
			return
		}
		if text, ok := decodeDVBText(data); ok && !utf8.ValidString(text) {
			t.Fatalf("decoded % x to invalid UTF-8 %q", data, text)
		}
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
		0x00, 0xB5, 0x9E, 0xA0, 0xB0,
	}
	largePMT := fuzzLargePMT()
	for framing, packetSize := range supportedPacketSizes {
		f.Add(fuzzPSIStream(packetSize, 0x0000, validPAT), uint8(framing))
		f.Add(fuzzPSIStream(packetSize, 0x003F, validPMT), uint8(framing))
		f.Add(fuzzPSIContinuationStream(packetSize, 0x003F, largePMT), uint8(framing))
	}
	invalidPointer := make([]byte, tsPayloadSize)
	invalidPointer[0] = 0x47
	invalidPointer[1] = 0x40
	invalidPointer[3] = 0x10
	invalidPointer[4] = 0xFF
	f.Add(invalidPointer, uint8(0))

	// framing selects the packet size: 188, 192 or 204 bytes.
	f.Fuzz(func(t *testing.T, data []byte, framing uint8) {
		if len(data) > maxFuzzStreamData {
			return
		}

		packetSize := supportedPacketSizes[int(framing)%len(supportedPacketSizes)]
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
		// The multi-section assembler reads the same packets.
		pos := int64(0)
		_, _, _ = acquirePat(bytes.NewReader(data), &pos, options.Options{}, packetSize, 0)
		pos = 0
		_, _, _ = acquirePmt(bytes.NewReader(data), &pos, 0x003F, 1, options.Options{}, packetSize, 0)
	})
}

func FuzzDescriptors(f *testing.F) {
	f.Add(uint16(6), []byte{0x0A, 0x04, 'e', 'n', 'g', 0x00})
	f.Add(uint16(6), []byte{0x05, 0xFF, 'H', 'D', 'M', 'V'})
	f.Add(uint16(1), []byte{0x7C})
	f.Add(uint16(4), []byte{0x0A})
	f.Add(uint16(0), []byte{})

	f.Fuzz(func(t *testing.T, totalLength uint16, data []byte) {
		if int(totalLength) > maxFuzzSectionSize || len(data) > maxFuzzSectionSize {
			return
		}

		reader := newDefaultBitReader()
		reader.Set(data)
		_, _ = parseDescriptors(reader, totalLength)
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
	f.Add([]byte{0x00, 0x00, 0x01, 0xBF, 0x00, 0x00})
	f.Add([]byte{0x00, 0x00, 0x01, 0xBF, 0xFF, 0xFF})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzSectionSize {
			return
		}

		pes := NewPes()
		pes.Initialize(0x100, 0, 0, 0, false)
		pes.Append(data)
		_ = pes.Parse()
	})
}

func FuzzPacketDiscovery(f *testing.F) {
	packet188 := fuzzPATPackets(tsPayloadSize)
	packet192 := fuzzPATPackets(tsPayloadSize + tpExtraHeaderSize)
	f.Add(packet188)
	f.Add(packet192)
	f.Add(withTestTrailers(bytes.Repeat(packet188, 2)))
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

// FuzzPsiTracking scans arbitrary 188-byte packets with the PAT and PMT
// version tracking of one program that starts from a fixed layout, and builds
// the PSI layout report of the scan.
func FuzzPsiTracking(f *testing.F) {
	layout, _ := layoutChangeStream()
	move, _, _, _ := moveStream()
	candidates, _ := candidateStream(1, 0)
	for _, s := range []*trackingStream{layout, move, candidates} {
		f.Add(bytes.Join(s.packets, nil))
	}
	pat, _ := newPatTable([]*Pat{mustParse(f, parsePatSection, patSectionN(0, 0, 0, [2]uint16{1, 0x1000}, [2]uint16{2, 0x1001}))})
	pmt, _ := newPmtTable([]*Pmt{mustParse(f, parsePmtSection, pmtSectionN(1, 0, 0, 0, 0x100, [2]uint16{0x02, 0x100}, [2]uint16{0x03, 0x102}))})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*tsPayloadSize {
			return
		}
		opts := options.Options{DumpBitrate: true}
		analyzer := newProgramAnalyzer(opts, 0, 1, 0x1000, pmt.pcrPid, pmt.programInfos, true)
		continuity := newContinuityCounterSummary(0x1000, pmt.pcrPid, pmt.programInfos)
		tracker := newPsiTracker(pat, continuity, opts, tsPayloadSize, true)
		tracker.addProgram(1, 0x1000, pmt, analyzer)
		tracker.layout = newPsiLayoutRecorder()
		var pos int64
		if _, err := scanPrograms(bytes.NewReader(data), &pos, []*programAnalyzer{analyzer}, continuity, newTransportTrustSummary(), tracker, opts, tsPayloadSize, 0, true); err != nil {
			return
		}
		analyzer.finish()
		_ = reportPsiTransitions(tracker.transitions)
		_ = reportSeries(analyzer)
		analysis := &rangeAnalysis{psi: tracker, pat: pat, packetSize: tsPayloadSize, scanEnd: pos, programs: []rangeProgram{
			{program: Program{ProgramNumber: 1, PmtPid: 0x1000}, pmtPid: 0x1000, pmt: pmt.Pmt, table: pmt, analyzer: analyzer},
			{program: Program{ProgramNumber: 2, PmtPid: 0x1001}, pmtPid: 0x1001},
		}}
		analysis.classifyPsiSections()
		_ = reportPsiLayout(analysis)
	})
}

func mustParse[T any](f *testing.F, parse func([]byte) (T, error), data []byte) T {
	parsed, err := parse(data)
	if err != nil {
		f.Fatal(err)
	}
	return parsed
}

func fuzzPATPackets(packetSize int) []byte {
	data := make([]byte, 3*packetSize)
	syncOffset := tsOffset(packetSize)
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
	syncOffset := tsOffset(packetSize)
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

func fuzzLargePMT() []byte {
	const descriptorPayloadLength = 172
	body := []byte{
		0x00, 0x01, // program_number
		0xC1,       // version_number=0, current_next_indicator=1
		0x00, 0x00, // section_number, last_section_number
		0xE0, 0x31, // PCR_PID=0x31
		0xF0, descriptorPayloadLength + 2, // program_info_length
		0x05, descriptorPayloadLength, // registration descriptor
	}
	body = append(body, make([]byte, descriptorPayloadLength)...)
	sectionLength := len(body) + 4
	section := append([]byte{0x02, 0xB0 | byte(sectionLength>>8), byte(sectionLength)}, body...)
	checksum := crc32(section)
	return append(section, byte(checksum>>24), byte(checksum>>16), byte(checksum>>8), byte(checksum))
}

func fuzzPSIContinuationStream(packetSize int, pid uint16, section []byte) []byte {
	data := make([]byte, 2*packetSize)
	syncOffset := tsOffset(packetSize)
	first := syncOffset
	second := packetSize + syncOffset
	for i := first; i < first+tsPayloadSize; i++ {
		data[i] = 0xFF
	}
	for i := second; i < second+tsPayloadSize; i++ {
		data[i] = 0xFF
	}

	data[first] = 0x47
	data[first+1] = 0x40 | byte(pid>>8)
	data[first+2] = byte(pid)
	data[first+3] = 0x10
	data[first+4] = 0x00
	written := copy(data[first+5:first+tsPayloadSize], section)

	data[second] = 0x47
	data[second+1] = byte(pid >> 8)
	data[second+2] = byte(pid)
	data[second+3] = 0x11
	copy(data[second+4:second+tsPayloadSize], section[written:])
	return data
}

// FuzzSyncRecovery scans arbitrary bytes as 188-byte TS, 192-byte M2TS and
// 204-byte packets: every loss of synchronization is either recovered or ends the
// scan, and the scan never reads a skipped byte as a packet.
func FuzzSyncRecovery(f *testing.F) {
	stream := syncTestStream(nil, 6)
	f.Add(stream)
	f.Add(append(append([]byte{0x00}, stream...), 0x47))
	f.Add(syncTestStream(bytes.Repeat([]byte{0x47}, 400), 6))
	f.Add(fuzzPATPackets(tsPayloadSize + tpExtraHeaderSize))
	f.Add(withTestTrailers(stream))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*trailerPacketSize {
			return
		}
		for _, packetSize := range supportedPacketSizes {
			var pos int64
			scan, err := scanPrograms(bytes.NewReader(data), &pos, nil, newEmptyContinuitySummary(), newTransportTrustSummary(), nil, options.Options{}, packetSize, 0, true)
			if err != nil {
				continue
			}
			s := scan.sync
			lost := 0
			if s.Unrecovered {
				lost = 1
			}
			if s.Losses != s.Resyncs+lost || s.SkippedBytes < int64(s.Resyncs) || pos > int64(len(data)) ||
				s.SkippedBytes+int64(scan.truncatedBytes) > int64(len(data)) || (s.Unrecovered && pos != s.UnrecoveredOffset) {
				t.Fatalf("packet size %d: inconsistent scan %+v ending at %d", packetSize, scan, pos)
			}
		}
	})
}

// FuzzPacketLayout decodes any bytes as a 188-, 192- or 204-byte packet for
// --output packet and checks that its bit layout stays complete, ordered and
// consistent with the decoded fields (see checkLayout).
func FuzzPacketLayout(f *testing.F) {
	f.Add(byte(0), rawPsiPacket(0x1000, true, 0, append([]byte{0x00}, pmtSectionN(1, 0, 0, 0, 0x100, [2]uint16{0x02, 0x100})...)))
	f.Add(byte(1), append([]byte{0, 0, 0, 0}, pesStartPacket(0x100, 0, 90000)...))
	f.Add(byte(2), append(tsPacket([4]byte{0x47, 0x01, 0x00, 0x30}, 7, 0x10), bytes.Repeat([]byte{0x80}, trailerSize)...))
	f.Add(byte(0), tsPacket([4]byte{0x47, 0x41, 0x00, 0x10}, 0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0xFF, 0xFF))
	f.Add(byte(0), []byte{0x47})
	f.Fuzz(func(t *testing.T, size byte, data []byte) {
		packetSize := supportedPacketSizes[int(size)%len(supportedPacketSizes)]
		raw := make([]byte, packetSize)
		copy(raw, data)
		checkLayout(t, "fuzz", raw, decodePacketLayers(raw), false)
	})
}
