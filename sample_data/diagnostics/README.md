# Diagnostic samples

Streams built to contain faults or edge cases, so that the analyzer's checks
have something to find. Most are **intentionally invalid**; do not use them as
examples of a good stream (the ordinary samples are in
[`sample_data/`](../README.md)). Each is written by a generator under
[`tools/`](../../tools/README.md). File names are
`sample_<size>byte_<family>.ts`, `<size>` being 188 (TS), 192 (M2TS) or 204
(TS with a 16-byte filler trailer after each packet). The contents, expected
results and reference-tool cross-checks are in [Sample data](../../docs/development/sample-data.md).

- `all_findings` (188, 192, 204): one defect per Overview check — [details](../../docs/development/sample-data.md#diagnostic-sample-with-every-overview-finding).
- `sync_loss` (188, 192, 204): four synchronization losses and a partial last packet — [details](../../docs/development/sample-data.md#diagnostic-sample-with-ts-synchronization-loss).
- `pcr_discontinuities` (188, 192): every kind of PCR step — [details](../../docs/development/sample-data.md#pcr-discontinuity-sample).
- `transport_errors`, `transport_errors_malformed` (188, 192): transport-error and scrambled packets on every kind of PID — [details](../../docs/development/sample-data.md#transport-error-and-scrambling-sample).
- `psi_health` (188, 192): every PAT/PMT health table `NG` for a different reason — [details](../../docs/development/sample-data.md#patpmt-health-sample).
- `psi_version_change_mpeg2_320x240_25fps_mp2` (188, 192): PMT and PAT version changes in the video sample — [details](../../docs/development/sample-data.md#patpmt-version-change-sample).
- `pid_references` (188, 192): absent, silent, intermittent and unreferenced PIDs — [details](../../docs/development/sample-data.md#pid-reference-sample).
- `pes_health` (188, 192): malformed PES headers and a 1080 ms PTS repetition interval — [details](../../docs/development/sample-data.md#pes-header-and-pts-repetition-sample).
- `packet_gaps` (188, 192, 204): a bursty remux for the per-PID maximum packet gaps — [details](../../docs/development/sample-data.md#packet-gap-sample).
- `mux_lead` (188, 192, 204): known mux leads, including an audio preload and a late AAC PES — [details](../../docs/development/sample-data.md#mux-lead-sample).
- `pts_gap_wraparound` (188, 192, 204): PCR-to-PTS/DTS gaps across the 33-bit wraparounds — [details](../../docs/development/sample-data.md#pcr-to-ptsdts-gap-wraparound-sample).

```bash
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_all_findings.ts
make gui-run-sample SAMPLE=sample_data/diagnostics/sample_204byte_all_findings.ts
```
