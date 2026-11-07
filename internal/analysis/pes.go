package analysis

import "slices"

// PES header health and PTS repetition.
//
// The parser decodes each PES header from the bounded bytes it retains (at
// most the 264-byte maximum header, see tsparser.maxPesHeaderSize) and reports
// one PES observation per PES packet, with the header problem it found, if
// any. This check counts header problems per elementary PID and measures how
// often a PTS occurs on each video and audio PID.
//
// # PTS repetition
//
// ISO/IEC 13818-1, clause 2.7.4 requires the transport stream to be
// constructed so that coded presentation time stamps of each elementary video
// or audio stream are at most 0.7 s apart; ETSI TR 101 290 V1.4.1, indicator
// 2.5 PTS_error, monitors it as "PTS repetition period more than 700 ms" and
// notes that the limit is not applied to still pictures. Both are
// profile-neutral MPEG-2 requirements, so the limit applies to every PID whose
// stream_type is a video or audio type (the types labeled "video" or "audio",
// see streamTypeLabel); other PIDs are skipped.
//
// The interval is measured, as TR 101 290 monitors it, between the times at
// which PES packets carrying a PTS occur in the multiplex: the time of a PES
// is the program's PCR time at the first byte of the packet that starts it,
// interpolated by byte position between the PCRs before and after it (see
// ClockTime). The PTS values themselves are not compared, so a splice that
// moves the presentation timeline does not count as a repetition problem
// (the timestamp anomaly check reports it), and neither does B-frame
// reordering.
//
//   - Time base: 27 MHz PCR ticks. Two times are compared only within one PCR
//     segment; their difference is taken modulo 2^33*300, so a PCR wraparound
//     inside a segment is measured.
//   - Discontinuity: a PCR discontinuity (declared, an undeclared backward
//     reset, or a PCR_PID change) starts a new segment and no interval spans
//     it.
//   - Acquisition: nothing is known before the first PES of a PID in the
//     range. PES packets without a PTS before the first PTS only provide a
//     lower bound (see below).
//   - Breaks: a PES whose header is malformed or incomplete, a PES that cannot
//     be placed on the PCR timeline but has a PTS, untrusted payload
//     (transport error or scrambling), a continuity error, a stream_type
//     change and the removal of the PID from the program end the interval in
//     progress, since a PTS may have been lost there.
//   - Lower bounds: when PES packets without a PTS are observed for longer
//     than the limit (before the first PTS, before a break or at the end of
//     the range), the stream went at least that long without a PTS. Such an
//     interval counts as exceeded when it is above the limit; it is not a
//     measured interval otherwise.
//
// # State
//
// One fixed-size state per elementary PID of each program, so the state is
// bounded by the PIDs announced by the PMTs regardless of the input length.

// PTSRepetitionLimitTicks is the largest allowed interval between PES packets
// carrying a PTS, 700 ms in 27 MHz ticks (ISO/IEC 13818-1 clause 2.7.4, ETSI
// TR 101 290 indicator 2.5).
const PTSRepetitionLimitTicks = 700 * ticksPerMs

const (
	ticksPerMs = 27000
	// pcrWrap is the modulus of the 27 MHz PCR value: a 33-bit base times 300.
	pcrWrap = uint64(1) << 33 * 300
)

// PESHeaderProblem classifies a PES header that cannot be trusted. The parser
// decides it from the retained header bytes; see the tsparser PES parser for
// the exact rules.
type PESHeaderProblem int

const (
	// PESHeaderOK is a header that was decoded completely and passed every
	// check.
	PESHeaderOK PESHeaderProblem = iota
	// PESHeaderTruncated is a header that the next PES packet of the PID
	// started before it was complete.
	PESHeaderTruncated
	// PESHeaderBadStartCode is a PES packet that does not start with
	// packet_start_code_prefix 0x000001 followed by a PES stream_id.
	PESHeaderBadStartCode
	// PESHeaderBadFlags is an optional header without its leading '10' bits
	// or with the forbidden PTS_DTS_flags value '01'.
	PESHeaderBadFlags
	// PESHeaderInconsistentLength is a PES_header_data_length too small for
	// the fields its flags announce, or a non-zero PES_packet_length too
	// small for the header.
	PESHeaderInconsistentLength
	// PESHeaderBadMarkerBits is a PTS or DTS field with a wrong 4-bit prefix
	// or a marker_bit that is not 1.
	PESHeaderBadMarkerBits
)

