package analysis

import (
	"cmp"
	"slices"
)

// Mux lead and audio/video lead difference (report-only).
//
// The mux lead of a PES packet is how far ahead of its decode time it was
// multiplexed: its DTS (its PTS when it has no DTS) minus its time on the
// program's PCR timeline, the PCR value at the first byte of the packet that
// starts it, interpolated by byte position between the PCRs before and after
// it (PES.Arrival, see ClockTime). It is the computation of the PCR-PTS/DTS gap
// check (tsparser Pes.BracketedTimestampDelay), applied to the same PES
// packets with the same PCR bracket and interpolation; the gap check keeps
// only its largest value per program and judges it against 1000 ms, while this
// report aggregates the lead per elementary PID and pairs audio with video.
// It is a report only: it has no limit and no verdict, a negative lead (a PES
// packet multiplexed after its decode time) is counted as an observation, and
// nothing here becomes a finding or changes the exit status.
//
//   - Samples: every PES packet with a valid header carrying a PTS, on any
//     elementary PID of an analyzed program. A PES packet whose header failed a
//     check is not a sample (see PESCheck), and neither is one the parser left
//     out because its payload was untrusted (transport error or scrambling).
//   - Unbracketed: a sample PES packet that cannot be placed on the PCR
//     timeline, because it does not lie between two PCRs of one segment (before
//     the first or after the last PCR, across a PCR discontinuity, or a program
//     without PCR). It is counted, and not measured.
//   - Wraparound: the PTS/DTS (33 bits at 90 kHz) and the PCR (modulo
//     2^33*300 at 27 MHz) wrap independently, so the lead is taken modulo the
//     PCR period and read as the signed value nearest to zero: a lead is
//     correct across either wraparound as long as its magnitude is below half
//     the period (about 13 hours).
//   - A/V difference: for each program with a video and an audio stream
//     (classified by stream_type, see AVClass), the most recent lead of each
//     audio PID is compared with the most recent lead of the program's video
//     PID (its lowest-numbered video PID in the layout in force) whenever
//     either of them gets a new sample, in the order the parser reports the
//     PES packets. Two leads are compared only when they lie in the same PCR
//     segment. A break of the PID (untrusted payload, a continuity error, a
//     header that could not be checked), a malformed header, an unbracketed
//     sample and a layout change that removes or retypes the PID forget its
//     most recent lead. Each audio PID keeps its largest absolute difference;
//     the program reports its worst audio PID.
//
// # State
//
// A fixed-size aggregate in the state of each elementary PID of PESCheck, and
// per program the lowest video PID and the audio PIDs of its layout in force,
// so the state is bounded by the PIDs announced by the PMTs regardless of the
// input length.

// AVClass classifies a stream_type for the audio/video lead difference.
type AVClass int

const (
	// AVOther is any stream_type that is not AVVideo or AVAudio.
	AVOther AVClass = iota
	// AVVideo is MPEG-1 video (0x01), MPEG-2 video (0x02), H.264 (0x1B) and
	// HEVC (0x24).
	AVVideo
	// AVAudio is MPEG-1 audio (0x03), MPEG-2 audio (0x04), AAC in ADTS (0x0F),
	// AAC in LATM (0x11), and the ATSC AC-3 (0x81) and E-AC-3 (0x87) types.
	AVAudio
)

// String returns the name of the class used in the JSON output.
func (a AVClass) String() string { return [...]string{"other", "video", "audio"}[a] }

// ClassifyAV returns the class of a stream_type.
func ClassifyAV(streamType uint8) AVClass {
	switch streamType {
	case 0x01, 0x02, 0x1B, 0x24:
		return AVVideo
	case 0x03, 0x04, 0x0F, 0x11, 0x81, 0x87:
		return AVAudio
	}
	return AVOther
}

const (
	// PESInfo is the status of a report without a verdict that measured
	// something (mux lead).
	PESInfo PESStatus = "info"
	// PESReasonNoComparableLeads: the audio PID and the program's video never
	// had a lead in the same PCR segment at the same time.
	PESReasonNoComparableLeads PESReason = "no_comparable_leads"
)

// leadSample is one measured lead.
type leadSample struct {
	ticks   int64 // the lead in 27 MHz ticks
	offset  int64 // offset of the PES packet
	segment int   // PCR segment of its time
}

// muxLead is the mux lead aggregate of one elementary PID.
type muxLead struct {
	samples, unbracketed, dts, negative int
	min, max                            leadSample
	// The most recent lead, for the A/V difference.
	last    leadSample
	hasLast bool
	// Audio PIDs only: the comparisons with the program's video and the one
	// with the largest absolute difference.
	comparisons int
	worst       avDifference
}

type avDifference struct {
	ticks        int64 // absolute difference
	audio, video leadSample
	videoPID     uint16
}

