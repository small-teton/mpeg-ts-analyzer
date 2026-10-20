package tsparser

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

func ParseTsFile(filename string, options options.Options) error {
	if options.Format == "" {
		options.Format = "text"
	}
	if options.Format != "text" && options.Format != "json" {
		return fmt.Errorf("unsupported output format %q (expected text or json)", options.Format)
	}
	if options.Format == "json" {
		return parseTsFileJSON(filename, options)
	}
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("file open error: %s: %w", filename, err)
	}
	defer func() { _ = file.Close() }()
	fmt.Println("Input file: ", filename)

	return parseTsReader(file, options)
}

func parseTsFileJSON(filename string, options options.Options) error {
	return parseTsFileJSONTo(filename, options, os.Stdout)
}

func parseTsFileJSONTo(filename string, options options.Options, output io.Writer) error {
	report := newAnalysisReport(filename, options.Offset, options.Limit)
	var parseErr error
	reason := "analysis_failed"
	if options.DumpHeader || options.DumpPayload || options.DumpAdaptationField || options.DumpPsi || options.DumpPesHeader || options.DumpTimestamp {
		parseErr = errors.New("--format json cannot be combined with packet-level dump flags")
		reason = "incompatible_options"
	} else {
		file, err := os.Open(filename)
		if err != nil {
			parseErr = fmt.Errorf("file open error: %s: %w", filename, err)
			reason = "input_open_failed"
		} else {
			defer func() { _ = file.Close() }()
			parseErr = parseTsReaderReport(file, options, report)
		}
	}
	if parseErr != nil {
		var complianceErr *ComplianceError
		if !errors.As(parseErr, &complianceErr) {
			report.Status = "failed"
			report.Reason = reason
			report.Error = parseErr.Error()
		}
	}
	if err := writeJSONReport(output, report); err != nil {
		if parseErr != nil {
			return errors.Join(parseErr, err)
		}
		return fmt.Errorf("write JSON output: %w", err)
	}
	return parseErr
}

func parseTsReader(reader io.ReadSeeker, options options.Options) error {
	return parseTsReaderReport(reader, options, nil)
}