// pesHeaderProblems is the number of PESHeaderProblem values, including OK.
const pesHeaderProblems = int(PESHeaderBadMarkerBits) + 1

// String returns the name of the problem used in the JSON output.
func (p PESHeaderProblem) String() string {
	return [...]string{"ok", "truncated", "bad_start_code", "bad_flags", "inconsistent_length", "bad_marker_bits"}[p]
}

// PESStatus is the status of a PES header or PTS repetition result, spelled
// as in the JSON output.
type PESStatus string

const (
	PESOK               PESStatus = "ok"
	PESErrors           PESStatus = "errors"
	PESNG               PESStatus = "ng"
	PESInsufficientData PESStatus = "insufficient_data"
	PESSkipped          PESStatus = "skipped"
)

// PESReason explains a PESInsufficientData or PESSkipped status, spelled as in
// the JSON output.
type PESReason string

const (
	PESReasonNone PESReason = ""
	// PESReasonNoPES: no PES packet started on the PID in the range.
	PESReasonNoPES PESReason = "no_pes"
	// PESReasonUntrustedPayload: no header could be checked and payload of
	// the PID was not used because of transport errors or scrambling.
	PESReasonUntrustedPayload PESReason = "untrusted_payload"
	// PESReasonIncompleteHeaders: every PES packet ended before its header
	// was complete.
	PESReasonIncompleteHeaders PESReason = "incomplete_headers"
	// PESReasonMalformedHeaders: every header checked was malformed.
	PESReasonMalformedHeaders PESReason = "malformed_headers"
	// PESReasonNoPCRTiming: no PES packet could be placed on the PCR
	// timeline (the program has no PCR, or no PES packet lies between two
	// PCRs of one segment).
	PESReasonNoPCRTiming PESReason = "no_pcr_timing"
	// PESReasonNoPTS: PES packets were placed on the PCR timeline, none
	// carried a PTS, and they did not span more than the limit.
	PESReasonNoPTS PESReason = "no_pts"
	// PESReasonSinglePTS: no two comparable PES packets with a PTS were
	// observed.
	PESReasonSinglePTS PESReason = "single_pts"
	// PESReasonNotVideoOrAudio: the PID's stream_type is not a video or
	// audio type, so the limit does not apply.
	PESReasonNotVideoOrAudio PESReason = "not_video_or_audio"
	// PESReasonNoVideoOrAudio: no analyzed program has a video or audio
	// stream (totals only).
	PESReasonNoVideoOrAudio PESReason = "no_video_or_audio"
)

// ClockTime is a time on a program's PCR timeline.
type ClockTime struct {
	// Valid is false when the time is unknown: the PES packet does not lie
	// between two PCRs of one segment, or the program has no PCR.
	Valid bool
	// Ticks is the 27 MHz PCR value, from 0 to 2^33*300-1.
	Ticks uint64
	// Segment numbers the PCR segments of the program. Times of different
	// segments are not comparable.
	Segment int
}

// PES is one PES packet of an elementary stream of an analyzed program,
// reported when the packet ends (the next payload_unit_start_indicator of the
// PID, untrusted payload, a layout change or the end of the range) and its
// header was complete. A packet whose header the data ended before is
// reported as a PESBreak with BreakIncompleteHeader instead.
type PES struct {
	Program uint16 // program_number
	PID     uint16
	// Offset is the byte offset of the packet carrying the PES packet's
	// payload_unit_start_indicator.
	Offset int64
	// Problem is PESHeaderOK unless the header failed a check; the fields
	// below are then not set.
	Problem PESHeaderProblem
	HasPTS  bool
	HasDTS  bool
	// PTS and DTS are 33-bit values of the 90 kHz system clock.
	PTS, DTS uint64
	// Arrival is the time of the packet at Offset on the program's PCR
	// timeline.
	Arrival ClockTime
}

