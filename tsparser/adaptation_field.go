package tsparser

import (
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/small-teton/mpeg-ts-analyzer/bitbuffer"
	"github.com/small-teton/mpeg-ts-analyzer/options"
)

// AdaptationField adaptation_field data.
type AdaptationField struct {
	pcr     uint64
	pos     int64
	options options.Options
	buf     []byte

	adaptationFieldLength                  uint8
	discontinuityIndicator                 uint8
	randomAccessIndicator                  uint8
	elementaryStreamPriorityIndicator      uint8
	pcrFlag                                uint8
	oPcrFlag                               uint8
	splicingPointFlag                      uint8
	transportPrivateDataFlag               uint8
	adaptationFieldExtensionFlag           uint8
	programClockReferenceBase              uint64
	programClockReferenceExtension         uint16
	originalProgramClockReferenceBase      uint64
	originalProgramClockReferenceExtension uint16
	spliceCountdown                        uint8
	transportPrivateDataLength             uint8
	privateDataByte                        []byte
	adaptationFieldExtensionLength         uint8
	ltwFlag                                uint8
	piecewiseRateFlag                      uint8
	seamlessSpliceFlag                     uint8
	ltwValidFlag                           uint8
	ltwOffset                              uint16
	piecewiseRate                          uint32
	spliceType                             uint8
	dtsNextAu                              uint32
}

// NewAdaptationField create new adaptation_field instance.
func NewAdaptationField() *AdaptationField {
	af := new(AdaptationField)
	af.buf = make([]byte, 0, tsPacketSize)
	af.privateDataByte = make([]byte, 0, tsPacketSize)
	return af
}

// Initialize Set Params for TsPacket
func (af *AdaptationField) Initialize(pos int64, options options.Options) {
	af.pcr = 0
	af.pos = pos
	af.options = options
	af.buf = af.buf[0:0]

	af.adaptationFieldLength = 0
	af.discontinuityIndicator = 0
	af.randomAccessIndicator = 0
	af.elementaryStreamPriorityIndicator = 0
	af.pcrFlag = 0
	af.oPcrFlag = 0
	af.splicingPointFlag = 0
	af.transportPrivateDataFlag = 0
	af.adaptationFieldExtensionFlag = 0
	af.programClockReferenceBase = 0
	af.programClockReferenceExtension = 0
	af.originalProgramClockReferenceBase = 0
	af.originalProgramClockReferenceExtension = 0
	af.spliceCountdown = 0
	af.transportPrivateDataLength = 0
	af.privateDataByte = af.privateDataByte[0:0]
	af.adaptationFieldExtensionLength = 0
	af.ltwFlag = 0
	af.piecewiseRateFlag = 0
	af.seamlessSpliceFlag = 0
	af.ltwValidFlag = 0
	af.ltwOffset = 0
	af.piecewiseRate = 0
	af.spliceType = 0
	af.dtsNextAu = 0
}

// Append append adaptation_field data for buffer.
func (af *AdaptationField) Append(buf []byte) {
	af.buf = append(af.buf, buf...)
}

// PcrFlag return this adaptation_field PCR_flag.
func (af *AdaptationField) PcrFlag() bool { return af.pcrFlag == 1 }

// Pcr return this adaptation_field PCR.
func (af *AdaptationField) Pcr() uint64 { return af.pcr }

