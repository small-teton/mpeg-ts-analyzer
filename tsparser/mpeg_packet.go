package tsparser

import (
	"fmt"
	"io"
	"sort"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// MpegPacket PSI or PES
type MpegPacket interface {
	ContinuityCounter() uint8
	SetContinuityCounter(continuityCounter uint8)
	Append(buf []byte)
	Parse() error
	Dump()
}

const (
	tsPayloadSize     = 188
	tpExtraHeaderSize = 4
)

// BufferPsi buffer PSI data from TS payload: one section, from the first
// payload unit start on pid to the next. The analyzer itself assembles complete
// multi-section PAT and PMT tables instead.
func BufferPsi(reader io.Reader, pos *int64, pid uint16, mpegPacket MpegPacket, options options.Options, packetSize int, endOffset int64) error {
	tsBuffer := make([]byte, packetSize)
	isBuffering := false
	tsPacket := NewTsPacket()

	for endOffset <= 0 || *pos+int64(packetSize) <= endOffset {
		size, err := io.ReadFull(reader, tsBuffer)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			// A trailing partial packet cannot carry a usable section; stop as at EOF.
			break
		} else if err != nil {
			return fmt.Errorf("file read error: %w", err)
		}

		tsData := tsBytes(tsBuffer, packetSize)
		tsPacket.Initialize(*pos, options)
		tsPacket.Append(tsData)
		if err := tsPacket.Parse(); err != nil {
			return fmt.Errorf("failed to parse TS packet in BufferPsi: %w", err)
		}
		*pos += int64(size)

		if tsPacket.Pid() != pid {
			continue
		}

		buf := tsPacket.Payload()
		if len(buf) == 0 {
			continue
		}
		if tsPacket.PayloadUnitStartIndicator() {
			// buf[0] is the pointer_field; reject values that run past the payload.
			if 1+int(buf[0]) > len(buf) {
				return fmt.Errorf("invalid pointer_field %d at pos:0x%08x", buf[0], *pos)
			}
			if isBuffering {
				mpegPacket.Append(buf[1 : 1+buf[0]]) // read until pointer_field
				break
			} else {
				mpegPacket.Append(buf[1+buf[0]:]) // read until pointer_field
				isBuffering = true
			}
		} else if tsPacket.ContinuityCounter() == (mpegPacket.ContinuityCounter()+1)&0xF {
			mpegPacket.SetContinuityCounter(tsPacket.ContinuityCounter())
			mpegPacket.Append(tsPacket.Payload())
		} else {
			return fmt.Errorf("packet loss. pos:0x%08x", *pos)
		}
	}
	return nil
}

// BufferPes buffer PES data from TS payload
func BufferPes(reader io.Reader, pos *int64, pmtPid, pcrPid uint16, programInfos []ProgramInfo, options options.Options, packetSize int, endOffset int64) error {
	_, err := bufferPes(reader, pos, pmtPid, pcrPid, programInfos, nil, options, packetSize, endOffset, false)
	return err
}

type pesAnalysis struct {
	compliance complianceReport
	continuity *continuityCounterSummary
	trust      *transportTrustSummary
	pes        *analysis.PESCheck
	references *pidReferenceSummary // nil without PSI tracking
	anomalies  *TimestampAnomaly
	jitter     *PcrJitter
	bitrate    *BitrateStats
	// truncatedBytes is the size of an incomplete packet left at the end of the
	// input (0 when the input ends on a packet boundary), starting at truncatedPos.
	truncatedBytes int
	truncatedPos   int64
	// sync is the synchronization result of the scan.
	sync analysis.SyncResult
	// transitions and psiWarnings come from following PAT/PMT version changes.
	transitions []psiTransition
	psiWarnings []psiDiagnostic
	sdt         *sdtCollector
	health      *psiHealthCollector
	// policy is the selected failure policy and failedCategories the
	// selected categories that reported a finding.
	policy           failurePolicy
	failedCategories []string
}

