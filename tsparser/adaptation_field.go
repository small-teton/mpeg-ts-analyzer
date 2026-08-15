package tsparser

import (
	"fmt"

	"github.com/cockroachdb/errors"
	"github.com/small-teton/mpeg-ts-analyzer/options"
)

// AdaptationField adaptation_field data.
type AdaptationField struct {
	pcr          uint64
	pos          int64
	options      options.Options
	buf          []byte
	newBitReader func() bitReader

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
	dtsNextAu                              uint64
}

// NewAdaptationField create new adaptation_field instance.
func NewAdaptationField() *AdaptationField {
	af := new(AdaptationField)
	af.buf = make([]byte, 0, tsPayloadSize)
	af.privateDataByte = make([]byte, 0, tsPayloadSize)
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

// DiscontinuityIndicator return this adaptation_field discontinuity_indicator.
func (af *AdaptationField) DiscontinuityIndicator() bool { return af.discontinuityIndicator == 1 }

// Parse parse adaptation_field data.
func (af *AdaptationField) Parse() (uint8, error) {
	var bb bitReader
	if af.newBitReader != nil {
		bb = af.newBitReader()
	} else {
		bb = newDefaultBitReader()
	}
	bb.Set(af.buf)

	var err error
	if af.adaptationFieldLength, err = bb.ReadUint8(8); err != nil {
		return 0, errors.Wrap(err, "failed to read adaptation_fields adaptation_field_length")
	}
	if af.adaptationFieldLength <= 0 {
		return 0, nil
	}
	if af.discontinuityIndicator, err = bb.ReadUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed to read adaptation_fields discontinuity_indicator")
	}
	if af.randomAccessIndicator, err = bb.ReadUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed to read adaptation_fields randomAccess_indicator")
	}
	if af.elementaryStreamPriorityIndicator, err = bb.ReadUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed to read adaptation_fields elementary_stream_priority_indicator")
	}
	if af.pcrFlag, err = bb.ReadUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed to read adaptation_fields pcr_flag")
	}
	if af.oPcrFlag, err = bb.ReadUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed to read adaptation_fields o_pcr_flag")
	}
	if af.splicingPointFlag, err = bb.ReadUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed to read adaptation_fields splicing_point_flag")
	}
	if af.transportPrivateDataFlag, err = bb.ReadUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed to read adaptation_fields transport_private_data_flag")
	}
	if af.adaptationFieldExtensionFlag, err = bb.ReadUint8(1); err != nil {
		return 0, errors.Wrap(err, "failed to read adaptation_fields adaptation_field_extension_flag")
	}
	if af.pcrFlag == 1 {
		if af.programClockReferenceBase, err = bb.ReadUint64(33); err != nil {
			return 0, errors.Wrap(err, "failed to read adaptation_fields program_clock_reference_base")
		}
		if err = bb.Skip(6); err != nil {
			return 0, errors.Wrap(err, "failed to skip in adaptation_fields: reserved")
		} // reserved
		if af.programClockReferenceExtension, err = bb.ReadUint16(9); err != nil {
			return 0, errors.Wrap(err, "failed to read adaptation_fields program_clock_reference_extension")
		}

		pcrBase := af.programClockReferenceBase
		pcrExt := uint64(af.programClockReferenceExtension)
		af.pcr = pcrBase*300 + pcrExt
	}
	if af.oPcrFlag == 1 {
		if af.originalProgramClockReferenceBase, err = bb.ReadUint64(33); err != nil {
			return 0, errors.Wrap(err, "failed to read adaptation_fields original_program_clock_reference_base")
		}
		_ = bb.Skip(6) // reserved
		if af.originalProgramClockReferenceExtension, err = bb.ReadUint16(9); err != nil {
			return 0, errors.Wrap(err, "failed to read adaptation_fields original_program_clock_reference_extension")
		}
	}
	if af.splicingPointFlag == 1 {
		if af.spliceCountdown, err = bb.ReadUint8(8); err != nil {
			return 0, errors.Wrap(err, "failed to read adaptation_fields splice_countdown")
		}
	}
	if af.transportPrivateDataFlag == 1 {
		if af.transportPrivateDataLength, err = bb.ReadUint8(8); err != nil {
			return 0, errors.Wrap(err, "failed to read adaptation_fields transport_private_data_length")
		}
		for i := uint8(0); i < af.transportPrivateDataLength; i++ {
			chunk, err := bb.ReadUint8(8)
			if err != nil {
				return 0, errors.Wrap(err, "failed to read adaptation_fields transport_private_data chunk")
			}
			af.privateDataByte = append(af.privateDataByte, chunk)
		}
	}
	if af.adaptationFieldExtensionFlag == 1 {
		if af.adaptationFieldExtensionLength, err = bb.ReadUint8(8); err != nil {
			return 0, errors.Wrap(err, "failed to read adaptation_fields adaptation_field_extension_length")
		}
		if af.ltwFlag, err = bb.ReadUint8(1); err != nil {
			return 0, errors.Wrap(err, "failed to read adaptation_fields ltw_flag")
		}
		if af.piecewiseRateFlag, err = bb.ReadUint8(1); err != nil {
			return 0, errors.Wrap(err, "failed to read adaptation_fields piecewise_rate_flag")
		}
		if af.seamlessSpliceFlag, err = bb.ReadUint8(1); err != nil {
			return 0, errors.Wrap(err, "failed to read adaptation_fields seamless_splice_flag")
		}
		if err := bb.Skip(5); err != nil {
			return 0, errors.Wrap(err, "failed to skip in adaptation_fields: reserved")
		} // reserved
		if af.ltwFlag == 1 {
			if af.ltwValidFlag, err = bb.ReadUint8(1); err != nil {
				return 0, errors.Wrap(err, "failed to read adaptation_fields ltw_valid_flag")
			}
			if af.ltwOffset, err = bb.ReadUint16(15); err != nil {
				return 0, errors.Wrap(err, "failed to read adaptation_fields ltw_offset")
			}
		}
		if af.piecewiseRateFlag == 1 {
			if err := bb.Skip(2); err != nil {
				return 0, errors.Wrap(err, "failed to skip in adaptation_fields: reserved")
			} // reserved
			if af.piecewiseRate, err = bb.ReadUint32(22); err != nil {
				return 0, errors.Wrap(err, "failed to read adaptation_fields piecewise_rate")
			}
		}
		if af.seamlessSpliceFlag == 1 {
			if af.spliceType, err = bb.ReadUint8(4); err != nil {
				return 0, errors.Wrap(err, "failed to read adaptation_fields splice_type")
			}
			// DTS_next_AU is 33 bits (3 + 15 + 15); it must not be truncated to 32.
			first, err := bb.ReadUint32(3)
			if err != nil {
				return 0, errors.Wrap(err, "failed to read adaptation_fields dts_next_au first")
			}
			af.dtsNextAu = uint64(first) << 30
			if err := bb.Skip(1); err != nil {
				return 0, errors.Wrap(err, "failed to skip in adaptation_fields dts_next_au: first")
			} // marker_bit
			second, err := bb.ReadUint32(15)
			if err != nil {
				return 0, errors.Wrap(err, "failed to read adaptation_fields dts_next_au second")
			}
			af.dtsNextAu |= uint64(second) << 15
			if err := bb.Skip(1); err != nil {
				return 0, errors.Wrap(err, "failed to skip in adaptation_fields dts_next_au: second")
			} // marker_bit
			third, err := bb.ReadUint32(15)
			if err != nil {
				return 0, errors.Wrap(err, "failed to read adaptation_fields dts_next_au third")
			}
			af.dtsNextAu |= uint64(third)
			if err := bb.Skip(1); err != nil {
				return 0, errors.Wrap(err, "failed to skip in adaptation_fields dts_next_au: third")
			} // marker_bit
		}
	}

	return af.adaptationFieldLength, nil
}

