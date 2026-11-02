# Options & output examples

Annotated output for every `mpeg-ts-analyzer` flag, run against the bundled
streams in `sample_data/`. See the [README](README.md) for install and usage.

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
Compliance Check Results:
Max PCR interval: 80.000000ms [OK, limit: <= 100.000000ms]
PCR-PTS/DTS max gap: 726.666667ms [OK, limit: <= 1000.000000ms]
-----------------------------
Continuity Counter: no errors detected
```

## Timing compliance verdicts and `--fail-on-error`

The maximum PCR interval and PCR-to-PTS/DTS gap verdicts are always printed.
The gap uses DTS when both PTS and DTS are present, otherwise PTS. It measures
the decode timeline rather than the presentation delay of reordered frames. The
limits are inclusive, so exactly 100 ms and 1000 ms are `OK`. When there are
fewer than two comparable PCR observations, or no parsed PTS/DTS bracketed by
PCR observations, the corresponding result is explicit and non-failing:

```text
-----------------------------
Compliance Check Results:
Max PCR interval: SKIPPED (need at least two comparable PCR observations)
PCR-PTS/DTS max gap: SKIPPED (need a parsed PTS/DTS bracketed by PCR observations)
```

The maximum PCR interval is measured between consecutive PCRs on the selected
program's PCR PID, one segment at a time:

- A PCR packet with `discontinuity_indicator` starts a new segment, so no
  interval is measured across a declared clock discontinuity, forward or
  backward.
- A PCR that wraps from the top of the 2^33 × 300 range back toward zero is a
  normal interval and is measured across the wrap, as long as the wrapped
  interval is at most 1000 ms.
- Any other backward PCR without `discontinuity_indicator` is an undeclared
  reset. It starts a new segment without producing an interval.
- A forward jump without `discontinuity_indicator` is measured as is, because
  a file has no arrival time that could distinguish it from a genuinely late
  PCR. A large undeclared jump therefore reports `NG`.

`--fail-on-error` is intended for CI. It preserves all normal output and exits
with code 2 if either evaluated check is `NG`. Healthy and insufficient-data
runs exit 0; usage, input, and parsing errors use exit 1.

```sh
mpeg-ts-analyzer input.ts --fail-on-error
```

## JSON output

`--format json` emits one deterministic, indented JSON document on stdout. The
same analysis is performed as in text mode; `--dump-pcr-jitter` and
`--dump-bitrate` add their structured summaries. Object fields have a stable
order, PID collections are sorted numerically, and PID values use zero-padded
hexadecimal strings. Diagnostics and the final error message use stderr. The
formal Draft 2020-12 specification is
[`docs/json-schema-v1.json`](docs/json-schema-v1.json).

```sh
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --format json
{
  "schema_version": "1.0",
  "input": {
    "path": "sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts",
    "packet_size_bytes": 188,
    "offset_bytes": 0
  },
  "status": "complete",
  "programs": [
    {
      "program_number": 1,
      "pmt_pid": "0x1000",
      "pcr_pid": "0x0100",
      "elementary_streams": [
        {
          "pid": "0x0100",
          "stream_type": "0x02",
          "stream_type_description": "13818-2 video or 11172-2 constrained parameter video stream"
        },
        {
          "pid": "0x0101",
          "stream_type": "0x03",
          "stream_type_description": "11172 audio"
        }
      ],
      "status": "complete"
    }
  ],
  "selected_program_number": 1,
  "compliance": {
    "max_pcr_interval": {
      "status": "ok",
      "value_ms": 80,
      "limit_ms": 100,
      "observations": 59
    },
    "max_pcr_pts_dts_gap": {
      "status": "ok",
      "value_ms": 726.6666666666665,
      "limit_ms": 1000,
      "observations": 118
    }
  },
  "continuity_counter": {
    "status": "ok",
    "total_error_count": 0,
    "pids": []
  },
  "timestamp_anomalies": {
    "status": "ok",
    "items": []
  },
  "warnings": []
}
```

Empty arrays are retained, so “checked and healthy” is distinct from a missing
or unrequested section. Insufficient timing samples use `skipped` plus a
human-readable `reason`; input or parsing failure uses top-level
`status: "failed"`, a stable `reason` code, and a human-readable `error`.
Automatic multi-program listing uses `status: "programs_listed"` and
`reason: "program_selection_required"`.

Status and top-level reason values are:

| Field | Values |
| --- | --- |
| top-level `status` | `complete`, `programs_listed`, `incomplete`, `failed` |
| top-level `reason` | `program_selection_required`, `program_listing_requested`, `end_of_input_before_complete_analysis`, `incompatible_options`, `input_open_failed`, `analysis_failed` |
| `programs[].status` | `complete`, `incomplete` |
| incomplete program `reason` | `pmt_parse_failed` |
| `compliance.*.status` | `ok`, `ng`, `skipped` |
| `pcr_jitter.status` | `ok`, `warning`, `ng`, `skipped` |
| `bitrate.status` | `ok`, `skipped` |
| `continuity_counter.status` | `ok`, `errors` |
| `timestamp_anomalies.status` | `ok`, `anomalies` |

Top-level and per-program `reason` values are stable codes intended for machine
matching. `error` and analysis-section `reason` values are explanatory text and
must not be matched. `status: "incomplete"` exits 0 for compatibility with text
mode; automation should require `status == "complete"` instead of relying on
the exit code alone.

An input that ends with an incomplete packet, such as a capture cut off
mid-packet, is analyzed up to the last complete packet. The leftover bytes are
reported as a warning at the offset where the incomplete packet starts (in text
mode as a line such as `0x0004c0dc ignored 100 trailing bytes (incomplete packet
at end of input)`) and do not change the analysis result. With `--limit`, this
applies when the range reaches the end of the input; a range that ends
mid-packet inside the file reports nothing.

In a bitrate result, `pids` contains only selected-program PIDs and
`program_average_bps` / `program_peak_1s_bps` aggregate those rows. Mux-wide
NULL stuffing is reported separately as `null_pid` and is excluded from the
program totals.

JSON contains aggregate results only, not per-packet dumps, so output size does
not scale with every packet in a large input. Packet-level dump flags are
rejected with `--format json` instead of mixing human text into stdout. Schema
`1.x` may gain fields and enum values. Consumers must treat unknown enum values
as unrecognized. Existing fields are not removed or retyped, and existing enum
meanings do not change; those incompatible changes increment `schema_version`.

## Range analysis output (`--output analysis`)

`--format json --output analysis` analyzes **every program** of the range in a
single pass and adds time series, for callers that plot results (such as a GUI)
rather than only checking verdicts. It is a separate document with its own
schema, [`docs/json-schema-analysis-v1.json`](docs/json-schema-analysis-v1.json);
the default `--output summary` report is unchanged.

```sh
mpeg-ts-analyzer capture.ts --format json --output analysis --offset 0 --limit 67108864
```

Abridged output for the two-program sample:

```json
{
  "schema_version": "1.0",
  "output": "analysis",
  "input": {
    "path": "sample_data/sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts",
    "file_size_bytes": 202852,
    "packet_size_bytes": 188,
    "offset_bytes": 0
  },
  "status": "complete",
  "analysis_start_offset_bytes": 25380,
  "analysis_end_offset_bytes": 202852,
  "continuity_counter": { "status": "ok", "total_error_count": 0, "pids": [] },
  "continuity_events": [],
  "programs": [
    {
      "program_number": 1,
      "pmt_pid": "0x1000",
      "pcr_pid": "0x0100",
      "elementary_streams": [ ... ],
      "status": "complete",
      "analysis_start_offset_bytes": 25380,
      "average_bitrate_bps": 323018.1818181818,
      "compliance": { ... },
      "timestamp_anomalies": { ... },
      "pcr_jitter": { ... },
      "bitrate": { ... },
      "series": {
        "pcr": [
          { "offset_bytes": 29892, "elapsed_ms": 0, "pcr_ms": 860 },
          { "offset_bytes": 38540, "elapsed_ms": 80, "pcr_ms": 940, "interval_ms": 80 }
        ],
        "pes": [
          { "pid": "0x0100", "offset_bytes": 34592, "elapsed_ms": 0, "pts_ms": 1640, "dts_ms": 1600, "pcr_gap_ms": 696.5217391304348 }
        ],
        "timestamp_events": [],
        "pcr_jitter": [ { "offset_bytes": 38540, "jitter_ns": -14358974.35897437 } ],
        "bitrate": {
          "window_seconds": 1,
          "duration_seconds": [ 1, 0.76 ],
          "pids": [ { "pid": "0x0100", "label": "13818-2 video or 11172-2 constrained parameter video stream", "bytes": [ 30268, 18988 ], "bps": [ 242144, 199873.68421052632 ] } ]
        }
      }
    },
    { "program_number": 2, ... }
  ],
  "warnings": []
}
```

- Programs are those of the first complete PAT/PMT set in the range. Each
  program starts collecting just after its own PMT, so its results equal a
  `--program N --dump-bitrate --dump-pcr-jitter` run of the same range;
  `analysis_start_offset_bytes` reports where that was. A program whose PMT is
  not found is listed with `status: "incomplete"` and
  `reason: "pmt_parse_failed"`; if no program can be analyzed, the document has
  `status: "incomplete"` and `reason: "no_program_analyzed"`.
- Continuity counters are checked for the whole multiplex, from the earliest
  program start, and reported once (`continuity_counter`, `continuity_events`)
  rather than per program.
- Bitrate and PCR jitter are always collected in this mode.
- `elapsed_ms` is PCR time since the program's first PCR in the range. It does
  not advance across a PCR discontinuity, which matches the 1-second bitrate
  windows (window `i` covers elapsed seconds `i` to `i+1`). A PES point uses the
  elapsed time of the last PCR before it. Every point also carries
  `offset_bytes`, and each array is in stream order.
- The last bitrate window is shorter when the measured duration is not a whole
  number of seconds; `duration_seconds` gives each window's length. A PID's
  `average_bps` is `sum(bytes) * 8 / sum(duration_seconds)`, and its
  `peak_1s_bps` is the largest `bps` among the full 1-second windows.
- Output size grows with the range (one point per PCR, PES and jitter sample),
  so choose `--offset`/`--limit` accordingly.
- `--output analysis` requires `--format json` and cannot be combined with
  `--program`, `--list-programs`, `--fail-on-error` or packet-level dump flags.
  PAT/PMT changes within the range are not tracked yet.

## Packet pages (`--output packets`)

`--format json --output packets` lists up to `--count` packets (1–10000)
starting at the first packet boundary at or after `--offset`. Packets are read
directly, without PAT/PMT discovery, so a page starts with its first packet even
in the middle of a file. Schema:
[`docs/json-schema-packets-v1.json`](docs/json-schema-packets-v1.json).

```sh
$ mpeg-ts-analyzer sample_data/sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts \
    --format json --output packets --offset 29800 --count 2
{
  "schema_version": "1.0",
  "output": "packets",
  "input": {
    "path": "sample_data/sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts",
    "file_size_bytes": 202852,
    "packet_size_bytes": 188,
    "offset_bytes": 29800
  },
  "status": "complete",
  "first_offset_bytes": 29892,
  "next_offset_bytes": 30268,
  "end_of_file": false,
  "total_packets": 1079,
  "packets": [
    {
      "offset_bytes": 29892,
      "valid": true,
      "pid": "0x0100",
      "pusi": true,
      "transport_error": false,
      "scrambling_control": 0,
      "adaptation_field_control": 3,
      "continuity_counter": 1,
      "pcr_ms": 860
    },
    {
      "offset_bytes": 30080,
      "valid": true,
      "pid": "0x0100",
      "pusi": false,
      "transport_error": false,
      "scrambling_control": 0,
      "adaptation_field_control": 1,
      "continuity_counter": 2
    }
  ],
  "warnings": []
}
```

- Request the next page with `--offset` set to `next_offset_bytes`; it is absent
  at `end_of_file`. Paging this way visits every packet once. `--offset` equal
  to the file size, or an empty file, gives an empty page at `end_of_file`.
- The packet size is detected from the bytes at `--offset`, or from the last
  64 KiB of the file near its end; `--packet-size` can force it. For 192-byte
  M2TS, offsets point at the 4-byte arrival timestamp that starts each packet.
- A packet without a sync byte is listed with `"valid": false`. An incomplete
  last packet ends the page with a warning.

## Packet detail (`--output packet`)

`--format json --output packet --offset N` decodes the packet at offset `N`
(which must be a packet boundary) field by field. Every field carries its
`byte_offset` and `byte_length` within `bytes`, the raw packet in hex, so a
caller can highlight it, and `bits` when the value does not use every bit of
that span (see [Field bits](#field-bits)). Layers are `m2ts_header` (192-byte
packets), `ts_header`, `adaptation_field`, `payload`, and for a packet that
starts a unit either `pes_header` or a section (`pat`, `pmt`, or `psi_section`
for other tables). Table entries are in `rows`. A structure that continues in
the next packet is marked `truncated`, and only fields inside this packet are
listed.

```sh
mpeg-ts-analyzer sample_data/sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts \
    --format json --output packet --offset 29892
