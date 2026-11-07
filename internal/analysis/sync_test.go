package analysis

import "testing"

func TestSyncCheckWithoutLoss(t *testing.T) {
	var c SyncCheck
	if got := c.Result(); got != (SyncResult{Status: SyncOK}) {
		t.Errorf("empty check: %+v", got)
	}
	// A trailing partial packet is not a loss.
	c.ObserveTrailingPartialPacket(TrailingPartialPacket{Offset: 1880, Bytes: 100})
	if got := c.Result(); got != (SyncResult{Status: SyncOK, TrailingBytes: 100, TrailingOffset: 1880}) {
		t.Errorf("trailing partial packet: %+v", got)
	}
}

func TestSyncCheckRecovered(t *testing.T) {
	var c SyncCheck
	c.ObserveLoss(SyncLoss{Offset: 940})
	c.ObserveResync(Resync{LossOffset: 940, Offset: 941})
	c.ObserveLoss(SyncLoss{Offset: 1880})
	c.ObserveResync(Resync{LossOffset: 1880, Offset: 2880})
	c.ObserveLoss(SyncLoss{Offset: 4000})
	c.ObserveResync(Resync{LossOffset: 4000, Offset: 4188})
	want := SyncResult{
		Status:            SyncRecovered,
		Losses:            3,
		Resyncs:           3,
		SkippedBytes:      1 + 1000 + 188,
		FirstLossOffset:   940,
		FirstResyncOffset: 941,
		MaxSkippedBytes:   1000,
		MaxSkippedOffset:  1880,
	}
	if got := c.Result(); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSyncCheckLost(t *testing.T) {
	var c SyncCheck
	c.ObserveLoss(SyncLoss{Offset: 940})
	c.ObserveResync(Resync{LossOffset: 940, Offset: 1128})
	c.ObserveLoss(SyncLoss{Offset: 5000})
	c.ObserveUnrecovered(SyncUnrecovered{LossOffset: 5000, End: SyncEndInput})
	want := SyncResult{
		Status:            SyncLost,
		Losses:            2,
		Resyncs:           1,
		SkippedBytes:      188,
		FirstLossOffset:   940,
		FirstResyncOffset: 1128,
		MaxSkippedBytes:   188,
		MaxSkippedOffset:  940,
		Unrecovered:       true,
		UnrecoveredOffset: 5000,
		UnrecoveredEnd:    SyncEndInput,
	}
	if got := c.Result(); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
