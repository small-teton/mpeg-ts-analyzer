package tsparser

import (
	"fmt"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

// PES header health and PTS repetition: the parser side.
//
// Each PES packet of an analyzed elementary stream is reported to
// analysis.PESCheck when it ends, with the problem Parse found in its header
// (see pes.go) and, for a valid header, its PTS/DTS and its time on the
// program's PCR timeline. Points where PES packets of a PID may have been lost
// or not read (untrusted payload, a continuity error, a header the data ended
// before) are reported as breaks. The check decides how they are counted and
// the status of each PID.

// pcrAnchor is one PCR of a program: its 27 MHz value, the offset of its
// packet and the PCR segment it belongs to. unwrapped is the PCR value
// counted on from the first PCR of the segment across every wraparound, so
// times of one segment can be subtracted however many wraps lie between them.
type pcrAnchor struct {
	valid     bool
	pcr       uint64
	unwrapped uint64
	pos       int64
	segment   int
}

// nextPcrAnchor returns the anchor of a PCR at pos that follows prev. measured
// tells whether the PCR interval from prev was measured; otherwise the PCR
// starts a new segment.
func nextPcrAnchor(prev pcrAnchor, pcr uint64, pos int64, measured bool) pcrAnchor {
	next := pcrAnchor{valid: true, pcr: pcr, unwrapped: pcr, pos: pos, segment: prev.segment}
	if measured {
		next.unwrapped = prev.unwrapped + (pcr+pcrWrapTicks-prev.pcr)%pcrWrapTicks
	} else {
		next.segment++
	}
	return next
}

// arrivalAt returns the time on the PCR timeline of the packet at pos, which
// lies after the PCR prev and before the PCR next: the PCR value at its
// offset, interpolated by byte position between them (the PCR of a packet is
// the time its last byte of program_clock_reference_base arrives; the
// difference to the packet start is negligible here). The time is unknown
// unless both PCRs are known and belong to the same segment, which makes them
// consecutive PCRs whose interval was measured; it is never extrapolated. The
// time is unwrapped within the segment (see pcrAnchor).
func arrivalAt(prev, next pcrAnchor, pos int64) analysis.ClockTime {
	if !prev.valid || !next.valid || next.segment != prev.segment || next.pos <= prev.pos {
		return analysis.ClockTime{}
	}
	span := next.unwrapped - prev.unwrapped
	elapsed := uint64(float64(span) * float64(pos-prev.pos) / float64(next.pos-prev.pos))
	return analysis.ClockTime{Valid: true, Ticks: prev.unwrapped + elapsed, Segment: prev.segment}
}

// maxPendingPES bounds the observations a program holds until its next PCR.
// Reaching it (this many PES packets and breaks between two PCRs) releases
// them, and the held PES packets stay unplaced.
const maxPendingPES = 256

// pesFeed passes the PES observations of one program to the PES check. A PES
// packet that ends before the PCR after its start has arrived is held until
// that PCR, so that it is placed on the PCR timeline by interpolation; later
// observations of its PID are held behind it, so the check sees each PID's
// observations in stream order. Held observations are released at the next
// PCR, and also at a layout change and at the end of the analysis, where the
// waiting PES packets stay unplaced. At most maxPendingPES observations are
// held.
type pesFeed struct {
	check   *analysis.PESCheck
	program uint16
	pending []pendingPES
	held    map[uint16]int // number of pending observations per PID
}

// pendingPES is a held PES observation or break.
type pendingPES struct {
	pes     analysis.PES
	brk     analysis.PESBreak
	isBreak bool
	// place is set for a PES waiting for the next PCR; prev is the PCR before
	// it.
	place bool
	prev  pcrAnchor
}

func newPESFeed(check *analysis.PESCheck, program uint16) *pesFeed {
	return &pesFeed{check: check, program: program, held: make(map[uint16]int)}
}

// observe passes a PES packet that started at offset pos between the PCRs
// prev and next (next is invalid when it has not arrived yet).
func (f *pesFeed) observe(o analysis.PES, prev, next pcrAnchor) {
	switch {
	case o.Problem == analysis.PESHeaderOK && prev.valid && !next.valid:
		f.hold(pendingPES{pes: o, place: true, prev: prev})
		return
	case o.Problem == analysis.PESHeaderOK:
		o.Arrival = arrivalAt(prev, next, o.Offset)
	}
	if f.held[o.PID] > 0 {
		f.hold(pendingPES{pes: o})
		return
	}
	f.check.ObservePES(o)
}

// observeBreak passes a break, behind the held observations of its PID.
func (f *pesFeed) observeBreak(b analysis.PESBreak) {
	if f.held[b.PID] > 0 {
		f.hold(pendingPES{brk: b, isBreak: true})
		return
	}
	f.check.ObserveBreak(b)
}

func (f *pesFeed) hold(item pendingPES) {
	pid := item.pes.PID
	if item.isBreak {
		pid = item.brk.PID
	}
	f.pending = append(f.pending, item)
	f.held[pid]++
	if len(f.pending) >= maxPendingPES {
		f.release(pcrAnchor{})
	}
}

// release passes every held observation in order, placing the waiting PES
// packets with next, the first PCR after them (invalid when there is none).
func (f *pesFeed) release(next pcrAnchor) {
	for _, item := range f.pending {
		switch {
		case item.isBreak:
			f.check.ObserveBreak(item.brk)
		default:
			if item.place {
				item.pes.Arrival = arrivalAt(item.prev, next, item.pes.Offset)
			}
			f.check.ObservePES(item.pes)
		}
	}
	f.pending = f.pending[:0]
	clear(f.held)
}

// observePES reports a completed PES packet to the PES check. next tells
// whether the next PES of the PID ended it (see completePes).
func (a *programAnalyzer) observePES(pes *Pes, problem analysis.PESHeaderProblem, next bool) {
	if problem == analysis.PESHeaderTruncated && !next {
		a.pes.observeBreak(analysis.PESBreak{Program: a.pes.program, PID: pes.pid, Offset: pes.pos, Cause: analysis.BreakIncompleteHeader})
		return
	}
	o := analysis.PES{Program: a.pes.program, PID: pes.pid, Offset: pes.pos, Problem: problem}
	if problem == analysis.PESHeaderOK {
		o.HasPTS, o.HasDTS = pes.ptsDtsFlags >= 2, pes.ptsDtsFlags == 3
		o.PTS, o.DTS = pes.pts, pes.dts
	}
	a.pes.observe(o, pes.prevAnchor, pes.nextAnchor)
}

// pesReasonText is the text summary's wording of each reason.
var pesReasonText = map[analysis.PESReason]string{
	analysis.PESReasonNoPES:             "no PES packet started",
	analysis.PESReasonUntrustedPayload:  "payload not used because of transport errors or scrambling",
	analysis.PESReasonIncompleteHeaders: "no PES header was complete",
	analysis.PESReasonMalformedHeaders:  "every PES header was malformed",
	analysis.PESReasonNoPCRTiming:       "no PES packet lies between two PCRs",
	analysis.PESReasonNoPTS:             "no PTS observed",
	analysis.PESReasonSinglePTS:         "fewer than two comparable PTS",
	analysis.PESReasonNotVideoOrAudio:   "not a video or audio stream",
	analysis.PESReasonNoVideoOrAudio:    "no video or audio stream",
	analysis.PESReasonNoComparableLeads: "the audio and video leads never lay in one PCR segment at the same time",
}

// dumpPESHealth prints the PES header and PTS repetition summary. A healthy
// stream gets one line for each; PIDs are listed only when their status is not
// ok (and, for PTS repetition, not skipped).
func dumpPESHealth(r analysis.PESResult) {
	fmt.Println("-----------------------------")
	h := r.Headers
	switch h.Status {
	case analysis.PESOK:
		fmt.Printf("PES Headers: %d checked on %d %s, no errors%s\n", h.Checked, len(r.PIDs), pidWord(len(r.PIDs)), uncheckedText(h.Unchecked))
	case analysis.PESErrors:
		fmt.Printf("PES Headers: %d %s in %d checked on %d %s, first at 0x%08x%s [ERRORS]\n", h.Errors, errorWord(h.Errors), h.Checked, len(r.PIDs), pidWord(len(r.PIDs)), h.FirstErrorOffset, uncheckedText(h.Unchecked))
	default:
		fmt.Printf("PES Headers: INSUFFICIENT DATA (%s)\n", pesReasonText[h.Reason])
	}
	for _, p := range r.PIDs {
		switch p.Headers.Status {
		case analysis.PESErrors:
			fmt.Printf("  %s : %d %s in %d checked, first at 0x%08x [ERRORS]\n", pesPIDText(p), p.Headers.Errors, errorWord(p.Headers.Errors), p.Headers.Checked, p.Headers.FirstErrorOffset)
			for _, problem := range p.Headers.Problems {
				fmt.Printf("    %s: %d, first at 0x%08x\n", problem.Problem, problem.Count, problem.FirstOffset)
			}
		case analysis.PESInsufficientData:
			fmt.Printf("  %s : INSUFFICIENT DATA (%s)\n", pesPIDText(p), pesReasonText[p.Headers.Reason])
		}
	}

	t := r.Repetition
	const title = "PTS Repetition (limit 700ms)"
	switch t.Status {
	case analysis.PESOK:
		fmt.Printf("%s: max interval %fms over %d %s [OK]\n", title, pcrToMs(float64(t.MaxTicks)), t.Intervals, intervalWord(t.Intervals))
	case analysis.PESNG:
		fmt.Printf("%s: max interval %fms from 0x%08x to 0x%08x, %d exceeded, first ending at 0x%08x [NG]\n", title, pcrToMs(float64(t.MaxTicks)), t.MaxFrom, t.MaxTo, t.Exceeded, t.FirstExceeded)
	case analysis.PESInsufficientData:
		fmt.Printf("%s: INSUFFICIENT DATA (%s)\n", title, pesReasonText[t.Reason])
	default:
		fmt.Printf("%s: SKIPPED (%s)\n", title, pesReasonText[t.Reason])
	}
	for _, p := range r.PIDs {
		switch rep := p.Repetition; rep.Status {
		case analysis.PESNG:
			fmt.Printf("  %s : max interval %fms from 0x%08x to 0x%08x, %d exceeded, first ending at 0x%08x [NG]\n", pesPIDText(p), pcrToMs(float64(rep.MaxTicks)), rep.MaxFrom, rep.MaxTo, rep.Exceeded, rep.FirstExceeded)
		case analysis.PESInsufficientData:
			fmt.Printf("  %s : INSUFFICIENT DATA (%s)\n", pesPIDText(p), pesReasonText[rep.Reason])
		}
	}
}

func pesPIDText(p analysis.PESPIDResult) string {
	return fmt.Sprintf("Program %d PID 0x%04x (%s)", p.Program, p.PID, p.Label)
}

func uncheckedText(unchecked int) string {
	if unchecked == 0 {
		return ""
	}
	return fmt.Sprintf(", %d unchecked", unchecked)
}

func intervalWord(count int) string {
	if count == 1 {
		return "interval"
	}
	return "intervals"
}

// ReportPESHeaderError is the count of one header problem.
type ReportPESHeaderError struct {
	Kind             string `json:"kind"`
	Count            int    `json:"count"`
	FirstOffsetBytes int64  `json:"first_offset_bytes"`
}

// ReportPESHeaderCounts are the header counts of one PID or of all PIDs.
type ReportPESHeaderCounts struct {
	Status                string                 `json:"status"`
	Reason                string                 `json:"reason,omitempty"`
	CheckedCount          int                    `json:"checked_count"`
	ErrorCount            int                    `json:"error_count"`
	UncheckedCount        int                    `json:"unchecked_count"`
	FirstErrorOffsetBytes *int64                 `json:"first_error_offset_bytes,omitempty"`
	Errors                []ReportPESHeaderError `json:"errors"`
}

// ReportPESStream identifies an elementary stream of a program.
type ReportPESStream struct {
	ProgramNumber uint16 `json:"program_number"`
	PID           string `json:"pid"`
	StreamType    string `json:"stream_type"`
	Label         string `json:"label"`
}

// ReportPESHeaderPID is the header result of one elementary PID.
type ReportPESHeaderPID struct {
	ReportPESStream
	ReportPESHeaderCounts
}

// ReportPESHeaders is the pes_headers result: the totals and every elementary
// PID of the analyzed programs in program and PID order.
type ReportPESHeaders struct {
	ReportPESHeaderCounts
	PIDs []ReportPESHeaderPID `json:"pids"`
}

// ReportPTSRepetitionCounts are the PTS repetition values of one PID or of all
// PIDs. The interval fields are present when an interval was measured or
// exceeded the limit.
type ReportPTSRepetitionCounts struct {
	Status                      string   `json:"status"`
	Reason                      string   `json:"reason,omitempty"`
	PTSCount                    int      `json:"pts_count"`
	IntervalCount               int      `json:"interval_count"`
	MaxIntervalMs               *float64 `json:"max_interval_ms,omitempty"`
	MaxIntervalStartOffsetBytes *int64   `json:"max_interval_start_offset_bytes,omitempty"`
	MaxIntervalEndOffsetBytes   *int64   `json:"max_interval_end_offset_bytes,omitempty"`
	ExceededCount               int      `json:"exceeded_count"`
	FirstExceededOffsetBytes    *int64   `json:"first_exceeded_offset_bytes,omitempty"`
}

// ReportPTSRepetitionPID is the PTS repetition result of one elementary PID.
type ReportPTSRepetitionPID struct {
	ReportPESStream
	ReportPTSRepetitionCounts
}

// ReportPTSRepetition is the pts_repetition result: the limit, the totals and
// every elementary PID of the analyzed programs in program and PID order.
type ReportPTSRepetition struct {
	LimitMs float64 `json:"limit_ms"`
	ReportPTSRepetitionCounts
	PIDs []ReportPTSRepetitionPID `json:"pids"`
}

// reportPESHealth returns the pes_headers and pts_repetition results.
func reportPESHealth(check *analysis.PESCheck) (*ReportPESHeaders, *ReportPTSRepetition) {
	r := check.Result()
	headers := &ReportPESHeaders{ReportPESHeaderCounts: reportPESHeaderCounts(r.Headers), PIDs: []ReportPESHeaderPID{}}
	repetition := &ReportPTSRepetition{LimitMs: pcrToMs(analysis.PTSRepetitionLimitTicks), ReportPTSRepetitionCounts: reportPTSRepetitionCounts(r.Repetition), PIDs: []ReportPTSRepetitionPID{}}
	for _, p := range r.PIDs {
		stream := ReportPESStream{ProgramNumber: p.Program, PID: reportPID(p.PID), StreamType: fmt.Sprintf("0x%02x", p.StreamType), Label: p.Label}
		headers.PIDs = append(headers.PIDs, ReportPESHeaderPID{stream, reportPESHeaderCounts(p.Headers)})
		repetition.PIDs = append(repetition.PIDs, ReportPTSRepetitionPID{stream, reportPTSRepetitionCounts(p.Repetition)})
	}
	return headers, repetition
}

func reportPESHeaderCounts(h analysis.PESHeaderResult) ReportPESHeaderCounts {
	r := ReportPESHeaderCounts{Status: string(h.Status), Reason: string(h.Reason), CheckedCount: h.Checked, ErrorCount: h.Errors, UncheckedCount: h.Unchecked, Errors: []ReportPESHeaderError{}}
	if h.Errors > 0 {
		first := h.FirstErrorOffset
		r.FirstErrorOffsetBytes = &first
	}
	for _, p := range h.Problems {
		r.Errors = append(r.Errors, ReportPESHeaderError{Kind: p.Problem.String(), Count: p.Count, FirstOffsetBytes: p.FirstOffset})
	}
	return r
}

func reportPTSRepetitionCounts(t analysis.PTSRepetitionResult) ReportPTSRepetitionCounts {
	r := ReportPTSRepetitionCounts{Status: string(t.Status), Reason: string(t.Reason), PTSCount: t.PTS, IntervalCount: t.Intervals, ExceededCount: t.Exceeded}
	if t.HasMax {
		maxMs, from, to := pcrToMs(float64(t.MaxTicks)), t.MaxFrom, t.MaxTo
		r.MaxIntervalMs, r.MaxIntervalStartOffsetBytes, r.MaxIntervalEndOffsetBytes = &maxMs, &from, &to
	}
	if t.Exceeded > 0 {
		first := t.FirstExceeded
		r.FirstExceededOffsetBytes = &first
	}
	return r
}
