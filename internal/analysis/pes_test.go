package analysis

import (
	"reflect"
	"testing"
)

const ms = ticksPerMs

// at is a time on segment 0 of the PCR timeline, in milliseconds.
func at(milliseconds uint64) ClockTime {
	return ClockTime{Valid: true, Ticks: 10_000*ms + milliseconds*ms}
}

func pesLayout(streams ...ElementaryStream) ProgramLayout {
	return ProgramLayout{PMTPID: 0x1000, PCRPID: 0x100, Streams: streams}
}

var (
	video = ElementaryStream{PID: 0x100, StreamType: 0x02}
	audio = ElementaryStream{PID: 0x101, StreamType: 0x03}
	data  = ElementaryStream{PID: 0x102, StreamType: 0x06}
)

// pts is a valid PES with a PTS of program 1.
func pts(pid uint16, offset int64, arrival ClockTime) PES {
	return PES{Program: 1, PID: pid, Offset: offset, HasPTS: true, PTS: 900, Arrival: arrival}
}

// untimed is a valid PES without PTS of program 1.
func untimed(pid uint16, offset int64, arrival ClockTime) PES {
	return PES{Program: 1, PID: pid, Offset: offset, Arrival: arrival}
}

func pidResult(t *testing.T, r PESResult, pid uint16) PESPIDResult {
	t.Helper()
	for _, p := range r.PIDs {
		if p.PID == pid {
			return p
		}
	}
	t.Fatalf("PID 0x%04x not in result %+v", pid, r.PIDs)
	return PESPIDResult{}
}

func TestPESHeaderProblemString(t *testing.T) {
	want := []string{"ok", "truncated", "bad_start_code", "bad_flags", "inconsistent_length", "bad_marker_bits"}
	for i, name := range want {
		if got := PESHeaderProblem(i).String(); got != name {
			t.Errorf("PESHeaderProblem(%d) = %q, want %q", i, got, name)
		}
	}
}