// BreakCause says why the parser could not follow the PES packets of a PID.
type BreakCause int

const (
	// BreakUntrustedPayload is a packet whose payload was not used because of
	// a transport error or scrambling.
	BreakUntrustedPayload BreakCause = iota
	// BreakContinuityError is a continuity_counter error on the PID.
	BreakContinuityError
	// BreakIncompleteHeader is a PES packet whose header was not complete
	// when the data ended: untrusted payload followed, the PMT changed or the
	// range ended. The header could not be checked.
	BreakIncompleteHeader
)

// PESBreak is a point at which PES packets of a PID may have been lost or not
// read.
type PESBreak struct {
	Program uint16
	PID     uint16
	Offset  int64
	Cause   BreakCause
}

// PESCheck aggregates the PES header problems and the PTS repetition of each
// elementary PID of the analyzed programs.
type PESCheck struct {
	streams map[pesKey]*pesStream
}

type pesKey struct{ program, pid uint16 }

type pesStream struct {
	streamType uint8
	active     bool // the PID is in the program's current layout
	applicable bool // its stream_type has been a video or audio type

	pes       int
	unchecked int
	untrusted int
	problems  [pesHeaderProblems]problemCount

	pts       int
	placed    int // PES packets placed on the PCR timeline
	intervals int
	hasMax    bool
	max       interval
	exceeded  int
	firstOver int64

	// The interval in progress, from the last PTS (or from the first PES
	// without a PTS) to the last PES observed since.
	open            bool
	openHasPTS      bool
	openSegment     int
	openFrom        int64
	openStart, last uint64
	lastAt          int64
}

type problemCount struct {
	count int
	first int64
}

type interval struct {
	measured bool
	ticks    uint64
	from, to int64
}

// NewPESCheck returns a check with no streams.
func NewPESCheck() *PESCheck { return &PESCheck{streams: make(map[pesKey]*pesStream)} }

// ObserveProgramLayout applies the layout of a program: each elementary stream
// is added, a stream whose stream_type changed ends its interval and takes the
// new type, and a stream no longer in the layout ends its interval. A PID
// keeps its counts across changes and is reported with its last stream_type.
func (c *PESCheck) ObserveProgramLayout(program uint16, layout ProgramLayout) {
	current := make(map[uint16]bool, len(layout.Streams))
	for _, s := range layout.Streams {
		current[s.PID] = true
		st, ok := c.streams[pesKey{program, s.PID}]
		if !ok {
			st = &pesStream{streamType: s.StreamType}
			c.streams[pesKey{program, s.PID}] = st
		} else if !st.active || st.streamType != s.StreamType {
			st.endInterval()
			st.streamType = s.StreamType
		}
		st.active = true
		st.applicable = st.applicable || isVideoOrAudio(s.StreamType)
	}
	for key, st := range c.streams {
		if key.program == program && st.active && !current[key.pid] {
			st.endInterval()
			st.active = false
		}
	}
}

// ObservePES records one PES packet.
func (c *PESCheck) ObservePES(o PES) {
	st := c.streams[pesKey{o.Program, o.PID}]
	if st == nil {
		return
	}
	if o.Problem != PESHeaderOK {
		p := &st.problems[o.Problem]
		if p.count == 0 {
			p.first = o.Offset
		}
		p.count++
		st.endInterval()
		return
	}
	st.pes++
	if o.HasPTS {
		st.pts++
	}
	if !isVideoOrAudio(st.streamType) {
		return
	}
	if !o.Arrival.Valid {
		if o.HasPTS {
			st.endInterval()
		}
		return
	}
	st.placed++
	at := o.Arrival.Ticks
	if st.open && st.openSegment != o.Arrival.Segment {
		st.endInterval()
	}
	switch {
	case !st.open:
		st.open, st.openHasPTS, st.openSegment = true, o.HasPTS, o.Arrival.Segment
		st.openStart, st.openFrom = at, o.Offset
	case o.HasPTS:
		st.record(interval{measured: st.openHasPTS, ticks: (at + pcrWrap - st.openStart) % pcrWrap, from: st.openFrom, to: o.Offset})
		st.openHasPTS, st.openStart, st.openFrom = true, at, o.Offset
	}
	st.last, st.lastAt = at, o.Offset
}

