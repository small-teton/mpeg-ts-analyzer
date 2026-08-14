package tsparser

import (
	"fmt"

	"github.com/cockroachdb/errors"
)

// parseDescriptors reads descriptors covering exactly totalLength bytes from bb.
// It always consumes totalLength bytes so the surrounding parser stays aligned,
// even if the descriptor loop is malformed.
func parseDescriptors(bb bitReader, totalLength uint16) ([]Descriptor, error) {
	var descriptors []Descriptor
	remaining := int(totalLength)
	for remaining >= 2 {
		tag, err := bb.ReadUint8(8)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read descriptor_tag")
		}
		length, err := bb.ReadUint8(8)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read descriptor_length")
		}
		remaining -= 2
		n := int(length)
		if n > remaining {
			// Malformed length: clamp to what the ES info loop actually holds.
			n = remaining
		}
		data := make([]byte, n)
		for i := 0; i < n; i++ {
			if data[i], err = bb.ReadUint8(8); err != nil {
				return nil, errors.Wrap(err, "failed to read descriptor payload")
			}
		}
		remaining -= n
		descriptors = append(descriptors, Descriptor{tag: tag, data: data})
	}
	if remaining > 0 {
		if err := bb.Skip(8 * uint32(remaining)); err != nil {
			return nil, errors.Wrap(err, "failed to skip trailing descriptor bytes")
		}
	}
	return descriptors, nil
}

// Descriptor represents a single descriptor found in a PMT ES info loop.
// It keeps the raw tag/data so unknown descriptors can still be reported.
type Descriptor struct {
	tag  uint8
	data []byte
}

// Tag returns the descriptor_tag.
func (d Descriptor) Tag() uint8 { return d.tag }

// Data returns the raw descriptor payload (excluding tag and length).
func (d Descriptor) Data() []byte { return d.data }

// Descriptor detail lines are printed under the "PMT : Program Info" line they
// belong to, so their ':' is aligned with that line's value column. The
// "PMT : Program Info : elementary_PID" label is 35 columns wide and is followed
// by a tab, putting the Program Info value ':' at column 40. descFieldIndent is
// 10 columns and field names are padded to descFieldWidth (30) so the detail
// ':' also lands at column 40 (10 + 30).
const (
	descHeaderPrefix = "PMT :   descriptor : "
	descFieldIndent  = "PMT :     "
	descFieldWidth   = 30
)

// descField prints one "PMT :     <name> : <value>" line with the value
// formatted from format/args, keeping the ':' aligned at column 40.
func descField(name, format string, args ...interface{}) {
	fmt.Printf("%s%-*s: %s\n", descFieldIndent, descFieldWidth, name, fmt.Sprintf(format, args...))
}

// Dump prints the descriptor detail in the --dump-psi output format.
func (d Descriptor) Dump() {
	switch d.tag {
	case 0x05:
		d.dumpRegistration()
	case 0x0A:
		d.dumpISO639Language()
	case 0x28:
		d.dumpAVCVideo()
	case 0x38:
		d.dumpHEVCVideo()
	case 0x56:
		d.dumpTeletext()
	case 0x59:
		d.dumpSubtitling()
	case 0x7C:
		d.dumpAACAudio()
	default:
		fmt.Printf("%sunknown descriptor (tag 0x%02X)\n", descHeaderPrefix, d.tag)
		descField("raw", "% X", d.data)
	}
}

// dumpRegistration handles tag 0x05 (registration_descriptor).
func (d Descriptor) dumpRegistration() {
	fmt.Printf("%sRegistration descriptor\n", descHeaderPrefix)
	if len(d.data) < 4 {
		descField("format_identifier", "(truncated)")
		return
	}
	id := d.data[0:4]
	descField("format_identifier", "%s (0x%08X)", printableASCII(id), uint32(id[0])<<24|uint32(id[1])<<16|uint32(id[2])<<8|uint32(id[3]))
	if len(d.data) > 4 {
		descField("additional_info", "% X", d.data[4:])
	}
}

// dumpISO639Language handles tag 0x0A (ISO_639_language_descriptor).
func (d Descriptor) dumpISO639Language() {
	fmt.Printf("%sISO 639 language descriptor\n", descHeaderPrefix)
	for i := 0; i+4 <= len(d.data); i += 4 {
		lang := printableASCII(d.data[i : i+3])
		audioType := d.data[i+3]
		descField("language_code", "%s", lang)
		descField("audio_type", "0x%02X (%s)", audioType, audioTypeName(audioType))
	}
}

// dumpAVCVideo handles tag 0x28 (AVC_video_descriptor).
func (d Descriptor) dumpAVCVideo() {
	fmt.Printf("%sAVC video descriptor\n", descHeaderPrefix)
	if len(d.data) < 3 {
		descField("profile_idc", "(truncated)")
		return
	}
	profileIDC := d.data[0]
	levelIDC := d.data[2]
	descField("profile_idc", "%d (%s)", profileIDC, avcProfileName(profileIDC))
	descField("level_idc", "%d (%s)", levelIDC, avcLevelName(levelIDC))
}

