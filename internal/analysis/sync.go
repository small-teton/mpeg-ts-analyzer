package analysis

// SyncLoss is a packet boundary at which the parser expected a sync byte
// (0x47) and found another value. Offset is the byte offset of the boundary:
// the first byte of the packet, which for 192-byte M2TS input is the first byte
// of its TP_extra_header.
type SyncLoss struct {
	Offset int64
}

// Resync is an alignment the parser established again after a SyncLoss.
// LossOffset is the offset of the loss and Offset that of the first packet of
// the new alignment; the bytes in between were skipped without being read as
// packets.
type Resync struct {
	LossOffset int64
	Offset     int64
}

// SyncEnd says why the parser found no new alignment after a loss.
type SyncEnd int

const (
	// SyncEndScanLimit means no alignment was found within the parser's fixed
	// search limit.
	SyncEndScanLimit SyncEnd = iota
	// SyncEndInput means the input ended before an alignment was confirmed.
	SyncEndInput
	// SyncEndRange means the analyzed range (--limit) ended before an
	// alignment was confirmed.
	SyncEndRange
)

// SyncUnrecovered is a loss after which the parser found no new alignment, so
// the analysis stopped at LossOffset.
type SyncUnrecovered struct {
	LossOffset int64
	End        SyncEnd
}

// TrailingPartialPacket is an incomplete packet at the end of the input: Bytes
// (fewer than one packet) starting at the packet boundary Offset.
type TrailingPartialPacket struct {
	Offset int64
	Bytes  int
}

// SyncStatus is the outcome of the synchronization check.
type SyncStatus int

const (
	// SyncOK means every packet boundary of the scan had its sync byte. A
	// trailing partial packet does not change it.
	SyncOK SyncStatus = iota
	// SyncRecovered means synchronization was lost at least once and every
	// loss was followed by a new alignment; the skipped bytes were not
	// analyzed.
	SyncRecovered
	// SyncLost means the last loss was not recovered and the analysis stopped
	// there.
	SyncLost
)

// SyncCheck aggregates the synchronization observations of one scan. Its
// state is a fixed set of counters and offsets, independent of the input
// length and of the number of losses.
type SyncCheck struct {
	result SyncResult
}

// ObserveLoss counts one loss of synchronization.
func (c *SyncCheck) ObserveLoss(o SyncLoss) {
	r := &c.result
	if r.Losses == 0 {
		r.FirstLossOffset = o.Offset
	}
	r.Losses++
}

// ObserveResync counts one recovered loss and the bytes it skipped.
func (c *SyncCheck) ObserveResync(o Resync) {
	r := &c.result
	skipped := o.Offset - o.LossOffset
	if r.Resyncs == 0 {
		r.FirstResyncOffset = o.Offset
	}
	if skipped > r.MaxSkippedBytes {
		r.MaxSkippedBytes, r.MaxSkippedOffset = skipped, o.LossOffset
	}
	r.Resyncs++
	r.SkippedBytes += skipped
}

// ObserveUnrecovered records the loss after which the analysis stopped.
func (c *SyncCheck) ObserveUnrecovered(o SyncUnrecovered) {
	c.result.Unrecovered = true
	c.result.UnrecoveredOffset = o.LossOffset
	c.result.UnrecoveredEnd = o.End
}

// ObserveTrailingPartialPacket records the incomplete packet at the end of
// the input.
func (c *SyncCheck) ObserveTrailingPartialPacket(o TrailingPartialPacket) {
	c.result.TrailingOffset, c.result.TrailingBytes = o.Offset, o.Bytes
}

// SyncResult is the synchronization summary of a scan. The offsets are only
// meaningful when the count or flag they belong to is set: FirstLossOffset
// with Losses, FirstResyncOffset and MaxSkipped* with Resyncs,
// UnrecoveredOffset and UnrecoveredEnd with Unrecovered, TrailingOffset with
// TrailingBytes.
type SyncResult struct {
	Status            SyncStatus
	Losses            int
	Resyncs           int
	SkippedBytes      int64
	FirstLossOffset   int64
	FirstResyncOffset int64
	// MaxSkippedBytes is the largest number of bytes skipped by one recovered
	// loss, and MaxSkippedOffset the offset of that loss.
	MaxSkippedBytes   int64
	MaxSkippedOffset  int64
	Unrecovered       bool
	UnrecoveredOffset int64
	UnrecoveredEnd    SyncEnd
	TrailingBytes     int
	TrailingOffset    int64
}

// Result returns the summary of the observations so far.
func (c *SyncCheck) Result() SyncResult {
	r := c.result
	switch {
	case r.Unrecovered:
		r.Status = SyncLost
	case r.Losses > 0:
		r.Status = SyncRecovered
	default:
		r.Status = SyncOK
	}
	return r
}