// trackedTables are the PAT and PMT a single-program analysis starts from;
// later versions of them are followed during the scan.
type trackedTables struct {
	pat           *patTable
	programNumber uint16
	pmt           *pmtTable
	// trust and references have already counted, and health has already
	// seen, the packets read while acquiring the PAT and PMT.
	trust      *transportTrustSummary
	references *pidReferenceSummary
	health     *psiHealthCollector
}

func truncatedPacketMessage(size int) string {
	return fmt.Sprintf("ignored %d trailing bytes (incomplete packet at end of input)", size)
}

// trailingBytesAtEOF handles a --limit range that ends less than a packet
// after the last complete one. It returns the number of leftover bytes only when
// the input also ends there; a range that merely ends mid-stream is not a
// truncated input. Detecting an input that ends exactly at the limit reads one
// byte past it.
func trailingBytesAtEOF(reader io.Reader, buf []byte, rest int64) int {
	if rest <= 0 || rest >= int64(len(buf)) {
		return 0
	}
	n, err := io.ReadFull(reader, buf[:rest])
	if err == nil {
		if _, err = io.ReadFull(reader, buf[:1]); err != io.EOF {
			return 0
		}
	} else if err != io.ErrUnexpectedEOF {
		return 0
	}
	return n
}

func bufferPes(reader io.Reader, pos *int64, pmtPid, pcrPid uint16, programInfos []ProgramInfo, tables *trackedTables, options options.Options, packetSize int, endOffset int64, quiet bool) (*pesAnalysis, error) {
	policy, err := parseFailurePolicy(options.FailOn, options.FailOnError)
	if err != nil {
		return nil, err
	}
	analyzer := newProgramAnalyzer(options, *pos, uint16(options.Program), pmtPid, pcrPid, programInfos, false)
	analyzer.pcrJitter.packetSize = packetSize
	continuity := newContinuityCounterSummary(pmtPid, pcrPid, programInfos)
	trust := newTransportTrustSummary()
	var psi *psiTracker
	if tables != nil {
		trust = tables.trust
		psi = newPsiTracker(tables.pat, continuity, options, packetSize, quiet)
		psi.trust = trust
		psi.health = tables.health
		psi.health.ref = analyzer
		psi.addProgram(tables.programNumber, pmtPid, tables.pmt, analyzer)
		psi.references = tables.references
		tables.references.usePcrOf([]*programAnalyzer{analyzer})
		analyzer.attachPESCheck(analysis.NewPESCheck(), tables.programNumber)
	}
	trust.addProgramLabels(pmtPid, pcrPid, programInfos)
	scan, err := scanPrograms(reader, pos, []*programAnalyzer{analyzer}, continuity, trust, psi, options, packetSize, endOffset, quiet)
	if err != nil {
		return nil, err
	}
	analyzer.finish()

	report := analyzer.complianceReport()
	result := &pesAnalysis{compliance: report, continuity: continuity, trust: trust, pes: analyzer.pes.check, anomalies: analyzer.anomaly, truncatedBytes: scan.truncatedBytes, truncatedPos: scan.truncatedPos, sync: scan.sync}
	if psi != nil {
		result.transitions, result.psiWarnings, result.sdt, result.health = psi.transitions, psi.warnings, psi.sdt, psi.health
		result.references = psi.references
	}
	if options.DumpPcrJitter {
		result.jitter = &analyzer.pcrJitter
	}
	if analyzer.bitrate != nil {
		result.bitrate = analyzer.bitrate
	}
	if !quiet {
		dumpSync(scan.sync)
		report.dump()
		if options.DumpPcrJitter {
			analyzer.pcrJitter.Dump()
		}
		if analyzer.bitrate != nil {
			analyzer.bitrate.Dump()
		}
		analyzer.anomaly.Dump()
		continuity.Dump()
		trust.Dump()
		if result.health != nil {
			dumpPsiHealth(reportPsiHealth(result.health))
		}
		if psi != nil {
			psi.references.Dump()
		}
		dumpPESHealth(analyzer.pes.check.Result())
		// The report-only packet gaps and mux lead come last, after every
		// verdict.
		if psi != nil {
			dumpPacketGaps(psi.references)
		}
		dumpMuxLead(analyzer.pes.check)
	}
	result.policy = policy
	failed, checks := policy.evaluate(result)
	result.failedCategories = failed
	if len(checks) > 0 {
		return result, &ComplianceError{Checks: checks, Categories: failed}
	}

	return result, nil
}