// dumpHEVCVideo handles tag 0x38 (HEVC_video_descriptor).
func (d Descriptor) dumpHEVCVideo() {
	fmt.Printf("%sHEVC video descriptor\n", descHeaderPrefix)
	if len(d.data) < 12 {
		descField("profile_idc", "(truncated)")
		return
	}
	tierFlag := (d.data[0] >> 5) & 0x01
	profileIDC := d.data[0] & 0x1F
	levelIDC := d.data[12-1] // level_idc is the 12th byte (index 11)
	tier := "Main"
	if tierFlag == 1 {
		tier = "High"
	}
	descField("profile_idc", "%d (%s)", profileIDC, hevcProfileName(profileIDC))
	descField("tier_flag", "%d (%s tier)", tierFlag, tier)
	descField("level_idc", "%d (%s)", levelIDC, hevcLevelName(levelIDC))
}

// dumpAACAudio handles tag 0x7C (MPEG-4_AAC_descriptor / AAC_audio_descriptor).
func (d Descriptor) dumpAACAudio() {
	fmt.Printf("%sAAC audio descriptor\n", descHeaderPrefix)
	if len(d.data) < 2 {
		descField("profile_and_level", "(truncated)")
		return
	}
	profileAndLevel := d.data[0]
	aacTypeFlag := (d.data[1] >> 7) & 0x01
	descField("profile_and_level", "0x%02X", profileAndLevel)
	descField("AAC_type_flag", "%d", aacTypeFlag)
	if aacTypeFlag == 1 && len(d.data) >= 3 {
		descField("AAC_type", "0x%02X", d.data[2])
	}
}

// dumpTeletext handles tag 0x56 (teletext_descriptor).
func (d Descriptor) dumpTeletext() {
	fmt.Printf("%sTeletext descriptor\n", descHeaderPrefix)
	for i := 0; i+5 <= len(d.data); i += 5 {
		lang := printableASCII(d.data[i : i+3])
		teletextType := (d.data[i+3] >> 3) & 0x1F
		magazine := d.data[i+3] & 0x07
		page := d.data[i+4]
		descField("language_code", "%s", lang)
		descField("teletext_type", "0x%02X (%s)", teletextType, teletextTypeName(teletextType))
		descField("magazine_number", "%d", magazine)
		descField("page_number", "0x%02X", page)
	}
}

// dumpSubtitling handles tag 0x59 (subtitling_descriptor).
func (d Descriptor) dumpSubtitling() {
	fmt.Printf("%sSubtitling descriptor\n", descHeaderPrefix)
	for i := 0; i+8 <= len(d.data); i += 8 {
		lang := printableASCII(d.data[i : i+3])
		subtitlingType := d.data[i+3]
		compositionPageID := uint16(d.data[i+4])<<8 | uint16(d.data[i+5])
		ancillaryPageID := uint16(d.data[i+6])<<8 | uint16(d.data[i+7])
		descField("language_code", "%s", lang)
		descField("subtitling_type", "0x%02X (%s)", subtitlingType, subtitlingTypeName(subtitlingType))
		descField("composition_page_id", "0x%04X", compositionPageID)
		descField("ancillary_page_id", "0x%04X", ancillaryPageID)
	}
}

// printableASCII renders bytes as ASCII, replacing non-printable bytes with '.'.
func printableASCII(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 0x20 && c < 0x7F {
			out[i] = c
		} else {
			out[i] = '.'
		}
	}
	return string(out)
}

func audioTypeName(t uint8) string {
	switch t {
	case 0x00:
		return "undefined"
	case 0x01:
		return "clean effects"
	case 0x02:
		return "hearing impaired"
	case 0x03:
		return "visual impaired commentary"
	default:
		return "reserved"
	}
}

func avcProfileName(p uint8) string {
	switch p {
	case 66:
		return "Baseline"
	case 77:
		return "Main"
	case 88:
		return "Extended"
	case 100:
		return "High"
	case 110:
		return "High 10"
	case 122:
		return "High 4:2:2"
	case 244:
		return "High 4:4:4 Predictive"
	default:
		return "unknown"
	}
}

func avcLevelName(l uint8) string {
	// level_idc is 10 * level for AVC (e.g. 40 -> 4.0).
	return fmt.Sprintf("%d.%d", l/10, l%10)
}

func hevcProfileName(p uint8) string {
	switch p {
	case 1:
		return "Main"
	case 2:
		return "Main 10"
	case 3:
		return "Main Still Picture"
	case 4:
		return "Range Extensions"
	default:
		return "unknown"
	}
}

func hevcLevelName(l uint8) string {
	// general_level_idc is 30 * level for HEVC (e.g. 120 -> 4.0).
	whole := l / 30
	frac := (uint16(l%30) * 10) / 30
	return fmt.Sprintf("%d.%d", whole, frac)
}

func teletextTypeName(t uint8) string {
	switch t {
	case 0x01:
		return "initial Teletext page"
	case 0x02:
		return "Teletext subtitle page"
	case 0x03:
		return "additional information page"
	case 0x04:
		return "programme schedule page"
	case 0x05:
		return "Teletext subtitle for hearing impaired"
	default:
		return "reserved"
	}
}

func subtitlingTypeName(t uint8) string {
	switch {
	case t == 0x10:
		return "DVB subtitles (normal) no aspect ratio"
	case t == 0x20:
		return "DVB subtitles (hard of hearing) no aspect ratio"
	case t >= 0x10 && t <= 0x13:
		return "DVB subtitles (normal)"
	case t >= 0x20 && t <= 0x23:
		return "DVB subtitles (hard of hearing)"
	default:
		return "reserved"
	}
}
