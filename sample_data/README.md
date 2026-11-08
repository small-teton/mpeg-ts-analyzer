# Sample data

Small transport streams for trying the analyzer and for its tests. This
directory holds ordinary, healthy streams; streams built to contain faults or
edge cases are in [`diagnostics/`](diagnostics/). The full description of
every sample, with the commands that made it, is in
[Sample data](../docs/development/sample-data.md).

| Sample | Packet sizes | Contents | Use | Produced by |
| --- | --- | --- | --- | --- |
| `sample_<size>byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts` | 188 (311 KB, 5 s), 192 (129 KB, 2 s) | One program: MPEG-2 video 320×240 at 25 fps and MP2 audio 48 kHz stereo (188-byte: PMT 0x1000, video 0x0100, audio 0x0101; 192-byte, FFmpeg's M2TS mode: PMT 0x0100, video 0x1011, audio 0x1100 as stream_type 0x06) | First look at the analyzer: no check reports a problem. Used by the README demo and `make gui-run-sample` | FFmpeg, see [Sample data](../docs/development/sample-data.md) |
| `sample_<size>byte_two_programs_mpeg2_160x120_25fps_mp2.ts` | 188 (203 KB), 192 (207 KB) | Two programs ("Main", PMT 0x1000, PIDs 0x0100/0x0101; "Second", PMT 0x1001, PIDs 0x0102/0x0103), each MPEG-2 video 160×120 at 25 fps with MPEG-1 audio, about 1.7 s | Multi-program listing and `--program`; GUI fixtures and screenshots | FFmpeg (the exact command was not recorded) |
| `sample_<size>byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts` | 188, 192 | The two-program stream with a PAT of one section per program and PMTs of one section per elementary stream, sent last section first | Tables split into sections ([PSI tables](../docs/cli/psi.md)); results match the single-section files | [`tools/generate_multi_section_psi`](../tools/generate_multi_section_psi/README.md) from the two-program samples |

`<size>` is the packet size: 188-byte TS or 192-byte M2TS (a 4-byte
TP_extra_header before each packet). The packet size is detected
automatically.

```bash
./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
./mpeg-ts-analyzer --list-programs sample_data/sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts
make gui-run-sample   # the GUI on the first sample, whole file
```
