package tsparser

import (
	"fmt"
	"io"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

func ParseTsFile(filename string, options options.Options) error {
	file, err := os.Open(filename)
	if err != nil {
		return errors.WithMessagef(err, "file open error: %s", filename)
	}
	defer func() { _ = file.Close() }()
	fmt.Println("Input file: ", filename)

	return parseTsReader(file, options)
}

func parseTsReader(reader io.ReadSeeker, options options.Options) error {
	const patPid = 0x0
	const bufSize = 65536
	var fileOffset int64
	patDetected := false
	buf := make([]byte, bufSize)
	packetSize := 0

	if options.Offset > 0 {
		if _, err := reader.Seek(options.Offset, io.SeekStart); err != nil {
			return errors.Wrap(err, "file seek error")
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
			return errors.Wrap(err, "file read error")
		}
		fileOffset += int64(size)

		if packetSize == 0 {
			packetSize = detectPacketSize(buf[:size])
			fmt.Printf("Packet size: %d bytes\n", packetSize)
		}

		patOffset, err := findPat(buf[:size], packetSize)
		if err != nil {
			continue
		}

		pos := readStart + patOffset
		if _, err = reader.Seek(pos, 0); err != nil {
			return errors.Wrap(err, "file seek error")
		}

		// Parse PAT
		pat := NewPat()
		if err = BufferPsi(reader, &pos, patPid, pat, options, packetSize, endOffset); err != nil {
			fmt.Printf("0x%08x PAT buffering error: %s, retrying PAT discovery...\n", pos, err)
			fileOffset = maxInt64(pos, readStart+patOffset+int64(packetSize))
			continue
		}
		if err = pat.Parse(); err != nil {
			fmt.Printf("0x%08x PAT parse error: %s, retrying PAT discovery...\n", pos, err)
			fileOffset = pos
			continue
		}
		patDetected = true
		patPrograms := pat.Programs()
		programStart := pos

		if _, err = reader.Seek(pos, 0); err != nil {
			return errors.Wrap(err, "file seek error")
		}
		fmt.Printf("Detected PAT: %d program(s)\n", len(patPrograms))
		if options.DumpPsi {
			pat.Dump()
		}

		// A single-program stream is analyzed directly. A multi-program stream
		// (e.g. a raw broadcast capture: main service + 1seg) is listed instead of
		// picking one arbitrarily — the user selects one with --program. Both cases
		// only parse PMTs here (cheap; no PES), so listing stays lightweight.
		if options.ListPrograms || (len(patPrograms) > 1 && options.Program == 0) {
			if !options.ListPrograms {
				fmt.Println("Multiple programs detected; pass --program <program_number> to analyze one:")
			}
			listPrograms(reader, patPrograms, programStart, options, packetSize, endOffset)
			return nil
		}

		if len(patPrograms) == 0 {
			fmt.Printf("0x%08x PAT has no program, retrying PAT discovery...\n", pos)
			fileOffset = pos
			continue
		}
		pmtPid := patPrograms[0].PmtPid
		if options.Program > 0 {
			found := false
			for _, prog := range patPrograms {
				if int(prog.ProgramNumber) == options.Program {
					pmtPid = prog.PmtPid
					found = true
					break
				}
			}
			if !found {
				return errors.Newf("program %d not found in PAT", options.Program)
			}
		}

		// Parse PMT
		pmt := NewPmt()
		if err = BufferPsi(reader, &pos, pmtPid, pmt, options, packetSize, endOffset); err != nil {
			fmt.Printf("0x%08x PMT buffering error: %s, retrying PAT discovery...\n", pos, err)
			fileOffset = maxInt64(pos, readStart+patOffset+int64(packetSize))
			continue
		}
		if err = pmt.Parse(); err != nil {
			fmt.Printf("0x%08x PMT parse error: %s, retrying PAT discovery...\n", pos, err)
			fileOffset = pos
			continue
		}
		programs := pmt.ProgramInfos()
		pcrPid := pmt.PcrPid()

		if _, err = reader.Seek(pos, 0); err != nil {
			return errors.Wrap(err, "file seek error")
		}
		fmt.Println("Detected PMT")
		if options.DumpPsi {
			pmt.Dump()
		} else {
			pmt.DumpProgramInfos(false)
		}

		err = BufferPes(reader, &pos, pcrPid, programs, options, packetSize, endOffset)
		if err != nil {
			fmt.Printf("0x%08x PES parse error: %s, retrying PAT discovery...\n", pos, err)
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
	return nil
}

// listPrograms parses each program's PMT (no PES analysis, so it only reads far
// enough to find each PMT) and prints a summary of what the stream contains.
func listPrograms(reader io.ReadSeeker, programs []Program, start int64, options options.Options, packetSize int, endOffset int64) {
	for _, prog := range programs {
		pos := start
		_, _ = reader.Seek(pos, 0)
		pmt := NewPmt()
		_ = BufferPsi(reader, &pos, prog.PmtPid, pmt, options, packetSize, endOffset)
		if err := pmt.Parse(); err != nil {
			fmt.Printf("Program %d: PMT PID 0x%x (PMT parse error: %s)\n", prog.ProgramNumber, prog.PmtPid, err)
			continue
		}
		fmt.Printf("Program %d: PMT PID 0x%x, PCR PID 0x%x\n", prog.ProgramNumber, prog.PmtPid, pmt.PcrPid())
		pmt.DumpProgramInfos(false)
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
