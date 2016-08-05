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
	getByte, err := bb.PeekUint8(8)
	if getByte != data[2] || err != nil {
		t.Errorf("data is invalid(actual: 0x%02x, But got 0x%02x)", data[2], getByte)
	}

	bb.Skip(3)
	if bb.pos != 27 {
		t.Errorf("pos is invalid(actual: 2, But got %d)", bb.pos)
	}
	getByte, err = bb.PeekUint8(8)
	if getByte != 0xB1 || err != nil {
		t.Errorf("data is invalid(actual: 0x%02x, But got 0x%02x)", 0xB1, getByte)
	}
	if err = bb.Skip(10000); err == nil {
		t.Errorf("Skip over buffer length. but err is nil")
	}
}

func TestPeekUint8(t *testing.T) {
	data := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}

	bb := new(BitBuffer)
	bb.Set(data)

	getByte, err := bb.PeekUint8(8)
	if getByte != data[0] || err != nil {
		t.Errorf("data is invalid(actual: 0x%02x, But got 0x%02x)", data[0], getByte)
	}
	getByte, err = bb.PeekUint8(3)
	if getByte != 0x04 || err != nil {
		t.Errorf("data is invalid(actual: 0x%02x, But got 0x%02x)", 0x04, getByte)
	}
	if _, err = bb.PeekUint8(9); err == nil {
		t.Errorf("Specified over uint8 size. But err is nil")
	}
	if _, err = bb.PeekUint8(10000); err == nil {
		t.Errorf("Peek over buffer length. But err is nil")
	}
}

func TestPeekUint16(t *testing.T) {
	data := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}

	bb := new(BitBuffer)
	bb.Set(data)

	getByte, err := bb.PeekUint16(16)
	if getByte != 0xc293 || err != nil {
		t.Errorf("data is invalid(actual: 0x%04x, But got 0x%04x)", 0xc293, getByte)
	}
	getByte, err = bb.PeekUint16(3)
	if getByte != 0x03 || err != nil {
		t.Errorf("data is invalid(actual: 0x%04x, But got 0x%04x)", 0x03, getByte)
	}
	if _, err = bb.PeekUint16(17); err == nil {
		t.Errorf("Specified over uint16 size. But err is nil")
	}
	if _, err = bb.PeekUint8(10000); err == nil {
		t.Errorf("Peek over buffer length. But err is nil")
	}
}

func TestPeekUint32(t *testing.T) {
	data := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}

	bb := new(BitBuffer)
	bb.Set(data)

	getByte, err := bb.PeekUint32(32)
	if getByte != 0xc2937016 || err != nil {
		t.Errorf("data is invalid(actual: 0x%08x, But got 0x%08x)", 0xc2937016, getByte)
	}
	getByte, err = bb.PeekUint32(3)
	if getByte != 0x01 || err != nil {
		t.Errorf("data is invalid(actual: 0x%08x, But got 0x%08x)", 0x01, getByte)
	}
	if _, err = bb.PeekUint32(33); err == nil {
		t.Errorf("Specified over uint32 size. But err is nil")
	}
	if _, err = bb.PeekUint32(10000); err == nil {
		t.Errorf("Peek over buffer length. But err is nil")
	}
}

func TestPeekUint64(t *testing.T) {
	data := []byte{0xc2, 0x93, 0x70, 0x16, 0x2d, 0x08, 0xa2, 0xf1, 0x3a, 0x5c, 0xf9, 0xde, 0xbc, 0xee, 0xfc, 0x90, 0x63}

	bb := new(BitBuffer)
	bb.Set(data)

	getByte, err := bb.PeekUint64(64)
	if getByte != 0xc29370162d08a2f1 || err != nil {
		t.Errorf("data is invalid(actual: 0xc29370162d08a2f1, But got 0x%x)", getByte)
	}
	getByte, err = bb.PeekUint64(3)
	if getByte != 0x01 || err != nil {
		t.Errorf("data is invalid(actual: 0x%02x, But got 0x%02x)", 0x01, getByte)
	}
	if _, err = bb.PeekUint64(65); err == nil {
		t.Errorf("Specified over uint64 size. But err is nil")
	}
	if _, err = bb.PeekUint64(10000); err == nil {
		t.Errorf("Peek over buffer length. But err is nil")
	}
}
