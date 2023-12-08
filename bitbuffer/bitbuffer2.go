package bitbuffer

import (
	"fmt"
	"math"
	"unsafe"

	"golang.org/x/exp/constraints"
)

////////////////////////////////////////////////
// すげぇ単純にGeneric化してみる
//
// しかし、これに果たして意味があるのか？

// func NewPeek[T constraints.Unsigned](b *BitBuffer, length uint16) T {
// 	var t interface{} = *new(T)
// 	switch t.(type) {
// 	case uint8:
// 		return T(b.PeekUint8(length))
// 	case uint16:
// 		return T(b.PeekUint8(length))
// 	case uint32:
// 		return T(b.PeekUint8(length))
// 	case uint64:
// 		return T(b.PeekUint8(length))
// 	}
// }

const BYTE_SIZE = 8

func Peek[T constraints.Unsigned](b *BitBuffer, length uint16) (T, error) {
	retVal, err := b.NewPeek(length)
	if err != nil {
		return 0, err
	}

	var t interface{} = *new(T)
	s := uint16(unsafe.Sizeof(t) * BYTE_SIZE)
	if length > s {
		return 0, fmt.Errorf("length(%d) is out of range(0-%d)", length, s)
	}

	return T(retVal), nil
}

func (b *BitBuffer) NewPeek(length uint16) (uint64, error) {
	if length > 64 {
		return 0, fmt.Errorf("length(%d) is out of range(0-64)", length)
	}
	bufSize := uint16(len(b.buf) * BYTE_SIZE)
	if (b.pos + length) > bufSize {
		return 0, fmt.Errorf("length(%d) is out of buf size(%d)", length, bufSize)
	}
	firstByteIndex := b.pos / BYTE_SIZE
	firstBytePos := b.pos % BYTE_SIZE
	lastByteIndex := (b.pos + length) / BYTE_SIZE
	// fmt.Printf("(b.pos + length): %d\n", (b.pos + length))
	lastBytePos := (b.pos + length) % BYTE_SIZE

	var retVal uint64
	var pos uint16 = 0
	var digit uint16 = length
	// byte単位のループ
	for i := firstByteIndex; i <= lastByteIndex; i++ {
		// fmt.Printf("index roop: b.pos=%d, index=%d, firstByteIndex=%d, firstBytePos=%d, lastByteIndex=%d, lastBytePos=%d\n",
		// 	b.pos, i, firstByteIndex, firstBytePos, lastByteIndex, lastBytePos)
		pos = 0
		if i == firstByteIndex {
			pos = firstBytePos
		}

		// byteの中のbit単位のループ
		for j := pos; j < BYTE_SIZE; j++ {
			// fmt.Printf("isBitSet: %v, i=%d, j=%d, pos=%d, buf=%#08b\n", isBitSet(b.buf[i], BYTE_SIZE-1-j), i, j, pos, b.buf[i])
			if isBitSet(b.buf[i], BYTE_SIZE-1-j) {
				// fmt.Printf("retVal add: %d\n", uint64(math.Pow(2, float64(digit-1))))
				retVal += uint64(math.Pow(2, float64(digit-1)))
			}
			digit--
			pos++
			if i == lastByteIndex && pos > lastBytePos {
				break
			}
		}
	}
	b.pos += length

	return retVal, nil
}

// 任意の桁のbitが立っているかを返す
func isBitSet(b byte, p uint16) bool {
	return ((b >> p) & 1) == 1
}
