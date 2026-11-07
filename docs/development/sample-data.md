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
`transport_error_indicator` set and a scrambled null packet. The stream is
delivered at a constant 75.2 kbit/s for 2.4 s, long enough for two 1-second
windows, so the PCR jitter analysis finds constant delivery and judges the
late PCR (`NG`). Its PCR clock is continuous, so PCR discontinuities is the one
check it leaves `OK`; the [PCR discontinuity sample](#pcr-discontinuity-sample)
covers that check. Use the normal sample above for a first look at the
analyzer.

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

## PCR discontinuity sample

`sample_data/sample_188byte_pcr_discontinuities.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_pcr_discontinuities.ts` (48 KiB each) are
**intentionally invalid**. Their PCR, one every 40 ms of a constant 75.2 kbit/s
multiplex, passes through every kind of PCR step the analyzer distinguishes: a
33-bit wraparound, two declared discontinuities (one declared on an earlier
PCR-less packet), an undeclared backward step, an undeclared forward jump of
4 s and a `PCR_PID` change. Max PCR interval stays `OK` (40 ms) while
PCR discontinuities is `NG`; see [PCR steps](../cli/usage.md#pcr-steps).

```bash
go run ./tools/generate_pcr_discontinuities -output sample_data/sample_188byte_pcr_discontinuities.ts
go run ./tools/generate_pcr_discontinuities -m2ts -output sample_data/sample_192byte_pcr_discontinuities.ts
./mpeg-ts-analyzer sample_data/sample_188byte_pcr_discontinuities.ts --dump-pcr-jitter --dump-bitrate
```

The generator, the expected results and the TSDuck cross-check are described
in
[`tools/generate_pcr_discontinuities`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_pcr_discontinuities).
The GUI's Overview tests use the CLI's analysis of the 188-byte file
(`gui/tests/fixtures/analysis_pcr_discontinuities_188.json`), which
`make gui-test` compares with the output of the CLI built from the tree.

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
