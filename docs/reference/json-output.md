# Machine-readable output

[Documentation home](../index.md)

Use `--format json` when the result will be consumed by CI, monitoring, or
another program:

```sh
mpeg-ts-analyzer input.ts --format json
mpeg-ts-analyzer input.ts --format json --dump-pcr-jitter --dump-bitrate
```

JSON mode writes exactly one JSON document to stdout. Errors and Cobra usage
diagnostics go to stderr, so redirecting or piping stdout remains safe. A failed
analysis still emits a valid document with `status: "failed"` and a
stable `reason` code plus a human-readable `error`, then exits with the usual
non-zero status. Multi-program
listing and insufficient-data checks are represented separately by
`status: "programs_listed"` and per-check `skipped` states. An `incomplete`
result exits 0 for compatibility with text mode; automation should require
`status == "complete"` rather than relying on the exit code alone.

For plotting, `--output analysis` analyzes every program of the range in one
pass and adds per-program time series (PCR intervals, PTS/DTS and PCR gaps,
jitter, 1-second bitrate windows) under its own schema, plus the PSI layout of
the range (`psi_layout`: PAT/PMT sections, the layouts in effect and their byte
intervals, and every observed PID with what it carries); see
[Range analysis output](#range-analysis-output---output-analysis) and
[PSI layout of the range](#psi-layout-of-the-range). `--output
packets` pages through packets (with the headers of the PSI sections that
start in each packet) and `--output packet` decodes one packet with
the byte position of every field, plus the exact bits of fields that do not
fill whole bytes ([Packet pages](#packet-pages---output-packets)).

The document contains summaries, not one object per transport packet. Its size
therefore does not grow with the input file's packet count (apart from naturally
variable summary collections such as encountered PIDs, warnings, and detected
anomalies). Analysis remains streaming, making JSON suitable for large files.

Long-running callers can add `--progress json` to receive progress events on
stderr, and `mpeg-ts-analyzer --capabilities` reports which machine-readable
outputs and schema versions a build supports. See
[Progress events](#progress-events).
Packet-level flags (`--dump-ts-header`, `--dump-ts-payload`,
`--dump-adaptation-field`, `--dump-psi`, `--dump-pes-header`, and
`--dump-timestamp`) are intentionally incompatible with JSON mode; their large,
human-oriented output remains available in the default text mode.

PID values are zero-padded hexadecimal strings such as `"0x0100"`. Numeric
units are part of field names, for example `value_ms`, `offset_bytes`, and
`average_bps`. The top-level `schema_version` of the summary and analysis
documents is currently `"2.0"` (packet documents: `"1.0"`). Within one
schema version, new fields may be added, but existing fields will not be removed
or change type or enum meaning. New enum values may be added, so consumers must
treat unknown values as unrecognized. Removing or retyping a field, or changing
an existing enum's meaning, requires a schema-version increment.
See [JSON output](#json-output) for an annotated example and
[Changes in schema 2.0](#changes-in-schema-20) for what changed from 1.0.
The machine-readable contract is published as
[`docs/json-schema-v2.json`](../json-schema-v2.json) (JSON Schema Draft
2020-12).

## JSON output

`--format json` emits one deterministic, indented JSON document on stdout. The
same analysis is performed as in text mode; `--dump-pcr-jitter` and
`--dump-bitrate` add their structured summaries. Object fields have a stable
order, PID collections are sorted numerically, and PID values use zero-padded
hexadecimal strings. Diagnostics and the final error message use stderr. The
formal Draft 2020-12 specification is
[`docs/json-schema-v2.json`](../json-schema-v2.json).

```sh
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --format json
{
  "schema_version": "2.0",
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
      "service": {
        "service_type": "0x01",
        "service_type_description": "digital television service",
        "provider_name": "FFmpeg",
        "name": "Service01"
      },
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
  "transport_errors": {
    "status": "ok",
    "packet_count": 0,
    "excluded_packet_count": 0,
    "pids": []
  },
  "scrambled_packets": {
    "status": "ok",
    "packet_count": 0,
    "excluded_packet_count": 0,
    "pids": []
  },
  "psi_health": {
    "status": "ok",
    "repetition_limit_ms": 500,
    "time_base": {
      "status": "pcr",
      "program_number": 1,
      "pcr_pid": "0x0100",
      "segment_count": 1
    },
    "tables": [
      {
        "table": "pat",
        "pid": "0x0000",
        "status": "ok",
        "reasons": [],
        "instance_count": 43,
        "first_instance_offset_bytes": 188,
        "version_change_count": 0,
        "crc_error_count": 0,
        "malformed_section_count": 0,
        "incomplete_count": 0,
        "wrong_table_id_count": 0,
        "unusable_packet_count": 0,
        "repetition": {
          "status": "ok",
          "interval_count": 40,
          "max_interval_ms": 150.22222222222223,
          "max_interval_end_offset_bytes": 131976,
          "late_interval_count": 0
        }
      },
      {
        "table": "pmt",
        "program_number": 1,
        "pid": "0x1000",
        "status": "ok",
        "reasons": [],
        "announced_offset_bytes": 188,
        "instance_count": 43,
        "first_instance_offset_bytes": 376,
        "version_change_count": 0,
        "crc_error_count": 0,
        "malformed_section_count": 0,
        "incomplete_count": 0,
        "wrong_table_id_count": 0,
        "unusable_packet_count": 0,
        "repetition": {
          "status": "ok",
          "interval_count": 40,
          "max_interval_ms": 148.44444444444446,
          "max_interval_end_offset_bytes": 132164,
          "late_interval_count": 0
        }
      }
    ]
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
| `pcr_jitter.status` | `ok`, `warning`, `ng`, `not_assessable`, `skipped` |
| `pcr_jitter.estimate.model` | `byte_position` |
| `pcr_jitter.delivery.assessment` | `constant`, `variable`, `unknown` |
| `bitrate.status` | `ok`, `skipped` |
| `continuity_counter.status` | `ok`, `errors` |
| `transport_errors.status`, `scrambled_packets.status` | `ok`, `affected`, `data_excluded` |
| `psi_health.status`, `psi_health.tables[].status` | `ok`, `ng`, `insufficient_data` |
| `psi_health.tables[].reasons[]` | `missing`, `late`, `crc_error`, `malformed_section`, `incomplete`, `wrong_table_id`, `unusable_packets` |
| `psi_health.tables[].repetition.status` | `ok`, `late`, `insufficient_data`, `skipped` |
| `psi_health.tables[].repetition.reason` | `no_complete_interval`, `no_pcr_time_base` |
| `psi_health.time_base.status` | `pcr`, `none` |
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

`programs[].service` is the program's service from the DVB SDT, when the
stream carries one; see [Service names (SDT)](../cli/psi.md#service-names-sdt).

When the PAT or the selected program's PMT changes version during the
analysis, `psi_transitions` lists each applied change; `programs` keeps the
layout the analysis started with. The field is absent when no change was
applied. See [PAT and PMT version changes](../cli/psi.md#pat-and-pmt-version-changes).

In a bitrate result, `pids` contains only selected-program PIDs and
`program_average_bps` / `program_peak_1s_bps` aggregate those rows. Mux-wide
NULL stuffing is reported separately as `null_pid` and is excluded from the
program totals.

JSON contains aggregate results only, not per-packet dumps, so output size does
not scale with every packet in a large input. Packet-level dump flags are
rejected with `--format json` instead of mixing human text into stdout. Schema
`2.x` may gain fields and enum values. Consumers must treat unknown enum values
as unrecognized. Existing fields are not removed or retyped, and existing enum
meanings do not change; those incompatible changes increment `schema_version`.

## Transport errors and scrambled packets

Every complete analysis (summary and `--output analysis`) has
`transport_errors` (packets with `transport_error_indicator` set) and
`scrambled_packets` (packets with a non-zero `transport_scrambling_control`),
both for the whole multiplex. They count every packet of the range from its
first packet, including the packets read while the first PAT and PMT were
acquired and the null PID; they are absent when no analysis ran (`incomplete`,
`failed`, `programs_listed`). For
`sample_data/sample_188byte_transport_errors.ts`:

```json
{
  "transport_errors": {
    "status": "data_excluded",
    "packet_count": 6,
    "excluded_packet_count": 5,
    "first_offset_bytes": 0,
    "pids": [
      {
        "pid": "0x0000",
        "label": "PAT",
        "packet_count": 1,
        "excluded_packet_count": 1,
        "first_offset_bytes": 7332
      },
      {
        "pid": "0x0100",
        "label": "video",
        "packet_count": 2,
        "excluded_packet_count": 2,
        "first_offset_bytes": 5640
      },
      {
        "pid": "0x0101",
        "label": "audio",
        "packet_count": 1,
        "excluded_packet_count": 1,
        "first_offset_bytes": 4700
      },
      {
        "pid": "0x1000",
        "label": "PMT",
        "packet_count": 1,
        "excluded_packet_count": 1,
        "first_offset_bytes": 8084
      },
      {
        "pid": "0x1fff",
        "label": "null",
        "packet_count": 1,
        "excluded_packet_count": 0,
        "first_offset_bytes": 0
      }
    ]
  },
  "scrambled_packets": {
    "status": "data_excluded",
    "packet_count": 6,
    "excluded_packet_count": 5,
    "first_offset_bytes": 1316,
    "pids": [
      {
        "pid": "0x0000",
        "label": "PAT",
        "packet_count": 1,
        "excluded_packet_count": 1,
        "first_offset_bytes": 11092
      },
      {
        "pid": "0x0100",
        "label": "video",
        "packet_count": 2,
        "excluded_packet_count": 2,
        "first_offset_bytes": 9400
      },
      {
        "pid": "0x0101",
        "label": "audio",
        "packet_count": 1,
        "excluded_packet_count": 1,
        "first_offset_bytes": 8460
      },
      {
        "pid": "0x0200",
        "label": "unknown",
        "packet_count": 1,
        "excluded_packet_count": 0,
        "first_offset_bytes": 1316
      },
      {
        "pid": "0x1000",
        "label": "PMT",
        "packet_count": 1,
        "excluded_packet_count": 1,
        "first_offset_bytes": 11844
      }
    ]
  }
}
```

- `status`: `ok` when no packet has the condition; `affected` when packets
  have it but none carried data the analysis reads, so the other results are
  complete; `data_excluded` when data of at least one affected packet was left
  out of the analysis, so the other results are based on less data than the
  stream carried.
- `packet_count` and `excluded_packet_count` are numbers of packets.
  `first_offset_bytes` is the byte offset of the first affected packet (for
  M2TS, of its `TP_extra_header`); it is absent when `packet_count` is 0.
- `pids` lists each affected PID once, in numeric order, with its own counts
  and first offset; it is empty when nothing is affected. `label` is the PID's
  role in the applied layouts (`PAT`, `PMT`, `PCR`, `video`, `audio`,
  `stream_type 0xNN`), `null` for `0x1fff`, or `unknown`.
- An affected packet is excluded when it is on a PID whose data the analysis
  reads at that point: the PAT, the PMT or an elementary stream of an analyzed
  program, or, for a transport error only, its `PCR_PID` (a PCR in a packet
  with a transport error is not used; the clear adaptation field of a
  scrambled packet is). Null packets, unreferenced PIDs, scrambled packets
  without payload and packets read during the PAT/PMT acquisition are never
  excluded. A packet with both conditions counts in both objects.

The payload of an affected packet is never parsed as PSI or PES, so it
contributes no PSI transition, PES header, PTS/DTS, timestamp anomaly or
PCR-PTS/DTS gap; the PAT/PMT warnings it causes are listed in `warnings`. See
[Transport errors and scrambled packets](../cli/usage.md#transport-errors-and-scrambled-packets-always-on)
for the complete policy. Neither object takes part in `--fail-on-error`.

## PAT and PMT health

Every complete analysis (summary and `--output analysis`) has `psi_health`:
the availability and repetition of the PAT and of the PMT of every program the
applied PAT announces, including programs that are not analyzed, from the
first packet of the range. It is absent when no analysis ran. For
`sample_data/sample_188byte_psi_health.ts` with `--program 1`:

```json
{
  "psi_health": {
    "status": "ng",
    "repetition_limit_ms": 500,
    "time_base": {
      "status": "pcr",
      "program_number": 1,
      "pcr_pid": "0x0100",
      "segment_count": 1
    },
    "tables": [
      {
        "table": "pat",
        "pid": "0x0000",
        "status": "ng",
        "reasons": [
          "late",
          "wrong_table_id"
        ],
        "instance_count": 12,
        "first_instance_offset_bytes": 188,
        "version_change_count": 0,
        "crc_error_count": 0,
        "malformed_section_count": 0,
        "incomplete_count": 0,
        "wrong_table_id_count": 1,
        "unusable_packet_count": 0,
        "first_problem_offset_bytes": 11092,
        "repetition": {
          "status": "late",
          "interval_count": 8,
          "max_interval_ms": 1200,
          "max_interval_end_offset_bytes": 18612,
          "late_interval_count": 1,
          "first_late_end_offset_bytes": 18612
        }
      },
      {
        "table": "pmt",
        "program_number": 1,
        "pid": "0x1000",
        "status": "ng",
        "reasons": [
          "crc_error"
        ],
        "announced_offset_bytes": 188,
        "instance_count": 15,
        "first_instance_offset_bytes": 564,
        "version_change_count": 1,
        "crc_error_count": 1,
        "malformed_section_count": 0,
        "incomplete_count": 0,
        "wrong_table_id_count": 0,
        "unusable_packet_count": 0,
        "first_problem_offset_bytes": 11844,
        "repetition": {
          "status": "ok",
          "interval_count": 12,
          "max_interval_ms": 400,
          "max_interval_end_offset_bytes": 13724,
          "late_interval_count": 0
        }
      },
      {
        "table": "pmt",
        "program_number": 2,
        "pid": "0x1010",
        "status": "ng",
        "reasons": [
          "missing",
          "incomplete"
        ],
        "announced_offset_bytes": 188,
        "instance_count": 0,
        "version_change_count": 0,
        "crc_error_count": 0,
        "malformed_section_count": 0,
        "incomplete_count": 1,
        "wrong_table_id_count": 0,
        "unusable_packet_count": 0,
        "first_problem_offset_bytes": 28012,
        "repetition": {
          "status": "late",
          "interval_count": 0,
          "max_interval_ms": 2840,
          "max_interval_end_offset_bytes": 27824,
          "late_interval_count": 1,
          "first_late_end_offset_bytes": 27824
        }
      }
    ]
  }
}
```

- `status` is the worst table status: `ng` before `insufficient_data` before
  `ok`. `repetition_limit_ms` is 500, the value of ETSI TR 101 290 V1.4.1
  §5.2.1 indicators 1.3.a (`PAT_error_2`) and 1.5.a (`PMT_error_2`).
  ISO/IEC 13818-1 sets no repetition interval; the table identity, CRC_32 and
  section syntax rules are generic ISO/IEC 13818-1 §2.4.4 requirements.
- `time_base` names the PCR that times the intervals: the reference
  `PCR_PID` (of the selected program, or with `--output analysis` of the first
  analyzed program that has one) and the number of PCR segments. Packet times
  are interpolated by byte position between the PCRs around them; no interval
  spans a segment boundary. `status` is `none` without two PCRs in one segment,
  and every `repetition.status` is then `skipped`.
- `tables` lists the PAT, then one entry per `program_number` announced in the
  range, in numeric order. A PMT entry has `program_number`,
  `announced_offset_bytes` (the PAT packet that first announced it; the PMT is
  read from the next packet) and, for a program a later PAT removed,
  `withdrawn_offset_bytes`. `pid` is the latest PMT PID.
- `instance_count` counts complete current tables (every section of one
  version valid), at the packet that completes each; `version_change_count`
  counts consecutive tables with different `version_number`.
- The problem counts are numbers of sections or packets; see the reasons in
  [PAT and PMT health](../cli/usage.md#pat-and-pmt-health-always-on).
  `first_problem_offset_bytes` is the earliest of them and is absent without
  problem. `reasons` lists the reasons of an `ng` table in a fixed order and is
  empty otherwise.
- `repetition.interval_count` counts complete intervals (two consecutive
  tables in one PCR segment). `max_interval_ms` is the longest interval,
  including open intervals at the start and end of a segment, which are lower
  bounds of the real one; `max_interval_end_offset_bytes` is where it ends (the
  packet completing the table, or the last PCR of the segment).
  `late_interval_count` counts intervals over the limit and
  `first_late_end_offset_bytes` is where the first one ends. The interval
  fields are absent when nothing was measured. `status` is `ok` with a complete
  interval and none late, `late`, `insufficient_data` (`reason`
  `no_complete_interval`) or `skipped` (`reason` `no_pcr_time_base`).

The size of `psi_health` is bounded by the number of programs announced in the
range. It does not take part in `--fail-on-error`.

## PCR jitter

`--dump-pcr-jitter` (and every `--output analysis` program) adds `pcr_jitter`.
A file has no packet-arrival times, so the jitter of each PCR is **estimated**
from its byte position: the PCR is compared with the value interpolated from
its two neighbors, which is exact only when the multiplex is delivered at a
constant rate. A PCR stamping error and a change of delivery rate look the same
in a file, so the estimate is judged only when the stream itself shows constant
delivery. The object has three parts:

- **`estimate`** — the jitter estimated with one `model`. The only model is
  `byte_position`; validated M2TS arrival timestamps may add another one later.
  `pcr_pids` lists the PIDs the PCRs came from (several when a PMT version
  changes the `PCR_PID`), `observations` the PCRs that have a neighbor on both
  sides in their segment, and `max_abs_jitter_ns` / `max_abs_jitter_offset_bytes`
  the worst value the status is judged on, next to the signed extremes, the
  average `|jitter|`, the share within ±500 ns and the PCR discontinuities that
  split the samples into segments. The value fields are absent when no
  estimate could be made. `series.pcr_jitter` of `--output analysis` holds the
  estimate of every observation.
- **`delivery`** — whether the stream supports the constant-delivery premise.
  The PCR intervals of each timing segment are grouped into consecutive windows
  of at least `window_seconds` (1 s) of PCR time; each window starts and ends on
  a PCR packet, so its multiplex rate (transport-stream bytes of all PIDs over
  PCR time) is exact. `assessment` is
  - `variable` when, with at least two windows, a window's rate deviates from
    the range average (`average_rate_bps`) by more than `tolerance_percent`
    (1 %), or when the estimate reaches more than `plausible_jitter_limit_ns`
    (1 ms): a "jitter" that is a sizeable fraction of a PCR interval is not a
    plausible PCR error, only a varying delivery rate explains it;
  - `unknown` when there are fewer than two windows (a range shorter than about
    two seconds of PCR time) and the estimate is plausible;
  - `constant` otherwise.

  `window_count`, `min_window_rate_bps` / `max_window_rate_bps` and
  `max_deviation_percent` (present from two windows on) are the evidence, and
  `reason` explains the assessment. Neither the file extension nor null packets
  are used as evidence. A constant-bitrate multiplex measures 0.00 %; the
  bundled VBR samples deviate by about 3 % and longer VBR encodes by more than
  10 %.
- **`status`** — `ok` (max `|jitter|` within ±500 ns, the ISO/IEC 13818-1 PCR
  accuracy), `warning` (within ±25 µs, the DVB receiver tolerance) or `ng`
  (beyond), **only when `delivery.assessment` is `constant`**. With a
  `variable` or `unknown` delivery the status is `not_assessable`, and `reason`
  says why; the estimate is still reported. `skipped` means no estimate could be
  made (fewer than 3 PCRs, no segment of 3 PCRs, or more than 30 % of the PCRs
  starting a new segment) and takes precedence over `not_assessable`.

PCR jitter is not part of `--fail-on-error`, which covers only the
`compliance` checks.

The deterministic diagnostic stream `sample_data/sample_188byte_all_findings.ts`
(2.4 s at a constant 75.2 kbit/s, one PCR 100 µs late) gives:

```sh
$ ./mpeg-ts-analyzer sample_data/sample_188byte_all_findings.ts --format json --dump-pcr-jitter
```

```json
"pcr_jitter": {
  "status": "ng",
  "estimate": {
    "model": "byte_position",
    "pcr_pids": [
      "0x0100"
    ],
    "sample_count": 55,
    "segment_count": 1,
    "observations": 53,
    "max_abs_jitter_ns": 100000,
    "max_abs_jitter_offset_bytes": 10904,
    "max_jitter_ns": 100000,
    "max_jitter_offset_bytes": 10904,
    "min_jitter_ns": -50000,
    "min_jitter_offset_bytes": 10528,
    "average_absolute_jitter_ns": 3773.5849056603774,
    "within_500ns_percent": 94.33962264150944,
    "discontinuities": []
  },
  "delivery": {
    "assessment": "constant",
    "reason": "the multiplex rate of 2 one-second windows deviates up to 0.00% from the average 75,200 b/s (tolerance 1%)",
    "window_seconds": 1,
    "window_count": 2,
    "average_rate_bps": 75200,
    "min_window_rate_bps": 75200,
    "max_window_rate_bps": 75200,
    "max_deviation_percent": 0,
    "tolerance_percent": 1,
    "plausible_jitter_limit_ns": 1000000
  }
}
```

The bundled VBR sample is not assessable:

```sh
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --format json --dump-pcr-jitter
```

```json
"pcr_jitter": {
  "status": "not_assessable",
  "reason": "delivery rate is variable, so the byte-position estimate does not measure PCR accuracy",
  "estimate": {
    "model": "byte_position",
    "pcr_pids": [
      "0x0100"
    ],
    "sample_count": 60,
    "segment_count": 1,
    "observations": 58,
    "max_abs_jitter_ns": 28679245.28301893,
    "max_abs_jitter_offset_bytes": 34404,
    "max_jitter_ns": 23333333.333333332,
    "max_jitter_offset_bytes": 156228,
    "min_jitter_ns": -28679245.28301893,
    "min_jitter_offset_bytes": 34404,
    "average_absolute_jitter_ns": 16822812.08050342,
    "within_500ns_percent": 0,
    "discontinuities": []
  },
  "delivery": {
    "assessment": "variable",
    "reason": "the multiplex rate of 4 one-second windows deviates up to 3.06% from the average 496,754 b/s (tolerance 1%)",
    "window_seconds": 1,
    "window_count": 4,
    "average_rate_bps": 496753.8461538461,
    "min_window_rate_bps": 481569.23076923075,
    "max_window_rate_bps": 507600,
    "max_deviation_percent": 3.0567685589519633,
    "tolerance_percent": 1,
    "plausible_jitter_limit_ns": 1000000
  }
}
```

## Changes in schema 2.0

Schema 2.0 (summary and `--output analysis`) changes only `pcr_jitter`; the
packet documents stay at 1.0. A 1.x consumer must be updated:

| Schema 1.0 | Schema 2.0 |
| --- | --- |
| `pcr_jitter.status` `ok` / `warning` / `ng` for every estimate | the same values only when `pcr_jitter.delivery.assessment` is `constant`; otherwise `not_assessable` |
| `sample_count`, `segment_count`, `discontinuities` | `estimate.sample_count`, `estimate.segment_count`, `estimate.discontinuities` |
| `measured_sample_count` | `estimate.observations` |
| `max_jitter_ns`, `min_jitter_ns` and their offsets, `average_absolute_jitter_ns`, `within_500ns_percent` | the same names under `estimate`, plus `estimate.max_abs_jitter_ns` / `max_abs_jitter_offset_bytes` |
| — | `estimate.model`, `estimate.pcr_pids`, `delivery` |

Everything else, including the `series.pcr_jitter` points, is unchanged.
Within 2.0, `transport_errors`, `scrambled_packets` and `psi_health` were added
to both documents (additive changes).
`--capabilities` reports the schema version of each output, so a caller can
tell the two apart before running an analysis.

## Range analysis output (`--output analysis`)

`--format json --output analysis` analyzes **every program** of the range in a
single pass and adds time series, for callers that plot results (such as a GUI)
rather than only checking verdicts. It is a separate document with its own
schema, [`docs/json-schema-analysis-v2.json`](../json-schema-analysis-v2.json);
the default `--output summary` report is unchanged.

```sh
mpeg-ts-analyzer capture.ts --format json --output analysis --offset 0 --limit 67108864
```

Abridged output for the two-program sample:

```json
{
  "schema_version": "2.0",
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
  "transport_errors": { "status": "ok", "packet_count": 0, "excluded_packet_count": 0, "pids": [] },
  "scrambled_packets": { "status": "ok", "packet_count": 0, "excluded_packet_count": 0, "pids": [] },
  "psi_health": { "status": "ok", "repetition_limit_ms": 500, "time_base": { ... }, "tables": [ ... ] },
  "programs": [
    {
      "program_number": 1,
      "pmt_pid": "0x1000",
      "pcr_pid": "0x0100",
      "elementary_streams": [ ... ],
      "service": { "service_type": "0x01", "service_type_description": "digital television service", "provider_name": "FFmpeg", "name": "Main" },
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
  "psi_layout": { "sections": [ ... ], "layouts": [ ... ], "pids": [ ... ] },
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
- `transport_errors` and `scrambled_packets` are reported once for the whole
  multiplex, from the first packet of the range, as in the summary
  ([Transport errors and scrambled packets](#transport-errors-and-scrambled-packets)).
- `psi_health` is reported once for the range, from its first packet, with the
  PCR of the first analyzed program that has one as its time base
  ([PAT and PMT health](#pat-and-pmt-health)).
- Bitrate and PCR jitter are always collected in this mode. `series.pcr_jitter`
  is the `byte_position` estimate of each observation; see
  [PCR jitter](#pcr-jitter).
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
- PAT and PMT version changes of the analyzed programs are followed and listed
  in a top-level `psi_transitions` array (see
  [PAT and PMT version changes](../cli/psi.md#pat-and-pmt-version-changes)). A program
  removed by a later PAT has `analysis_end_offset_bytes`. Programs added by a
  later PAT are not analyzed.
- `psi_layout` reports the PAT and PMT sections seen in the range, the layouts
  in effect with the byte interval each applies to, and every PID observed with
  what it carries; see [PSI layout of the range](#psi-layout-of-the-range).

## PSI layout of the range

`--output analysis` adds `psi_layout`, which tells a caller what each PID
carries at any offset of the range, for example to label packets read with
`--output packets` (pages are read without PSI context) or to mark table
updates on a timeline. For
`sample_data/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts`
(see [PAT and PMT version changes](../cli/psi.md#pat-and-pmt-version-changes)) it is
(elementary streams and roles abridged):

```json
"psi_layout": {
  "sections": [
    { "offset_bytes": 188, "last_offset_bytes": 205484, "count": 29, "pid": "0x0000", "table": "pat", "status": "applied",
      "transport_stream_id": 1, "version_number": 0, "section_number": 0, "last_section_number": 0, "current_next_indicator": 1 },
    { "offset_bytes": 376, "last_offset_bytes": 94188, "count": 14, "pid": "0x1000", "table": "pmt", "status": "applied", "program_number": 1, "version_number": 0, ... },
    { "offset_bytes": 102460, "last_offset_bytes": 205672, "count": 15, "pid": "0x1000", "table": "pmt", "status": "applied", "program_number": 1, "version_number": 1, ... },
    { "offset_bytes": 212628, "last_offset_bytes": 309824, "count": 14, "pid": "0x0000", "table": "pat", "status": "applied", "transport_stream_id": 1, "version_number": 1, ... },
    { "offset_bytes": 212816, "last_offset_bytes": 310012, "count": 14, "pid": "0x1100", "table": "pmt", "status": "retransmission", "program_number": 1, "version_number": 1, ... }
  ],
  "layouts": [
    { "offset_bytes": 0, "end_offset_bytes": 102648, "transition_indices": [], "pat_version": 0, "pat_offset_bytes": 188,
      "programs": [ { "program_number": 1, "pmt_pid": "0x1000", "pmt_version": 0, "pmt_offset_bytes": 5076, "pcr_pid": "0x0100",
                      "elementary_streams": [ { "pid": "0x0100", "stream_type": "0x02", ... }, { "pid": "0x0101", "stream_type": "0x03", ... } ] } ] },
    { "offset_bytes": 102648, "end_offset_bytes": 212816, "transition_indices": [ 0 ], "pat_version": 0, "pat_offset_bytes": 188,
      "programs": [ { "program_number": 1, "pmt_pid": "0x1000", "pmt_version": 1, "pmt_offset_bytes": 102460, "pcr_pid": "0x0200",
                      "elementary_streams": [ { "pid": "0x0200", "stream_type": "0x02", ... }, { "pid": "0x0101", "stream_type": "0x04", ... } ] } ] },
    { "offset_bytes": 212816, "end_offset_bytes": 311516, "transition_indices": [ 1 ], "pat_version": 1, "pat_offset_bytes": 212628,
      "programs": [ { "program_number": 1, "pmt_pid": "0x1100", "pmt_version": 1, "pmt_offset_bytes": 102460, "pcr_pid": "0x0200", ... } ] }
  ],
  "pids": [
    { "pid": "0x0000", "packet_count": 43, "first_offset_bytes": 188, "last_offset_bytes": 309824, "roles": [ { "kind": "pat" } ] },
    { "pid": "0x0011", "packet_count": 10, "first_offset_bytes": 0, "last_offset_bytes": 286136, "roles": [ { "kind": "unannounced" } ] },
    { "pid": "0x0100", "packet_count": 70, "first_offset_bytes": 564, "last_offset_bytes": 99640,
      "roles": [ { "kind": "pcr", "program_number": 1 }, { "kind": "elementary_stream", "program_number": 1, "stream_type": "0x02", ... } ] },
    { "pid": "0x0101", "packet_count": 1359, "first_offset_bytes": 2256, "last_offset_bytes": 311328,
      "roles": [ { "kind": "elementary_stream", "program_number": 1, "stream_type": "0x03", ... }, { "kind": "elementary_stream", "program_number": 1, "stream_type": "0x04", ... } ] },
    { "pid": "0x0200", "packet_count": 132, ... },
    { "pid": "0x1000", "packet_count": 29, ... },
    { "pid": "0x1100", "packet_count": 14, ... }
  ]
}
```

- **Range.** Everything in `psi_layout` covers the range from its first packet
  (the first packet boundary at or after `--offset`) to
  `analysis_end_offset_bytes`, including the lead before the first complete
  PAT/PMT, which the rest of the analysis skips.
- **`layouts`** cover that range without gaps, in stream order. A layout
  applies to the packets in `[offset_bytes, end_offset_bytes)`. The first one
  starts at the first packet of the range, before its tables were complete:
  `pat_offset_bytes` and each program's `pmt_offset_bytes` give the packet that
  completed the PAT and the PMT the analysis started from, so a range that does
  not start on PSI shows where the first complete tables were found. Each later
  layout starts at the packet after the change, and `transition_indices` lists
  the entries of `psi_transitions` that started it (several when one packet
  completes PMTs of programs sharing a PID). The layouts are built from the
  tables the analysis applied, so they always agree with `psi_transitions`,
  which keeps the per-change differences. A program without a known PMT (not
  found, or added by a later PAT and therefore not analyzed) has only
  `program_number`, `pmt_pid` and empty `elementary_streams`.
- **`sections`** lists the PAT sections on PID 0x0000 and the PMT sections
  (`table_id` 0x02) on every PMT PID that the first complete PAT or an applied
  PAT change announced, ordered by first occurrence. PMT sections sent on a PID
  before a later PAT announces it are included. Offsets are those of the packet
  in which the section starts. Each entry is a run of copies:
  - A valid section identical to the previous one with the same PID, table,
    `transport_stream_id`/`program_number`, `current_next_indicator` and
    `section_number` is counted in that entry (`count`, `last_offset_bytes`)
    instead of adding one. Copies of other sections on the same PID (another
    program's PMT, another section of a multi-section table) may lie in
    between.
  - An invalid section, or data the section reader had to drop on that PID
    (packet loss, a transport error, a cut section), ends every valid run of
    the PID; the next valid copy starts a new entry.
  - Consecutive invalid copies with the same `invalid_reason` and header form
    one invalid entry; anything else on the PID starts a new one.

  The list therefore grows with table changes and PSI errors, not with
  retransmissions or the range length. `status` is `applied` for the entry
  whose sections the analysis applied (the first complete table or a
  `psi_transitions` change), `retransmission` for a later entry identical to an
  applied section (for example a table repeated on a moved PMT PID, a copy
  after an error, or a late replay of a superseded version, which is not
  applied again), `not_applied` for a valid section that never became part of
  the layout (an incomplete or conflicting version, a "next" table, or the PMT
  of a program that is not analyzed), and `invalid` with an `invalid_reason`
  (`malformed`, `invalid_crc32`, `section_number_beyond_last`, `parse_error`).
  Invalid entries carry the header fields when the section has a long-form
  header (always for `invalid_crc32`, `section_number_beyond_last` and
  `parse_error`), for example:

  ```json
  { "offset_bytes": 2820, "last_offset_bytes": 2820, "count": 1, "pid": "0x1000", "table": "pmt", "status": "invalid",
    "invalid_reason": "invalid_crc32", "program_number": 1, "version_number": 0, "section_number": 1, "last_section_number": 1, "current_next_indicator": 1 }
  ```
- **`pids`** lists every PID seen in the range, in PID order, with its packet
  count and first and last packet offsets, and `roles`: what it carries in any
  layout (`pat`, `pmt`, `pcr`, `elementary_stream` with `stream_type`, `null`).
  A PID no layout lists is `unannounced` (for example SDT on 0x0011, or an
  elementary stream of a program whose PMT is not known). To label a packet,
  look up its PID in the layout whose interval contains its offset.
- **Per packet.** A caller that lists packets (such as the GUI's packet list)
  combines the two documents. The layout in effect at the packet's offset gives
  what its PID carries. For a packet that starts a section, take each entry of
  the packet's own `sections` (from `--output packets`) and look for the
  `psi_layout.sections` entry with the packet's PID, a matching table
  (`table_id` 0x00 for `pat`, 0x02 for `pmt`), the same
  `table_id_extension` (`transport_stream_id` or `program_number`),
  `version_number`, `current_next_indicator`, `section_number` and
  `last_section_number`, and `offset_bytes <= offset <= last_offset_bytes`. By
  the run rules above at most one entry matches, and its `status` (including
  `invalid`) is that copy's. No match means the section was not logged: it is
  not a PAT or PMT of an announced PID, it started before the range, or the
  section reader dropped it.
- Size: one section entry per table change or PSI error, one layout per
  applied change, and one entry per PID, independent of the range length and
  of retransmissions.

## Packet pages (`--output packets`)

`--format json --output packets` lists up to `--count` packets (1–10000)
starting at the first packet boundary at or after `--offset`. Packets are read
directly, without PAT/PMT discovery, so a page starts with its first packet even
in the middle of a file. Schema:
[`docs/json-schema-packets-v1.json`](../json-schema-packets-v1.json).

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
- A packet in which PSI sections start has `sections`: for each long-form
  section (`section_syntax_indicator` 1) starting after the `pointer_field`,
  in order, its `table_id`, `table_id_extension`, `version_number`,
  `current_next_indicator`, `section_number` and `last_section_number`. They
  are read from the packet's own bytes, so a page needs no PSI context. Listing
  stops at stuffing (0xFF), a short-form section, a header that does not fit
  in the packet, or a `section_length` below 9, which cannot hold the long-form
  fields and the CRC32; nothing beyond the header is checked (the CRC32 may lie in a
  later packet). The field is absent for every other packet, including packets
  with `transport_error` or a scrambled payload; PES packets never have it.
  For example, the PMT version 1 of the version-change sample:

  ```json
  { "offset_bytes": 102460, "valid": true, "pid": "0x1000", "pusi": true, ..., "continuity_counter": 14,
    "sections": [ { "table_id": "0x02", "table_id_extension": 1, "version_number": 1, "current_next_indicator": 1, "section_number": 0, "last_section_number": 0 } ] }
  ```

  Combined with the PSI layout of `--output analysis` it identifies the table
  each packet carries; see [PSI layout of the range](#psi-layout-of-the-range).

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
  "cli_version": "3.0.0",
  "outputs": [
    {
      "name": "summary",
      "format": "json",
      "schema_version": "2.0"
    },
    {
      "name": "analysis",
      "format": "json",
      "schema_version": "2.0"
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
