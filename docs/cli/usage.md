# CLI usage

[Documentation home](../index.md)

The command-line analyzer parses TS packets and checks whether the stream conforms to the following requirements defined in the specification:

- **Max PCR interval** should be no greater than 100 ms (ISO/IEC 13818-1, Section 2.7.2); intervals are measured only between comparable PCRs, and a PCR wraparound is a normal interval (see [Timing compliance verdicts](#timing-compliance-verdicts-and---fail-on-error))
- **PCR discontinuities** must be declared with `discontinuity_indicator` (ISO/IEC 13818-1, Section 2.4.3.5); a PCR that jumps backward, or more than 1000 ms forward, without it is reported (ETSI TR 101 290 PCR_discontinuity_indicator_error)
- **PCR-PTS/DTS max gap** (decode delay) should be no greater than 1000 ms; DTS is used when both timestamps are present, otherwise PTS is used
- **PTS/DTS anomalies** — timestamps going backward, 33-bit wraparound, large forward jumps (splicing/ad insertion), or DTS later than PTS — are detected and reported (always on; silent when the stream is healthy)

In addition, it can dump various MPEG-2 TS internal structures for stream investigation:

- TS header and payload
- Adaptation field (including PCR)
- PSI tables (PAT/PMT) with CRC32 validation, assembled across every section of the table
- PAT/PMT version changes during the analysis (moved PMT or PCR PID, added, removed or retyped elementary streams), followed and reported with their byte offsets
- PSI layout of an analyzed range (`--output analysis`): the PAT/PMT sections seen, the layout in effect over each byte interval, and every PID observed with what it carries
- PMT ES info descriptors (ISO 639 language, registration, AVC/HEVC video, AAC audio, teletext, DVB subtitling)
- PES header with PTS/DTS timestamps
- PCR jitter analysis (per-interval deviation from the expected PCR, estimated from byte positions and judged only when the stream shows constant delivery)
- `continuity_counter` validation with an always-on per-PID error summary
- TS synchronization: an always-on check that reports packets without their sync byte and continues from the next confirmed alignment (see [TS synchronization](#ts-synchronization-always-on))
- Transport errors (`transport_error_indicator`) and scrambled packets (`transport_scrambling_control`), counted per PID with an always-on summary; their payload (and the PCR of a packet with a transport error) is kept out of the analysis

Both 188-byte TS packets and 192-byte M2TS packets (BDAV format with TP_extra_header) are supported. The packet size is auto-detected from the stream.

**Note:** The accuracy of the output is not guaranteed.

## Command line

By default, mpeg-ts-analyzer prints deterministic `OK`, `NG`, or `SKIPPED`
verdicts for the maximum PCR interval and PCR-to-PTS/DTS gap. Individual PCR,
PTS, and DTS lines remain opt-in through `--dump-timestamp`.

```
Usage:
  mpeg-ts-analyzer [input file path] [flags]

Flags:
      --capabilities            Print the supported machine-readable outputs as JSON and exit (no input file).
      --count int               With --output packets: maximum number of packets in the page. (default 100)
      --dump-adaptation-field   Dump TS packet adaptation_field detail.
      --dump-bitrate            Summarize per-PID average/peak bitrate (PCR time base) for the analyzed program.
      --dump-pcr-jitter         Analyze PCR jitter (per-interval deviation from the expected PCR).
      --dump-pes-header         Dump PES packet header detail.
      --dump-psi                Dump PSI(PAT/PMT) detail.
      --dump-timestamp          Dump PCR/PTS/DTS timestamps.
      --dump-ts-header          Dump TS packet header.
      --dump-ts-payload         Dump TS packet payload binary.
      --fail-on-error           Exit with code 2 when a timing compliance check reports NG.
      --format string           Output format: text or json. (default "text")
  -h, --help                    help for mpeg-ts-analyzer
      --limit int               Stop reading after this many bytes (0 = no limit).
      --list-programs           List every program (program_number, PMT PID, service name, elementary streams) and exit.
      --offset int              Start reading from this byte offset.
      --output string           JSON output: summary, analysis (every program with time series), packets (a page of packets) or packet (one packet decoded); needs --format json. (default "summary")
      --packet-size int         With --output packets or packet: packet size 188 or 192 (0 = detect).
      --program int             Analyze only this program_number (default: the sole program; a multi-program stream is listed instead).
      --progress string         Write progress events to stderr in this format: json.
      --range-limit int         With --output packet: length of that scan range in bytes (0 = to the end of the file).
      --range-offset int        With --output packet: start of the scan range searched for the packet's PES or section.
      --version                 show mpeg-ts-analyzer version.
```

## Flag reference

| Flag | Default | Purpose |
| --- | --- | --- |
| `--format` | `text` | Select human-readable text or aggregate JSON output. |
| `--dump-ts-header` | off | Dump each TS packet header. |
| `--dump-ts-payload` | off | Dump TS packet payload bytes. |
| `--dump-adaptation-field` | off | Dump adaptation-field flags and values. |
| `--dump-psi` | off | Dump PAT and PMT details. |
| `--dump-pes-header` | off | Dump PES header details. |
| `--dump-timestamp` | off | Dump PCR, PTS, and DTS observations. |
| `--dump-pcr-jitter` | off | Add the PCR jitter analysis. |
| `--dump-bitrate` | off | Add the per-PID bitrate summary. |
| `--fail-on-error` | off | Exit 2 when a timing compliance check is `NG`. |
| `--list-programs` | off | List programs and stop before PES analysis. |
| `--program` | automatic | Select one program by `program_number`. |
| `--offset` | `0` | Start reading at a byte offset. Negative values are rejected. |
| `--limit` | `0` (unlimited) | Stop after the specified number of bytes. Negative values are rejected. |
| `--output` | `summary` | With `--format json`, select the summary report, the `analysis` of every program with time series, a page of `packets`, or one decoded `packet`. |
| `--count` | `100` | With `--output packets`, the maximum number of packets in the page. |
| `--packet-size` | `0` (detect) | With `--output packets` or `packet`, force 188 or 192. |
| `--range-offset` | `0` | With `--output packet`, start of the scan range searched for the packet's PES or section. Negative values are rejected. |
| `--range-limit` | `0` (to the end) | With `--output packet`, length of that scan range. Negative values are rejected. |
| `--progress` | off | Write progress events to stderr (`json`). |
| `--capabilities` | off | Print the supported machine-readable outputs as JSON and exit. |

The standard Cobra `--help` and `--version` flags are also available. Detailed
examples for the analysis and dump modes follow.

## No options

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Input file:  sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Packet size: 188 bytes
Detected PAT: 1 program(s)
Detected PMT
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x02 (13818-2 video or 11172-2 constrained parameter video stream)
PMT : Program Info : elementary_PID     : 0x101, stream_type : 0x03 (11172 audio)
-----------------------------
TS Sync: no sync loss detected
-----------------------------
Compliance Check Results:
Max PCR interval: 80.000000ms [OK, limit: <= 100.000000ms]
PCR discontinuities: 0 undeclared in 60 PCRs [OK]
PCR-PTS/DTS max gap: 726.666667ms [OK, limit: <= 1000.000000ms]
-----------------------------
Continuity Counter: no errors detected
-----------------------------
Transport Error Indicator: no packets with transport_error_indicator set
Transport Scrambling: no scrambled packets
```

## Timing compliance verdicts and `--fail-on-error`

The maximum PCR interval, PCR discontinuity and PCR-to-PTS/DTS gap verdicts
are always printed. The gap uses DTS when both PTS and DTS are present,
otherwise PTS. It measures the decode timeline rather than the presentation
delay of reordered frames. The limits are inclusive, so exactly 100 ms and
1000 ms are `OK`. When there are fewer than two comparable PCR observations,
fewer than two PCRs at all, or no parsed PTS/DTS bracketed by PCR observations,
the corresponding result is explicit and non-failing:

```text
-----------------------------
Compliance Check Results:
Max PCR interval: SKIPPED (need at least two comparable PCR observations)
PCR discontinuities: SKIPPED (need at least two PCR observations)
PCR-PTS/DTS max gap: SKIPPED (need a parsed PTS/DTS bracketed by PCR observations)
```

### PCR steps

Every PCR of the selected program is compared with the previous one, and every
PCR-based result uses the same classification:

| Step | When | Interval measured | New timing segment | PCR discontinuities |
| --- | --- | --- | --- | --- |
| first | The first PCR of the analyzed range (range acquisition) | no | — | not counted |
| `continuous` | 0 to 1000 ms later, same PID, no `discontinuity_indicator` | yes | no | not listed |
| `wraparound` | Smaller, but 0 to 1000 ms later counted across the 2^33 × 300 wrap | yes | no | listed, legal |
| `declared` | `discontinuity_indicator` on the PCR packet, or on an earlier packet of the `PCR_PID` that carried no PCR (the next PCR then starts the new time base) | no | yes | listed, legal |
| `pcr_pid_change` | A PMT version changed the `PCR_PID` since the previous PCR, even if a later version changed it back before a PCR arrived on the other PID | no | yes | listed, legal |
| `backward` | Otherwise smaller, without `discontinuity_indicator` | no | yes | **undeclared** |
| `forward_jump` | More than 1000 ms later, without `discontinuity_indicator` | no | yes | **undeclared** |

- **Max PCR interval** is the largest `continuous` or `wraparound` interval,
  judged against 100 ms (ISO/IEC 13818-1, 2.7.2; ETSI TR 101 290 V1.4.1
  indicator 2.3a, which withdrew the earlier 40 ms DVB recommendation). A file
  has no arrival times, so the interval is the difference of the PCR values.
- **PCR discontinuities** is `NG` when at least one step is `backward` or
  `forward_jump`: the time base changed without the `discontinuity_indicator`
  that ISO/IEC 13818-1, 2.4.3.5 requires (ETSI TR 101 290 indicator 2.3b,
  PCR_discontinuity_indicator_error). Declared discontinuities, `PCR_PID`
  changes and wraparounds are listed with their first offset but do not fail.
- TR 101 290 2.3b reports any undeclared difference outside 0 to 100 ms. A
  forward step between 100 ms and 1000 ms is instead measured as an interval
  and fails Max PCR interval: in a file it cannot be told apart from a PCR that
  genuinely arrived late (2.3a). Only a step that no late PCR explains, more
  than ten times the limit, is a `forward_jump`.
- A new timing segment also restarts the PCR jitter estimate, freezes the
  bitrate clock and the elapsed time of `--output analysis` series, and ends
  the PCR bracket of the PES packets in progress, so no PCR-to-PTS/DTS gap is
  measured across it. A `wraparound` keeps all of them running, but a PES
  packet whose PCRs straddle the wrap gets no gap.

For example, the intentionally invalid
`sample_data/sample_188byte_pcr_discontinuities.ts` passes through every kind:

```text
Max PCR interval: 40.000000ms [OK, limit: <= 100.000000ms]
PCR discontinuities: 2 undeclared in 127 PCRs [NG]
  - wraparound    : 1, first at 0x000024b8 (PCR jumped +40.000ms)
  - declared      : 2, first at 0x00003ac0 (PCR jumped +5040.000ms)
  - pcr_pid_change: 1, first at 0x00009458 (PCR jumped +40.000ms)
  - backward      : 1, first at 0x000066d0 (PCR jumped -1960.000ms)
  - forward_jump  : 1, first at 0x00007cd8 (PCR jumped +4040.000ms)
```

The jump of a wraparound is the interval counted across the wrap. See
[PCR timing results](../reference/json-output.md#pcr-timing-results) for how
each PCR-based result uses these steps.

`--fail-on-error` is intended for CI. It preserves all normal output and exits
with code 2 if any evaluated check (max PCR interval, PCR discontinuities or
PCR-to-PTS/DTS gap) is `NG`. Healthy and insufficient-data
runs exit 0; usage, input, and parsing errors use exit 1. The PCR jitter
estimate of `--dump-pcr-jitter` does not take part, whatever its status
(see [Dump PCR jitter](dumps.md#dump-pcr-jitter)).

```sh
mpeg-ts-analyzer input.ts --fail-on-error
```

The timing checks use inclusive limits: exactly 100 ms for the maximum PCR
interval and exactly 1000 ms for the PCR-to-PTS/DTS gap are `OK`. A check is
`SKIPPED` when there are not enough observations; `SKIPPED` does not
make `--fail-on-error` fail. Exit codes are:

- `0`: parsing completed and no evaluated timing check reported `NG`
- `1`: usage, input, or parsing error
- `2`: parsing completed but `--fail-on-error` found one or more `NG` checks

Example default result:

```text
-----------------------------
Compliance Check Results:
Max PCR interval: 80.000000ms [OK, limit: <= 100.000000ms]
PCR discontinuities: 0 undeclared in 60 PCRs [OK]
PCR-PTS/DTS max gap: 726.666667ms [OK, limit: <= 1000.000000ms]
```

## Multi-program transport streams

A single-program stream (a recording or an extracted service) is analyzed
directly. A **multi-program** stream — e.g. a raw broadcast capture (ISDB-T
carries a main service plus 1seg; BS/CS muxes carry many channels) — is not
analyzed program-by-program by default: the tool lists the programs and stops,
so you can pick one.

```
$ ./mpeg-ts-analyzer capture.ts
Detected PAT: 2 program(s)
Multiple programs detected; pass --program <program_number> to analyze one:
Program 1: PMT PID 0x1000, PCR PID 0x0100
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x1b (AVC video ...)
PMT : Program Info : elementary_PID     : 0x110, stream_type : 0x0f (AAC audio ...)
Program 2: PMT PID 0x1001, PCR PID 0x0110
PMT : Program Info : elementary_PID     : 0x120, stream_type : 0x02 (1seg video ...)

$ ./mpeg-ts-analyzer capture.ts --program 1     # analyze one program fully
$ ./mpeg-ts-analyzer capture.ts --list-programs # list only the programs, even when there is just one
```

Listing only parses each program's PMT (no PES analysis), so it stays lightweight.
When the stream names its services in a DVB SDT (as FFmpeg does), the listing
adds a `Service:` line with the service and provider name, and JSON output adds
`service` to each program; see [Service names](psi.md#service-names-sdt).

PAT and PMT tables split into several sections are collected completely before
programs are listed or selected, and programs are listed in `program_number`
order. Missing sections, inconsistent section sets, and invalid CRCs are
reported as warnings instead of producing a partial table; see
[Multi-section PAT and PMT](psi.md#multi-section-pat-and-pmt).

Long captures can update the PAT or PMT while they are analyzed. The analyzer
keeps collecting both tables and applies a new version once all its sections
have arrived: a moved PMT is read from its new PID, a new `PCR_PID` starts a new
PCR timing segment (a `pcr_pid_change` step: no interval, jitter, bitrate
window or PCR-to-PTS/DTS gap spans the change),
and elementary streams that are added, removed or change `stream_type` start
or stop cleanly. Each applied change is printed with its offset in text mode
and listed under `psi_transitions` in JSON; a program removed from the PAT
stops being analyzed there. `--output analysis` also reports each layout with
the byte interval it applies to (`psi_layout`). See
[PAT and PMT version changes](psi.md#pat-and-pmt-version-changes).

## Continuity counter summary (always on)

A TS-layer tracker checks every non-null PID from the start of the selected
program's PES analysis through the end of the analysis window. This includes
repeated PSI/SI packets, unreferenced PIDs, and PIDs belonging to other programs.
Checking is stream-wide within this window: `--program` does not filter the
checked PIDs. PID `0x1FFF` null packets are
excluded because their continuity counters are not required to form a sequence.
Every violation is printed inline at the byte position where it is detected. A
per-PID summary is always appended at the end so errors remain visible even when
the analysis produces a large amount of output:

```
packet loss. : pid=0x0100. count=0x5, pos=0x00123456
packet loss. : pid=0x0100. count=0x9, pos=0x00234567
packet loss. : pid=0x0101. count=0x3, pos=0x00345678
-----------------------------
Continuity Counter Error Summary:
  PID 0x0100 (video) : 2 errors
  PID 0x0101 (audio) : 1 error
  Total              : 3 errors in 2 PIDs
```

Counters advance modulo 16 only on packets whose header declares payload.
Adaptation-field-only packets keep the current counter. A duplicate TS
packet with the same counter is accepted and is not appended to PES data twice.
Only the PCR value may differ; other header, adaptation-field and payload bytes
must match. PCR observations are still processed, including refreshed PCRs on
duplicates, and duplicates still count toward bitrate packet totals.
A packet carrying `discontinuity_indicator` starts a new counter segment, and a
PUSI packet follows the same rules as any other payload packet. After an
undeclared mismatch, tracking resumes from the received counter so one fault
produces one event instead of cascading warnings.

The summary count is the number of discontinuity events, not an estimate of the
number of missing transport packets. Known PAT, PMT, PCR, video, and audio PIDs
receive labels; any other included PID is labeled `unknown`.

## TS synchronization (always on)

Every packet of the analysis must start with the sync byte `0x47` (for
192-byte M2TS input, the fifth byte, after the 4-byte `TP_extra_header`). When
a packet boundary has another value, synchronization is lost there. The
analyzer then looks for the next offset at which **five** consecutive packets,
one packet size apart, start with `0x47`, and continues from there. The bytes
in between are skipped: they are never read as packets, so they cannot add PIDs,
PCRs or timestamps. A single `0x47` inside a payload, or fewer than five in a
row, is not accepted. The packet size does not change during the search. Five
is the reacquisition count that monitors of the ETSI TR 101 290 `TS_sync_loss`
indicator commonly use; it is a fixed policy of this analyzer, not a claim of
TR 101 290 conformance. TR 101 290 also distinguishes a single corrupted sync
byte (`Sync_byte_error`) from a loss of synchronization; here every packet
boundary without a sync byte counts as a loss, and a corrupted sync byte with an
unchanged alignment skips that one packet.

The search is bounded: a new alignment must start within 1,048,576 bytes
(1 MiB) of the lost boundary. Each byte is examined at most once, the search
keeps one fixed 64 KiB buffer, and the result is a fixed set of counters and
offsets, so neither time nor memory grows with the corrupt span or with
repeated corruption. When no alignment is found, because the limit is reached
or the input or `--limit` range ends first (fewer than five aligned packets
remain), the analysis stops at the lost boundary. Results up to there are kept,
and JSON reports `status: "incomplete"` with `reason: "sync_lost"`.

Each loss is printed inline, and a summary always appears before the timing
verdicts:

```
0x00001d60 sync lost (found 0x00 instead of 0x47); resynchronized at 0x00001e1c after skipping 188 bytes
0x00002d88 sync lost (found 0xff instead of 0x47); resynchronized at 0x00002d89 after skipping 1 bytes
0x00003c39 sync lost (found 0x41 instead of 0x47); resynchronized at 0x00003cf4 after skipping 187 bytes
0x000058dc sync lost (found 0x00 instead of 0x47); resynchronized at 0x00005cc4 after skipping 1000 bytes
0x000096c8 ignored 100 trailing bytes (incomplete packet at end of input)
-----------------------------
TS Sync Summary: recovered
  Sync losses   : 4 (first at 0x00001d60)
  Resyncs       : 4 (first at 0x00001e1c)
  Skipped bytes : 1376 (largest 1000 at 0x000058dc)
```

That is `sample_data/sample_188byte_sync_loss.ts`: a corrupted sync byte (one
packet skipped), one inserted byte, one deleted byte (the rest of the next
packet skipped) and a 1000-byte block of garbage with four false sync bytes.
All offsets are absolute positions in the input, also after a recovery. A
healthy stream prints `TS Sync: no sync loss detected`. An unrecovered loss adds
a line such as
`Not recovered : at 0x00009494, the input ended before 5 aligned sync bytes; analysis stopped`.

Packets lost to a skipped span usually also cause continuity-counter errors on
their PIDs, and timing values measured across the span (PCR intervals, the PCR
jitter estimate, which uses byte positions) include its effect.

Other cases are reported separately:

- **No alignment at all.** When the input has no three consecutive sync bytes
  one packet apart, there is no transport stream to analyze. The analysis
  fails with `no TS synchronization found` (JSON `reason:
  "sync_not_acquired"`), distinct from aligned packets without a PAT.
- **Before the analysis starts.** The check covers the analyzed packets, from
  the end of the first complete PAT and PMT on. A packet without a sync byte
  while those tables are collected restarts the PAT search, with a warning, as
  before.
- **A packet with its sync byte but an unparseable header** (for example a
  reserved `adaptation_field_control`) is aligned, so it is not a loss of
  synchronization. It still interrupts the analysis, which restarts at the
  next PAT with a `PES parse error` warning, as described under
  [Transport errors and scrambled packets](#transport-errors-and-scrambled-packets-always-on).
- **Trailing partial packet.** Fewer than one packet of bytes at the end of the
  input is an incomplete packet, not a loss: it is reported as a warning
  (`ignored N trailing bytes`) and in the JSON `sync` object, and does not
  change the status.

`--fail-on-error` and the exit status do not depend on this check.

## Transport errors and scrambled packets (always on)

Two always-on checks count, per PID, the packets whose
`transport_error_indicator` is set (a demodulator or other upstream device
marks a packet with an uncorrectable bit error this way) and the packets whose
`transport_scrambling_control` is not zero (a payload encrypted by conditional
access). They cover every packet of the analyzed range, from its first packet,
including the packets read while the first PAT and PMT were acquired, null
packets and PIDs no PMT references. A healthy stream gets one line for each:

```
-----------------------------
Transport Error Indicator: no packets with transport_error_indicator set
Transport Scrambling: no scrambled packets
```

When packets are affected, each check prints its totals, the offset of the
first affected packet and a status, then one line per PID in numeric order
(output of `sample_data/sample_188byte_transport_errors.ts`, see
[Sample data](../development/sample-data.md)):

```
-----------------------------
Transport Error Indicator: 6 packets in 5 PIDs, first at 0x00000000, 5 excluded from analysis [DATA EXCLUDED]
  PID 0x0000 (PAT) : 1 packet, first at 0x00001ca4, 1 excluded
  PID 0x0100 (video) : 2 packets, first at 0x00001608, 2 excluded
  PID 0x0101 (audio) : 1 packet, first at 0x0000125c, 1 excluded
  PID 0x1000 (PMT) : 1 packet, first at 0x00001f94, 1 excluded
  PID 0x1fff (null) : 1 packet, first at 0x00000000, 0 excluded
Transport Scrambling: 6 packets in 5 PIDs, first at 0x00000524, 5 excluded from analysis [DATA EXCLUDED]
  PID 0x0000 (PAT) : 1 packet, first at 0x00002b54, 1 excluded
  PID 0x0100 (video) : 2 packets, first at 0x000024b8, 2 excluded
  PID 0x0101 (audio) : 1 packet, first at 0x0000210c, 1 excluded
  PID 0x0200 (unknown) : 1 packet, first at 0x00000524, 0 excluded
  PID 0x1000 (PMT) : 1 packet, first at 0x00002e44, 1 excluded
```

A packet with both conditions counts in both. Offsets are those of the first
byte of the packet (for M2TS, of its `TP_extra_header`). The status is:

| Status | Meaning |
| --- | --- |
| no line of PIDs (`ok` in JSON) | No packet has the condition |
| `AFFECTED` | Packets have the condition, but none carried data the analysis reads; the other results are complete |
| `DATA EXCLUDED` | Data of at least one affected packet was left out of the analysis, so the other results are based on less data than the stream carried |

What the analysis does with an affected packet:

| Data | Transport error | Scrambled |
| --- | --- | --- |
| PAT / PMT section | The packet is ignored and the section in progress on its PID is discarded, with a warning ([PSI](psi.md#multi-section-pat-and-pmt)) | The same, when the packet has a payload |
| PES | The payload is not used. The PES packet in progress on the PID ends at the packet and is parsed from the packets before it; the PID resumes at its next unaffected `payload_unit_start_indicator`. No PES header, PTS/DTS, timestamp anomaly or PCR-PTS/DTS gap comes from the affected payload | The same, when the packet has a payload; a scrambled packet without payload changes nothing |
| PCR | Not used: intervals, jitter, bitrate windows and PCR-PTS/DTS brackets use the surrounding PCRs | Used: the header and adaptation field of a scrambled packet are not scrambled |
| Continuity counter, bitrate | Counted with the header as decoded | Counted |

An affected packet is **excluded** when it is on a PID whose data the
analysis reads at that point: the PAT, the PMT of an analyzed program, an
elementary stream of an analyzed program, or (for a transport error only) its
`PCR_PID`. A scrambled packet without payload, and an affected packet read
while the first PAT and PMT were acquired, are never excluded; the acquisition
reports what it ignored as warnings. The PID of a packet with a transport error
is the one in its own header, which the error may have changed.

The null PID `0x1FFF` is counted like any other PID and labeled `null`. Its
payload is never analyzed, so its packets are never excluded; a scrambled null
packet is itself invalid (ISO/IEC 13818-1 requires
`transport_scrambling_control` `00` there).

A PAT or PMT that arrives only in affected packets is never acquired: the run
ends with `PAT not detected` or as incomplete, never as a complete result built
from unusable tables. Neither check takes part in `--fail-on-error`. A packet
whose header cannot be parsed at all (for example a reserved
`adaptation_field_control`) still interrupts the analysis as any malformed
packet does (it restarts at the next PAT), whether or not it has a transport
error.

## Timestamp anomaly report (always on)

PTS/DTS anomalies are checked on every run, with no flag. Nothing is printed for
a healthy stream; when problems are found, a report is appended at the end:

```
-----------------------------
Timestamp Anomaly Report:
  PID 0x0100: PTS went backward at 0x00123457 (2001.000ms) after 0x00123456 (2041.000ms)
  PID 0x0100: PTS jumped forward 5.3s at 0x00234567 (7374.333ms) after 0x00123458 (2041.000ms), expected ~40ms
  PID 0x0101: PTS wrapped around (33-bit counter overflow) at 0x00345679
  PID 0x0102: DTS is later than PTS in the access unit at 0x00400000 (DTS 22.222ms > PTS 11.111ms)
```

Reading the output:

- Each line is one PID and the first place a problem was seen; repeats of the
  same problem on the same PID are collapsed with `(and N more)`.
- Checks run on the decode timeline (DTS when present, otherwise PTS), so a
  B-frame stream — whose PTS legitimately reorders — is not falsely flagged.

## Large files

For large files, dump options can produce a huge amount of output. Use `--limit` to restrict the byte range, or redirect output to a file to avoid losing the beginning of the output in your terminal scrollback:

```bash
mpeg-ts-analyzer large.ts --dump-ts-header --limit 1000000 > dump.txt
```

Memory use does not grow with the size of a PES packet. PES analysis reads only
the PES header (start code, `stream_id`, `PES_packet_length`, flags, PTS/DTS and
the other optional header fields), so for each elementary stream being analyzed
the analyzer keeps at most the first 264 bytes of the current PES packet: the
largest header ISO/IEC 13818-1 allows (6 + 3 + 255 bytes of
`PES_header_data_length`). Payload beyond that is counted but not stored. A
large video PES, a PES with `PES_packet_length` 0, or a malformed stream whose
PID never starts another PES therefore needs no more memory than a short one,
while continuity counters, PCR observations and bitrate counting still cover
every packet.