// DumpPcr prints PCR. If prevPcr is non-zero, the interval is also shown.
func (af *AdaptationField) DumpPcr(prevPcr uint64) {
	if af.pcrFlag == 1 {
		pcrMilisec := float64(af.pcr) / 300 / 90
		if prevPcr != 0 {
			pcrInterval := float64(af.pcr-prevPcr) / 300 / 90
			fmt.Printf("0x%08x PCR: 0x%08x[%012fms] (Interval:%012fms)\n", af.pos, af.pcr, pcrMilisec, pcrInterval)
		} else {
			fmt.Printf("0x%08x PCR: 0x%08x[%012fms]\n", af.pos, af.pcr, pcrMilisec)
		}
	}
}

// afColonColumn is the column where the adaptation_field dump aligns its colons.
const afColonColumn = 64

// afField prints one aligned "Adaptation Field : <label> : <value>" line.
func afField(label, format string, args ...interface{}) {
	dumpField("Adaptation Field : ", afColonColumn, label, format, args...)
}

// Dump adaptation_field detail.
func (af *AdaptationField) Dump() {
	fmt.Printf("\n===========================================\n")
	fmt.Printf(" Adaptation Field")
	fmt.Printf("\n===========================================\n")
	afField("adaptation_field_length", "%d", af.adaptationFieldLength)
	if af.adaptationFieldLength <= 0 {
		return
	}
	afField("discontinuity_indicator", "%d", af.discontinuityIndicator)
	afField("random_access_indicator", "%d", af.randomAccessIndicator)
	afField("elementary_stream_priority_indicator", "%d", af.elementaryStreamPriorityIndicator)
	afField("PCR_flag", "%d", af.pcrFlag)
	afField("OPCR_flag", "%d", af.oPcrFlag)
	afField("splicing_point_flag", "%d", af.splicingPointFlag)
	afField("adaptation_field_extension_flag", "%d", af.adaptationFieldExtensionFlag)
	if af.pcrFlag == 1 {
		afField("program_clock_reference_base", "%d", af.programClockReferenceBase)
		afField("program_clock_reference_extension", "%d", af.programClockReferenceExtension)
		pcrBase := af.programClockReferenceBase
		pcrExt := uint64(af.programClockReferenceExtension)
		fmt.Printf("Adaptation Field : PCR 0x%x[%fms]\n", pcrBase*300+pcrExt, float64(pcrBase*300+pcrExt)/300/90)

	}
	if af.oPcrFlag == 1 {
		afField("original_program_clock_reference_base", "%d", af.originalProgramClockReferenceBase)
		afField("original_program_clock_reference_extension", "%d", af.originalProgramClockReferenceExtension)
	}
	if af.splicingPointFlag == 1 {
		afField("splice_countdown", "%d", af.spliceCountdown)
	}
	if af.transportPrivateDataFlag == 1 {
		afField("transport_private_data_length", "%d", af.transportPrivateDataLength)
	}
	if af.adaptationFieldExtensionFlag == 1 {
		afField("adaptation_field_extension_length", "%d", af.adaptationFieldExtensionLength)
		afField("ltw_flag", "%d", af.ltwFlag)
		afField("piecewise_rate_flag", "%d", af.piecewiseRateFlag)
		afField("seamless_splice_flag", "%d", af.seamlessSpliceFlag)
		if af.ltwFlag == 1 {
			afField("ltw_valid_flag", "%d", af.ltwValidFlag)
			afField("ltw_offset", "%d", af.ltwOffset)
		}
		if af.piecewiseRateFlag == 1 {
			afField("piecewise_rate", "%d", af.piecewiseRate)
		}
		if af.seamlessSpliceFlag == 1 {
			afField("splice_type", "%d", af.spliceType)
			afField("DTS_next_AU", "%d", af.dtsNextAu)
		}
	}
}
