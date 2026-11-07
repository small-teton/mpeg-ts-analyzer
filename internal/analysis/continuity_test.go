package analysis

import (
	"reflect"
	"testing"
)

func TestContinuityCheckHealthy(t *testing.T) {
	c := NewContinuityCheck()
	c.ObserveLayout(ProgramLayout{PMTPID: 0x1000, PCRPID: 0x100, Streams: []ElementaryStream{{PID: 0x100, StreamType: 0x1B}}})
	got := c.Result()
	if got.Status != StatusOK || got.TotalErrors != 0 || got.PIDs == nil || len(got.PIDs) != 0 {
		t.Errorf("Result() = %+v, want ok with an empty PID list", got)
	}
}

func TestContinuityCheckErrors(t *testing.T) {
	c := NewContinuityCheck()
	streams := []ElementaryStream{
		{PID: 0x100, StreamType: 0x1B},
		{PID: 0x101, StreamType: 0x0F},
		{PID: 0x102, StreamType: 0x06},
	}
	c.ObserveLayout(ProgramLayout{PMTPID: 0x1000, PCRPID: 0x1FF, Streams: streams})
	// The caller owns Streams and may reuse it after the call.
	streams[0] = ElementaryStream{PID: 0x100, StreamType: 0x06}
	for _, pid := range []uint16{0x0999, 0x101, 0x100, 0x0000, 0x1000, 0x1FF, 0x102, 0x100} {
		c.ObserveError(ContinuityError{PID: pid, Offset: 188})
	}
	want := ContinuityResult{Status: StatusErrors, TotalErrors: 8, PIDs: []ContinuityPID{
		{PID: 0x0000, Label: "PAT", Errors: 1},
		{PID: 0x0100, Label: "video", Errors: 2},
		{PID: 0x0101, Label: "audio", Errors: 1},
		{PID: 0x0102, Label: "stream_type 0x06", Errors: 1},
		{PID: 0x01FF, Label: "PCR", Errors: 1},
		{PID: 0x0999, Label: "unknown", Errors: 1},
		{PID: 0x1000, Label: "PMT", Errors: 1},
	}}
	if got := c.Result(); !reflect.DeepEqual(got, want) {
		t.Errorf("Result() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestContinuityCheckLabelPrecedence(t *testing.T) {
	// Elementary streams name a PID shared with the PCR_PID; an unset or
	// "no PCR" PCR_PID names nothing; a later layout renames a PID.
	for _, tt := range []struct {
		name    string
		layouts []ProgramLayout
		pid     uint16
		want    string
	}{
		{"stream over PCR", []ProgramLayout{{PMTPID: 0x1000, PCRPID: 0x100, Streams: []ElementaryStream{{PID: 0x100, StreamType: 0x02}}}}, 0x100, "video"},
		{"PCR only", []ProgramLayout{{PMTPID: 0x1000, PCRPID: 0x100}}, 0x100, "PCR"},
		{"unset PCR_PID", []ProgramLayout{{PMTPID: 0x1000, PCRPID: 0}}, 0, "PAT"},
		{"no PCR", []ProgramLayout{{PMTPID: 0x1000, PCRPID: 0x1FFF}}, 0x1FFF, "unknown"},
		{"renamed", []ProgramLayout{
			{PMTPID: 0x1000, Streams: []ElementaryStream{{PID: 0x101, StreamType: 0x03}}},
			{PMTPID: 0x1001, Streams: []ElementaryStream{{PID: 0x101, StreamType: 0x24}}},
		}, 0x101, "video"},
		{"kept after removal", []ProgramLayout{
			{PMTPID: 0x1000, Streams: []ElementaryStream{{PID: 0x101, StreamType: 0x03}}},
			{PMTPID: 0x1000},
		}, 0x101, "audio"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := NewContinuityCheck()
			for _, layout := range tt.layouts {
				c.ObserveLayout(layout)
			}
			c.ObserveError(ContinuityError{PID: tt.pid})
			if got := c.Result().PIDs[0].Label; got != tt.want {
				t.Errorf("label = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStreamTypeLabel(t *testing.T) {
	for _, tt := range []struct {
		streamType uint8
		want       string
	}{
		{0x01, "video"}, {0x02, "video"}, {0x10, "video"}, {0x1B, "video"}, {0x24, "video"},
		{0x03, "audio"}, {0x04, "audio"}, {0x0F, "audio"}, {0x11, "audio"},
		{0x06, "stream_type 0x06"},
	} {
		if got := streamTypeLabel(tt.streamType); got != tt.want {
			t.Errorf("stream_type 0x%02x label = %q, want %q", tt.streamType, got, tt.want)
		}
	}
}
