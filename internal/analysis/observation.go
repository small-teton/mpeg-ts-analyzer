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
