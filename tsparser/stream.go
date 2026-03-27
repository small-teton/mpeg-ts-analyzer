package tsparser

import (
	"fmt"
	"io"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/small-teton/mpeg-ts-analyzer/options"
)

func ParseTsFile(filename string, options options.Options) error {
	file, err := os.Open(filename)
	if err != nil {
		return errors.WithMessagef(err, "file open error: %s", filename)
	}
	defer file.Close()
	fmt.Println("Input file: ", filename)

	return parseTsReader(file, options)
}

func parseTsReader(reader io.ReadSeeker, options options.Options) error {
	const patPid = 0x0
	const bufSize = 65536
	var fileOffset int64
	buf := make([]byte, bufSize)
	packetSize := 0
	for {
		readStart := fileOffset
		size, err := reader.Read(buf)
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
		if err = BufferPsi(reader, &pos, patPid, pat, options, packetSize); err != nil {
			fmt.Printf("0x%08x PAT buffering error: %s, retrying PAT discovery...\n", pos, err)
			fileOffset = maxInt64(pos, readStart+patOffset+int64(packetSize))
			continue
		}
		if err = pat.Parse(); err != nil {
			fmt.Printf("0x%08x PAT parse error: %s, retrying PAT discovery...\n", pos, err)
			fileOffset = pos
			continue
		}
		pmtPid := pat.PmtPid()

		if _, err = reader.Seek(pos, 0); err != nil {
			return errors.Wrap(err, "file seek error")
		}
		fmt.Printf("Detected PAT: PMT pid = 0x%02x\n", pmtPid)
		if options.DumpPsi {
			pat.Dump()
		}

		// Parse PMT
		pmt := NewPmt()
		if err = BufferPsi(reader, &pos, pmtPid, pmt, options, packetSize); err != nil {
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
			pmt.DumpProgramInfos()
		}

		err = BufferPes(reader, &pos, pcrPid, programs, options, packetSize)
		if err != nil {
			fmt.Printf("0x%08x PES parse error: %s, retrying PAT discovery...\n", pos, err)
			fileOffset = maxInt64(pos, readStart+patOffset+int64(packetSize))
			continue
		}
		break
	}
	return nil
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