```

Abridged output:

```json
{
  "schema_version": "1.0",
  "output": "packet",
  "status": "complete",
  "packet": {
    "offset_bytes": 29892,
    "bytes": "4741003107100000972c7e00000001e0000080c00a310009...",
    "layers": [
      { "name": "ts_header", "fields": [
        { "name": "sync_byte", "value": "0x47", "byte_offset": 0, "byte_length": 1 },
        { "name": "PID", "value": "0x0100", "byte_offset": 1, "byte_length": 2,
          "bits": [ { "bit_offset": 11, "bit_length": 13 } ] }, ... ] },
      { "name": "adaptation_field", "fields": [
        { "name": "program_clock_reference", "value": "23220000 (0.860000 s)", "byte_offset": 6, "byte_length": 6,
          "bits": [ { "bit_offset": 48, "bit_length": 33 }, { "bit_offset": 87, "bit_length": 9 } ] }, ... ] },
      { "name": "payload", "fields": [
        { "name": "payload", "value": "176 bytes", "byte_offset": 12, "byte_length": 176 } ] },
      { "name": "pes_header", "fields": [
        { "name": "PTS", "value": "144000 (1.600000 s)", "byte_offset": 21, "byte_length": 5,
          "bits": [ { "bit_offset": 172, "bit_length": 3 }, { "bit_offset": 176, "bit_length": 15 },
                    { "bit_offset": 192, "bit_length": 15 } ] },
        { "name": "DTS", "value": "140400 (1.560000 s)", "byte_offset": 26, "byte_length": 5,
          "bits": [ { "bit_offset": 212, "bit_length": 3 }, { "bit_offset": 216, "bit_length": 15 },
                    { "bit_offset": 232, "bit_length": 15 } ] }, ... ] }
    ]
  },
  "unit": {
    "kind": "pes",
    "first_offset_bytes": 29892,
    "position": 1,
    "packets_in_range": 3,
    "next_offset_bytes": 30080,
    "stream_id": "0xe0",
    "pts_ms": 1600,
    "dts_ms": 1560,
    "header_packet_offsets": [29892]
  },
  "warnings": []
}
```

`unit` describes the PES packet or PSI section the packet belongs to, searched
among packets of the same PID within the scan range `--range-offset` /
`--range-limit` (default: the whole file): its first packet (or
`started_before_range`), the packet's `position` and the unit's
`packets_in_range`, and the previous and next member packets. A long video PES
can span thousands of packets, so the full member list is not returned. For a
PES, `stream_id` and PTS/DTS come from its header, gathered across the first
packets when the header does not fit in one (`header_packet_offsets`).
Packets without payload, such as adaptation-field-only packets, carry no unit
bytes: they are not counted as members, and for such a packet (or a null
packet) `unit.kind` is `none`.

### Field bits

Many fields occupy only part of a byte or cross byte boundaries. For those,
`bits` lists the exact bits the value was read from as one or more
`{ "bit_offset", "bit_length" }` ranges, in wire order. Bit offsets are
packet-relative and count from the most significant bit: offset 0 is bit 7 of
byte 0, and bit `b` (7 = MSB, 0 = LSB) of byte `i` is at offset
`i * 8 + (7 - b)`. In a 192-byte packet every TS offset is 32 bits later,
because the 4-byte `m2ts_header` comes first. Reading the ranges in order and
concatenating their bits gives the value.

`bits` is omitted when the value uses every bit of its `byte_offset` /
`byte_length` span (`sync_byte`, `stream_id`, `CRC_32`, `payload`, ...);
consumers then treat all bits of the span as the field. `byte_offset` and
`byte_length` are always present and always cover the ranges.

In the example above, the 13-bit PID uses the low 5 bits of byte 1 and all of
byte 2 (`bit_offset` 11, `bit_length` 13); `payload_unit_start_indicator` is
the single bit at offset 9 and `continuity_counter` is offsets 28–31. A value
that is split by marker or reserved bits has several ranges:

- `PTS` / `DTS`: the 33 value bits only, as 3 + 15 + 15 bits. The 4-bit prefix
  (`0010`, `0011` or `0001`) and the three marker bits are left out, so the
  ranges of the PTS at byte 21 are 172+3, 176+15 and 192+15.
- `program_clock_reference` / `original_program_clock_reference`: two ranges,
  the 33-bit `program_clock_reference_base` and the 9-bit
  `program_clock_reference_extension`, skipping the 6 reserved bits between
  them. The value is `base * 300 + extension` (27 MHz units), not the
  concatenation of the two ranges.
- `arrival_time_stamp` (192-byte packets): the low 30 bits of the 4-byte
  header, after the 2-bit `copy_permission_indicator`.

Fields with `bits` include the TS header flags, PID, scrambling control,
`adaptation_field_control` and `continuity_counter`; the `m2ts_header` fields;
the adaptation field flags and clock references; the PES
`PES_scrambling_control`, `PTS_DTS_flags`, PTS and DTS; and the section
`section_syntax_indicator`, `section_length`, `version_number`,
`current_next_indicator`, PAT `network_PID` / `program_map_PID`, PMT `PCR_PID`,
`program_info_length`, `elementary_PID` and `ES_info_length`.

## Progress events

`--progress json` writes one JSON object per line to stderr while the input is
read, so a caller can show how far an analysis has got. stdout is unchanged and
still carries only the result, in text or JSON mode.

```sh
$ mpeg-ts-analyzer large.ts --format json --progress json > report.json
{"type":"progress","processed_bytes":134217728,"total_bytes":283761748}
{"type":"progress","processed_bytes":254832872,"total_bytes":283761748}
{"type":"progress","processed_bytes":283761748,"total_bytes":283761748,"done":true}
```

- `total_bytes` is the size of the analyzed range: the file size minus
  `--offset`, capped by `--limit`.
- `processed_bytes` is the furthest position read within that range. It never
  decreases, even when the analyzer seeks back to rediscover PAT/PMT.
- Progress applies to the summary and `analysis` outputs, which scan a range;
  `packets` and `packet` read only a few packets and write no events.
- Events are written at most every 250 ms. A final event with `"done": true`
  follows when reading ends; on failure, the normal error handling follows it.
- `done` means the operation has ended, not that the whole range was read. An
  operation that has its answer early, such as listing the programs of a
  multi-program stream, ends with `processed_bytes` below `total_bytes`.
- Lines carry a `type` field so they can be told apart from other diagnostics on
  stderr. Without `--progress`, nothing is written.

## Capabilities

`--capabilities` prints, without reading an input file, which machine-readable
outputs this build supports, so a calling program (such as a GUI) can reject an
incompatible CLI before running an analysis:

```sh
$ mpeg-ts-analyzer --capabilities
{
  "capabilities_version": "1.0",
  "cli_version": "2.3.0",
  "outputs": [
    {
      "name": "summary",
      "format": "json",
      "schema_version": "1.0"
    },
    {
      "name": "analysis",
      "format": "json",
      "schema_version": "1.0"
    },
    {
      "name": "packets",
      "format": "json",
      "schema_version": "1.0"
    },
    {
      "name": "packet",
      "format": "json",
      "schema_version": "1.0"
    }
  ],
  "progress_formats": [
    "json"
  ]
}
```

`outputs` lists each output with its schema version; `summary` is the `--format
json` report, `analysis` the range analysis, and `packets` / `packet` the
packet documents described above. New outputs and fields may be added; existing
entries keep their meaning. Passing an input file or any other option together
with `--capabilities` is an error.

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

## Dump TS header

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --dump-ts-header
Input file:  sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
===============================================================
 TS Header
===============================================================
transport_error_indicator       : 0
payload_unit_start_indicator    : 1
transport_priority              : 0
pid                             : 0x0
transport_scrambling_control    : 0
adaptation_field_control        : 1
continuity_counter              : 0
===============================================================
 TS Header
===============================================================
transport_error_indicator       : 0
payload_unit_start_indicator    : 1
transport_priority              : 0
pid                             : 0x1000
transport_scrambling_control    : 0
adaptation_field_control        : 1
continuity_counter              : 0
===============================================================
 TS Header
===============================================================
transport_error_indicator       : 0
payload_unit_start_indicator    : 1
transport_priority              : 0
pid                             : 0x100
transport_scrambling_control    : 0
adaptation_field_control        : 3
continuity_counter              : 0
```

