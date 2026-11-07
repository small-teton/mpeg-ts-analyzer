// Package tsparser parses MPEG-2 Transport Stream packets (PAT/PMT/PES,
// adaptation fields, and descriptors) and checks transport-layer timing:
// the PCR interval, the PCR-PTS/DTS gap, and PCR jitter.
//
// tsparser decodes, recovers and decides which bytes can be trusted. During its
// single pass it feeds payload-free observations to the internal analysis
// package, which aggregates them into check results (currently the continuity
// counter summary); tsparser renders those results in its public report types.
// analysis never imports tsparser.
package tsparser
