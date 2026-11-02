package tsparser

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// packetsSchemaVersion versions the documents written by --output packets and
// --output packet.
const packetsSchemaVersion = "1.0"

// maxPageCount bounds --count so a page stays small.
const maxPageCount = 10000

// PacketsInput describes the input of a packet-level document.
type PacketsInput struct {
	Path            string `json:"path"`
	FileSizeBytes   int64  `json:"file_size_bytes"`
	PacketSizeBytes int    `json:"packet_size_bytes,omitempty"`
	OffsetBytes     int64  `json:"offset_bytes"`
}

// PacketsEnvelope is shared by --output packets and --output packet, with the
// same meaning as in the summary report.
type PacketsEnvelope struct {
	SchemaVersion string       `json:"schema_version"`
	Output        string       `json:"output"`
	Input         PacketsInput `json:"input"`
	Status        string       `json:"status"`
	Reason        string       `json:"reason,omitempty"`
	Error         string       `json:"error,omitempty"`
}

// PacketsPage is the document written by --output packets: up to --count
// packets from --offset, read without PAT/PMT context.
type PacketsPage struct {
	PacketsEnvelope
	FirstOffsetBytes *int64          `json:"first_offset_bytes,omitempty"`
	NextOffsetBytes  *int64          `json:"next_offset_bytes,omitempty"`
	EndOfFile        bool            `json:"end_of_file"`
	TotalPackets     int64           `json:"total_packets"`
	Packets          []PacketSummary `json:"packets"`
	Warnings         []ReportWarning `json:"warnings"`
}

// PacketSummary holds the light per-packet fields of a page.
type PacketSummary struct {
	OffsetBytes int64 `json:"offset_bytes"`
	// Valid is false when the packet could not be parsed (no sync byte); the
	// other fields are then absent or zero.
	Valid                  bool     `json:"valid"`
	PID                    string   `json:"pid,omitempty"`
	PUSI                   bool     `json:"pusi"`
	TransportError         bool     `json:"transport_error"`
	ScramblingControl      uint8    `json:"scrambling_control"`
	AdaptationFieldControl uint8    `json:"adaptation_field_control"`
	ContinuityCounter      uint8    `json:"continuity_counter"`
	PCRMs                  *float64 `json:"pcr_ms,omitempty"`
	// Sections lists the long-form PSI sections that start in this packet.
	Sections []PacketSection `json:"sections,omitempty"`
}

// PacketSection is the header of a long-form section (PAT, PMT and other
// tables with section_syntax_indicator 1) that starts in a packet, as the
// packet's own bytes give it. The section is not checked further: its CRC32
// may lie in a later packet.
type PacketSection struct {
	TableID              string `json:"table_id"`
	TableIDExtension     uint16 `json:"table_id_extension"`
	VersionNumber        uint8  `json:"version_number"`
	CurrentNextIndicator uint8  `json:"current_next_indicator"`
	SectionNumber        uint8  `json:"section_number"`
	LastSectionNumber    uint8  `json:"last_section_number"`
}

// packetFile gives random access to the packets of an input file.
type packetFile struct {
	r          io.ReaderAt
	size       int64
	packetSize int
	// phase is the offset of the first packet boundary (0 ≤ phase < packetSize).
	phase int64
}

func checkPacketSizeHint(hint int) error {
	if hint != 0 && hint != tsPayloadSize && hint != tsPayloadSize+tpExtraHeaderSize {
		return fmt.Errorf("unsupported packet size %d (expected 188 or 192)", hint)
	}
	return nil
}

// openPacketFile detects the packet size and packet boundaries around offset,
// which may be the end of a non-empty file. hint forces the packet size when
// it is 188 or 192.
func openPacketFile(r io.ReaderAt, size, offset int64, hint int) (*packetFile, error) {
	if err := checkPacketSizeHint(hint); err != nil {
		return nil, err
	}
	if offset < 0 || offset > size || size == 0 {
		return nil, fmt.Errorf("offset %d is outside the file (size %d)", offset, size)
	}
	// Near the end of the file, probe the packets before offset as well, so
	// that the packet size is detected from several sync bytes.
	const probe = 65536
	probeOffset := max(0, min(offset, size-probe))
	buf := make([]byte, min(probe, size-probeOffset))
	n, err := r.ReadAt(buf, probeOffset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("file read error: %w", err)
	}
	buf = buf[:n]
	packetSize := hint
	if packetSize == 0 {
		packetSize = detectPacketSize(buf)
	}
	start, ok := findSync(buf, packetSize)
	if !ok {
		return nil, fmt.Errorf("no TS sync byte found from offset %d", probeOffset)
	}
	first := probeOffset + int64(start)
	return &packetFile{r: r, size: size, packetSize: packetSize, phase: first % int64(packetSize)}, nil
}

