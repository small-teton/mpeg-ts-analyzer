package tsparser

import (
	"fmt"
)

// Pes Packetized Elementary Stream.
type Pes struct {
	pid               uint16
	continuityCounter uint8
	buf               []byte
	pos               int64
	newBitReader      func() bitReader
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
	pesExtensionFlag                 uint8
	pesHeaderDataLength              uint8
	pts                              uint64
	dts                              uint64
	escr                             uint32
	escrBase                         uint64
	escrExtension                    uint16
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
	pesExtensionFlag2                uint8
	programPacketSequenceCounter     uint8
	mpeg1Mpeg2Identifier             uint8
	originalStuffLength              uint8
	pStdBufferScale                  uint8
	pStdBufferSize                   uint16
	pesExtensionFieldLength          uint8

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
	p.pesExtensionFlag = 0
	p.pesHeaderDataLength = 0
	p.pts = 0
	p.dts = 0
	p.escr = 0
	p.escrBase = 0
	p.escrExtension = 0
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
	p.pesExtensionFlag2 = 0
	p.programPacketSequenceCounter = 0
	p.mpeg1Mpeg2Identifier = 0
	p.originalStuffLength = 0
	p.pStdBufferScale = 0
	p.pStdBufferSize = 0
	p.pesExtensionFieldLength = 0
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
	var bb bitReader
	if p.newBitReader != nil {
		bb = p.newBitReader()
	} else {
		bb = newDefaultBitReader()
	}
	bb.Set(p.buf)

	var err error
	if p.packetStartCodePrefix, err = bb.ReadUint32(24); err != nil {
		return fmt.Errorf("failed to read pes packet_start_code_prefix: %w", err)
	}
	if p.packetStartCodePrefix != 0x000001 {
		return fmt.Errorf("invalid pes packet_start_code_prefix: 0x%06x", p.packetStartCodePrefix)
	}
	if p.streamID, err = bb.ReadUint8(8); err != nil {
		return fmt.Errorf("failed to read pes stream_id: %w", err)
	}
	if p.pesPacketLength, err = bb.ReadUint16(16); err != nil {
		return fmt.Errorf("failed to read pes pes_packet_length: %w", err)
	}
	switch p.streamID {
	case 0xBC, 0xBF, 0xF0, 0xF1, 0xFF, 0xF2, 0xF8:
		// pes_packet_length is attacker-controlled; reject a length that runs
		// past the buffered data.
		if 6+int(p.pesPacketLength) > len(p.buf) {
			return fmt.Errorf("invalid pes_packet_length: %d", p.pesPacketLength)
		}
		p.data = p.buf[6 : 6+p.pesPacketLength]
		return nil
	}
	if err = bb.Skip(2); err != nil {
		return fmt.Errorf("failed to skip in pes: 10: %w", err)
	} // '10'
	if p.pesScramblingControl, err = bb.ReadUint8(2); err != nil {
		return fmt.Errorf("failed to read pes pes_scrambling_control: %w", err)
	}
	if p.pesPriority, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to read pes pes_priority: %w", err)
	}
	if p.dataAlignmentIndicator, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to read pes data_alignment_indicator: %w", err)
	}
	if p.copyright, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to read pes copyright: %w", err)
	}
	if p.originalOrCopy, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to read pes original_or_copy: %w", err)
	}
	if p.ptsDtsFlags, err = bb.ReadUint8(2); err != nil {
		return fmt.Errorf("failed to read pes pts_fts_flag: %w", err)
	}
	if p.escrFlag, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to read pes escr_flag: %w", err)
	}
	if p.esRateFlag, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to read pes es_rate_flag: %w", err)
	}
	if p.dsmTrickModeFlag, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to read pes dsm_trick_mode_flag: %w", err)
	}
	if p.additionalCopyInfoFlag, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to read pes additional_copy_info_flag: %w", err)
	}
	if p.pesCrcFlag, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to read pes pes_crc_flag: %w", err)
	}
	if p.pesExtensionFlag, err = bb.ReadUint8(1); err != nil {
		return fmt.Errorf("failed to read pes pes_extension_flag: %w", err)
	}
	if p.pesHeaderDataLength, err = bb.ReadUint8(8); err != nil {
		return fmt.Errorf("failed to read pes pes_header_data_length: %w", err)
	}

	if p.ptsDtsFlags == 2 {
		if err = bb.Skip(4); err != nil {
			return fmt.Errorf("failed to skip in pes: 0011 (PtsDtsFlag=2): %w", err)
		} // '0011'
		var first, second, third uint64
		if first, err = bb.ReadUint64(3); err != nil {
			return fmt.Errorf("failed to read pes pts first (PtsDtsFlag=2): %w", err)
		}
		p.pts = first << 30
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: first pts marker_bit (PtsDtsFlag=2): %w", err)
		} // marker_bit
		if second, err = bb.ReadUint64(15); err != nil {
			return fmt.Errorf("failed to read pes pts second (PtsDtsFlag=2): %w", err)
		}
		p.pts |= second << 15
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: second pts marker_bit (PtsDtsFlag=2): %w", err)
		} // marker_bit
		if third, err = bb.ReadUint64(15); err != nil {
			return fmt.Errorf("failed to read pes pts third (PtsDtsFlag=2): %w", err)
		}
		p.pts |= third
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: third pts marker_bit (PtsDtsFlag=2): %w", err)
		} // marker_bit
	}
	if p.ptsDtsFlags == 3 {
		if err = bb.Skip(4); err != nil {
			return fmt.Errorf("failed to skip in pes: 0011 (PtsDtsFlag=3): %w", err)
		} // '0011'
		var first, second, third uint64
		if first, err = bb.ReadUint64(3); err != nil {
			return fmt.Errorf("failed to read pes pts first (PtsDtsFlag=3): %w", err)
		}
		p.pts = first << 30
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: first pts marker_bit (PtsDtsFlag=3): %w", err)
		} // marker_bit
		if second, err = bb.ReadUint64(15); err != nil {
			return fmt.Errorf("failed to read pes pts second (PtsDtsFlag=3): %w", err)
		}
		p.pts |= second << 15
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: second pts marker_bit (PtsDtsFlag=3): %w", err)
		} // marker_bit
		if third, err = bb.ReadUint64(15); err != nil {
			return fmt.Errorf("failed to read pes pts third (PtsDtsFlag=3): %w", err)
		}
		p.pts |= third
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: third pts marker_bit (PtsDtsFlag=3): %w", err)
		} // marker_bit
		if err = bb.Skip(4); err != nil {
			return fmt.Errorf("failed to skip in pes: pts-dts 0001 (PtsDtsFlag=3): %w", err)
		} // '0001'
		if first, err = bb.ReadUint64(3); err != nil {
			return fmt.Errorf("failed to read pes dts first (PtsDtsFlag=3): %w", err)
		}
		p.dts = first << 30
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: first dts marker_bit (PtsDtsFlag=3): %w", err)
		} // marker_bit
		if second, err = bb.ReadUint64(15); err != nil {
			return fmt.Errorf("failed to read pes dts second (PtsDtsFlag=3): %w", err)
		}
		p.dts |= second << 15
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: second dts marker_bit (PtsDtsFlag=3): %w", err)
		} // marker_bit
		if third, err = bb.ReadUint64(15); err != nil {
			return fmt.Errorf("failed to read pes dts third (PtsDtsFlag=3): %w", err)
		}
		p.dts |= third
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: third dts marker_bit (PtsDtsFlag=3): %w", err)
		} // marker_bit
	}
	if p.escrFlag == 1 {
		if err = bb.Skip(2); err != nil {
			return fmt.Errorf("failed to skip in pes: reserved(EscrFlag=1): %w", err)
		} // reserved
		var first, second, third uint64
		if first, err = bb.ReadUint64(3); err != nil {
			return fmt.Errorf("failed to read pes escr first: %w", err)
		}
		p.escrBase = first << 30
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: first ercr marker_bit: %w", err)
		} // marker_bit
		if second, err = bb.ReadUint64(15); err != nil {
			return fmt.Errorf("failed to read pes escr second: %w", err)
		}
		p.escrBase |= second << 15
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: second ercr marker_bit: %w", err)
		} // marker_bit
		if third, err = bb.ReadUint64(15); err != nil {
			return fmt.Errorf("failed to read pes escr third: %w", err)
		}
		p.escrBase |= third
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: third escr marker_bit: %w", err)
		} // marker_bit
		if p.escrExtension, err = bb.ReadUint16(9); err != nil {
			return fmt.Errorf("failed to read pes escr_extension: %w", err)
		}
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: escr_extension marker_bit: %w", err)
		} // marker_bit
	}
	if p.esRateFlag == 1 {
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: first es_rate marker_bit: %w", err)
		} // marker_bit
		if p.esRate, err = bb.ReadUint32(22); err != nil {
			return fmt.Errorf("failed to read pes es_rate: %w", err)
		}
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: second es_rate marker_bit: %w", err)
		} // marker_bit
	}
	if p.dsmTrickModeFlag == 1 {
		if p.trickModeControl, err = bb.ReadUint8(3); err != nil {
			return fmt.Errorf("failed to read pes trick_mode_control: %w", err)
		}
		switch p.trickModeControl {
		case 0x00, 0x03: // fast_forward, freeze_frame
			if p.fieldID, err = bb.ReadUint8(2); err != nil {
				return fmt.Errorf("failed to read pes field_id: %w", err)
			}
			if p.intraSliceRefresh, err = bb.ReadUint8(1); err != nil {
				return fmt.Errorf("failed to read pes intra_slice_refresh: %w", err)
			}
			if p.frequencyTruncation, err = bb.ReadUint8(2); err != nil {
				return fmt.Errorf("failed to read pes frequency_truncation: %w", err)
			}
		case 0x01: // slow_motion, slow_reverse
			if p.repCntrl, err = bb.ReadUint8(5); err != nil {
				return fmt.Errorf("failed to read pes rep_cntrl: %w", err)
			}
		default:
			if err = bb.Skip(5); err != nil {
				return fmt.Errorf("failed to skip in pes: dsm_trick_mode reserved: %w", err)
			} // reserved
		}
	}
	if p.additionalCopyInfoFlag == 1 {
		if err = bb.Skip(1); err != nil {
			return fmt.Errorf("failed to skip in pes: additional_copy_info marker_bit: %w", err)
		} // marker_bit
		if p.additionalCopyInfo, err = bb.ReadUint8(7); err != nil {
			return fmt.Errorf("failed to read pes additional_copy_info: %w", err)
		}
	}
	if p.pesCrcFlag == 1 {
		if p.previousPesPacketCrc, err = bb.ReadUint16(16); err != nil {
			return fmt.Errorf("failed to read pes previous_pes_packet_crc: %w", err)
		}
	}
	return nil
}