// ObserveBreak records that PES packets of a PID may have been lost or not
// read at a point.
func (c *PESCheck) ObserveBreak(o PESBreak) {
	st := c.streams[pesKey{o.Program, o.PID}]
	if st == nil {
		return
	}
	switch o.Cause {
	case BreakIncompleteHeader:
		st.unchecked++
	case BreakUntrustedPayload:
		st.untrusted++
	}
	st.endInterval()
}

// record counts a closed interval: a measured one always, a lower bound only
// when it exceeds the limit. Intervals of a PID end in stream order, so the
// first one above the limit is the earliest.
func (st *pesStream) record(iv interval) {
	over := iv.ticks > PTSRepetitionLimitTicks
	if !iv.measured && !over {
		return
	}
	if iv.measured {
		st.intervals++
	}
	if over {
		if st.exceeded == 0 {
			st.firstOver = iv.to
		}
		st.exceeded++
	}
	if !st.hasMax || iv.ticks > st.max.ticks {
		st.max, st.hasMax = iv, true
	}
}

// pending returns the lower bound of the interval in progress: the time from
// its start to the last PES observed since, when that PES has no PTS.
func (st *pesStream) pending() (interval, bool) {
	if !st.open || st.lastAt == st.openFrom {
		return interval{}, false
	}
	return interval{ticks: (st.last + pcrWrap - st.openStart) % pcrWrap, from: st.openFrom, to: st.lastAt}, true
}

// endInterval closes the interval in progress at a break.
func (st *pesStream) endInterval() {
	if iv, ok := st.pending(); ok {
		st.record(iv)
	}
	st.open = false
}

func isVideoOrAudio(streamType uint8) bool {
	switch streamTypeLabel(streamType) {
	case "video", "audio":
		return true
	}
	return false
}

// PESProblemCount is the number of headers of one PID with one problem.
type PESProblemCount struct {
	Problem PESHeaderProblem
	Count   int
	// FirstOffset is the offset of the first PES packet with the problem.
	FirstOffset int64
}

// PESHeaderResult is the header check of one PID or of all PIDs.
//
// Status is PESErrors when at least one header failed a check, PESOK
// when at least one header was checked and none failed, and
// PESInsufficientData when no header was checked. Reason explains
// PESInsufficientData: PESReasonUntrustedPayload, PESReasonIncompleteHeaders or
// PESReasonNoPES.
type PESHeaderResult struct {
	Status PESStatus
	Reason PESReason
	// Checked is the number of headers checked, valid or not.
	Checked int
	Errors  int
	// Unchecked is the number of PES packets whose header the data ended
	// before (BreakIncompleteHeader).
	Unchecked int
	// FirstErrorOffset is meaningful when Errors is not zero.
	FirstErrorOffset int64
	// Problems lists the problems found, in PESHeaderProblem order.
	Problems []PESProblemCount
}

// PTSRepetitionResult is the PTS repetition check of one PID or of all PIDs.
//
// Status is PESNG when an interval exceeded the limit, PESOK when at
// least one interval was measured and none exceeded it,
// PESInsufficientData when nothing could be measured, and PESSkipped
// for a PID whose stream_type is not a video or audio type. Reason explains
// the last two.
type PTSRepetitionResult struct {
	Status PESStatus
	Reason PESReason
	// PTS is the number of valid headers with a PTS.
	PTS int
	// Intervals is the number of intervals measured between two PES packets
	// carrying a PTS.
	Intervals int
	// MaxTicks is the largest interval (27 MHz ticks) that was measured or
	// that exceeded the limit as a lower bound, from the PES packet at
	// MaxFrom to the one at MaxTo. Meaningful when HasMax is set.
	HasMax         bool
	MaxTicks       uint64
	MaxFrom, MaxTo int64
	// Exceeded is the number of intervals above the limit; FirstExceeded is
	// the offset at which the first of them ended.
	Exceeded      int
	FirstExceeded int64
}