func TestPESCheckEmpty(t *testing.T) {
	got := NewPESCheck().Result()
	want := PESResult{
		Headers:    PESHeaderResult{Status: PESInsufficientData, Reason: PESReasonNoPES, Problems: []PESProblemCount{}},
		Repetition: PTSRepetitionResult{Status: PESSkipped, Reason: PESReasonNoVideoOrAudio},
		PIDs:       []PESPIDResult{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Result() = %+v, want %+v", got, want)
	}
}

func TestPESCheckHealthy(t *testing.T) {
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video, audio, data))
	for i := range uint64(10) {
		c.ObservePES(pts(video.PID, int64(i*376), at(i*40)))
		c.ObservePES(PES{Program: 1, PID: video.PID, Offset: int64(i*376 + 188), HasPTS: true, HasDTS: true, Arrival: at(i*40 + 20)})
	}
	c.ObservePES(pts(audio.PID, 100, at(5)))
	c.ObservePES(untimed(audio.PID, 200, at(30)))
	c.ObservePES(pts(audio.PID, 300, at(65)))
	c.ObservePES(pts(data.PID, 400, at(70)))
	got := c.Result()
	want := PESResult{
		Headers:    PESHeaderResult{Status: PESOK, Checked: 24, Problems: []PESProblemCount{}},
		Repetition: PTSRepetitionResult{Status: PESOK, PTS: 23, Intervals: 20, HasMax: true, MaxTicks: 60 * ms, MaxFrom: 100, MaxTo: 300},
		PIDs: []PESPIDResult{
			{Program: 1, PID: 0x100, StreamType: 0x02, Label: "video",
				Headers:    PESHeaderResult{Status: PESOK, Checked: 20, Problems: []PESProblemCount{}},
				Repetition: PTSRepetitionResult{Status: PESOK, PTS: 20, Intervals: 19, HasMax: true, MaxTicks: 20 * ms, MaxFrom: 0, MaxTo: 188}},
			{Program: 1, PID: 0x101, StreamType: 0x03, Label: "audio",
				Headers:    PESHeaderResult{Status: PESOK, Checked: 3, Problems: []PESProblemCount{}},
				Repetition: PTSRepetitionResult{Status: PESOK, PTS: 2, Intervals: 1, HasMax: true, MaxTicks: 60 * ms, MaxFrom: 100, MaxTo: 300}},
			{Program: 1, PID: 0x102, StreamType: 0x06, Label: "stream_type 0x06",
				Headers:    PESHeaderResult{Status: PESOK, Checked: 1, Problems: []PESProblemCount{}},
				Repetition: PTSRepetitionResult{Status: PESSkipped, Reason: PESReasonNotVideoOrAudio, PTS: 1}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Result() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestPESCheckHeaderProblems(t *testing.T) {
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video, data))
	c.ObservePES(PES{Program: 1, PID: data.PID, Offset: 940, Problem: PESHeaderBadMarkerBits})
	c.ObservePES(PES{Program: 1, PID: data.PID, Offset: 564, Problem: PESHeaderBadFlags})
	c.ObservePES(PES{Program: 1, PID: data.PID, Offset: 1128, Problem: PESHeaderBadFlags})
	c.ObservePES(pts(data.PID, 1316, at(0)))
	c.ObservePES(PES{Program: 1, PID: video.PID, Offset: 188, Problem: PESHeaderTruncated})
	c.ObserveBreak(PESBreak{Program: 1, PID: video.PID, Offset: 2000, Cause: BreakIncompleteHeader})
	// Observations of a PID no layout announced are ignored.
	c.ObservePES(PES{Program: 2, PID: data.PID, Offset: 0, Problem: PESHeaderBadStartCode})
	c.ObserveBreak(PESBreak{Program: 2, PID: data.PID, Offset: 0})
	got := c.Result()
	wantData := PESHeaderResult{Status: PESErrors, Checked: 4, Errors: 3, FirstErrorOffset: 564, Problems: []PESProblemCount{
		{Problem: PESHeaderBadFlags, Count: 2, FirstOffset: 564},
		{Problem: PESHeaderBadMarkerBits, Count: 1, FirstOffset: 940},
	}}
	if h := pidResult(t, got, data.PID).Headers; !reflect.DeepEqual(h, wantData) {
		t.Errorf("data headers = %+v, want %+v", h, wantData)
	}
	wantVideo := PESHeaderResult{Status: PESErrors, Checked: 1, Errors: 1, Unchecked: 1, FirstErrorOffset: 188, Problems: []PESProblemCount{
		{Problem: PESHeaderTruncated, Count: 1, FirstOffset: 188},
	}}
	if h := pidResult(t, got, video.PID).Headers; !reflect.DeepEqual(h, wantVideo) {
		t.Errorf("video headers = %+v, want %+v", h, wantVideo)
	}
	wantTotal := PESHeaderResult{Status: PESErrors, Checked: 5, Errors: 4, Unchecked: 1, FirstErrorOffset: 188, Problems: []PESProblemCount{
		{Problem: PESHeaderTruncated, Count: 1, FirstOffset: 188},
		{Problem: PESHeaderBadFlags, Count: 2, FirstOffset: 564},
		{Problem: PESHeaderBadMarkerBits, Count: 1, FirstOffset: 940},
	}}
	if !reflect.DeepEqual(got.Headers, wantTotal) {
		t.Errorf("total headers = %+v, want %+v", got.Headers, wantTotal)
	}
	if r := pidResult(t, got, video.PID).Repetition; r.Status != PESInsufficientData || r.Reason != PESReasonMalformedHeaders {
		t.Errorf("video repetition = %+v, want insufficient data (malformed headers)", r)
	}
}

func TestPESCheckHeaderReasons(t *testing.T) {
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video, audio, data))
	c.ObserveBreak(PESBreak{Program: 1, PID: video.PID, Offset: 188, Cause: BreakUntrustedPayload})
	c.ObserveBreak(PESBreak{Program: 1, PID: audio.PID, Offset: 376, Cause: BreakIncompleteHeader})
	c.ObserveBreak(PESBreak{Program: 1, PID: data.PID, Offset: 564, Cause: BreakContinuityError})
	got := c.Result()
	for pid, want := range map[uint16]PESReason{video.PID: PESReasonUntrustedPayload, audio.PID: PESReasonIncompleteHeaders, data.PID: PESReasonNoPES} {
		if h := pidResult(t, got, pid).Headers; h.Status != PESInsufficientData || h.Reason != want {
			t.Errorf("PID 0x%04x headers = %+v, want insufficient data (%s)", pid, h, want)
		}
	}
	// The totals take the reason of the first PID.
	if got.Headers.Status != PESInsufficientData || got.Headers.Reason != PESReasonUntrustedPayload || got.Headers.Unchecked != 1 {
		t.Errorf("total headers = %+v", got.Headers)
	}
	if r := pidResult(t, got, video.PID).Repetition; r.Reason != PESReasonUntrustedPayload {
		t.Errorf("video repetition = %+v, want untrusted payload", r)
	}
	if r := pidResult(t, got, audio.PID).Repetition; r.Reason != PESReasonNoPES {
		t.Errorf("audio repetition = %+v, want no PES", r)
	}
	if got.Repetition.Status != PESInsufficientData || got.Repetition.Reason != PESReasonUntrustedPayload {
		t.Errorf("total repetition = %+v", got.Repetition)
	}
}

