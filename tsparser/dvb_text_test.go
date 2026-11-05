package tsparser

import "testing"

func TestDecodeDVBText(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
		ok   bool
	}{
		{"empty", nil, "", true},
		{"ASCII", []byte("Service01"), "Service01", true},
		{"default table symbols", []byte{0xA4, 0xE0, 0xD5}, "€\u2126♪", true},
		{"diacritic composed", []byte{'R', 0xC2, 'e', 's', 'e', 'a', 'u'}, "Réseau", true},
		{"diacritic without letter", []byte{'a', 0xC2}, "", false},
		{"diacritic before a space", []byte{0xC2, ' '}, "", false},
		{"diacritic before a brace", []byte{0xC2, '{'}, "", false},
		{"emphasis dropped, line break kept", []byte{0x86, 'A', 0x87, 0x8A, 'B'}, "A\nB", true},
		{"unassigned in the default table", []byte{'a', 0xA6}, "", false},
		{"DEL in the default table", []byte{'a', 0x7F}, "", false},
		{"C0 control in the default table", []byte{'a', 0x0A}, "", false},
		{"ISO 8859-5", []byte{0x01, 0xB0, 0xD0}, "Аа", true},
		{"ISO 8859-15", []byte{0x0B, 0xA4}, "€", true},
		{"ISO 8859-11", []byte{0x07, 0xA1}, "ก", true},
		{"three-byte selector ISO 8859-2", []byte{0x10, 0x00, 0x02, 0xA1}, "Ą", true},
		{"single-byte control codes", []byte{0x01, 'a', 0x8A, 0x86, 'b'}, "a\nb", true},
		{"single-byte C0 control", []byte{0x01, 0x0A}, "", false},
		{"single-byte DEL", []byte{0x01, 0x7F}, "", false},
		{"single-byte unassigned", []byte{0x02, 0xA1}, "", false},
		{"selector 0x08 is reserved", []byte{0x08, 'a'}, "", false},
		{"selector 0x00", []byte{0x00, 'a'}, "", false},
		{"three-byte selector for 8859-12", []byte{0x10, 0x00, 0x0C, 'a'}, "", false},
		{"three-byte selector out of range", []byte{0x10, 0x00, 0x10, 'a'}, "", false},
		{"three-byte selector with a nonzero second byte", []byte{0x10, 0x01, 0x01, 'a'}, "", false},
		{"three-byte selector cut short", []byte{0x10, 0x00}, "", false},
		{"UCS-2 is not decoded", []byte{0x11, 0x00, 'a'}, "", false},
		{"encoding_type_id is not decoded", []byte{0x1F, 0x01, 'a'}, "", false},
		{"UTF-8", append([]byte{0x15}, "日本語"...), "日本語", true},
		{"UTF-8 control codes", append([]byte{0x15}, "abc"...), "ab\nc", true},
		{"UTF-8 invalid", []byte{0x15, 0xFF}, "", false},
		{"UTF-8 C0 control", []byte{0x15, 'a', 0x0A}, "", false},
	}
	for _, tt := range tests {
		got, ok := decodeDVBText(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%s: decodeDVBText(% x) = %q, %v; want %q, %v", tt.name, tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
