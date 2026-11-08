# Architecture

This page describes how the Go code of the command-line analyzer is divided,
and where a new transport check belongs. The desktop GUI in `gui/` runs the CLI
as a subprocess and contains no analysis logic; see the
[GUI design](../gui/design.md).

## Packages

| Package | Responsibility |
| --- | --- |
| `main`, `cmd` | Command-line parsing (Cobra), flag validation and the exit status. |
| `options` | The options shared by the command and the parser. |
| `bitbuffer` | Bit-level reading of byte buffers. |
| `tsparser` | Packet-size detection (188-byte TS, 192-byte M2TS, 204-byte TS with a 16-byte trailer) and physical framing (`framing.go`: only the 188 TS packet bytes reach the parser), synchronization and recovery, decoding of TS headers, adaptation fields, PSI (PAT, PMT, SDT) and PES headers, byte offsets, and the decision whether bytes can be parsed and trusted. It also orchestrates the single pass over the analyzed range (`scanPrograms`, `programAnalyzer`, `rangeAnalysis`) and holds the public report types and their text and JSON rendering. |
| `internal/analysis` (package `analysis`) | Bounded state across observations, metrics, whether a check applies, thresholds and the status of each transport check. |

Dependencies point one way:

```text
cmd ──▶ tsparser ──▶ internal/analysis
            │
            └──────▶ bitbuffer, options
```

`analysis` imports only the standard library. It must not import `tsparser`,
`cmd` or GUI code: `tsparser` feeds it during the scan, so the reverse import
would be a cycle, and check policy would come to depend on parser types. The
test `TestImportsOnlyStandardLibrary` in `internal/analysis` fails when the
package imports anything else. The package is under `internal/` so that it is
not a public Go API while its result model evolves; the exported `tsparser`
report types, such as `AnalysisReport`, stay the public Go and JSON contract.

## Observations

The analyzer reads each range once. While `tsparser` scans it, it passes small
typed observations to `analysis`, for example the PID layout of a program when
a PMT is applied (`analysis.ProgramLayout`) or a continuity counter error
(`analysis.ContinuityError`). The rules for observations are:

- **No payload.** An observation never contains TS payload, PSI sections or PES
  bodies, nor a pointer or slice into a parser buffer. A slice in an
  observation belongs to the caller; the check copies what it keeps before
  returning.
- **Offsets** are byte offsets in the input file of the first byte of the
  packet concerned. For 192-byte M2TS input that is the first byte of the
  4-byte `TP_extra_header`, as everywhere else in the output; for 204-byte
  input it is the sync byte, since the 16-byte trailer ends the packet.
  Packet indexes count physical packets, so offsets and indexes stay
  consistent for every packet size.
- **Units** are stated on each field: PIDs are 13-bit packet identifiers,
  stream types are PMT `stream_type` values, counts are numbers of events.
- **Lifetime.** Observations are values, used during the call. A check keeps
  only aggregate state bounded by the number of PIDs (at most 8192) or by a
  documented fixed limit, never one object per packet. A result is a plain
  value computed on demand and owned by the caller.

Not every observation goes through a single structure: each check takes the
observations it needs through focused methods.

## Who decides what