func TestPTSRepetitionExceeded(t *testing.T) {
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video, audio))
	c.ObservePES(pts(video.PID, 0, at(0)))
	c.ObservePES(untimed(video.PID, 188, at(40)))
	c.ObservePES(pts(video.PID, 376, at(1080))) // 1080 ms after the last PTS
	c.ObservePES(pts(video.PID, 564, at(1120))) // 40 ms
	c.ObservePES(pts(video.PID, 752, at(1900))) // 780 ms
	c.ObservePES(pts(audio.PID, 100, at(0)))    //
	c.ObservePES(pts(audio.PID, 200, at(701)))  // 701 ms
	c.ObservePES(pts(audio.PID, 300, at(1401))) // 700 ms, not above the limit
	got := c.Result()
	wantVideo := PTSRepetitionResult{Status: PESNG, PTS: 4, Intervals: 3, HasMax: true, MaxTicks: 1080 * ms, MaxFrom: 0, MaxTo: 376, Exceeded: 2, FirstExceeded: 376}
	if r := pidResult(t, got, video.PID).Repetition; r != wantVideo {
		t.Errorf("video = %+v, want %+v", r, wantVideo)
	}
	wantAudio := PTSRepetitionResult{Status: PESNG, PTS: 3, Intervals: 2, HasMax: true, MaxTicks: 701 * ms, MaxFrom: 100, MaxTo: 200, Exceeded: 1, FirstExceeded: 200}
	if r := pidResult(t, got, audio.PID).Repetition; r != wantAudio {
		t.Errorf("audio = %+v, want %+v", r, wantAudio)
	}
	wantTotal := PTSRepetitionResult{Status: PESNG, PTS: 7, Intervals: 5, HasMax: true, MaxTicks: 1080 * ms, MaxFrom: 0, MaxTo: 376, Exceeded: 3, FirstExceeded: 200}
	if got.Repetition != wantTotal {
		t.Errorf("total = %+v, want %+v", got.Repetition, wantTotal)
	}
}

