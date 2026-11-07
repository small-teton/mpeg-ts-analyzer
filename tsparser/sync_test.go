package tsparser

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// The sync-loss fixtures are a constant-rate stream whose alignment is
// damaged in four places and that ends with a partial packet
// (tools/generate_sync_loss). Slot n of the generator is packet n of the
// undamaged stream.
var syncLossFixtures = []struct {
	path       string
	packetSize int64
	// flag selects the packet size of the generator: none, -m2ts or -trailer.
	flag string
}{
	{"../sample_data/sample_188byte_sync_loss.ts", 188, ""},
	{"../sample_data/sample_192byte_sync_loss.ts", 192, "-m2ts"},
	{"../sample_data/sample_204byte_sync_loss.ts", 204, "-trailer"},
}

func generateSyncLoss(t *testing.T, args ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.ts")
	args = append([]string{"run", "../tools/generate_sync_loss", "-output", path}, args...)
	if output, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate fixture: %v\n%s", err, output)
	}
	return path
}

func TestSyncLossFixtureGenerator(t *testing.T) {
	for _, fixture := range syncLossFixtures {
		var args []string
		if fixture.flag != "" {
			args = append(args, fixture.flag)
		}
		generated, err := os.ReadFile(generateSyncLoss(t, args...))
		if err != nil {
			t.Fatal(err)
		}
		committed, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, committed) {
			t.Errorf("%s does not match the generator output", fixture.path)
		}
		if len(committed) >= 48*1024 {
			t.Errorf("%s is %d bytes", fixture.path, len(committed))
		}
	}
}

func int64Pointer(v int64) *int64 { return &v }

// fixtureSync is the sync result expected for the committed fixtures.
func fixtureSync(ps int64) ReportSync {
	return ReportSync{
		Status:                           "recovered",
		RequiredSyncBytes:                5,
		MaxResyncScanBytes:               1 << 20,
		LossCount:                        4,
		ResyncCount:                      4,
		SkippedBytes:                     ps + 1 + (ps - 1) + 1000,
		FirstLossOffsetBytes:             int64Pointer(40 * ps),
		FirstResyncOffsetBytes:           int64Pointer(41 * ps),
		MaxSkippedBytes:                  int64Pointer(1000),
		MaxSkippedOffsetBytes:            int64Pointer(121 * ps),
		TrailingPartialPacketBytes:       100,
		TrailingPartialPacketOffsetBytes: int64Pointer(200*ps + 1000),
	}
}

func TestSyncLossFixtures(t *testing.T) {
	for _, fixture := range syncLossFixtures {
		ps := fixture.packetSize
		report, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatalf("%s: %v", fixture.path, err)
		}
		if report.Status != "complete" || report.Reason != "" || *report.AnalysisStartOffsetBytes != 4*ps || *report.AnalysisEndOffsetBytes != 200*ps+1000 {
			t.Errorf("%s: envelope %s", fixture.path, toJSON(t, report))
		}
		if got, want := toJSON(t, report.Sync), toJSON(t, fixtureSync(ps)); got != want {
			t.Errorf("%s: sync\n got %s\nwant %s", fixture.path, got, want)
		}
		// Later findings keep their absolute offsets: the video packets in
		// slots 40 and 82 are lost, so slots 42 and 84 have continuity errors.
		// Slot 42 is after the first loss and slot 84 after the inserted and
		// the deleted byte, which cancel out.
		events := report.ContinuityEvents
		if len(events) != 2 || events[0].OffsetBytes != 42*ps || events[1].OffsetBytes != 84*ps || events[0].PID != "0x0100" || events[1].PID != "0x0100" {
			t.Errorf("%s: continuity events %s", fixture.path, toJSON(t, events))
		}
		// The PES and PCR points after the garbage block are 1000 bytes later
		// than in the undamaged stream.
		pcr := report.Programs[0].Series.PCR
		if last := pcr[len(pcr)-1]; last.OffsetBytes != 198*ps+1000 || last.PCRMs != 10_000+198*10 {
			t.Errorf("%s: last PCR %s", fixture.path, toJSON(t, last))
		}
		// Every packet outside the skipped bytes is analyzed exactly once: the
		// same per-PID counts as TSDuck after tsresync.
		counts := map[string]int64{}
		for _, pid := range report.PSILayout.PIDs {
			counts[pid.PID] = pid.PacketCount
		}
		if counts["0x0000"] != 6 || counts["0x1000"] != 6 || counts["0x0100"] != 96 || counts["0x1fff"] != 90 || len(counts) != 4 {
			t.Errorf("%s: packet counts %v", fixture.path, counts)
		}
		if len(report.Warnings) != 1 || report.Warnings[0].OffsetBytes != 200*ps+1000 {
			t.Errorf("%s: warnings %s", fixture.path, toJSON(t, report.Warnings))
		}

		// The summary report carries the same result.
		s := summary(t, fixture.path, options.Options{})
		if s.Status != "complete" || toJSON(t, s.Sync) != toJSON(t, fixtureSync(ps)) {
			t.Errorf("%s: summary %s", fixture.path, toJSON(t, s))
		}
	}
}

