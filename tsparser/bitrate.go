package tsparser

import (
	"fmt"
	"sort"
)

// Per-PID bitrate statistics for the selected program.
//
// Bitrate needs a time axis. A file has no arrival timestamps, so — like the PCR
// jitter analysis (see pcr_jitter.go) — PCR (the 27 MHz program clock, ISO/IEC
// 13818-1 §2.4.2.2) is used as the clock: the elapsed stream time between two
// PCRs is their value difference. Bytes are attributed to the PCR interval they
// fall in, and the intervals are bucketed into fixed 1-second windows.
//
//   - Average bitrate per PID = (bytes counted within valid PCR segments) * 8 /
//     (summed duration of those segments).
//   - Peak bitrate per PID = the largest byte total in any full 1-second window,
//     * 8. A single PCR interval (tens of ms) is too short and irregular to be a
//     meaningful peak, so a 1-second tumbling window is used instead.
//
// The average uses every byte within valid PCR segments over the full measured
// duration (numerator and denominator stay in step, so the trailing partial
// second is fine). The peak, in contrast, only considers *full* 1-second
// windows — a partial trailing window covers less than a second and would
// understate as a per-second rate, so it is dropped from the peak.
//
// Bytes seen before the first PCR have no time reference and are dropped
// entirely. A PCR discontinuity / reset / wrap (see isSegmentBoundary) breaks
// the clock: the bytes waiting across such a gap are dropped (their real
// duration is unknown) and the elapsed clock does not advance over the gap, so a
// broken stream does not inflate or deflate the numbers.
//
// Only PIDs of the selected program are counted (the tool analyses one program
// at a time; a multi-program capture interleaves other programs' PIDs in the
// same byte stream). NULL (0x1FFF) stuffing is mux-wide, not program-specific,
// so it is reported separately and excluded from the program total.

const (
	patPidValue  = 0x0000
	nullPidValue = 0x1fff
	// bitrateTicksPerSec is one second of the 27 MHz PCR clock.
	bitrateTicksPerSec = pcrClockHz
)

// pidStat holds the byte accounting for a single PID.
type pidStat struct {
	totalBytes int64           // bytes within valid PCR segments (for average)
	buckets    map[int64]int64 // 1-second window index -> bytes (for peak)
}

func newPidStat() *pidStat { return &pidStat{buckets: make(map[int64]int64)} }

// BitrateStats accumulates per-PID byte counts on a PCR time axis.
type BitrateStats struct {
	stats  map[uint16]*pidStat // counted PIDs (program PIDs + NULL)
	labels map[uint16]string   // PID -> human-readable label
	// programPids is the set of PIDs that belong to the selected program
	// (PAT, PMT, PCR and the elementary streams); NULL is excluded.
	programPids map[uint16]bool
	program     int // program_number for the header (0 = sole/default program)

	// PCR clock state
	started      bool
	prev         pcrSample
	elapsedTicks uint64           // cumulative valid stream time up to prev's PCR
	pending      map[uint16]int64 // bytes since prev, awaiting interval assignment
	segments     int              // number of valid timing segments (>=1 once started)
}

// NewBitrateStats builds a collector for the selected program. programInfos are
// the PMT's elementary streams; pmtPid and pcrPid identify the program's PSI and
// clock PIDs. NULL is tracked separately for reference.
func NewBitrateStats(program int, pmtPid, pcrPid uint16, programInfos []ProgramInfo) *BitrateStats {
	b := &BitrateStats{
		stats:       make(map[uint16]*pidStat),
		labels:      make(map[uint16]string),
		programPids: make(map[uint16]bool),
		program:     program,
		pending:     make(map[uint16]int64),
	}

	track := func(pid uint16, label string, isProgram bool) {
		if _, ok := b.stats[pid]; !ok {
			b.stats[pid] = newPidStat()
			b.labels[pid] = label
		}
		if isProgram {
			b.programPids[pid] = true
		}
	}

	// Elementary streams first so their stream_type label wins even when a PID
	// doubles as the PCR carrier (PCR often rides on the video PID).
	for _, info := range programInfos {
		track(info.elementaryPid, StreamTypeString(info.streamType), true)
	}
	track(patPidValue, "PAT", true)
	track(pmtPid, "PMT", true)
	// PCR_PID 0 (unset) and 0x1FFF ("no PCR") are not real carrier PIDs; treating
	// the latter as a program PID would fold NULL stuffing into the program total.
	if pcrPid != 0 && pcrPid != nullPidValue {
		track(pcrPid, "PCR", true)
	}
	track(nullPidValue, "NULL (mux-wide)", false)
	return b
}

// CountPacket records one TS packet of pid. Each transport packet contributes a
// fixed 188 bytes: any 192-byte timestamped-packet (M2TS) header is container
// overhead, not part of the PID's transport packet. PIDs outside the selected
// program are ignored.
func (b *BitrateStats) CountPacket(pid uint16) {
	if _, ok := b.stats[pid]; !ok {
		return
	}
	b.pending[pid] += tsPayloadSize
}

// MarkPcr records a PCR observation (the pcrPid packet's value and its
// adaptation_field discontinuity_indicator) and closes the interval that ends at
// it, attributing the pending bytes to the current 1-second window.
func (b *BitrateStats) MarkPcr(pcr uint64, discontinuity bool) {
	cur := pcrSample{pcr: pcr, discontinuity: discontinuity}
	if !b.started {
		// Drop bytes seen before the first PCR: they have no time reference.
		b.started = true
		b.segments = 1
		b.prev = cur
		b.clearPending()
		return
	}
	if isSegmentBoundary(b.prev, cur) {
		// The gap's real duration is unknown: drop its bytes and freeze the
		// clock across it. The next interval resumes at the same elapsed time.
		b.segments++
		b.prev = cur
		b.clearPending()
		return
	}
	bucket := int64(b.elapsedTicks / bitrateTicksPerSec)
	for pid, bytes := range b.pending {
		if bytes == 0 {
			continue
		}
		s := b.stats[pid]
		s.totalBytes += bytes
		s.buckets[bucket] += bytes
	}
	b.elapsedTicks += pcr - b.prev.pcr
	b.prev = cur
	b.clearPending()
}