// Parse parse adaptation_field data.
func (af *AdaptationField) Parse() (uint8, error) {
	bb := new(bitbuffer.BitBuffer)
	bb.Set(af.buf)

	var err error
	if af.adaptationFieldLength, err = bb.PeekUint8(8); err != nil {
		return 0, errors.Wrap(err, "failed peek adaptation_fields adaptation_field_length")
	}
	if af.adaptationFieldLength <= 0 {
		return 0, nil
	}
	if af.discontinuityIndicator, err = bb.PeekUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed peek adaptation_fields discontinuity_indicator")
	}
	if af.randomAccessIndicator, err = bb.PeekUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed peek adaptation_fields randomAccess_indicator")
	}
	if af.elementaryStreamPriorityIndicator, err = bb.PeekUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed peek adaptation_fields elementary_stream_priority_indicator")
	}
	if af.pcrFlag, err = bb.PeekUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed peek adaptation_fields pcr_flag")
	}
	if af.oPcrFlag, err = bb.PeekUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed peek adaptation_fields o_pcr_flag")
	}
	if af.splicingPointFlag, err = bb.PeekUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed peek adaptation_fields splicing_point_flag")
	}
	if af.transportPrivateDataFlag, err = bb.PeekUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed peek adaptation_fields transport_private_data_flag")
	}
	if af.adaptationFieldExtensionFlag, err = bb.PeekUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed peek adaptation_fields adaptation_field_extension_flag")
	}
	if af.pcrFlag == 1 {
		if af.programClockReferenceBase, err = bb.PeekUint64(33); err != nil {
			return 0, errors.Wrap(err, "failed peek adaptation_fields program_clock_reference_base")
		}
		if err = bb.Skip(6); err != nil {
			return 0, errors.Wrap(err, "failed to skip in adaptation_fields: reserved")
		} // reserved
		if af.programClockReferenceExtension, err = bb.PeekUint16(9); err != nil {
			return 0, errors.Wrap(err, "failed peek adaptation_fields program_clock_reference_extension")
		}

		pcrBase := af.programClockReferenceBase
		pcrExt := uint64(af.programClockReferenceExtension)
		af.pcr = pcrBase*300 + pcrExt
	}
	if af.oPcrFlag == 1 {
		if af.originalProgramClockReferenceBase, err = bb.PeekUint64(33); err != nil {
			return 0, errors.Wrap(err, "failed peek adaptation_fields original_program_clock_reference_base")
		}
		bb.Skip(6) // reserved
		if af.originalProgramClockReferenceExtension, err = bb.PeekUint16(9); err != nil {
			return 0, errors.Wrap(err, "failed peek adaptation_fields original_program_clock_reference_extension")
		}
	}
	if af.splicingPointFlag == 1 {
		if af.spliceCountdown, err = bb.PeekUint8(8); err != nil {
			return 0, errors.Wrap(err, "failed peek adaptation_fields splice_countdown")
		}
	}
	if af.transportPrivateDataFlag == 1 {
		if af.transportPrivateDataLength, err = bb.PeekUint8(8); err != nil {
			return 0, errors.Wrap(err, "failed peek adaptation_fields transport_private_data_length")
		}
		for i := uint8(0); i < af.transportPrivateDataLength; i++ {
			chunk, err := bb.PeekUint8(8)
			if err != nil {
				return 0, errors.Wrap(err, "failed peek adaptation_fields transport_private_data chunk")
			}
			af.privateDataByte = append(af.privateDataByte, chunk)
		}
	}
	if af.adaptationFieldExtensionFlag == 1 {
		if af.adaptationFieldExtensionLength, err = bb.PeekUint8(8); err != nil {
			return 0, errors.Wrap(err, "failed peek adaptation_fields adaptation_field_extension_length")
		}
		if af.ltwFlag, err = bb.PeekUint8(1); err != nil {
			return 0, errors.Wrap(err, "failed peek adaptation_fields ltw_flag")
		}
		if af.piecewiseRateFlag, err = bb.PeekUint8(1); err != nil {
			return 0, errors.Wrap(err, "failed peek adaptation_fields piecewise_rate_flag")
		}
		if af.seamlessSpliceFlag, err = bb.PeekUint8(1); err != nil {
			return 0, errors.Wrap(err, "failed peek adaptation_fields seamless_splice_flag")
		}
		if err := bb.Skip(5); err != nil {
			return 0, errors.Wrap(err, "failed to skip in adaptation_fields: reserved")
		} // reserved
		if af.ltwFlag == 1 {
			if af.ltwValidFlag, err = bb.PeekUint8(1); err != nil {
				return 0, errors.Wrap(err, "failed peek adaptation_fields ltw_valid_flag")
			}
			if af.ltwOffset, err = bb.PeekUint16(15); err != nil {
				return 0, errors.Wrap(err, "failed peek adaptation_fields ltw_offset")
			}
		}
		if af.piecewiseRateFlag == 1 {
			if err := bb.Skip(2); err != nil {
				return 0, errors.Wrap(err, "failed to skip in adaptation_fields: reserved")
			} // reserved
			if af.piecewiseRate, err = bb.PeekUint32(22); err != nil {
				return 0, errors.Wrap(err, "failed peek adaptation_fields piecewise_rate")
			}
		}
		if af.seamlessSpliceFlag == 1 {
			if af.spliceType, err = bb.PeekUint8(4); err != nil {
				return 0, errors.Wrap(err, "failed peek adaptation_fields splice_type")
			}
			if af.dtsNextAu, err = bb.PeekUint32(3); err != nil {
				return 0, errors.Wrap(err, "failed peek adaptation_fields dts_next_au first")
			}
			af.dtsNextAu <<= 30
			if err := bb.Skip(1); err != nil {
				return 0, errors.Wrap(err, "failed to skip in adaptation_fields dts_next_au: first")
			} // marker_bit
			second, err := bb.PeekUint32(15)
			if err != nil {
				return 0, errors.Wrap(err, "failed peek adaptation_fields dts_next_au second")
			}
			af.dtsNextAu |= second << 15
			if err := bb.Skip(1); err != nil {
				return 0, errors.Wrap(err, "failed to skip in adaptation_fields dts_next_au: second")
			} // marker_bit
			third, err := bb.PeekUint32(15)
			if err != nil {
				return 0, errors.Wrap(err, "failed peek adaptation_fields dts_next_au third")
			}
			af.dtsNextAu |= third
			if err := bb.Skip(1); err != nil {
				return 0, errors.Wrap(err, "failed to skip in adaptation_fields dts_next_au: third")
			} // marker_bit
		}
	}

	return af.adaptationFieldLength, nil
}

