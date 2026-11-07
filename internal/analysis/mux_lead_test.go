package analysis

import (
	"reflect"
	"testing"
)

var (
	audio2 = ElementaryStream{PID: 0x102, StreamType: 0x0F}
	video2 = ElementaryStream{PID: 0x0FF, StreamType: 0x1B}
	ac3    = ElementaryStream{PID: 0x103, StreamType: 0x81}
)

// led is a valid PES of program 1 at arrival (ms on segment 0) whose PTS, or
// DTS when dts is set, lies leadMs after its arrival.
func led(pid uint16, offset int64, arrival uint64, leadMs int64, dts bool) PES {
	at := atMs(arrival)
	stamp := uint64(int64(at.Ticks)+leadMs*ms) / 300
	o := PES{Program: 1, PID: pid, Offset: offset, HasPTS: true, PTS: stamp + 3600, Arrival: at}
	if dts {
		o.HasDTS, o.DTS = true, stamp
	} else {
		o.PTS = stamp
	}
	return o
}

func leadResult(t *testing.T, r MuxLeadResult, pid uint16) MuxLeadPID {
	t.Helper()
	for _, p := range r.PIDs {
		if p.PID == pid {
			return p
		}
	}
	t.Fatalf("PID 0x%04x not in result %+v", pid, r.PIDs)
	return MuxLeadPID{}
}

func TestLeadTicks(t *testing.T) {
	for _, tc := range []struct {
		name           string
		stamp, arrival uint64
		want           int64
	}{
		{"ahead", 90_000, 0, 1000 * ms},
		{"behind", 0, 9 * ms, -9 * ms},
		{"PTS wrapped, PCR not", 900, PCRWrapTicks - 10*ms, 20 * ms},
		{"PCR wrapped, PTS not", 1<<33 - 900, 10 * ms, -20 * ms},
		{"unwrapped arrival", 90_000 + 90, PCRWrapTicks + 1000*ms, ms},
		{"33-bit stamp beyond the period", 1<<33 + 90, 0, ms},
	} {
		if got := LeadTicks(tc.stamp, tc.arrival); got != tc.want {
			t.Errorf("%s: LeadTicks(%d, %d) = %d, want %d", tc.name, tc.stamp, tc.arrival, got, tc.want)
		}
	}
}

func TestClassifyAV(t *testing.T) {
	for streamType, want := range map[uint8]AVClass{
		0x01: AVVideo, 0x02: AVVideo, 0x1B: AVVideo, 0x24: AVVideo,
		0x03: AVAudio, 0x04: AVAudio, 0x0F: AVAudio, 0x11: AVAudio, 0x81: AVAudio, 0x87: AVAudio,
		0x06: AVOther, 0x10: AVOther, 0x86: AVOther,
	} {
		if got := ClassifyAV(streamType); got != want {
			t.Errorf("ClassifyAV(0x%02x) = %v, want %v", streamType, got, want)
		}
	}
	if AVOther.String() != "other" || AVVideo.String() != "video" || AVAudio.String() != "audio" {
		t.Error("AVClass names")
	}
}

func TestMuxLeadEmpty(t *testing.T) {
	want := MuxLeadResult{PIDs: []MuxLeadPID{}, Programs: []AVLeadProgram{}}
	if got := NewPESCheck().MuxLead(); !reflect.DeepEqual(got, want) {
		t.Errorf("MuxLead() = %+v, want %+v", got, want)
	}
}

