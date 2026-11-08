# Sample data

Small, healthy transport streams for trying the analyzer and for its tests.
Streams built to contain faults or edge cases are in
[`diagnostics/`](diagnostics/README.md). `<size>` is the packet size, 188
(TS) or 192 (M2TS), detected automatically. What each file contains and how it
was made is in [Sample data](../docs/development/sample-data.md).

- `sample_<size>byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts`: one program of MPEG-2 video and MP2 audio (FFmpeg); the first sample to try, used by the README demo and `make gui-run-sample` — [details](../docs/development/sample-data.md#healthy-samples).
- `sample_<size>byte_two_programs_mpeg2_160x120_25fps_mp2.ts`: two programs of MPEG-2 video and MPEG-1 audio (FFmpeg) for the multi-program listing and `--program` — [details](../docs/development/sample-data.md#healthy-samples).
- `sample_<size>byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts`: the two-program stream with multi-section PAT and PMTs (`tools/generate_multi_section_psi`) — [details](../docs/development/sample-data.md#multi-section-psi-sample).

```bash
./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
./mpeg-ts-analyzer --list-programs sample_data/sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts
make gui-run-sample   # the GUI on the first sample, whole file
```