// avLayout is the part of a program's layout in force that the A/V difference
// needs.
type avLayout struct {
	hasVideo bool
	video    uint16
	audio    []uint16
}

// leadTicks returns the lead of a 90 kHz timestamp over a 27 MHz PCR time,
// both modulo their periods, as the signed value nearest to zero.
func leadTicks(stamp, arrival uint64) int64 {
	d := (stamp*300%PCRWrapTicks + PCRWrapTicks - arrival%PCRWrapTicks) % PCRWrapTicks
	if d >= PCRWrapTicks/2 {
		return int64(d) - int64(PCRWrapTicks)
	}
	return int64(d)
}

// observeAVLayout records the video and audio PIDs of a program's layout in
// force.
func (c *PESCheck) observeAVLayout(program uint16, layout ProgramLayout) {
	av := avLayout{}
	for _, s := range layout.Streams {
		switch ClassifyAV(s.StreamType) {
		case AVVideo:
			if !av.hasVideo || s.PID < av.video {
				av.hasVideo, av.video = true, s.PID
			}
		case AVAudio:
			av.audio = append(av.audio, s.PID)
		}
	}
	c.av[program] = av
}

// observeLead records the lead of a PES packet with a valid header.
func (c *PESCheck) observeLead(key pesKey, st *pesStream, o PES) {
	if !o.HasPTS {
		return
	}
	l := &st.lead
	if !o.Arrival.Valid {
		l.unbracketed++
		l.hasLast = false
		return
	}
	stamp := o.PTS
	if o.HasDTS {
		stamp = o.DTS
		l.dts++
	}
	s := leadSample{ticks: leadTicks(stamp, o.Arrival.Ticks), offset: o.Offset, segment: o.Arrival.Segment}
	if l.samples == 0 || s.ticks < l.min.ticks {
		l.min = s
	}
	if l.samples == 0 || s.ticks > l.max.ticks {
		l.max = s
	}
	if s.ticks < 0 {
		l.negative++
	}
	l.samples++
	l.last, l.hasLast = s, true

	av := c.av[key.program]
	if !av.hasVideo {
		return
	}
	switch ClassifyAV(st.streamType) {
	case AVAudio:
		compareLeads(st, c.streams[pesKey{key.program, av.video}], av.video)
	case AVVideo:
		if key.pid == av.video {
			for _, pid := range av.audio {
				compareLeads(c.streams[pesKey{key.program, pid}], st, av.video)
			}
		}
	}
}

// compareLeads compares the most recent leads of an audio and a video PID.
func compareLeads(audio, video *pesStream, videoPID uint16) {
	a, v := &audio.lead, &video.lead
	if !a.hasLast || !v.hasLast || a.last.segment != v.last.segment {
		return
	}
	d := a.last.ticks - v.last.ticks
	if d < 0 {
		d = -d
	}
	if a.comparisons == 0 || d > a.worst.ticks {
		a.worst = avDifference{ticks: d, audio: a.last, video: v.last, videoPID: videoPID}
	}
	a.comparisons++
}

// MuxLeadPID is the mux lead of one elementary PID of one program.
//
// Status is PESInfo when at least one lead was measured, otherwise
// PESInsufficientData with Reason PESReasonUntrustedPayload,
// PESReasonMalformedHeaders, PESReasonNoPES, PESReasonNoPTS or
// PESReasonNoPCRTiming.
type MuxLeadPID struct {
	Program    uint16
	PID        uint16
	StreamType uint8 // the last applied stream_type
	Label      string
	Class      AVClass
	Status     PESStatus
	Reason     PESReason
	// Samples is the number of leads measured; Unbracketed the number of PES
	// packets with a PTS that could not be placed on the PCR timeline.
	Samples, Unbracketed int
	// DTSSamples is the number of samples measured from a DTS; the others
	// used the PTS.
	DTSSamples int
	// Negative is the number of samples multiplexed after their DTS/PTS.
	Negative int
	// MinTicks and MaxTicks (27 MHz) are the smallest and largest lead, of
	// the PES packets at MinOffset and MaxOffset (the first of equal leads).
	// Meaningful when Samples is not zero.
	MinTicks, MaxTicks   int64
	MinOffset, MaxOffset int64
}

// AVLeadDifference is the largest lead difference between one audio PID and
// the program's video.
//
// Status is PESInfo when the leads were compared at least once, otherwise
// PESInsufficientData with PESReasonNoComparableLeads.
type AVLeadDifference struct {
	AudioPID    uint16
	Status      PESStatus
	Reason      PESReason
	Comparisons int
	// The fields below are meaningful when Comparisons is not zero:
	// MaxTicks is the largest absolute difference (27 MHz), between the
	// audio lead AudioLeadTicks of the PES packet at AudioOffset and the
	// video lead VideoLeadTicks of the PES packet at VideoOffset on VideoPID.
	VideoPID                       uint16
	MaxTicks                       int64
	AudioLeadTicks, VideoLeadTicks int64
	AudioOffset, VideoOffset       int64
}