func parseTsReaderReport(reader io.ReadSeeker, options options.Options, report *AnalysisReport) error {
	const patPid = 0x0
	const bufSize = 65536
	var fileOffset int64
	patDetected := false
	buf := make([]byte, bufSize)
	packetSize := 0

	if options.Offset > 0 {
		if _, err := reader.Seek(options.Offset, io.SeekStart); err != nil {
			return fmt.Errorf("file seek error: %w", err)
		}
		fileOffset = options.Offset
	}

	var endOffset int64
	if options.Limit > 0 {
		endOffset = fileOffset + options.Limit
	}

	for {
		readStart := fileOffset
		readBuf := buf
		if endOffset > 0 && fileOffset+int64(len(buf)) > endOffset {
			remaining := endOffset - fileOffset
			if remaining <= 0 {
				break
			}
			readBuf = buf[:remaining]
		}
		size, err := reader.Read(readBuf)
		if err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("file read error: %w", err)
		}
		fileOffset += int64(size)

		if packetSize == 0 {
			packetSize = detectPacketSize(buf[:size])
			if report != nil {
				report.Input.PacketSizeBytes = packetSize
			} else {
				fmt.Printf("Packet size: %d bytes\n", packetSize)
			}
		}

		patOffset, err := findPat(buf[:size], packetSize)
		if err != nil {
			continue
		}

		pos := readStart + patOffset
		if _, err = reader.Seek(pos, 0); err != nil {
			return fmt.Errorf("file seek error: %w", err)
		}

		// Parse PAT
		pat := NewPat()
		if err = BufferPsi(reader, &pos, patPid, pat, options, packetSize, endOffset); err != nil {
			message := fmt.Sprintf("PAT buffering error: %s; retrying PAT discovery", err)
			if report != nil {
				report.Warnings = append(report.Warnings, ReportWarning{pos, message})
			} else {
				fmt.Printf("0x%08x PAT buffering error: %s, retrying PAT discovery...\n", pos, err)
			}
			fileOffset = maxInt64(pos, readStart+patOffset+int64(packetSize))
			continue
		}
		if err = pat.Parse(); err != nil {
			message := fmt.Sprintf("PAT parse error: %s; retrying PAT discovery", err)
			if report != nil {
				report.Warnings = append(report.Warnings, ReportWarning{pos, message})
			} else {
				fmt.Printf("0x%08x PAT parse error: %s, retrying PAT discovery...\n", pos, err)
			}
			fileOffset = pos
			continue
		}
		patDetected = true
		patPrograms := pat.Programs()
		programStart := pos

		if _, err = reader.Seek(pos, 0); err != nil {
			return fmt.Errorf("file seek error: %w", err)
		}
		if report == nil {
			fmt.Printf("Detected PAT: %d program(s)\n", len(patPrograms))
		}
		if report == nil && options.DumpPsi {
			pat.Dump()
		}

		// A single-program stream is analyzed directly. A multi-program stream
		// (e.g. a raw broadcast capture: main service + 1seg) is listed instead of
		// picking one arbitrarily — the user selects one with --program. Both cases
		// only parse PMTs here (cheap; no PES), so listing stays lightweight.
		if options.ListPrograms || (len(patPrograms) > 1 && options.Program == 0) {
			if report == nil && !options.ListPrograms {
				fmt.Println("Multiple programs detected; pass --program <program_number> to analyze one:")
			}
			listPrograms(reader, patPrograms, programStart, options, packetSize, endOffset, report)
			if report != nil {
				report.Status = "programs_listed"
				if options.ListPrograms {
					report.Reason = "program_listing_requested"
				} else {
					report.Reason = "program_selection_required"
				}
			}
			return nil
		}

		if len(patPrograms) == 0 {
			message := "PAT has no program; retrying PAT discovery"
			if report != nil {
				report.Warnings = append(report.Warnings, ReportWarning{pos, message})
			} else {
				fmt.Printf("0x%08x PAT has no program, retrying PAT discovery...\n", pos)
			}
			fileOffset = pos
			continue
		}
		pmtPid := patPrograms[0].PmtPid
		selectedProgram := patPrograms[0].ProgramNumber
		if options.Program > 0 {
			found := false
			for _, prog := range patPrograms {
				if int(prog.ProgramNumber) == options.Program {
					pmtPid = prog.PmtPid
					selectedProgram = prog.ProgramNumber
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("program %d not found in PAT", options.Program)
			}
		}

		// Parse PMT
		pmt := NewPmt()
		if err = BufferPsi(reader, &pos, pmtPid, pmt, options, packetSize, endOffset); err != nil {
			message := fmt.Sprintf("PMT buffering error: %s; retrying PAT discovery", err)
			if report != nil {
				report.Warnings = append(report.Warnings, ReportWarning{pos, message})
			} else {
				fmt.Printf("0x%08x PMT buffering error: %s, retrying PAT discovery...\n", pos, err)
			}
			fileOffset = maxInt64(pos, readStart+patOffset+int64(packetSize))
			continue
		}
		if err = pmt.Parse(); err != nil {
			message := fmt.Sprintf("PMT parse error: %s; retrying PAT discovery", err)
			if report != nil {
				report.Warnings = append(report.Warnings, ReportWarning{pos, message})
			} else {
				fmt.Printf("0x%08x PMT parse error: %s, retrying PAT discovery...\n", pos, err)
			}
			fileOffset = pos
			continue
		}
		programs := pmt.ProgramInfos()
		pcrPid := pmt.PcrPid()

		if _, err = reader.Seek(pos, 0); err != nil {
			return fmt.Errorf("file seek error: %w", err)
		}
		if report == nil {
			fmt.Println("Detected PMT")
		}
		if report == nil && options.DumpPsi {
			pmt.Dump()
		} else if report == nil {
			pmt.DumpProgramInfos(false)
		}

		var analysis *pesAnalysis
		analysis, err = bufferPes(reader, &pos, pmtPid, pcrPid, programs, options, packetSize, endOffset, report != nil)
		if report != nil && analysis != nil {
			report.Programs = []ReportProgram{reportProgram(selectedProgram, pmtPid, pmt)}
			report.SelectedProgramNumber = &selectedProgram
			report.Compliance = reportCompliance(analysis.compliance)
			report.ContinuityCounter = reportContinuity(analysis.continuity)
			report.TimestampAnomalies = reportAnomalies(analysis.anomalies)
			if analysis.jitter != nil {
				report.PCRJitter = reportJitter(analysis.jitter)
			}
			if analysis.bitrate != nil {
				report.Bitrate = reportBitrate(analysis.bitrate)
			}
			report.Status = "complete"
			report.Reason = ""
			report.Error = ""
		}
		if err != nil {
			var complianceErr *ComplianceError
			if errors.As(err, &complianceErr) {
				return err
			}
			message := fmt.Sprintf("PES parse error: %s; retrying PAT discovery", err)
			if report != nil {
				report.Warnings = append(report.Warnings, ReportWarning{pos, message})
			} else {
				fmt.Printf("0x%08x PES parse error: %s, retrying PAT discovery...\n", pos, err)
			}
			fileOffset = maxInt64(pos, readStart+patOffset+int64(packetSize))
			continue
		}
		return nil
	}
	// Reaching EOF is fine once a PAT was seen (the tool degrades gracefully on
	// later errors); but a stream with no PAT at all is not a transport stream.
	if !patDetected {
		return errors.New("no valid transport stream found (PAT not detected)")
	}
	if report != nil {
		report.Status = "incomplete"
		report.Reason = "end_of_input_before_complete_analysis"
	}
	return nil
}