func TestSyncLossFixtureText(t *testing.T) {
	out := captureStdout(t, func() {
		if err := ParseTsFile(syncLossFixtures[0].path, options.Options{}); err != nil {
			t.Error(err)
		}
	})
	for _, line := range []string{
		"0x00001d60 sync lost (found 0x00 instead of 0x47); resynchronized at 0x00001e1c after skipping 188 bytes\n",
		"0x00002d88 sync lost (found 0xff instead of 0x47); resynchronized at 0x00002d89 after skipping 1 bytes\n",
		"0x00003c39 sync lost (found 0x41 instead of 0x47); resynchronized at 0x00003cf4 after skipping 187 bytes\n",
		"0x000058dc sync lost (found 0x00 instead of 0x47); resynchronized at 0x00005cc4 after skipping 1000 bytes\n",
		"0x000096c8 ignored 100 trailing bytes (incomplete packet at end of input)\n",
		"-----------------------------\nTS Sync Summary: recovered\n" +
			"  Sync losses   : 4 (first at 0x00001d60)\n" +
			"  Resyncs       : 4 (first at 0x00001e1c)\n" +
			"  Skipped bytes : 1376 (largest 1000 at 0x000058dc)\n" +
			"-----------------------------\nCompliance Check Results:\n",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("text output lacks %q:\n%s", line, out)
		}
	}
}

// Each kind of damage on its own, and the stream without damage.
func TestSyncLossDamageKinds(t *testing.T) {
	all := []string{"sync-byte", "insert-byte", "delete-byte", "garbage", "truncated-tail"}
	for _, fixture := range syncLossFixtures {
		ps, args := fixture.packetSize, []string{}
		if fixture.flag != "" {
			args = []string{fixture.flag}
		}
		for _, tc := range []struct {
			damage        string
			loss, skipped int64
			trailing      int
			packets       int64
		}{
			{"sync-byte", 40 * ps, ps, 0, 199},
			{"insert-byte", 62 * ps, 1, 0, 200},
			{"delete-byte", 82 * ps, ps - 1, 0, 199},
			{"garbage", 121 * ps, 1000, 0, 200},
			{"truncated-tail", 0, 0, 100, 200},
			{"", 0, 0, 0, 200},
		} {
			var omit []string
			for _, d := range all {
				if d != tc.damage {
					omit = append(omit, d)
				}
			}
			report, err := decodeRangeAnalysis(t, generateSyncLoss(t, append(args, "-omit", strings.Join(omit, ","))...), options.Options{})
			if err != nil {
				t.Fatal(err)
			}
			sync := report.Sync
			packets := int64(0)
			for _, pid := range report.PSILayout.PIDs {
				packets += pid.PacketCount
			}
			ok := report.Status == "complete" && sync.TrailingPartialPacketBytes == tc.trailing && packets == tc.packets
			if tc.skipped == 0 {
				ok = ok && sync.Status == "ok" && sync.LossCount == 0 && sync.FirstLossOffsetBytes == nil
			} else {
				ok = ok && sync.Status == "recovered" && sync.LossCount == 1 && sync.ResyncCount == 1 &&
					*sync.FirstLossOffsetBytes == tc.loss && sync.SkippedBytes == tc.skipped && *sync.FirstResyncOffsetBytes == tc.loss+tc.skipped
			}
			if !ok {
				t.Errorf("%d bytes %q: %d packets, %s", ps, tc.damage, packets, toJSON(t, report))
			}
		}
	}
}

