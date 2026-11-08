# Diagnostic samples

Streams built to contain faults or edge cases, so that the analyzer's checks
have something to find. Most are **intentionally invalid**; do not use them as
examples of a good stream (the ordinary samples are in
[`sample_data/`](../README.md)). Each is
made by a generator under [`tools/`](../../tools/); the full description,
regeneration commands and reference-tool cross-checks are in
[Sample data](../../docs/development/sample-data.md).

| Sample family | Packet sizes | What it contains | What the analyzer reports | Generator |
| --- | --- | --- | --- | --- |
| `all_findings` | 188, 192, 204 | One 2.4 s program with one defect per Overview check: 160 ms PCR interval, 1200 ms PCR → PTS/DTS gap, backward PTS, a continuity gap, a late PCR, transport-error and scrambled null packets, 800 ms PAT and PMT gaps, a bad PTS marker bit, 800 ms of audio without a PTS | Every transport-health check except TS synchronization and PCR discontinuities reports a problem | [`generate_all_findings`](../../tools/generate_all_findings/README.md) |
| `sync_loss` | 188, 192, 204 | A clean 2 s stream damaged in four places (corrupted sync byte, inserted byte, deleted byte, 1000 bytes of garbage with false sync bytes), ending in a partial packet | Four synchronization losses, each recovered, with offsets and skipped bytes | [`generate_sync_loss`](../../tools/generate_sync_loss/README.md) |
| `pcr_discontinuities` | 188, 192 | A PCR every 40 ms through a 33-bit wraparound, declared discontinuities, an undeclared backward step and 4 s forward jump, and a `PCR_PID` change | Max PCR interval `OK`, PCR discontinuities `NG` | [`generate_pcr_discontinuities`](../../tools/generate_pcr_discontinuities/README.md) |
| `transport_errors`, `transport_errors_malformed` | 188, 192 | Packets with `transport_error_indicator` and scrambled packets on the PAT, PMT, PCR, elementary-stream, unreferenced and null PIDs; the `_malformed` twin also corrupts what follows their header | Affected packets counted per PID and kept out of the analysis; both files give the same results | [`generate_transport_errors`](../../tools/generate_transport_errors/README.md) |
| `psi_health` | 188, 192 | Two programs sharing a video PID; no PAT for 1.2 s, a PMT on the PAT PID, a PMT with a bad CRC_32 and a version change, a PMT section never completed | Every PAT/PMT health table `NG`, each for a different reason | [`generate_psi_health`](../../tools/generate_psi_health/README.md) |
| `pid_references` | 188, 192 | 16 s capture starting before the first PAT; an audio PID silent for 6.1 s, referenced PIDs that never appear, a PMT change, unreferenced PIDs | Referenced and unreferenced PID findings, without false findings from acquisition or the PMT change | [`generate_pid_references`](../../tools/generate_pid_references/README.md) |
| `pes_health` | 188, 192 | Truncated PES headers, bad start code, flags, lengths and PTS marker bits, a header split over two packets, `PES_packet_length` 0 on video, video without a PTS for 1080 ms | PES headers `ERRORS`, PTS repetition `NG` | [`generate_pes_health`](../../tools/generate_pes_health/README.md) |
| `packet_gaps` | 188, 192, 204 | A 4 s poorly interleaved remux: audio in bursts every 320 ms, a PCR-only PID, a stream removed and added back by PMT changes, an SDT PID | Report-only maximum packet gaps per PID | [`generate_packet_gaps`](../../tools/generate_packet_gaps/README.md) |
| `mux_lead` | 188, 192, 204 | A 1500 ms audio preload against about 700 ms of video, AAC PES packets late or without a PTS, PCR and PTS/DTS wraparound, a declared PCR discontinuity, one transport error | Report-only mux lead and A/V lead difference; the 1500 ms lead fails the PCR → PTS/DTS gap check | [`generate_mux_lead`](../../tools/generate_mux_lead/README.md) |
| `pts_gap_wraparound` | 188, 192, 204 | Video DTS and audio PTS that wrap before the PCR, an AAC PTS that wraps after it | PCR → PTS/DTS gap `OK` at 700 ms across the wraparounds | [`generate_pts_gap_wraparound`](../../tools/generate_pts_gap_wraparound/README.md) |
| `psi_version_change_mpeg2_320x240_25fps_mp2` | 188, 192 | The ordinary 188-byte sample rewritten so that its PMT and then its PAT change version mid-stream (PIDs move, a stream type changes) | Applied PAT/PMT version changes and the PSI layout of the range | [`generate_psi_version_change`](../../tools/generate_psi_version_change/README.md) |

File names are `sample_<size>byte_<family>.ts`, `<size>` being 188 (TS), 192
(M2TS) or 204 (TS with a 16-byte trailer after each packet; deterministic
filler, not real Reed-Solomon parity).

```bash
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_all_findings.ts
make gui-run-sample SAMPLE=sample_data/diagnostics/sample_204byte_all_findings.ts
```
