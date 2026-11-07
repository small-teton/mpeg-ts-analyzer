// Package analysis evaluates what the transport-stream parser observed: it
// keeps bounded state across observations, computes aggregate metrics, and
// assigns the status of each transport health check.
//
// # Responsibility
//
// The layers of the analyzer are:
//
//   - tsparser detects the packet size, synchronizes, decodes TS packets,
//     adaptation fields, PSI and PES, records byte offsets, and decides
//     whether bytes can be parsed and trusted (for example, whether a packet is
//     a duplicate that must not be appended to PES data twice).
//   - analysis (this package) aggregates the resulting observations, decides
//     whether a check applies, applies thresholds, and produces a status per
//     check.
//   - The report code in tsparser and the cmd package turn results into the
//     stable text and JSON contract and the exit status.
//
// # Dependency direction
//
// tsparser imports analysis; analysis imports only the standard library. It
// must never import tsparser, cmd or GUI code: the scanner feeding analysis
// would otherwise form an import cycle, and check policy would depend on
// parser implementation types. TestImportsOnlyStandardLibrary enforces this.
// The package lives under internal/ so that its types are not a public Go API
// while the result model evolves; the exported tsparser report types remain
// the public contract.
//
// # Observations
//
// The parser reports what it found as small typed observations, passed by
// value during its single pass over the input:
//
//   - Observations carry no payload: no TS payload, PSI section or PES body,
//     and no pointer or slice into parser-owned buffers. A slice field (such as
//     ProgramLayout.Streams) belongs to the caller; a check copies what it keeps
//     before returning, so the caller may reuse the slice afterwards.
//   - Offsets are byte offsets in the input file of the first byte of the
//     packet concerned. For 192-byte M2TS input this is the first byte of the
//     4-byte TP_extra_header, as in the rest of the analyzer's output; for
//     204-byte input it is the sync byte, before the 16-byte trailer.
//   - A PID is the 13-bit packet identifier; stream types are the PMT
//     stream_type values. Counts are numbers of events, not of bytes.
//
// PCR is the exception that feeds results outside this package: PCRCheck
// classifies each PCR against the previous one (PCRClock) and returns the
// PCRStep, so the PCR jitter estimate, the bitrate time base and the time
// series in tsparser split into the same timing segments as the PCR interval
// and discontinuity checks.
//
// A check keeps only aggregate state, bounded by the number of PIDs (at most
// 8192) or by a documented fixed limit; it never retains one object per
// packet. Results are plain values computed on demand and owned by the caller.
//
// # Adding a check
//
// A new check gets a typed observation for the fact the parser can establish,
// a check type that aggregates it, and a result type, for example:
//
//	// Scrambled is a packet whose transport_scrambling_control is not zero.
//	type Scrambled struct {
//		PID    uint16
//		Offset int64
//	}
//
//	type ScramblingCheck struct{ packets map[uint16]int }
//
//	func (c *ScramblingCheck) ObserveScrambled(o Scrambled) { c.packets[o.PID]++ }
//
// tsparser calls ObserveScrambled from its scan where it already decided that
// the packet is scrambled (and keeps its payload out of PES assembly); the
// report code converts the check's result into the JSON and text output.
// Whether a payload can be decoded at all stays in tsparser; how many affected
// packets make the check fail, and with which reason, belongs here.
package analysis