// listPrograms parses each program's PMT (no PES analysis, so it only reads far
// enough to find each PMT) and prints a summary of what the stream contains.
func listPrograms(reader io.ReadSeeker, programs []Program, start int64, options options.Options, packetSize int, endOffset int64, report *AnalysisReport) {
	for _, prog := range programs {
		pos := start
		_, _ = reader.Seek(pos, 0)
		pmt := NewPmt()
		_ = BufferPsi(reader, &pos, prog.PmtPid, pmt, options, packetSize, endOffset)
		if err := pmt.Parse(); err != nil {
			if report != nil {
				r := reportProgram(prog.ProgramNumber, prog.PmtPid, nil)
				r.Reason = "pmt_parse_failed"
				r.Error = err.Error()
				report.Programs = append(report.Programs, r)
			} else {
				fmt.Printf("Program %d: PMT PID 0x%x (PMT parse error: %s)\n", prog.ProgramNumber, prog.PmtPid, err)
			}
			continue
		}
		if report != nil {
			report.Programs = append(report.Programs, reportProgram(prog.ProgramNumber, prog.PmtPid, pmt))
		} else {
			fmt.Printf("Program %d: PMT PID 0x%x, PCR PID 0x%x\n", prog.ProgramNumber, prog.PmtPid, pmt.PcrPid())
			pmt.DumpProgramInfos(false)
		}
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// detectPacketSize detects whether the stream uses 188-byte or 192-byte packets.
func detectPacketSize(data []byte) int {
	for _, candidate := range []int{tsPayloadSize, tsPayloadSize + tpExtraHeaderSize} {
		syncOffset := candidate - tsPayloadSize // 0 for 188, 4 for 192
		if len(data) < syncOffset+candidate*2+1 {
			continue
		}
		for i := 0; i+syncOffset+candidate*2 < len(data); i++ {
			if data[i+syncOffset] == 0x47 &&
				data[i+syncOffset+candidate] == 0x47 &&
				data[i+syncOffset+candidate*2] == 0x47 {
				return candidate
			}
		}
	}
	return tsPayloadSize
}

// findPat finds the first PAT packet in the data with the given packet size.
func findPat(data []byte, packetSize int) (int64, error) {
	syncOffset := packetSize - tsPayloadSize // 0 for 188, 4 for 192
	for i := 0; i+syncOffset+packetSize*2 < len(data); i++ {
		sync0 := i + syncOffset
		sync1 := sync0 + packetSize
		sync2 := sync1 + packetSize
		if data[sync0] == 0x47 && data[sync1] == 0x47 && data[sync2] == 0x47 {
			if (data[sync0+1]&0x5F) == 0x40 && data[sync0+2] == 0x00 {
				return int64(i), nil
			}
		}
	}
	return 0, errors.New("cannot find pat")
}
