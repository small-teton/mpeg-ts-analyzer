package tsparser

import (
	"bytes"
	"fmt"
	"sort"
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
	packet []byte
}

// continuityTracker validates continuity counters independently of PSI or PES
// assembly. It tracks every non-null PID passed to Check. State is bounded to
// one 188-byte packet per observed PID so exact payload duplicates can be
// distinguished from invalid same-counter packets.
type continuityTracker struct {
	states map[uint16]continuityState
}

func newContinuityTracker() *continuityTracker {
	return &continuityTracker{states: make(map[uint16]continuityState)}
}

func (t *continuityTracker) Check(packet *TsPacket) continuityResult {
	pid := packet.Pid()
	if pid == nullPidValue {
		return continuityResult{}
	}

	current := continuityState{
		cc:     packet.ContinuityCounter(),
		packet: append([]byte(nil), packet.buf...),
	}
	previous, seen := t.states[pid]
	if !seen || packet.adaptationField.DiscontinuityIndicator() {
		t.states[pid] = current
		return continuityResult{}
	}

	if packet.HasPayload() && current.cc == previous.cc && bytes.Equal(current.packet, previous.packet) {
		return continuityResult{Duplicate: true}
	}

	expected := previous.cc
	if packet.HasPayload() {
		expected = (expected + 1) & 0x0F
	}
	t.states[pid] = current
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

// continuityCounterSummary collects TS-layer continuity events and makes the
// inline warnings visible as a deterministic end-of-run summary.
type continuityCounterSummary struct {
	counts map[uint16]int
	labels map[uint16]string
}

func newContinuityCounterSummary(pmtPid, pcrPid uint16, programInfos []ProgramInfo) *continuityCounterSummary {
	s := &continuityCounterSummary{
		counts: make(map[uint16]int),
		labels: make(map[uint16]string),
	}
	s.labels[0] = "PAT"
	s.labels[pmtPid] = "PMT"
	if pcrPid != 0 && pcrPid != nullPidValue {
		s.labels[pcrPid] = "PCR"
	}
	for _, info := range programInfos {
		s.labels[info.elementaryPid] = continuityCounterLabel(info.streamType)
	}
	return s
}

func (s *continuityCounterSummary) Add(pid uint16) { s.counts[pid]++ }

// Dump prints a deterministic per-PID summary. A healthy stream still gets a
// one-line result because continuity integrity is an always-on health check.
func (s *continuityCounterSummary) Dump() {
	fmt.Println("-----------------------------")
	if len(s.counts) == 0 {
		fmt.Println("Continuity Counter: no errors detected")
		return
	}

	fmt.Println("Continuity Counter Error Summary:")
	pids := make([]uint16, 0, len(s.counts))
	for pid := range s.counts {
		pids = append(pids, pid)
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })

	var total int
	for _, pid := range pids {
		count := s.counts[pid]
		total += count
		label := s.labels[pid]
		if label == "" {
			label = "unknown"
		}
		fmt.Printf("  PID 0x%04x (%s) : %d %s\n", pid, label, count, errorWord(count))
	}
	fmt.Printf("  Total              : %d %s in %d %s\n", total, errorWord(total), len(pids), pidWord(len(pids)))
}

func continuityCounterLabel(streamType uint8) string {
	switch streamType {
	case 0x01, 0x02, 0x10, 0x1B, 0x24:
		return "video"
	case 0x03, 0x04, 0x0F, 0x11:
		return "audio"
	default:
		return fmt.Sprintf("stream_type 0x%02x", streamType)
	}
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
