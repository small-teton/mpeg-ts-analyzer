package tsparser

import (
	"fmt"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

// continuityEvent describes one continuity_counter violation. Expected is the
// value required by the previous packet on the PID; Actual is the value that
// was observed. The tracker resynchronizes to Actual after returning the event.
type continuityEvent struct {
	PID      uint16
	Expected uint8
	Actual   uint8
	Pos      int64
}

type continuityResult struct {
	Event     *continuityEvent
	Duplicate bool
}

type continuityState struct {
	cc     uint8
	packet [tsPayloadSize]byte
}

// continuityTracker validates continuity counters independently of PSI or PES
// assembly. It tracks every non-null PID passed to Check. State is bounded to
// one 188-byte packet per observed PID so exact payload duplicates can be
// distinguished from invalid same-counter packets.
type continuityTracker struct {
	states map[uint16]*continuityState
}

func newContinuityTracker() *continuityTracker {
	return &continuityTracker{states: make(map[uint16]*continuityState)}
}

func (t *continuityTracker) Check(packet *TsPacket) continuityResult {
	pid := packet.Pid()
	if pid == nullPidValue {
		return continuityResult{}
	}

	current := continuityState{
		cc: packet.ContinuityCounter(),
	}
	copy(current.packet[:], packet.buf)
	// Duplicate packets may refresh PCR, but every other header, adaptation
	// field and payload byte must match. Normalize only the six PCR bytes.
	if packet.HasAf() && packet.adaptationField.PcrFlag() {
		clear(current.packet[6:12])
	}
	previous, seen := t.states[pid]
	if !seen {
		previous = new(continuityState)
		*previous = current
		t.states[pid] = previous
		return continuityResult{}
	}
	if packet.adaptationField.DiscontinuityIndicator() {
		*previous = current
		return continuityResult{}
	}

	if packet.HasPayload() && current.cc == previous.cc && current.packet == previous.packet {
		return continuityResult{Duplicate: true}
	}

	expected := previous.cc
	if packet.HasPayload() {
		expected = (expected + 1) & 0x0F
	}
	*previous = current
	if current.cc == expected {
		return continuityResult{}
	}
	return continuityResult{Event: &continuityEvent{
		PID:      pid,
		Expected: expected,
		Actual:   current.cc,
		Pos:      packet.pos,
	}}
}

// continuityCounterSummary feeds the continuity errors found by
// continuityTracker and the applied program layouts to the continuity check of
// the analysis package, and renders its result as the end-of-run summary.
type continuityCounterSummary struct {
	check *analysis.ContinuityCheck
}

// newContinuityCounterSummary returns a summary that names the PIDs of one
// program.
func newContinuityCounterSummary(pmtPid, pcrPid uint16, programInfos []ProgramInfo) *continuityCounterSummary {
	s := newEmptyContinuitySummary()
	s.addProgramLabels(pmtPid, pcrPid, programInfos)
	return s
}

// newEmptyContinuitySummary returns a summary with no named PIDs; programs are
// added with addProgramLabels.
func newEmptyContinuitySummary() *continuityCounterSummary {
	return &continuityCounterSummary{check: analysis.NewContinuityCheck()}
}

// addProgramLabels names the PIDs of one program in the summary.
func (s *continuityCounterSummary) addProgramLabels(pmtPid, pcrPid uint16, programInfos []ProgramInfo) {
	s.check.ObserveLayout(programLayout(pmtPid, pcrPid, programInfos))
}

// programLayout is the observation of one program layout.
func programLayout(pmtPid, pcrPid uint16, programInfos []ProgramInfo) analysis.ProgramLayout {
	streams := make([]analysis.ElementaryStream, len(programInfos))
	for i, info := range programInfos {
		streams[i] = analysis.ElementaryStream{PID: info.elementaryPid, StreamType: info.streamType}
	}
	return analysis.ProgramLayout{PMTPID: pmtPid, PCRPID: pcrPid, Streams: streams}
}

// Add counts one continuity error.
func (s *continuityCounterSummary) Add(event continuityEvent) {
	s.check.ObserveError(analysis.ContinuityError{PID: event.PID, Offset: event.Pos})
}

// Dump prints a deterministic per-PID summary. A healthy stream still gets a
// one-line result because continuity integrity is an always-on health check.
func (s *continuityCounterSummary) Dump() {
	fmt.Println("-----------------------------")
	result := s.check.Result()
	if result.Status == analysis.StatusOK {
		fmt.Println("Continuity Counter: no errors detected")
		return
	}

	fmt.Println("Continuity Counter Error Summary:")
	for _, pid := range result.PIDs {
		fmt.Printf("  PID 0x%04x (%s) : %d %s\n", pid.PID, pid.Label, pid.Errors, errorWord(pid.Errors))
	}
	fmt.Printf("  Total              : %d %s in %d %s\n", result.TotalErrors, errorWord(result.TotalErrors), len(result.PIDs), pidWord(len(result.PIDs)))
}

func errorWord(count int) string {
	if count == 1 {
		return "error"
	}
	return "errors"
}

func pidWord(count int) string {
	if count == 1 {
		return "PID"
	}
	return "PIDs"
}
