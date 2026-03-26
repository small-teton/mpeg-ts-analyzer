package bitbuffer

import (
	"github.com/cockroachdb/errors"
)

// BitBuffer reads buffer by the bit.
type BitBuffer struct {
	buf []byte
	pos uint32
}

// Set set the data in the buffer.
func (b *BitBuffer) Set(src []byte) {
	b.buf = src
}

// Skip advances the bit position without reading.
func (b *BitBuffer) Skip(length uint32) error {
	if (b.pos + length) > uint32(len(b.buf)*8) {
		return errors.Newf("Length(%d) is out of range(%d)", length, len(b.buf))
	}
	b.pos += length
	return nil
}

// readBits reads up to 64 bits and returns them as uint64.
func (b *BitBuffer) readBits(n uint32) (uint64, error) {
	if n == 0 {
		return 0, nil
	}
	if n > 64 || (b.pos+n) > uint32(len(b.buf)*8) {
		return 0, errors.Newf("Length(%d) is out of range", n)
	}

	var result uint64
	remaining := n
	byteIndex := b.pos / 8
	bitOffset := b.pos % 8

	// 1. Head: partial bits from the first byte
	if bitOffset > 0 {
		bitsFromFirst := 8 - bitOffset
		if bitsFromFirst > remaining {
			bitsFromFirst = remaining
		}
		result = uint64(b.buf[byteIndex]>>(8-bitOffset-bitsFromFirst)) & ((1 << bitsFromFirst) - 1)
		remaining -= bitsFromFirst
		byteIndex++
	}

	// 2. Middle: full bytes
	for remaining >= 8 {
		result = (result << 8) | uint64(b.buf[byteIndex])
		remaining -= 8
		byteIndex++
	}

	// 3. Tail: partial bits from the last byte
	if remaining > 0 {
		result = (result << remaining) | uint64(b.buf[byteIndex]>>(8-remaining))
	}

	b.pos += n
	return result, nil
}

// ReadUint8 reads up to 8 bits and returns them as uint8.
func (b *BitBuffer) ReadUint8(length uint32) (uint8, error) {
	if length > 8 {
		return 0, errors.Newf("Length(%d) is out of range(0-8)", length)
	}
	v, err := b.readBits(length)
	return uint8(v), err
}

// ReadUint16 reads up to 16 bits and returns them as uint16.
func (b *BitBuffer) ReadUint16(length uint32) (uint16, error) {
	if length > 16 {
		return 0, errors.Newf("Length(%d) is out of range(0-16)", length)
	}
	v, err := b.readBits(length)
	return uint16(v), err
}

// ReadUint32 reads up to 32 bits and returns them as uint32.
func (b *BitBuffer) ReadUint32(length uint32) (uint32, error) {
	if length > 32 {
		return 0, errors.Newf("Length(%d) is out of range(0-32)", length)
	}
	v, err := b.readBits(length)
	return uint32(v), err
}

// ReadUint64 reads up to 64 bits and returns them as uint64.
func (b *BitBuffer) ReadUint64(length uint32) (uint64, error) {
	if length > 64 {
		return 0, errors.Newf("Length(%d) is out of range(0-64)", length)
	}
	return b.readBits(length)
}
