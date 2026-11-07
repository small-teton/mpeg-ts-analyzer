package tsparser

import "github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"

// programSeries collects the time series of one program for the range
// analysis output: every PCR with its interval, and every PES timestamp with
// its PCR→PTS/DTS gap.
//
// Times are given as elapsed program time: milliseconds of PCR time since the
// program's first PCR in the range. Across a PCR discontinuity the clock does
// not advance, the same model the bitrate windows use, so the series share one
// time axis.
type programSeries struct {
	pcr       []seriesPcr
	pes       []seriesPes
	elapsedMs float64
	elapsedAt map[int64]float64 // PCR packet offset → elapsed time
}

type seriesPcr struct {
	pos           int64
	elapsedMs     float64
	pcrMs         float64
	intervalMs    float64
	hasInterval   bool
	discontinuity bool
}

type seriesPes struct {
	pid        uint16
	pos        int64
	elapsedMs  float64
	hasElapsed bool
	ptsMs      float64
	hasPts     bool
	dtsMs      float64
	hasDts     bool
	gapMs      float64
	hasGap     bool
}

func newProgramSeries() *programSeries {
	return &programSeries{elapsedAt: make(map[int64]float64)}
}

// addPcr records a PCR observation classified as step against the previous
// PCR: a comparable step advances the elapsed time and carries the interval, a
// discontinuity freezes the elapsed time.
func (s *programSeries) addPcr(pos int64, pcr uint64, step analysis.PCRStep) {
	if step.Comparable() {
		s.elapsedMs += float64(step.Ticks) / float64(pcrTicksPerMs)
	}
	s.elapsedAt[pos] = s.elapsedMs
	s.pcr = append(s.pcr, seriesPcr{
		pos:           pos,
		elapsedMs:     s.elapsedMs,
		pcrMs:         float64(pcr) / float64(pcrTicksPerMs),
		intervalMs:    float64(step.Ticks) / float64(pcrTicksPerMs),
		hasInterval:   step.Comparable(),
		discontinuity: step.Boundary(),
	})
}

// addPes records the timestamps of a successfully parsed PES. Its time is the
// elapsed time of the last PCR before it; gapMs is valid when bracketed is true.
func (s *programSeries) addPes(p *Pes, gapMs float64, bracketed bool) {
	entry := seriesPes{pid: p.pid, pos: p.pos, gapMs: gapMs, hasGap: bracketed}
	if p.hasPcrReference() {
		entry.elapsedMs, entry.hasElapsed = s.elapsedAt[p.prevAnchor.pos]
	}
	if p.ptsDtsFlags == 2 || p.ptsDtsFlags == 3 {
		entry.ptsMs, entry.hasPts = float64(p.pts)/90, true
	}
	if p.ptsDtsFlags == 3 {
		entry.dtsMs, entry.hasDts = float64(p.dts)/90, true
	}
	s.pes = append(s.pes, entry)
}
