package analysis

// Initial acquisition.
//
// Program-level analysis needs a packet alignment and then a complete PAT,
// which gives the programs, their PMT PIDs and, through the PMTs, the PCR PIDs
// and elementary-stream roles. When the parser cannot acquire them, it does
// not guess: the analysis stops. ClassifyAcquisition turns what the parser
// observed before stopping into the prerequisite that failed, so the report
// can name it and show the evidence instead of a generic failure.

// AlignmentLayout is one supported physical packet layout and what the
// parser's detector requires to accept it: RequiredSyncBytes sync bytes, one
// packet apart, the first SyncOffset bytes into a packet. Found reports
// whether the detector accepted the layout anywhere in the scanned range.
type AlignmentLayout struct {
	PacketSize        int
	SyncOffset        int
	RequiredSyncBytes int
	Found             bool
}

// RequiredBytes is the shortest input in which the detector can find the
// layout: from the first byte of a packet to its last required sync byte.
func (l AlignmentLayout) RequiredBytes() int64 {
	return int64(l.SyncOffset + (l.RequiredSyncBytes-1)*l.PacketSize + 1)
}

// AlignmentStatus is the result for one layout.
type AlignmentStatus int

const (
	// AlignmentFound means the detector accepted the layout.
	AlignmentFound AlignmentStatus = iota
	// AlignmentNotFound means the scanned range was long enough for the
	// detector's evidence but did not contain it.
	AlignmentNotFound
	// AlignmentInsufficientData means the scanned range was shorter than
	// RequiredBytes, so the layout could not be established whatever the
	// bytes were.
	AlignmentInsufficientData
)

// AcquisitionEvidence is what the parser observed in a range that it could
// not start analyzing. ScannedBytes is the number of bytes of the range it
// read; Layouts lists every supported layout. PATPackets is the number of
// aligned packets on PID 0 and is used only when a layout was found.
type AcquisitionEvidence struct {
	ScannedBytes int64
	Layouts      []AlignmentLayout
	PATPackets   int64
}

// AcquisitionOutcome is the prerequisite that failed.
type AcquisitionOutcome int

const (
	// AcquisitionSyncNotAcquired means no supported layout was found
	// although the range was long enough to show each of them.
	AcquisitionSyncNotAcquired AcquisitionOutcome = iota
	// AcquisitionSyncInsufficientData means no layout was found and the range
	// was too short to show at least one of them.
	AcquisitionSyncInsufficientData
	// AcquisitionPATNotObserved means packets were aligned but none was on
	// PID 0.
	AcquisitionPATNotObserved
	// AcquisitionPATNotAcquired means packets on PID 0 were observed but no
	// usable PAT was acquired from them.
	AcquisitionPATNotAcquired
)

// LayoutResult is the status of one layout.
type LayoutResult struct {
	AlignmentLayout
	Status AlignmentStatus
}

// AcquisitionResult is the classified failure: the outcome and the status of
// every layout, in the order of the evidence.
type AcquisitionResult struct {
	Outcome AcquisitionOutcome
	Layouts []LayoutResult
}

// ClassifyAcquisition decides which prerequisite of program analysis failed.
// A found layout makes the failure a PAT failure; otherwise a layout the range
// was too short to show makes it insufficient data rather than a range
// without any alignment, so a short fragment is never reported as unaligned
// data.
func ClassifyAcquisition(e AcquisitionEvidence) AcquisitionResult {
	r := AcquisitionResult{Outcome: AcquisitionSyncNotAcquired, Layouts: make([]LayoutResult, 0, len(e.Layouts))}
	aligned, short := false, false
	for _, layout := range e.Layouts {
		status := AlignmentNotFound
		switch {
		case layout.Found:
			status, aligned = AlignmentFound, true
		case e.ScannedBytes < layout.RequiredBytes():
			status, short = AlignmentInsufficientData, true
		}
		r.Layouts = append(r.Layouts, LayoutResult{AlignmentLayout: layout, Status: status})
	}
	switch {
	case aligned && e.PATPackets == 0:
		r.Outcome = AcquisitionPATNotObserved
	case aligned:
		r.Outcome = AcquisitionPATNotAcquired
	case short:
		r.Outcome = AcquisitionSyncInsufficientData
	}
	return r
}
