package tsparser

import (
	"math"
	"os"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// oneSecond is one second of the 27 MHz PCR clock, the unit these tests build
// synthetic PCR timelines from.
const oneSecond = uint64(pcrClockHz)

// newTestStats builds a collector for a simple program: video (0x100, also the
// PCR carrier), audio (0x101), PMT 0x30.
func newTestStats() *BitrateStats {
	infos := []ProgramInfo{
		{streamType: 0x02, elementaryPid: 0x100},
		{streamType: 0x03, elementaryPid: 0x101},
	}
	return NewBitrateStats(1, 0x30, 0x100, infos)
}

func countN(b *BitrateStats, pid uint16, n int) {
	for i := 0; i < n; i++ {
		b.CountPacket(pid)
	}
}

func approx(got, want float64) bool { return math.Abs(got-want) < 1e-6 }

func TestBitrateConstantRate(t *testing.T) {
	b := newTestStats()
	b.MarkPcr(0, false)  // first PCR: establishes t=0
	countN(b, 0x101, 10) // 10 * 188 = 1880 bytes in [0s, 1s)
	b.MarkPcr(oneSecond, false)
	countN(b, 0x101, 20) // 3760 bytes in [1s, 2s)
	b.MarkPcr(2*oneSecond, false)

	if b.segments != 1 {
		t.Fatalf("segments = %d, want 1", b.segments)
	}
	if got := b.fullBuckets(); got != 2 {
		t.Fatalf("fullBuckets = %d, want 2", got)
	}
	// avg = (1880+3760)*8 / 2s = 22560 bps
	if got := b.avgBps(0x101); !approx(got, 22560) {
		t.Errorf("avg audio = %v, want 22560", got)
	}
	// peak = max(1880, 3760)*8 = 30080 bps
	if got := b.peakBps(0x101); !approx(got, 30080) {
		t.Errorf("peak audio = %v, want 30080", got)
	}
}

func TestBitrateExcludesBytesBeforeFirstPcr(t *testing.T) {
	b := newTestStats()
	countN(b, 0x101, 100) // before any PCR: no time reference, must be dropped
	b.MarkPcr(0, false)
	countN(b, 0x101, 10)
	b.MarkPcr(oneSecond, false)

	// Only the 10 packets after the first PCR count: 1880*8/1s = 15040 bps.
	if got := b.avgBps(0x101); !approx(got, 15040) {
		t.Errorf("avg = %v, want 15040 (pre-first-PCR bytes must be excluded)", got)
	}
}

func TestBitrateExcludesPartialTrailingWindow(t *testing.T) {
	b := newTestStats()
	b.MarkPcr(0, false)
	countN(b, 0x101, 10)
	b.MarkPcr(oneSecond, false)
	countN(b, 0x101, 5) // trailing 0.5s partial window
	b.MarkPcr(oneSecond+oneSecond/2, false)

	if got := b.fullBuckets(); got != 1 {
		t.Fatalf("fullBuckets = %d, want 1 (partial window excluded)", got)
	}
	// peak considers only the full first window (bucket 0 = 1880 bytes).
	if got := b.peakBps(0x101); !approx(got, 15040) {
		t.Errorf("peak = %v, want 15040", got)
	}
}

func TestBitrateDiscontinuityDropsGap(t *testing.T) {
	b := newTestStats()
	b.MarkPcr(0, false)
	countN(b, 0x101, 10)
	b.MarkPcr(oneSecond, false)  // valid interval: counted
	countN(b, 0x101, 999)        // bytes spanning a discontinuity: unknown duration
	b.MarkPcr(5*oneSecond, true) // discontinuity_indicator set -> drop the gap
	countN(b, 0x101, 10)
	b.MarkPcr(6*oneSecond, false) // valid again in the new segment

	if b.segments != 2 {
		t.Fatalf("segments = %d, want 2", b.segments)
	}
	// Only the two valid 1880-byte intervals count; the 999-packet gap is dropped.
	// elapsed = 1s (first) + 1s (last) = 2s, total = 3760 bytes -> 15040 bps.
	if got := b.avgBps(0x101); !approx(got, 15040) {
		t.Errorf("avg = %v, want 15040 (discontinuity gap must be dropped)", got)
	}
}

func TestBitrateWrapTreatedAsSegmentBoundary(t *testing.T) {
	b := newTestStats()
	b.MarkPcr(2*oneSecond, false)
	countN(b, 0x101, 10)
	b.MarkPcr(3*oneSecond, false)
	countN(b, 0x101, 999)
	b.MarkPcr(oneSecond, false) // non-increasing PCR (wrap/reset) with no indicator

	if b.segments != 2 {
		t.Fatalf("segments = %d, want 2 (backward PCR must split a segment)", b.segments)
	}
}

func TestBitrateFallbackNoTimeBase(t *testing.T) {
	b := newTestStats()
	countN(b, 0x101, 10)
	b.MarkPcr(0, false) // only one PCR: no elapsed time

	if b.fullBuckets() != 0 {
		t.Errorf("fullBuckets = %d, want 0", b.fullBuckets())
	}
	if got := b.avgBps(0x101); got != 0 {
		t.Errorf("avg = %v, want 0 with no time base", got)
	}
	if got := b.peakBps(0x101); got != 0 {
		t.Errorf("peak = %v, want 0 with no full window", got)
	}
	if got := b.programPeakBps(); got != 0 {
		t.Errorf("program peak = %v, want 0 with no full window", got)
	}
}

func TestBitrateIgnoresUntrackedPid(t *testing.T) {
	b := newTestStats()
	b.MarkPcr(0, false)
	b.CountPacket(0x1234) // a PID from another program: not tracked
	b.MarkPcr(oneSecond, false)

	if _, ok := b.stats[0x1234]; ok {
		t.Error("untracked PID 0x1234 must not be recorded")
	}
}

func TestBitrateNullExcludedFromProgram(t *testing.T) {
	b := newTestStats()
	if b.programPids[nullPidValue] {
		t.Error("NULL PID must not be part of the program set")
	}
	if _, ok := b.stats[nullPidValue]; !ok {
		t.Error("NULL PID should still be tracked (reported separately)")
	}
	// PAT/PMT/PCR/ES are all program PIDs.
	for _, pid := range []uint16{patPidValue, 0x30, 0x100, 0x101} {
		if !b.programPids[pid] {
			t.Errorf("PID 0x%04x should be in the program set", pid)
		}
	}
}

func TestBitratePcrPidNoPcrValue(t *testing.T) {
	// PCR_PID 0x1FFF means "no PCR": it must not be registered as a program PID
	// (that would fold NULL stuffing into the program total) and must keep its
	// NULL label.
	infos := []ProgramInfo{{streamType: 0x02, elementaryPid: 0x100}}
	b := NewBitrateStats(1, 0x30, nullPidValue, infos)
	if b.programPids[nullPidValue] {
		t.Error("PCR_PID 0x1FFF must not be added to the program set")
	}
	if got := b.labels[nullPidValue]; got != "NULL (mux-wide)" {
		t.Errorf("label for 0x1FFF = %q, want NULL label", got)
	}
}

func TestBitrateProgramPeak(t *testing.T) {
	b := newTestStats()
	b.MarkPcr(0, false)
	countN(b, 0x100, 10) // 1880 B video   } window 0: combined 40 pkts = 7520 B
	countN(b, 0x101, 30) // 5640 B audio   }
	b.MarkPcr(oneSecond, false)
	countN(b, 0x100, 5) // window 1: combined 15 pkts = 2820 B
	countN(b, 0x101, 10)
	b.MarkPcr(2*oneSecond, false)

	// PAT/PMT/PCR carry 0 bytes here; the busiest window is window 0.
	// combined 40*188 = 7520 B * 8 = 60160 bps.
	if got := b.programPeakBps(); !approx(got, 60160) {
		t.Errorf("program peak = %v, want 60160", got)
	}
}

func TestBitratePcrCarrierLabelKeepsStreamType(t *testing.T) {
	// 0x100 is both the video ES and the PCR carrier; the stream_type label must
	// win over the generic "PCR" label.
	b := newTestStats()
	if got := b.labels[0x100]; got != StreamTypeString(0x02) {
		t.Errorf("label for PCR-carrying video PID = %q, want the video stream_type", got)
	}
}

func TestBitratePendingZeroEntrySkipped(t *testing.T) {
	// A PID counted in one interval but not the next leaves a zeroed pending
	// entry; the next MarkPcr must skip it without touching stats.
	b := newTestStats()
	b.MarkPcr(0, false)
	countN(b, 0x101, 10) // audio in interval 1
	b.MarkPcr(oneSecond, false)
	countN(b, 0x100, 5) // video only in interval 2; audio pending entry is 0
	b.MarkPcr(2*oneSecond, false)

	if got := b.stats[0x101].totalBytes; got != 10*tsPayloadSize {
		t.Errorf("audio bytes = %d, want %d (must not gain bytes in interval 2)", got, 10*tsPayloadSize)
	}
	if got := b.stats[0x100].totalBytes; got != 5*tsPayloadSize {
		t.Errorf("video bytes = %d, want %d", got, 5*tsPayloadSize)
	}
}

func TestBitrateDumpDirect(t *testing.T) {
	// started == false: no PCR was ever seen -> skipped message.
	NewBitrateStats(0, 0x30, 0x100, nil).Dump()

	// program > 0 header, with a full time base so the whole table is printed.
	b := NewBitrateStats(2, 0x30, 0x100, []ProgramInfo{
		{streamType: 0x02, elementaryPid: 0x100},
		{streamType: 0x03, elementaryPid: 0x101},
	})
	b.MarkPcr(0, false)
	countN(b, 0x100, 10)
	countN(b, 0x101, 30)
	b.MarkPcr(oneSecond, false)
	countN(b, 0x100, 10)
	countN(b, 0x101, 30)
	b.MarkPcr(2*oneSecond, false)
	b.Dump()
}

func TestCommaInt(t *testing.T) {
	cases := map[int64]string{0: "0", 12: "12", 123: "123", 1234: "1,234", 1234567: "1,234,567", -1234: "-1,234"}
	for in, want := range cases {
		if got := commaInt(in); got != want {
			t.Errorf("commaInt(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncateLabel(t *testing.T) {
	cases := []struct {
		s     string
		width int
		want  string
	}{
		{"short", 62, "short"},     // fits, returned unchanged
		{"abcdefghij", 6, "ab..."}, // width>=4, ellipsis
		{"abcdefghij", 3, "ab"},    // width<4, hard cut
		{"abc", 1, "abc"},          // width<=1, returned unchanged
	}
	for _, c := range cases {
		if got := truncateLabel(c.s, c.width); got != c.want {
			t.Errorf("truncateLabel(%q, %d) = %q, want %q", c.s, c.width, got, c.want)
		}
	}
}

// TestParseTsFile_DumpBitrateOption exercises the full parse path and the
// end-of-stream Dump with a time base long enough for a full 1-second window.
func TestParseTsFile_DumpBitrateOption(t *testing.T) {
	f, err := os.CreateTemp("", "bitrate*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	// PCRs 0.9 s apart: no discontinuity (<1000 ms), 1.8 s total -> one full window.
	writeFullStream(f, 2, []uint64{0, 24_300_000, 48_600_000})
	_ = f.Close()

	var opt options.Options
	opt.DumpBitrate = true
	if err := ParseTsFile(f.Name(), opt); err != nil {
		t.Errorf("expected successful parse with DumpBitrate, got: %s", err)
	}
}

// TestParseTsFile_DumpBitrateShortStream covers the branch where a PCR time base
// exists but the stream is shorter than one full 1-second window (peak = N/A).
func TestParseTsFile_DumpBitrateShortStream(t *testing.T) {
	f, err := os.CreateTemp("", "bitrateshort*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	writeFullStream(f, 2, []uint64{13500, 27000, 40500}) // sub-millisecond span
	_ = f.Close()

	var opt options.Options
	opt.DumpBitrate = true
	if err := ParseTsFile(f.Name(), opt); err != nil {
		t.Errorf("expected successful parse, got: %s", err)
	}
}

// TestParseTsFile_DumpBitrateNoTimeBase covers the "no usable PCR time base"
// path (a single PCR, so elapsed time is zero).
func TestParseTsFile_DumpBitrateNoTimeBase(t *testing.T) {
	f, err := os.CreateTemp("", "bitratenopcr*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(f.Name()) }()

	writeFullStream(f, 1, []uint64{13500}) // a single PCR
	_ = f.Close()

	var opt options.Options
	opt.DumpBitrate = true
	if err := ParseTsFile(f.Name(), opt); err != nil {
		t.Errorf("expected successful parse, got: %s", err)
	}
}
