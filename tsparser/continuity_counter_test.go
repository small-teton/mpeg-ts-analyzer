package tsparser

import (
	"strings"
	"testing"
)

func TestContinuityCounterSummaryHealthy(t *testing.T) {
	s := newContinuityCounterSummary(nil)
	got := captureStdout(t, s.Dump)
	want := "-----------------------------\nContinuity Counter: no errors detected\n"
	if got != want {
		t.Errorf("healthy summary:\n%s\nwant:\n%s", got, want)
	}
}

func TestContinuityCounterSummaryErrors(t *testing.T) {
	infos := []ProgramInfo{
		{streamType: 0x1B, elementaryPid: 0x100},
		{streamType: 0x0F, elementaryPid: 0x101},
		{streamType: 0x06, elementaryPid: 0x102},
	}
	s := newContinuityCounterSummary(infos)
	s.Add(0x101)
	s.Add(0x100)
	s.Add(0x100)
	s.Add(0x999)

	got := captureStdout(t, s.Dump)
	for _, want := range []string{
		"Continuity Counter Error Summary:",
		"PID 0x0100 (video) : 2 errors",
		"PID 0x0101 (audio) : 1 error",
		"PID 0x0999 (unknown) : 1 error",
		"Total              : 4 errors in 3 PIDs",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "PID 0x0100") > strings.Index(got, "PID 0x0101") {
		t.Errorf("PIDs are not sorted:\n%s", got)
	}
}

func TestContinuityCounterLabelsAndPlurals(t *testing.T) {
	for _, tt := range []struct {
		streamType uint8
		want       string
	}{
		{0x01, "video"}, {0x02, "video"}, {0x10, "video"}, {0x1B, "video"}, {0x24, "video"},
		{0x03, "audio"}, {0x04, "audio"}, {0x0F, "audio"}, {0x11, "audio"},
		{0x06, "stream_type 0x06"},
	} {
		if got := continuityCounterLabel(tt.streamType); got != tt.want {
			t.Errorf("stream_type 0x%02x label = %q, want %q", tt.streamType, got, tt.want)
		}
	}
	if got := errorWord(1); got != "error" {
		t.Errorf("errorWord(1) = %q", got)
	}
	if got := errorWord(2); got != "errors" {
		t.Errorf("errorWord(2) = %q", got)
	}
	if got := pidWord(1); got != "PID" {
		t.Errorf("pidWord(1) = %q", got)
	}
	if got := pidWord(2); got != "PIDs" {
		t.Errorf("pidWord(2) = %q", got)
	}
}
