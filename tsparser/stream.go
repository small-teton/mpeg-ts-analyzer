package tsparser

import (
	"fmt"
	"io"
	"os"

	"github.com/small-teton/mpeg-ts-analyzer/options"
)

func ParseTsFile(filename string, options options.Options) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("File open error: %s %s", filename, err)
	}
	fmt.Println("Input file: ", filename)

	pat := NewPat()
	pmt := NewPmt()

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
		err = BufferPsi(file, &pos, patPid, pat, options)
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
		err = BufferPsi(file, &pos, pmtPid, pmt, options)
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

		err = BufferPes(file, &pos, pcrPid, programs, options)
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
