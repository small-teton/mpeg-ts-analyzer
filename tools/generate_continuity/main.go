// Command generate_continuity writes a deterministic MPEG-TS fixture for
// cross-checking continuity-counter behavior with an independent analyzer.
package main

import (
	"flag"
	"fmt"
	"os"
)

const (
	packetSize = 188
	testPID    = 0x0100
	nullPID    = 0x1FFF
)

func payloadPacket(pid uint16, cc uint8, pusi bool, fill byte) []byte {
	p := make([]byte, packetSize)
	p[0] = 0x47
	p[1] = byte(pid >> 8)
	if pusi {
		p[1] |= 0x40
	}
	p[2] = byte(pid)
	p[3] = 0x10 | cc
	for i := 4; i < len(p); i++ {
		p[i] = fill
	}
	return p
}

func adaptationOnlyPacket(pid uint16, cc uint8, discontinuity bool) []byte {
	p := make([]byte, packetSize)
	p[0] = 0x47
	p[1] = byte(pid >> 8)
	p[2] = byte(pid)
	p[3] = 0x20 | cc
	p[4] = 183
	if discontinuity {
		p[5] = 0x80
	}
	for i := 6; i < len(p); i++ {
		p[i] = 0xFF
	}
	return p
}

func main() {
	output := flag.String("output", "continuity.ts", "output transport stream")
	flag.Parse()

	wrapped := payloadPacket(testPID, 0, false, 0x20)
	packets := [][]byte{
		payloadPacket(testPID, 15, false, 0x10),
		wrapped,
		wrapped,                                // legal exact duplicate
		payloadPacket(testPID, 0, false, 0x30), // invalid same counter
		payloadPacket(testPID, 1, false, 0x40), // resynchronized
		adaptationOnlyPacket(testPID, 1, false),
		payloadPacket(testPID, 2, false, 0x50),
		adaptationOnlyPacket(testPID, 9, true), // declared discontinuity
		payloadPacket(testPID, 10, false, 0x60),
		payloadPacket(testPID, 11, true, 0x70),  // PUSI follows normal rules
		payloadPacket(testPID, 14, false, 0x80), // gap: expected 12
		payloadPacket(testPID, 15, false, 0x90), // resynchronized
		payloadPacket(nullPID, 0, false, 0xFF),
		payloadPacket(nullPID, 9, false, 0xFF), // null PID is excluded
	}

	f, err := os.Create(*output)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }()
	for _, packet := range packets {
		if _, err := f.Write(packet); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