| Decision | Where |
| --- | --- |
| Whether a packet is synchronized, which PID it belongs to, where its payload starts | `tsparser` |
| Where synchronization was lost, how far to search and which alignment to accept | `tsparser` (`packetReader`), reported as `analysis.SyncLoss`, `analysis.Resync` and `analysis.SyncUnrecovered` |
| How losses, skipped bytes and the first and worst offsets are aggregated, the sync status | `analysis` (`SyncCheck`) |
| When initial acquisition failed, which layouts the detector found in the range, how many packets were aligned, and what PID 0 carried (with the `psi_health` section classification) | `tsparser` (`alignmentScan`, `observeAcquisition`), reported as `analysis.AcquisitionEvidence` |
| Which prerequisite failed (`sync_not_acquired`, `insufficient_data_for_sync`, `pat_not_observed`, `pat_not_acquired`) and each layout's `found` / `not_found` / `insufficient_data` status | `analysis` (`ClassifyAcquisition`) |
| Whether a packet is an exact duplicate that must not be appended to PES data twice, and how counters resynchronize after an error | `tsparser` (`continuityTracker`) |
| Whether a continuity counter error occurred | `tsparser`, reported as `analysis.ContinuityError` |
| How errors are counted per PID, how each PID is labeled, the check's status | `analysis` (`ContinuityCheck`) |
| The `continuity_counter` JSON object, the text summary, exit codes | `tsparser` report code and `cmd` |
| Whether a packet with `transport_error_indicator` set or a scrambled payload may feed PSI, PES or PCR analysis, and whether its data was excluded | `tsparser` (`TsPacket.payloadUntrusted`, `transportTrustSummary.observe`), reported as `analysis.TransportError` / `analysis.Scrambled` |
| How much of a packet with `transport_error_indicator` set is decoded: its fixed 4-byte header only, never its adaptation field, so that a damaged packet is counted instead of failing the parse | `tsparser` (`TsPacket.Parse`, `TsPacket.parseUntrusted`) |
| How affected packets are counted per PID, labeled, and the `ok` / `affected` / `data_excluded` status | `analysis` (`TransportTrustCheck`) |
| Whether a packet of the `PCR_PID` carries a trusted PCR, and whether `discontinuity_indicator` declares a new time base for it (on the PCR packet, or on an earlier packet of the PID without a PCR) | `tsparser` (`programAnalyzer.observe`), reported as `analysis.PCR` |
| How each PCR relates to the previous one (`continuous`, `wraparound`, `declared`, `pcr_pid_change`, `backward`, `forward_jump`), the maximum PCR interval and the PCR discontinuity status | `analysis` (`PCRClock`, `PCRCheck`) |
| Where the PCR jitter estimate, the bitrate time base, the time series and the PCR-PTS/DTS bracket split into timing segments | `tsparser`, following the `analysis.PCRStep` of each PCR |
| Whether a PAT or PMT section is usable, when a table is complete, which programs the applied PAT announces, and which PCRs give the time base | `tsparser` (`psiHealthCollector`, with the same `psiSectionReader`, `psiAssembler` and trust policy as the analysis), reported as `analysis.PSIInstance`, `PSIProblem`, `PSIAnnounced`, `PSIWithdrawn`, and the reference program's `analysis.PCR` values, classified by the same `PCRClock` as `PCRCheck` |
| How packet times are interpolated from PCRs, which intervals are complete or open, the 500 ms limit and the `ok` / `ng` / `insufficient_data` status per table | `analysis` (`PSIHealthCheck`) |
| Which packets show a PID's presence (not one with `transport_error_indicator`), which PCRs give the time basis, and which PSI layout applies from which packet | `tsparser` (`pidReferenceSummary`, `referenceLayout`), reported as `analysis.Packet`, `analysis.TimedPCR` (the reference program's PCR with its `PCRStep`) and `analysis.ReferenceLayout` |
| The time of a packet between two PCRs, per-PID packet counts and gaps, silences while referenced, unreferenced runs and their grace period, the referenced and unreferenced statuses, the report-only packet-gap statuses | `analysis` (`PacketGaps`, `PIDReferenceCheck`) |
| Whether a PES header is valid and which problem it has, its PTS/DTS, and the PES packet's time on the PCR timeline (interpolated between the PCRs around it) | `tsparser` (`Pes.Parse`, `pesFeed`, `arrivalAt`), reported as `analysis.PES` and `analysis.PESBreak` |
| How header problems are counted per PID, which streams the 700 ms PTS repetition limit applies to, how intervals, breaks and lower bounds are measured, and the statuses and reasons | `analysis` (`PESCheck`) |
| The mux lead of each PES packet (DTS/PTS minus its PCR-timeline time, modulo the PCR period), its per-PID minimum, maximum and spread, the audio/video classes and the A/V lead difference; report only | `analysis` (`PESCheck.MuxLead`, from the same `analysis.PES` observations) |

