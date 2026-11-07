package analysis

import (
	"reflect"
	"testing"
)

func TestTransportTrustCheckHealthy(t *testing.T) {
	c := NewTransportTrustCheck()
	c.ObserveLayout(ProgramLayout{PMTPID: 0x1000, PCRPID: 0x100, Streams: []ElementaryStream{{PID: 0x100, StreamType: 0x1B}}})
	got := c.Result()
	for name, r := range map[string]AffectedPackets{"transport errors": got.TransportErrors, "scrambled": got.Scrambled} {
		if r.Status != StatusOK || r.Packets != 0 || r.Excluded != 0 || r.FirstOffset != 0 || r.PIDs == nil || len(r.PIDs) != 0 {
			t.Errorf("%s = %+v, want ok with an empty PID list", name, r)
		}
	}
}

func TestTransportTrustCheckCounts(t *testing.T) {
	c := NewTransportTrustCheck()
	c.ObserveLayout(ProgramLayout{PMTPID: 0x1000, PCRPID: 0x100, Streams: []ElementaryStream{{PID: 0x100, StreamType: 0x02}, {PID: 0x101, StreamType: 0x03}}})
	// Offsets need not arrive in order: the first offset is the smallest.
	for _, o := range []TransportError{
		{PID: 0x101, Offset: 940, Excluded: true},
		{PID: 0x1FFF, Offset: 0},
		{PID: 0x100, Offset: 5640, Excluded: true},
		{PID: 0x100, Offset: 1880, Excluded: true},
		{PID: 0x0200, Offset: 376},
	} {
		c.ObserveTransportError(o)
	}
	c.ObserveScrambled(Scrambled{PID: 0x0200, Offset: 752})
	c.ObserveScrambled(Scrambled{PID: 0x0200, Offset: 564})
	got := c.Result()
	want := TransportTrustResult{
		TransportErrors: AffectedPackets{Status: StatusDataExcluded, Packets: 5, Excluded: 3, FirstOffset: 0, PIDs: []AffectedPID{
			{PID: 0x0100, Label: "video", Packets: 2, Excluded: 2, FirstOffset: 1880},
			{PID: 0x0101, Label: "audio", Packets: 1, Excluded: 1, FirstOffset: 940},
			{PID: 0x0200, Label: "unknown", Packets: 1, FirstOffset: 376},
			{PID: 0x1FFF, Label: "null", Packets: 1, FirstOffset: 0},
		}},
		Scrambled: AffectedPackets{Status: StatusAffected, Packets: 2, FirstOffset: 564, PIDs: []AffectedPID{
			{PID: 0x0200, Label: "unknown", Packets: 2, FirstOffset: 564},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Result() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestTransportTrustCheckLabels(t *testing.T) {
	// A layout name takes precedence over the null fallback, and a layout
	// observed after the packet still names it.
	c := NewTransportTrustCheck()
	c.ObserveScrambled(Scrambled{PID: 0x1FFF, Offset: 188, Excluded: true})
	c.ObserveScrambled(Scrambled{PID: 0x0000, Offset: 376, Excluded: true})
	c.ObserveLayout(ProgramLayout{PMTPID: 0x1000, PCRPID: 0x1FFF, Streams: []ElementaryStream{{PID: 0x1FFF, StreamType: 0x06}}})
	got := c.Result().Scrambled
	if got.Status != StatusDataExcluded || got.PIDs[0].Label != "PAT" || got.PIDs[1].Label != "stream_type 0x06" {
		t.Errorf("Scrambled = %+v", got)
	}
}
