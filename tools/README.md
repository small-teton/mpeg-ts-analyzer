# tools

Small Go programs that support development; none of them is part of the
analyzer binary or the GUI.

## Fixture generators

The `generate_*` programs deterministically write the transport-stream test
fixtures: fault-injected and edge-case streams, mostly built from scratch and
two rewritten from the healthy FFmpeg samples. They cover TS synchronization
loss, transport errors and scrambling, PCR discontinuities, PAT/PMT health,
PID references, PES header health, packet gaps, mux lead, PTS/DTS wraparound,
multi-section PSI, PSI version changes, continuity counters, and one stream
with every Overview finding.

- Except for the uncommitted `generate_continuity` reference stream, their
  output is committed under `sample_data/diagnostics/` (or `sample_data/` for
  the multi-section PSI rewrite of a healthy sample) and is used by the CLI
  and GUI tests, by documented examples, and for TSDuck cross-checks.
- `tsparser` tests check that every committed file equals its generator's
  output, so a generator change must come with the regenerated files.
- `-m2ts` writes 192-byte M2TS packets and `-trailer` 204-byte packets (a
  16-byte filler trailer after each TS packet); other flags are listed in each
  sample's section.
- These are our own fixtures. Third-party corpora, such as the
  [tsduck-test](https://github.com/tsduck/tsduck-test) streams, are used only
  for local cross-checks and are never committed.

What each stream contains, the positions of its defects, the results the
analyzer must give and the reference-tool cross-checks are in
[Sample data](../docs/development/sample-data.md); each generator's package
comment describes how it builds the stream.

| Generator | Output (`<size>` = packet sizes) | Regenerate | Description |
| --- | --- | --- | --- |
| `generate_all_findings` | `sample_data/diagnostics/sample_<size>byte_all_findings.ts` (188, 192, 204) | `go run ./tools/generate_all_findings [-m2ts\|-trailer] -output …` | [All findings](../docs/development/sample-data.md#diagnostic-sample-with-every-overview-finding) |
| `generate_sync_loss` | `sample_data/diagnostics/sample_<size>byte_sync_loss.ts` (188, 192, 204) | `go run ./tools/generate_sync_loss [-m2ts\|-trailer] -output …` | [Sync loss](../docs/development/sample-data.md#diagnostic-sample-with-ts-synchronization-loss) |
| `generate_pcr_discontinuities` | `sample_data/diagnostics/sample_<size>byte_pcr_discontinuities.ts` (188, 192) | `go run ./tools/generate_pcr_discontinuities [-m2ts] -output …` | [PCR discontinuities](../docs/development/sample-data.md#pcr-discontinuity-sample) |
| `generate_transport_errors` | `sample_data/diagnostics/sample_<size>byte_transport_errors.ts` and `…_transport_errors_malformed.ts` (188, 192) | `go run ./tools/generate_transport_errors [-malformed] [-m2ts] -output …` | [Transport errors](../docs/development/sample-data.md#transport-error-and-scrambling-sample) |
| `generate_psi_health` | `sample_data/diagnostics/sample_<size>byte_psi_health.ts` (188, 192) | `go run ./tools/generate_psi_health [-m2ts] [-scenario …] -output …` | [PAT/PMT health](../docs/development/sample-data.md#patpmt-health-sample) |
| `generate_psi_version_change` | `sample_data/diagnostics/sample_<size>byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts` (188, 192) | `go run ./tools/generate_psi_version_change [-m2ts] -input sample_data/sample_188byte_video_….ts -output …` | [PAT/PMT version change](../docs/development/sample-data.md#patpmt-version-change-sample) |
| `generate_pid_references` | `sample_data/diagnostics/sample_<size>byte_pid_references.ts` (188, 192) | `go run ./tools/generate_pid_references [-m2ts] -output …` | [PID references](../docs/development/sample-data.md#pid-reference-sample) |
| `generate_pes_health` | `sample_data/diagnostics/sample_<size>byte_pes_health.ts` (188, 192) | `go run ./tools/generate_pes_health [-m2ts] -output …` | [PES health](../docs/development/sample-data.md#pes-header-and-pts-repetition-sample) |
| `generate_packet_gaps` | `sample_data/diagnostics/sample_<size>byte_packet_gaps.ts` (188, 192, 204) | `go run ./tools/generate_packet_gaps [-m2ts\|-trailer] -output …` | [Packet gaps](../docs/development/sample-data.md#packet-gap-sample) |
| `generate_mux_lead` | `sample_data/diagnostics/sample_<size>byte_mux_lead.ts` (188, 192, 204) | `go run ./tools/generate_mux_lead [-m2ts\|-trailer] -output …` | [Mux lead](../docs/development/sample-data.md#mux-lead-sample) |
| `generate_pts_gap_wraparound` | `sample_data/diagnostics/sample_<size>byte_pts_gap_wraparound.ts` (188, 192, 204) | `go run ./tools/generate_pts_gap_wraparound [-m2ts\|-trailer] -output …` | [PTS/DTS wraparound](../docs/development/sample-data.md#pcr-to-ptsdts-gap-wraparound-sample) |
| `generate_multi_section_psi` | `sample_data/sample_<size>byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts` (188, 192) | `go run ./tools/generate_multi_section_psi -input sample_data/sample_<size>byte_two_programs_….ts -output …` | [Multi-section PSI](../docs/development/sample-data.md#multi-section-psi-sample) |
| `generate_continuity` | none committed; the test writes it to a temporary directory | `go run ./tools/generate_continuity -output /tmp/continuity.ts` | [Continuity](../docs/development/sample-data.md#continuity-reference-stream) |

The exact command for every committed file is in its section of
[Sample data](../docs/development/sample-data.md).

## Other tools

- `docs_policy`: the deterministic documentation-policy check run by
  `make check-docs-policy` and the `docs-policy` pull-request workflow; see
  [Test and coverage](../docs/development/index.md#test-and-coverage).
