# PES header and PTS repetition fixture

Write a short constant-rate stream with malformed PES headers and an excessive
PTS repetition interval, then check it with TSDuck and ffprobe. The stream is
**intentionally invalid**: use it to test the PES header and PTS repetition
checks.

```sh
go run ./tools/generate_pes_health -output sample_data/sample_188byte_pes_health.ts
go run ./tools/generate_pes_health -m2ts -output sample_data/sample_192byte_pes_health.ts
tsp -I file sample_data/sample_188byte_pes_health.ts -P pes --trace-packets -O drop
tsp -I file sample_data/sample_188byte_pes_health.ts -P pcrextract --pcr --pts --dts --csv -O drop
tsp -I file sample_data/sample_188byte_pes_health.ts -P continuity -O drop
ffprobe -show_streams sample_data/sample_188byte_pes_health.ts
```

200 packets of one 20 ms slot each (75.2 kbit/s, 4 s): program 1 (PMT PID
0x1000) with MPEG-2 video (stream_type 0x02) on PID 0x0100, which carries a PCR
in every packet, MPEG-1 audio (0x03) on 0x0101, PES private data (0x06) on
0x0102, MPEG-2 audio (0x04) on 0x0103 with a single PES packet (slot 15), and
AVC video (0x1B) on 0x0104, which the PMT announces but which carries nothing.
Packet 0 is a null packet, the PAT is in packets 1 and 2 and the PMT in 3 and
4, so the analysis starts at packet 5. Video takes the even packets from 6,
audio the packets 4k+1 from 5, private data the packets 8k+3 from 11 that PSI
does not use. Video headers carry PTS and DTS with `PES_packet_length` 0,
audio and private headers a PTS alone. The video PES of packet 10 is split:
its first packet holds only 12 header bytes and packet 12 the rest. `-clean`
writes the same stream without the defects.

| Packet | PID | Defect | Expected `kind` |
| --- | --- | --- | --- |
| 27 | 0x0102 | `packet_start_code_prefix` 0x000002 | `bad_start_code` |
| 35 | 0x0102 | only 8 header bytes before the next PES of the PID (packet 51) | `truncated` |
| 61 | 0x0101 | last PTS `marker_bit` 0 | `bad_marker_bits` |
| 67 | 0x0102 | `PTS_DTS_flags` `'01'` | `bad_flags` |
| 75 | 0x0102 | optional header starts with `'01'` | `bad_flags` |
| 91 | 0x0102 | PTS flag with `PES_header_data_length` 3 | `inconsistent_length` |
| 107 | 0x0102 | `PES_packet_length` 4, shorter than the 8-byte header | `inconsistent_length` |
| 115 | 0x0102 | PTS prefix `'0011'` without a DTS | `bad_marker_bits` |
| 100–150 | 0x0100 | PES packets without PTS or DTS | PTS interval 1080 ms (98 → 152) |

Expected analysis (both packet sizes; `tsparser` tests pin it and check that
the committed files equal the generator output):

- `pes_headers`: `errors`, 161 headers checked, 8 errors (video 96 checked, 0
  errors; audio 49 checked, 1; private 15 checked, 7; PID 0x0103 1 checked;
  PID 0x0104 `insufficient_data` / `no_pes`).
- `pts_repetition`: `ng`, one interval of 1080 ms from packet 98 to 152 on
  PID 0x0100 (68 intervals measured). Audio is `ok` with a maximum of 80 ms:
  the malformed header of packet 61 breaks the interval, so 57 → 65 (160 ms)
  is not measured. Private data is `skipped` (`not_video_or_audio`), PID
  0x0103 `insufficient_data` (`single_pts`) and 0x0104 `no_pes`.
- No timestamp anomaly: the 1080 ms DTS step over PES packets without
  timestamps is not reported as a jump, and malformed headers give no
  timestamps. PCR-PTS/DTS gap 300 ms, from valid headers only.
- Without the defects (`-clean`), every header is valid and the largest video
  interval is 80 ms (around the split PES).

TSDuck 3.45 for both files (`--format m2ts` for the 192-byte one):

- `pes --trace-packets`: 95 video PES (the last, unbounded one is not flushed
  without `-f`; the analyzer checks it at the end of the range, 96) and 49
  audio PES, as the analyzer. On PID 0x0102 it reports the packets 35
  (`no PES header found`), 91 (`truncated, missing 2 bytes`, from the length
  mismatch) and 107 (`expected header size: 14 bytes`) as invalid, and 11
  valid PES packets. It does not see packet 27 as a PES packet at all (no
  start code), and it does not check the `'10'` bits, `PTS_DTS_flags` `'01'`
  or marker bits, so it accepts 67 and 75.
- `pcrextract`: video PTS in packet 98 then 152, so the 1080 ms interval
  matches. Its audio PTS skip packet 61 as well, and its private-data PTS skip
  67 and 115. It lists 69 video PTS where the analyzer counts 70: it does not
  decode the PTS of the header split over packets 10 and 12. Interpolating its
  PCRs by packet index gives the same per-PID maximum interval as the analyzer
  for every sample in `sample_data`, except where the analyzer breaks an
  interval (audio 160 ms here, around the malformed header) or starts after the
  PAT/PMT acquisition.
- `continuity`: no discontinuity.

ffprobe 9.0 finds the same five streams on the same PIDs (mpeg2video, mp3,
bin_data, mp3, h264). Its parsers cannot frame the filler elementary-stream
payload, so it is not a per-PES reference for video and audio; for the private
data it demuxes 13 PES packets (without packets 27 and 35), 12 of them with a
PTS, so it also accepts the flag, length and marker-bit problems.