## Dump PSI

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --dump-psi
Input file:  sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Packet size: 188 bytes
Detected PAT: 1 program(s)

===========================================
 PAT
===========================================
PAT : table_id                  : 0x0
PAT : section_syntax_indicator  : 1
PAT : section_length            : 13
PAT : transport_stream_id       : 1
PAT : version_number            : 0
PAT : current_next_indicator    : 1
PAT : section_number            : 0
PAT : last_section_number       : 0
PAT : program_number            : 1
PAT : program_map_PID           : 0x1000
PAT : CRC_32                    : 2ab104b2
Detected PMT

===========================================
 PMT
===========================================
PMT : table_id                          : 0x2
PMT : section_syntax_indicator          : 1
PMT : section_length                    : 29
PMT : program_number                    : 1
PMT : version_number                    : 0
PMT : current_next_indicator            : 1
PMT : section_number                    : 0
PMT : last_section_number               : 0
PMT : PCR_PID                           : 0x100
PMT : program_info_length               : 0
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x02 (13818-2 video or 11172-2 constrained parameter video stream)
PMT : Program Info : elementary_PID     : 0x101, stream_type : 0x03 (11172 audio)
PMT :   descriptor                      : ISO 639 language descriptor
PMT :     language_code                 : eng
PMT :     audio_type                    : 0x00 (undefined)
PMT : CRC_32                            : 11625f80
```

Descriptors found in a program's ES info loop are decoded and printed under the
corresponding `Program Info` line (as seen above for the audio stream's ISO 639
language descriptor). Other descriptor types are decoded similarly; for example
an AVC video descriptor on an H.264 stream is dumped as:

```
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x1b (AVC video stream as defined in ITU-T Rec. H.264|ISO/IEC 14496-10 Video)
PMT :   descriptor                      : AVC video descriptor
PMT :     profile_idc                   : 100 (High)
PMT :     level_idc                     : 40 (4.0)
```

mpeg-ts-analyzer decodes a curated set of the most commonly encountered
descriptors rather than aiming for exhaustive coverage. Descriptors outside this
set are still reported by tag with their raw payload, so nothing is silently
dropped.

Decoded descriptors, grouped by the standard that defines them (the
descriptor_tag range decides which one applies):

- ISO/IEC 13818-1 (ITU-T H.222.0), clause 2.6 — registration (0x05),
  ISO 639 language (0x0A), AVC video (0x28), HEVC video (0x38)
- ETSI EN 300 468 (DVB), clause 6.2 — teletext (0x56), subtitling (0x59),
  AAC (0x7C)

## Dump timestamp

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --dump-timestamp
Input file:  sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Packet size: 188 bytes
Detected PAT: 1 program(s)
Detected PMT
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x02 (13818-2 video or 11172-2 constrained parameter video stream)
PMT : Program Info : elementary_PID     : 0x101, stream_type : 0x03 (11172 audio)
0x000034e0 PCR: 0x018344a0[00940.000000ms]
0x000034e0 DTS: 0x00024090[01640.000000ms] (pid:0x100) (delay:700.000000ms)
0x0000359c PTS: 0x00023a3a[01621.977778ms] (pid:0x101) (delay:668.922222ms)
0x00004970 PCR: 0x01a43a20[01020.000000ms] (Interval:00080.000000ms)
0x00003f28 DTS: 0x00024ea0[01680.000000ms] (pid:0x100) (delay:557.222222ms)
0x00004970 DTS: 0x00025cb0[01720.000000ms] (pid:0x100) (delay:700.000000ms)
0x00003fe4 PTS: 0x00024b1a[01669.977778ms] (pid:0x101) (delay:534.144444ms)
0x000055ec PCR: 0x01c52fa0[01100.000000ms] (Interval:00080.000000ms)
0x00004ba4 DTS: 0x00026ac0[01760.000000ms] (pid:0x100) (delay:709.400000ms)
0x00004c60 PTS: 0x00025bfa[01717.977778ms] (pid:0x101) (delay:657.177778ms)

(snip)

0x0004a66c PCR: 0x091bd920[05660.000000ms] (Interval:00080.000000ms)
0x00049c24 DTS: 0x0008ade0[06320.000000ms] (pid:0x100) (delay:729.563591ms)
0x00049ce0 PTS: 0x00089f1a[06277.977778ms] (pid:0x101) (delay:684.062566ms)
0x0004a728 PTS: 0x0008affa[06325.977778ms] (pid:0x101) (delay:662.486106ms)
0x0004b0b4 PTS: 0x0008c0da[06373.977778ms] (pid:0x101) (delay:665.094372ms)
-----------------------------
Compliance Check Results:
Max PCR interval: 80.000000ms [OK, limit: <= 100.000000ms]
PCR-PTS/DTS max gap: 726.666667ms [OK, limit: <= 1000.000000ms]
```