func TestMuxLeadSteady(t *testing.T) {
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video, audio, data))
	for i := range uint64(5) {
		c.ObservePES(led(video.PID, int64(i*1000), i*40, 700+int64(i), true))
		c.ObservePES(led(audio.PID, int64(i*1000+500), i*40+20, 900, false))
	}
	c.ObservePES(led(data.PID, 9000, 300, -50, false))
	c.ObservePES(untimed(data.PID, 9100, atMs(310)))
	got := c.MuxLead()
	want := MuxLeadResult{
		PIDs: []MuxLeadPID{
			{Program: 1, PID: 0x100, StreamType: 0x02, Label: "video", Class: AVVideo, Status: PESInfo, Samples: 5, DTSSamples: 5,
				MinTicks: 700 * ms, MaxTicks: 704 * ms, MinOffset: 0, MaxOffset: 4000},
			{Program: 1, PID: 0x101, StreamType: 0x03, Label: "audio", Class: AVAudio, Status: PESInfo, Samples: 5,
				MinTicks: 900 * ms, MaxTicks: 900 * ms, MinOffset: 500, MaxOffset: 500},
			{Program: 1, PID: 0x102, StreamType: 0x06, Label: "stream_type 0x06", Class: AVOther, Status: PESInfo, Samples: 1, Negative: 1,
				MinTicks: -50 * ms, MaxTicks: -50 * ms, MinOffset: 9000, MaxOffset: 9000},
		},
		Programs: []AVLeadProgram{{Program: 1, Status: PESInfo, HasWorst: true,
			// Compared at each audio sample and at each later video sample:
			// the largest is the first audio (900) against video 700.
			Worst: AVLeadDifference{AudioPID: 0x101, Status: PESInfo, Comparisons: 9, VideoPID: 0x100, MaxTicks: 200 * ms,
				AudioLeadTicks: 900 * ms, VideoLeadTicks: 700 * ms, AudioOffset: 500, VideoOffset: 0},
		}},
	}
	want.Programs[0].Audio = []AVLeadDifference{want.Programs[0].Worst}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MuxLead() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestMuxLeadReasons(t *testing.T) {
	c := NewPESCheck()
	streams := []ElementaryStream{{0x200, 0x02}, {0x201, 0x03}, {0x202, 0x06}, {0x203, 0x06}, {0x204, 0x06}}
	c.ObserveProgramLayout(1, pesLayout(streams...))
	c.ObserveBreak(PESBreak{Program: 1, PID: 0x200, Offset: 0, Cause: BreakUntrustedPayload})
	c.ObservePES(PES{Program: 1, PID: 0x201, Offset: 188, Problem: PESHeaderBadFlags})
	c.ObservePES(untimed(0x203, 376, atMs(0)))
	c.ObservePES(pts(0x204, 564, ClockTime{}))
	c.ObservePES(pts(0x204, 752, ClockTime{}))
	got := c.MuxLead()
	for pid, reason := range map[uint16]PESReason{
		0x200: PESReasonUntrustedPayload, 0x201: PESReasonMalformedHeaders, 0x202: PESReasonNoPES, 0x203: PESReasonNoPTS, 0x204: PESReasonNoPCRTiming,
	} {
		if p := leadResult(t, got, pid); p.Status != PESInsufficientData || p.Reason != reason {
			t.Errorf("PID 0x%04x = %+v, want insufficient data (%s)", pid, p, reason)
		}
	}
	if p := leadResult(t, got, 0x204); p.Unbracketed != 2 || p.Samples != 0 {
		t.Errorf("unbracketed = %+v", p)
	}
	want := []AVLeadProgram{{Program: 1, Status: PESInsufficientData, Reason: PESReasonNoComparableLeads,
		Audio: []AVLeadDifference{{AudioPID: 0x201, Status: PESInsufficientData, Reason: PESReasonNoComparableLeads}}}}
	if !reflect.DeepEqual(got.Programs, want) {
		t.Errorf("Programs = %+v, want %+v", got.Programs, want)
	}
}