The continuity counter summary is the first check that uses this boundary from
end to end. It was chosen because it is an existing, always-on transport check
whose output is already an aggregate with a status: moving its aggregation
proves the observation path without changing any public output. The
transport-error and scrambling check
([#61](https://github.com/small-teton/mpeg-ts-analyzer/issues/61)) is the first
new check built on it: the scan decides which packets it does not trust and
whether their data was excluded, and `TransportTrustCheck` aggregates the
resulting `TransportError` and `Scrambled` observations into the
`transport_errors` and `scrambled_packets` results. The PAT/PMT health check
([#161](https://github.com/small-teton/mpeg-ts-analyzer/issues/161)) follows
the same split: the scan reports complete tables, unusable sections and PCRs,
and `PSIHealthCheck` turns them into timed intervals and the `psi_health`
result.

The PCR interval and PCR discontinuity checks
([#163](https://github.com/small-teton/mpeg-ts-analyzer/issues/163)) moved to
`analysis` because every PCR-based result had its own copy of the rule that
decides when two PCRs are comparable, and the copies disagreed (one measured
across a wraparound, the others split there). `PCRClock` is now that single
rule: the scan reports each trusted PCR as an `analysis.PCR`, `PCRCheck`
returns its `PCRStep`, and the jitter estimate, bitrate and time series in
`tsparser` follow the step instead of classifying PCRs themselves.

The PID reference check
([#162](https://github.com/small-teton/mpeg-ts-analyzer/issues/162)) adds the
shared per-PID packet collector `analysis.PacketGaps`. It keeps, per PID, the
packet count, the first and last packet and the largest gap between two
consecutive packets in packets and in estimated time, and it owns the PCR clock
(`pcr_clock.go`) that turns packet positions into time: a stamp taken at a
packet is resolved at the next PCR, by interpolation between the two PCRs.
It does not classify PCRs itself: the scan passes each PCR of the reference
program with the `PCRStep` its `PCRCheck` returned (`analysis.TimedPCR`), so
time advances on `continuous` and `wraparound` steps and a new segment starts
exactly where every other PCR-based result starts one. The state waiting
for a PCR is a fixed number of stamps per PID; no per-packet history is kept. `PIDReferenceCheck` is built on it and adds the reference
windows and unreferenced runs. The report-only per-PID maximum packet gaps
([#176](https://github.com/small-teton/mpeg-ts-analyzer/issues/176)) are
presented from the same collector instead of a second per-PID history:
`PIDReferenceCheck.PacketGaps()` joins each PID of `PacketGaps.PIDs()` with
the references of the layouts and gives it the neutral status `info` (or
`insufficient_data`), and `PIDReferenceCheck.ObserveLayout` restarts a PID's
chain when a layout stops or resumes referencing it, so a removed and re-added
stream has no gap across its absence. `tsparser` only formats the
`multiplexing.packet_gaps` JSON and the text block.

The PES header and PTS repetition check
([#164](https://github.com/small-teton/mpeg-ts-analyzer/issues/164)) follows
the same split. Its `analysis.PES` observation carries one PES packet's header
outcome, PTS/DTS and PCR-timeline time without any payload, so later
timestamp checks can consume the same observation. The report-only mux lead
([#177](https://github.com/small-teton/mpeg-ts-analyzer/issues/177)) does: it
adds a fixed-size aggregate to each elementary PID of `PESCheck` and keeps,
per program, only the video and audio PIDs of the PMT in force
(`PESCheck.MuxLead()`), so no second PES or time model exists. The lead of a
PES packet is defined once, by `analysis.PES.Lead` (27 MHz ticks) and
`PES.LeadMs` (the same lead from the untruncated arrival time) in
`internal/analysis/pes_lead.go`
([#193](https://github.com/small-teton/mpeg-ts-analyzer/issues/193)): the
timestamp minus the PES packet's time on the PCR timeline (`arrivalAt`, which
interpolates across a `wraparound` step and not across a segment boundary),
read modulo the 2^33 period as the signed shortest difference. The mux lead
aggregates `Lead`; the PCR-PTS/DTS gap check, `--dump-timestamp` and the time
series in `tsparser` take `LeadMs` from the same `prevAnchor`/`nextAnchor`
bracket of each `Pes` (`Pes.BracketedTimestampDelay`). The gap check keeps its
verdict and the mux lead only reports the per-PID distribution.

Other aggregate evaluators (PCR jitter, PCR-PTS/DTS gap, timestamp anomalies,
bitrate) are still in `tsparser`. They move to `analysis` when a change to them
would otherwise couple a check further to the parser, not all at once.

## Adding a check

A new transport check states which part belongs to `tsparser`, which to
`analysis` and which to the report. For example, a check that counts scrambled
packets per PID would be split like this:

**Observation** (`internal/analysis`): a value type for what the parser can
establish, without payload.

```go
// Scrambled is a packet whose transport_scrambling_control is not zero.
// Offset is the byte offset of the packet.
type Scrambled struct {
    PID    uint16
    Offset int64
}
```

**Check** (`internal/analysis`): bounded aggregation and the status.

```go
type ScramblingCheck struct{ packets map[uint16]int }

func (c *ScramblingCheck) ObserveScrambled(o Scrambled) { c.packets[o.PID]++ }

func (c *ScramblingCheck) Result() ScramblingResult { /* status, PIDs in order */ }
```

**Scan** (`tsparser`): where the parser already decides that the packet is
scrambled and keeps its payload out of PES assembly, call `ObserveScrambled`.
The trust decision stays in `tsparser`; the check does not change what the
parser decodes.

**Report** (`tsparser` report code, `cmd`): convert the result into the text
summary and JSON fields, and update the schema and documentation as for any
public output change. Add a case for the new JSON shape to
`tsparser/json_schema_test.go`, which validates the emitted documents against
the schemas.

Test the check in `internal/analysis` with observations built directly, and the
scan and report in `tsparser`. `bitbuffer`, `tsparser` and `analysis` each keep
100.0% statement coverage (`make coverage`), each through its own tests.