// scanResult describes how a scan of packets ended.
type scanResult struct {
	truncatedBytes   int
	truncatedPos     int64
	continuityEvents []continuityEvent
	// sync is the synchronization result of the scan.
	sync analysis.SyncResult
}

// scanPrograms reads packets from *pos until the end of the input or endOffset
// and feeds each one to every analyzer whose start it has reached and that has
// not stopped. Continuity counters are checked once for the whole multiplex.
// When psi is set, each packet is then passed to it so PAT/PMT version changes
// apply from the next packet on.
func scanPrograms(reader io.Reader, pos *int64, analyzers []*programAnalyzer, continuity *continuityCounterSummary, trust *transportTrustSummary, psi *psiTracker, options options.Options, packetSize int, endOffset int64, quiet bool) (scanResult, error) {
	var result scanResult
	tsBuffer := make([]byte, packetSize)
	tsPacket := NewTsPacket()
	continuityTracker := newContinuityTracker()
	packets := newPacketReader(reader, *pos, packetSize, endOffset)
	if !quiet {
		packets.report = func(message string) { fmt.Println(message) }
	}

	for {
		size, err := packets.next(tsBuffer)
		if err != nil {
			return result, err
		}
		if size == 0 {
			break
		} else if size < packetSize {
			// Captures cut off mid-packet are common; keep the analysis of the
			// complete packets and report the leftover bytes.
			result.truncatedBytes = size
			break
		}
		*pos = packets.pos - int64(size)

		tsData := tsBytes(tsBuffer, packetSize)
		tsPacket.Initialize(*pos, options)
		tsPacket.Append(tsData)
		if err := tsPacket.Parse(); err != nil {
			// The caller retries from the input position after this packet.
			if seeker, ok := reader.(io.Seeker); ok && packets.buffered() > 0 {
				if _, serr := seeker.Seek(packets.pos, io.SeekStart); serr != nil {
					return result, fmt.Errorf("file seek error: %w", serr)
				}
			}
			return result, fmt.Errorf("failed to parse TS packet in BufferPes: %w", err)
		}
		continuityResult := continuityTracker.Check(tsPacket)
		if continuityResult.Event != nil {
			event := continuityResult.Event
			if !quiet {
				fmt.Printf("packet loss. : pid=0x%04x. count=0x%x, pos=0x%08x\n", event.PID, event.Actual, event.Pos)
			}
			continuity.Add(*event)
			result.continuityEvents = append(result.continuityEvents, *event)
			for _, analyzer := range analyzers {
				if *pos >= analyzer.start && !analyzer.stopped {
					analyzer.breakPES(event.PID, *pos, analysis.BreakContinuityError)
				}
			}
		}
		// Before the analyzers and PSI tracking see the packet, which may stop
		// a program or move a PMT.
		trust.observe(tsPacket, *pos, analyzers, psi)
		for _, analyzer := range analyzers {
			if *pos >= analyzer.start && !analyzer.stopped {
				analyzer.observe(tsPacket, *pos, continuityResult.Duplicate)
			}
		}
		if psi != nil {
			if psi.health != nil {
				psi.health.observe(tsPacket, *pos)
			}
			// After the analyzers, which pass the packet's PCR to the PID
			// reference check, and before PSI tracking applies a change
			// from the next packet on.
			if psi.references != nil {
				psi.references.observe(tsPacket, *pos, analyzers, psi)
			}
			psi.observe(tsPacket, *pos, continuityTracker)
		}
	}
	*pos = packets.pos
	if psi != nil {
		psi.finish(*pos)
		if psi.references != nil {
			psi.references.finish(*pos)
		}
	}

	if result.truncatedBytes == 0 && endOffset > 0 {
		result.truncatedBytes = packets.trailingAtEOF(tsBuffer, endOffset-*pos)
	}
	if result.truncatedBytes > 0 {
		result.truncatedPos = *pos
		packets.check.ObserveTrailingPartialPacket(analysis.TrailingPartialPacket{Offset: *pos, Bytes: result.truncatedBytes})
		if !quiet {
			fmt.Printf("0x%08x %s\n", *pos, truncatedPacketMessage(result.truncatedBytes))
		}
	}
	result.sync = packets.check.Result()
	return result, nil
}