// findSync returns the first position in data where packets of packetSize
// line up on sync bytes. It requires up to three consecutive sync bytes, fewer
// when data ends sooner.
func findSync(data []byte, packetSize int) (int, bool) {
	syncOffset := packetSize - tsPayloadSize
	for i := 0; i+syncOffset < len(data); i++ {
		ok := true
		for k := 0; k < 3; k++ {
			j := i + syncOffset + k*packetSize
			if j >= len(data) {
				break
			}
			if data[j] != 0x47 {
				ok = false
				break
			}
		}
		if ok {
			return i, true
		}
	}
	return 0, false
}

// alignUp returns the first packet boundary at or after offset.
func (f *packetFile) alignUp(offset int64) int64 {
	ps := int64(f.packetSize)
	rem := (offset - f.phase) % ps
	if rem < 0 {
		rem += ps
	}
	if rem == 0 {
		return offset
	}
	return offset + ps - rem
}

// packetCount is the number of complete packets in the file.
func (f *packetFile) packetCount() int64 {
	return (f.size - f.phase) / int64(f.packetSize)
}

// readPacket reads the packet at offset. It returns io.ErrUnexpectedEOF for a
// truncated last packet and io.EOF at the end of the file.
func (f *packetFile) readPacket(offset int64) ([]byte, error) {
	if offset >= f.size {
		return nil, io.EOF
	}
	buf := make([]byte, f.packetSize)
	n, err := f.r.ReadAt(buf, offset)
	if n == f.packetSize {
		return buf, nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		return buf[:n], io.ErrUnexpectedEOF
	}
	return nil, fmt.Errorf("file read error: %w", err)
}

// parsePacket parses the TS header and adaptation field of raw packet bytes.
func (f *packetFile) parsePacket(raw []byte, offset int64) (*TsPacket, error) {
	tp := NewTsPacket()
	tp.Initialize(offset, options.Options{})
	tp.Append(raw[f.packetSize-tsPayloadSize:])
	if err := tp.Parse(); err != nil {
		return nil, err
	}
	return tp, nil
}

func packetSummary(tp *TsPacket, offset int64) PacketSummary {
	s := PacketSummary{
		OffsetBytes:            offset,
		Valid:                  true,
		PID:                    reportPID(tp.Pid()),
		PUSI:                   tp.PayloadUnitStartIndicator(),
		TransportError:         tp.transportErrorIndicator == 1,
		ScramblingControl:      tp.transportScramblingControl,
		AdaptationFieldControl: tp.adaptationFieldControl,
		ContinuityCounter:      tp.ContinuityCounter(),
	}
	if tp.HasAf() && tp.adaptationField.PcrFlag() {
		s.PCRMs = optionalFloat(float64(tp.Pcr())/float64(pcrTicksPerMs), true)
	}
	s.Sections = packetSectionStarts(tp)
	return s
}

// packetSectionStarts reads the headers of the sections that start in a
// payload_unit_start packet: from the pointer_field on, each section whose
// first 8 bytes are in the packet and whose section_syntax_indicator is set,
// until stuffing (0xFF), a short-form section, or the end of the packet. A
// PES packet never qualifies: its payload starts with 0x00 0x00 0x01. Packets
// with a transport error or a scrambled payload are skipped.
func packetSectionStarts(tp *TsPacket) []PacketSection {
	payload := tp.Payload()
	if !tp.PayloadUnitStartIndicator() || tp.transportErrorIndicator != 0 || tp.transportScramblingControl != 0 || len(payload) == 0 || 1+int(payload[0]) > len(payload) {
		return nil
	}
	var sections []PacketSection
	for rest := payload[1+int(payload[0]):]; len(rest) >= 8 && rest[0] != 0xFF && rest[1]&0x80 != 0; {
		sections = append(sections, PacketSection{
			TableID: fmt.Sprintf("0x%02x", rest[0]), TableIDExtension: uint16(rest[3])<<8 | uint16(rest[4]),
			VersionNumber: rest[5] >> 1 & 0x1F, CurrentNextIndicator: rest[5] & 0x01, SectionNumber: rest[6], LastSectionNumber: rest[7],
		})
		size, _ := psiSectionSize(rest)
		if size >= len(rest) {
			break
		}
		rest = rest[size:]
	}
	return sections
}

