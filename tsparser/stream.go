package tsparser

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// ValidateOptions rejects option values that no output mode accepts.
func ValidateOptions(options options.Options) error {
	if options.Format != "" && options.Format != "text" && options.Format != "json" {
		return fmt.Errorf("unsupported output format %q (expected text or json)", options.Format)
	}
	if options.Progress != "" && options.Progress != "json" {
		return fmt.Errorf("unsupported progress format %q (expected json)", options.Progress)
	}
	if options.Offset < 0 {
		return fmt.Errorf("--offset must not be negative (got %d)", options.Offset)
	}
	if options.Limit < 0 {
		return fmt.Errorf("--limit must not be negative (got %d; 0 means no limit)", options.Limit)
	}
	if options.RangeOffset < 0 || options.RangeLimit < 0 {
		return errors.New("--range-offset and --range-limit must not be negative")
	}
	switch options.Output {
	case "", "summary":
	case "analysis", "packets", "packet":
		if options.Format != "json" {
			return fmt.Errorf("--output %s requires --format json", options.Output)
		}
	default:
		return fmt.Errorf("unsupported output %q (expected summary, analysis, packets or packet)", options.Output)
	}
	return nil
}

func ParseTsFile(filename string, options options.Options) error {
	if err := ValidateOptions(options); err != nil {
		return err
	}
	if options.Format == "" {
		options.Format = "text"
	}
	if options.Output == "packets" || options.Output == "packet" {
		return writePacketsDocument(filename, options, os.Stdout)
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

	reader, finish := withProgress(newBufferedReadSeeker(file), file, options)
	defer finish()
	return parseTsReader(reader, options)
}

func parseTsFileJSON(filename string, options options.Options) error {
	return parseTsFileJSONTo(filename, options, os.Stdout)
}

func parseTsFileJSONTo(filename string, options options.Options, output io.Writer) error {
	report := newAnalysisReport(filename, options.Offset, options.Limit)
	if options.Output == "analysis" {
		report.analysis = &rangeAnalysis{}
	}
	var parseErr error
	reason := "analysis_failed"
	if options.DumpHeader || options.DumpPayload || options.DumpAdaptationField || options.DumpPsi || options.DumpPesHeader || options.DumpTimestamp {
		parseErr = errors.New("--format json cannot be combined with packet-level dump flags")
		reason = "incompatible_options"
	} else if report.analysis != nil && (options.Program != 0 || options.ListPrograms || options.FailOnError) {
		parseErr = errors.New("--output analysis covers every program and cannot be combined with --program, --list-programs or --fail-on-error")
		reason = "incompatible_options"
	} else {
		file, err := os.Open(filename)
		if err != nil {
			parseErr = fmt.Errorf("file open error: %s: %w", filename, err)
			reason = "input_open_failed"
		} else {
			defer func() { _ = file.Close() }()
			if report.analysis != nil {
				report.analysis.fileSize = fileSize(file)
			}
			reader, finish := withProgress(newBufferedReadSeeker(file), file, options)
			parseErr = parseTsReaderReport(reader, options, report)
			finish()
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
	var document any = report
	if report.analysis != nil {
		document = rangeAnalysisReport(report)
	}
	if err := writeJSONReport(output, document); err != nil {
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

		// Collect every section of the PAT.
		pat, diagnostics, err := acquirePat(reader, &pos, options, packetSize, endOffset)
		reportPsiDiagnostics(report, diagnostics)
		if err != nil {
			reportWarning(report, pos, fmt.Sprintf("%s; retrying PAT discovery", err), fmt.Sprintf("%s, retrying PAT discovery...", err))
			fileOffset = maxInt64(pos, readStart+patOffset+int64(packetSize))
			continue
		}
		patDetected = true
		patPrograms := pat.Programs()
		programStart := pos
		// The first packet of the range, aligned with the packet that started
		// the PAT search.
		leadStart := options.Offset + (readStart+patOffset-options.Offset)%int64(packetSize)

		if report != nil && report.analysis != nil {
			return analyzeAllPrograms(reader, pat, leadStart, programStart, options, packetSize, endOffset, report)
		}
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

		// Collect every section of the selected program's PMT.
		pmt, diagnostics, err := acquirePmt(reader, &pos, pmtPid, selectedProgram, options, packetSize, endOffset)
		reportPsiDiagnostics(report, diagnostics)
		if err != nil {
			reportWarning(report, pos, fmt.Sprintf("%s; retrying PAT discovery", err), fmt.Sprintf("%s, retrying PAT discovery...", err))
			fileOffset = maxInt64(pos, readStart+patOffset+int64(packetSize))
			continue
		}
		programs := pmt.ProgramInfos()
		pcrPid := pmt.PcrPid()

		// Affected packets are counted over the whole range, including the
		// packets read while acquiring the PAT and PMT.
		trust := newTransportTrustSummary()
		if err = observeLead(reader, leadStart, pos, options, packetSize, trust.observeLead); err != nil {
			return err
		}
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
		tables := &trackedTables{pat: pat, programNumber: selectedProgram, pmt: pmt, trust: trust}
		analysis, err = bufferPes(reader, &pos, pmtPid, pcrPid, programs, tables, options, packetSize, endOffset, report != nil)
		if report != nil && analysis != nil {
			report.Programs = []ReportProgram{reportProgram(selectedProgram, pmtPid, pmt.Pmt)}
			report.Programs[0].Service = analysis.sdt.reportService(selectedProgram)
			report.SelectedProgramNumber = &selectedProgram
			report.Compliance = reportCompliance(analysis.compliance)
			report.ContinuityCounter = reportContinuity(analysis.continuity)
			report.TransportErrors, report.ScrambledPackets = reportTransportTrust(analysis.trust)
			report.TimestampAnomalies = reportAnomalies(analysis.anomalies)
			if analysis.jitter != nil {
				report.PCRJitter = reportJitter(analysis.jitter)
			}
			if analysis.bitrate != nil {
				report.Bitrate = reportBitrate(analysis.bitrate)
			}
			report.PSITransitions = reportPsiTransitions(analysis.transitions)
			reportPsiDiagnostics(report, analysis.psiWarnings)
			if analysis.truncatedBytes > 0 {
				report.Warnings = append(report.Warnings, ReportWarning{analysis.truncatedPos, truncatedPacketMessage(analysis.truncatedBytes)})
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
	sdt := findSdt(reader, start, options, packetSize, endOffset)
	for _, prog := range programs {
		pos := start
		_, _ = reader.Seek(pos, 0)
		pmt, diagnostics, err := acquirePmt(reader, &pos, prog.PmtPid, prog.ProgramNumber, options, packetSize, endOffset)
		reportPsiDiagnostics(report, diagnostics)
		if err != nil {
			if report != nil {
				r := reportProgram(prog.ProgramNumber, prog.PmtPid, nil)
				r.Service = sdt.reportService(prog.ProgramNumber)
				r.Reason = "pmt_parse_failed"
				r.Error = err.Error()
				report.Programs = append(report.Programs, r)
			} else {
				fmt.Printf("Program %d: PMT PID 0x%x (PMT parse error: %s)\n", prog.ProgramNumber, prog.PmtPid, err)
				printService(sdt, prog.ProgramNumber)
			}
			continue
		}
		if report != nil {
			r := reportProgram(prog.ProgramNumber, prog.PmtPid, pmt.Pmt)
			r.Service = sdt.reportService(prog.ProgramNumber)
			report.Programs = append(report.Programs, r)
		} else {
			fmt.Printf("Program %d: PMT PID 0x%x, PCR PID 0x%x\n", prog.ProgramNumber, prog.PmtPid, pmt.PcrPid())
			printService(sdt, prog.ProgramNumber)
			pmt.DumpProgramInfos(false)
		}
	}
}

// printService prints the SDT service of a listed program, if it is named.
func printService(sdt *sdtCollector, programNumber uint16) {
	if description := sdt.describeService(programNumber); description != "" {
		fmt.Println(description)
	}
}

// reportWarning records a warning in the JSON report, or prints it in text
// mode, where some messages are worded differently.
func reportWarning(report *AnalysisReport, pos int64, jsonMessage, textMessage string) {
	if report != nil {
		report.Warnings = append(report.Warnings, ReportWarning{pos, jsonMessage})
	} else {
		fmt.Printf("0x%08x %s\n", pos, textMessage)
	}
}

// reportPsiDiagnostics records the problems found while collecting a PAT or
// PMT as warnings.
func reportPsiDiagnostics(report *AnalysisReport, diagnostics []psiDiagnostic) {
	for _, d := range diagnostics {
		reportWarning(report, d.pos, d.message, d.message)
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
