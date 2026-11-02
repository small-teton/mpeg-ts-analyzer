package tsparser

import (
	"bufio"
	"io"
)

const fileReadBufferSize = 256 << 10

// bufferedReadSeeker batches the small per-packet reads into large reads of the
// underlying file. Seek discards the buffer.
type bufferedReadSeeker struct {
	rs io.ReadSeeker
	br *bufio.Reader
}

func newBufferedReadSeeker(rs io.ReadSeeker) *bufferedReadSeeker {
	return &bufferedReadSeeker{rs: rs, br: bufio.NewReaderSize(rs, fileReadBufferSize)}
}

func (b *bufferedReadSeeker) Read(p []byte) (int, error) {
	return b.br.Read(p)
}

func (b *bufferedReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekCurrent {
		// The underlying reader is ahead of the caller by the buffered bytes.
		offset -= int64(b.br.Buffered())
	}
	pos, err := b.rs.Seek(offset, whence)
	if err != nil {
		return pos, err
	}
	b.br.Reset(b.rs)
	return pos, nil
}
