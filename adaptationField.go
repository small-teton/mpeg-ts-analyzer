package main

import (
	"fmt"
)

type AdaptationField struct {
	pcr uint64

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
	privateDataByte                        []uint8
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

func NewAdaptationField() *AdaptationField { return new(AdaptationField) }

func (this *AdaptationField) PcrFlag() bool { return this.pcrFlag == 1 }
func (this *AdaptationField) Pcr() uint64   { return this.pcr }

func (this *AdaptationField) Parse(buf []byte, pos int64, prevPcr *uint64, npt bool) (uint8, error) {
	bb := new(BitBuffer)
	bb.Set(buf)

	var err error
	if this.adaptationFieldLength, err = bb.PeekUint8(8); err != nil {
		return 0, err
	}
	if this.adaptationFieldLength <= 0 {
		return 0, nil
	}
	if this.discontinuityIndicator, err = bb.PeekUint8(1); err != nil {
		return 0, err
	}
	if this.randomAccessIndicator, err = bb.PeekUint8(1); err != nil {
		return 0, err
	}
	if this.elementaryStreamPriorityIndicator, err = bb.PeekUint8(1); err != nil {
		return 0, err
	}
	if this.pcrFlag, err = bb.PeekUint8(1); err != nil {
		return 0, err
	}
	if this.oPcrFlag, err = bb.PeekUint8(1); err != nil {
		return 0, err
	}
	if this.splicingPointFlag, err = bb.PeekUint8(1); err != nil {
		return 0, err
	}
	if this.transportPrivateDataFlag, err = bb.PeekUint8(1); err != nil {
		return 0, err
	}
	if this.adaptationFieldExtensionFlag, err = bb.PeekUint8(1); err != nil {
		return 0, err
	}
	if this.pcrFlag == 1 {
		if this.programClockReferenceBase, err = bb.PeekUint64(33); err != nil {
			return 0, err
		}
		bb.Skip(6) // reserved
		if this.programClockReferenceExtension, err = bb.PeekUint16(9); err != nil {
			return 0, err
		}

		pcrBase := this.programClockReferenceBase
		pcrExt := uint64(this.programClockReferenceExtension)
		this.pcr = pcrBase*300 + pcrExt
		pcrMilisec := float64(this.pcr) / 300 / 90
		pcrInterval := float64(this.pcr-*prevPcr) / 300 / 90
		if !npt {
			fmt.Printf("0x%08x PCR: 0x%08x[%012fms] (Interval:%012fms)\n", pos, this.pcr, pcrMilisec, pcrInterval)
		}
		*prevPcr = this.pcr
	}
	if this.oPcrFlag == 1 {
		if this.originalProgramClockReferenceBase, err = bb.PeekUint64(33); err != nil {
			return 0, err
		}
		bb.Skip(6) // reserved
		if this.originalProgramClockReferenceExtension, err = bb.PeekUint16(9); err != nil {
			return 0, err
		}
	}
	if this.splicingPointFlag == 1 {
		if this.spliceCountdown, err = bb.PeekUint8(8); err != nil {
			return 0, err
		}
	}
	if this.transportPrivateDataFlag == 1 {
		if this.transportPrivateDataLength, err = bb.PeekUint8(8); err != nil {
			return 0, err
		}
		for i := uint8(0); i < this.transportPrivateDataLength; i++ {
			chunk, err := bb.PeekUint8(8)
			if err != nil {
				return 0, err
			}
			this.privateDataByte = append(this.privateDataByte, chunk)
		}
	}
	if this.adaptationFieldExtensionFlag == 1 {
		if this.adaptationFieldExtensionLength, err = bb.PeekUint8(8); err != nil {
			return 0, err
		}
		if this.ltwFlag, err = bb.PeekUint8(1); err != nil {
			return 0, err
		}
		if this.piecewiseRateFlag, err = bb.PeekUint8(1); err != nil {
			return 0, err
		}
		if this.seamlessSpliceFlag, err = bb.PeekUint8(1); err != nil {
			return 0, err
		}
		if err := bb.Skip(5); err != nil {
			return 0, err
		} // reserved
		if this.ltwFlag == 1 {
			if this.ltwValidFlag, err = bb.PeekUint8(1); err != nil {
				return 0, err
			}
			if this.ltwOffset, err = bb.PeekUint16(15); err != nil {
				return 0, err
			}
		}
		if this.piecewiseRateFlag == 1 {
			if err := bb.Skip(2); err != nil {
				return 0, err
			} // reserved
			if this.piecewiseRate, err = bb.PeekUint32(22); err != nil {
				return 0, err
			}
		}
		if this.seamlessSpliceFlag == 1 {
			if this.spliceType, err = bb.PeekUint8(4); err != nil {
				return 0, err
			}
			if this.dtsNextAu, err = bb.PeekUint32(3); err != nil {
				return 0, err
			}
			this.dtsNextAu <<= 30
			if err := bb.Skip(1); err != nil {
				return 0, err
			} // marker_bit
			second, err := bb.PeekUint32(15)
			if err != nil {
				return 0, err
			}
			this.dtsNextAu |= second << 15
			if err := bb.Skip(1); err != nil {
				return 0, err
			} // marker_bit
			third, err := bb.PeekUint32(15)
			if err != nil {
				return 0, err
			}
			this.dtsNextAu |= third
			if err := bb.Skip(1); err != nil {
				return 0, err
			} // marker_bit
		}
	}

	return this.adaptationFieldLength, nil
}

func (this *AdaptationField) Dump() {
	fmt.Printf("Adaptation Field : adaptation_field_length			: %d\n", this.adaptationFieldLength)
	if this.adaptationFieldLength <= 0 {
		return
	}
	fmt.Printf("Adaptation Field : discontinuity_indicator			: %d\n", this.discontinuityIndicator)
	fmt.Printf("Adaptation Field : random_access_indicator			: %d\n", this.randomAccessIndicator)
	fmt.Printf("Adaptation Field : elementary_stream_priority_indicator		: %d\n", this.elementaryStreamPriorityIndicator)
	fmt.Printf("Adaptation Field : PCR_flag					: %d\n", this.pcrFlag)
	fmt.Printf("Adaptation Field : OPCR_flag					: %d\n", this.oPcrFlag)
	fmt.Printf("Adaptation Field : splicing_point_flag				: %d\n", this.splicingPointFlag)
	fmt.Printf("Adaptation Field : adaptation_field_extension_flag		: %d\n", this.adaptationFieldExtensionFlag)
	if this.pcrFlag == 1 {
		fmt.Printf("Adaptation Field : program_clock_reference_base			: %d\n", this.programClockReferenceBase)
		fmt.Printf("Adaptation Field : program_clock_reference_extension		: %d\n", this.programClockReferenceExtension)
		pcrBase := this.programClockReferenceBase
		pcrExt := uint64(this.programClockReferenceExtension)
		fmt.Printf("Adaptation Field : PCR 0x%x[%fms]\n", pcrBase*300+pcrExt, float64(pcrBase*300+pcrExt)/300/90)

	}
	if this.oPcrFlag == 1 {
		fmt.Printf("Adaptation Field : original_program_clock_reference_base	: %d\n", this.originalProgramClockReferenceBase)
		fmt.Printf("Adaptation Field : original_program_clock_reference_extension	: %d\n", this.originalProgramClockReferenceExtension)
	}
	if this.splicingPointFlag == 1 {
		fmt.Printf("Adaptation Field : splice_countdown				: %d\n", this.spliceCountdown)
	}
	if this.transportPrivateDataFlag == 1 {
		fmt.Printf("Adaptation Field : transport_private_data_length		: %d\n", this.transportPrivateDataLength)
	}
	if this.adaptationFieldExtensionFlag == 1 {
		fmt.Printf("Adaptation Field : adaptation_field_extension_length		: %d\n", this.adaptationFieldExtensionLength)
		fmt.Printf("Adaptation Field : ltw_flag					: %d\n", this.ltwFlag)
		fmt.Printf("Adaptation Field : piecewise_rate_flag				: %d\n", this.piecewiseRateFlag)
		fmt.Printf("Adaptation Field : seamless_splice_flag				: %d\n", this.seamlessSpliceFlag)
		if this.ltwFlag == 1 {
			fmt.Printf("Adaptation Field : ltw_valid_flag				: %d\n", this.ltwValidFlag)
			fmt.Printf("Adaptation Field : ltw_offset					: %d\n", this.ltwOffset)
		}
		if this.piecewiseRateFlag == 1 {
			fmt.Printf("Adaptation Field : piecewise_rate				: %d\n", this.piecewiseRate)
		}
		if this.seamlessSpliceFlag == 1 {
			fmt.Printf("Adaptation Field : splice_type					: %d\n", this.spliceType)
			fmt.Printf("Adaptation Field : DTS_next_AU					: %d\n", this.dtsNextAu)
		}
	}
}
