package tsparser

import "github.com/small-teton/mpeg-ts-analyzer/bitbuffer"

// bitReader abstracts bit-level reading for testability.
type bitReader interface {
	Set([]byte)
	Skip(uint32) error
	ReadUint8(uint32) (uint8, error)
	ReadUint16(uint32) (uint16, error)
	ReadUint32(uint32) (uint32, error)
	ReadUint64(uint32) (uint64, error)
}

func newDefaultBitReader() bitReader {
	return new(bitbuffer.BitBuffer)
}