func TestPTSRepetitionLowerBounds(t *testing.T) {
	// PES packets without a PTS before the first PTS, before a break and at
	// the end of the range give lower bounds: counted only above the limit.
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video, audio))
	c.ObservePES(untimed(video.PID, 0, at(0)))
	c.ObservePES(untimed(video.PID, 188, at(400)))
	c.ObservePES(pts(video.PID, 376, at(800))) // at least 800 ms without PTS
	c.ObservePES(untimed(video.PID, 564, at(1000)))
	c.ObservePES(untimed(video.PID, 752, at(1600))) // 800 ms, then a break
	c.ObserveBreak(PESBreak{Program: 1, PID: video.PID, Offset: 940, Cause: BreakContinuityError})
	c.ObservePES(untimed(video.PID, 1128, at(2000)))
	c.ObservePES(pts(video.PID, 1316, at(2500))) // 500 ms lower bound: ignored
	c.ObservePES(pts(video.PID, 1504, at(2540)))
	c.ObservePES(untimed(video.PID, 1692, at(3500))) // 960 ms at the end
	c.ObservePES(untimed(audio.PID, 0, at(0)))
	c.ObservePES(untimed(audio.PID, 100, at(600))) // 600 ms: not enough
	got := c.Result()
	wantVideo := PTSRepetitionResult{Status: PESNG, PTS: 3, Intervals: 1, HasMax: true, MaxTicks: 960 * ms, MaxFrom: 1504, MaxTo: 1692, Exceeded: 3, FirstExceeded: 376}
	if r := pidResult(t, got, video.PID).Repetition; r != wantVideo {
		t.Errorf("video = %+v, want %+v", r, wantVideo)
	}
	wantAudio := PTSRepetitionResult{Status: PESInsufficientData, Reason: PESReasonNoPTS}
	if r := pidResult(t, got, audio.PID).Repetition; r != wantAudio {
		t.Errorf("audio = %+v, want %+v", r, wantAudio)
	}
}

func TestPTSRepetitionPendingAfterExceeded(t *testing.T) {
	// A pending lower bound smaller than the measured maximum keeps it.
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video))
	c.ObservePES(pts(video.PID, 0, at(0)))
	c.ObservePES(pts(video.PID, 188, at(1000)))
	c.ObservePES(untimed(video.PID, 376, at(1800)))
	want := PTSRepetitionResult{Status: PESNG, PTS: 2, Intervals: 1, HasMax: true, MaxTicks: 1000 * ms, MaxFrom: 0, MaxTo: 188, Exceeded: 2, FirstExceeded: 188}
	if r := c.Result().Repetition; r != want {
		t.Errorf("Result = %+v, want %+v", r, want)
	}
}

func TestPTSRepetitionTimeline(t *testing.T) {
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video, audio))
	// A new segment ends the interval: no interval spans a discontinuity.
	c.ObservePES(pts(video.PID, 0, at(0)))
	c.ObservePES(pts(video.PID, 188, ClockTime{Valid: true, Ticks: 0, Segment: 1}))
	// A PCR wraparound inside a segment is measured.
	c.ObservePES(pts(video.PID, 376, ClockTime{Valid: true, Ticks: pcrWrap - 20*ms, Segment: 2}))
	c.ObservePES(pts(video.PID, 564, ClockTime{Valid: true, Ticks: 20 * ms, Segment: 2}))
	// A PTS that cannot be placed ends the interval; an unplaced PES without
	// PTS is ignored.
	c.ObservePES(untimed(video.PID, 752, ClockTime{}))
	c.ObservePES(pts(video.PID, 940, ClockTime{}))
	c.ObservePES(pts(video.PID, 1128, ClockTime{Valid: true, Ticks: 5000 * ms, Segment: 2}))
	// The audio PID never gets a place on the timeline.
	c.ObservePES(pts(audio.PID, 0, ClockTime{}))
	got := c.Result()
	wantVideo := PTSRepetitionResult{Status: PESOK, PTS: 6, Intervals: 1, HasMax: true, MaxTicks: 40 * ms, MaxFrom: 376, MaxTo: 564}
	if r := pidResult(t, got, video.PID).Repetition; r != wantVideo {
		t.Errorf("video = %+v, want %+v", r, wantVideo)
	}
	if r := pidResult(t, got, audio.PID).Repetition; r.Status != PESInsufficientData || r.Reason != PESReasonNoPCRTiming {
		t.Errorf("audio = %+v, want insufficient data (no PCR timing)", r)
	}
}

