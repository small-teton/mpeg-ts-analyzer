package tsparser

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/unicode/norm"
)

// DVB text strings (ETSI EN 300 468, Annex A) select their character table
// with the first byte:
//
//	0x20-0xFF      no selector: the default table, ISO/IEC 6937 (Figure A.1)
//	0x01-0x0B      ISO/IEC 8859-5 to 8859-15 (8859-12 does not exist)
//	0x10 0x00 0xNN ISO/IEC 8859-NN
//	0x15           UTF-8
//
// Other selectors (UCS-2, KS X 1001, GB2312, Big5, an encoding_type_id) are
// not decoded. 0x80-0x9F are control codes in the single-byte tables, and
// U+E080-U+E09F in UTF-8: 0x8A is a line break, the others (character
// emphasis on and off, reserved, user-defined) are dropped.

// iso8859Tables are the tables selected by 0x10 0x00 0xNN, indexed by NN.
// 8859-11 is decoded with Windows-874, which matches it outside 0x80-0x9F,
// where DVB has its control codes.
var iso8859Tables = [...]*charmap.Charmap{
	1: charmap.ISO8859_1, 2: charmap.ISO8859_2, 3: charmap.ISO8859_3, 4: charmap.ISO8859_4,
	5: charmap.ISO8859_5, 6: charmap.ISO8859_6, 7: charmap.ISO8859_7, 8: charmap.ISO8859_8,
	9: charmap.ISO8859_9, 10: charmap.ISO8859_10, 11: charmap.Windows874, 13: charmap.ISO8859_13,
	14: charmap.ISO8859_14, 15: charmap.ISO8859_15,
}

// iso6937 maps 0xA0-0xFF of the default table; 0 marks an unassigned code and
// the non-spacing diacritical marks 0xC1-0xCF are in iso6937Diacritics.
var iso6937 = [96]rune{
	0x00A0, 0x00A1, 0x00A2, 0x00A3, 0x20AC, 0x00A5, 0, 0x00A7, 0x00A4, 0x2018, 0x201C, 0x00AB, 0x2190, 0x2191, 0x2192, 0x2193,
	0x00B0, 0x00B1, 0x00B2, 0x00B3, 0x00D7, 0x00B5, 0x00B6, 0x00B7, 0x00F7, 0x2019, 0x201D, 0x00BB, 0x00BC, 0x00BD, 0x00BE, 0x00BF,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0x2015, 0x00B9, 0x00AE, 0x00A9, 0x2122, 0x266A, 0x00AC, 0x00A6, 0, 0, 0, 0, 0x215B, 0x215C, 0x215D, 0x215E,
	0x2126, 0x00C6, 0x0110, 0x00AA, 0x0126, 0, 0x0132, 0x013F, 0x0141, 0x00D8, 0x0152, 0x00BA, 0x00DE, 0x0166, 0x014A, 0x0149,
	0x0138, 0x00E6, 0x0111, 0x00F0, 0x0127, 0x0131, 0x0133, 0x0140, 0x0142, 0x00F8, 0x0153, 0x00DF, 0x00FE, 0x0167, 0x014B, 0x00AD,
}

// iso6937Diacritics are the combining characters of the marks 0xC1-0xCF,
// which precede the letter they modify.
var iso6937Diacritics = map[byte]rune{
	0xC1: 0x0300, 0xC2: 0x0301, 0xC3: 0x0302, 0xC4: 0x0303, 0xC5: 0x0304, 0xC6: 0x0306, 0xC7: 0x0307,
	0xC8: 0x0308, 0xCA: 0x030A, 0xCB: 0x0327, 0xCD: 0x030B, 0xCE: 0x0328, 0xCF: 0x030C,
}

// decodeDVBText decodes a DVB string. It returns false when the string uses a
// character table that is not decoded or contains a code the table does not
// assign; the caller then reports the raw bytes.
func decodeDVBText(b []byte) (string, bool) {
	switch {
	case len(b) == 0:
		return "", true
	case b[0] >= 0x20:
		return decodeISO6937(b)
	case b[0] >= 0x01 && b[0] <= 0x0B && b[0] != 0x08:
		// 0x01 selects 8859-5, and so on; 8859-12 was never published.
		return decodeSingleByte(b[1:], iso8859Tables[b[0]+4])
	case b[0] == 0x10 && len(b) >= 3 && b[1] == 0x00 && int(b[2]) < len(iso8859Tables) && iso8859Tables[b[2]] != nil:
		return decodeSingleByte(b[3:], iso8859Tables[b[2]])
	case b[0] == 0x15:
		return decodeUTF8(b[1:])
	}
	return "", false
}

func decodeISO6937(b []byte) (string, bool) {
	var s strings.Builder
	for i := 0; i < len(b); i++ {
		c := b[i]
		if mark, ok := iso6937Diacritics[c]; ok {
			if i+1 == len(b) || b[i+1] < 0x41 || b[i+1] > 0x7A {
				return "", false
			}
			// Only the letter and its mark are composed: normalizing the
			// whole string would also replace characters such as the ohm
			// sign (0xE0) with their canonical equivalents.
			i++
			s.WriteString(norm.NFC.String(string([]rune{rune(b[i]), mark})))
			continue
		}
		switch {
		case c >= 0x20 && c < 0x7F:
			s.WriteByte(c)
		case c >= 0x80 && c < 0xA0:
			writeControlCode(&s, rune(c))
		case c >= 0xA0 && iso6937[c-0xA0] != 0:
			s.WriteRune(iso6937[c-0xA0])
		default:
			return "", false
		}
	}
	return s.String(), true
}

func decodeSingleByte(b []byte, table *charmap.Charmap) (string, bool) {
	var s strings.Builder
	for _, c := range b {
		switch r := table.DecodeByte(c); {
		case c >= 0x80 && c < 0xA0:
			writeControlCode(&s, rune(c))
		case c < 0x20 || c == 0x7F || r == utf8.RuneError:
			return "", false
		default:
			s.WriteRune(r)
		}
	}
	return s.String(), true
}

func decodeUTF8(b []byte) (string, bool) {
	if !utf8.Valid(b) {
		return "", false
	}
	var s strings.Builder
	for _, r := range string(b) {
		switch {
		case r >= 0xE080 && r <= 0xE09F:
			writeControlCode(&s, r-0xE000)
		case r < 0x20 || r == 0x7F:
			return "", false
		default:
			s.WriteRune(r)
		}
	}
	return s.String(), true
}

// writeControlCode handles a control code 0x80-0x9F.
func writeControlCode(s *strings.Builder, code rune) {
	if code == 0x8A {
		s.WriteByte('\n')
	}
}
