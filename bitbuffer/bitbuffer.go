package bitbuffer

import "fmt"

// BitBuffer Peek buffer by the bit
type BitBuffer struct {
	buf []byte
	pos uint16
}

// Set set the data in the buffer
func (b *BitBuffer) Set(src []byte) {
	b.buf = make([]byte, len(src))
	copy(b.buf, src)
}

// Skip Only increase position. Not Peek data.
func (b *BitBuffer) Skip(length uint16) error {
	if (b.pos + length) > uint16(len(b.buf)*8) {
		return fmt.Errorf("Length(%d) is out of range(%d)", length, len(b.buf))
	}
	b.pos += length
	return nil
}

// PeekUint8 return type uint8
func (b *BitBuffer) PeekUint8(length uint16) (uint8, error) {
	if length > 8 || (b.pos+length) > uint16(len(b.buf)*8) {
		return 0, fmt.Errorf("Length(%d) is out of range(0-8)", length)
	}

	index := b.pos / 8
	offset := b.pos % 8

	var firstByte, secondByte, buf uint16
	if uint16(len(b.buf)*8)-b.pos <= 8 {
		firstByte = 0x0
		secondByte = uint16(b.buf[index])
		buf = firstByte | secondByte
		buf >>= (8 - offset - length)
	} else {
		firstByte = uint16(b.buf[index])
		firstByte <<= 8
		secondByte = uint16(b.buf[index+1])
		buf = firstByte | secondByte
		buf >>= (16 - offset - length)
	}
	var digit uint16 = 1
	var mask uint16
	for i := uint16(0); i < length; i++ {
		mask += digit
		digit *= 2
	}
	b.pos += length
	return uint8(buf & mask), nil
}

// PeekUint16 return type uint16
func (b *BitBuffer) PeekUint16(length uint16) (uint16, error) {
	if length > 16 || (b.pos+length) > uint16(len(b.buf)*8) {
		return 0, fmt.Errorf("Length(%d) is out of range(0-16)%d %d", length, b.pos, len(b.buf)*8)
	}

	if length <= 8 {
		val, err := b.PeekUint8(length)
		return uint16(val), err
	}
	first8, err := b.PeekUint8(8)
	second8, err := b.PeekUint8(length - 8)
	return uint16(first8)<<(length-8) | uint16(second8), err
}

// PeekUint32 return type uint32
func (b *BitBuffer) PeekUint32(length uint16) (uint32, error) {
	if length > 32 || (b.pos+length) > uint16(len(b.buf)*8) {
		return 0, fmt.Errorf("Length(%d) is out of range(0-32)", length)
	}

	if length <= 16 {
		val, err := b.PeekUint16(length)
		return uint32(val), err
	} else if length-16 <= 8 {
		first16, err := b.PeekUint16(16)
		second8, err := b.PeekUint8(length - 16)
		return uint32(first16)<<(length-16) | uint32(second8), err
	}
	first16, err := b.PeekUint16(16)
	second16, err := b.PeekUint16(length - 16)
	return uint32(first16)<<(length-16) | uint32(second16), err
}

// PeekUint64 return type uint64
func (b *BitBuffer) PeekUint64(length uint16) (uint64, error) {
	if length > 64 || (b.pos+length) > uint16(len(b.buf)*8) {
		return 0, fmt.Errorf("Length(%d) is out of range(0-64)", length)
	}

	if length <= 32 {
		val, err := b.PeekUint32(length)
		return uint64(val), err
	} else if length-32 <= 8 {
		first32, err := b.PeekUint32(32)
		second8, err := b.PeekUint8(length - 32)
		return uint64(first32)<<(length-32) | uint64(second8), err
	} else if length-32 <= 16 {
		first32, err := b.PeekUint32(32)
		second16, err := b.PeekUint16(length - 16)
		return uint64(first32)<<(length-32) | uint64(second16), err
	}
	first32, err := b.PeekUint32(32)
	second32, err := b.PeekUint32(length - 32)
	return uint64(first32)<<(length-32) | uint64(second32), err
}
