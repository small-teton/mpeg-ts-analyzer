# mpeg-ts-analyzer

![Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/small-teton/9d60b1e4226ac2926940b20ce3381621/raw/coverage.json)

mpeg-ts-analyzer is an analyzer for MPEG-2 Transport Stream (ISO/IEC 13818-1).

It parses TS packets and checks whether the stream conforms to the following requirements defined in the specification:

- **Max PCR interval** should be no greater than 100ms (ISO/IEC 13818-1, Section 2.7.2)
- **PCR-PTS max gap** (end-to-end delay) should be no greater than 1000ms

In addition, it can dump various MPEG-2 TS internal structures for stream investigation purposes:

- TS header and payload
- Adaptation Field (including PCR)
- PSI tables (PAT/PMT) with CRC32 validation
- PES header with PTS/DTS timestamps
- continuity_counter validation

Both 188-byte TS packets and 192-byte M2TS packets (BDAV format with TP_extra_header) are supported. The packet size is auto-detected from the stream.

**Note:** The correctness of the output is not guaranteed.

Sample TS files are included in `sample_data/` for quick testing:

```bash
# 188-byte TS
ffmpeg -f lavfi -i "color=c=blue:s=320x240:d=5,format=yuv420p" \
       -f lavfi -i "anullsrc=r=48000:cl=stereo" \
       -t 5 -c:v mpeg2video -c:a mp2 -f mpegts sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts

# 192-byte M2TS
ffmpeg -f lavfi -i "color=c=red:s=320x240:d=2,format=yuv420p" \
       -f lavfi -i "anullsrc=r=48000:cl=stereo" \
       -t 2 -c:v mpeg2video -c:a mp2 -f mpegts -mpegts_m2ts_mode 1 sample_data/sample_192byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
```

# Usage

By default, it dumps all timestamps (PCR/PTS/DTS) including PCR interval and PCR-PTS gap. To dump more details, add the corresponding command-line flags.

```
Usage:
  mpeg-ts-analyzer [input file path] [flags]

Flags:
      --dump-adaptation-field   Dump TS packet adaptation_field detail.
      --dump-pes-header         Dump PES packet header detail.
      --dump-psi                Dump PSI(PAT/PMT) detail.
      --dump-timestamp          Dump PCR/PTS/DTS timestamps.
      --dump-ts-header          Dump TS packet header.
      --dump-ts-payload         Dump TS packet payload binary.
  -h, --help                    help for mpeg-ts-analyzer
      --version                 show mpeg-ts-analyzer version.
```

# Result Examples

## No option

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Input file:  sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Detected PAT: PMT pid = 0x1000
Detected PMT
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x02 (13818-2 video or 11172-2 constrained parameter video stream)
PMT : Program Info : elementary_PID     : 0x101, stream_type : 0x03 (11172 audio)
```

## Dump TS header

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --dump-ts-header
Input file:  sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
===============================================================
 TS Header
===============================================================
transport_error_indicator       : 0
payload_unit_start_indicator    : 1
transport_priority              : 0
pid                             : 0x0
transport_scrambling_control    : 0
adaptation_field_control        : 1
continuity_counter              : 0
===============================================================
 TS Header
===============================================================
transport_error_indicator       : 0
payload_unit_start_indicator    : 1
transport_priority              : 0
pid                             : 0x1000
transport_scrambling_control    : 0
adaptation_field_control        : 1
continuity_counter              : 0
===============================================================
 TS Header
===============================================================
transport_error_indicator       : 0
payload_unit_start_indicator    : 1
transport_priority              : 0
pid                             : 0x100
transport_scrambling_control    : 0
adaptation_field_control        : 3
continuity_counter              : 0
```

## Dump PSI

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --dump-psi
Input file:  sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Detected PAT: PMT pid = 0x1000

===========================================
 PAT
===========================================
PAT : table_id                          : 0x0
PAT : section_syntax_indicator          : 1
PAT : section_length                    : 13
PAT : transport_stream_id               : 1
PAT : version_number                    : 0
PAT : current_next_indicator            : 1
PAT : section_number                    : 0
PAT : last_section_number               : 0
PAT : program_number                    : 1
PAT : program_map_PID                   : 0x1000
PAT : CRC_32                            : 2ab104b2
Detected PMT

===========================================
 PMT
===========================================
PMT : table_id                          : 0x2
PMT : section_syntax_indicator          : 1
PMT : section_length                    : 23
PMT : program_number                    : 1
PMT : version_number                    : 0
PMT : current_next_indicator            : 1
PMT : section_number                    : 0
PMT : last_section_number               : 0
PMT : PCR_PID                           : 0x100
PMT : program_info_length               : 0
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x02 (13818-2 video or 11172-2 constrained parameter video stream)
PMT : Program Info : elementary_PID     : 0x101, stream_type : 0x03 (11172 audio)
PMT : CRC_32                            : f64a0355
```

## Dump timestamp

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --dump-timestamp
Input file:  sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Detected PAT: PMT pid = 0x1000
Detected PMT
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x02 (13818-2 video or 11172-2 constrained parameter video stream)
PMT : Program Info : elementary_PID     : 0x101, stream_type : 0x03 (11172 audio)
0x000034e0 PCR: 0x018344a0[00940.000000ms]
0x000034e0 DTS: 0x00024090[01640.000000ms] (pid:0x100) (delay:700.000000ms)
0x0000359c PTS: 0x00023a3a[01621.977778ms] (pid:0x101) (delay:668.922222ms)
0x00004970 PCR: 0x01a43a20[01020.000000ms] (Interval:00080.000000ms)
0x00003f28 DTS: 0x00024ea0[01680.000000ms] (pid:0x100) (delay:557.222222ms)
0x00004970 DTS: 0x00025cb0[01720.000000ms] (pid:0x100) (delay:700.000000ms)
0x00003fe4 PTS: 0x00024b1a[01669.977778ms] (pid:0x101) (delay:534.144444ms)
0x000055ec PCR: 0x01c52fa0[01100.000000ms] (Interval:00080.000000ms)
0x00004ba4 DTS: 0x00026ac0[01760.000000ms] (pid:0x100) (delay:709.400000ms)
0x00004c60 PTS: 0x00025bfa[01717.977778ms] (pid:0x101) (delay:657.177778ms)

(snip)

0x0004a66c PCR: 0x091bd920[05660.000000ms] (Interval:00080.000000ms)
0x00049c24 DTS: 0x0008ade0[06320.000000ms] (pid:0x100) (delay:729.563591ms)
0x00049ce0 PTS: 0x00089f1a[06277.977778ms] (pid:0x101) (delay:684.062566ms)
0x0004a728 PTS: 0x0008affa[06325.977778ms] (pid:0x101) (delay:662.486106ms)
0x0004b0b4 PTS: 0x0008c0da[06373.977778ms] (pid:0x101) (delay:665.094372ms)
-----------------------------
Max PCR interval: 80.000000ms
PCR-PTS max gap: 729.563591ms
```

# Development

## Test & Coverage

```bash
make setup      # configure git hooks (run once after clone)
make test       # run all tests
make coverage   # run tests with coverage report
make clean      # remove build/coverage artifacts
```

Coverage is measured for `bitbuffer` and `tsparser` packages only. CLI entrypoint (`cmd`, `main.go`) is excluded from coverage targets. Both packages should maintain 100% coverage.

A pre-push hook (`make setup` to enable) runs build, test, and coverage checks before every push. Push is rejected if coverage drops below 100%.
