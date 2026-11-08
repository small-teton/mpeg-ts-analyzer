package analysis

import "testing"

func testLayouts(found188, found192, found204 bool) []AlignmentLayout {
	return []AlignmentLayout{
		{PacketSize: 188, RequiredSyncBytes: 3, Found: found188},
		{PacketSize: 192, SyncOffset: 4, RequiredSyncBytes: 3, Found: found192},
		{PacketSize: 204, RequiredSyncBytes: 5, Found: found204},
	}
}

func TestAlignmentLayoutRequiredBytes(t *testing.T) {
	want := []int64{377, 389, 817}
	for i, layout := range testLayouts(false, false, false) {
		if got := layout.RequiredBytes(); got != want[i] {
			t.Errorf("%d-byte layout: RequiredBytes = %d, want %d", layout.PacketSize, got, want[i])
		}
	}
}

func TestClassifyAcquisition(t *testing.T) {
	cases := []struct {
		name     string
		evidence AcquisitionEvidence
		outcome  AcquisitionOutcome
		statuses [3]AlignmentStatus
	}{
		{
			name:     "empty range",
			evidence: AcquisitionEvidence{Layouts: testLayouts(false, false, false)},
			outcome:  AcquisitionSyncInsufficientData,
			statuses: [3]AlignmentStatus{AlignmentInsufficientData, AlignmentInsufficientData, AlignmentInsufficientData},
		},
		{
			name:     "two 188-byte packets",
			evidence: AcquisitionEvidence{ScannedBytes: 376, Layouts: testLayouts(false, false, false)},
			outcome:  AcquisitionSyncInsufficientData,
			statuses: [3]AlignmentStatus{AlignmentInsufficientData, AlignmentInsufficientData, AlignmentInsufficientData},
		},
		{
			name:     "long enough for 188 and 192 but not 204",
			evidence: AcquisitionEvidence{ScannedBytes: 816, Layouts: testLayouts(false, false, false)},
			outcome:  AcquisitionSyncInsufficientData,
			statuses: [3]AlignmentStatus{AlignmentNotFound, AlignmentNotFound, AlignmentInsufficientData},
		},
		{
			name:     "long enough for every layout",
			evidence: AcquisitionEvidence{ScannedBytes: 817, Layouts: testLayouts(false, false, false)},
			outcome:  AcquisitionSyncNotAcquired,
			statuses: [3]AlignmentStatus{AlignmentNotFound, AlignmentNotFound, AlignmentNotFound},
		},
		{
			name:     "aligned without PID 0",
			evidence: AcquisitionEvidence{ScannedBytes: 564, Layouts: testLayouts(true, false, false)},
			outcome:  AcquisitionPATNotObserved,
			statuses: [3]AlignmentStatus{AlignmentFound, AlignmentNotFound, AlignmentInsufficientData},
		},
		{
			name:     "aligned with PID 0 packets",
			evidence: AcquisitionEvidence{ScannedBytes: 4000, Layouts: testLayouts(false, false, true), PATPackets: 2},
			outcome:  AcquisitionPATNotAcquired,
			statuses: [3]AlignmentStatus{AlignmentNotFound, AlignmentNotFound, AlignmentFound},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ClassifyAcquisition(c.evidence)
			if r.Outcome != c.outcome {
				t.Errorf("Outcome = %d, want %d", r.Outcome, c.outcome)
			}
			if len(r.Layouts) != 3 {
				t.Fatalf("Layouts = %+v, want 3", r.Layouts)
			}
			for i, layout := range r.Layouts {
				if layout.Status != c.statuses[i] || layout.PacketSize != c.evidence.Layouts[i].PacketSize {
					t.Errorf("layout %d = %+v, want %d-byte status %d", i, layout, c.evidence.Layouts[i].PacketSize, c.statuses[i])
				}
			}
		})
	}
}
