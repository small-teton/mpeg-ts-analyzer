package tsparser

import (
	"errors"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

func TestPesParseHeaderProblems(t *testing.T) {
	pts := []byte{0x21, 0x00, 0x05, 0xBF, 0x21}                                  // '0010', PTS 90000
	ptsDts := []byte{0x31, 0x00, 0x05, 0xBF, 0x21, 0x11, 0x00, 0x05, 0xBF, 0x21} // '0011' + '0001'
	header := func(length uint16, flags1, flags2 byte, fields ...byte) []byte {
		h := []byte{0x00, 0x00, 0x01, 0xE0, byte(length >> 8), byte(length), flags1, flags2, byte(len(fields))}
		return append(h, fields...)
	}
	withByte := func(b []byte, i int, v byte) []byte {
		c := append([]byte(nil), b...)
		c[i] = v
		return c
	}
	for _, tt := range []struct {
		name string
		data []byte
		want analysis.PESHeaderProblem
	}{
		{"empty", nil, analysis.PESHeaderTruncated},
		{"prefix only", []byte{0x00, 0x00, 0x01}, analysis.PESHeaderTruncated},
		{"bad prefix", []byte{0x00, 0x00, 0x02}, analysis.PESHeaderBadStartCode},
		{"stream_id below 0xBC", []byte{0x00, 0x00, 0x01, 0xBA, 0x00, 0x00}, analysis.PESHeaderBadStartCode},
		{"no PES_packet_length", []byte{0x00, 0x00, 0x01, 0xE0, 0x00}, analysis.PESHeaderTruncated},
		{"padding stream", []byte{0x00, 0x00, 0x01, 0xBE, 0x00, 0x10}, analysis.PESHeaderOK},
		{"fixed header cut", []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80}, analysis.PESHeaderTruncated},
		{"no '10'", header(0, 0x40, 0x80, pts...), analysis.PESHeaderBadFlags},
		{"PTS_DTS_flags 01", header(0, 0x80, 0x40, pts...), analysis.PESHeaderBadFlags},
		{"header data too short for PTS", header(0, 0x80, 0x80, pts[:4]...), analysis.PESHeaderInconsistentLength},
		{"header data too short for every flag", header(0, 0x80, 0xFF, make([]byte, 23)...), analysis.PESHeaderInconsistentLength},
		{"PES_packet_length shorter than the header", header(7, 0x80, 0x80, pts...), analysis.PESHeaderInconsistentLength},
		{"PES_packet_length equal to the header", header(8, 0x80, 0x80, pts...), analysis.PESHeaderOK},
		{"optional fields cut", header(0, 0x80, 0x80, pts...)[:12], analysis.PESHeaderTruncated},
		{"stuffing cut", header(0, 0x80, 0x80, append(pts, 0xFF, 0xFF)...)[:15], analysis.PESHeaderTruncated},
		{"PTS alone", header(0, 0x80, 0x80, pts...), analysis.PESHeaderOK},
		{"PTS prefix 0011 without DTS", header(0, 0x80, 0x80, withByte(pts, 0, 0x31)...), analysis.PESHeaderBadMarkerBits},
		{"PTS first marker", header(0, 0x80, 0x80, withByte(pts, 0, 0x20)...), analysis.PESHeaderBadMarkerBits},
		{"PTS second marker", header(0, 0x80, 0x80, withByte(pts, 2, 0x04)...), analysis.PESHeaderBadMarkerBits},
		{"PTS third marker", header(0, 0x80, 0x80, withByte(pts, 4, 0x20)...), analysis.PESHeaderBadMarkerBits},
		{"PTS and DTS", header(0, 0x80, 0xC0, ptsDts...), analysis.PESHeaderOK},
		{"extension without fields", header(0, 0x80, 0x81, append(pts, 0x00)...), analysis.PESHeaderOK},
		{"extension private data missing", header(0, 0x80, 0x01, 0x80), analysis.PESHeaderInconsistentLength},
		{"extension private data", header(0, 0x80, 0x01, append([]byte{0x80}, make([]byte, 16)...)...), analysis.PESHeaderOK},
		{"extension pack header length missing", header(0, 0x80, 0x01, 0x40), analysis.PESHeaderInconsistentLength},
		{"extension pack header too long", header(0, 0x80, 0x01, 0x40, 0x05, 0x00), analysis.PESHeaderInconsistentLength},
		{"extension pack header", header(0, 0x80, 0x01, 0x40, 0x01, 0x00), analysis.PESHeaderOK},
		{"extension sequence counter and P-STD missing", header(0, 0x80, 0x01, 0x30, 0x00, 0x00), analysis.PESHeaderInconsistentLength},
		{"extension sequence counter and P-STD", header(0, 0x80, 0x01, 0x30, 0x00, 0x00, 0x00, 0x00), analysis.PESHeaderOK},
		{"extension 2 length missing", header(0, 0x80, 0x01, 0x01), analysis.PESHeaderInconsistentLength},
		{"extension 2 too long", header(0, 0x80, 0x01, 0x01, 0x82, 0x00), analysis.PESHeaderInconsistentLength},
		{"extension 2", header(0, 0x80, 0x01, 0x01, 0x81, 0x00), analysis.PESHeaderOK},
		{"PTS of PTS and DTS", header(0, 0x80, 0xC0, withByte(ptsDts, 0, 0x21)...), analysis.PESHeaderBadMarkerBits},
		{"DTS prefix", header(0, 0x80, 0xC0, withByte(ptsDts, 5, 0x31)...), analysis.PESHeaderBadMarkerBits},
		{"DTS marker", header(0, 0x80, 0xC0, withByte(ptsDts, 9, 0x20)...), analysis.PESHeaderBadMarkerBits},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pes := NewPes()
			pes.Append(tt.data)
			err := pes.Parse()
			if got := pesProblem(err); got != tt.want {
				t.Errorf("Parse() = %v (%s), want %s", err, got, tt.want)
			}
			if (err == nil) != (tt.want == analysis.PESHeaderOK) || err != nil && err.Error() == "" {
				t.Errorf("Parse() = %v", err)
			}
		})
	}
	if got := pesProblem(errors.New("other")); got != analysis.PESHeaderOK {
		t.Errorf("pesProblem(other error) = %s", got)
	}
}

