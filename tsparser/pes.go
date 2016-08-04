package tsparser

import (
	"fmt"

	"github.com/small-teton/MpegTsAnalyzer/bitbuffer"
)

// Pes Packetized Elementary Stream.
type Pes struct {
	pid               uint16
	continuityCounter uint8
	buf               []byte
	pos               int64
	prevPcr           uint64
	nextPcr           uint64
	prevPcrPos        int64
	nextPcrPos        int64
	notPrintTimestamp bool

	packetStartCodePrefix            uint32
	streamID                         uint8
	pesPacketLength                  uint16
	pesScramblingControl             uint8
	pesPriority                      uint8
	dataAlignmentIndicator           uint8
	copyright                        uint8
	originalOrCopy                   uint8
	ptsDtsFlags                      uint8
	escrFlag                         uint8
	esRateFlag                       uint8
	dsmTrickModeFlag                 uint8
	additionalCopyIntoFlag           uint8
	pesCrcFlag                       uint8
	pesExtentionFlag                 uint8
	pesHeaderDataLength              uint8
	pts                              uint64
	dts                              uint64
	escr                             uint32
	escrBase                         uint64
	escrExtention                    uint16
	esRate                           uint32
	trickModeControl                 uint8
	fieldID                          uint8
	intraSliceRefresh                uint8
	frequencyTruncation              uint8
	repCntrl                         uint8
	additionalCopyInfo               uint8
	previousPesPacketCrc             uint16
	pesPrivateDataFlag               uint8
	packHeaderFieldFlag              uint8
	programPacketSequenceCounterFlag uint8
	pStdBufferFlag                   uint8
	pesExtentionFlag2                uint8
	programPacketSequenceCounter     uint8
	mpeg1Mpeg2Identifer              uint8
	originalStuffLength              uint8
	pStdBufferScale                  uint8
	pStdBufferSize                   uint16
	pesExtentionFieldLength          uint8

	data []byte
}

// NewPes create new PES instance
func NewPes() *Pes {
	pes := new(Pes)
	pes.buf = make([]byte, 0, 65536)
	return pes
}

// ContinuityCounter return current continuity_counter of TsPacket.
func (p *Pes) ContinuityCounter() uint8 { return p.continuityCounter }

// SetContinuityCounter set current continuity_counter of TsPacket.
func (p *Pes) SetContinuityCounter(continuityCounter uint8) { p.continuityCounter = continuityCounter }

// Append append ts payload data for buffer.
func (p *Pes) Append(buf []byte) {
	p.buf = append(p.buf, buf...)
}

