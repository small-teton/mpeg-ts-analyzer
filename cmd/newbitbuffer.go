package cmd

import (
	"fmt"

	"github.com/small-teton/mpeg-ts-analyzer/bitbuffer"
)

func newBitBufferTest() {
	buf := []byte{0b10101010, 0b10101010, 0b10101010, 0b10101011, 0b10101010, 0b10101010, 0b10101010, 0b10101010}
	bb := new(bitbuffer.BitBuffer)
	bb.Set(buf)

	f, err := bitbuffer.Peek[uint16](bb, 14)
	if err != nil {
		fmt.Printf("err1: %v\n", err)
	}
	fmt.Printf("======first peek 14byte: %d, %#16b\n", f, f)
	s, err := bitbuffer.Peek[uint32](bb, 21)
	if err != nil {
		fmt.Printf("err2: %v\n", err)
	}
	fmt.Printf("======second peek 21byte: %d, %#32b\n", s, s)
}