// A loss followed by fewer than five aligned packets is not recovered: the
// analysis stops at the loss and the document is incomplete.
func TestSyncLossUnrecovered(t *testing.T) {
	for _, fixture := range syncLossFixtures {
		ps, args := fixture.packetSize, []string{"-unrecoverable"}
		if fixture.flag != "" {
			args = append(args, fixture.flag)
		}
		path := generateSyncLoss(t, args...)
		report, err := decodeRangeAnalysis(t, path, options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		want := fixtureSync(ps)
		want.Status, want.LossCount = "lost", 5
		want.UnrecoveredOffsetBytes = int64Pointer(197*ps + 1000)
		want.Reason = "synchronization lost at " + hex32(197*ps+1000) + " and not recovered: the input ended before 5 aligned sync bytes; the analysis stopped there"
		// The partial packet is part of the bytes the search could not align.
		want.TrailingPartialPacketBytes, want.TrailingPartialPacketOffsetBytes = 0, nil
		if report.Status != "incomplete" || report.Reason != "sync_lost" || *report.AnalysisEndOffsetBytes != 197*ps+1000 || len(report.Warnings) != 0 {
			t.Errorf("%d bytes: envelope %s", ps, toJSON(t, report))
		}
		if got := toJSON(t, report.Sync); got != toJSON(t, want) {
			t.Errorf("%d bytes: sync\n got %s\nwant %s", ps, got, toJSON(t, want))
		}
		s := summary(t, path, options.Options{})
		if s.Status != "incomplete" || s.Reason != "sync_lost" || toJSON(t, s.Sync) != toJSON(t, want) {
			t.Errorf("%d bytes: summary %s", ps, toJSON(t, s))
		}
		if ps != 188 {
			continue
		}
		out := captureStdout(t, func() {
			if err := ParseTsFile(path, options.Options{}); err != nil {
				t.Error(err)
			}
		})
		for _, line := range []string{
			"0x00009494 sync lost (found 0x00 instead of 0x47); the input ended before 5 aligned sync bytes, analysis stopped\n",
			"TS Sync Summary: lost\n  Sync losses   : 5 (first at 0x00001d60)\n",
			"  Not recovered : at 0x00009494, the input ended before 5 aligned sync bytes; analysis stopped\n",
		} {
			if !strings.Contains(out, line) {
				t.Errorf("text output lacks %q:\n%s", line, out)
			}
		}
	}
}

func hex32(v int64) string {
	const digits = "0123456789abcdef"
	b := []byte("0x00000000")
	for i := 9; i >= 2; i-- {
		b[i] = digits[v&0xF]
		v >>= 4
	}
	return string(b)
}

// syncTestStream is a single-program stream (PSI, PCR and PES packets)
// followed by the given bytes and then n null packets.
func syncTestStream(middle []byte, n int) []byte {
	data := syntheticStream([]syntheticProgram{{number: 1, pmtPid: 0x1000, pcrPid: 0x100, videoPid: 0x100, withPmt: true}}, nil, -1)
	data = append(data, middle...)
	for range n {
		data = append(data, nullTestPacket()...)
	}
	return data
}

func nullTestPacket() []byte {
	p := bytes.Repeat([]byte{0xFF}, 188)
	p[0], p[1], p[2], p[3] = 0x47, 0x1F, 0xFF, 0x10
	return p
}

// The search after a loss is bounded: a new alignment must start within
// maxResyncScanBytes of the loss.
func TestSyncResyncScanLimit(t *testing.T) {
	head := int64(len(syncTestStream(nil, 0)))
	for _, tc := range []struct {
		gap    int
		status string
	}{
		{maxResyncScanBytes, "recovered"},
		{maxResyncScanBytes + 1, "lost"},
	} {
		report := analyzeSynthetic(t, syncTestStream(make([]byte, tc.gap), 10), options.Options{})
		sync := report.Sync
		if sync.Status != tc.status || *sync.FirstLossOffsetBytes != head {
			t.Errorf("gap %d: %s", tc.gap, toJSON(t, sync))
		}
		if tc.status == "recovered" && (sync.SkippedBytes != int64(tc.gap) || report.Status != "complete") {
			t.Errorf("gap %d: %s", tc.gap, toJSON(t, report))
		}
		if tc.status == "lost" && (report.Status != "incomplete" || !strings.Contains(sync.Reason, "no 5 aligned sync bytes within 1048576 bytes") || *report.AnalysisEndOffsetBytes != head) {
			t.Errorf("gap %d: %s", tc.gap, toJSON(t, report))
		}
	}
}

// False sync bytes one packet apart are accepted only when five line up.
func TestSyncResyncNeedsFiveSyncBytes(t *testing.T) {
	head := int64(len(syncTestStream(nil, 0)))
	for _, tc := range []struct {
		falseSyncs int
		skipped    int64
	}{
		{1, 1000},
		{4, 1000},
		{5, 7}, // accepted: five packet headers starting 7 bytes in
	} {
		garbage := make([]byte, 1000)
		for k := range tc.falseSyncs {
			copy(garbage[7+k*188:], nullTestPacket()[:4])
		}
		sync := analyzeSynthetic(t, syncTestStream(garbage, 10), options.Options{}).Sync
		if sync.Status != "recovered" || *sync.FirstLossOffsetBytes != head || *sync.FirstResyncOffsetBytes != head+tc.skipped {
			t.Errorf("%d false sync bytes: %s", tc.falseSyncs, toJSON(t, sync))
		}
	}
}

// A range that ends before a new alignment is confirmed stops the analysis;
// a range that ends inside the input after a recovery is not truncated.
func TestSyncResyncAtRangeEnd(t *testing.T) {
	data := syncTestStream(append(make([]byte, 50), nullTestPacket()...), 10)
	head := int64(len(syncTestStream(nil, 0)))
	path := filepath.Join(t.TempDir(), "range.ts")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Five packets after the garbage do not fit in the range.
	report, err := decodeRangeAnalysis(t, path, options.Options{Limit: head + 50 + 4*188})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "incomplete" || report.Sync.Status != "lost" || !strings.Contains(report.Sync.Reason, "the range ended before 5 aligned sync bytes") {
		t.Errorf("range ends during the search: %s", toJSON(t, report))
	}
	// The range ends 100 bytes into a packet: inside the input, so nothing
	// is truncated; at the end of the input, the 100 bytes are.
	for _, tc := range []struct {
		data     []byte
		trailing int
	}{
		{data, 0},
		{data[:int(head)+50+6*188+100], 100},
	} {
		if err := os.WriteFile(path, tc.data, 0o600); err != nil {
			t.Fatal(err)
		}
		report, err = decodeRangeAnalysis(t, path, options.Options{Limit: head + 50 + 6*188 + 100})
		if err != nil {
			t.Fatal(err)
		}
		if report.Status != "complete" || report.Sync.Status != "recovered" || report.Sync.TrailingPartialPacketBytes != tc.trailing || *report.AnalysisEndOffsetBytes != head+50+6*188 {
			t.Errorf("range ends after the recovery (trailing %d): %s", tc.trailing, toJSON(t, report))
		}
	}
}

func TestSyncNotAcquired(t *testing.T) {
	data := bytes.Repeat([]byte{0x47, 0x00, 0x00, 0x00, 0x11}, 4000)
	path := filepath.Join(t.TempDir(), "noise.ts")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := decodeJSONReport(t, func() error { return ParseTsFile(path, options.Options{Format: "json"}) })
	if !errors.Is(err, errSyncNotAcquired) || report.Status != "failed" || report.Reason != "sync_not_acquired" || report.Sync != nil {
		t.Errorf("err = %v, report %s", err, toJSON(t, report))
	}
	// Aligned packets without a PAT are a different failure.
	if err := os.WriteFile(path, syncTestStream(nil, 20)[188*4:], 0o600); err != nil {
		t.Fatal(err)
	}
	report, err = decodeJSONReport(t, func() error { return ParseTsFile(path, options.Options{Format: "json"}) })
	if err == nil || errors.Is(err, errSyncNotAcquired) || report.Reason != "analysis_failed" {
		t.Errorf("err = %v, report %s", err, toJSON(t, report))
	}
	// Also when the first read has no alignment (detection falls back to 188
	// bytes) and a later one holds 192-byte packets.
	var m2ts []byte
	for _, p := range [][]byte{nullTestPacket(), nullTestPacket(), nullTestPacket(), nullTestPacket()} {
		m2ts = append(append(m2ts, 0, 0, 0, 0), p...)
	}
	if err := os.WriteFile(path, append(bytes.Repeat([]byte{0x00}, 70000), m2ts...), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err = decodeJSONReport(t, func() error { return ParseTsFile(path, options.Options{Format: "json"}) })
	if err == nil || errors.Is(err, errSyncNotAcquired) || report.Reason != "analysis_failed" {
		t.Errorf("late M2TS alignment: err = %v, report %s", err, toJSON(t, report))
	}
}

// failingReader returns data, then err.
type failingReader struct {
	data []byte
	err  error
}

func (r *failingReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestPacketReaderReadErrors(t *testing.T) {
	failure := errors.New("disk failure")
	packet := make([]byte, 188)
	// While aligned.
	r := newPacketReader(&failingReader{data: nullTestPacket()[:100], err: failure}, 0, 188, 0)
	if _, err := r.next(packet); !errors.Is(err, failure) {
		t.Errorf("aligned: err = %v", err)
	}
	// While searching for a new alignment.
	r = newPacketReader(&failingReader{data: make([]byte, 300), err: failure}, 0, 188, 0)
	if _, err := r.next(packet); !errors.Is(err, failure) {
		t.Errorf("searching: err = %v", err)
	}
}

// seekFailingReader fails every Seek.
type seekFailingReader struct{ io.Reader }

func (seekFailingReader) Seek(int64, int) (int64, error) { return 0, errors.New("seek failure") }

// A packet that cannot be parsed after a recovery still ends the scan; the
// input is repositioned after that packet as without a lookahead.
func TestScanParseErrorAfterResync(t *testing.T) {
	bad := nullTestPacket()
	bad[3] = 0x00 // adaptation_field_control 0
	data := append([]byte{0x00}, nullTestPacket()...)
	for range 5 {
		data = append(data, nullTestPacket()...)
	}
	data = append(data, bad...)
	data = append(data, nullTestPacket()...)
	badPos := int64(1 + 6*188)

	reader := bytes.NewReader(data)
	var pos int64
	_, err := scanPrograms(reader, &pos, nil, newEmptyContinuitySummary(), newTransportTrustSummary(), nil, options.Options{}, 188, 0, true)
	if err == nil || !strings.Contains(err.Error(), "failed to parse TS packet") || pos != badPos {
		t.Errorf("err = %v at %d", err, pos)
	}
	if offset, _ := reader.Seek(0, io.SeekCurrent); offset != badPos+188 {
		t.Errorf("input at %d, want %d", offset, badPos+188)
	}

	pos = 0
	_, err = scanPrograms(seekFailingReader{bytes.NewReader(data)}, &pos, nil, newEmptyContinuitySummary(), newTransportTrustSummary(), nil, options.Options{}, 188, 0, true)
	if err == nil || !strings.Contains(err.Error(), "file seek error") {
		t.Errorf("seek failure: err = %v", err)
	}
}

func TestSyncStatusStrings(t *testing.T) {
	for status, want := range map[analysis.SyncStatus]string{analysis.SyncOK: "ok", analysis.SyncRecovered: "recovered", analysis.SyncLost: "lost"} {
		if got := syncStatusString(status); got != want {
			t.Errorf("%d: %q", status, got)
		}
	}
	if hasAlignment(bytes.Repeat([]byte{0x47}, 2*188), 188) || !hasAlignment(bytes.Repeat([]byte{0x47}, 2*188+1), 188) {
		t.Error("hasAlignment needs three sync bytes")
	}
}

// A packet that cannot be parsed still ends the scan of a program and makes
// the analysis look for the next PAT, in both output modes.
func TestScanParseErrorRetriesPatDiscovery(t *testing.T) {
	bad := nullTestPacket()
	bad[3] = 0x00 // adaptation_field_control 0
	head := int64(len(syncTestStream(nil, 0)))
	path := filepath.Join(t.TempDir(), "bad.ts")
	if err := os.WriteFile(path, syncTestStream(bad, 5), 0o600); err != nil {
		t.Fatal(err)
	}
	report := summary(t, path, options.Options{})
	message := "PES parse error: failed to parse TS packet in BufferPes: invalid adaptation_field_control: 0 (reserved); retrying PAT discovery"
	if report.Status != "incomplete" || len(report.Warnings) != 1 || report.Warnings[0] != (ReportWarning{head, message}) {
		t.Errorf("summary %s", toJSON(t, report))
	}
	out := captureStdout(t, func() {
		if err := ParseTsFile(path, options.Options{}); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, hex32(head)+" PES parse error: failed to parse TS packet in BufferPes: invalid adaptation_field_control: 0 (reserved), retrying PAT discovery...\n") {
		t.Errorf("text output:\n%s", out)
	}
}
