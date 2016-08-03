package tsparser

import (
	"fmt"
	"os"
	"reflect"
)

// MpegPacket PSI or PES
type MpegPacket interface {
	ContinuityCounter() uint8
	SetContinuityCounter(continuityCounter uint8)
	Append(buf []byte)
	Parse() error
	Dump()
}

const tsPacketSize = 188

// BufferingMpegPacket buffering PSI or PES data and Parse.
func BufferingMpegPacket(file *os.File, pos *int64, dHeader bool, dPayload bool, dAf bool, dPsi bool, ndt bool) error {
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
		tsPacket.Parse(tsBuffer, dAf)
		if dHeader {
			tsPacket.DumpHeader()
		}
		if dPayload {
			tsPacket.DumpData()
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
			buf := tsPacket.Payload()
			if pid == 0x0 && !isParsedPsi {
				pat = NewPat()
				pat.Append(buf[1+buf[0]:]) // read until pointer_field
			} else if prevMpegPacket != nil && !isParsedPsi && reflect.TypeOf(prevMpegPacket) == reflect.TypeOf(pat) {
				pat.Append(buf[1 : 1+buf[0]]) // read until pointer_field
				pat.Parse()
				if dPsi {
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
				if dPsi {
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
					newPes.SetContinuityCounter(tsPacket.ContinuityCounter())
					newPes.prevPcr = lastPcr
					newPes.prevPcrPos = lastPcrPos
					newPes.Append(buf)
				}
			} else if ok {
				mpegPacket.Parse()
				if !ndt {
					pes.DumpTimestamp()
				}
				pesMap[pid] = NewPes()
				if newPes, exits := pesMap[pid]; exits {
					newPes.pid = pid
					newPes.pos = *pos
					newPes.SetContinuityCounter(tsPacket.ContinuityCounter())
					newPes.prevPcr = lastPcr
					newPes.prevPcrPos = lastPcrPos
					newPes.Append(buf)
				}
			}
		} else if mpegPacket != nil && tsPacket.ContinuityCounter() == (mpegPacket.ContinuityCounter()+1) || (tsPacket.ContinuityCounter() == 0x0 && mpegPacket.ContinuityCounter() == 0xF) {
			mpegPacket.SetContinuityCounter(tsPacket.ContinuityCounter())
			if tsPacket.HasAf() && pcrPid != 0 && pid == pcrPid {
				if newPesa, exits := pesMap[pid]; exits {
					if newPesa.nextPcr == 0 {
						newPesa.nextPcr = lastPcr
						newPesa.nextPcrPos = lastPcrPos
					}
				}
			}
			mpegPacket.Append(tsPacket.Payload())
		} else if mpegPacket != nil {
			mpegPacket.SetContinuityCounter(mpegPacket.ContinuityCounter() + 1)
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
