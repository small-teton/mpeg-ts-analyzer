package main

import (
	"fmt"
	"os"

	"github.com/small-teton/MpegTsAnalyzer/tsparser"
	"gopkg.in/alecthomas/kingpin.v2"
)

const tsPacketSize = 188

var (
	filename *string
	dHeader  *bool
	dPayload *bool
	dAf      *bool
	dPsi     *bool
	ndt      *bool
)

func main() {
	filename = kingpin.Arg("input", "Input file name.").Required().String()
	dHeader = kingpin.Flag("dump-ts-header", "").Bool()
	dPayload = kingpin.Flag("dump-ts-payload", "").Bool()
	dAf = kingpin.Flag("dump-adaptation-field", "").Bool()
	dPsi = kingpin.Flag("dump-psi", "").Bool()
	ndt = kingpin.Flag("not-dump-timestamp", "").Short('n').Bool()
	kingpin.Parse()

	if err := parseTsFile(*filename); err != nil {

	}
}

func parseTsFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("File open error: %s", err)
	}

	const bufSize = 65536
	buf := make([]byte, bufSize)
	for {
		size, err := file.Read(buf)
		if err != nil {
			return fmt.Errorf("File read error: %s", err)
		}
		pos, err := findPat(buf)
		if err != nil {
			continue
		}

		_, err = file.Seek(pos, 0)
		if err != nil {
			return fmt.Errorf("File seek error: %s", err)
		}

		err = tsparser.BufferingMpegPacket(file, &pos, *dHeader, *dPayload, *dPsi, *dAf, *ndt)
		if err != nil {
			return fmt.Errorf("TS parse error: %s", err)
		}
		if size < bufSize {
			break
		}
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