## Dump PCR jitter

`--dump-pcr-jitter` reports **PCR jitter** — the error in each PCR *value*, i.e.
how far the stamped clock is from the time that byte was actually delivered. This
is a different concern from the PCR *interval* check above (how often a PCR
appears): a PCR can arrive on time yet carry a wrong value, which upsets the
decoder's clock recovery (audio pops, video stutter).

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --dump-pcr-jitter
...
-----------------------------
PCR Jitter Summary (per-interval, byte-position model):
  (file-based estimate; assumes ~constant delivery rate, not a TR 101 290 measurement)
  Samples          : 60 PCR in 1 segment(s)
  Measured         : 58 interior samples
  Max jitter       : +23.333333ms at 0x00026244
  Min jitter       : -28.679245ms at 0x00008664
  Avg |jitter|     : 16.822812ms
  Within +/-500ns  : 0.0% (ISO/IEC 13818-1 accuracy limit)
  Status           : NG (max |jitter| exceeds the +/-25 microsec DVB limit)
  Discontinuities  : 0
```

Jitter magnitudes are printed in milliseconds (six decimals, so 1 ns is resolvable).
`Status` grades the worst-case `|jitter|` against the two standard limits:

- **OK** — within ±500 ns (ISO/IEC 13818-1 accuracy limit)
- **WARNING** — beyond ±500 ns but within ±25 µs (DVB receiver tolerance)
- **NG** — beyond ±25 µs

The result is a file-based estimate (there are no real arrival timestamps in a
file), not a hardware TR 101 290 measurement, as noted in the output header.

> The bundled sample shows large jitter because its PCR PID also carries VBR video,
> so the byte rate between PCRs varies a lot — not a clean CBR mux. A compliant
> CBR broadcast multiplex reports sub-microsecond jitter.

## Dump bitrate

`--dump-bitrate` summarizes how bandwidth is distributed across the analyzed
program's PIDs: the **average** and **peak** bitrate of each elementary stream,
the PSI overhead (PAT/PMT), and the program total. It answers questions like
"which stream is eating the bandwidth", "does the video meet its CBR/VBR target",
and "where does a live HLS/DASH pipeline buffer".

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --dump-bitrate
...
-----------------------------
Bitrate Summary (PCR time base):
  PID     Type                                                                  Avg(bps)    Peak/1s(bps)
  0x0000  PAT                                                                     12,427          13,536
  0x0100  13818-2 video or 11172-2 constrained parameter video stream             59,905          60,160
  0x0101  11172 audio                                                            405,952         430,144
  0x1000  PMT                                                                     12,427          13,536
          Total (program)                                                        490,711         514,368
  0x1fff  NULL (mux-wide)                                                              0               0
  Duration: 4.72s (PCR, 1 segment(s))
```

Reading the output:

- **Avg** is the mean bitrate over the measured `Duration`; **Peak/1s** is the
  busiest single 1-second window (`N/A` for streams shorter than one second).
- **Total (program)** sums the analyzed program's PIDs. **NULL** (`0x1FFF`)
  stuffing is mux-wide, so it is listed separately and left out of the total.
- Byte counts are TS-layer: 188 bytes per packet, including for 192-byte M2TS.
- The segment count next to the duration reflects PCR discontinuities found in
  the stream.
