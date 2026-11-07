package analysis

import (
	"fmt"
	"slices"
)

// Status is the outcome of a check.
type Status int

const (
	// StatusOK means the check ran and found nothing.
	StatusOK Status = iota
	// StatusErrors means the check found at least one error.
	StatusErrors
	// StatusAffected means the check found affected packets, none of which
	// carried data the analysis reads.
	StatusAffected
	// StatusDataExcluded means the check found affected packets and the
	// parser left data of at least one of them out of the analysis.
	StatusDataExcluded
	// StatusNG means the check found a violation of its rule.
	StatusNG
	// StatusInsufficientData means the check found no violation but the
	// range did not hold enough evidence to call the result OK.
	StatusInsufficientData
	// StatusSkipped means the check could not be evaluated: the input held
	// too little data for it, for example no PCR time base.
	StatusSkipped
)

// ContinuityCheck aggregates continuity_counter errors per PID for the
// continuity summary. Its state is one error count and one label per PID, so it
// is bounded by the 8192 possible PIDs regardless of the input length.
type ContinuityCheck struct {
	errors map[uint16]int
	labels pidLabels
}

// NewContinuityCheck returns a check with no errors and no named PIDs.
func NewContinuityCheck() *ContinuityCheck {
	return &ContinuityCheck{errors: make(map[uint16]int), labels: make(pidLabels)}
}

// ObserveLayout names the PIDs of an applied program layout in the result:
// PID 0 is the PAT, then the PMT PID, the PCR_PID (unless it is unset or 0x1FFF)
// and each elementary stream by its stream_type, later names replacing earlier
// ones. A name is kept after the layout changes, so an error counted earlier
// keeps a meaningful label.
func (c *ContinuityCheck) ObserveLayout(layout ProgramLayout) { c.labels.observe(layout) }

// ObserveError counts one continuity error.
func (c *ContinuityCheck) ObserveError(e ContinuityError) { c.errors[e.PID]++ }

// ContinuityPID is the error count of one PID.
type ContinuityPID struct {
	PID uint16
	// Label names the PID's role: "PAT", "PMT", "PCR", "video", "audio",
	// "stream_type 0xNN", or "unknown" for a PID no layout named.
	Label  string
	Errors int
}

// ContinuityResult is the continuity summary: StatusErrors with the PIDs that
// had errors in ascending PID order, or StatusOK with no PIDs.
type ContinuityResult struct {
	Status      Status
	TotalErrors int
	PIDs        []ContinuityPID
}

// Result returns the summary of the errors observed so far.
func (c *ContinuityCheck) Result() ContinuityResult {
	r := ContinuityResult{Status: StatusOK, PIDs: make([]ContinuityPID, 0, len(c.errors))}
	for pid, count := range c.errors {
		r.PIDs = append(r.PIDs, ContinuityPID{PID: pid, Label: c.labels.label(pid, "unknown"), Errors: count})
		r.TotalErrors += count
	}
	slices.SortFunc(r.PIDs, func(a, b ContinuityPID) int { return int(a.PID) - int(b.PID) })
	if r.TotalErrors > 0 {
		r.Status = StatusErrors
	}
	return r
}

// pidLabels names the role of each PID from the applied program layouts: PID
// 0 is the PAT, then the PMT PID, the PCR_PID (unless it is unset or 0x1FFF)
// and each elementary stream by its stream_type, later names replacing earlier
// ones. A name is kept after the layout changes, so a PID counted earlier
// keeps a meaningful label. It holds at most one name per PID.
type pidLabels map[uint16]string

func (l pidLabels) observe(layout ProgramLayout) {
	l[0] = "PAT"
	l[layout.PMTPID] = "PMT"
	if layout.PCRPID != 0 && layout.PCRPID != nullPID {
		l[layout.PCRPID] = "PCR"
	}
	for _, stream := range layout.Streams {
		l[stream.PID] = streamTypeLabel(stream.StreamType)
	}
}

// label returns the name of pid, or fallback when no layout named it.
func (l pidLabels) label(pid uint16, fallback string) string {
	if label, ok := l[pid]; ok {
		return label
	}
	return fallback
}

func streamTypeLabel(streamType uint8) string {
	switch streamType {
	case 0x01, 0x02, 0x10, 0x1B, 0x24:
		return "video"
	case 0x03, 0x04, 0x0F, 0x11:
		return "audio"
	default:
		return fmt.Sprintf("stream_type 0x%02x", streamType)
	}
}