// programAnalyzer holds the per-program state of a scan: PCR timing, PES
// timestamps, bitrate and PCR jitter of one program.
type programAnalyzer struct {
	options       options.Options
	start         int64 // offset of the first packet analyzed for this program
	pcrPid        uint16
	pesMap        map[uint16]*Pes
	lastPcr       uint64 // the last PCR, for --dump-timestamp
	maxDelay      float64
	pcr           analysis.PCRCheck
	pcrPtsSamples int
	collectJitter bool
	pcrJitter     PcrJitter
	bitrate       *BitrateStats
	anomaly       *TimestampAnomaly
	// pes feeds the check of the PES headers and the PTS repetition of the
	// program; in a range analysis the check is shared by every program.
	pes *pesFeed
	// clock is the last PCR, which places a PES that starts next on the PCR
	// timeline.
	clock        pcrAnchor
	series       *programSeries // nil unless time series are collected
	programInfos []ProgramInfo  // elementary streams of the current layout
	// onPcr, when set, receives each PCR the analysis uses and its step: the
	// time basis of the PID reference check.
	onPcr func(pos int64, step analysis.PCRStep)
	// pcrDiscontinuityPending is set by a packet of the PCR_PID with
	// discontinuity_indicator set but no PCR: the next PCR of the PID starts
	// the new time base (ISO/IEC 13818-1, 2.4.3.5).
	pcrDiscontinuityPending bool
	// pcrPidChanged is set when a PMT version changes the PCR_PID: the next
	// PCR is a pcr_pid_change step even if the PCR_PID changed back to the
	// PID of the previous PCR in the meantime.
	pcrPidChanged bool
	stopped       bool
	end           int64 // offset after the last packet analyzed, once stopped
}

// newProgramAnalyzer prepares the analysis of one program from offset start.
// withSeries collects bitrate, PCR jitter and time series regardless of the
// dump flags.
func newProgramAnalyzer(options options.Options, start int64, programNumber, pmtPid, pcrPid uint16, programInfos []ProgramInfo, withSeries bool) *programAnalyzer {
	a := &programAnalyzer{
		options:       options,
		start:         start,
		pcrPid:        pcrPid,
		pesMap:        make(map[uint16]*Pes),
		collectJitter: options.DumpPcrJitter || withSeries,
		// PTS/DTS anomaly detection is always on; it prints only when a problem is
		// found, so a healthy stream stays quiet. Only a cleanly parsed PES is
		// checked — a partially parsed header would carry garbage timestamps.
		anomaly:      NewTimestampAnomaly(),
		programInfos: programInfos,
	}
	for _, val := range programInfos {
		a.pesMap[val.elementaryPid] = nil
	}
	if options.DumpBitrate || withSeries {
		a.bitrate = NewBitrateStats(int(programNumber), pmtPid, pcrPid, programInfos)
	}
	if withSeries {
		a.series = newProgramSeries()
	}
	a.attachPESCheck(analysis.NewPESCheck(), programNumber)
	return a
}

// attachPESCheck makes check record the PES packets of the program, numbered
// program, from its current layout on.
func (a *programAnalyzer) attachPESCheck(check *analysis.PESCheck, program uint16) {
	a.pes = newPESFeed(check, program)
	check.ObserveProgramLayout(program, programLayout(0, a.pcrPid, a.programInfos))
}

