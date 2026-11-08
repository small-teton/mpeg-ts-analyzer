# CLI usage

[Documentation home](../index.md)

The command-line analyzer parses TS packets and checks whether the stream conforms to the following requirements defined in the specification:

- **Max PCR interval** should be no greater than 100 ms (ISO/IEC 13818-1, Section 2.7.2); intervals are measured only between comparable PCRs, and a PCR wraparound is a normal interval (see [Timing compliance verdicts](#timing-compliance-verdicts-and---fail-on-error))
- **PCR discontinuities** must be declared with `discontinuity_indicator` (ISO/IEC 13818-1, Section 2.4.3.5); a PCR that jumps backward, or more than 1000 ms forward, without it is reported (ETSI TR 101 290 PCR_discontinuity_indicator_error)
- **PCR-PTS/DTS max gap** (decode delay) should be no greater than 1000 ms; DTS is used when both timestamps are present, otherwise PTS is used. The gap is read modulo the 2^33 period of the PTS/DTS and the PCR as the signed shortest difference, so it stays correct whichever of the two wraps around first (see [PCR steps](#pcr-steps))
- **PTS/DTS anomalies** — timestamps going backward, 33-bit wraparound, large forward jumps (splicing/ad insertion), or DTS later than PTS — are detected and reported (always on; silent when the stream is healthy)
- **PTS repetition**: on each video and audio stream, PES packets carrying a PTS should be no more than 700 ms apart (ISO/IEC 13818-1, Section 2.7.4; ETSI TR 101 290 PTS_error), measured on the PCR timeline (see [PES headers and PTS repetition](#pes-headers-and-pts-repetition-always-on))

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
- PAT/PMT health: availability, repetition interval (500 ms, ETSI TR 101 290 1.3.a/1.5.a) and section errors of the PAT and of the PMT of every announced program, with an always-on summary
- PES header health per elementary stream (truncated headers, bad start codes, bad flags, inconsistent lengths, bad PTS/DTS marker bits), with an always-on summary; timestamps of a malformed header are not used
- PID references: an always-on check that reports PMT, PCR and elementary-stream PIDs the PSI layout references but that are absent or silent for more than 5 s, and PIDs that carry packets without being referenced (see [PID references](#pid-references-always-on))

Three physical packet sizes are supported, and the size is auto-detected from the stream:

| Packet size | Layout | Parsed |
| --- | --- | --- |
| 188 bytes | MPEG-2 TS packet | all 188 bytes |
| 192 bytes | 4-byte `TP_extra_header` (M2TS/BDAV), then the TS packet | bytes 4–191 |
| 204 bytes | TS packet, then a 16-byte trailer | bytes 0–187 |

The trailer of a 204-byte packet is commonly Reed-Solomon `RS(204,188)` parity
in broadcast systems (for example ISDB-T captures). The analyzer never parses it
as TS data and never validates or corrects it; `--output packet` lists it as raw
bytes (see [Packet detail](../reference/json-output.md#packet-detail---output-packet)).
Every offset the analyzer reports is a physical file offset of the first byte
of a physical packet, so consecutive packets are 188, 192 or 204 bytes apart.

Detection takes 188 bytes when three sync bytes (`0x47`) are 188 bytes apart
and 192 bytes when they are 192 bytes apart (the sync byte after the
4-byte header), as before 204-byte support. 204 bytes needs a stricter match:
five sync bytes 204 bytes apart, at a position that does not also start five
sync bytes 188 or 192 bytes apart, so payload bytes of a 188-byte or 192-byte
stream are not taken for 204-byte packets. A weaker coincidence, such as a
`0x47` trailer or payload byte 188 bytes after a sync byte, does not reject
204 bytes. Bitrates count 188 bytes per packet
for every packet size (see [Dump bitrate](dumps.md#dump-bitrate)).

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
      --fail-on category        Exit with code 2 when a selected category reports a finding; comma-separated or repeated: any, timing, timestamp, pes_headers, pts_repetition, continuity, transport_errors, scrambled_packets, sync, psi_health, referenced_pids, unreferenced_pids.
      --fail-on-error           Exit with code 2 when a timing compliance check reports NG (same as --fail-on timing).
      --format string           Output format: text or json. (default "text")
  -h, --help                    help for mpeg-ts-analyzer
      --limit int               Stop reading after this many bytes (0 = no limit).
      --list-programs           List every program (program_number, PMT PID, service name, elementary streams) and exit.
      --offset int              Start reading from this byte offset.
      --output string           JSON output: summary, analysis (every program with time series), packets (a page of packets) or packet (one packet decoded); needs --format json. (default "summary")
      --packet-size int         With --output packets or packet: packet size 188, 192 or 204 (0 = detect).
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
| `--dump-adaptation-field` | off | Dump adaptation-field flags and values. For a packet with `transport_error_indicator` set, the field is not parsed and the dump says `not parsed: transport_error_indicator is set`. |
| `--dump-psi` | off | Dump PAT and PMT details. |
| `--dump-pes-header` | off | Dump PES header details. |
| `--dump-timestamp` | off | Dump PCR, PTS, and DTS observations. |
| `--dump-pcr-jitter` | off | Add the PCR jitter analysis. |
| `--dump-bitrate` | off | Add the per-PID bitrate summary. |
| `--fail-on` | none | Exit 2 when a selected category reports a finding; see [CI failure policies](#ci-failure-policies---fail-on). |
| `--fail-on-error` | off | Exit 2 when a timing compliance check is `NG`; the same as `--fail-on timing`. |
| `--list-programs` | off | List programs and stop before PES analysis. |
| `--program` | automatic | Select one program by `program_number`. |
| `--offset` | `0` | Start reading at a byte offset. Negative values are rejected. |
| `--limit` | `0` (unlimited) | Stop after the specified number of bytes. Negative values are rejected. |
| `--output` | `summary` | With `--format json`, select the summary report, the `analysis` of every program with time series, a page of `packets`, or one decoded `packet`. |
| `--count` | `100` | With `--output packets`, the maximum number of packets in the page. |
| `--packet-size` | `0` (detect) | With `--output packets` or `packet`, force 188, 192 or 204. |
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
-----------------------------
PAT/PMT health: OK (repetition limit 500 ms, ETSI TR 101 290 1.3.a/1.5.a; time base: PCR PID 0x0100 of program 1)
  PAT PID 0x0000 : 43 tables, 40 intervals, max 150.222 ms ending at 0x00020388 [OK]
  PMT program 1 PID 0x1000 : 43 tables, 40 intervals, max 148.444 ms ending at 0x00020444 [OK]
-----------------------------
Referenced PIDs: 4 references, silence limit 5000 ms [OK]
Unreferenced PIDs: none [OK]
-----------------------------
PES Headers: 220 checked on 2 PIDs, no errors
PTS Repetition (limit 700ms): max interval 66.666667ms over 214 intervals [OK]
-----------------------------
Max packet gaps (report only): 5 PIDs, time basis pcr_interpolated_file_position
  PID 0x0000 : 43 packets, max gap 44 packets, 150.222 ms from 0x0001e27c to 0x00020388
  PID 0x0011 : 10 packets, max gap 173 packets, 551.111 ms from 0x00016e74 to 0x0001ee3c
  PID 0x0100 (program 1 PCR, program 1 ES stream_type 0x02) : 202 packets, max gap 16 packets, 43.871 ms from 0x0002e094 to 0x0002ed10
  PID 0x0101 (program 1 ES stream_type 0x03) : 1359 packets, max gap 10 packets, 25.778 ms from 0x000069c0 to 0x000071d4
  PID 0x1000 (program 1 PMT) : 43 packets, max gap 44 packets, 148.444 ms from 0x0001e338 to 0x00020444
-----------------------------
Mux Lead (report only: DTS, or PTS without DTS, minus PCR-interpolated arrival)
  Program 1 PID 0x0100 (video) : 691.111ms to 726.667ms (spread 35.556ms) over 118 PES, min at 0x00007c1c, max at 0x0001e3f4, 1 unbracketed
  Program 1 PID 0x0101 (audio) : 663.311ms to 688.598ms (spread 25.287ms) over 98 PES, min at 0x000056a8, max at 0x0003df68, 3 unbracketed
  Program 1 A/V lead difference: 47.016ms, audio PID 0x0101 (lead 678.867ms at 0x00007cd8) against video PID 0x0100 (lead 725.882ms at 0x00008898)
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
  measured across it. A `wraparound` keeps all of them running: a PES packet
  whose PCRs straddle the wrap gets its gap interpolated across it.
- The PTS/DTS and the PCR wrap around after the same 2^33 / 90 kHz (about
  26.5 hours) but at different points of the stream, since one leads the
  other. The PCR-to-PTS/DTS gap is therefore read modulo that period as the
  signed difference nearest to zero: a PTS/DTS that has wrapped before the PCR
  (a positive lead) and a PCR that has wrapped before the PTS/DTS (a small or
  negative lead) give the same gap as without a wrap. The gap check,
  `--dump-timestamp` and the [mux lead](#mux-lead-always-on-report-only) use
  one computation, so they give the same lead for the same PES packet.

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
(see [Dump PCR jitter](dumps.md#dump-pcr-jitter)). It is the `timing`
category of [`--fail-on`](#ci-failure-policies---fail-on), which can select
the other transport-health checks as well.

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

The complete contract, with every `--fail-on` category, is under
[Exit codes](#exit-codes).

Example default result:

```text
-----------------------------
Compliance Check Results:
Max PCR interval: 80.000000ms [OK, limit: <= 100.000000ms]
PCR discontinuities: 0 undeclared in 60 PCRs [OK]
PCR-PTS/DTS max gap: 726.666667ms [OK, limit: <= 1000.000000ms]
```

## CI failure policies (`--fail-on`)

By default the exit status does not depend on any verdict: a stream with
findings exits 0 as long as it could be analyzed. `--fail-on` selects which
completed-analysis findings make the command exit 2, so a CI job can fail on
the checks its policy cares about without parsing the output:

```sh
mpeg-ts-analyzer input.ts --fail-on timing                       # the same as --fail-on-error
mpeg-ts-analyzer input.ts --fail-on timing,continuity,timestamp
mpeg-ts-analyzer input.ts --fail-on psi_health --fail-on sync    # repeated values add up
mpeg-ts-analyzer input.ts --fail-on any                          # every category below
```

Each value is a comma-separated list of category names, without spaces; the
flag may be repeated, and the policy is the union of every value. Names are
lowercase and matched exactly. A name given twice, or a category also covered
by `any`, adds nothing. An empty name (`--fail-on ""`, `--fail-on timing,`)
or an unknown one (`--fail-on Timing`, `--fail-on pcr_jitter`) is a usage
error: the command exits 1 before reading the input and names the accepted
categories. `--fail-on-error` selects `timing`, so combining it with
`--fail-on` gives the union, for example `--fail-on-error --fail-on continuity`
is `--fail-on timing,continuity`. `mpeg-ts-analyzer --capabilities` lists the
categories a build accepts (`fail_on_categories`).

Each category is one check (or, for `timing`, the three timing compliance
checks) and fails on the statuses that are findings, exactly as the text and
JSON output report them:

| Category | Check (JSON) | Fails on | Never fails on |
| --- | --- | --- | --- |
| `timing` | Max PCR interval, PCR discontinuities, PCR-PTS/DTS max gap (`compliance`) | `NG` (`ng`) | `OK`, `SKIPPED` |
| `timestamp` | [Timestamp anomaly report](#timestamp-anomaly-report-always-on) (`timestamp_anomalies`) | any anomaly (`anomalies`), a PTS/DTS wraparound included | no anomaly (`ok`) |
| `pes_headers` | [PES headers](#pes-headers-and-pts-repetition-always-on) (`pes_headers`) | `ERRORS` (`errors`) | `OK`, `INSUFFICIENT DATA` |
| `pts_repetition` | [PTS repetition](#pes-headers-and-pts-repetition-always-on) (`pts_repetition`) | `NG` (`ng`) | `OK`, `INSUFFICIENT DATA`, `SKIPPED` |
| `continuity` | [Continuity counter](#continuity-counter-summary-always-on) (`continuity_counter`) | errors (`errors`) | no errors (`ok`) |
| `transport_errors` | [Transport errors](#transport-errors-and-scrambled-packets-always-on) (`transport_errors`) | `AFFECTED`, `DATA EXCLUDED` | none affected (`ok`) |
| `scrambled_packets` | [Scrambled packets](#transport-errors-and-scrambled-packets-always-on) (`scrambled_packets`) | `AFFECTED`, `DATA EXCLUDED` | none scrambled (`ok`) |
| `sync` | [TS synchronization](#ts-synchronization-always-on) (`sync`) | `recovered`, `lost` | `ok` |
| `psi_health` | [PAT and PMT health](#pat-and-pmt-health-always-on) (`psi_health`) | `NG` (`ng`) | `OK`, `INSUFFICIENT DATA` |
| `referenced_pids` | [Referenced PIDs](#pid-references-always-on) (`pid_references.referenced`) | `ABSENT`, `SILENT` | `OK`, `INSUFFICIENT DATA` |
| `unreferenced_pids` | [Unreferenced PIDs](#pid-references-always-on) (`pid_references.unreferenced`) | `UNREFERENCED` | `OK`, `INSUFFICIENT DATA`, `SKIPPED` |

`any` selects every category of this table and nothing else. What never
fails, whatever `--fail-on` selects:

- a skipped, `INSUFFICIENT DATA` or not-checked result: it means the check
  could not be evaluated on this range, not that it found a problem;
- the report-only [packet gaps](#packet-gaps-report-only) and
  [mux lead](#mux-lead-always-on-report-only) and the `--dump-bitrate`
  summary, which have no verdict;
- the PCR jitter estimate of `--dump-pcr-jitter`, whatever its status
  (`warning`, `ng`, `not_assessable`): it is an estimate from byte positions,
  not a measurement (see [Dump PCR jitter](dumps.md#dump-pcr-jitter));
- warnings, such as an ignored partial packet at the end of the input or a
  PSI section problem reported while the tables were acquired;
- listing programs: a multi-program stream without `--program`, or
  `--list-programs`, analyzes no program, so it exits 0.

A loss of synchronization that the analysis cannot recover ends it early with
the JSON status `incomplete`, which exits 0 by default; `--fail-on sync` makes
it exit 2. Scrambled packets are normal in a stream under conditional access,
so `any` fails such a stream; list the categories explicitly to leave
`scrambled_packets` out. The `timestamp` category fails on every reported
anomaly, including a 33-bit wraparound, which a stream longer than about
26.5 hours always has.

The policy judges the analyzed program of the summary output (text or
`--format json`) after the whole report has been written: all output stays
available, and the failed checks follow on stderr, for example
`compliance check failed: Continuity counter`. With `--format json` the report
gains a `failure_policy` object with the selected and the failed categories;
see [Machine-readable output](../reference/json-output.md#failure-policy).
`--output analysis` covers every program and reports `--fail-on` (like
`--fail-on-error`) as `incompatible_options`; `--output packets` and
`--output packet` analyze no program and reject `--fail-on` as a usage error.
`--fail-on-error` keeps its earlier behavior there: it is accepted and has no
effect.

A CI step that fails on the integrity and structure of the transport stream,
keeps the JSON report as an artifact and treats a usage or input error
differently from a finding:

```sh
set +e
mpeg-ts-analyzer input.ts --format json \
  --fail-on timing,continuity,sync,transport_errors,psi_health,referenced_pids \
  > report.json
status=$?
set -e
case $status in
  0) echo "transport stream healthy for this policy" ;;
  2) echo "findings: $(jq -c .failure_policy.failed_categories report.json)"; exit 1 ;;
  *) echo "the analysis could not run"; exit "$status" ;;
esac
```

In GitHub Actions, for example:

```yaml
- name: Check the transport stream
  run: mpeg-ts-analyzer output.ts --fail-on any
```

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | The command completed: an analysis (complete or `incomplete`) in which no category selected with `--fail-on` or `--fail-on-error` reported a finding, a program listing, `--capabilities`, `--help` or `--version`. Without a policy every completed analysis exits 0, whatever its verdicts. |
| `1` | A usage error (an unknown flag or `--fail-on` category, an invalid value or combination), an input error (the file cannot be opened or read) or a failed analysis (no TS synchronization, no PAT, an unknown `--program`); with `--format json` the report has `status: "failed"`. |
| `2` | The analysis completed, and at least one selected category reported a finding. The report is complete; stderr names the failed checks. |

JSON automation that also needs to reject a range the analysis could not
finish should check `status == "complete"`, since `incomplete` exits 0 unless
`--fail-on sync` applies to its cause.

## When the analysis cannot start

Program-level analysis needs two things from the selected range (the whole
input, or `--offset`/`--limit`): a packet alignment, and then a complete PAT,
which gives the programs, their PMT PIDs and, through the PMTs, the PCR PIDs
and elementary streams. Without them the analysis stops with exit status 1. It
never guesses a program layout and reports no PAT/PMT-dependent result, so a
failure is never shown as a partial or healthy analysis. The error names the
missing prerequisite and the evidence gathered while scanning the range:

| Error starts with | JSON `reason` | Meaning |
| --- | --- | --- |
| `no TS synchronization found: the range ... holds only N bytes` | `insufficient_data_for_sync` | The range is too short to establish at least one supported packet alignment: 188-byte packets need 377 bytes (three sync bytes one packet apart), 192-byte M2TS 389 bytes, 204-byte packets 817 bytes (five sync bytes). The layouts that could be checked and were not found are named. |
| `no TS synchronization found: no 188-byte, 192-byte or 204-byte packet alignment` | `sync_not_acquired` | The range was long enough for every layout and contains none. The data may not be MPEG-2 TS packets of a supported size, or may be damaged; no file type is guessed. |
| `PAT not detected: ... no packet is on PID 0x0000` | `pat_not_observed` | Packets are aligned, but none is on PID 0. |
| `PAT not detected: ... N packets on PID 0x0000` | `pat_not_acquired` | Packets on PID 0 were seen, but no usable PAT could be assembled from them. |

For example, a range of twelve packets of the bundled sample that contains no
PAT:

```
$ ./mpeg-ts-analyzer --offset 1128 --limit 2256 sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Input file:  sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Packet size: 188 bytes
...
PAT not detected: no complete PAT after scanning 12 aligned 188-byte packets (2256 bytes) in the range from 0x00000468 to 0x00000d38; no packet is on PID 0x0000. The range may be a partial capture or a PSI/PES fragment, or may end before the next PAT repetition; a wider --offset/--limit range may include one; program analysis needs a PAT and stops here
```

and a range whose only PAT packet has `transport_error_indicator` set:

```
$ ./mpeg-ts-analyzer --offset 7332 --limit 1880 sample_data/sample_188byte_transport_errors.ts
...
PAT not detected: no complete PAT after scanning 10 aligned 188-byte packets (1880 bytes) in the range from 0x00001ca4 to 0x000023fc; 1 packet on PID 0x0000, 1 starting a section; problems (sections or packets): 1 with transport_error_indicator or a scrambled payload. The PAT may be damaged or cut off by the range; a wider --offset/--limit range may include one; program analysis needs a PAT and stops here
```

The ranges are absolute offsets; the end is exclusive. Counted facts (aligned
packets, packets on PID 0 and those starting a section, and the PAT problems,
counted as in [PAT and PMT health](#pat-and-pmt-health-always-on): a packet
with a transport error or scrambled payload, an invalid CRC_32, a malformed
section, a `table_id` other than 0x00, or an incomplete section) are stated as
such; why the PAT is missing is worded as a possibility. "Aligned packets"
counts the packets of the detected size that start with the sync byte, from
the first alignment the detector accepts; after a packet without it, a new
alignment needs five sync bytes, as in
[TS synchronization](#ts-synchronization-always-on). With `--offset` or
`--limit`, the error suggests a wider range, since the selected one may omit
the PSI. The extra pass that gathers this evidence runs only after a failure,
reads the range once more and keeps a fixed amount of state.

When the first read of the input has no packet alignment, the `Packet size`
line says that 188 bytes was only assumed:
`Packet size: 188 bytes (assumed: no packet alignment in the first read)`.
PAT discovery keeps that size. If a later read holds packets of another size,
the evidence is gathered at that size, and the error ends by saying that PAT
discovery assumed 188-byte packets and could not use them.

A short fragment, or a range without a PAT, can still be inspected packet by
packet: `--format json --output packets` (with `--packet-size 188`, `192` or
`204` when the size cannot be detected) lists the packets without PAT/PMT
context; see [Packet pages](../reference/json-output.md#packet-pages---output-packets).
That inspection does not supply the program layout, so it is not a program
analysis.

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
number of missing transport packets. Errors do not change the default exit
status; `--fail-on continuity` makes them exit 2. Known PAT, PMT, PCR, video, and audio PIDs
receive labels; any other included PID is labeled `unknown`.

## TS synchronization (always on)

Every packet of the analysis must start with the sync byte `0x47` (for
192-byte M2TS input, the fifth byte, after the 4-byte `TP_extra_header`; for
204-byte input, the first byte, with the 16-byte trailer at the end of the
packet). When
a packet boundary has another value, synchronization is lost there. The
analyzer then looks for the next offset at which **five** consecutive packets,
one packet size apart, start with `0x47`, and continues from there. The bytes
in between are skipped: they are never read as packets, so they cannot add PIDs,
PCRs or timestamps. A single `0x47` inside a payload, or fewer than five in a
row, is not accepted; for 204-byte input that includes a `0x47` in a trailer.
The packet size does not change during the search, so 204-byte input is
searched with a 204-byte stride. Five
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
  "sync_not_acquired"`, or `insufficient_data_for_sync` when the range is too
  short to show an alignment), distinct from aligned packets without a PAT;
  see [When the analysis cannot start](#when-the-analysis-cannot-start).
- **Before the analysis starts.** The check covers the analyzed packets, from
  the end of the first complete PAT and PMT on. A packet without a sync byte
  while those tables are collected restarts the PAT search, with a warning, as
  before.
- **A packet with its sync byte but an unparseable header** (for example a
  reserved `adaptation_field_control`) is aligned, so it is not a loss of
  synchronization. With `transport_error_indicator` set it is counted and the
  analysis continues; without it, it interrupts the analysis, as described
  under
  [Transport errors and scrambled packets](#transport-errors-and-scrambled-packets-always-on).
- **Trailing partial packet.** Fewer than one packet of bytes at the end of the
  input is an incomplete packet, not a loss: it is reported as a warning
  (`ignored N trailing bytes`) and in the JSON `sync` object, and does not
  change the status.

`--fail-on-error` and the default exit status do not depend on this check;
`--fail-on sync` makes a `recovered` or `lost` result exit 2.

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
byte of the packet (for M2TS, of its `TP_extra_header`; for 204-byte packets,
the sync byte, which is also the first byte). The status is:

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
| Adaptation field | Not parsed: no PCR, `discontinuity_indicator` or other value of it is used, and a damaged one does not stop the analysis | Parsed as for any packet |
| Continuity counter, bitrate | Counted with the header as decoded (its `continuity_counter` and `adaptation_field_control`); the packet's `discontinuity_indicator` is not used, and its PCR bytes are compared like any other byte when duplicates are detected | Counted |

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
ends with `PAT not detected` (JSON `reason: "pat_not_acquired"`, with the
unusable packets counted in `acquisition.pat`) or as incomplete, never as a complete result built
from unusable tables. Neither check takes part in `--fail-on-error`;
`--fail-on transport_errors` and `--fail-on scrambled_packets` select them.

Since a transport error may be anywhere in the packet, nothing after the
fixed 4-byte TS header of such a packet is checked or decoded. A packet with
`transport_error_indicator` set whose `adaptation_field_control` is the
reserved value 0, whose `adaptation_field_length` runs past the packet, or
whose adaptation field is otherwise malformed is therefore counted like any
other affected packet, and the analysis continues with the next packet. Where
the analysis needs to know whether such a packet has a payload, the payload is
everything after the header (`adaptation_field_control` 1), everything after
the adaptation field when `adaptation_field_length` stays inside the packet
(3), and nothing otherwise. `sample_data/sample_188byte_transport_errors_malformed.ts`
damages every transport-error packet of the transport-errors sample this way
and gives the same results.

A packet **without** `transport_error_indicator` whose header or adaptation
field cannot be parsed (a reserved `adaptation_field_control`, or an
adaptation field whose lengths run past the packet) is not covered by this
policy: nothing marks its data as unreliable, so the analyzer treats it as a
parse error. With the summary output (text or JSON), the analysis of the
program stops at that packet and restarts at the next PAT with a
`PES parse error` warning; when no later analysis completes, the JSON status
is `incomplete`. `--output analysis` reports `status: "failed"` with the error
and exits with status 1. The packet list (`--output packets`) shows the packet
with `valid: false`.

## PAT and PMT health (always on)

An always-on check reports, for the PAT and for the PMT of every program the
PAT announces (not only the analyzed one), whether the table was found, how
often it repeats, and what was wrong with the sections that could not be used.
It covers every packet of the range from its first packet. A healthy stream
prints one line for the check and one per table:

```
-----------------------------
PAT/PMT health: OK (repetition limit 500 ms, ETSI TR 101 290 1.3.a/1.5.a; time base: PCR PID 0x0100 of program 1)
  PAT PID 0x0000 : 43 tables, 40 intervals, max 150.222 ms ending at 0x00020388 [OK]
  PMT program 1 PID 0x1000 : 43 tables, 40 intervals, max 148.444 ms ending at 0x00020444 [OK]
```

The sample `sample_data/sample_188byte_psi_health.ts` (see
[Sample data](../development/sample-data.md)) has no PAT for 1.2 s, a PMT
section on the PAT PID, a PMT with an invalid CRC_32 and a two-section PMT
whose second section is never sent:

```
-----------------------------
PAT/PMT health: NG (repetition limit 500 ms, ETSI TR 101 290 1.3.a/1.5.a; time base: PCR PID 0x0100 of program 1)
  PAT PID 0x0000 : 12 tables, 8 intervals, max 1200.000 ms ending at 0x000048b4, first problem at 0x00002b54 [NG: late x1, wrong_table_id x1]
  PMT program 1 PID 0x1000 : 15 tables, 12 intervals, max 400.000 ms ending at 0x0000359c, first problem at 0x00002e44 [NG: crc_error x1]
  PMT program 2 PID 0x1010 : not found, 0 intervals, max 2840.000 ms ending at 0x00006cb0, first problem at 0x00006d6c [NG: missing, incomplete x1]
```

**What counts as a table.** A table is counted when every section of one
version has arrived valid (CRC_32, syntax, `current_next_indicator` 1), at the
packet that completes it. Sections are collected exactly as the analysis
collects them ([Multi-section PAT and PMT](psi.md#multi-section-pat-and-pmt)),
so a table counts when the analysis could use it. A PMT is read on the PID the
applied PAT assigns to its program, from the packet after the PAT that
announced it; a later PAT change that adds, removes or moves a program is
followed (see [PAT and PMT version changes](psi.md#pat-and-pmt-version-changes)).

**Which rules come from where.**

| Rule | Source | Reason |
| --- | --- | --- |
| The PAT is on PID 0x0000 with `table_id` 0x00; each PMT has `table_id` 0x02 on the PID the PAT assigns | ISO/IEC 13818-1 §2.4.4 (generic MPEG-2) | `wrong_table_id` for any other `table_id` on PID 0x0000 |
| Valid CRC_32, section syntax, every section of a table | ISO/IEC 13818-1 §2.4.4 and Annex A (generic MPEG-2) | `crc_error`, `malformed_section`, `incomplete` |
| A PAT or PMT announced in the range is found | generic MPEG-2: a program cannot be decoded without them | `missing` |
| At most 500 ms between two tables | ETSI TR 101 290 V1.4.1 §5.2.1, 1.3.a `PAT_error_2` and 1.5.a `PMT_error_2` (DVB measurement guideline); ISO/IEC 13818-1 sets no repetition interval | `late` |
| PAT/PMT packets are not scrambled, and not damaged | TR 101 290 1.3.a / 1.5.a (scrambling); the trust policy of [transport errors and scrambled packets](#transport-errors-and-scrambled-packets-always-on) | `unusable_packets` |

The 500 ms limit is the only profile-derived value. Other broadcast profiles
require shorter intervals; the analyzer does not apply them, so a stream that
passes may still fail a stricter profile.

**Time.** A file has no packet-arrival times, so the time of a table is
interpolated by byte position between the two PCRs around it on one
reference `PCR_PID`: that of the analyzed program, or with
`--output analysis` that of the first analyzed program (in `program_number`
order) that has one. A PCR in a packet with `transport_error_indicator` is not
used. The PCRs are classified into the same [PCR steps](#pcr-steps) as every
other PCR-based result: only `continuous` and `wraparound` steps are
measured, and every other step (`declared`, `pcr_pid_change`, `backward`,
`forward_jump`) starts a new PCR segment; no interval spans two segments.

**Intervals.** Within a segment, the time between two consecutive tables is a
*complete* interval. The time from the start of a segment, or from the first
PCR after a PMT was announced, to the first table, and from the last table to
the last PCR of the segment, are *open* intervals: the table may have been
sent just outside them, so they are lower bounds of the real interval. Both
are compared with the limit, because an open interval over 500 ms proves a
longer gap. A table must have at least one complete interval to be `OK`. A
version change keeps the table's identity (it must still repeat on the same
PID), so it does not restart the intervals; a PMT moved to another PID, or a
program removed and announced again, does. The counts and the longest
interval cover every identity of the program, but evidence does not carry
over: a late or missing identity keeps the table `NG`, and the table is `OK`
only when its latest identity has a complete interval of its own.

**Status per table.**

| Status | Meaning |
| --- | --- |
| `OK` | At least one complete interval, none over 500 ms, and no problem |
| `NG` | At least one reason below |
| `INSUFFICIENT DATA` | No problem, but the repetition could not be shown: no complete interval (for example a range shorter than two repetitions, or a table seen only once), or no PCR time base |

| Reason | Meaning |
| --- | --- |
| `missing` | No table in the range, while it was followed with a time base for more than 500 ms |
| `late` | An interval over 500 ms (`x` count) |
| `crc_error` | Sections with an invalid CRC_32 |
| `malformed_section` | Sections that cannot be read: too short, without `section_syntax_indicator`, `section_number` beyond `last_section_number`, an invalid `pointer_field`, or fields that do not parse |
| `incomplete` | Collected sections discarded before the table completed (a section cut short by the next section start, or sections that changed without a version change), and once when the range ends while a table that never completed is being collected |
| `wrong_table_id` | A section with a `table_id` other than 0x00 on PID 0x0000 (PAT only) |
| `unusable_packets` | Packets of the table's PID not read because of `transport_error_indicator` or a scrambled payload; the tables they carried are also missing from the intervals |

A continuity counter jump inside a section is reported by the
[continuity counter summary](#continuity-counter-summary-always-on), not
here; the table it breaks shows as a longer interval. A section that cannot be
attributed to one program of a shared PMT PID counts for every program on that
PID. Without a PCR time base (`no pcr time base`), the repetition is not
measured and a table that was never found is `INSUFFICIENT DATA`, not
`missing`. A range that starts in the middle of a stream measures the first
interval from its first PCR, as an open interval.

The check keeps one fixed-size record per table and one section reader per
PMT PID, so its memory is bounded by the programs of the PAT, not by the
length of the input. It does not take part in `--fail-on-error`;
`--fail-on psi_health` makes an `NG` result exit 2. In JSON it is
`psi_health` ([Machine-readable output](../reference/json-output.md#pat-and-pmt-health)).

## PID references (always on)

An always-on check reconciles the PIDs that the PSI layout references with the
PIDs that actually carry packets, for the whole multiplex. A healthy stream
gets one line for each part:

```
-----------------------------
Referenced PIDs: 4 references, silence limit 5000 ms [OK]
Unreferenced PIDs: none [OK]
```

When something is wrong, the PIDs concerned follow (output of
`sample_data/sample_188byte_pid_references.ts`, see
[Sample data](../development/sample-data.md)):

```
-----------------------------
Referenced PIDs: 8 references, silence limit 5000 ms [ABSENT]
  program 1 ES PID 0x0101 (stream_type 0x03) : silent, no packet for 6100.0 ms from 0x0000ae0c to 0x0001613c
  program 1 ES PID 0x0102 (stream_type 0x06) : insufficient data (intermittent_stream_type)
  program 1 ES PID 0x0103 (stream_type 0x04) : absent, no packet for 13175.0 ms from 0x00001f94 to 0x0001a354
Unreferenced PIDs: 2 PIDs, grace 500 ms [UNREFERENCED]
  PID 0x0300 : unreferenced, 5 packets from 0x00005c88 to 0x00008b88, 12750.0 ms
  PID 0x0500 : insufficient data, 2 packets from 0x0001c8c8 to 0x0001d488, 350.0 ms
```

**Referenced PIDs.** Every program of the PAT references its PMT PID. A
program whose PMT the analysis follows (the selected program, or every program
with `--output analysis`) also references its `PCR_PID` (unless `0x1FFF`) and
each elementary PID. Each program, role (PMT, PCR, ES) and PID is one
reference, so a `PCR_PID` that also carries an elementary stream is listed
for both roles; a stream that a PMT change retypes is a new reference of the
new `stream_type`, judged only under that type from the change on. A reference holds from the packet where its layout applies
(the start of the analysis, which follows the acquisition of the PAT and PMT,
or the packet after the one that completed a PAT or PMT change) until a later
layout no longer contains it or the analysis ends. Its silence is the longest
time without a packet of the PID while referenced, counting from the start of
the reference to the first packet and from the last packet to the end of the
reference, so a stream removed by a PMT change is not silent after it.

| Status | Meaning |
| --- | --- |
| `OK` | Packets arrived, and no silence of a judged role exceeded 5000 ms |
| `ABSENT` | A judged role had no packet at all during a reference longer than 5000 ms |
| `SILENT` | A judged role had packets, but went more than 5000 ms without one |
| `INSUFFICIENT DATA` | No packet, but the reference was not longer than 5000 ms (`reference_shorter_than_limit`) or the stream type is not judged (`intermittent_stream_type`); or no PCR to measure time (`no_pcr_time_base`) |

Absence and silence are judged for the PMT, the `PCR_PID` and video and audio
streams (the stream types the continuity summary labels `video` or `audio`),
which carry packets continuously. Other stream types, such as private data,
subtitles or metadata, may be idle for a long time, so the check only reports
whether they had packets. The summary line shows the most severe status.

ETSI TR 101 290 (check 1.6, PID_error) leaves the period after which a
referenced PID is missing to the user; the analyzer uses 5 s, the default of
TSDuck's TR 101 290 analyzer for video and audio PIDs. Repetition of the PMT
itself is not this check: a PMT that arrives every 3 s is present here. The
5000 ms limit and the 500 ms grace period below are fixed: no option changes
them, and they are reported in JSON (`silence_limit_ms`,
`unreferenced_grace_ms`). Configurable limits may come with the CI failure
policy ([#62](https://github.com/small-teton/mpeg-ts-analyzer/issues/62)).

**Unreferenced PIDs.** A packet is unreferenced when no layout references its
PID at that point and the PID is not the null PID `0x1FFF`, one of `0x0000` to
`0x001F` (reserved for PSI and SI by ISO/IEC 13818-1 and ETSI EN 300 468),
`0x1FFB` (ATSC PSIP), the PAT's `network_PID`, or the `CA_PID` of a
CA_descriptor in a PMT. ETSI TR 101 290 (check 3.4, Unreferenced_PID) allows
0.5 s for a PMT to refer to a PID, which covers the packets of a new stream
sent shortly before the PMT that announces it and of a removed stream sent
shortly after the PMT that removes it. Consecutive unreferenced packets of a
PID form a run:

- a PID that no earlier layout referenced is unreferenced when no layout
  references it within 500 ms of the first packet of the run (until the end of
  the range, a PID that is never referenced is unreferenced from its first
  packet);
- a PID that an earlier layout referenced is unreferenced when its packets
  continue for more than 500 ms after the first unreferenced one;
- a run that the end of the range cuts before 500 ms could elapse is
  `insufficient data`: the range is too short to tell.

| Status | Meaning |
| --- | --- |
| `none [OK]` | No PID carried unreferenced packets for longer than 500 ms |
| `UNREFERENCED` | At least one PID did; each is listed with its packets, the first and last of them and how long its longest run lasted |
| `INSUFFICIENT DATA` | No PID is unreferenced, but a run could not be judged (`unreferenced_within_grace`) or there is no PCR time base (`no_pcr_time_base`) |
| `not checked (program_layouts_unknown) [SKIPPED]` | The analysis did not follow every program's PMT, so the referenced PIDs are not fully known; for example `--program 2` on a two-program stream (use `--output analysis` to analyze every program) |

The CAT is not parsed, so an EMM PID that only the CAT announces is reported
as unreferenced; ECM PIDs named by a CA_descriptor in a PMT are referenced.
Whether the data on an unreferenced PID is useful is not judged.

Unreferenced packets are only judged while every program of the PAT is
analyzed, so the packets read while the first PAT and PMT are acquired (for a
capture that starts in the middle of a stream) never count, and neither do
packets of a program added by a later PAT, whose PMT is not followed.

Both parts share these rules:

- Time is estimated from the packet's byte position between two PCRs of the
  reference program (the analyzed program, or with `--output analysis` the
  first program in PAT order that has a `PCR_PID`), as for the bitrate and
  PCR jitter estimates; all programs' clocks run at 27 MHz. Time before the
  first or after the last PCR of the range, and across a timing-segment
  boundary (a `declared`, `pcr_pid_change`, `backward` or `forward_jump`
  [PCR step](#pcr-steps)), is not counted, so a duration there is a lower
  bound; a `wraparound` is an ordinary interval (`max_silence_exact: false` in JSON).
  Without any PCR, durations are unknown and the results are `INSUFFICIENT
  DATA`.
- A packet with `transport_error_indicator` set does not count: its PID may be
  corrupted, so it neither shows that a PID is present nor makes one
  unreferenced. A scrambled packet counts, because its header is clear.
- Packet counts cover the whole range and match the per-PID packet counts of
  TSDuck's `tsanalyze`, which also leaves packets with a transport error out.
- The check does not change the default exit status and does not take part in
  `--fail-on-error`; `--fail-on referenced_pids` (`ABSENT`, `SILENT`) and
  `--fail-on unreferenced_pids` (`UNREFERENCED`) select it. The state it keeps is bounded by the number of PIDs and
  programs; no per-packet history is kept. The JSON result is `pid_references`
  ([JSON output](../reference/json-output.md#pid-references)).

## PES headers and PTS repetition (always on)

Two always-on checks look at the PES packets of every elementary stream of the
analyzed program. A healthy stream gets one line for each:

```
-----------------------------
PES Headers: 220 checked on 2 PIDs, no errors
PTS Repetition (limit 700ms): max interval 66.666667ms over 214 intervals [OK]
```

When a check finds a problem, it prints its totals and a status, then one
line per affected PID (output of `sample_data/sample_188byte_pes_health.ts`,
see [Sample data](../development/sample-data.md)):

```
-----------------------------
PES Headers: 8 errors in 161 checked on 5 PIDs, first at 0x000013d4 [ERRORS]
  Program 1 PID 0x0101 (audio) : 1 error in 49 checked, first at 0x00002ccc [ERRORS]
    bad_marker_bits: 1, first at 0x00002ccc
  Program 1 PID 0x0102 (stream_type 0x06) : 7 errors in 15 checked, first at 0x000013d4 [ERRORS]
    truncated: 1, first at 0x000019b4
    bad_start_code: 1, first at 0x000013d4
    bad_flags: 2, first at 0x00003134
    inconsistent_length: 2, first at 0x000042d4
    bad_marker_bits: 1, first at 0x00005474
  Program 1 PID 0x0104 (video) : INSUFFICIENT DATA (no PES packet started)
PTS Repetition (limit 700ms): max interval 1080.000000ms from 0x000047f8 to 0x00006fa0, 1 exceeded, first ending at 0x00006fa0 [NG]
  Program 1 PID 0x0100 (video) : max interval 1080.000000ms from 0x000047f8 to 0x00006fa0, 1 exceeded, first ending at 0x00006fa0 [NG]
  Program 1 PID 0x0103 (audio) : INSUFFICIENT DATA (fewer than two comparable PTS)
  Program 1 PID 0x0104 (video) : INSUFFICIENT DATA (no PES packet started)
```

**PES headers** are checked against ISO/IEC 13818-1 (2.4.3.6, 2.4.3.7) from the
header bytes only; the analyzer retains at most the 264-byte maximum header of
each PES packet and never reads the elementary-stream payload. A header split
over several TS packets is assembled first. The problems are:

| Problem | The header |
| --- | --- |
| `truncated` | ends before its fixed bytes or its `PES_header_data_length` bytes, because the next PES packet of the PID started |
| `bad_start_code` | does not start with `packet_start_code_prefix` 0x000001 and a PES `stream_id` (0xBC to 0xFF) |
| `bad_flags` | has an optional header that does not start with `'10'`, or `PTS_DTS_flags` `'01'` (forbidden) |
| `inconsistent_length` | has a `PES_header_data_length` too small for the fields its flags announce, or a non-zero `PES_packet_length` too small for the header |
| `bad_marker_bits` | has a PTS or DTS with a wrong 4-bit prefix or a `marker_bit` of 0 |

A failed header gives no PTS/DTS to any other check: no timestamp anomaly,
PCR-PTS/DTS gap or PTS repetition interval comes from it, so one malformed
header is one finding. A PES packet whose header is still incomplete when its
payload stops being trusted (a transport error or scrambling), when the PMT
changes or when the range ends is counted as *unchecked*, not as an error. A
`PES_packet_length` of 0 (unbounded) is accepted for every stream.
`INSUFFICIENT DATA` means no header could be checked on the PID.

**PTS repetition** measures, on each video and audio stream (stream types
labeled `video` or `audio`), how long the multiplex goes without a PES packet
carrying a PTS. ISO/IEC 13818-1 (2.7.4) requires a PTS at least every 0.7 s on
video and audio streams; ETSI TR 101 290 monitors it as PTS_error (2.5). Other
streams are skipped.

- The time of a PES packet is the program's PCR at its first packet,
  interpolated by byte position between the PCRs before and after it. The
  interval is how long the transport carried no PTS, not a difference of PTS
  values, so B-frame reordering and splices do not affect it.
- No interval spans a PCR discontinuity, a malformed or incomplete header,
  untrusted payload, a continuity error or a PMT change of the PID, since a
  PTS may have been lost there. PES packets before the first PCR or after the
  last one are not placed.
- PES packets without a PTS for longer than 700 ms count as an exceeded
  interval even before the first PTS or at the end of the range.
- `INSUFFICIENT DATA` gives the reason: no PES packet, payload not used, only
  malformed headers, no PES packet between two PCRs, no PTS, or fewer than two
  comparable PTS. TR 101 290 exempts still pictures, which the analyzer cannot
  recognize without reading the elementary stream.

The timestamp anomaly report does not report a forward jump across PES packets
without a timestamp (or a malformed header, untrusted payload or a continuity
error); PTS repetition and the other checks report that cause. Neither check
takes part in `--fail-on-error`; `--fail-on pes_headers` and
`--fail-on pts_repetition` select them. See
[JSON output](../reference/json-output.md#pes-headers-and-pts-repetition) for
the complete rules.

## Packet gaps (report only)

Near the end of every analysis, before the mux lead, a block lists, for every
PID of the range except the null PID, the longest stretch of other packets between two consecutive
packets of the PID. It is a report-only multiplexing diagnostic: a poorly
interleaved remux that sends a stream in large blocks can starve it for a
long stretch even when its average bitrate is right. There is no limit and no
verdict, the exit status never changes and `--fail-on` and
`--fail-on-error` ignore it.
Output of the bursty remux `sample_data/sample_188byte_packet_gaps.ts` (see
[Sample data](../development/sample-data.md)):

```
-----------------------------
Max packet gaps (report only): 8 PIDs, time basis pcr_interpolated_file_position
  PID 0x0000 : 25 packets, max gap 31 packets, 160.000 ms from 0x00004970 to 0x000060f0
  PID 0x0011 : 13 packets, max gap 63 packets, 320.000 ms from 0x000066d0 to 0x000095d0
  PID 0x0012 : 1 packet, no gap (no_consecutive_packets)
  PID 0x0100 (program 1 ES stream_type 0x02) : 300 packets, max gap 5 packets, 30.000 ms from 0x000042d4 to 0x0000473c
  PID 0x0101 (program 1 ES stream_type 0x03) : 36 packets, max gap 125 packets, at least 590.000 ms from 0x00011f24 to 0x00017bac
  PID 0x0102 (program 1 ES stream_type 0x06) : 7 packets, max gap 63 packets, 320.000 ms from 0x00004a2c to 0x0000792c
  PID 0x01ff (program 1 PCR) : 100 packets, max gap 7 packets, 40.000 ms from 0x000040a0 to 0x00004680
  PID 0x1000 (program 1 PMT) : 25 packets, max gap 31 packets, 160.000 ms from 0x00005530 to 0x00006cb0
```

Each line is one PID, with the programs and roles that reference it (none for
PSI/SI PIDs and PIDs that no PAT or PMT references), its packet count and its
largest gap:

- The gap is the number of packets of other PIDs (null packets included)
  between two consecutive packets of the PID; the two packets are not counted.
  It is exact, and it chooses the largest gap. `from` and `to` are the two
  packets. Of equal gaps the first is shown, preferring one after the first
  PCR, whose time can be interpolated.
- The time is estimated from file position between PCRs, as for the PID
  references above. `at least` marks a lower bound: the gap lies partly
  before the first or after the last PCR of the analysis, or crosses a PCR
  timing-segment boundary (here, the audio bursts around a declared PCR
  discontinuity). A file has no arrival times, so even an exact value is an
  estimate, and on a variable-bitrate stream gaps in packets and in time can
  disagree. Without any PCR the time is unknown (`no_pcr_time_base`).
- The time before the first and after the last packet of a PID is not a gap;
  [PID references](#pid-references-always-on) judges absence and silence.
- A PAT or PMT change that removes a PID, or references it again after an
  earlier change removed it, starts a new chain: the packets on either side
  of the change are not consecutive. The private data PID `0x0102` above is
  removed for 1.6 s and re-added, and its gap across that absence (383
  packets) is not counted. Its packet count and earlier gaps are kept.
- A PID with a single packet, or no two packets within one stretch of the
  layout, has `no gap (no_consecutive_packets)`.
- The packets counted are those of the PID references: from the first packet
  of the range, without packets with `transport_error_indicator` set (which
  still take their place in the gaps of other PIDs). Packet counts match
  TSDuck's `tsanalyze`.

The state is one record per PID, shared with the PID references; nothing is
kept per packet. The JSON result is `multiplexing.packet_gaps`
([JSON output](../reference/json-output.md#multiplexing-packet-gaps)).

## Mux lead (always on, report only)

The mux lead of a PES packet is how far ahead of its decode time it was
multiplexed: its DTS (its PTS without DTS) minus its arrival time, the
program's PCR at its first packet interpolated by byte position between the
PCRs before and after it. The analyzer prints, for every elementary stream,
the smallest and largest lead with their PES offsets and the spread, and for a
program with video and audio the largest difference between an audio lead and
the video lead:

```
-----------------------------
Mux Lead (report only: DTS, or PTS without DTS, minus PCR-interpolated arrival)
  Program 1 PID 0x0100 (video) : 691.111ms to 726.667ms (spread 35.556ms) over 118 PES, min at 0x00007c1c, max at 0x0001e3f4, 1 unbracketed
  Program 1 PID 0x0101 (audio) : 663.311ms to 688.598ms (spread 25.287ms) over 98 PES, min at 0x000056a8, max at 0x0003df68, 3 unbracketed
  Program 1 A/V lead difference: 47.016ms, audio PID 0x0101 (lead 678.867ms at 0x00007cd8) against video PID 0x0100 (lead 725.882ms at 0x00008898)
```

It is a **report, not a check**: no limit applies, it never prints a verdict,
never becomes a finding and never changes the exit status, also with
`--fail-on` or `--fail-on-error`. A negative lead (a PES packet multiplexed after its
DTS/PTS) is printed as an observation, not as an error. With a large audio
preload (output of `sample_data/sample_188byte_mux_lead.ts`, see
[Sample data](../development/sample-data.md)):

```
-----------------------------
Mux Lead (report only: DTS, or PTS without DTS, minus PCR-interpolated arrival)
  Program 1 PID 0x0100 (video) : 700.000ms to 708.000ms (spread 8.000ms) over 115 PES, min at 0x00000758, max at 0x000005e0, 2 unbracketed
  Program 1 PID 0x0101 (audio) : 1500.000ms to 1500.000ms (spread 0.000ms) over 56 PES, min at 0x0000069c, max at 0x0000069c, 2 unbracketed
  Program 1 PID 0x0102 (audio) : -20.000ms to 650.000ms (spread 670.000ms) over 17 PES, min at 0x00002574, max at 0x00000814
    1 PES multiplexed after their DTS/PTS (negative lead: an observation, not an error)
  Program 1 PID 0x0103 (stream_type 0x06) : 300.000ms to 300.000ms (spread 0.000ms) over 1 PES, min at 0x00000b04, max at 0x00000b04
  Program 1 A/V lead difference: 800.000ms, audio PID 0x0101 (lead 1500.000ms at 0x0000069c) against video PID 0x0100 (lead 700.000ms at 0x00000758)
    audio PID 0x0101 : 800.000ms
    audio PID 0x0102 : 728.000ms
```

- The samples are the PES packets with a valid header and a PTS, the same as
  for PTS repetition and the PCR-PTS/DTS gap. A PES packet before the first
  PCR, after the last one or across a PCR discontinuity is *unbracketed* and
  not measured. The PTS/DTS and PCR wraparounds are handled: the lead is the
  signed difference nearest to zero modulo the PCR period.
- The PCR-PTS/DTS gap verdict above is the largest of the same leads in the
  program, judged against 1000 ms, and `--dump-timestamp` prints the same lead
  of each PES packet; the mux lead only reports how the leads are distributed
  per PID. One PES packet is never two findings. The gap check measures a PES
  packet when it ends, so a PES packet that ends before the next PCR arrives
  (a short audio PES packet, for example) is a mux lead sample but not a gap
  observation.
- Video is stream_type 0x01, 0x02, 0x1B or 0x24; audio is 0x03, 0x04, 0x0F,
  0x11, 0x81 or 0x87. Each audio stream is compared with the program's
  lowest-numbered video PID, whenever either gets a new lead and both leads
  lie in one PCR segment; with several audio streams the worst is printed
  first, then each one.
- `no lead (...)` gives the reason a PID has no sample, and `not measured`
  that an audio stream and the video never had leads in one PCR segment at
  the same time.

The state is bounded by the number of elementary streams. See
[JSON output](../reference/json-output.md#multiplexing-mux-lead) for the
complete rules.

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
- Only valid PES headers are checked. A forward jump is not reported across
  PES packets without a timestamp, a malformed or incomplete header, untrusted
  payload or a continuity error.
- The report does not change the default exit status; `--fail-on timestamp`
  makes any reported anomaly, a wraparound included, exit 2.

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
