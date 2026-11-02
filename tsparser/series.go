package tsparser

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
	prev      *pcrSample
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

// addPcr records a PCR observation. intervalMs is valid when measured is true.
func (s *programSeries) addPcr(pos int64, pcr uint64, discontinuity bool, intervalMs float64, measured bool) {
	cur := pcrSample{pos: pos, pcr: pcr, discontinuity: discontinuity}
	boundary := false
	if s.prev != nil {
		if isSegmentBoundary(*s.prev, cur) {
			boundary = true
		} else {
			s.elapsedMs += float64(pcr-s.prev.pcr) / float64(pcrTicksPerMs)
		}
	}
	s.prev = &cur
	s.elapsedAt[pos] = s.elapsedMs
	s.pcr = append(s.pcr, seriesPcr{
		pos:           pos,
		elapsedMs:     s.elapsedMs,
		pcrMs:         float64(pcr) / float64(pcrTicksPerMs),
		intervalMs:    intervalMs,
		hasInterval:   measured,
		discontinuity: boundary,
	})
}

// addPes records the timestamps of a successfully parsed PES. Its time is the
// elapsed time of the last PCR before it; gapMs is valid when bracketed is true.
func (s *programSeries) addPes(p *Pes, gapMs float64, bracketed bool) {
	entry := seriesPes{pid: p.pid, pos: p.pos, gapMs: gapMs, hasGap: bracketed}
	if p.prevPcr != 0 {
		entry.elapsedMs, entry.hasElapsed = s.elapsedAt[p.prevPcrPos]
	}
	if p.ptsDtsFlags == 2 || p.ptsDtsFlags == 3 {
		entry.ptsMs, entry.hasPts = float64(p.pts)/90, true
	}
	if p.ptsDtsFlags == 3 {
		entry.dtsMs, entry.hasDts = float64(p.dts)/90, true
	}
	s.pes = append(s.pes, entry)
}