// breakPES records that PES packets of pid may have been lost or not read at
// pos, if pid is an elementary stream of the program.
//
// After a continuity error the PES in progress ends: it is parsed from the
// packets before the loss, and the PID waits for its next
// payload_unit_start_indicator, so no PES packet spans the loss.
func (a *programAnalyzer) breakPES(pid uint16, pos int64, cause analysis.BreakCause) {
	pes, stream := a.pesMap[pid]
	if !stream {
		return
	}
	if cause == analysis.BreakContinuityError && pes != nil {
		a.completePes(pes, false)
		a.pesMap[pid] = nil
	}
	a.anomaly.markUntimed(pid)
	a.pes.observeBreak(analysis.PESBreak{Program: a.pes.program, PID: pid, Offset: pos, Cause: cause})
}

// observe processes one packet of the multiplex at offset pos.
func (a *programAnalyzer) observe(tsPacket *TsPacket, pos int64, duplicate bool) {
	pid := tsPacket.Pid()
	if a.bitrate != nil {
		a.bitrate.CountPacket(pid)
	}
	pes, exist := a.pesMap[pid]
	// PCR_PID 0 (unset) and 0x1FFF ("no PCR") are not real carrier PIDs, so no
	// PCR-based analysis (bitrate, jitter, timestamps) runs against them.
	// The PCR of a packet with a transport error is not trusted; a scrambled
	// packet's adaptation field is clear, so its PCR is used.
	pcrCarrier := a.pcrPid != 0 && a.pcrPid != nullPidValue && pid == a.pcrPid && !tsPacket.transportError() && tsPacket.HasAf()
	if pcrCarrier && !tsPacket.adaptationField.PcrFlag() && tsPacket.adaptationField.DiscontinuityIndicator() {
		a.pcrDiscontinuityPending = true
	}
	if pcrCarrier && tsPacket.adaptationField.PcrFlag() {
		a.observePcr(tsPacket, pos)
	}
	if !exist || duplicate {
		return
	}
	if tsPacket.payloadUntrusted() {
		// The payload is not used. It may continue the PES packet in progress
		// or start the next one, so the PES in progress ends here and is parsed
		// from the trusted packets received so far (a header they leave
		// incomplete fails to parse, so nothing is read from later packets).
		// The PID then waits for its next unaffected payload_unit_start.
		if pes != nil {
			a.completePes(pes, false)
			a.pesMap[pid] = nil
		}
		a.breakPES(pid, pos, analysis.BreakUntrustedPayload)
		return
	}
	if pes == nil && !tsPacket.PayloadUnitStartIndicator() {
		return
	}
	// Skip packets without payload (e.g., AF-only packets with afc=2).
	// Per MPEG-2 spec, these do not increment continuity_counter.
	if len(tsPacket.Payload()) == 0 {
		return
	}

	if tsPacket.PayloadUnitStartIndicator() {
		if pes != nil {
			a.completePes(pes, true)
		} else {
			pes = NewPes()
			a.pesMap[pid] = pes
		}
		pes.initialize(pid, pos, a.clock)
		pes.Append(tsPacket.Payload()) // read until pointer_field
	} else {
		pes.Append(tsPacket.Payload())
	}
}

