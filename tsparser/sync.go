package tsparser

import (
	"bytes"
	"fmt"
	"io"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

const (
	syncByteValue = 0x47
	// resyncSyncBytes is the number of consecutive sync bytes, one packet
	// apart, that establish a new alignment after a loss of synchronization:
	// the packet at the candidate offset and the four after it. Five is the
	// acquisition criterion ETSI TR 101 290 gives for TS_sync_loss (indicator
	// 1.1). A single 0x47 in a payload is never enough.
	resyncSyncBytes = 5
	// maxResyncScanBytes bounds the search after one loss: a new alignment
	// must start within this many bytes of the lost packet boundary, or the
	// analysis stops there.
	maxResyncScanBytes = 1 << 20
	// resyncBufferSize is the lookahead buffer allocated on the first loss.
	// It is fixed, independent of the input and of the corrupt span.
	resyncBufferSize = 64 << 10
)

// packetReader reads the packets of a scan and keeps them aligned. While
// every packet starts with the sync byte it reads straight from the input.
// At a packet boundary without one it reports the loss and searches forward,
// one byte at a time, for the next offset at which resyncSyncBytes
// consecutive packets start with a sync byte; the skipped bytes are never
// read as packets. The search keeps the bytes it has read in a fixed buffer,
// so the packets of the new alignment are then served from it. Each byte is
// examined as a candidate at most once and each candidate costs at most
// resyncSyncBytes comparisons, so repeated corruption takes linear time.
type packetReader struct {
	src        io.Reader
	packetSize int
	syncOffset int
	// end is the offset where the range ends (exclusive); 0 for none.
	end int64
	// pos is the offset of the next byte: the start of the next packet.
	pos int64
	// buf[head:tail] holds the bytes from pos on that were read ahead. buf is
	// nil until the first loss.
	buf        []byte
	head, tail int
	check      analysis.SyncCheck
	// lost is set when a loss was not recovered; no packet is read after it.
	lost bool
	// report prints each loss and recovery (text mode).
	report func(string)
}

func newPacketReader(src io.Reader, pos int64, packetSize int, end int64) *packetReader {
	return &packetReader{src: src, pos: pos, packetSize: packetSize, syncOffset: packetSize - tsPayloadSize, end: end}
}

// buffered returns the number of bytes read ahead of pos.
func (r *packetReader) buffered() int { return r.tail - r.head }

// fill reads len(p) bytes from pos on into p, from the lookahead buffer and
// then the input, and returns how many it got; fewer means the input ended.
func (r *packetReader) fill(p []byte) (int, error) {
	n := copy(p, r.buf[r.head:r.tail])
	r.head += n
	if n == len(p) {
		return n, nil
	}
	m, err := io.ReadFull(r.src, p[n:])
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		err = nil
	}
	return n + m, err
}

