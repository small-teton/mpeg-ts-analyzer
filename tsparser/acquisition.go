package tsparser

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// Initial acquisition diagnostics.
//
// Program analysis needs a packet alignment and a complete PAT. When the scan
// of the range ends without them, the analysis stops (it never guesses a
// program layout), and the failure reports which prerequisite was missing and
// the evidence gathered: the range, the alignment of each supported packet
// layout and, for an aligned range, what was seen on PID 0. The evidence is a
// fixed set of counters and offsets, whatever the length of the range.

// Top-level reasons of a failed initial acquisition.
const (
	syncNotAcquiredReason          = "sync_not_acquired"
	insufficientDataForSyncReason  = "insufficient_data_for_sync"
	patNotObservedReason           = "pat_not_observed"
	patNotAcquiredReason           = "pat_not_acquired"
	acquisitionEvidenceBufferBytes = 64 << 10
)

// ReportAcquisition is the evidence of a failed initial acquisition. The range
// is [range_start_offset_bytes, range_end_offset_bytes): the selected
// --offset/--limit range, clipped to the input.
type ReportAcquisition struct {
	RangeStartOffsetBytes int64                      `json:"range_start_offset_bytes"`
	RangeEndOffsetBytes   int64                      `json:"range_end_offset_bytes"`
	ScannedBytes          int64                      `json:"scanned_bytes"`
	Alignment             ReportAcquisitionAlignment `json:"alignment"`
	PAT                   *ReportAcquisitionPAT      `json:"pat,omitempty"`
}

// ReportAcquisitionAlignment is the packet alignment of the range. Status is
// acquired, not_acquired or insufficient_data. PacketSizeBytes and
// FirstAlignedOffsetBytes are present when it was acquired.
type ReportAcquisitionAlignment struct {
	Status                  string                  `json:"status"`
	PacketSizeBytes         int                     `json:"packet_size_bytes,omitempty"`
	FirstAlignedOffsetBytes *int64                  `json:"first_aligned_offset_bytes,omitempty"`
	AlignedPacketCount      int64                   `json:"aligned_packet_count"`
	Layouts                 []ReportAlignmentLayout `json:"layouts"`
}

// ReportAlignmentLayout is one supported packet layout: what the detector
// requires and whether it found it (found, not_found or insufficient_data).
type ReportAlignmentLayout struct {
	PacketSizeBytes   int    `json:"packet_size_bytes"`
	RequiredSyncBytes int    `json:"required_sync_bytes"`
	RequiredBytes     int64  `json:"required_bytes"`
	Status            string `json:"status"`
}

// ReportAcquisitionPAT is what the aligned packets of the range carried on
// PID 0. Status is not_observed or not_acquired. The problem counts follow
// psi_health.
type ReportAcquisitionPAT struct {
	Status                 string `json:"status"`
	PacketCount            int64  `json:"packet_count"`
	SectionStartCount      int64  `json:"section_start_count"`
	FirstPacketOffsetBytes *int64 `json:"first_packet_offset_bytes,omitempty"`
	CompleteTableCount     int    `json:"complete_table_count"`
	CRCErrorCount          int    `json:"crc_error_count"`
	MalformedSectionCount  int    `json:"malformed_section_count"`
	IncompleteCount        int    `json:"incomplete_count"`
	WrongTableIDCount      int    `json:"wrong_table_id_count"`
	UnusablePacketCount    int    `json:"unusable_packet_count"`
}

// acquisitionError is the error of a failed initial acquisition. Its message
// is the human-readable explanation; reason and report go to the JSON report.
type acquisitionError struct {
	reason  string
	message string
	report  *ReportAcquisition
}

func (e *acquisitionError) Error() string { return e.message }

// alignmentScan records, over every read of the main scan, which layouts the
// detector accepted.
type alignmentScan struct {
	found [3]bool // in supportedPacketSizes order
}

func (s *alignmentScan) observe(data []byte) {
	s.found[0] = s.found[0] || hasAlignment(data, tsPayloadSize)
	s.found[1] = s.found[1] || hasAlignment(data, m2tsPacketSize)
	s.found[2] = s.found[2] || hasTrailerAlignment(data)
}

