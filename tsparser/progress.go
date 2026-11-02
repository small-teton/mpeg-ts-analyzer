package tsparser

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// Progress events are written for callers such as the desktop GUI. They go to
// stderr so stdout keeps only the analysis result.
var (
	progressOutput   io.Writer = os.Stderr
	progressInterval           = 250 * time.Millisecond
	progressNow                = time.Now
)

type progressEvent struct {
	Type           string `json:"type"`
	ProcessedBytes int64  `json:"processed_bytes"`
	TotalBytes     int64  `json:"total_bytes"`
	Done           bool   `json:"done,omitempty"`
}

// progressReadSeeker reports how far the analysis has read. It reports the
// furthest position reached, so seeking back (PAT/PMT discovery, retries) never
// makes progress go backwards.
type progressReadSeeker struct {
	rs       io.ReadSeeker
	pos      int64
	start    int64
	furthest int64
	total    int64
	last     time.Time
}

// newProgressReadSeeker reports progress over [offset, offset+total).
func newProgressReadSeeker(rs io.ReadSeeker, offset, total int64) *progressReadSeeker {
	return &progressReadSeeker{rs: rs, start: offset, furthest: offset, total: total, last: progressNow()}
}

// progressTotal is the number of bytes an analysis of [offset, offset+limit)
// reads from a file of the given size.
func progressTotal(size, offset, limit int64) int64 {
	total := max(size-offset, 0)
	if limit > 0 {
		total = min(total, limit)
	}
	return total
}

func fileSize(file *os.File) int64 {
	info, err := file.Stat()
	if err != nil {
		return 0
	}
	return info.Size()
}

func (p *progressReadSeeker) Read(b []byte) (int, error) {
	n, err := p.rs.Read(b)
	p.pos += int64(n)
	p.furthest = max(p.furthest, p.pos)
	if progressNow().Sub(p.last) >= progressInterval {
		p.emit(false)
	}
	return n, err
}

func (p *progressReadSeeker) Seek(offset int64, whence int) (int64, error) {
	pos, err := p.rs.Seek(offset, whence)
	if err == nil {
		p.pos = pos
	}
	return pos, err
}

// finish reports the final position once reading has ended.
func (p *progressReadSeeker) finish() {
	p.emit(true)
}

func (p *progressReadSeeker) emit(done bool) {
	p.last = progressNow()
	processed := min(max(p.furthest-p.start, 0), p.total)
	line, _ := json.Marshal(progressEvent{Type: "progress", ProcessedBytes: processed, TotalBytes: p.total, Done: done})
	_, _ = fmt.Fprintf(progressOutput, "%s\n", line)
}

// withProgress wraps the input reader when progress events were requested.
// The returned function reports the final event and must be called when the
// analysis ends.
func withProgress(rs io.ReadSeeker, file *os.File, options options.Options) (io.ReadSeeker, func()) {
	if options.Progress != "json" {
		return rs, func() {}
	}
	p := newProgressReadSeeker(rs, options.Offset, progressTotal(fileSize(file), options.Offset, options.Limit))
	return p, p.finish
}
