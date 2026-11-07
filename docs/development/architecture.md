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
| `tsparser` | Packet-size detection (188-byte TS, 192-byte M2TS), synchronization and recovery, decoding of TS headers, adaptation fields, PSI (PAT, PMT, SDT) and PES headers, byte offsets, and the decision whether bytes can be parsed and trusted. It also orchestrates the single pass over the analyzed range (`scanPrograms`, `programAnalyzer`, `rangeAnalysis`) and holds the public report types and their text and JSON rendering. |
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
  4-byte `TP_extra_header`, as everywhere else in the output.
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
| Whether a packet is an exact duplicate that must not be appended to PES data twice, and how counters resynchronize after an error | `tsparser` (`continuityTracker`) |
| Whether a continuity counter error occurred | `tsparser`, reported as `analysis.ContinuityError` |
| How errors are counted per PID, how each PID is labeled, the check's status | `analysis` (`ContinuityCheck`) |
| The `continuity_counter` JSON object, the text summary, exit codes | `tsparser` report code and `cmd` |

The continuity counter summary is the first check that uses this boundary from
end to end. It was chosen because it is an existing, always-on transport check
whose output is already an aggregate with a status: moving its aggregation
proves the observation path without changing any public output. The planned
transport-error and scrambling check
([#61](https://github.com/small-teton/mpeg-ts-analyzer/issues/61)) adds public
output of its own and follows the same pattern.

Other aggregate evaluators (PCR interval, PCR jitter, PCR-PTS/DTS gap,
timestamp anomalies, bitrate) are still in `tsparser`. They move to `analysis`
when a change to them would otherwise couple a check further to the parser,
not all at once.

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
public output change.

Test the check in `internal/analysis` with observations built directly, and the
scan and report in `tsparser`. `bitbuffer`, `tsparser` and `analysis` each keep
100.0% statement coverage (`make coverage`), each through its own tests.