// observePcr feeds the PCR of tsPacket, a trusted packet of the PCR_PID at
// pos, to every PCR-based result. analysis.PCRCheck classifies it once against
// the previous PCR, and the bitrate time base, the time series and the PCR-PTS
// bracket of the active PES packets follow that classification; the jitter
// estimate classifies its stored samples with the same policy.
func (a *programAnalyzer) observePcr(tsPacket *TsPacket, pos int64) {
	pid := tsPacket.Pid()
	pcr := tsPacket.Pcr()
	declared := tsPacket.adaptationField.DiscontinuityIndicator() || a.pcrDiscontinuityPending
	pidChanged := a.pcrPidChanged
	a.pcrDiscontinuityPending, a.pcrPidChanged = false, false
	step := a.pcr.Observe(analysis.PCR{PID: pid, Offset: pos, Value: pcr, DiscontinuityIndicator: declared, PCRPIDChanged: pidChanged})
	if a.onPcr != nil {
		a.onPcr(pos, step)
	}
	sample := pcrSample{pos: pos, pid: pid, pcr: pcr, discontinuity: declared, pidChanged: pidChanged}
	if a.bitrate != nil {
		a.bitrate.markPcrStep(sample, step)
	}
	if a.options.DumpTimestamp {
		tsPacket.adaptationField.DumpPcr(a.lastPcr)
	}
	if a.collectJitter {
		a.pcrJitter.add(sample)
	}
	// A PCR that the PCR check does not measure against the previous one
	// starts a new segment of the timeline that places PES packets: no
	// PCR-PTS/DTS gap spans it. A wraparound step continues the segment.
	a.clock = nextPcrAnchor(a.clock, pcr, pos, step.Comparable())
	a.pes.release(a.clock)
	// Complete the PCR bracket of every active PES, including short PES
	// packets which have no continuation packet after this PCR.
	for _, activePes := range a.pesMap {
		if activePes != nil && !activePes.nextAnchor.valid {
			activePes.nextAnchor = a.clock
		}
	}
	if a.series != nil {
		a.series.addPcr(pos, pcr, step)
	}
	a.lastPcr = pcr
}

// completePes parses a buffered PES, checks its header and records its
// timestamps. next is true when the PES ended because the next PES of the PID
// started; otherwise the data ended (untrusted payload, a layout change or the
// end of the range), and a header that is not complete yet is not a problem of
// the stream but a header that could not be checked. Only a header that passed
// every check feeds the timestamp checks.
func (a *programAnalyzer) completePes(pes *Pes, next bool) {
	perr := pes.Parse()
	if a.options.DumpTimestamp {
		pes.DumpTimestamp()
	}
	if a.options.DumpPesHeader {
		pes.DumpHeader()
	}
	a.observePES(pes, pesProblem(perr), next)
	if perr != nil {
		a.anomaly.markUntimed(pes.pid)
		return
	}
	a.anomaly.Check(pes)
	delay, bracketed := pes.BracketedTimestampDelay()
	if bracketed {
		if a.pcrPtsSamples == 0 || delay > a.maxDelay {
			a.maxDelay = delay
		}
		a.pcrPtsSamples++
	}
	if a.series != nil {
		a.series.addPes(pes, delay, bracketed)
	}
}

