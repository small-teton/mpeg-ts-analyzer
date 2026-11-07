package analysis

// nullPID is the PID of null packets; as a PCR_PID it means "no PCR".
const nullPID = 0x1FFF

// ElementaryStream is one elementary stream announced by a PMT.
type ElementaryStream struct {
	PID        uint16 // elementary_PID
	StreamType uint8  // stream_type
}

// ProgramLayout is the PID layout of one program that the parser applied: the
// PID carrying the program's PMT and the PCR_PID and elementary streams of the
// PMT version in force from that point on. The parser reports a layout when it
// starts analyzing a program and again whenever a PAT or PMT change is
// applied. Streams belongs to the caller and is not retained.
type ProgramLayout struct {
	PMTPID uint16
	// PCRPID is the PMT's PCR_PID; 0x1FFF means the program has no PCR.
	PCRPID  uint16
	Streams []ElementaryStream
}

// ContinuityError is one continuity_counter discontinuity that the parser
// detected on a non-null PID: a counter that did not follow the previous packet
// of the PID, other than an exact duplicate or across a declared
// discontinuity_indicator. Offset is the byte offset of the packet. The parser
// resynchronizes to the received counter, so one fault yields one observation.
type ContinuityError struct {
	PID    uint16
	Offset int64
}

// TransportError is one packet with transport_error_indicator set, on any PID
// including the null PID. The PID is the one decoded from the packet's own
// header, which the error may have corrupted. Offset is the byte offset of the
// packet. Excluded reports that the parser left data of the packet out of the
// analysis: the packet is on a PID whose payload or PCR the analysis reads at
// that point (the PAT, the PMT or PCR_PID of an analyzed program, or one of its
// elementary streams).
type TransportError struct {
	PID      uint16
	Offset   int64
	Excluded bool
}

// Scrambled is one packet whose transport_scrambling_control is not zero, on
// any PID including the null PID. Offset is the byte offset of the packet.
// Excluded reports that the parser left the packet's payload out of the
// analysis: the packet carries a payload on a PID whose payload the analysis
// reads at that point (the PAT, the PMT of an analyzed program or one of its
// elementary streams). The header and adaptation field of a scrambled packet
// are not scrambled, so its PCR is still used and is not reported as excluded.
type Scrambled struct {
	PID      uint16
	Offset   int64
	Excluded bool
}