func (s *alignmentScan) aligned() bool { return s.found[0] || s.found[1] || s.found[2] }

// alignmentLayouts describes the supported layouts as the detector checks
// them: three sync bytes for 188 and 192 bytes, strictSyncBytes for 204.
func (s *alignmentScan) layouts() []analysis.AlignmentLayout {
	layouts := make([]analysis.AlignmentLayout, len(supportedPacketSizes))
	for i, size := range supportedPacketSizes {
		layouts[i] = analysis.AlignmentLayout{PacketSize: size, SyncOffset: tsOffset(size), RequiredSyncBytes: alignmentSyncBytes(size), Found: s.found[i]}
	}
	return layouts
}

func alignmentSyncBytes(packetSize int) int {
	if packetSize == trailerPacketSize {
		return strictSyncBytes
	}
	return 3
}

// acquisitionPackets is what the evidence pass found in the aligned packets.
type acquisitionPackets struct {
	aligned       int64
	firstAligned  int64
	patPackets    int64
	sectionStarts int64
	firstPat      int64
	pat           analysis.PSITableResult
}

// observeAcquisition reads [start, end) once more, after the scan found no
// usable PAT, and counts the packets aligned at packetSize and those on PID 0.
// It searches, one byte at a time, for the first position at which the
// detector accepts the layout, keeps that alignment while every packet starts
// with a sync byte, and after a packet without one searches forward for a new
// alignment as synchronization recovery does (resyncSyncBytes sync bytes), so
// each byte is a candidate at most once. PID 0 is read with the same section reader, assembler and
// problem classification as psi_health. Memory is a fixed read buffer and
// one PAT assembler.
func observeAcquisition(reader io.ReadSeeker, start, end int64, packetSize int) (acquisitionPackets, error) {
	var found acquisitionPackets
	if _, err := reader.Seek(start, io.SeekStart); err != nil {
		return found, fmt.Errorf("file seek error: %w", err)
	}
	br := bufio.NewReaderSize(io.LimitReader(reader, end-start), acquisitionEvidenceBufferBytes)
	health := newPsiHealthCollector(nil, 0, 0)
	tsPacket := NewTsPacket()
	syncOffset := tsOffset(packetSize)
	syncs := alignmentSyncBytes(packetSize)
	pos, last := start, start
	synced := false
	for {
		if synced {
			packet, err := peek(br, packetSize)
			if err != nil {
				return found, err
			}
			if len(packet) < packetSize {
				break
			}
			if packet[syncOffset] == syncByteValue {
				if found.aligned == 0 {
					found.firstAligned = pos
				}
				found.aligned++
				last = pos
				observeAcquisitionPacket(&found, health, tsPacket, tsBytes(packet, packetSize), pos)
				_, _ = br.Discard(packetSize)
				pos += int64(packetSize)
				continue
			}
			// Recover as the analysis does after a loss of synchronization.
			syncs = resyncSyncBytes
		}
		window := syncOffset + (syncs-1)*packetSize + 1
		data, err := peek(br, window)
		if err != nil {
			return found, err
		}
		if len(data) < window {
			break
		}
		synced = alignedAt(data, packetSize, syncs)
		if !synced {
			_, _ = br.Discard(1)
			pos++
		}
	}
	if health.patReader.collecting && len(health.pat.sets) == 0 {
		// A section cut off by the end of the range never completed.
		health.check.ObserveNeverCompleted(analysis.PATTable, last)
	}
	found.pat = health.result().Tables[0]
	return found, nil
}

func observeAcquisitionPacket(found *acquisitionPackets, health *psiHealthCollector, tsPacket *TsPacket, data []byte, pos int64) {
	tsPacket.Initialize(pos, options.Options{})
	tsPacket.Append(data)
	if tsPacket.Parse() != nil {
		// An aligned packet whose header cannot be parsed carries nothing
		// usable; it still counts as aligned.
		return
	}
	if tsPacket.Pid() == patPidValue {
		if found.patPackets == 0 {
			found.firstPat = pos
		}
		found.patPackets++
		if tsPacket.PayloadUnitStartIndicator() {
			found.sectionStarts++
		}
	}
	health.observe(tsPacket, pos)
}

