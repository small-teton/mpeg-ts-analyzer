package tsparser

// Physical packet framing.
//
// Every supported input carries one 188-byte MPEG-2 TS packet (ISO/IEC
// 13818-1) per physical packet, and only those 188 bytes are parsed:
//
//   - 188 bytes: the TS packet alone.
//   - 192 bytes (M2TS): a 4-byte TP_extra_header (copy_permission_indicator
//     and arrival_time_stamp) followed by the TS packet.
//   - 204 bytes: the TS packet followed by a 16-byte trailer, commonly
//     Reed-Solomon RS(204,188) parity in broadcast systems. The trailer is
//     never parsed as TS data, validated or corrected; packet detail exposes
//     it as raw bytes only.
//
// Offsets are always physical file offsets of the first byte of a physical
// packet, so consecutive packets are packetSize bytes apart.

const (
	m2tsPacketSize = tsPayloadSize + tpExtraHeaderSize
	// trailerSize is the length of the trailer of a 204-byte packet.
	trailerSize = 16
	// trailerPacketSize is the size of a TS packet followed by a 16-byte
	// trailer.
	trailerPacketSize = tsPayloadSize + trailerSize
	// strictSyncBytes is the number of consecutive sync bytes, one packet
	// apart, that detection requires for 204-byte packets: the same count
	// as a resynchronization (resyncSyncBytes).
	strictSyncBytes = resyncSyncBytes
)

// supportedPacketSizes lists the physical packet sizes, in the order
// detectPacketSize prefers them when the alignments are otherwise equal.
var supportedPacketSizes = []int{tsPayloadSize, m2tsPacketSize, trailerPacketSize}

// tsOffset is the offset of the TS packet's sync byte within a physical packet
// of packetSize bytes.
func tsOffset(packetSize int) int {
	if packetSize == m2tsPacketSize {
		return tpExtraHeaderSize
	}
	return 0
}

// tsBytes returns the 188 TS packet bytes of a physical packet of packetSize
// bytes, leaving out an M2TS header or a 204-byte packet's trailer.
func tsBytes(packet []byte, packetSize int) []byte {
	offset := tsOffset(packetSize)
	return packet[offset : offset+tsPayloadSize]
}

// isPacketSize reports whether size is a supported physical packet size.
func isPacketSize(size int) bool {
	for _, s := range supportedPacketSizes {
		if size == s {
			return true
		}
	}
	return false
}

// trailerAligned reports whether data has, from i on, strictSyncBytes sync
// bytes 204 bytes apart, and whether i does not also start an equally strong
// 188-byte or 192-byte alignment (strictSyncBytes sync bytes 188 or 192 bytes
// apart): there the 204-byte stride would only be a coincidence of payload
// bytes in a stream of another packet size. A weaker competing match is not
// enough to reject 204 bytes, since the trailer and payload bytes of a
// 204-byte stream may be 0x47. The caller guarantees that data holds
// i+(strictSyncBytes-1)*204, which also covers the shorter strides.
func trailerAligned(data []byte, i int) bool {
	if !syncsFrom(data, i, trailerPacketSize) {
		return false
	}
	return !syncsFrom(data, i, tsPayloadSize) && !syncsFrom(data, i, m2tsPacketSize)
}

// syncsFrom reports whether data has strictSyncBytes sync bytes stride bytes
// apart from i on.
func syncsFrom(data []byte, i, stride int) bool {
	for k := range strictSyncBytes {
		if data[i+k*stride] != syncByteValue {
			return false
		}
	}
	return true
}

// hasTrailerAlignment reports whether data holds a 204-byte alignment that
// trailerAligned accepts.
func hasTrailerAlignment(data []byte) bool {
	for i := 0; i+(strictSyncBytes-1)*trailerPacketSize < len(data); i++ {
		if trailerAligned(data, i) {
			return true
		}
	}
	return false
}
