package tsparser

import (
	"fmt"
	"sort"
)

// continuityCounterSummary collects the continuity_counter violations already
// detected while buffering PES packets. It does not change detection or
// recovery behavior; it only makes the inline warnings visible as an end-of-run
// summary.
type continuityCounterSummary struct {
	counts map[uint16]int
	labels map[uint16]string
}

func newContinuityCounterSummary(programInfos []ProgramInfo) *continuityCounterSummary {
	s := &continuityCounterSummary{
		counts: make(map[uint16]int),
		labels: make(map[uint16]string),
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