// peek returns up to n bytes; fewer means the range or the input ended.
func peek(br *bufio.Reader, n int) ([]byte, error) {
	data, err := br.Peek(n)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("file read error: %w", err)
	}
	return data, nil
}

// alignedAt reports whether data starts with syncs packets of packetSize that
// each start with a sync byte; a 204-byte alignment must also pass
// trailerAligned, as in detection. data holds the last required sync byte.
func alignedAt(data []byte, packetSize, syncs int) bool {
	if packetSize == trailerPacketSize {
		return trailerAligned(data, 0)
	}
	syncOffset := tsOffset(packetSize)
	for k := range syncs {
		if data[syncOffset+k*packetSize] != syncByteValue {
			return false
		}
	}
	return true
}

// acquisitionFailure explains why the scan of the range acquired no PAT. It
// is called once the whole range was read.
func acquisitionFailure(reader io.ReadSeeker, opts options.Options, scan *alignmentScan, packetSize int, endOffset int64) error {
	size, err := reader.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("file seek error: %w", err)
	}
	start := opts.Offset
	end := size
	if endOffset > 0 {
		end = min(end, endOffset)
	}
	end = max(end, start)
	evidence := analysis.AcquisitionEvidence{ScannedBytes: end - start, Layouts: scan.layouts()}
	var packets acquisitionPackets
	if scan.aligned() {
		if packets, err = observeAcquisition(reader, start, end, packetSize); err != nil {
			return err
		}
		evidence.PATPackets = packets.patPackets
	}
	result := analysis.ClassifyAcquisition(evidence)
	report := &ReportAcquisition{RangeStartOffsetBytes: start, RangeEndOffsetBytes: end, ScannedBytes: end - start}
	report.Alignment.Layouts = []ReportAlignmentLayout{}
	for _, layout := range result.Layouts {
		report.Alignment.Layouts = append(report.Alignment.Layouts, ReportAlignmentLayout{
			PacketSizeBytes: layout.PacketSize, RequiredSyncBytes: layout.RequiredSyncBytes,
			RequiredBytes: layout.RequiredBytes(), Status: alignmentStatusNames[layout.Status],
		})
	}
	failure := &acquisitionError{report: report}
	switch result.Outcome {
	case analysis.AcquisitionSyncNotAcquired:
		report.Alignment.Status = "not_acquired"
		failure.reason = syncNotAcquiredReason
		failure.message = fmt.Sprintf("no TS synchronization found: no 188-byte, 192-byte or 204-byte packet alignment (three sync bytes one packet apart, five for 204 bytes) in the %d bytes %s; the range may not hold MPEG-2 TS packets of a supported size, or may be damaged", end-start, rangeText(start, end))
	case analysis.AcquisitionSyncInsufficientData:
		report.Alignment.Status = "insufficient_data"
		failure.reason = insufficientDataForSyncReason
		failure.message = shortRangeMessage(start, end, result.Layouts)
	default:
		report.Alignment.Status = "acquired"
		report.Alignment.PacketSizeBytes = packetSize
		report.Alignment.AlignedPacketCount = packets.aligned
		if packets.aligned > 0 {
			report.Alignment.FirstAlignedOffsetBytes = &packets.firstAligned
		}
		report.PAT = reportAcquisitionPAT(packets)
		failure.reason, failure.message = patFailure(packets, report, opts, packetSize)
		if result.Outcome == analysis.AcquisitionPATNotObserved {
			report.PAT.Status = "not_observed"
		}
	}
	return failure
}

var alignmentStatusNames = map[analysis.AlignmentStatus]string{
	analysis.AlignmentFound:            "found",
	analysis.AlignmentNotFound:         "not_found",
	analysis.AlignmentInsufficientData: "insufficient_data",
}

func rangeText(start, end int64) string {
	return fmt.Sprintf("from 0x%08x to 0x%08x", start, end)
}