func (b *BitrateStats) clearPending() {
	for pid := range b.pending {
		b.pending[pid] = 0
	}
}

// fullBuckets is the number of complete 1-second windows measured. The trailing
// partial second (if any) is excluded from the peak.
func (b *BitrateStats) fullBuckets() int64 { return int64(b.elapsedTicks / bitrateTicksPerSec) }

// avgBps returns the average bitrate of a PID over the whole measured duration.
func (b *BitrateStats) avgBps(pid uint16) float64 {
	seconds := float64(b.elapsedTicks) / float64(pcrClockHz)
	if seconds <= 0 {
		return 0
	}
	return float64(b.stats[pid].totalBytes) * 8 / seconds
}

// peakBps returns the highest bitrate of a PID over any full 1-second window, or
// 0 if no full window was measured.
func (b *BitrateStats) peakBps(pid uint16) float64 {
	full := b.fullBuckets()
	if full <= 0 {
		return 0
	}
	var maxBytes int64
	for idx, bytes := range b.stats[pid].buckets {
		if idx < full && bytes > maxBytes {
			maxBytes = bytes
		}
	}
	return float64(maxBytes) * 8
}

// programPeakBps returns the highest aggregate bitrate of all program PIDs
// (excluding NULL) over any full 1-second window. It is the sum of the PIDs in
// the same window, not the sum of their individual peaks, so it reflects the
// real worst-case combined load.
func (b *BitrateStats) programPeakBps() float64 {
	full := b.fullBuckets()
	if full <= 0 {
		return 0
	}
	var maxBytes int64
	for idx := int64(0); idx < full; idx++ {
		var sum int64
		for pid := range b.programPids {
			sum += b.stats[pid].buckets[idx]
		}
		if sum > maxBytes {
			maxBytes = sum
		}
	}
	return float64(maxBytes) * 8
}

// Dump prints the per-PID bitrate summary. When no usable PCR time base was
// found (fewer than two PCRs in a single valid segment) the bitrates cannot be
// computed and the table reports "N/A".
func (b *BitrateStats) Dump() {
	fmt.Println("-----------------------------")
	if b.program > 0 {
		fmt.Printf("Bitrate Summary (program %d, PCR time base):\n", b.program)
	} else {
		fmt.Println("Bitrate Summary (PCR time base):")
	}

	seconds := float64(b.elapsedTicks) / float64(pcrClockHz)
	if !b.started || seconds <= 0 {
		fmt.Println("  analysis skipped - no usable PCR time base (need >=2 PCRs in one segment)")
		return
	}

	hasFullWindow := b.fullBuckets() > 0

	// Program PIDs, sorted, with a running total.
	pids := make([]uint16, 0, len(b.programPids))
	for pid := range b.programPids {
		pids = append(pids, pid)
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })

	const pidCol, typeCol, avgCol, peakCol = 8, 62, 16, 16
	header := fmt.Sprintf("  %-*s%-*s%*s%*s", pidCol, "PID", typeCol, "Type", avgCol, "Avg(bps)", peakCol, "Peak/1s(bps)")
	fmt.Println(header)

	var totalBytes int64
	for _, pid := range pids {
		printBitrateRow(pid, b.labels[pid], b.avgBps(pid), b.peakBps(pid), hasFullWindow, pidCol, typeCol, avgCol, peakCol)
		totalBytes += b.stats[pid].totalBytes
	}
	totalAvgBps := float64(totalBytes) * 8 / seconds
	totalPeakStr := "N/A"
	if hasFullWindow {
		totalPeakStr = commaInt(int64(b.programPeakBps()))
	}
	fmt.Printf("  %-*s%-*s%*s%*s\n", pidCol, "", typeCol, "Total (program)", avgCol, commaInt(int64(totalAvgBps)), peakCol, totalPeakStr)

	// NULL is reported after the total, outside it.
	if _, ok := b.stats[nullPidValue]; ok {
		printBitrateRow(nullPidValue, b.labels[nullPidValue], b.avgBps(nullPidValue), b.peakBps(nullPidValue), hasFullWindow, pidCol, typeCol, avgCol, peakCol)
	}

	fmt.Printf("  Duration: %.2fs (PCR, %d segment(s))\n", seconds, b.segments)
	if !hasFullWindow {
		fmt.Println("  Peak: N/A - stream shorter than one full 1-second window")
	}
}

func printBitrateRow(pid uint16, label string, avg, peak float64, hasFullWindow bool, pidCol, typeCol, avgCol, peakCol int) {
	peakStr := "N/A"
	if hasFullWindow {
		peakStr = commaInt(int64(peak))
	}
	fmt.Printf("  %-*s%-*s%*s%*s\n",
		pidCol, fmt.Sprintf("0x%04x", pid),
		typeCol, truncateLabel(label, typeCol),
		avgCol, commaInt(int64(avg)),
		peakCol, peakStr,
	)
}

// truncateLabel shortens a label to width columns so the table stays aligned for
// the very long stream_type descriptions.
func truncateLabel(s string, width int) string {
	if width <= 1 || len(s) <= width-1 {
		return s
	}
	if width < 4 {
		return s[:width-1]
	}
	return s[:width-4] + "..."
}

// commaInt formats a non-negative integer with thousands separators.
func commaInt(n int64) string {
	if n < 0 {
		return "-" + commaInt(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