// DumpTimestamp dump PTS and DTS
func (p *Pes) DumpTimestamp() float64 {
	switch p.ptsDtsFlags {
	case 2:
		return p.dumpTimestamp("PTS", p.pts)
	case 3:
		// ptsDtsFlags == 3 carries both PTS and DTS; the end-to-end delay is
		// measured against the DTS (when the access unit is decoded).
		return p.dumpTimestamp("DTS", p.dts)
	}
	return 0
}

// TimestampDelay returns the PCR-to-PTS/DTS delay in milliseconds when the PES
// has a timestamp and a preceding PCR reference. DTS is used when both are
// present because it represents the decode timeline.
func (p *Pes) TimestampDelay() (float64, bool) {
	var stamp uint64
	switch p.ptsDtsFlags {
	case 2:
		stamp = p.pts
	case 3:
		stamp = p.dts
	default:
		return 0, false
	}
	refPcr, ok := p.referencePcrMs()
	if !ok {
		return 0, false
	}
	return float64(stamp)/90 - refPcr, true
}

// BracketedTimestampDelay returns a delay only when PCR observations on both
// sides of the PES position are available. Compliance evaluation uses this
// stricter form because treating the previous PCR value as if it occurred at a
// later PES position overstates the delay near the end of a stream.
func (p *Pes) BracketedTimestampDelay() (float64, bool) {
	if p.nextPcr <= p.prevPcr || p.nextPcrPos <= p.prevPcrPos {
		return 0, false
	}
	return p.TimestampDelay()
}

