package main

import (
	"fmt"
	"os"
	"reflect"

	"gopkg.in/alecthomas/kingpin.v2"
)

const tsPacketSize = 188

var (
	filename *string
	dHeader  *bool
	dPayload *bool
	dAf      *bool
	dPsi     *bool
	npt      *bool
)

func main() {
	filename = kingpin.Arg("input", "Input file name.").Required().String()
	dHeader = kingpin.Flag("dumpTsHeader", "").Bool()
	dPayload = kingpin.Flag("dumpTsPayload", "").Bool()
	dAf = kingpin.Flag("dumpAdaptationField", "").Bool()
	dPsi = kingpin.Flag("dumpPsi", "").Bool()
	npt = kingpin.Flag("notPrintTimestamp", "").Short('n').Bool()
	kingpin.Parse()

	if err := ParseTsFile(*filename); err != nil {

	}
}

func ParseTsFile(filename string) error {
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
		pos, err := FindPat(buf)
		if err != nil {
			continue
		}

		_, err = file.Seek(pos, 0)
		if err != nil {
			return fmt.Errorf("File seek error: %s", err)
		}

		err = BufferingMpegPacket(file, &pos)
		if err != nil {
			return fmt.Errorf("TS parse error: %s", err)
		}
		if size < bufSize {
			break
		}
	}
	return nil
}

func FindPat(data []byte) (int64, error) {
	for i := 0; i+188*2 <= len(data)-1; i++ {
		if data[i] == 0x47 && data[i+188] == 0x47 && data[i+188*2] == 0x47 {
			if (data[i+1]&0x5F) == 0x40 && data[i+2] == 0x00 {
				return int64(i), nil
			}
		}
	}
	return 0, fmt.Errorf("Cannot find pat")
}

func BufferingMpegPacket(file *os.File, pos *int64) error {
	tsBuffer := make([]byte, tsPacketSize)
	var pmtPid uint16
	var pat *Pat
	var pmt *Pmt
	pesMap := make(map[uint16]*Pes)
	var prevPcr uint64
	var prevMpegPacket MpegPacket
	isParsedPsi := false
	var pcrPid uint16
	var lastPcr uint64
	var lastPcrPos int64

	for {
		size, err := file.Read(tsBuffer)
		if err != nil || size != tsPacketSize {
			return fmt.Errorf("File read error: %s", err)
		}
		if size < tsPacketSize {
			break
		}

		tsPacket := NewTsPacket(*pos, &prevPcr)
		tsPacket.Parse(tsBuffer)
		if *dHeader {
			tsPacket.DumpTsHeader()
		}
		if *dPayload {
			tsPacket.DumpTsHeader()
		}

		pid := tsPacket.Pid()
		updateLastPcr(tsPacket, *pos, pcrPid, &lastPcr, &lastPcrPos)
		pes, ok := pesMap[tsPacket.Pid()]

		var mpegPacket MpegPacket
		if pid == 0x0 {
			mpegPacket = pat
		} else if pid == pmtPid {
			mpegPacket = pmt
		} else if ok {
			mpegPacket = pes
		} else if pid == 0x1FFF {
			*pos += int64(size)
			continue
		}

		if tsPacket.PayloadUnitStartIndicator() {
			buf := tsPacket.Body()
			if pid == 0x0 && !isParsedPsi {
				pat = NewPat()
				pat.Append(buf[1+buf[0]:]) // read until pointer_field
			} else if prevMpegPacket != nil && !isParsedPsi && reflect.TypeOf(prevMpegPacket) == reflect.TypeOf(pat) {
				pat.Append(buf[1 : 1+buf[0]]) // read until pointer_field
				pat.Parse()
				if *dPsi {
					pat.Dump()
				}
				pmtPid = pat.PmtPid()
				pmt = NewPmt()
				if pid == pmtPid {
					mpegPacket = pmt
					mpegPacket.Append(buf[1+buf[0]:])
				}
			} else if prevMpegPacket != nil && !isParsedPsi && reflect.TypeOf(prevMpegPacket) == reflect.TypeOf(pmt) {
				pmt.Append(buf[1 : 1+buf[0]]) // read until pointer_field
				pmt.Parse()
				if *dPsi {
					pmt.Dump()
				}
				pcrPid = pmt.PcrPid()
				updateLastPcr(tsPacket, *pos, pcrPid, &lastPcr, &lastPcrPos)
				isParsedPsi = true
				for _, val := range pmt.ProgramInfos() {
					pesMap[val.elementaryPid] = NewPes()
				}
				if newPes, exits := pesMap[pid]; exits {
					newPes.pid = pid
					newPes.pos = *pos
					newPes.SetCyclicValue(tsPacket.CyclicValue())
					newPes.prevPcr = lastPcr
					newPes.prevPcrPos = lastPcrPos
					newPes.Append(buf)
				}
			} else if ok {
				mpegPacket.Parse()
				pesMap[pid] = NewPes()
				if newPes, exits := pesMap[pid]; exits {
					newPes.pid = pid
					newPes.pos = *pos
					newPes.SetCyclicValue(tsPacket.CyclicValue())
					newPes.prevPcr = lastPcr
					newPes.prevPcrPos = lastPcrPos
					newPes.Append(buf)
				}
			}
		} else if mpegPacket != nil && tsPacket.CyclicValue() == (mpegPacket.CyclicValue()+1) || (tsPacket.CyclicValue() == 0x0 && mpegPacket.CyclicValue() == 0xF) {
			mpegPacket.SetCyclicValue(tsPacket.CyclicValue())
			if tsPacket.HasAf() && pcrPid != 0 && pid == pcrPid {
				if newPesa, exits := pesMap[pid]; exits {
					if newPesa.nextPcr == 0 {
						newPesa.nextPcr = lastPcr
						newPesa.nextPcrPos = lastPcrPos
					}
				}
			}
			mpegPacket.Append(tsPacket.Body())
		} else if mpegPacket != nil {
			mpegPacket.SetCyclicValue(mpegPacket.CyclicValue() + 1)
		}

		*pos += int64(size)
		prevMpegPacket = mpegPacket
	}
	return nil
}

func updateLastPcr(tsPacket *TsPacket, pos int64, pcrPid uint16, lastPcr *uint64, lastPcrPos *int64) {
	if tsPacket.HasAf() && tsPacket.adaptationField.PcrFlag() && pcrPid != 0 && tsPacket.Pid() == pcrPid {
		*lastPcr = tsPacket.Pcr()
		*lastPcrPos = pos
	}
}
