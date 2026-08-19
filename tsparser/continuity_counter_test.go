package tsparser

import (
	"strings"
	"testing"
)

func TestContinuityCounterSummaryHealthy(t *testing.T) {
	s := newContinuityCounterSummary(0x1000, 0x0100, nil)
	if s.labels[0] != "PAT" || s.labels[0x1000] != "PMT" || s.labels[0x0100] != "PCR" {
		t.Fatalf("standard PID labels = %#v", s.labels)
	}
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
	s := newContinuityCounterSummary(0x1000, 0x0100, infos)
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

func continuityTestPacket(pid uint16, cc, afc uint8, pusi, discontinuity bool, content byte) *TsPacket {
	p := NewTsPacket()
	p.pid = pid
	p.continuityCounter = cc
	p.adaptationFieldControl = afc
	p.payloadUnitStartIndicator = 0
	if pusi {
		p.payloadUnitStartIndicator = 1
	}
	if discontinuity {
		p.adaptationField.discontinuityIndicator = 1
	}
	p.buf = append(p.buf, 0x47, byte(pid>>8), byte(pid), byte(afc<<4|cc), content)
	return p
}

func TestContinuityTrackerRules(t *testing.T) {
	tracker := newContinuityTracker()
	checkOK := func(name string, packet *TsPacket) continuityResult {
		t.Helper()
		result := tracker.Check(packet)
		if result.Event != nil {
			t.Fatalf("%s: unexpected event: %+v", name, result.Event)
		}
		return result
	}

	checkOK("first payload", continuityTestPacket(0x100, 15, 1, false, false, 0x10))
	checkOK("wraparound", continuityTestPacket(0x100, 0, 1, false, false, 0x20))
	duplicate := checkOK("exact duplicate", continuityTestPacket(0x100, 0, 1, false, false, 0x20))
	if !duplicate.Duplicate {
		t.Error("exact duplicate was not identified")
	}

	invalidSame := tracker.Check(continuityTestPacket(0x100, 0, 1, false, false, 0x30))
	if invalidSame.Event == nil || invalidSame.Event.Expected != 1 || invalidSame.Event.Actual != 0 {
		t.Fatalf("invalid same-counter event = %+v", invalidSame.Event)
	}
	checkOK("resynchronized payload", continuityTestPacket(0x100, 1, 1, false, false, 0x40))
	checkOK("adaptation only", continuityTestPacket(0x100, 1, 2, false, false, 0x50))
	checkOK("payload after adaptation", continuityTestPacket(0x100, 2, 1, false, false, 0x60))
	checkOK("declared discontinuity", continuityTestPacket(0x100, 9, 2, false, true, 0x70))
	checkOK("after declared discontinuity", continuityTestPacket(0x100, 10, 1, false, false, 0x80))
	checkOK("PUSI", continuityTestPacket(0x100, 11, 1, true, false, 0x90))

	gap := tracker.Check(continuityTestPacket(0x100, 14, 1, false, false, 0xA0))
	if gap.Event == nil || gap.Event.PID != 0x100 || gap.Event.Expected != 12 || gap.Event.Actual != 14 {
		t.Fatalf("gap event = %+v", gap.Event)
	}
	if gap.Event.Pos != 0 {
		t.Errorf("event pos = %d, want 0", gap.Event.Pos)
	}
	checkOK("resynchronized after gap", continuityTestPacket(0x100, 15, 1, false, false, 0xB0))

	checkOK("first adaptation-only", continuityTestPacket(0x200, 3, 2, false, false, 0xC0))
	badAdaptation := tracker.Check(continuityTestPacket(0x200, 4, 2, false, false, 0xD0))
	if badAdaptation.Event == nil || badAdaptation.Event.Expected != 3 {
		t.Fatalf("adaptation-only event = %+v", badAdaptation.Event)
	}
	checkOK("null packet excluded", continuityTestPacket(nullPidValue, 0, 1, false, false, 0xE0))
	checkOK("null counter arbitrary", continuityTestPacket(nullPidValue, 9, 1, false, false, 0xF0))
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