// Parse PES header.
func (p *Pes) Parse() error {
	bb := new(bitbuffer.BitBuffer)
	bb.Set(p.buf)

	var err error
	if p.packetStartCodePrefix, err = bb.PeekUint32(24); err != nil {
		return err
	}
	if p.streamID, err = bb.PeekUint8(8); err != nil {
		return err
	}
	if p.pesPacketLength, err = bb.PeekUint16(16); err != nil {
		return err
	}
	switch p.streamID {
	case 0xBC, 0xBF, 0xF0, 0xF1, 0xFF, 0xF2, 0xF8:
		p.data = p.buf[6 : 6+p.pesPacketLength]
		return nil
	}
	if err = bb.Skip(2); err != nil {
		return err
	} // '10'
	if p.pesScramblingControl, err = bb.PeekUint8(2); err != nil {
		return err
	}
	if p.pesPriority, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if p.dataAlignmentIndicator, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if p.copyright, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if p.originalOrCopy, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if p.ptsDtsFlags, err = bb.PeekUint8(2); err != nil {
		return err
	}
	if p.escrFlag, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if p.esRateFlag, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if p.dsmTrickModeFlag, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if p.additionalCopyIntoFlag, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if p.pesCrcFlag, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if p.pesExtentionFlag, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if p.pesHeaderDataLength, err = bb.PeekUint8(8); err != nil {
		return err
	}

	if p.ptsDtsFlags == 2 {
		if err = bb.Skip(4); err != nil {
			return err
		} // '0011'
		var first, second, third uint64
		if first, err = bb.PeekUint64(3); err != nil {
			return err
		}
		p.pts = first << 30
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if second, err = bb.PeekUint64(15); err != nil {
			return err
		}
		p.pts |= second << 15
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if third, err = bb.PeekUint64(15); err != nil {
			return err
		}
		p.pts |= third
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
	}
	if p.ptsDtsFlags == 3 {
		if err = bb.Skip(4); err != nil {
			return err
		} // '0011'
		var first, second, third uint64
		if first, err = bb.PeekUint64(3); err != nil {
			return err
		}
		p.pts = first << 30
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if second, err = bb.PeekUint64(15); err != nil {
			return err
		}
		p.pts |= second << 15
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if third, err = bb.PeekUint64(15); err != nil {
			return err
		}
		p.pts |= third
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if err = bb.Skip(4); err != nil {
			return err
		} // '0001'
		if first, err = bb.PeekUint64(3); err != nil {
			return err
		}
		p.dts = first << 30
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if second, err = bb.PeekUint64(15); err != nil {
			return err
		}
		p.dts |= second << 15
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if third, err = bb.PeekUint64(15); err != nil {
			return err
		}
		p.dts |= third
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
	}
	if p.escrFlag == 1 {
		if err = bb.Skip(2); err != nil {
			return err
		} // reserved
		var first, second, third uint64
		if first, err = bb.PeekUint64(3); err != nil {
			return err
		}
		p.escrBase = first << 30
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if second, err = bb.PeekUint64(15); err != nil {
			return err
		}
		p.escrBase |= second << 15
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if third, err = bb.PeekUint64(15); err != nil {
			return err
		}
		p.escrBase |= third
	}
	if p.esRateFlag == 1 {
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if p.esRate, err = bb.PeekUint32(22); err != nil {
			return err
		}
		if bb.Skip(1); err != nil {
			return err
		} // marker_bit
	}
	if p.dsmTrickModeFlag == 1 {
		if p.trickModeControl, err = bb.PeekUint8(3); err != nil {
			return err
		}
		switch p.trickModeControl {
		case 0x00, 0x03: // fast_forward, freeze_frame
			if p.fieldID, err = bb.PeekUint8(2); err != nil {
				return err
			}
			if p.intraSliceRefresh, err = bb.PeekUint8(1); err != nil {
				return err
			}
			if p.frequencyTruncation, err = bb.PeekUint8(2); err != nil {
				return err
			}
		case 0x01: // slow_motion, slow_reverse
			if p.repCntrl, err = bb.PeekUint8(5); err != nil {
				return err
			}
		default:
			if err = bb.Skip(5); err != nil {
				return err
			} // reserved
		}
	}
	if p.additionalCopyIntoFlag == 1 {
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if p.additionalCopyInfo, err = bb.PeekUint8(7); err != nil {
			return err
		}
	}
	if p.pesCrcFlag == 1 {
		if p.previousPesPacketCrc, err = bb.PeekUint16(16); err != nil {
			return err
		}
	}
	return nil
}

// DumpTimestamp dump PTS and DTS
func (p *Pes) DumpTimestamp() float64 {
	var pcrDelay float32
	if p.ptsDtsFlags == 2 {
		prevPcr := float32(p.prevPcr) / 300 / 90
		nextPcr := float32(p.nextPcr) / 300 / 90
		pcrDelay = float32(p.pts)/90 - (prevPcr + (nextPcr-prevPcr)*(float32(p.pos-p.prevPcrPos)/float32(p.nextPcrPos-p.prevPcrPos)))
		fmt.Printf("0x%08x PTS: 0x%08x[%012fms] (pid:0x%02x) (delay:%fms)\n", p.pos, p.pts, float32(p.pts)/90, p.pid, pcrDelay)
	}
	if p.ptsDtsFlags == 3 {
		fmt.Printf("0x%08x PTS: 0x%08x[%012fms] (pid:0x%02x)\n", p.pos, p.pts, float32(p.pts)/90, p.pid)
		prevPcr := float32(p.prevPcr) / 300 / 90
		nextPcr := float32(p.nextPcr) / 300 / 90
		pcrDelay = float32(p.dts)/90 - (prevPcr + (nextPcr-prevPcr)*(float32(p.pos-p.prevPcrPos)/float32(p.nextPcrPos-p.prevPcrPos)))
		fmt.Printf("0x%08x DTS: 0x%08x[%012fms] (pid:0x%02x) (delay:%fms)\n", p.pos, p.dts, float32(p.dts)/90, p.pid, pcrDelay)
	}
	return float64(pcrDelay)
}

// Dump PES header detail.
func (p *Pes) Dump() {

}