// A PES header split over TS packets is assembled before it is checked.
func TestPesParseSplitHeader(t *testing.T) {
	data := []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05, 0x21, 0x00, 0x05, 0xBF, 0x21}
	pes := NewPes()
	pes.Append(data[:4])
	pes.Append(data[4:11])
	pes.Append(data[11:])
	if err := pes.Parse(); err != nil || pes.pts != 90000 {
		t.Errorf("Parse() = %v, pts %d", err, pes.pts)
	}
}

func TestArrivalAt(t *testing.T) {
	prev := pcrAnchor{valid: true, pcr: 1000, unwrapped: 1000, pos: 0, segment: 1}
	next := nextPcrAnchor(prev, 2000, 100, true)
	if got := arrivalAt(prev, next, 25); got != (analysis.ClockTime{Valid: true, Ticks: 1250, Segment: 1}) {
		t.Errorf("interpolated = %+v", got)
	}
	// Across the PCR wrap the time keeps counting, also over several wraps.
	high := pcrAnchor{valid: true, pcr: pcrWrapTicks - 100, unwrapped: pcrWrapTicks - 100, pos: 0}
	wrapped := nextPcrAnchor(high, 100, 100, true)
	if got := arrivalAt(high, wrapped, 75); got != (analysis.ClockTime{Valid: true, Ticks: pcrWrapTicks + 50}) {
		t.Errorf("wrapped = %+v", got)
	}
	again := nextPcrAnchor(nextPcrAnchor(wrapped, pcrWrapTicks-100, 200, true), 100, 300, true)
	if again.unwrapped != 2*pcrWrapTicks+100 {
		t.Errorf("twice wrapped = %d", again.unwrapped)
	}
	for name, n := range map[string]pcrAnchor{
		"no next PCR":   {},
		"new segment":   nextPcrAnchor(prev, 2000, 100, false),
		"same position": {valid: true, pcr: 2000, pos: 0, segment: 1},
	} {
		if got := arrivalAt(prev, n, 0); got.Valid {
			t.Errorf("%s: %+v, want unknown", name, got)
		}
	}
	if got := arrivalAt(pcrAnchor{}, next, 0); got.Valid {
		t.Errorf("no previous PCR: %+v", got)
	}
}