// finish parses the final PES of each elementary stream, which never sees a
// following PUSI, rather than dropping it at the end of the input (best effort).
func (a *programAnalyzer) finish() {
	pids := make([]uint16, 0, len(a.pesMap))
	for pid := range a.pesMap {
		pids = append(pids, pid)
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	for _, pid := range pids {
		pes := a.pesMap[pid]
		if pes == nil || len(pes.buf) == 0 {
			continue
		}
		a.completePes(pes, false)
	}
	// No PCR follows: the PES packets still waiting for one are placed by
	// extrapolation.
	a.pes.release(pcrAnchor{})
}

// setLayout switches the analysis to a new PMT version (or a PMT moved to
// another PID) from the next packet on. Elementary streams that were removed
// or changed stream_type have their buffered PES finalized now, as at the end
// of the input, and their PES and timestamp state dropped; added and retyped
// streams start from their next payload_unit_start_indicator with fresh
// timestamp state. A PCR_PID change ends the PCR timing segment: PES packets
// that have no following PCR yet lose their preceding one, so no PCR-PTS/DTS
// gap mixes the two PCR PIDs.
func (a *programAnalyzer) setLayout(pmtPid, pcrPid uint16, programInfos []ProgramInfo) {
	types := make(map[uint16]uint8, len(programInfos))
	for _, info := range programInfos {
		types[info.elementaryPid] = info.streamType
	}
	for _, info := range a.programInfos {
		if streamType, kept := types[info.elementaryPid]; kept && streamType == info.streamType {
			continue
		}
		if pes := a.pesMap[info.elementaryPid]; pes != nil && len(pes.buf) > 0 {
			a.completePes(pes, false)
		}
		delete(a.pesMap, info.elementaryPid)
		delete(a.anomaly.states, info.elementaryPid)
	}
	for _, info := range programInfos {
		if _, exist := a.pesMap[info.elementaryPid]; !exist {
			a.pesMap[info.elementaryPid] = nil
			delete(a.anomaly.states, info.elementaryPid)
		}
	}
	a.programInfos = programInfos
	if pcrPid != a.pcrPid {
		a.pcrPid, a.pcrDiscontinuityPending, a.pcrPidChanged = pcrPid, false, true
		a.lastPcr = 0
		a.clock = pcrAnchor{segment: a.clock.segment}
		for _, pes := range a.pesMap {
			if pes != nil && !pes.nextAnchor.valid {
				pes.prevAnchor = pcrAnchor{}
			}
		}
	}
	a.pes.release(pcrAnchor{})
	a.pes.check.ObserveProgramLayout(a.pes.program, programLayout(pmtPid, pcrPid, programInfos))
	if a.bitrate != nil {
		a.bitrate.setLayout(pmtPid, pcrPid, programInfos)
	}
}

// stop ends the analysis of the program after the packet at pos, when the
// program is no longer listed in the PAT. Buffered PES packets are finalized.
func (a *programAnalyzer) stop(end int64) {
	a.finish()
	clear(a.pesMap)
	a.stopped, a.end = true, end
}

func (a *programAnalyzer) complianceReport() complianceReport {
	return newComplianceReport(a.pcr.Interval(), a.pcr.Discontinuities(), a.maxDelay, a.pcrPtsSamples)
}

func crc32(buf []byte) uint32 {
	table := []uint32{
		0x00000000, 0x04c11db7, 0x09823b6e, 0x0d4326d9,
		0x130476dc, 0x17c56b6b, 0x1a864db2, 0x1e475005,
		0x2608edb8, 0x22c9f00f, 0x2f8ad6d6, 0x2b4bcb61,
		0x350c9b64, 0x31cd86d3, 0x3c8ea00a, 0x384fbdbd,
		0x4c11db70, 0x48d0c6c7, 0x4593e01e, 0x4152fda9,
		0x5f15adac, 0x5bd4b01b, 0x569796c2, 0x52568b75,
		0x6a1936c8, 0x6ed82b7f, 0x639b0da6, 0x675a1011,
		0x791d4014, 0x7ddc5da3, 0x709f7b7a, 0x745e66cd,
		0x9823b6e0, 0x9ce2ab57, 0x91a18d8e, 0x95609039,
		0x8b27c03c, 0x8fe6dd8b, 0x82a5fb52, 0x8664e6e5,
		0xbe2b5b58, 0xbaea46ef, 0xb7a96036, 0xb3687d81,
		0xad2f2d84, 0xa9ee3033, 0xa4ad16ea, 0xa06c0b5d,
		0xd4326d90, 0xd0f37027, 0xddb056fe, 0xd9714b49,
		0xc7361b4c, 0xc3f706fb, 0xceb42022, 0xca753d95,
		0xf23a8028, 0xf6fb9d9f, 0xfbb8bb46, 0xff79a6f1,
		0xe13ef6f4, 0xe5ffeb43, 0xe8bccd9a, 0xec7dd02d,
		0x34867077, 0x30476dc0, 0x3d044b19, 0x39c556ae,
		0x278206ab, 0x23431b1c, 0x2e003dc5, 0x2ac12072,
		0x128e9dcf, 0x164f8078, 0x1b0ca6a1, 0x1fcdbb16,
		0x018aeb13, 0x054bf6a4, 0x0808d07d, 0x0cc9cdca,
		0x7897ab07, 0x7c56b6b0, 0x71159069, 0x75d48dde,
		0x6b93dddb, 0x6f52c06c, 0x6211e6b5, 0x66d0fb02,
		0x5e9f46bf, 0x5a5e5b08, 0x571d7dd1, 0x53dc6066,
		0x4d9b3063, 0x495a2dd4, 0x44190b0d, 0x40d816ba,
		0xaca5c697, 0xa864db20, 0xa527fdf9, 0xa1e6e04e,
		0xbfa1b04b, 0xbb60adfc, 0xb6238b25, 0xb2e29692,
		0x8aad2b2f, 0x8e6c3698, 0x832f1041, 0x87ee0df6,
		0x99a95df3, 0x9d684044, 0x902b669d, 0x94ea7b2a,
		0xe0b41de7, 0xe4750050, 0xe9362689, 0xedf73b3e,
		0xf3b06b3b, 0xf771768c, 0xfa325055, 0xfef34de2,
		0xc6bcf05f, 0xc27dede8, 0xcf3ecb31, 0xcbffd686,
		0xd5b88683, 0xd1799b34, 0xdc3abded, 0xd8fba05a,
		0x690ce0ee, 0x6dcdfd59, 0x608edb80, 0x644fc637,
		0x7a089632, 0x7ec98b85, 0x738aad5c, 0x774bb0eb,
		0x4f040d56, 0x4bc510e1, 0x46863638, 0x42472b8f,
		0x5c007b8a, 0x58c1663d, 0x558240e4, 0x51435d53,
		0x251d3b9e, 0x21dc2629, 0x2c9f00f0, 0x285e1d47,
		0x36194d42, 0x32d850f5, 0x3f9b762c, 0x3b5a6b9b,
		0x0315d626, 0x07d4cb91, 0x0a97ed48, 0x0e56f0ff,
		0x1011a0fa, 0x14d0bd4d, 0x19939b94, 0x1d528623,
		0xf12f560e, 0xf5ee4bb9, 0xf8ad6d60, 0xfc6c70d7,
		0xe22b20d2, 0xe6ea3d65, 0xeba91bbc, 0xef68060b,
		0xd727bbb6, 0xd3e6a601, 0xdea580d8, 0xda649d6f,
		0xc423cd6a, 0xc0e2d0dd, 0xcda1f604, 0xc960ebb3,
		0xbd3e8d7e, 0xb9ff90c9, 0xb4bcb610, 0xb07daba7,
		0xae3afba2, 0xaafbe615, 0xa7b8c0cc, 0xa379dd7b,
		0x9b3660c6, 0x9ff77d71, 0x92b45ba8, 0x9675461f,
		0x8832161a, 0x8cf30bad, 0x81b02d74, 0x857130c3,
		0x5d8a9099, 0x594b8d2e, 0x5408abf7, 0x50c9b640,
		0x4e8ee645, 0x4a4ffbf2, 0x470cdd2b, 0x43cdc09c,
		0x7b827d21, 0x7f436096, 0x7200464f, 0x76c15bf8,
		0x68860bfd, 0x6c47164a, 0x61043093, 0x65c52d24,
		0x119b4be9, 0x155a565e, 0x18197087, 0x1cd86d30,
		0x029f3d35, 0x065e2082, 0x0b1d065b, 0x0fdc1bec,
		0x3793a651, 0x3352bbe6, 0x3e119d3f, 0x3ad08088,
		0x2497d08d, 0x2056cd3a, 0x2d15ebe3, 0x29d4f654,
		0xc5a92679, 0xc1683bce, 0xcc2b1d17, 0xc8ea00a0,
		0xd6ad50a5, 0xd26c4d12, 0xdf2f6bcb, 0xdbee767c,
		0xe3a1cbc1, 0xe760d676, 0xea23f0af, 0xeee2ed18,
		0xf0a5bd1d, 0xf464a0aa, 0xf9278673, 0xfde69bc4,
		0x89b8fd09, 0x8d79e0be, 0x803ac667, 0x84fbdbd0,
		0x9abc8bd5, 0x9e7d9662, 0x933eb0bb, 0x97ffad0c,
		0xafb010b1, 0xab710d06, 0xa6322bdf, 0xa2f33668,
		0xbcb4666d, 0xb8757bda, 0xb5365d03, 0xb1f740b4,
	}

	var crc uint32 = 0xffffffff
	for _, val := range buf {
		crc = (crc << 8) ^ table[((crc>>24)^uint32(val))&0xff]
	}
	return crc
}
