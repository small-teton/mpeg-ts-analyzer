package bitbuffer

import "testing"

func TestSet(t *testing.T) {
	data := []byte{0xc2, 0x93, 0x70, 0x16}

	bb := new(BitBuffer)
	bb.Set(data)

	if len(data) != len(bb.buf) {
		t.Errorf("different data length(actual: %d, But got %d)", len(data), len(bb.buf))
	}
	if bb.pos != 0 {
		t.Errorf("pos is invalid(actual: 0, But got %d)", bb.pos)
	}
	for i := 0; i < len(data); i++ {
		if data[i] != bb.buf[i] {
			t.Errorf("different byte(actual: 0x%02x, But got 0x%02x)", data[i], bb.buf[i])
		}
	}
}

func TestSkip(t *testing.T) {
	data := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}

	bb := new(BitBuffer)
	bb.Set(data)

	bb.Skip(16)
	if bb.pos != 16 {
		t.Errorf("pos is invalid(actual: 2, But got %d)", bb.pos)
	}
	getByte, err := bb.ReadUint8(8)
	if getByte != data[2] || err != nil {
		t.Errorf("data is invalid(actual: 0x%02x, But got 0x%02x)", data[2], getByte)
	}

	bb.Skip(3)
	if bb.pos != 27 {
		t.Errorf("pos is invalid(actual: 2, But got %d)", bb.pos)
	}
	getByte, err = bb.ReadUint8(8)
	if getByte != 0xB1 || err != nil {
		t.Errorf("data is invalid(actual: 0x%02x, But got 0x%02x)", 0xB1, getByte)
	}
	if err = bb.Skip(10000); err == nil {
		t.Errorf("Skip over buffer length. but err is nil")
	}
}

func TestReadUint8(t *testing.T) {
	data := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}

	bb := new(BitBuffer)
	bb.Set(data)

	getByte, err := bb.ReadUint8(8)
	if getByte != data[0] || err != nil {
		t.Errorf("data is invalid(actual: 0x%02x, But got 0x%02x)", data[0], getByte)
	}
	getByte, err = bb.ReadUint8(3)
	if getByte != 0x04 || err != nil {
		t.Errorf("data is invalid(actual: 0x%02x, But got 0x%02x)", 0x04, getByte)
	}
	if _, err = bb.ReadUint8(9); err == nil {
		t.Errorf("Specified over uint8 size. But err is nil")
	}
	if _, err = bb.ReadUint8(10000); err == nil {
		t.Errorf("Read over buffer length. But err is nil")
	}
}

func TestReadUint16(t *testing.T) {
	data := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}

	bb := new(BitBuffer)
	bb.Set(data)

	getByte, err := bb.ReadUint16(16)
	if getByte != 0xc293 || err != nil {
		t.Errorf("data is invalid(actual: 0x%04x, But got 0x%04x)", 0xc293, getByte)
	}
	getByte, err = bb.ReadUint16(3)
	if getByte != 0x03 || err != nil {
		t.Errorf("data is invalid(actual: 0x%04x, But got 0x%04x)", 0x03, getByte)
	}
	if _, err = bb.ReadUint16(17); err == nil {
		t.Errorf("Specified over uint16 size. But err is nil")
	}
	if _, err = bb.ReadUint8(10000); err == nil {
		t.Errorf("Read over buffer length. But err is nil")
	}
}

func TestReadUint32(t *testing.T) {
	data := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}

	bb := new(BitBuffer)
	bb.Set(data)

	getByte, err := bb.ReadUint32(32)
	if getByte != 0xc2937016 || err != nil {
		t.Errorf("data is invalid(actual: 0x%08x, But got 0x%08x)", 0xc2937016, getByte)
	}
	getByte, err = bb.ReadUint32(3)
	if getByte != 0x01 || err != nil {
		t.Errorf("data is invalid(actual: 0x%08x, But got 0x%08x)", 0x01, getByte)
	}
	if _, err = bb.ReadUint32(33); err == nil {
		t.Errorf("Specified over uint32 size. But err is nil")
	}
	if _, err = bb.ReadUint32(10000); err == nil {
		t.Errorf("Read over buffer length. But err is nil")
	}
}

func TestReadUint64(t *testing.T) {
	data := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}

	bb := new(BitBuffer)
	bb.Set(data)

	getByte, err := bb.ReadUint64(64)
	if getByte != 0xc29370162d08a2f1 || err != nil {
		t.Errorf("data is invalid(actual: 0xc29370162d08a2f1, But got 0x%x)", getByte)
	}
	getByte, err = bb.ReadUint64(3)
	if getByte != 0x01 || err != nil {
		t.Errorf("data is invalid(actual: 0x%02x, But got 0x%02x)", 0x01, getByte)
	}
	if _, err = bb.ReadUint64(65); err == nil {
		t.Errorf("Specified over uint64 size. But err is nil")
	}
	if _, err = bb.ReadUint64(10000); err == nil {
		t.Errorf("Read over buffer length. But err is nil")
	}
}

func TestReadBitsZero(t *testing.T) {
	bb := new(BitBuffer)
	bb.Set([]byte{0xFF})
	val, err := bb.ReadUint8(0)
	if err != nil {
		t.Errorf("unexpected error: %s", err)
	}
	if val != 0 {
		t.Errorf("expected 0 for 0-bit read, got %d", val)
	}
	// Position should not advance
	if bb.pos != 0 {
		t.Errorf("expected pos=0 after 0-bit read, got %d", bb.pos)
	}
}

func TestReadBitsPartialHead(t *testing.T) {
	// Test reading bits that start mid-byte and span into next byte
	bb := new(BitBuffer)
	bb.Set([]byte{0xAB, 0xCD}) // 10101011 11001101
	// Skip 4 bits, then read 8 bits across byte boundary
	bb.Skip(4)
	val, err := bb.ReadUint8(8)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// After skipping 4 bits: remaining of first byte = 1011, first 4 of second = 1100
	// 10111100 = 0xBC
	if val != 0xBC {
		t.Errorf("expected 0xBC, got 0x%02X", val)
	}
}

func TestReadBitsOverflow(t *testing.T) {
	// Test n > 64 in readBits (defensive check)
	bb := new(BitBuffer)
	bb.Set([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	_, err := bb.readBits(65)
	if err == nil {
		t.Errorf("expected error for n > 64, got nil")
	}
}

func TestReadBitsHeadOnlyPartial(t *testing.T) {
	// Read fewer bits than remaining in the first byte (bitsFromFirst > remaining)
	bb := new(BitBuffer)
	bb.Set([]byte{0xAB}) // 10101011
	bb.Skip(2)           // pos=2, bitOffset=2, 6 bits remaining in byte
	// Read only 3 bits (< 6 remaining) → triggers bitsFromFirst > remaining
	val, err := bb.ReadUint8(3)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	// bits at pos 2-4: 101 = 5
	if val != 5 {
		t.Errorf("expected 5, got %d", val)
	}
}

func TestReadBitsTailPartial(t *testing.T) {
	// Test reading bits that end mid-byte (tail partial)
	bb := new(BitBuffer)
	bb.Set([]byte{0xF0}) // 11110000
	val, err := bb.ReadUint8(4)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if val != 0x0F {
		t.Errorf("expected 0x0F, got 0x%02X", val)
	}
	val, err = bb.ReadUint8(4)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if val != 0x00 {
		t.Errorf("expected 0x00, got 0x%02X", val)
	}
}
