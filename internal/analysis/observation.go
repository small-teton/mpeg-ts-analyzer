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

// Packet is one packet the parser counts for PID presence and packet gaps: a
// packet of any PID without transport_error_indicator (the PID of a packet
// with a transport error may itself be corrupted, so such a packet is not
// observed; it still occupies its index). Index is the packet's position in
// the range, counted from the first packet of the range, so the difference of
// two indices minus one is the number of packets between them. Offset is the
// byte offset of the packet.
type Packet struct {
	PID    uint16
	Index  int64
	Offset int64
}

// TimedPCR is one PCR of the reference program that the parser uses for
// timing (see PCR), placed among the packets: Index is the index of the packet
// carrying it, as in Packet, and Offset its byte offset. Step is the PCRStep
// that the program's PCRCheck returned for it, so the time basis splits into
// segments exactly where every other PCR-based result does.
type TimedPCR struct {
	Index  int64
	Offset int64
	Step   PCRStep
}

// ReferenceLayout is the PSI layout that applies from the packet at Index
// (byte offset Offset) on: every program of the applicable PAT and, for the
// programs whose PMT the analysis follows, the PIDs that PMT references. The
// parser reports one when the scan starts and whenever the layout changes: a
// PAT or PMT change is applied, the analysis of a program starts (after its
// PMT was acquired) or stops (the program was removed from the PAT).
// Programs and its slices belong to the caller.
type ReferenceLayout struct {
	Index  int64
	Offset int64
	// NetworkPID is the network_PID of the PAT's program_number 0 entry, or 0
	// when the PAT has none.
	NetworkPID uint16
	Programs   []ReferencedProgram
}

// ReferencedProgram is one program of a ReferenceLayout. When Known is false
// the analysis does not follow its PMT (it was not selected, not found, or
// the program was added by a later PAT), so only its PMT PID is known.
type ReferencedProgram struct {
	Number uint16
	PMTPID uint16
	Known  bool
	// PCRPID is the PMT's PCR_PID; 0x1FFF means the program has no PCR.
	PCRPID  uint16
	Streams []ElementaryStream
	// CAPIDs are the CA_PIDs of the CA_descriptors (tag 0x09) in the PMT,
	// at program or elementary-stream level.
	CAPIDs []uint16
}