// dumpTimestamp prints one PTS/DTS line and returns the PCR-to-timestamp delay
// (the end-to-end buffering delay) in milliseconds.
func (p *Pes) dumpTimestamp(label string, stamp uint64) float64 {
	stampMs := float64(stamp) / 90
	refPcr, ok := p.referencePcrMs()
	if !ok {
		fmt.Printf("0x%08x %s: 0x%08x[%012fms] (pid:0x%02x)\n", p.pos, label, stamp, stampMs, p.pid)
		return 0
	}
	pcrDelay := stampMs - refPcr
	fmt.Printf("0x%08x %s: 0x%08x[%012fms] (pid:0x%02x) (delay:%fms)\n", p.pos, label, stamp, stampMs, p.pid, pcrDelay)
	return pcrDelay
}

// referencePcrMs returns the reference PCR (in milliseconds) at this PES's byte
// position. When a following PCR was captured during the PES, the value is
// interpolated between the surrounding PCRs by byte position for a tighter
// estimate. When no following PCR exists (e.g. the final PES before EOF), it
// falls back to the most recent PCR — interpolating against an unset (zero)
// nextPcr would otherwise produce a wildly wrong delay. The bool is false when
// there is no preceding PCR to reference at all.
func (p *Pes) referencePcrMs() (float64, bool) {
	if p.prevPcr == 0 {
		return 0, false
	}
	prevPcr := float64(p.prevPcr) / 300 / 90
	if p.nextPcr > p.prevPcr && p.nextPcrPos > p.prevPcrPos {
		nextPcr := float64(p.nextPcr) / 300 / 90
		return prevPcr + (nextPcr-prevPcr)*(float64(p.pos-p.prevPcrPos)/float64(p.nextPcrPos-p.prevPcrPos)), true
	}
	return prevPcr, true
}