// readPacketsPage reads up to count packets starting at the first packet
// boundary at or after offset.
func readPacketsPage(r io.ReaderAt, size int64, opts options.Options, page *PacketsPage) error {
	if opts.Count <= 0 || opts.Count > maxPageCount {
		return fmt.Errorf("--count must be between 1 and %d", maxPageCount)
	}
	if size == 0 && opts.Offset == 0 {
		if err := checkPacketSizeHint(opts.PacketSize); err != nil {
			return err
		}
		page.EndOfFile = true
		page.Status = "complete"
		return nil
	}
	f, err := openPacketFile(r, size, opts.Offset, opts.PacketSize)
	if err != nil {
		return err
	}
	page.Input.PacketSizeBytes = f.packetSize
	page.TotalPackets = f.packetCount()
	offset := f.alignUp(opts.Offset)
	if offset < size {
		first := offset
		page.FirstOffsetBytes = &first
	}
	for len(page.Packets) < opts.Count {
		raw, err := f.readPacket(offset)
		if errors.Is(err, io.EOF) {
			page.EndOfFile = true
			break
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			page.EndOfFile = true
			page.Warnings = append(page.Warnings, ReportWarning{offset, truncatedPacketMessage(len(raw))})
			break
		}
		if err != nil {
			return err
		}
		tp, perr := f.parsePacket(raw, offset)
		if perr != nil {
			page.Packets = append(page.Packets, PacketSummary{OffsetBytes: offset})
		} else {
			page.Packets = append(page.Packets, packetSummary(tp, offset))
		}
		offset += int64(f.packetSize)
	}
	if !page.EndOfFile {
		if offset >= size {
			page.EndOfFile = true
		} else {
			next := offset
			page.NextOffsetBytes = &next
		}
	}
	page.Status = "complete"
	return nil
}

// writePacketsDocument runs --output packets or --output packet on filename.
func writePacketsDocument(filename string, opts options.Options, output io.Writer) error {
	envelope := PacketsEnvelope{
		SchemaVersion: packetsSchemaVersion,
		Output:        opts.Output,
		Input:         PacketsInput{Path: filename, OffsetBytes: opts.Offset},
		Status:        "failed",
	}
	var document any
	var header *PacketsEnvelope
	var run func(io.ReaderAt, int64) error
	if opts.Output == "packets" {
		page := &PacketsPage{PacketsEnvelope: envelope, Packets: []PacketSummary{}, Warnings: []ReportWarning{}}
		document, header = page, &page.PacketsEnvelope
		run = func(r io.ReaderAt, size int64) error { return readPacketsPage(r, size, opts, page) }
	} else {
		detail := &PacketDetail{PacketsEnvelope: envelope, Warnings: []ReportWarning{}}
		document, header = detail, &detail.PacketsEnvelope
		run = func(r io.ReaderAt, size int64) error { return readPacketDetail(r, size, opts, detail) }
	}

	var runErr error
	reason := "analysis_failed"
	file, err := os.Open(filename)
	if err != nil {
		runErr = fmt.Errorf("file open error: %s: %w", filename, err)
		reason = "input_open_failed"
	} else {
		defer func() { _ = file.Close() }()
		header.Input.FileSizeBytes = fileSize(file)
		runErr = run(file, header.Input.FileSizeBytes)
	}
	if runErr != nil {
		header.Status, header.Reason, header.Error = "failed", reason, runErr.Error()
	}
	if err := writeJSONReport(output, document); err != nil {
		return errors.Join(runErr, err)
	}
	return runErr
}