// PESPIDResult is the result of one elementary PID of one program.
type PESPIDResult struct {
	Program    uint16
	PID        uint16
	StreamType uint8 // the last applied stream_type
	Label      string
	Headers    PESHeaderResult
	Repetition PTSRepetitionResult
}

// PESResult holds the totals and the PIDs in program and PID order.
type PESResult struct {
	Headers    PESHeaderResult
	Repetition PTSRepetitionResult
	PIDs       []PESPIDResult
}

// Result returns the results of the PES packets observed so far. An interval
// still in progress counts as a lower bound, as at a break.
func (c *PESCheck) Result() PESResult {
	var r PESResult
	r.PIDs = make([]PESPIDResult, 0, len(c.streams))
	for key, st := range c.streams {
		r.PIDs = append(r.PIDs, PESPIDResult{
			Program:    key.program,
			PID:        key.pid,
			StreamType: st.streamType,
			Label:      streamTypeLabel(st.streamType),
			Headers:    st.headerResult(),
			Repetition: st.repetitionResult(),
		})
	}
	slices.SortFunc(r.PIDs, func(a, b PESPIDResult) int {
		if a.Program != b.Program {
			return int(a.Program) - int(b.Program)
		}
		return int(a.PID) - int(b.PID)
	})
	headers := []PESHeaderResult{}
	repetition := []PTSRepetitionResult{}
	for _, p := range r.PIDs {
		headers = append(headers, p.Headers)
		repetition = append(repetition, p.Repetition)
	}
	r.Headers = sumHeaders(headers)
	r.Repetition = sumRepetition(repetition)
	return r
}

func (st *pesStream) headerResult() PESHeaderResult {
	h := PESHeaderResult{Checked: st.pes, Unchecked: st.unchecked, Problems: []PESProblemCount{}}
	for problem := PESHeaderProblem(1); int(problem) < pesHeaderProblems; problem++ {
		p := st.problems[problem]
		if p.count == 0 {
			continue
		}
		if h.Errors == 0 || p.first < h.FirstErrorOffset {
			h.FirstErrorOffset = p.first
		}
		h.Errors += p.count
		h.Problems = append(h.Problems, PESProblemCount{Problem: problem, Count: p.count, FirstOffset: p.first})
	}
	h.Checked += h.Errors
	switch {
	case h.Errors > 0:
		h.Status = PESErrors
	case h.Checked > 0:
		h.Status = PESOK
	default:
		h.Status = PESInsufficientData
		h.Reason = PESReasonNoPES
		if st.untrusted > 0 {
			h.Reason = PESReasonUntrustedPayload
		} else if st.unchecked > 0 {
			h.Reason = PESReasonIncompleteHeaders
		}
	}
	return h
}

