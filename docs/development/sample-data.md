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
defects so that every Overview compliance card reports a problem in one
analysis — a 160 ms PCR interval, a 1200 ms PCR → PTS/DTS gap, a backward PTS,
one continuity-counter gap and a PCR that is 100 µs late. The stream is
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
