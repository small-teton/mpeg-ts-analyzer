package main

import (
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kingpin/v2"
	"github.com/small-teton/MpegTsAnalyzer/options"
	"github.com/small-teton/MpegTsAnalyzer/tsparser"
)

const tsPacketSize = 188

var (
	dumpHeader          = kingpin.Flag("dump-ts-header", "Dump TS packet header.").Bool()
	dumpPayload         = kingpin.Flag("dump-ts-payload", "Dump TS packet payload binary.").Bool()
	dumpAdaptationField = kingpin.Flag("dump-adaptation-field", "Dump TS packet adaptation_field detail.").Bool()
	dumpPsi             = kingpin.Flag("dump-psi", "Dump PSI(PAT/PMT) detail.").Bool()
	dumpPesHeader       = kingpin.Flag("dump-pes-header", "Dump PES packet header detail.").Bool()
	dumpTimestamp       = kingpin.Flag("dump-timestamp", "Dump PCR/PTS/DTS timestamps.").Short('t').Bool()
	showVersion         = kingpin.Flag("version", "Show app version.").Bool()
)

var version string

func main() {
	filename := kingpin.Arg("input", "Input file name.").String()
	kingpin.Parse()

	var options options.Options
	options.SetDumpHeader(*dumpHeader)
	options.SetDumpPayload(*dumpPayload)
	options.SetDumpAdaptationField(*dumpAdaptationField)
	options.SetDumpPsi(*dumpPsi)
	options.SetDumpPesHeader(*dumpPesHeader)
	options.SetDumpTimestamp(*dumpTimestamp)
	options.SetVersion(*showVersion)

	if options.Version() {
		fmt.Printf("version: %s\n", version)
		os.Exit(0)
	}

	if *filename == "" {
		fmt.Println("input file name is empty.")
		os.Exit(1)
	}

	if err := parseTsFile(*filename, options); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func parseTsFile(filename string, options options.Options) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("File open error: %s %s", filename, err)
	}
	fmt.Println("Input file: ", filename)

	pat := tsparser.NewPat()
	pmt := tsparser.NewPmt()

	const patPid = 0x0
	const bufSize = 65536
	var pos int64
	buf := make([]byte, bufSize)
	for {
		size, err := file.Read(buf)
		if err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("File read error: %s", err)
		}
		if pos, err = findPat(buf); err != nil {
			continue
		}

		if _, err = file.Seek(pos, 0); err != nil {
			return fmt.Errorf("File seek error: %s", err)
		}

		// Parse PAT
		err = tsparser.BufferPsi(file, &pos, patPid, pat, options)
		err = pat.Parse()
		if err != nil {
			continue
		}
		pmtPid := pat.PmtPid()

		if _, err = file.Seek(pos, 0); err != nil {
			return fmt.Errorf("File seek error: %s", err)
		}
		fmt.Printf("Detected PAT: PMT pid = 0x%02x\n", pmtPid)
		if options.DumpPsi() {
			pat.Dump()
		}

		// Parse PMT
		err = tsparser.BufferPsi(file, &pos, pmtPid, pmt, options)
		err = pmt.Parse()
		if err != nil {
			continue
		}
		programs := pmt.ProgramInfos()
		pcrPid := pmt.PcrPid()

		if _, err = file.Seek(pos, 0); err != nil {
			return fmt.Errorf("File seek error: %s", err)
		}
		fmt.Println("Detected PMT")
		if options.DumpPsi() {
			pmt.Dump()
		} else {
			pmt.DumpProgramInfos()
		}

		err = tsparser.BufferPes(file, &pos, pcrPid, programs, options)
		if err != nil {
			return fmt.Errorf("TS parse error: %s", err)
		}
		if size < bufSize {
			break
		}
		pos += bufSize
	}
	return nil
}

func findPat(data []byte) (int64, error) {
	for i := 0; i+188*2 <= len(data)-1; i++ {
		if data[i] == 0x47 && data[i+188] == 0x47 && data[i+188*2] == 0x47 {
			if (data[i+1]&0x5F) == 0x40 && data[i+2] == 0x00 {
				return int64(i), nil
			}
		}
	}
	return 0, fmt.Errorf("Cannot find pat")
}