// shortRangeMessage explains a range too short to show at least one layout.
func shortRangeMessage(start, end int64, layouts []analysis.LayoutResult) string {
	var checked, short []string
	for _, layout := range layouts {
		if layout.Status == analysis.AlignmentInsufficientData {
			short = append(short, fmt.Sprintf("%d-byte packets need %d bytes (%d sync bytes one packet apart)", layout.PacketSize, layout.RequiredBytes(), layout.RequiredSyncBytes))
		} else {
			checked = append(checked, fmt.Sprintf("%d-byte", layout.PacketSize))
		}
	}
	message := fmt.Sprintf("no TS synchronization found: the range %s holds only %d bytes, too few to establish a packet alignment: %s", rangeText(start, end), end-start, strings.Join(short, ", "))
	if len(checked) > 0 {
		message += fmt.Sprintf("; no %s alignment was found", strings.Join(checked, " or "))
	}
	return message + "; to inspect a short fragment packet by packet, without program analysis, use --format json --output packets with --packet-size 188, 192 or 204"
}

// countText is n followed by noun, made plural unless n is 1.
func countText(n int64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func reportAcquisitionPAT(p acquisitionPackets) *ReportAcquisitionPAT {
	t := p.pat
	r := &ReportAcquisitionPAT{
		Status: "not_acquired", PacketCount: p.patPackets, SectionStartCount: p.sectionStarts, CompleteTableCount: t.Instances,
		CRCErrorCount: t.Problems[analysis.PSICRCError], MalformedSectionCount: t.Problems[analysis.PSIMalformed],
		IncompleteCount: t.Problems[analysis.PSIIncomplete], WrongTableIDCount: t.Problems[analysis.PSIWrongTableID],
		UnusablePacketCount: t.Problems[analysis.PSIUnusable],
	}
	if p.patPackets > 0 {
		r.FirstPacketOffsetBytes = &p.firstPat
	}
	return r
}

// patFailure returns the reason and explanation of an aligned range without a
// usable PAT. What is known is stated as fact; why the PAT is missing is
// given as possibilities.
func patFailure(p acquisitionPackets, r *ReportAcquisition, opts options.Options, packetSize int) (string, string) {
	message := fmt.Sprintf("PAT not detected: no complete PAT after scanning %s (%d bytes) in the range %s", countText(p.aligned, fmt.Sprintf("aligned %d-byte packet", packetSize)), p.aligned*int64(packetSize), rangeText(r.RangeStartOffsetBytes, r.RangeEndOffsetBytes))
	widen := ""
	if opts.Offset > 0 || opts.Limit > 0 {
		widen = "; a wider --offset/--limit range may include one"
	}
	const stop = "; program analysis needs a PAT and stops here"
	if p.patPackets == 0 {
		return patNotObservedReason, message + "; no packet is on PID 0x0000. The range may be a partial capture or a PSI/PES fragment, or may end before the next PAT repetition" + widen + stop
	}
	pat := r.PAT
	var causes []string
	for _, c := range []struct {
		count int
		text  string
	}{
		{pat.UnusablePacketCount, "with transport_error_indicator or a scrambled payload"},
		{pat.CRCErrorCount, "with an invalid CRC_32"},
		{pat.MalformedSectionCount, "malformed"},
		{pat.WrongTableIDCount, "with a table_id other than 0x00"},
		{pat.IncompleteCount, "incomplete"},
	} {
		if c.count > 0 {
			causes = append(causes, fmt.Sprintf("%d %s", c.count, c.text))
		}
	}
	message += fmt.Sprintf("; %s on PID 0x0000, %d starting a section", countText(p.patPackets, "packet"), p.sectionStarts)
	switch {
	case pat.CompleteTableCount > 0:
		message += fmt.Sprintf("; %d complete PAT(s) were assembled, but PAT discovery could not start from it (it needs the PAT packet followed by two aligned packets)", pat.CompleteTableCount)
	case len(causes) > 0:
		message += "; problems (sections or packets): " + strings.Join(causes, ", ") + ". The PAT may be damaged or cut off by the range"
	case p.sectionStarts == 0:
		message += ". The range may hold only the middle of a PAT section"
	default:
		message += ". No usable PAT section was assembled"
	}
	return patNotAcquiredReason, message + widen + stop
}
