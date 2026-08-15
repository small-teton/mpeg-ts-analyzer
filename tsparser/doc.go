// Package tsparser parses MPEG-2 Transport Stream packets (PAT/PMT/PES,
// adaptation fields, and descriptors) and checks transport-layer timing:
// the PCR interval, the PCR-PTS gap, and PCR jitter.
package tsparser
