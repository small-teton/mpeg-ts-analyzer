package tsparser

import (
	"fmt"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

// syncLostReason is the top-level reason of a report whose scan stopped at a
// loss of synchronization that was not recovered.
const syncLostReason = "sync_lost"

// ReportSync is the transport-stream synchronization result of the scan: how
// often a packet boundary had no sync byte, how the parser realigned and how
// many bytes it skipped. Offsets are absolute byte offsets in the input.
type ReportSync struct {
	Status                           string `json:"status"`
	Reason                           string `json:"reason,omitempty"`
	RequiredSyncBytes                int    `json:"required_sync_bytes"`
	MaxResyncScanBytes               int    `json:"max_resync_scan_bytes"`
	LossCount                        int    `json:"loss_count"`
	ResyncCount                      int    `json:"resync_count"`
	SkippedBytes                     int64  `json:"skipped_bytes"`
	FirstLossOffsetBytes             *int64 `json:"first_loss_offset_bytes,omitempty"`
	FirstResyncOffsetBytes           *int64 `json:"first_resync_offset_bytes,omitempty"`
	MaxSkippedBytes                  *int64 `json:"max_skipped_bytes,omitempty"`
	MaxSkippedOffsetBytes            *int64 `json:"max_skipped_offset_bytes,omitempty"`
	UnrecoveredOffsetBytes           *int64 `json:"unrecovered_offset_bytes,omitempty"`
	TrailingPartialPacketBytes       int    `json:"trailing_partial_packet_bytes"`
	TrailingPartialPacketOffsetBytes *int64 `json:"trailing_partial_packet_offset_bytes,omitempty"`
}

func syncLost(r analysis.SyncResult) bool { return r.Status == analysis.SyncLost }

func syncStatusString(status analysis.SyncStatus) string {
	switch status {
	case analysis.SyncRecovered:
		return "recovered"
	case analysis.SyncLost:
		return "lost"
	default:
		return "ok"
	}
}

func reportSync(r analysis.SyncResult) *ReportSync {
	offset := func(v int64) *int64 { return &v }
	s := &ReportSync{
		Status:                     syncStatusString(r.Status),
		RequiredSyncBytes:          resyncSyncBytes,
		MaxResyncScanBytes:         maxResyncScanBytes,
		LossCount:                  r.Losses,
		ResyncCount:                r.Resyncs,
		SkippedBytes:               r.SkippedBytes,
		TrailingPartialPacketBytes: r.TrailingBytes,
	}
	if r.Losses > 0 {
		s.FirstLossOffsetBytes = offset(r.FirstLossOffset)
	}
	if r.Resyncs > 0 {
		s.FirstResyncOffsetBytes = offset(r.FirstResyncOffset)
		s.MaxSkippedBytes = offset(r.MaxSkippedBytes)
		s.MaxSkippedOffsetBytes = offset(r.MaxSkippedOffset)
	}
	if r.Unrecovered {
		s.UnrecoveredOffsetBytes = offset(r.UnrecoveredOffset)
		s.Reason = fmt.Sprintf("synchronization lost at 0x%08x and not recovered: %s; the analysis stopped there", r.UnrecoveredOffset, syncEndText(r.UnrecoveredEnd))
	}
	if r.TrailingBytes > 0 {
		s.TrailingPartialPacketOffsetBytes = offset(r.TrailingOffset)
	}
	return s
}

// dumpSync prints the synchronization summary of a scan. A healthy stream
// still gets a one-line result, as for continuity counters.
func dumpSync(r analysis.SyncResult) {
	fmt.Println("-----------------------------")
	if r.Status == analysis.SyncOK {
		fmt.Println("TS Sync: no sync loss detected")
		return
	}
	fmt.Printf("TS Sync Summary: %s\n", syncStatusString(r.Status))
	fmt.Printf("  Sync losses   : %d (first at 0x%08x)\n", r.Losses, r.FirstLossOffset)
	if r.Resyncs > 0 {
		fmt.Printf("  Resyncs       : %d (first at 0x%08x)\n", r.Resyncs, r.FirstResyncOffset)
		fmt.Printf("  Skipped bytes : %d (largest %d at 0x%08x)\n", r.SkippedBytes, r.MaxSkippedBytes, r.MaxSkippedOffset)
	}
	if r.Unrecovered {
		fmt.Printf("  Not recovered : at 0x%08x, %s; analysis stopped\n", r.UnrecoveredOffset, syncEndText(r.UnrecoveredEnd))
	}
}
