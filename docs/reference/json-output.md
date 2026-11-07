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
  "sync": {
    "status": "ok",
    "required_sync_bytes": 5,
    "max_resync_scan_bytes": 1048576,
    "loss_count": 0,
    "resync_count": 0,
    "skipped_bytes": 0,
    "trailing_partial_packet_bytes": 0
  },
  "compliance": {
    "max_pcr_interval": {
      "status": "ok",
      "value_ms": 80,
      "limit_ms": 100,
      "observations": 59
    },
    "pcr_discontinuities": {
      "status": "ok",
      "pcr_count": 60,
      "undeclared_count": 0,
      "jump_limit_ms": 1000,
      "items": []
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
  "pid_references": {
    "time_basis": "pcr_interpolated_file_position",
    "silence_limit_ms": 5000,
    "unreferenced_grace_ms": 500,
    "referenced": {
      "status": "ok",
      "pids": [
        {
          "program_number": 1,
          "role": "pmt",
          "pid": "0x1000",
          "status": "ok",
          "silence_judged": true,
          "packet_count": 43,
          "first_offset_bytes": 376,
          "last_offset_bytes": 310012,
          "referenced_packet_count": 40,
          "referenced_from_offset_bytes": 13536,
          "referenced_until_offset_bytes": 311516,
          "max_silence_ms": 148.444,
          "max_silence_exact": true,
          "max_silence_start_offset_bytes": 123704,
          "max_silence_end_offset_bytes": 132164
        },
        {
          "program_number": 1,
          "role": "pcr",
          "pid": "0x0100",
          "status": "ok",
          "silence_judged": true,
          "packet_count": 202,
          "first_offset_bytes": 564,
          "last_offset_bytes": 304748,
          "referenced_packet_count": 189,
          "referenced_from_offset_bytes": 13536,
          "referenced_until_offset_bytes": 311516,
          "max_silence_ms": 66.667,
          "max_silence_exact": true,
          "max_silence_start_offset_bytes": 123892,
          "max_silence_end_offset_bytes": 126712
        },
        {
          "program_number": 1,
          "role": "elementary_stream",
          "pid": "0x0100",
          "stream_type": "0x02",
          "status": "ok",
          "silence_judged": true,
          "packet_count": 202,
          "first_offset_bytes": 564,
          "last_offset_bytes": 304748,
          "referenced_packet_count": 189,
          "referenced_from_offset_bytes": 13536,
          "referenced_until_offset_bytes": 311516,
          "max_silence_ms": 66.667,
          "max_silence_exact": true,
          "max_silence_start_offset_bytes": 123892,
          "max_silence_end_offset_bytes": 126712
        },
        {
          "program_number": 1,
          "role": "elementary_stream",
          "pid": "0x0101",
          "stream_type": "0x03",
          "status": "ok",
          "silence_judged": true,
          "packet_count": 1359,
          "first_offset_bytes": 2256,
          "last_offset_bytes": 311328,
          "referenced_packet_count": 1307,
          "referenced_from_offset_bytes": 13536,
          "referenced_until_offset_bytes": 311516,
          "max_silence_ms": 26.286,
          "max_silence_exact": true,
          "max_silence_start_offset_bytes": 264704,
          "max_silence_end_offset_bytes": 266772
        }
      ]
    },
    "unreferenced": {
      "status": "ok",
      "pids": []
    }
  },
  "pes_headers": {
    "status": "ok",
    "checked_count": 220,
    "error_count": 0,
    "unchecked_count": 0,
    "errors": [],
    "pids": [
      {
        "program_number": 1,
        "pid": "0x0100",
        "stream_type": "0x02",
        "label": "video",
        "status": "ok",
        "checked_count": 119,
        "error_count": 0,
        "unchecked_count": 0,
        "errors": []
      },
      {
        "program_number": 1,
        "pid": "0x0101",
        "stream_type": "0x03",
        "label": "audio",
        "status": "ok",
        "checked_count": 101,
        "error_count": 0,
        "unchecked_count": 0,
        "errors": []
      }
    ]
  },
  "pts_repetition": {
    "limit_ms": 700,
    "status": "ok",
    "pts_count": 220,
    "interval_count": 214,
    "max_interval_ms": 66.66666666666667,
    "max_interval_start_offset_bytes": 123892,
    "max_interval_end_offset_bytes": 126712,
    "exceeded_count": 0,
    "pids": [
      {
        "program_number": 1,
        "pid": "0x0100",
        "stream_type": "0x02",
        "label": "video",
        "status": "ok",
        "pts_count": 119,
        "interval_count": 117,
        "max_interval_ms": 66.66666666666667,
        "max_interval_start_offset_bytes": 123892,
        "max_interval_end_offset_bytes": 126712,
        "exceeded_count": 0
      },
      {
        "program_number": 1,
        "pid": "0x0101",
        "stream_type": "0x03",
        "label": "audio",
        "status": "ok",
        "pts_count": 101,
        "interval_count": 97,
        "max_interval_ms": 64.88888888888889,
        "max_interval_start_offset_bytes": 124080,
        "max_interval_end_offset_bytes": 126900,
        "exceeded_count": 0
      }
    ]
  },
  "multiplexing": {
    "packet_gaps": {
      "time_basis": "pcr_interpolated_file_position",
      "pids": [
        {
          "pid": "0x0000",
          "references": [],
          "packet_count": 43,
          "status": "info",
          "max_gap_packets": 44,
          "max_gap_start_offset_bytes": 123516,
          "max_gap_end_offset_bytes": 131976,
          "max_gap_ms": 150.222,
          "max_gap_exact": true
        },
        {
          "pid": "0x0011",
          "references": [],
          "packet_count": 10,
          "status": "info",
          "max_gap_packets": 173,
          "max_gap_start_offset_bytes": 93812,
          "max_gap_end_offset_bytes": 126524,
          "max_gap_ms": 551.111,
          "max_gap_exact": true
        },
        {
          "pid": "0x0100",
          "references": [
            {
              "program_number": 1,
              "role": "pcr"
            },
            {
              "program_number": 1,
              "role": "elementary_stream",
              "stream_type": "0x02"
            }
          ],
          "packet_count": 202,
          "status": "info",
          "max_gap_packets": 16,
          "max_gap_start_offset_bytes": 188564,
          "max_gap_end_offset_bytes": 191760,
          "max_gap_ms": 43.871,
          "max_gap_exact": true
        },
        {
          "pid": "0x0101",
          "references": [
            {
              "program_number": 1,
              "role": "elementary_stream",
              "stream_type": "0x03"
            }
          ],
          "packet_count": 1359,
          "status": "info",
          "max_gap_packets": 10,
          "max_gap_start_offset_bytes": 27072,
          "max_gap_end_offset_bytes": 29140,
          "max_gap_ms": 25.778,
          "max_gap_exact": true
        },
        {
          "pid": "0x1000",
          "references": [
            {
              "program_number": 1,
              "role": "pmt"
            }
          ],
          "packet_count": 43,
          "status": "info",
          "max_gap_packets": 44,
          "max_gap_start_offset_bytes": 123704,
          "max_gap_end_offset_bytes": 132164,
          "max_gap_ms": 148.444,
          "max_gap_exact": true
        }
      ]
    }
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
| top-level `reason` | `program_selection_required`, `program_listing_requested`, `end_of_input_before_complete_analysis`, `sync_lost`, `incompatible_options`, `input_open_failed`, `sync_not_acquired`, `analysis_failed` |
| `programs[].status` | `complete`, `incomplete` |
| incomplete program `reason` | `pmt_parse_failed` |
| `sync.status` | `ok`, `recovered`, `lost` |
| `compliance.*.status` | `ok`, `ng`, `skipped` |
| `compliance.pcr_discontinuities.items[].kind` | `wraparound`, `declared`, `pcr_pid_change`, `backward`, `forward_jump` |
| `pcr_jitter.estimate.discontinuities[].kind` | `declared`, `pcr_pid_change`, `backward`, `forward_jump` |
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
| `pid_references.referenced.status`, `pid_references.referenced.pids[].status` | `ok`, `absent`, `silent`, `insufficient_data` |
| `pid_references.unreferenced.status` | `ok`, `unreferenced`, `insufficient_data`, `skipped` |
| `pes_headers.status` (and per PID) | `ok`, `errors`, `insufficient_data` |
| `pes_headers` `reason` | `no_pes`, `untrusted_payload`, `incomplete_headers` |
| `pes_headers.errors[].kind` | `truncated`, `bad_start_code`, `bad_flags`, `inconsistent_length`, `bad_marker_bits` |
| `pts_repetition.status` (and per PID) | `ok`, `ng`, `insufficient_data`, `skipped` |
| `pts_repetition` `reason` | `no_pes`, `untrusted_payload`, `malformed_headers`, `no_pcr_timing`, `no_pts`, `single_pts`, `not_video_or_audio`; totals also `no_video_or_audio` |
| `multiplexing.packet_gaps.pids[].status` | `info`, `insufficient_data` (report only, never a verdict) |
| `timestamp_anomalies.status` | `ok`, `anomalies` |

Top-level and per-program `reason` values, and the `reason` values of
`pes_headers` and `pts_repetition`, are stable codes intended for machine
matching. `error` and analysis-section `reason` values are explanatory text and
must not be matched. `status: "incomplete"` exits 0 for compatibility with text
mode; automation should require `status == "complete"` instead of relying on
the exit code alone.

An input that ends with an incomplete packet, such as a capture cut off
mid-packet, is analyzed up to the last complete packet. The leftover bytes are
reported as a warning at the offset where the incomplete packet starts (in text
mode as a line such as `0x0004c0dc ignored 100 trailing bytes (incomplete packet
at end of input)`) and in `sync.trailing_partial_packet_bytes`, and do not
change the analysis result: an incomplete packet is not a loss of
synchronization. With `--limit`, this applies when the range reaches the end of
the input; a range that ends mid-packet inside the file reports nothing.

`sync` is the TS synchronization result of the analyzed packets; see
[TS synchronization](#ts-synchronization). `status: "incomplete"` with
`reason: "sync_lost"` means the analysis stopped at a loss of synchronization
that could not be recovered; the results cover the input up to there.
`reason: "sync_not_acquired"` (with `status: "failed"`) means the input has no
188-byte or 192-byte packet alignment at all, while `analysis_failed` covers
other failures, such as aligned packets without a PAT.

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
PCR-PTS/DTS gap; the PAT/PMT warnings it causes are listed in `warnings`.
Nothing after the 4-byte TS header of a packet with a transport error is
parsed, so such a packet is counted, with the PID of its header, even when
its `adaptation_field_control` is reserved or its adaptation field is
malformed, and the analysis continues after it. Because the error may also
have changed the header, `pid` is the PID as read from the damaged packet,
not necessarily the stream it belonged to. See
[Transport errors and scrambled packets](../cli/usage.md#transport-errors-and-scrambled-packets-always-on)
for the complete policy. Neither object takes part in `--fail-on-error`.

## PID references

Every complete analysis (summary and `--output analysis`) has `pid_references`
for the whole multiplex: whether the PIDs that the PAT and PMTs reference carry
packets, and whether packets arrive on PIDs that nothing references. It is
absent when no analysis ran (`incomplete`, `failed`, `programs_listed`). For
`sample_data/sample_188byte_pid_references.ts` (see
[Sample data](../development/sample-data.md)):

```json
{
  "pid_references": {
    "time_basis": "pcr_interpolated_file_position",
    "silence_limit_ms": 5000,
    "unreferenced_grace_ms": 500,
    "referenced": {
      "status": "absent",
      "pids": [
        {
          "program_number": 1,
          "role": "pmt",
          "pid": "0x1000",
          "status": "ok",
          "silence_judged": true,
          "packet_count": 40,
          "first_offset_bytes": 1880,
          "last_offset_bytes": 119192,
          "referenced_packet_count": 37,
          "referenced_from_offset_bytes": 8084,
          "referenced_until_offset_bytes": 120320,
          "max_silence_ms": 400,
          "max_silence_exact": true,
          "max_silence_start_offset_bytes": 10904,
          "max_silence_end_offset_bytes": 13912
        },
        {
          "program_number": 1,
          "role": "pcr",
          "pid": "0x0100",
          "status": "ok",
          "silence_judged": true,
          "packet_count": 160,
          "first_offset_bytes": 0,
          "last_offset_bytes": 119568,
          "referenced_packet_count": 149,
          "referenced_from_offset_bytes": 8084,
          "referenced_until_offset_bytes": 120320,
          "max_silence_ms": 100,
          "max_silence_exact": true,
          "max_silence_start_offset_bytes": 8272,
          "max_silence_end_offset_bytes": 9024
        },
        {
          "program_number": 1,
          "role": "elementary_stream",
          "pid": "0x0100",
          "stream_type": "0x02",
          "status": "ok",
          "silence_judged": true,
          "packet_count": 160,
          "first_offset_bytes": 0,
          "last_offset_bytes": 119568,
          "referenced_packet_count": 149,
          "referenced_from_offset_bytes": 8084,
          "referenced_until_offset_bytes": 120320,
          "max_silence_ms": 100,
          "max_silence_exact": true,
          "max_silence_start_offset_bytes": 8272,
          "max_silence_end_offset_bytes": 9024
        },
        {
          "program_number": 1,
          "role": "elementary_stream",
          "pid": "0x0101",
          "stream_type": "0x03",
          "status": "silent",
          "silence_judged": true,
          "packet_count": 100,
          "first_offset_bytes": 188,
          "last_offset_bytes": 119756,
          "referenced_packet_count": 89,
          "referenced_from_offset_bytes": 8084,
          "referenced_until_offset_bytes": 120320,
          "max_silence_ms": 6100,
          "max_silence_exact": true,
          "max_silence_start_offset_bytes": 44556,
          "max_silence_end_offset_bytes": 90428
        },
        {
          "program_number": 1,
          "role": "elementary_stream",
          "pid": "0x0102",
          "stream_type": "0x06",
          "status": "insufficient_data",
          "reason": "intermittent_stream_type",
          "silence_judged": false,
          "packet_count": 0,
          "referenced_packet_count": 0,
          "referenced_from_offset_bytes": 8084,
          "referenced_until_offset_bytes": 120320,
          "max_silence_ms": 14800,
          "max_silence_exact": false,
          "max_silence_start_offset_bytes": 8084,
          "max_silence_end_offset_bytes": 120320
        },
        {
          "program_number": 1,
          "role": "elementary_stream",
          "pid": "0x0103",
          "stream_type": "0x04",
          "status": "absent",
          "silence_judged": true,
          "packet_count": 0,
          "referenced_packet_count": 0,
          "referenced_from_offset_bytes": 8084,
          "referenced_until_offset_bytes": 107348,
          "max_silence_ms": 13175,
          "max_silence_exact": false,
          "max_silence_start_offset_bytes": 8084,
          "max_silence_end_offset_bytes": 107348
        },
        {
          "program_number": 1,
          "role": "elementary_stream",
          "pid": "0x0105",
          "stream_type": "0x03",
          "status": "ok",
          "silence_judged": true,
          "packet_count": 11,
          "first_offset_bytes": 105092,
          "last_offset_bytes": 120132,
          "referenced_packet_count": 9,
          "referenced_from_offset_bytes": 107348,
          "referenced_until_offset_bytes": 120320,
          "max_silence_ms": 200,
          "max_silence_exact": true,
          "max_silence_start_offset_bytes": 108100,
          "max_silence_end_offset_bytes": 109604
        },
        {
          "program_number": 1,
          "role": "elementary_stream",
          "pid": "0x0106",
          "stream_type": "0x03",
          "status": "ok",
          "silence_judged": true,
          "packet_count": 74,
          "first_offset_bytes": 564,
          "last_offset_bytes": 110356,
          "referenced_packet_count": 66,
          "referenced_from_offset_bytes": 8084,
          "referenced_until_offset_bytes": 107348,
          "max_silence_ms": 200,
          "max_silence_exact": true,
          "max_silence_start_offset_bytes": 9588,
          "max_silence_end_offset_bytes": 11092
        }
      ]
    },
    "unreferenced": {
      "status": "unreferenced",
      "pids": [
        {
          "pid": "0x0300",
          "status": "unreferenced",
          "packet_count": 5,
          "first_offset_bytes": 23688,
          "last_offset_bytes": 35720,
          "longest_run_ms": 12750
        },
        {
          "pid": "0x0500",
          "status": "insufficient_data",
          "packet_count": 2,
          "first_offset_bytes": 116936,
          "last_offset_bytes": 119944,
          "longest_run_ms": 350
        }
      ]
    }
  }
}
```

- `time_basis` is `pcr_interpolated_file_position`: the time of a packet is
  interpolated from its position between two PCRs of the reference program
  (the selected program, or with `--output analysis` the first analyzed
  program in PAT order with a `PCR_PID`), the model of the bitrate and PCR
  jitter estimates. Time before the first or after the last PCR of the range,
  and across a timing-segment boundary (a `declared`, `pcr_pid_change`,
  `backward` or `forward_jump` step, see
  [PCR timing results](#pcr-timing-results)), is not counted, so durations
  there are lower bounds; a `wraparound` step is an ordinary interval.
- `silence_limit_ms` (5000) and `unreferenced_grace_ms` (500) are the limits
  below. ETSI TR 101 290 leaves the PID_error period (check 1.6) to the user;
  5 s is the default of TSDuck's TR 101 290 analyzer for video and audio PIDs.
  The 0.5 s grace is TR 101 290's Unreferenced_PID period (check 3.4). Both
  are fixed (no option changes them); they are reported so that a consumer
  can tell if a later version makes them configurable, which may come with
  the CI failure policy
  ([#62](https://github.com/small-teton/mpeg-ts-analyzer/issues/62)).
- `referenced.pids` has one row per program, `role` and `pid` that a layout
  referenced at some point: `pmt` for the PMT PID of every program of the PAT,
  and for each program whose PMT the analysis follows `pcr` (its `PCR_PID`,
  unless `0x1fff`) and `elementary_stream` (with its `stream_type`). A
  `PCR_PID` that also carries an elementary stream has a row for each role
  with the same counts. An elementary stream is also identified by its
  `stream_type`: a PMT change that retypes it ends the row of the old type
  and starts a row of the new type, so each is judged under its own type
  only. Rows are ordered by `program_number`, then `pmt`, `pcr`,
  `elementary_stream`, then PID and `stream_type`. `referenced.status` is the most
  severe row status (`absent`, then `silent`, then `insufficient_data`).
- `packet_count`, `first_offset_bytes` and `last_offset_bytes` cover the PID
  in the whole range, from its first packet; `referenced_packet_count` counts
  the packets while the row was referenced, from `referenced_from_offset_bytes`
  (where its layout first applied) to `referenced_until_offset_bytes` (where a
  layout stopped referencing it, or the end of the scan). Packets with
  `transport_error_indicator` set are not counted, because their PID may be
  corrupted; scrambled packets are, because their header is clear.
- `max_silence_ms` is the longest time without a packet of the PID while
  referenced, including from the start of a reference to the first packet and
  from the last packet to the end of a reference; the offsets are the packet
  (or reference start) before it and the packet (or reference end) that ended
  it. `max_silence_exact` is `false` when it is a lower bound. It is absent
  without a PCR time base.
- `silence_judged` is `true` for the PMT, the `PCR_PID` and video or audio
  elementary streams, which carry packets continuously. Other stream types
  (private data, subtitles, metadata) may be idle for a long time, so only
  their presence is reported.

| Row `status` | Meaning |
| --- | --- |
| `ok` | A packet arrived while referenced, and for a judged role no silence exceeded `silence_limit_ms` |
| `absent` | A judged role had no packet at all while referenced, during a reference longer than `silence_limit_ms` |
| `silent` | A judged role had packets, but `max_silence_ms` exceeds `silence_limit_ms` |
| `insufficient_data` | `reason` is `reference_shorter_than_limit` (no packet, but the reference was too short to call the PID absent), `intermittent_stream_type` (no packet on a PID that is not judged) or `no_pcr_time_base` (no PCR to measure time) |

`unreferenced.pids` lists the PIDs that carried packets while no layout
referenced them. The null PID `0x1fff`, the PSI/SI PIDs `0x0000` to `0x001f`,
`0x1ffb`, the PAT's `network_PID` and the `CA_PID` of every CA_descriptor in a
PMT are never unreferenced. Consecutive unreferenced packets of a PID form a
run. A run is `unreferenced` when no layout references the PID within
`unreferenced_grace_ms` of its first packet, or, for a PID an earlier layout
referenced (a stream that a PMT removed), when its packets continue for longer
than that after the first unreferenced one. `longest_run_ms` is the longest
such run, `packet_count` and the offsets cover those runs. A run that the end
of the range cuts before the grace period could elapse is `insufficient_data`.

| `unreferenced.status` | Meaning |
| --- | --- |
| `ok` | No PID carried unreferenced packets beyond the grace period |
| `unreferenced` | At least one PID did |
| `insufficient_data` | No PID is unreferenced, but a run could not be judged (`reason: unreferenced_within_grace`), or there is no PCR time base (`no_pcr_time_base`) |
| `skipped` | The analysis never followed the PMT of every program of the PAT, so the referenced PIDs are not fully known (`reason: program_layouts_unknown`); for example a summary run with `--program` on a multi-program stream |

Packets are judged only while every program's PMT is followed, so packets read
while the first PAT and PMTs were acquired (a capture that starts in the
middle of a stream) are never unreferenced, and a PAT or PMT change applies
from the packet after the one that completed it, with the grace period around
it. See [PID references](../cli/usage.md#pid-references-always-on) for the
complete policy. `pid_references` does not take part in `--fail-on-error`.

## Multiplexing: packet gaps

Every complete analysis (summary and `--output analysis`) has
`multiplexing.packet_gaps`: for every PID of the range except the null PID,
the largest gap between two consecutive packets of the PID. It is a
**report-only** diagnostic of how the multiplex interleaves its PIDs: it has
no limit and no verdict, it never changes the exit status and it does not
take part in `--fail-on-error`. It is absent when no analysis ran. A poorly
interleaved remux shows up as a large gap even when the PID's average bitrate
is correct. For the bursty remux `sample_data/sample_188byte_packet_gaps.ts`
(see [Sample data](../development/sample-data.md)), abridged:

```json
{
  "multiplexing": {
    "packet_gaps": {
      "time_basis": "pcr_interpolated_file_position",
      "pids": [
        {
          "pid": "0x0012",
          "references": [],
          "packet_count": 1,
          "status": "insufficient_data",
          "reason": "no_consecutive_packets"
        },
        {
          "pid": "0x0101",
          "references": [
            {
              "program_number": 1,
              "role": "elementary_stream",
              "stream_type": "0x03"
            }
          ],
          "packet_count": 36,
          "status": "info",
          "max_gap_packets": 125,
          "max_gap_start_offset_bytes": 73508,
          "max_gap_end_offset_bytes": 97196,
          "max_gap_ms": 590,
          "max_gap_exact": false
        },
        {
          "pid": "0x01ff",
          "references": [
            {
              "program_number": 1,
              "role": "pcr"
            }
          ],
          "packet_count": 100,
          "status": "info",
          "max_gap_packets": 7,
          "max_gap_start_offset_bytes": 16544,
          "max_gap_end_offset_bytes": 18048,
          "max_gap_ms": 40,
          "max_gap_exact": true
        }
      ]
    }
  }
}
```

- `pids` lists every PID observed in the range except `0x1fff`, in numeric
  order. The packets are those of `pid_references`: every packet from the
  first packet of the range (the packets read while the first PAT and PMT
  were acquired included), without packets with `transport_error_indicator`
  set, which still occupy their position. `packet_count` matches the
  per-PID packet count of TSDuck's `tsanalyze` on a stream without transport
  errors.
- `references` lists every program and role under which a PAT or followed PMT
  of the range referenced the PID (`pmt`, `pcr`, `elementary_stream` with its
  `stream_type`, as in `pid_references.referenced`), ordered by
  `program_number`, role and `stream_type`. It is empty for PSI/SI PIDs and
  for PIDs that no layout referenced. With `--program` on a multi-program
  stream, only the selected program's PMT is followed, so the other
  programs' streams have no reference. A consumer that shows one program
  selects the PIDs whose `references` name it.
- `max_gap_packets` is the largest number of packets of other PIDs (null
  packets and packets with a transport error included) between two
  consecutive packets of the PID; the two packets themselves are not counted,
  so packets at indices 10 and 15 have a gap of 4. It is exact, and it is
  what chooses the largest gap. `max_gap_start_offset_bytes` and
  `max_gap_end_offset_bytes` are the two packets. Of equal gaps the first is
  reported, preferring the first one that starts after the first PCR of the
  time basis (a gap before it can only get a clamped time).
- The time before the first packet and after the last packet of a PID is not
  a gap: absence and silence are `pid_references`' domain.
- A PAT or PMT change that stops referencing a PID, or references it again
  after an earlier change stopped, starts a new chain for the PID: the two
  packets on either side of the change are not consecutive, so a stream that
  is removed and later re-added gets no gap across its absence. Its
  `packet_count` and the largest gap found so far are kept. A PID referenced
  for the first time does not start a new chain.
- `max_gap_ms` is the estimated time of that gap on `time_basis`
  `pcr_interpolated_file_position`, the time model of `pid_references`: each
  packet's position is interpolated between two PCRs of the reference
  program. `max_gap_exact` is `false` when it is a lower bound: the gap lies
  partly before the first or after the last PCR of the analysis, or crosses a
  PCR timing-segment boundary (any PCR step other than `continuous` or
  `wraparound`), whose PCR interval is not counted. A file has no
  packet-arrival times, so this is an estimate even when exact, and on a
  variable-bitrate stream a gap in packets and a gap in time can disagree.
  Without any PCR of the reference program `max_gap_ms` and `max_gap_exact`
  are absent and `reason` is `no_pcr_time_base`.

| PID `status` | Meaning |
| --- | --- |
| `info` | A gap was measured (`reason` `no_pcr_time_base` when it has no time) |
| `insufficient_data` | `reason` `no_consecutive_packets`: the PID had a single packet, or no two packets within one stretch of the layout; the gap fields are absent |

The state is one record per PID with a fixed number of stamps, shared with
`pid_references`; nothing is kept per packet, and the output has one entry per
PID. See [Packet gaps](../cli/usage.md#packet-gaps-report-only) for the text
summary.

## PCR timing results

Every PCR-based result is computed from the PCRs of the selected program's
`PCR_PID` in packets without `transport_error_indicator` (a scrambled packet's
PCR is used). Each PCR is classified against the previous one into one of the
steps in [PCR steps](../cli/usage.md#pcr-steps): first, `continuous`,
`wraparound`, `declared`, `pcr_pid_change`, `backward` or `forward_jump`.
`continuous` and `wraparound` steps are comparable; every other step except
the first starts a new timing segment. All results use the same steps:

| Result | Source and units | Scope | Comparison boundaries | Too little data |
| --- | --- | --- | --- | --- |
| `compliance.max_pcr_interval` | Largest PCR value difference of a comparable step, in ms; `observations` counts those steps. Limit 100 ms, inclusive (ISO/IEC 13818-1, 2.7.2; ETSI TR 101 290 2.3a). | Selected program, following `PCR_PID` changes | Not measured at the first PCR of the range or at any segment boundary; measured across a wraparound. An undeclared forward step of 100 to 1000 ms is measured and fails. | `skipped` with no comparable step |
| `compliance.pcr_discontinuities` | Count of each step kind other than `continuous`, with the first PCR packet's `first_offset_bytes` and `first_jump_ms` (the value difference; for a wraparound the interval across the wrap). `ng` when `undeclared_count` (`backward` + `forward_jump`) is above 0 (ISO/IEC 13818-1, 2.4.3.5; ETSI TR 101 290 2.3b). `jump_limit_ms` (1000) separates a late PCR from a `forward_jump`. | Selected program, following `PCR_PID` changes | Every PCR after the first is classified; `pcr_count` counts all PCRs. | `skipped` with fewer than two PCRs |
| `pcr_jitter` | Byte-position estimate in ns, and the delivery assessment ([PCR jitter](#pcr-jitter)); not a TR 101 290 2.4 measurement | Selected program | Interpolated only between three PCRs of one segment; delivery windows do not span a boundary; `estimate.discontinuities` lists each boundary with its `kind`, `jump_ms` and the segments it separates | `skipped` with fewer than 3 PCRs, no segment of 3 PCRs, or more than 30 % of the PCRs starting a segment |
| `bitrate` | Bytes over PCR time of the comparable steps, per PID, in bit/s; `segment_count` counts timing segments | Selected program's PIDs; null packets separately | Bytes before the first PCR and across a boundary are dropped and the clock does not advance across it | `skipped` without two PCRs in one segment |
| `compliance.max_pcr_pts_dts_gap` | DTS (or PTS) minus the PCR interpolated by byte position between the PCRs before and after the PES packet, in ms. Limit 1000 ms, inclusive. | Every elementary stream of the selected program | A PES packet is compared only when both PCRs belong to the same segment and the second is larger: no gap across a boundary or a wraparound, and none after the last PCR | `skipped` with no bracketed PTS/DTS |
| `series.pcr` (`--output analysis`) | Each PCR with `pcr_ms`, `elapsed_ms` and `interval_ms` | Each program | `interval_ms` only on comparable steps; `discontinuity: true` on the first PCR of each new segment, where `elapsed_ms` does not advance | Empty without PCRs |

Results are aggregates bounded by the number of step kinds; only the existing
series and jitter samples grow with the number of PCRs.

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
  analyzed program that has one) and the number of PCR segments, split at the
  same [PCR steps](#pcr-timing-results) as every other PCR-based result. Packet times
  are interpolated by byte position between the PCRs around them; no interval
  spans a segment boundary. `status` is `none` without two PCRs in one segment,
  and every `repetition.status` is then `skipped`. When a later PAT removes the
  reference program, no further time is measured, and `pcr_pid` keeps naming
  the last reference `PCR_PID`.
- `tables` lists the PAT, then one entry per `program_number` announced in the
  range, in numeric order. A PMT entry has `program_number`,
  `announced_offset_bytes` (the PAT packet that first announced it; the PMT is
  read from the next packet) and, for a program a later PAT removed,
  `withdrawn_offset_bytes`. `pid` is the latest PMT PID. A PMT moved to
  another PID, or a program announced again, starts a new identity: counts
  cover every identity, but `ok` needs a complete interval of the latest one,
  and a late or missing earlier identity keeps the table `ng`.
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

## PES headers and PTS repetition

Every complete analysis (summary and `--output analysis`) has `pes_headers`
and `pts_repetition`; they are absent when no analysis ran. Both list every
elementary stream of the analyzed programs (the selected program in the
summary, every analyzed program with `--output analysis`) by
`program_number` and PID, with its last `stream_type` and a `label` (`video`,
`audio` or `stream_type 0xNN`). Their state is a fixed set of counters per
elementary PID, and no PES payload is kept: each PES header is checked from
the at most 264 bytes the parser retains of it. For
`sample_data/sample_188byte_pes_health.ts` (generated by
`tools/generate_pes_health`):

```json
{
  "pes_headers": {
    "status": "errors",
    "checked_count": 161,
    "error_count": 8,
    "unchecked_count": 0,
    "first_error_offset_bytes": 5076,
    "errors": [
      {
        "kind": "truncated",
        "count": 1,
        "first_offset_bytes": 6580
      },
      {
        "kind": "bad_start_code",
        "count": 1,
        "first_offset_bytes": 5076
      },
      {
        "kind": "bad_flags",
        "count": 2,
        "first_offset_bytes": 12596
      },
      {
        "kind": "inconsistent_length",
        "count": 2,
        "first_offset_bytes": 17108
      },
      {
        "kind": "bad_marker_bits",
        "count": 2,
        "first_offset_bytes": 11468
      }
    ],
    "pids": [
      {
        "program_number": 1,
        "pid": "0x0100",
        "stream_type": "0x02",
        "label": "video",
        "status": "ok",
        "checked_count": 96,
        "error_count": 0,
        "unchecked_count": 0,
        "errors": []
      },
      {
        "program_number": 1,
        "pid": "0x0101",
        "stream_type": "0x03",
        "label": "audio",
        "status": "errors",
        "checked_count": 49,
        "error_count": 1,
        "unchecked_count": 0,
        "first_error_offset_bytes": 11468,
        "errors": [
          {
            "kind": "bad_marker_bits",
            "count": 1,
            "first_offset_bytes": 11468
          }
        ]
      },
      {
        "program_number": 1,
        "pid": "0x0102",
        "stream_type": "0x06",
        "label": "stream_type 0x06",
        "status": "errors",
        "checked_count": 15,
        "error_count": 7,
        "unchecked_count": 0,
        "first_error_offset_bytes": 5076,
        "errors": [
          {
            "kind": "truncated",
            "count": 1,
            "first_offset_bytes": 6580
          },
          {
            "kind": "bad_start_code",
            "count": 1,
            "first_offset_bytes": 5076
          },
          {
            "kind": "bad_flags",
            "count": 2,
            "first_offset_bytes": 12596
          },
          {
            "kind": "inconsistent_length",
            "count": 2,
            "first_offset_bytes": 17108
          },
          {
            "kind": "bad_marker_bits",
            "count": 1,
            "first_offset_bytes": 21620
          }
        ]
      },
      {
        "program_number": 1,
        "pid": "0x0103",
        "stream_type": "0x04",
        "label": "audio",
        "status": "ok",
        "checked_count": 1,
        "error_count": 0,
        "unchecked_count": 0,
        "errors": []
      },
      {
        "program_number": 1,
        "pid": "0x0104",
        "stream_type": "0x1b",
        "label": "video",
        "status": "insufficient_data",
        "reason": "no_pes",
        "checked_count": 0,
        "error_count": 0,
        "unchecked_count": 0,
        "errors": []
      }
    ]
  },
  "pts_repetition": {
    "limit_ms": 700,
    "status": "ng",
    "pts_count": 127,
    "interval_count": 113,
    "max_interval_ms": 1080,
    "max_interval_start_offset_bytes": 18424,
    "max_interval_end_offset_bytes": 28576,
    "exceeded_count": 1,
    "first_exceeded_offset_bytes": 28576,
    "pids": [
      {
        "program_number": 1,
        "pid": "0x0100",
        "stream_type": "0x02",
        "label": "video",
        "status": "ng",
        "pts_count": 70,
        "interval_count": 68,
        "max_interval_ms": 1080,
        "max_interval_start_offset_bytes": 18424,
        "max_interval_end_offset_bytes": 28576,
        "exceeded_count": 1,
        "first_exceeded_offset_bytes": 28576
      },
      {
        "program_number": 1,
        "pid": "0x0101",
        "stream_type": "0x03",
        "label": "audio",
        "status": "ok",
        "pts_count": 48,
        "interval_count": 45,
        "max_interval_ms": 80,
        "max_interval_start_offset_bytes": 1692,
        "max_interval_end_offset_bytes": 2444,
        "exceeded_count": 0
      },
      {
        "program_number": 1,
        "pid": "0x0102",
        "stream_type": "0x06",
        "label": "stream_type 0x06",
        "status": "skipped",
        "reason": "not_video_or_audio",
        "pts_count": 8,
        "interval_count": 0,
        "exceeded_count": 0
      },
      {
        "program_number": 1,
        "pid": "0x0103",
        "stream_type": "0x04",
        "label": "audio",
        "status": "insufficient_data",
        "reason": "single_pts",
        "pts_count": 1,
        "interval_count": 0,
        "exceeded_count": 0
      },
      {
        "program_number": 1,
        "pid": "0x0104",
        "stream_type": "0x1b",
        "label": "video",
        "status": "insufficient_data",
        "reason": "no_pes",
        "pts_count": 0,
        "interval_count": 0,
        "exceeded_count": 0
      }
    ]
  }
}
```

`pes_headers` checks each PES header against ISO/IEC 13818-1, clause 2.4.3.6
and 2.4.3.7, when the PES packet ends:

| `kind` | The header |
| --- | --- |
| `truncated` | ends before its 9 fixed bytes or its `PES_header_data_length` bytes of optional fields and stuffing, because the next PES packet of the PID started |
| `bad_start_code` | does not start with `packet_start_code_prefix` 0x000001 followed by a PES `stream_id` (0xBC to 0xFF) |
| `bad_flags` | has an optional header that does not start with `'10'`, or `PTS_DTS_flags` `'01'` (forbidden) |
| `inconsistent_length` | has a `PES_header_data_length` smaller than the fields its flags announce, or a non-zero `PES_packet_length` smaller than its header |
| `bad_marker_bits` | has a PTS or DTS with a wrong 4-bit prefix (`'0010'` for a PTS alone, `'0011'` and `'0001'` for PTS and DTS) or a `marker_bit` of 0 |

- `status` is `errors` when a header failed a check, `ok` when headers were
  checked and none failed, and `insufficient_data` when none was checked, with
  `reason` `no_pes` (no PES packet started), `untrusted_payload` (payload not
  used because of transport errors or scrambling) or `incomplete_headers`.
- `checked_count` counts the headers checked, valid or not; `error_count` the
  failed ones, with `errors` giving each `kind` with its `count` and
  `first_offset_bytes` (the packet that starts the PES packet; for M2TS its
  `TP_extra_header`). `first_error_offset_bytes` is absent without errors.
- `unchecked_count` counts PES packets whose header was not complete when the
  data ended: untrusted payload (see
  [Transport errors and scrambled packets](#transport-errors-and-scrambled-packets)),
  a PMT change or the end of the range. They are not errors, since the stream
  may be intact.
- A header split over several TS packets is assembled first. A
  `PES_packet_length` of 0 is accepted for every stream. Streams without an
  optional header (program_stream_map, padding, private_stream_2, ECM, EMM,
  DSMCC, H.222.1 type E, directory) are checked up to `PES_packet_length`.
- Only a header that passed every check feeds the other timestamp checks: a
  failed header contributes no PTS/DTS to `timestamp_anomalies`, the
  PCR-PTS/DTS gap, the time series or `pts_repetition`.

`pts_repetition` measures, on each video and audio stream, how long the
multiplex goes without a PES packet carrying a PTS. The limit (`limit_ms`,
700) is ISO/IEC 13818-1, clause 2.7.4 (coded PTS at most 0.7 s apart on each
video and audio stream), monitored by ETSI TR 101 290 V1.4.1 as indicator 2.5
PTS_error; both are profile-neutral.

- Applicability: a PID whose `stream_type` is a video or audio type
  (`label` `video` or `audio`: 0x01, 0x02, 0x10, 0x1B, 0x24, 0x03, 0x04, 0x0F,
  0x11). Other PIDs are `skipped` with `not_video_or_audio`; when no PID
  applies, the totals are `skipped` with `no_video_or_audio`.
- Time base: the time of a PES packet is the program's PCR (27 MHz) at the
  first byte of the packet that starts it, interpolated by byte position
  between the PCR before and the PCR after it. The interval is measured on
  this transport timeline, as TR 101 290 monitors it, not between PTS values,
  so B-frame reordering and splices do not affect it (`timestamp_anomalies`
  reports the values). A PES packet that does not lie between two PCRs of one
  segment (before the first PCR, after the last one, or a program without
  PCR) is not placed; if it carries a PTS, the interval in progress ends there.
- Wraparound and discontinuities: segments follow the
  [PCR timing results](#pcr-timing-results): a `wraparound` step is measured
  across, and every other step that starts a new segment (`declared`,
  `pcr_pid_change`, `backward`, `forward_jump`) ends the interval in progress,
  so no interval spans it.
- Acquisition: the first PES packet of each PID in the range starts the
  measurement. PES packets without a PTS before the first PTS, before a break
  and at the end of the range give a lower bound (the stream went at least
  that long without a PTS); it counts, as an interval, only when it is above
  the limit.
- Breaks: a malformed or incomplete header, untrusted payload, a continuity
  error on the PID, a `stream_type` change and the removal of the PID by a
  PMT change end the interval in progress, because a PTS may have been lost
  there. A PID keeps its counts across PMT changes and is reported with its
  last `stream_type`.
- `status` is `ng` when an interval exceeded the limit, `ok` when at least one
  interval was measured and none exceeded it, `insufficient_data` when nothing
  was measured (`reason` `no_pes`, `untrusted_payload`, `malformed_headers`,
  `no_pcr_timing`, `no_pts` or `single_pts`), and `skipped` as above. The
  totals are `ng` if any PID is, else `ok` if any PID is, else the first
  PID's insufficient-data reason.
- `pts_count` counts valid headers with a PTS, `interval_count` the intervals
  measured between two of them, and `exceeded_count` the intervals above the
  limit, with `first_exceeded_offset_bytes` (the PES packet that ended the
  first one). `max_interval_ms` is the largest interval, measured or a lower
  bound above the limit, from the PES packet at
  `max_interval_start_offset_bytes` to the one at
  `max_interval_end_offset_bytes`; the three are absent when there is none.
- Still pictures, which TR 101 290 exempts from the limit, cannot be
  recognized without reading the elementary stream and are judged like other
  video.

The PTS/DTS anomaly check does not report a forward jump across PES packets
without a timestamp, a malformed or incomplete header, untrusted payload or a
continuity error, since `pts_repetition`, `pes_headers` or the other checks
already report that cause. Neither object takes part in `--fail-on-error`.

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
  split the samples into segments, each with its `kind` (see
  [PCR timing results](#pcr-timing-results)); a wraparound does not split
  them. The value fields are absent when no
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

## TS synchronization

`sync` reports packet boundaries that did not start with the sync byte `0x47`
during the analysis, and how the analyzer realigned. It is present when the
analysis scanned packets (with `continuity_counter`), in both the summary and
the `--output analysis` document. Like the continuity check, it covers the
whole multiplex from the analysis start. Example for
`sample_data/sample_188byte_sync_loss.ts`:

```json
"sync": {
  "status": "recovered",
  "required_sync_bytes": 5,
  "max_resync_scan_bytes": 1048576,
  "loss_count": 4,
  "resync_count": 4,
  "skipped_bytes": 1376,
  "first_loss_offset_bytes": 7520,
  "first_resync_offset_bytes": 7708,
  "max_skipped_bytes": 1000,
  "max_skipped_offset_bytes": 22748,
  "trailing_partial_packet_bytes": 100,
  "trailing_partial_packet_offset_bytes": 38600
}
```

| Field | Meaning |
| --- | --- |
| `status` | `ok`: every packet boundary had its sync byte. `recovered`: synchronization was lost at least once and every loss was followed by a new alignment; the analysis is complete apart from the skipped bytes. `lost`: the last loss was not recovered and the analysis stopped there (top-level `status: "incomplete"`, `reason: "sync_lost"`). |
| `reason` | Present for `lost`: why no alignment was found (the search limit, the end of the input, or the end of the `--limit` range). Explanatory text, not for matching. |
| `required_sync_bytes` | Consecutive packets, one packet size apart, that must start with `0x47` to accept a new alignment (5). |
| `max_resync_scan_bytes` | How far after a lost boundary a new alignment may start (1,048,576). |
| `loss_count` | Packet boundaries without a sync byte, each followed by a search. |
| `resync_count` | Searches that found a new alignment. Equal to `loss_count` unless `status` is `lost` (then one less). |
| `skipped_bytes` | Bytes skipped by the recovered losses, from each lost boundary to its new alignment. They were not analyzed. |
| `first_loss_offset_bytes`, `first_resync_offset_bytes` | Offset of the first lost boundary and of the first packet of the first new alignment. Absent when there was none. |
| `max_skipped_bytes`, `max_skipped_offset_bytes` | The largest span skipped by one recovered loss and the offset of that loss (the worst). Absent without a recovery. |
| `unrecovered_offset_bytes` | The lost boundary where the analysis stopped (`status: "lost"`); `analysis_end_offset_bytes` equals it. |
| `trailing_partial_packet_bytes`, `trailing_partial_packet_offset_bytes` | An incomplete packet at the end of the input and where it starts; 0 and absent when there is none. |

Every offset is an absolute byte offset in the input file, also after a
recovery, and points at the first byte of the packet boundary (for 192-byte
M2TS, the first byte of the `TP_extra_header`). Offsets of later results
(continuity events, PCR and PES points, warnings) stay absolute too. The
search, its bounds and the cases it does not cover are described in
[TS synchronization](../cli/usage.md#ts-synchronization-always-on).

## Changes in schema 2.0

Schema 2.0 (summary and `--output analysis`) changes `pcr_jitter`; the
packet documents stay at 1.0. A 1.x consumer must be updated:

| Schema 1.0 | Schema 2.0 |
| --- | --- |
| `pcr_jitter.status` `ok` / `warning` / `ng` for every estimate | the same values only when `pcr_jitter.delivery.assessment` is `constant`; otherwise `not_assessable` |
| `sample_count`, `segment_count`, `discontinuities` | `estimate.sample_count`, `estimate.segment_count`, `estimate.discontinuities` |
| `measured_sample_count` | `estimate.observations` |
| `max_jitter_ns`, `min_jitter_ns` and their offsets, `average_absolute_jitter_ns`, `within_500ns_percent` | the same names under `estimate`, plus `estimate.max_abs_jitter_ns` / `max_abs_jitter_offset_bytes` |
| — | `estimate.model`, `estimate.pcr_pids`, `delivery` |

Schema 2.0 also adds the `sync` object ([TS synchronization](#ts-synchronization))
and the top-level reasons `sync_lost` and `sync_not_acquired`. An input with no
packet alignment, which 1.x reported as `analysis_failed`, now fails with
`sync_not_acquired`; a packet without its sync byte after the analysis started,
which 1.x let end the scan and restart the PAT search, is now skipped and
counted in `sync`.

Everything else, including the `series.pcr_jitter` points, is unchanged.
Within 2.0, `transport_errors` and `scrambled_packets` were added to both
documents, and `compliance.pcr_discontinuities` and the `kind` of each
`pcr_jitter.estimate.discontinuities` entry to both, and `psi_health`,
`pid_references`, `pes_headers`, `pts_repetition` and `multiplexing` to
both documents (additive changes). Aligning
the PCR steps also changed some values within 2.0 for streams with these steps
(none of the bundled samples except the new PCR-discontinuities sample):

| Stream with | Before | Now |
| --- | --- | --- |
| A 33-bit PCR wraparound | Jitter estimate, delivery windows, bitrate and `series.pcr` split at the wrap (`discontinuity: true`, elapsed time frozen) | One segment across the wrap; listed as a `wraparound` step |
| An undeclared forward PCR jump of more than 1000 ms | Measured as a PCR interval (`max_pcr_interval` `ng` with the jump as value) | A `forward_jump`: not an interval; `pcr_discontinuities` `ng` |
| Two equal consecutive PCRs | A segment boundary for jitter, bitrate and series | A 0 ms interval |
| `discontinuity_indicator` on a packet of the `PCR_PID` without a PCR | Ignored | The next PCR is `declared` |
| A PES packet whose PCR bracket spans a declared or undeclared discontinuity | Interpolated across the two time bases | No PCR-PTS/DTS gap |

With `--fail-on-error` an undeclared forward jump still exits 2 (now through
`PCR discontinuities`); an undeclared backward step, which previously passed,
now exits 2 as well.
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
  "sync": { "status": "ok", "required_sync_bytes": 5, "max_resync_scan_bytes": 1048576, "loss_count": 0, "resync_count": 0, "skipped_bytes": 0, "trailing_partial_packet_bytes": 0 },
  "continuity_counter": { "status": "ok", "total_error_count": 0, "pids": [] },
  "continuity_events": [],
  "transport_errors": { "status": "ok", "packet_count": 0, "excluded_packet_count": 0, "pids": [] },
  "scrambled_packets": { "status": "ok", "packet_count": 0, "excluded_packet_count": 0, "pids": [] },
  "psi_health": { "status": "ok", "repetition_limit_ms": 500, "time_base": { ... }, "tables": [ ... ] },
  "pid_references": { "time_basis": "pcr_interpolated_file_position", "silence_limit_ms": 5000, "unreferenced_grace_ms": 500, "referenced": { "status": "ok", "pids": [ ... ] }, "unreferenced": { "status": "ok", "pids": [] } },
  "pes_headers": { "status": "ok", "checked_count": 106, "error_count": 0, "unchecked_count": 0, "errors": [], "pids": [ ... ] },
  "pts_repetition": { "limit_ms": 700, "status": "ok", "pts_count": 106, "interval_count": 94, "max_interval_ms": 414.2792962962963, ... },
  "multiplexing": { "packet_gaps": { "time_basis": "pcr_interpolated_file_position", "pids": [ ... ] } },
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
  rather than per program. So is TS synchronization (`sync`); after an
  unrecovered loss the document has `status: "incomplete"`, `reason:
  "sync_lost"`, and `analysis_end_offset_bytes` is the lost boundary.
- `transport_errors` and `scrambled_packets` are reported once for the whole
  multiplex, from the first packet of the range, as in the summary
  ([Transport errors and scrambled packets](#transport-errors-and-scrambled-packets)).
- `psi_health` is reported once for the range, from its first packet, with the
  PCR of the first analyzed program that has one as its time base
  ([PAT and PMT health](#pat-and-pmt-health)).
- `pid_references` is reported once for the whole multiplex, from the first
  packet of the range ([PID references](#pid-references)). Each program's
  references apply from its own analysis start; unreferenced PIDs are judged
  once every program's PMT is followed.
- `pes_headers` and `pts_repetition` are reported once and list the
  elementary streams of every analyzed program by `program_number` and PID
  ([PES headers and PTS repetition](#pes-headers-and-pts-repetition)).
- `multiplexing` is reported once for the whole multiplex, from the first
  packet of the range ([Multiplexing: packet gaps](#multiplexing-packet-gaps));
  each PID's `references` name the programs that reference it.
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
- A packet without a sync byte is listed with `"valid": false`, and so is a
  packet without `transport_error` whose header or adaptation field cannot be
  parsed (for example a reserved `adaptation_field_control`). A packet with
  `transport_error` is always listed with its header fields as decoded, even
  when the rest of it is malformed. An incomplete last packet ends the page
  with a warning.
- `pcr_ms` is the PCR of the packet's adaptation field. For a packet with
  `transport_error`, whose adaptation field the analysis does not parse, it is
  read from the packet's bytes when `PCR_flag` is set and
  `adaptation_field_length` can hold it, as the packet detail shows it; the
  analysis never uses that PCR.
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