func (st *pesStream) repetitionResult() PTSRepetitionResult {
	r := PTSRepetitionResult{PTS: st.pts, Intervals: st.intervals, Exceeded: st.exceeded, FirstExceeded: st.firstOver}
	r.HasMax, r.MaxTicks, r.MaxFrom, r.MaxTo = st.hasMax, st.max.ticks, st.max.from, st.max.to
	if iv, ok := st.pending(); ok && iv.ticks > PTSRepetitionLimitTicks {
		if r.Exceeded == 0 {
			r.FirstExceeded = iv.to
		}
		r.Exceeded++
		if !r.HasMax || iv.ticks > r.MaxTicks {
			r.HasMax, r.MaxTicks, r.MaxFrom, r.MaxTo = true, iv.ticks, iv.from, iv.to
		}
	}
	errors := 0
	for _, p := range st.problems {
		errors += p.count
	}
	switch {
	case !st.applicable:
		r.Status, r.Reason = PESSkipped, PESReasonNotVideoOrAudio
	case r.Exceeded > 0:
		r.Status = PESNG
	case r.Intervals > 0:
		r.Status = PESOK
	default:
		r.Status = PESInsufficientData
		switch {
		case st.pes == 0 && st.untrusted > 0:
			r.Reason = PESReasonUntrustedPayload
		case st.pes == 0 && errors > 0:
			r.Reason = PESReasonMalformedHeaders
		case st.pes == 0:
			r.Reason = PESReasonNoPES
		case st.placed == 0:
			r.Reason = PESReasonNoPCRTiming
		case st.pts == 0:
			r.Reason = PESReasonNoPTS
		default:
			r.Reason = PESReasonSinglePTS
		}
	}
	return r
}

// sumHeaders adds up the header results of every PID: PESErrors when a
// PID had errors, PESOK when any header was checked, otherwise
// PESInsufficientData with the reason of the first PID (PESReasonNoPES when
// there is none).
func sumHeaders(pids []PESHeaderResult) PESHeaderResult {
	t := PESHeaderResult{Status: PESInsufficientData, Reason: PESReasonNoPES, Problems: []PESProblemCount{}}
	var counts [pesHeaderProblems]problemCount
	for i, p := range pids {
		if i == 0 {
			t.Reason = p.Reason
		}
		t.Checked += p.Checked
		t.Unchecked += p.Unchecked
		for _, pc := range p.Problems {
			c := &counts[pc.Problem]
			if c.count == 0 || pc.FirstOffset < c.first {
				c.first = pc.FirstOffset
			}
			c.count += pc.Count
		}
	}
	for problem := PESHeaderProblem(1); int(problem) < pesHeaderProblems; problem++ {
		if c := counts[problem]; c.count > 0 {
			if t.Errors == 0 || c.first < t.FirstErrorOffset {
				t.FirstErrorOffset = c.first
			}
			t.Errors += c.count
			t.Problems = append(t.Problems, PESProblemCount{Problem: problem, Count: c.count, FirstOffset: c.first})
		}
	}
	switch {
	case t.Errors > 0:
		t.Status, t.Reason = PESErrors, PESReasonNone
	case t.Checked > 0:
		t.Status, t.Reason = PESOK, PESReasonNone
	}
	return t
}

// sumRepetition combines the PIDs: PESNG when any PID is NG, else PESOK
// when any is OK, else PESInsufficientData when any PID applies (with the
// reason of the first such PID), else PESSkipped with
// PESReasonNoVideoOrAudio. The maximum is the largest of all PIDs.
func sumRepetition(pids []PTSRepetitionResult) PTSRepetitionResult {
	t := PTSRepetitionResult{Status: PESSkipped, Reason: PESReasonNoVideoOrAudio}
	ok, insufficient := false, PESReason("")
	for _, p := range pids {
		t.PTS += p.PTS
		t.Intervals += p.Intervals
		if p.Exceeded > 0 && (t.Exceeded == 0 || p.FirstExceeded < t.FirstExceeded) {
			t.FirstExceeded = p.FirstExceeded
		}
		t.Exceeded += p.Exceeded
		if p.HasMax && (!t.HasMax || p.MaxTicks > t.MaxTicks) {
			t.HasMax, t.MaxTicks, t.MaxFrom, t.MaxTo = true, p.MaxTicks, p.MaxFrom, p.MaxTo
		}
		switch p.Status {
		case PESOK:
			ok = true
		case PESInsufficientData:
			if insufficient == PESReasonNone {
				insufficient = p.Reason
			}
		}
	}
	switch {
	case t.Exceeded > 0:
		t.Status, t.Reason = PESNG, PESReasonNone
	case ok:
		t.Status, t.Reason = PESOK, PESReasonNone
	case insufficient != PESReasonNone:
		t.Status, t.Reason = PESInsufficientData, insufficient
	}
	return t
}