// next reads the packet at pos into packet, which is packetSize long. It
// returns the number of bytes read: packetSize for an aligned packet (pos then
// moves past it), 0 when the scan is over (the range or the input ended, or
// synchronization was lost for good), or fewer than packetSize for an
// incomplete packet at the end of the input, which starts at pos.
func (r *packetReader) next(packet []byte) (int, error) {
	for !r.lost {
		if r.end > 0 && r.pos+int64(r.packetSize) > r.end {
			return 0, nil
		}
		n, err := r.fill(packet)
		if err != nil {
			return 0, fmt.Errorf("file read error: %w", err)
		}
		if n < r.packetSize {
			return n, nil
		}
		if packet[r.syncOffset] == syncByteValue {
			r.pos += int64(n)
			return n, nil
		}
		if err := r.resync(packet); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

// resync handles a loss at pos, where packet was read: it finds the next
// alignment and leaves pos there with the bytes from it in the lookahead
// buffer, or marks the reader lost.
func (r *packetReader) resync(packet []byte) error {
	loss := r.pos
	found := packet[r.syncOffset]
	r.check.ObserveLoss(analysis.SyncLoss{Offset: loss})
	if r.buf == nil {
		r.buf = make([]byte, resyncBufferSize)
	}
	// Put the packet back without its first byte, ahead of what is still
	// buffered: the search starts one byte after the loss.
	pending := r.tail - r.head
	copy(r.buf[r.packetSize-1:], r.buf[r.head:r.tail])
	copy(r.buf, packet[1:])
	r.head, r.tail = 0, r.packetSize-1+pending
	r.pos = loss + 1

	window := r.syncOffset + (resyncSyncBytes-1)*r.packetSize + 1
	for r.pos-loss <= maxResyncScanBytes {
		if r.tail-r.head < window {
			more, err := r.readMore()
			if err != nil {
				return err
			}
			if !more {
				end := analysis.SyncEndInput
				if r.end > 0 && r.pos+int64(r.tail-r.head) >= r.end {
					end = analysis.SyncEndRange
				}
				return r.stop(loss, found, end)
			}
			continue
		}
		if r.aligned(r.buf[r.head:r.tail]) {
			r.check.ObserveResync(analysis.Resync{LossOffset: loss, Offset: r.pos})
			if r.report != nil {
				r.report(fmt.Sprintf("0x%08x sync lost (found 0x%02x instead of 0x47); resynchronized at 0x%08x after skipping %d bytes", loss, found, r.pos, r.pos-loss))
			}
			return nil
		}
		r.head++
		r.pos++
	}
	return r.stop(loss, found, analysis.SyncEndScanLimit)
}

// aligned reports whether data starts with resyncSyncBytes packets that each
// start with a sync byte.
func (r *packetReader) aligned(data []byte) bool {
	for k := range resyncSyncBytes {
		if data[r.syncOffset+k*r.packetSize] != syncByteValue {
			return false
		}
	}
	return true
}

// readMore moves the buffered bytes to the front of the buffer and reads more
// after them, without going past the end of the range. It reports whether any
// byte was added.
func (r *packetReader) readMore() (bool, error) {
	r.tail = copy(r.buf, r.buf[r.head:r.tail])
	r.head = 0
	room := len(r.buf) - r.tail
	if r.end > 0 {
		room = int(min(int64(room), max(r.end-(r.pos+int64(r.tail)), 0)))
	}
	if room == 0 {
		return false, nil
	}
	n, err := io.ReadFull(r.src, r.buf[r.tail:r.tail+room])
	r.tail += n
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return false, fmt.Errorf("file read error: %w", err)
	}
	return n > 0, nil
}

// stop ends the scan at the loss offset: no alignment was found.
func (r *packetReader) stop(loss int64, found byte, end analysis.SyncEnd) error {
	r.lost = true
	r.pos = loss
	r.head, r.tail = 0, 0
	r.check.ObserveUnrecovered(analysis.SyncUnrecovered{LossOffset: loss, End: end})
	if r.report != nil {
		r.report(fmt.Sprintf("0x%08x sync lost (found 0x%02x instead of 0x47); %s, analysis stopped", loss, found, syncEndText(end)))
	}
	return nil
}

// trailingAtEOF handles a range that ends rest bytes (less than a packet)
// after the last complete packet, as trailingBytesAtEOF does for an input
// read directly. It returns the number of leftover bytes only when the input
// also ends there.
func (r *packetReader) trailingAtEOF(packet []byte, rest int64) int {
	if r.lost {
		return 0
	}
	// The lookahead never reads past the range, so the bytes it holds come
	// first and the input continues after them.
	return trailingBytesAtEOF(io.MultiReader(bytes.NewReader(r.buf[r.head:r.tail]), r.src), packet, rest)
}

// hasAlignment reports whether data contains three consecutive sync bytes
// one packet apart, the alignment PAT discovery also requires.
func hasAlignment(data []byte, packetSize int) bool {
	syncOffset := packetSize - tsPayloadSize
	for i := syncOffset; i+2*packetSize < len(data); i++ {
		if data[i] == syncByteValue && data[i+packetSize] == syncByteValue && data[i+2*packetSize] == syncByteValue {
			return true
		}
	}
	return false
}

// syncEndText describes why no alignment was found after a loss.
func syncEndText(end analysis.SyncEnd) string {
	switch end {
	case analysis.SyncEndInput:
		return fmt.Sprintf("the input ended before %d aligned sync bytes", resyncSyncBytes)
	case analysis.SyncEndRange:
		return fmt.Sprintf("the range ended before %d aligned sync bytes", resyncSyncBytes)
	default:
		return fmt.Sprintf("no %d aligned sync bytes within %d bytes", resyncSyncBytes, maxResyncScanBytes)
	}
}
