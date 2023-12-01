package tsparser

import (
	"fmt"

	"github.com/small-teton/mpeg-ts-analyzer/bitbuffer"
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
	additionalCopyInfoFlag           uint8
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

// Initialize Set Params for PES
func (p *Pes) Initialize(pid uint16, pos int64, prevPcr uint64, prevPcrPos int64) {
	p.pid = pid
	p.continuityCounter = 0
	p.buf = p.buf[0:0]
	p.pos = pos
	p.prevPcr = prevPcr
	p.nextPcr = 0
	p.prevPcrPos = prevPcrPos
	p.nextPcrPos = 0

	p.packetStartCodePrefix = 0
	p.streamID = 0
	p.pesPacketLength = 0
	p.pesScramblingControl = 0
	p.pesPriority = 0
	p.dataAlignmentIndicator = 0
	p.copyright = 0
	p.originalOrCopy = 0
	p.ptsDtsFlags = 0
	p.escrFlag = 0
	p.esRateFlag = 0
	p.dsmTrickModeFlag = 0
	p.additionalCopyInfoFlag = 0
	p.pesCrcFlag = 0
	p.pesExtentionFlag = 0
	p.pesHeaderDataLength = 0
	p.pts = 0
	p.dts = 0
	p.escr = 0
	p.escrBase = 0
	p.escrExtention = 0
	p.esRate = 0
	p.trickModeControl = 0
	p.fieldID = 0
	p.intraSliceRefresh = 0
	p.frequencyTruncation = 0
	p.repCntrl = 0
	p.additionalCopyInfo = 0
	p.previousPesPacketCrc = 0
	p.pesPrivateDataFlag = 0
	p.packHeaderFieldFlag = 0
	p.programPacketSequenceCounterFlag = 0
	p.pStdBufferFlag = 0
	p.pesExtentionFlag2 = 0
	p.programPacketSequenceCounter = 0
	p.mpeg1Mpeg2Identifer = 0
	p.originalStuffLength = 0
	p.pStdBufferScale = 0
	p.pStdBufferSize = 0
	p.pesExtentionFieldLength = 0
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
	if p.additionalCopyInfoFlag, err = bb.PeekUint8(1); err != nil {
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
	if p.additionalCopyInfoFlag == 1 {
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
		//fmt.Printf("0x%08x PTS: 0x%08x[%012fms] (pid:0x%02x)\n", p.pos, p.pts, float32(p.pts)/90, p.pid)
		prevPcr := float32(p.prevPcr) / 300 / 90
		nextPcr := float32(p.nextPcr) / 300 / 90
		pcrDelay = float32(p.dts)/90 - (prevPcr + (nextPcr-prevPcr)*(float32(p.pos-p.prevPcrPos)/float32(p.nextPcrPos-p.prevPcrPos)))
		fmt.Printf("0x%08x DTS: 0x%08x[%012fms] (pid:0x%02x) (delay:%fms)\n", p.pos, p.dts, float32(p.dts)/90, p.pid, pcrDelay)
	}
	return float64(pcrDelay)
}

// Dump PES header detail.
func (p *Pes) DumpHeader() {
	fmt.Printf("\n===========================================\n")
	fmt.Printf(" PES")
	fmt.Printf("\n===========================================\n")
	fmt.Printf("PES : packet_start_code_prefix			: %d\n", p.packetStartCodePrefix)
	fmt.Printf("PES : stream_id					: %d\n", p.streamID)
	fmt.Printf("PES : pes_packet_length				: %d\n", p.pesPacketLength)
	fmt.Printf("PES : pes_scrambling_control			: %d\n", p.pesScramblingControl)
	fmt.Printf("PES : pes_priority				: %d\n", p.pesPriority)
	fmt.Printf("PES : data_alignment_indicator			: %d\n", p.dataAlignmentIndicator)
	fmt.Printf("PES : copyright					: %d\n", p.copyright)
	fmt.Printf("PES : original_or_copy				: %d\n", p.originalOrCopy)
	fmt.Printf("PES : pts_dts_flags				: %d\n", p.ptsDtsFlags)
	fmt.Printf("PES : escr_flag					: %d\n", p.escrFlag)
	fmt.Printf("PES : es_rate_flag				: %d\n", p.esRateFlag)
	fmt.Printf("PES : dsm_trick_mode_flag			: %d\n", p.dsmTrickModeFlag)
	fmt.Printf("PES : additional_copy_info_flag			: %d\n", p.additionalCopyInfoFlag)
	fmt.Printf("PES : pes_crc_flag				: %d\n", p.pesCrcFlag)
	fmt.Printf("PES : pes_extention_flag			: %d\n", p.pesExtentionFlag)
	fmt.Printf("PES : pes_header_data_length			: %d\n", p.pesHeaderDataLength)
	fmt.Printf("PES : pts					: %d\n", p.pts)
	fmt.Printf("PES : dts					: %d\n", p.dts)
	fmt.Printf("PES : escr					: %d\n", p.escr)
	fmt.Printf("PES : escr_base					: %d\n", p.escrBase)
	fmt.Printf("PES : escr_extention				: %d\n", p.escrExtention)
	fmt.Printf("PES : es_rate					: %d\n", p.esRate)
	fmt.Printf("PES : trick_mode_control			: %d\n", p.trickModeControl)
	fmt.Printf("PES : field_id					: %d\n", p.fieldID)
	fmt.Printf("PES : intra_slice_refresh			: %d\n", p.intraSliceRefresh)
	fmt.Printf("PES : frequency_truncation			: %d\n", p.frequencyTruncation)
	fmt.Printf("PES : rep_cntrl					: %d\n", p.repCntrl)
	fmt.Printf("PES : additional_copy_info			: %d\n", p.additionalCopyInfo)
	fmt.Printf("PES : previous_pes_packet_crc			: %d\n", p.previousPesPacketCrc)
	fmt.Printf("PES : pes_private_data_flag			: %d\n", p.pesPrivateDataFlag)
	fmt.Printf("PES : pack_header_field_flag			: %d\n", p.packHeaderFieldFlag)
	fmt.Printf("PES : program_packet_sequence_counter_flag	: %d\n", p.programPacketSequenceCounterFlag)
	fmt.Printf("PES : p-std_buffer_flag				: %d\n", p.pStdBufferFlag)
	fmt.Printf("PES : pes_extention_flag2			: %d\n", p.pesExtentionFlag2)
	fmt.Printf("PES : program_packet_sequence_counter		: %d\n", p.programPacketSequenceCounter)
	fmt.Printf("PES : mpeg1_mpeg2_identifer			: %d\n", p.mpeg1Mpeg2Identifer)
	fmt.Printf("PES : original_stuff_length			: %d\n", p.originalStuffLength)
	fmt.Printf("PES : p-std_buffer_scale			: %d\n", p.pStdBufferScale)
	fmt.Printf("PES : p-std_buffer_size				: %d\n", p.pStdBufferSize)
	fmt.Printf("PES : pes_extention_field_length		: %d\n", p.pesExtentionFieldLength)
}