// AVLeadProgram is the A/V lead difference of a program with a video and an
// audio stream. Status is PESInfo when any audio PID was compared, otherwise
// PESInsufficientData with PESReasonNoComparableLeads; Worst is then the
// audio PID with the largest difference (the lowest PID of equal ones).
type AVLeadProgram struct {
	Program  uint16
	Status   PESStatus
	Reason   PESReason
	HasWorst bool
	Worst    AVLeadDifference
	// Audio lists every audio PID of the program in PID order.
	Audio []AVLeadDifference
}

// MuxLeadResult is the mux lead of every elementary PID of the analyzed
// programs in program and PID order, and the A/V lead difference of every
// program that has had both a video and an audio stream, in program order.
type MuxLeadResult struct {
	PIDs     []MuxLeadPID
	Programs []AVLeadProgram
}

// MuxLead returns the mux lead of the PES packets observed so far.
func (c *PESCheck) MuxLead() MuxLeadResult {
	r := MuxLeadResult{PIDs: []MuxLeadPID{}, Programs: []AVLeadProgram{}}
	programs := map[uint16]*AVLeadProgram{}
	video := map[uint16]bool{}
	for key, st := range c.streams {
		r.PIDs = append(r.PIDs, st.muxLeadResult(key))
		switch ClassifyAV(st.streamType) {
		case AVVideo:
			video[key.program] = true
		case AVAudio:
			p := programs[key.program]
			if p == nil {
				p = &AVLeadProgram{Program: key.program}
				programs[key.program] = p
			}
			p.Audio = append(p.Audio, st.avResult(key.pid))
		}
	}
	slices.SortFunc(r.PIDs, func(a, b MuxLeadPID) int {
		return cmp.Or(cmp.Compare(a.Program, b.Program), cmp.Compare(a.PID, b.PID))
	})
	for number, p := range programs {
		if !video[number] {
			continue
		}
		slices.SortFunc(p.Audio, func(a, b AVLeadDifference) int { return cmp.Compare(a.AudioPID, b.AudioPID) })
		p.Status, p.Reason = PESInsufficientData, PESReasonNoComparableLeads
		for _, a := range p.Audio {
			if a.Comparisons > 0 && (!p.HasWorst || a.MaxTicks > p.Worst.MaxTicks) {
				p.HasWorst, p.Worst = true, a
				p.Status, p.Reason = PESInfo, PESReasonNone
			}
		}
		r.Programs = append(r.Programs, *p)
	}
	slices.SortFunc(r.Programs, func(a, b AVLeadProgram) int { return cmp.Compare(a.Program, b.Program) })
	return r
}

func (st *pesStream) muxLeadResult(key pesKey) MuxLeadPID {
	l := st.lead
	r := MuxLeadPID{
		Program: key.program, PID: key.pid, StreamType: st.streamType, Label: streamTypeLabel(st.streamType), Class: ClassifyAV(st.streamType),
		Status: PESInfo, Samples: l.samples, Unbracketed: l.unbracketed, DTSSamples: l.dts, Negative: l.negative,
		MinTicks: l.min.ticks, MaxTicks: l.max.ticks, MinOffset: l.min.offset, MaxOffset: l.max.offset,
	}
	if l.samples > 0 {
		return r
	}
	errors := 0
	for _, p := range st.problems {
		errors += p.count
	}
	r.Status = PESInsufficientData
	switch {
	case st.pes == 0 && st.untrusted > 0:
		r.Reason = PESReasonUntrustedPayload
	case st.pes == 0 && errors > 0:
		r.Reason = PESReasonMalformedHeaders
	case st.pes == 0:
		r.Reason = PESReasonNoPES
	case st.pts == 0:
		r.Reason = PESReasonNoPTS
	default:
		r.Reason = PESReasonNoPCRTiming
	}
	return r
}

func (st *pesStream) avResult(pid uint16) AVLeadDifference {
	l := st.lead
	if l.comparisons == 0 {
		return AVLeadDifference{AudioPID: pid, Status: PESInsufficientData, Reason: PESReasonNoComparableLeads}
	}
	w := l.worst
	return AVLeadDifference{
		AudioPID: pid, Status: PESInfo, Comparisons: l.comparisons, VideoPID: w.videoPID, MaxTicks: w.ticks,
		AudioLeadTicks: w.audio.ticks, VideoLeadTicks: w.video.ticks, AudioOffset: w.audio.offset, VideoOffset: w.video.offset,
	}
}