func TestPTSRepetitionSinglePTS(t *testing.T) {
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video, data))
	c.ObservePES(pts(video.PID, 0, at(0)))
	got := c.Result()
	if r := pidResult(t, got, video.PID).Repetition; r.Status != PESInsufficientData || r.Reason != PESReasonSinglePTS {
		t.Errorf("video = %+v, want insufficient data (single PTS)", r)
	}
	if got.Repetition.Status != PESInsufficientData || got.Repetition.Reason != PESReasonSinglePTS {
		t.Errorf("total = %+v", got.Repetition)
	}
}

func TestPESCheckLayoutChanges(t *testing.T) {
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(video, audio))
	c.ObserveProgramLayout(2, pesLayout(ElementaryStream{PID: 0x100, StreamType: 0x1B}))
	c.ObservePES(pts(video.PID, 0, at(0)))
	c.ObservePES(untimed(video.PID, 188, at(800)))
	// The PMT retypes the video PID: the interval in progress ends (its 800 ms
	// lower bound counts) and the PID keeps its counts with the new type.
	c.ObserveProgramLayout(1, pesLayout(ElementaryStream{PID: 0x100, StreamType: 0x06}, audio))
	c.ObservePES(pts(video.PID, 376, at(900)))
	// The audio PID is removed and added back: no interval spans its absence.
	c.ObservePES(pts(audio.PID, 0, at(0)))
	c.ObserveProgramLayout(1, pesLayout(ElementaryStream{PID: 0x100, StreamType: 0x06}))
	c.ObserveProgramLayout(1, pesLayout(ElementaryStream{PID: 0x100, StreamType: 0x06}, audio))
	c.ObservePES(pts(audio.PID, 100, at(2000)))
	// The same layout again changes nothing.
	c.ObserveProgramLayout(1, pesLayout(ElementaryStream{PID: 0x100, StreamType: 0x06}, audio))
	c.ObservePES(pts(audio.PID, 200, at(2040)))
	got := c.Result()
	if len(got.PIDs) != 3 || got.PIDs[2].Program != 2 {
		t.Fatalf("PIDs = %+v, want two of program 1 then one of program 2", got.PIDs)
	}
	v := pidResult(t, got, video.PID)
	wantVideo := PTSRepetitionResult{Status: PESNG, PTS: 2, HasMax: true, MaxTicks: 800 * ms, MaxFrom: 0, MaxTo: 188, Exceeded: 1, FirstExceeded: 188}
	if v.StreamType != 0x06 || v.Label != "stream_type 0x06" || v.Repetition != wantVideo {
		t.Errorf("video = %+v, want stream_type 0x06 with %+v", v, wantVideo)
	}
	wantAudio := PTSRepetitionResult{Status: PESOK, PTS: 3, Intervals: 1, HasMax: true, MaxTicks: 40 * ms, MaxFrom: 100, MaxTo: 200}
	if r := pidResult(t, got, audio.PID).Repetition; r != wantAudio {
		t.Errorf("audio = %+v, want %+v", r, wantAudio)
	}
	if got.PIDs[2].Repetition.Status != PESInsufficientData || got.PIDs[2].Repetition.Reason != PESReasonNoPES {
		t.Errorf("program 2 = %+v", got.PIDs[2])
	}
}

func TestPTSRepetitionNoPTSAboveLimit(t *testing.T) {
	// PES packets without any PTS for longer than the limit fail the check
	// even though no interval between two PTS was measured.
	c := NewPESCheck()
	c.ObserveProgramLayout(1, pesLayout(audio))
	c.ObservePES(untimed(audio.PID, 0, at(0)))
	c.ObservePES(untimed(audio.PID, 188, at(750)))
	want := PTSRepetitionResult{Status: PESNG, HasMax: true, MaxTicks: 750 * ms, MaxFrom: 0, MaxTo: 188, Exceeded: 1, FirstExceeded: 188}
	if r := c.Result().Repetition; r != want {
		t.Errorf("Result = %+v, want %+v", r, want)
	}
}