// DumpPcr print PCR.
func (af *AdaptationField) DumpPcr(prevPcr uint64) {
	if af.pcrFlag == 1 {
		pcrMilisec := float64(af.pcr) / 300 / 90
		pcrInterval := float64(af.pcr-prevPcr) / 300 / 90
		fmt.Printf("0x%08x PCR: 0x%08x[%012fms] (Interval:%012fms)\n", af.pos, af.pcr, pcrMilisec, pcrInterval)
	}
}

// Dump adaptation_field detail.
func (af *AdaptationField) Dump() {
	fmt.Printf("\n===========================================\n")
	fmt.Printf(" Adaptation Field")
	fmt.Printf("\n===========================================\n")
	fmt.Printf("Adaptation Field : adaptation_field_length			: %d\n", af.adaptationFieldLength)
	if af.adaptationFieldLength <= 0 {
		return
	}
	fmt.Printf("Adaptation Field : discontinuity_indicator			: %d\n", af.discontinuityIndicator)
	fmt.Printf("Adaptation Field : random_access_indicator			: %d\n", af.randomAccessIndicator)
	fmt.Printf("Adaptation Field : elementary_stream_priority_indicator		: %d\n", af.elementaryStreamPriorityIndicator)
	fmt.Printf("Adaptation Field : PCR_flag					: %d\n", af.pcrFlag)
	fmt.Printf("Adaptation Field : OPCR_flag					: %d\n", af.oPcrFlag)
	fmt.Printf("Adaptation Field : splicing_point_flag				: %d\n", af.splicingPointFlag)
	fmt.Printf("Adaptation Field : adaptation_field_extension_flag		: %d\n", af.adaptationFieldExtensionFlag)
	if af.pcrFlag == 1 {
		fmt.Printf("Adaptation Field : program_clock_reference_base			: %d\n", af.programClockReferenceBase)
		fmt.Printf("Adaptation Field : program_clock_reference_extension		: %d\n", af.programClockReferenceExtension)
		pcrBase := af.programClockReferenceBase
		pcrExt := uint64(af.programClockReferenceExtension)
		fmt.Printf("Adaptation Field : PCR 0x%x[%fms]\n", pcrBase*300+pcrExt, float64(pcrBase*300+pcrExt)/300/90)

	}
	if af.oPcrFlag == 1 {
		fmt.Printf("Adaptation Field : original_program_clock_reference_base	: %d\n", af.originalProgramClockReferenceBase)
		fmt.Printf("Adaptation Field : original_program_clock_reference_extension	: %d\n", af.originalProgramClockReferenceExtension)
	}
	if af.splicingPointFlag == 1 {
		fmt.Printf("Adaptation Field : splice_countdown				: %d\n", af.spliceCountdown)
	}
	if af.transportPrivateDataFlag == 1 {
		fmt.Printf("Adaptation Field : transport_private_data_length		: %d\n", af.transportPrivateDataLength)
	}
	if af.adaptationFieldExtensionFlag == 1 {
		fmt.Printf("Adaptation Field : adaptation_field_extension_length		: %d\n", af.adaptationFieldExtensionLength)
		fmt.Printf("Adaptation Field : ltw_flag					: %d\n", af.ltwFlag)
		fmt.Printf("Adaptation Field : piecewise_rate_flag				: %d\n", af.piecewiseRateFlag)
		fmt.Printf("Adaptation Field : seamless_splice_flag				: %d\n", af.seamlessSpliceFlag)
		if af.ltwFlag == 1 {
			fmt.Printf("Adaptation Field : ltw_valid_flag				: %d\n", af.ltwValidFlag)
			fmt.Printf("Adaptation Field : ltw_offset					: %d\n", af.ltwOffset)
		}
		if af.piecewiseRateFlag == 1 {
			fmt.Printf("Adaptation Field : piecewise_rate				: %d\n", af.piecewiseRate)
		}
		if af.seamlessSpliceFlag == 1 {
			fmt.Printf("Adaptation Field : splice_type					: %d\n", af.spliceType)
			fmt.Printf("Adaptation Field : DTS_next_AU					: %d\n", af.dtsNextAu)
		}
	}
}
