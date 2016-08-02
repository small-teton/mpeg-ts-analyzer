package main

import (
	"fmt"
)

type Pes struct {
	pid         uint16
	cyclicValue uint8
	buf         []byte
	pos         int64
	prevPcr     uint64
	nextPcr     uint64
	prevPcrPos  int64
	nextPcrPos  int64
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

func NewPes() *Pes {
	pes := new(Pes)
	pes.buf = make([]byte, 0, 65536)
	return pes
}

func (this *Pes) CyclicValue() uint8               { return this.cyclicValue }
func (this *Pes) SetCyclicValue(cyclicValue uint8) { this.cyclicValue = cyclicValue }

func (this *Pes) Append(buf []byte) {
	this.buf = append(this.buf, buf...)
}

func (this *Pes) Parse() error {
	bb := new(BitBuffer)
	bb.Set(this.buf)

	var err error
	if this.packetStartCodePrefix, err = bb.PeekUint32(24); err != nil {
		return err
	}
	if this.streamID, err = bb.PeekUint8(8); err != nil {
		return err
	}
	if this.pesPacketLength, err = bb.PeekUint16(16); err != nil {
		return err
	}
	switch this.streamID {
	case 0xBC, 0xBF, 0xF0, 0xF1, 0xFF, 0xF2, 0xF8:
		this.data = this.buf[6 : 6+this.pesPacketLength]
		return nil
	}
	if err = bb.Skip(2); err != nil {
		return err
	} // '10'
	if this.pesScramblingControl, err = bb.PeekUint8(2); err != nil {
		return err
	}
	if this.pesPriority, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.dataAlignmentIndicator, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.copyright, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.originalOrCopy, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.ptsDtsFlags, err = bb.PeekUint8(2); err != nil {
		return err
	}
	if this.escrFlag, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.esRateFlag, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.dsmTrickModeFlag, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.additionalCopyIntoFlag, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.pesCrcFlag, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.pesExtentionFlag, err = bb.PeekUint8(1); err != nil {
		return err
	}
	if this.pesHeaderDataLength, err = bb.PeekUint8(8); err != nil {
		return err
	}

	if this.ptsDtsFlags == 2 {
		if err = bb.Skip(4); err != nil {
			return err
		} // '0011'
		var first, second, third uint64
		if first, err = bb.PeekUint64(3); err != nil {
			return err
		}
		this.pts = first << 30
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if second, err = bb.PeekUint64(15); err != nil {
			return err
		}
		this.pts |= second << 15
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if third, err = bb.PeekUint64(15); err != nil {
			return err
		}
		this.pts |= third
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if this.pid == 0x31 {
			fmt.Printf("0x%08x PTS: 0x%08x[%012fms] (pid=0x%02x)\n", this.pos, this.pts, float32(this.pts)/90, this.pid)
		}
	}
	if this.ptsDtsFlags == 3 {
		if err = bb.Skip(4); err != nil {
			return err
		} // '0011'
		var first, second, third uint64
		if first, err = bb.PeekUint64(3); err != nil {
			return err
		}
		this.pts = first << 30
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if second, err = bb.PeekUint64(15); err != nil {
			return err
		}
		this.pts |= second << 15
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if third, err = bb.PeekUint64(15); err != nil {
			return err
		}
		this.pts |= third
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if err = bb.Skip(4); err != nil {
			return err
		} // '0001'
		if first, err = bb.PeekUint64(3); err != nil {
			return err
		}
		this.dts = first << 30
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if second, err = bb.PeekUint64(15); err != nil {
			return err
		}
		this.dts |= second << 15
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if third, err = bb.PeekUint64(15); err != nil {
			return err
		}
		this.dts |= third
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if !this.notPrintTimestamp {
			fmt.Printf("0x%08x PTS: 0x%08x[%012fms] (pid:0x%02x)\n", this.pos, this.pts, float32(this.pts)/90, this.pid)
			prevPcr := float32(this.prevPcr) / 300 / 90
			nextPcr := float32(this.nextPcr) / 300 / 90
			pcrDelay := float32(this.dts)/90 - (prevPcr + (nextPcr-prevPcr)*(float32(this.pos-this.prevPcrPos)/float32(this.nextPcrPos-this.prevPcrPos)))
			fmt.Printf("0x%08x DTS: 0x%08x[%012fms] (pid:0x%02x) (delay:%fms)\n", this.pos, this.dts, float32(this.dts)/90, this.pid, pcrDelay)
		}
	}
	if this.escrFlag == 1 {
		if err = bb.Skip(2); err != nil {
			return err
		} // reserved
		var first, second, third uint64
		if first, err = bb.PeekUint64(3); err != nil {
			return err
		}
		this.escrBase = first << 30
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if second, err = bb.PeekUint64(15); err != nil {
			return err
		}
		this.escrBase |= second << 15
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if third, err = bb.PeekUint64(15); err != nil {
			return err
		}
		this.escrBase |= third
	}
	if this.esRateFlag == 1 {
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if this.esRate, err = bb.PeekUint32(22); err != nil {
			return err
		}
		if bb.Skip(1); err != nil {
			return err
		} // marker_bit
	}
	if this.dsmTrickModeFlag == 1 {
		if this.trickModeControl, err = bb.PeekUint8(3); err != nil {
			return err
		}
		switch this.trickModeControl {
		case 0x00, 0x03: // fast_forward, freeze_frame
			if this.fieldID, err = bb.PeekUint8(2); err != nil {
				return err
			}
			if this.intraSliceRefresh, err = bb.PeekUint8(1); err != nil {
				return err
			}
			if this.frequencyTruncation, err = bb.PeekUint8(2); err != nil {
				return err
			}
		case 0x01: // slow_motion, slow_reverse
			if this.repCntrl, err = bb.PeekUint8(5); err != nil {
				return err
			}
		default:
			if err = bb.Skip(5); err != nil {
				return err
			} // reserved
		}
	}
	if this.additionalCopyIntoFlag == 1 {
		if err = bb.Skip(1); err != nil {
			return err
		} // marker_bit
		if this.additionalCopyInfo, err = bb.PeekUint8(7); err != nil {
			return err
		}
	}
	if this.pesCrcFlag == 1 {
		if this.previousPesPacketCrc, err = bb.PeekUint16(16); err != nil {
			return err
		}
	}
	return nil
}

func (this *Pes) Dump() {

}
