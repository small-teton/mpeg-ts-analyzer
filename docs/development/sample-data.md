# Sample data

[Documentation home](../index.md)

Sample TS files are included in `sample_data/` for quick testing:

```bash
# 188-byte TS
ffmpeg -f lavfi -i "color=c=blue:s=320x240:d=5,format=yuv420p" \
       -f lavfi -i "anullsrc=r=48000:cl=stereo" \
       -t 5 -c:v mpeg2video -c:a mp2 -metadata:s:a:0 language=eng \
       -f mpegts sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts

# 192-byte M2TS
ffmpeg -f lavfi -i "color=c=red:s=320x240:d=2,format=yuv420p" \
       -f lavfi -i "anullsrc=r=48000:cl=stereo" \
       -t 2 -c:v mpeg2video -c:a mp2 -metadata:s:a:0 language=eng \
       -f mpegts -mpegts_m2ts_mode 1 sample_data/sample_192byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
```

## Diagnostic sample with every Overview finding

`sample_data/sample_188byte_all_findings.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_all_findings.ts` (23 KiB each) are **intentionally
invalid**. They are not examples of a good stream: each carries deliberate
defects so that every Overview transport-health check reports a problem in one
analysis — a 160 ms PCR interval, a 1200 ms PCR → PTS/DTS gap, a backward PTS,
one continuity-counter gap, a PCR that is 100 µs late, a null packet with
`transport_error_indicator` set, a scrambled null packet and an 800 ms gap
between two PATs and between two PMTs. The stream is
delivered at a constant 75.2 kbit/s for 2.4 s, long enough for two 1-second
windows, so the PCR jitter analysis finds constant delivery and judges the
late PCR (`NG`). Use the normal sample above for a first look at the analyzer.

```bash
go run ./tools/generate_all_findings -output sample_data/sample_188byte_all_findings.ts
go run ./tools/generate_all_findings -m2ts -output sample_data/sample_192byte_all_findings.ts
./mpeg-ts-analyzer sample_data/sample_188byte_all_findings.ts --dump-pcr-jitter
make gui-run GUI_ARGS='sample_data/sample_188byte_all_findings.ts'
```

The generator, the position of each defect, the expected results and the
TSDuck cross-check are described in
[`tools/generate_all_findings`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_all_findings).
Tests check that the committed files equal the generator output and pin the
verdicts for both packet sizes; the GUI's Overview tests use the CLI's analysis
of the 188-byte file (`gui/tests/fixtures/analysis_all_findings_188.json`),
which `make gui-test` compares with the output of the CLI built from the tree.

## Diagnostic sample with TS synchronization loss

`sample_data/sample_188byte_sync_loss.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_sync_loss.ts` (39 KiB each) are also
**intentionally invalid**. A clean 2-second stream is damaged in four places:
a corrupted sync byte, one inserted byte, one deleted byte and a 1000-byte
block of garbage with false sync bytes; it ends with a 100-byte partial packet.
The analyzer reports four losses, each recovered, with their byte offsets and
the skipped bytes, and continues the analysis after each
([TS synchronization](../cli/usage.md#ts-synchronization-always-on)).

```bash
go run ./tools/generate_sync_loss -output sample_data/sample_188byte_sync_loss.ts
go run ./tools/generate_sync_loss -m2ts -output sample_data/sample_192byte_sync_loss.ts
./mpeg-ts-analyzer sample_data/sample_188byte_sync_loss.ts
make gui-run GUI_ARGS='sample_data/sample_188byte_sync_loss.ts'
```

The generator, the position of each defect, the expected results and the
TSDuck cross-check are described in
[`tools/generate_sync_loss`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_sync_loss).
Tests check that the committed files equal the generator output and pin the
results for both packet sizes; the GUI's tests use the CLI's analysis of the
188-byte file (`gui/tests/fixtures/analysis_sync_loss_188.json`), which `make
gui-test` compares with the output of the CLI built from the tree.

## Transport-error and scrambling sample

`sample_data/sample_188byte_transport_errors.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_transport_errors.ts` (19 KiB each) are also
**intentionally invalid**. They carry packets with `transport_error_indicator`
set and scrambled packets on the PAT, PMT, PCR, elementary-stream,
unreferenced and null PIDs, each built so that using its data would show as a
wrong PTS, PCR or table, to test how the analyzer counts them and keeps their
data out of the analysis.

```bash
go run ./tools/generate_transport_errors -output sample_data/sample_188byte_transport_errors.ts
go run ./tools/generate_transport_errors -m2ts -output sample_data/sample_192byte_transport_errors.ts
./mpeg-ts-analyzer sample_data/sample_188byte_transport_errors.ts
```

The packets, the expected results and the TSDuck cross-check are described in
[`tools/generate_transport_errors`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_transport_errors).

## PAT/PMT health sample

`sample_data/sample_188byte_psi_health.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_psi_health.ts` (28 KiB each) are also
**intentionally invalid**. Two programs share one video PID; the stream has
no PAT for 1.2 s, a PMT section on the PAT PID, a PMT with an invalid CRC_32
and a version change, and a two-section PMT whose second section is never
sent, so every table of the PAT/PMT health check is `NG` for a different
reason.

```bash
go run ./tools/generate_psi_health -output sample_data/sample_188byte_psi_health.ts
go run ./tools/generate_psi_health -m2ts -output sample_data/sample_192byte_psi_health.ts
./mpeg-ts-analyzer sample_data/sample_188byte_psi_health.ts --program 1
```

The generator also writes one focused scenario at a time (`-scenario
pat-late`, `pmt-missing`, `crc`, `no-pcr`, ...), which the tests use. The
scenarios, the expected results and the TSDuck cross-check are described in
[`tools/generate_psi_health`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_psi_health).
