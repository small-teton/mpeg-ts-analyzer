package main

type MpegPacket interface {
	CyclicValue() uint8
	SetCyclicValue(cyclicValue uint8)
	Append(buf []byte)
	Parse() error
	Dump()
}