func TestPESFeed(t *testing.T) {
	newFeed := func() *pesFeed {
		check := analysis.NewPESCheck()
		check.ObserveProgramLayout(1, analysis.ProgramLayout{Streams: []analysis.ElementaryStream{{PID: 0x100, StreamType: 0x02}, {PID: 0x101, StreamType: 0x03}}})
		return newPESFeed(check, 1)
	}
	pes := func(pid uint16, offset int64, problem analysis.PESHeaderProblem) analysis.PES {
		return analysis.PES{Program: 1, PID: pid, Offset: offset, HasPTS: problem == analysis.PESHeaderOK, Problem: problem}
	}
	prev := pcrAnchor{valid: true, pcr: 0, pos: 0}
	next := nextPcrAnchor(prev, 2000*27000, 2000, true)

	f := newFeed()
	f.observe(pes(0x100, 0, analysis.PESHeaderOK), prev, next) // placed now
	f.observe(pes(0x100, 1000, analysis.PESHeaderOK), prev, pcrAnchor{})
	f.observe(pes(0x100, 1100, analysis.PESHeaderBadFlags), prev, pcrAnchor{})
	f.observeBreak(analysis.PESBreak{Program: 1, PID: 0x100, Offset: 1200})
	f.observeBreak(analysis.PESBreak{Program: 1, PID: 0x101, Offset: 1300, Cause: analysis.BreakUntrustedPayload})
	if len(f.pending) != 3 || f.held[0x100] != 3 || f.held[0x101] != 0 {
		t.Fatalf("pending = %+v, held = %v", f.pending, f.held)
	}
	f.release(next)
	r := f.check.Result()
	video := r.PIDs[0]
	if len(f.pending) != 0 || video.Repetition.Intervals != 1 || video.Repetition.MaxTicks != 1000*27000 || video.Headers.Errors != 1 ||
		r.PIDs[1].Headers.Reason != analysis.PESReasonUntrustedPayload {
		t.Errorf("after release: %+v", r)
	}

	// Too many observations between two PCRs are released unplaced.
	f = newFeed()
	for i := range maxPendingPES {
		f.observe(pes(0x101, int64(i), analysis.PESHeaderOK), prev, pcrAnchor{})
	}
	if len(f.pending) != 0 {
		t.Fatalf("%d observations still pending", len(f.pending))
	}
	if rep := f.check.Result().PIDs[1].Repetition; rep.PTS != maxPendingPES || rep.Reason != analysis.PESReasonNoPCRTiming {
		t.Errorf("overflow: %+v", rep)
	}
}

// A PES whose header the data ended before is not a header problem.
func TestCompletePesIncompleteHeader(t *testing.T) {
	a := newProgramAnalyzer(options.Options{}, 0, 1, 0x1000, 0x100, []ProgramInfo{{elementaryPid: 0x100, streamType: 0x02}}, false)
	pes := NewPes()
	pes.Initialize(0x100, 188, 0, 0, false)
	pes.Append([]byte{0x00, 0x00, 0x01, 0xE0, 0x00})
	a.pesMap[0x100] = pes
	a.finish()
	h := a.pes.check.Result().Headers
	if h.Status != analysis.PESInsufficientData || h.Reason != analysis.PESReasonIncompleteHeaders || h.Unchecked != 1 || h.Errors != 0 {
		t.Errorf("headers = %+v", h)
	}
}

func TestDumpPESHealth(t *testing.T) {
	r := analysis.PESResult{
		Headers:    analysis.PESHeaderResult{Status: analysis.PESOK, Checked: 3, Unchecked: 1},
		Repetition: analysis.PTSRepetitionResult{Status: analysis.PESOK, Intervals: 1, HasMax: true, MaxTicks: 40 * 27000},
		PIDs:       []analysis.PESPIDResult{{Program: 1, PID: 0x100, Label: "video"}},
	}
	out := captureStdout(t, func() { dumpPESHealth(r) })
	want := "PES Headers: 3 checked on 1 PID, no errors, 1 unchecked\nPTS Repetition (limit 700ms): max interval 40.000000ms over 1 interval [OK]\n"
	if !strings.HasSuffix(out, want) {
		t.Errorf("ok:\n%s", out)
	}
	r = analysis.PESResult{
		Headers:    analysis.PESHeaderResult{Status: analysis.PESInsufficientData, Reason: analysis.PESReasonNoPES},
		Repetition: analysis.PTSRepetitionResult{Status: analysis.PESSkipped, Reason: analysis.PESReasonNoVideoOrAudio},
	}
	out = captureStdout(t, func() { dumpPESHealth(r) })
	want = "PES Headers: INSUFFICIENT DATA (no PES packet started)\nPTS Repetition (limit 700ms): SKIPPED (no video or audio stream)\n"
	if !strings.HasSuffix(out, want) {
		t.Errorf("skipped:\n%s", out)
	}
}

// A continuity error ends the PES in progress: it is parsed from the packets
// before the loss, the continuation after the loss is not appended, and no
// PTS interval spans the loss.
func TestContinuityErrorEndsPes(t *testing.T) {
	a := newProgramAnalyzer(options.Options{}, 0, 1, 0x1000, 0x100, []ProgramInfo{{elementaryPid: 0x100, streamType: 0x02}}, false)
	pes := NewPes()
	pes.Initialize(0x100, 0, 0, 0, false)
	pes.Append([]byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x80, 0x05, 0x21, 0x00, 0x05, 0xBF, 0x21})
	a.pesMap[0x100] = pes
	a.breakPES(0x100, 376, analysis.BreakContinuityError)
	if a.pesMap[0x100] != nil {
		t.Fatal("the PES in progress was kept after the continuity error")
	}
	if h := a.pes.check.Result().Headers; h.Checked != 1 || h.Errors != 0 {
		t.Errorf("headers = %+v, want the PES before the loss checked", h)
	}
	// A break on a PID without a PES in progress, or outside the program,
	// only records the break.
	a.breakPES(0x100, 564, analysis.BreakContinuityError)
	a.breakPES(0x200, 564, analysis.BreakContinuityError)
}