// Dump PES header detail.
// pesColonColumn is the column where the PES header dump aligns its colons.
const pesColonColumn = 48

// pesField prints one aligned "PES : <label> : <value>" line.
func pesField(label, format string, args ...interface{}) {
	dumpField("PES : ", pesColonColumn, label, format, args...)
}

func (p *Pes) DumpHeader() {
	fmt.Printf("\n===========================================\n")
	fmt.Printf(" PES")
	fmt.Printf("\n===========================================\n")
	pesField("packet_start_code_prefix", "%d", p.packetStartCodePrefix)
	pesField("stream_id", "%d", p.streamID)
	pesField("pes_packet_length", "%d", p.pesPacketLength)
	pesField("pes_scrambling_control", "%d", p.pesScramblingControl)
	pesField("pes_priority", "%d", p.pesPriority)
	pesField("data_alignment_indicator", "%d", p.dataAlignmentIndicator)
	pesField("copyright", "%d", p.copyright)
	pesField("original_or_copy", "%d", p.originalOrCopy)
	pesField("pts_dts_flags", "%d", p.ptsDtsFlags)
	pesField("escr_flag", "%d", p.escrFlag)
	pesField("es_rate_flag", "%d", p.esRateFlag)
	pesField("dsm_trick_mode_flag", "%d", p.dsmTrickModeFlag)
	pesField("additional_copy_info_flag", "%d", p.additionalCopyInfoFlag)
	pesField("pes_crc_flag", "%d", p.pesCrcFlag)
	pesField("pes_extension_flag", "%d", p.pesExtensionFlag)
	pesField("pes_header_data_length", "%d", p.pesHeaderDataLength)
	pesField("pts", "%d", p.pts)
	pesField("dts", "%d", p.dts)
	pesField("escr", "%d", p.escr)
	pesField("escr_base", "%d", p.escrBase)
	pesField("escr_extension", "%d", p.escrExtension)
	pesField("es_rate", "%d", p.esRate)
	pesField("trick_mode_control", "%d", p.trickModeControl)
	pesField("field_id", "%d", p.fieldID)
	pesField("intra_slice_refresh", "%d", p.intraSliceRefresh)
	pesField("frequency_truncation", "%d", p.frequencyTruncation)
	pesField("rep_cntrl", "%d", p.repCntrl)
	pesField("additional_copy_info", "%d", p.additionalCopyInfo)
	pesField("previous_pes_packet_crc", "%d", p.previousPesPacketCrc)
	pesField("pes_private_data_flag", "%d", p.pesPrivateDataFlag)
	pesField("pack_header_field_flag", "%d", p.packHeaderFieldFlag)
	pesField("program_packet_sequence_counter_flag", "%d", p.programPacketSequenceCounterFlag)
	pesField("p-std_buffer_flag", "%d", p.pStdBufferFlag)
	pesField("pes_extension_flag2", "%d", p.pesExtensionFlag2)
	pesField("program_packet_sequence_counter", "%d", p.programPacketSequenceCounter)
	pesField("mpeg1_mpeg2_identifier", "%d", p.mpeg1Mpeg2Identifier)
	pesField("original_stuff_length", "%d", p.originalStuffLength)
	pesField("p-std_buffer_scale", "%d", p.pStdBufferScale)
	pesField("p-std_buffer_size", "%d", p.pStdBufferSize)
	pesField("pes_extension_field_length", "%d", p.pesExtensionFieldLength)
}
