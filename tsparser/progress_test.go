package tsparser

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// captureProgress redirects progress events and controls the clock for one test.
func captureProgress(t *testing.T, interval time.Duration) (*bytes.Buffer, *time.Time) {
	t.Helper()
	var out bytes.Buffer
	clock := time.Unix(0, 0)
	oldOutput, oldInterval, oldNow := progressOutput, progressInterval, progressNow
	progressOutput, progressInterval, progressNow = &out, interval, func() time.Time { return clock }
	t.Cleanup(func() { progressOutput, progressInterval, progressNow = oldOutput, oldInterval, oldNow })
	return &out, &clock
}

func decodeProgress(t *testing.T, out *bytes.Buffer) []progressEvent {
	t.Helper()
	var events []progressEvent
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var event progressEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("progress line is not JSON: %q", line)
		}
		events = append(events, event)
	}
	return events
}

func TestProgressTotal(t *testing.T) {
	for _, tc := range []struct{ size, offset, limit, want int64 }{
		{1000, 0, 0, 1000},
		{1000, 200, 0, 800},
		{1000, 200, 300, 300},
		{1000, 900, 300, 100},
		{1000, 2000, 0, 0},
	} {
		if got := progressTotal(tc.size, tc.offset, tc.limit); got != tc.want {
			t.Errorf("progressTotal(%d, %d, %d) = %d, want %d", tc.size, tc.offset, tc.limit, got, tc.want)
		}
	}
}

func TestFileSizeOfClosedFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "size")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if got := fileSize(f); got != 0 {
		t.Errorf("fileSize(closed) = %d, want 0", got)
	}
}

func TestProgressReadSeekerReportsFurthestPosition(t *testing.T) {
	out, clock := captureProgress(t, time.Second)
	p := newProgressReadSeeker(bytes.NewReader(make([]byte, 1000)), 100, 500)
	buf := make([]byte, 200)

	if _, err := p.Seek(100, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	_, _ = p.Read(buf) // furthest 300; no event before the interval
	if out.Len() != 0 {
		t.Fatalf("event emitted before the interval: %q", out.String())
	}
	*clock = clock.Add(time.Second)
	_, _ = p.Seek(0, io.SeekStart)
	_, _ = p.Read(buf) // seeking back does not reduce progress
	*clock = clock.Add(time.Second)
	_, _ = p.Seek(700, io.SeekStart)
	_, _ = p.Read(buf) // furthest 900, reported as 500 (clamped to the total)
	p.finish()

	events := decodeProgress(t, out)
	want := []progressEvent{
		{Type: "progress", ProcessedBytes: 200, TotalBytes: 500},
		{Type: "progress", ProcessedBytes: 500, TotalBytes: 500},
		{Type: "progress", ProcessedBytes: 500, TotalBytes: 500, Done: true},
	}
	if len(events) != len(want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, events[i], want[i])
		}
	}
}

func TestProgressReadSeekerSeekError(t *testing.T) {
	captureProgress(t, time.Hour)
	p := newProgressReadSeeker(&errReadSeeker{data: make([]byte, 10), failAt: -1, seekFailAt: 1}, 0, 10)
	if _, err := p.Seek(5, io.SeekStart); err == nil {
		t.Fatal("expected seek error")
	}
	if p.pos != 0 {
		t.Errorf("pos = %d after failed seek, want 0", p.pos)
	}
}

func TestParseTsFileProgress(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		out, _ := captureProgress(t, 0)
		var err error
		captureStdout(t, func() {
			err = ParseTsFile(sample188Path, options.Options{Format: format, Progress: "json", Offset: 188, Limit: 188 * 1000})
		})
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		events := decodeProgress(t, out)
		if len(events) < 2 {
			t.Fatalf("%s: events = %+v", format, events)
		}
		last := events[len(events)-1]
		if !last.Done || last.TotalBytes != 188*1000 || last.ProcessedBytes != last.TotalBytes {
			t.Errorf("%s: final event = %+v", format, last)
		}
		for i := 1; i < len(events); i++ {
			if events[i].ProcessedBytes < events[i-1].ProcessedBytes {
				t.Errorf("%s: progress went backwards: %+v", format, events)
			}
		}
	}
}

func TestParseTsFileWithoutProgressWritesNoEvents(t *testing.T) {
	out, _ := captureProgress(t, 0)
	captureStdout(t, func() { _ = ParseTsFile(sample188Path, options.Options{Format: "json"}) })
	if out.Len() != 0 {
		t.Errorf("unexpected progress output: %q", out.String())
	}
}

func TestParseTsFileRejectsUnknownProgressFormat(t *testing.T) {
	err := ParseTsFile(sample188Path, options.Options{Progress: "xml"})
	if err == nil || !strings.Contains(err.Error(), `unsupported progress format "xml"`) {
		t.Errorf("err = %v", err)
	}
}

func TestParseTsFileRejectsNegativeRanges(t *testing.T) {
	for _, tt := range []struct {
		options options.Options
		message string
	}{
		{options.Options{Offset: -1}, "--offset must not be negative"},
		{options.Options{Limit: math.MinInt64}, "--limit must not be negative"},
		{options.Options{Format: "xml"}, `unsupported output format "xml"`},
	} {
		err := ParseTsFile(sample188Path, tt.options)
		if err == nil || !strings.Contains(err.Error(), tt.message) {
			t.Errorf("%+v: err = %v", tt.options, err)
		}
	}
}

// An operation that has its answer before the end of the range, such as
// listing programs, ends with done=true while processed_bytes < total_bytes.
func TestParseTsFileProgressEndsEarly(t *testing.T) {
	out, _ := captureProgress(t, 0)
	var err error
	captureStdout(t, func() {
		err = ParseTsFile(sample188Path, options.Options{Format: "json", Progress: "json", ListPrograms: true})
	})
	if err != nil {
		t.Fatal(err)
	}
	events := decodeProgress(t, out)
	last := events[len(events)-1]
	if !last.Done || last.ProcessedBytes >= last.TotalBytes {
		t.Errorf("final event = %+v, want done before the end of the range", last)
	}
}

func TestWriteCapabilities(t *testing.T) {
	var out bytes.Buffer
	if err := WriteCapabilities(&out, "9.9.9"); err != nil {
		t.Fatal(err)
	}
	var got Capabilities
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CapabilitiesVersion != "1.0" || got.CLIVersion != "9.9.9" ||
		!reflect.DeepEqual(got.Outputs, []CapabilityOutput{
			{Name: "summary", Format: "json", SchemaVersion: "2.0"},
			{Name: "analysis", Format: "json", SchemaVersion: "2.0"},
			{Name: "packets", Format: "json", SchemaVersion: "1.0"},
			{Name: "packet", Format: "json", SchemaVersion: "1.0"},
		}) ||
		len(got.ProgressFormats) != 1 || got.ProgressFormats[0] != "json" ||
		!reflect.DeepEqual(got.FailOnCategories, FailureCategories()) {
		t.Errorf("capabilities = %+v", got)
	}
}
