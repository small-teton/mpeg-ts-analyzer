# PAT/PMT version change reference fixture

Rewrite the single-program sample so that its PMT and then its PAT change
version mid-stream, then check the tables with TSDuck:

```sh
go run ./tools/generate_psi_version_change \
  -input sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts \
  -output sample_data/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts
go run ./tools/generate_psi_version_change -m2ts \
  -input sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts \
  -output sample_data/sample_192byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts
tsp -I file sample_data/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts \
  -P tables --pid 0 --pid 0x1000 --pid 0x1100 --diversified-payload --packet-index -O drop
tsp -I file --format m2ts sample_data/sample_192byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts \
  -P tables --pid 0 --pid 0x1000 --pid 0x1100 --diversified-payload --packet-index -O drop
tsanalyze --json sample_data/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts
```

- PMT version 1, from packet 545 on PID 0x1000: `PCR_PID` 0x0200, video moved
  from PID 0x0100 to 0x0200, audio PID 0x0101 `stream_type` 0x03 -> 0x04.
  Video packets move to 0x0200 from the first video `payload_unit_start_indicator`
  after that packet.
- PAT version 1, from packet 1131: program 1's PMT moves to PID 0x1100, which
  carries the same PMT version 1 from packet 1132.

The 192-byte fixture is the 188-byte one with a 4-byte TP_extra_header per
packet (constant arrival_time_stamp step), because the 192-byte sample uses
other PIDs. Packet count, continuity counters, PCRs and timestamps stay as in
the input. TSDuck 3.45 reports, for both files, PAT version 0 in packet 1, PMT
version 0 in packet 2, PMT version 1 in packet 545, PAT version 1 in packet
1131 and PMT version 1 on PID 0x1100 in packet 1132, and per-PID packet counts
PAT 43, 0x0100 70, 0x0101 1359, 0x0200 132, 0x1000 29, 0x1100 14 (service 1:
PCR PID 0x0200, PMT PID 0x1100 at the end). `tsparser` tests check that the
committed fixtures equal the generator output.