func TestMuxLeadPairing(t *testing.T) {
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video, audio, audio2, video2))
	// video2 (0x0FF) is the program's video: the lowest video PID.
	c.ObservePES(led(video.PID, 0, 0, 100, false))
	c.ObservePES(led(audio.PID, 188, 10, 400, false))  // no video lead yet
	c.ObservePES(led(video2.PID, 376, 20, 500, false)) // audio: 100 ms
	// Compared with the latest lead of every audio PID that has one.
	c.ObservePES(led(audio2.PID, 564, 30, 530, false)) // 30 ms
	// An unbracketed sample forgets the audio's lead.
	c.ObservePES(pts(audio.PID, 752, ClockTime{}))
	c.ObservePES(led(video2.PID, 940, 40, 520, false)) // audio2: 10 ms
	// A lead of another segment is not compared.
	c.ObservePES(PES{Program: 1, PID: audio.PID, Offset: 1128, HasPTS: true, PTS: 0, Arrival: ClockTime{Valid: true, Ticks: 0, Segment: 1}})
	c.ObservePES(led(audio.PID, 1316, 60, 600, false)) // 80 ms
	// A break forgets the video's lead.
	c.ObserveBreak(PESBreak{Program: 1, PID: video2.PID, Offset: 1400, Cause: BreakContinuityError})
	c.ObservePES(led(audio.PID, 1504, 70, 2000, false))
	// A malformed header forgets the audio's lead.
	c.ObservePES(led(video2.PID, 1692, 80, 500, false)) // audio: 1500 ms, audio2: 30 ms
	c.ObservePES(PES{Program: 1, PID: audio.PID, Offset: 1880, Problem: PESHeaderBadMarkerBits})
	c.ObservePES(led(video2.PID, 2068, 90, 500, false)) // audio2: 30 ms
	got := c.MuxLead().Programs
	wantAudio := AVLeadDifference{AudioPID: 0x101, Status: PESInfo, Comparisons: 3, VideoPID: 0x0FF, MaxTicks: 1500 * ms,
		AudioLeadTicks: 2000 * ms, VideoLeadTicks: 500 * ms, AudioOffset: 1504, VideoOffset: 1692}
	wantAudio2 := AVLeadDifference{AudioPID: 0x102, Status: PESInfo, Comparisons: 4, VideoPID: 0x0FF, MaxTicks: 30 * ms,
		AudioLeadTicks: 530 * ms, VideoLeadTicks: 500 * ms, AudioOffset: 564, VideoOffset: 376}
	want := []AVLeadProgram{{Program: 1, Status: PESInfo, HasWorst: true, Worst: wantAudio, Audio: []AVLeadDifference{wantAudio, wantAudio2}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Programs =\n%+v\nwant\n%+v", got, want)
	}
}

func TestMuxLeadLayoutChanges(t *testing.T) {
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video, audio))
	// Program 2 has video only, program 3 audio only: neither is listed.
	c.ObserveProgramLayout(2, pesLayout(video))
	c.ObserveProgramLayout(3, pesLayout(ac3))
	c.ObservePES(led(video.PID, 0, 0, 100, false))
	// Removing the audio forgets its lead; adding it back starts afresh.
	c.ObservePES(led(audio.PID, 188, 10, 300, false)) // 200 ms
	c.ObserveProgramLayout(1, pesLayout(video))
	c.ObserveProgramLayout(1, pesLayout(video, audio))
	c.ObservePES(led(video.PID, 376, 20, 150, false)) // audio has no lead
	// Retyping the video to a non-video type leaves the program without
	// video: the audio is not compared.
	c.ObserveProgramLayout(1, pesLayout(ElementaryStream{PID: video.PID, StreamType: 0x06}, audio))
	c.ObservePES(led(audio.PID, 564, 30, 900, false))
	c.ObservePES(led(video.PID, 752, 40, 100, false))
	// A retype back gives the video no lead until its next sample.
	c.ObserveProgramLayout(1, pesLayout(video, audio))
	c.ObservePES(led(audio.PID, 940, 50, 400, false))
	c.ObservePES(led(video.PID, 1128, 60, 380, false)) // 20 ms
	got := c.MuxLead().Programs
	wantAudio := AVLeadDifference{AudioPID: 0x101, Status: PESInfo, Comparisons: 2, VideoPID: 0x100, MaxTicks: 200 * ms,
		AudioLeadTicks: 300 * ms, VideoLeadTicks: 100 * ms, AudioOffset: 188, VideoOffset: 0}
	want := []AVLeadProgram{{Program: 1, Status: PESInfo, HasWorst: true, Worst: wantAudio, Audio: []AVLeadDifference{wantAudio}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Programs =\n%+v\nwant\n%+v", got, want)
	}
}

func TestMuxLeadWorstAudio(t *testing.T) {
	// The program's representative is the audio PID with the largest
	// difference; of equal ones the lowest PID.
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video, audio, audio2, ac3))
	c.ObserveProgramLayout(2, pesLayout(video, audio))
	c.ObserveProgramLayout(9, pesLayout(video, audio))
	c.ObservePES(led(video.PID, 0, 0, 100, false))
	c.ObservePES(led(ac3.PID, 188, 10, 150, false))
	c.ObservePES(led(audio2.PID, 376, 20, 150, false))
	programs := c.MuxLead().Programs
	if len(programs) != 3 || programs[1].Program != 2 || programs[2].Program != 9 {
		t.Fatalf("programs = %+v, want 1, 2 and 9", programs)
	}
	got := programs[0]
	if got.Worst.AudioPID != audio2.PID || got.Worst.MaxTicks != 50*ms || len(got.Audio) != 3 || got.Audio[0].Status != PESInsufficientData {
		t.Errorf("program = %+v", got)
	}
}
