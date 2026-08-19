# Options & output examples

Annotated output for every `mpeg-ts-analyzer` flag, run against the bundled
streams in `sample_data/`. See the [README](README.md) for install and usage.

## No options

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Input file:  sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Packet size: 188 bytes
Detected PAT: 1 program(s)
Detected PMT
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x02 (13818-2 video or 11172-2 constrained parameter video stream)
PMT : Program Info : elementary_PID     : 0x101, stream_type : 0x03 (11172 audio)
```

## Timestamp anomaly report (always on)

PTS/DTS anomalies are checked on every run, with no flag. Nothing is printed for
a healthy stream; when problems are found, a report is appended at the end:

```
-----------------------------
Timestamp Anomaly Report:
  PID 0x0100: PTS went backward at 0x00123457 (2001.000ms) after 0x00123456 (2041.000ms)
  PID 0x0100: PTS jumped forward 5.3s at 0x00234567 (7374.333ms) after 0x00123458 (2041.000ms), expected ~40ms
  PID 0x0101: PTS wrapped around (33-bit counter overflow) at 0x00345679
  PID 0x0102: DTS is later than PTS in the access unit at 0x00400000 (DTS 22.222ms > PTS 11.111ms)
```

Reading the output:

- Each line is one PID and the first place a problem was seen; repeats of the
  same problem on the same PID are collapsed with `(and N more)`.
- Checks run on the decode timeline (DTS when present, otherwise PTS), so a
  B-frame stream — whose PTS legitimately reorders — is not falsely flagged.

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
Packet size: 188 bytes
Detected PAT: 1 program(s)

===========================================
 PAT
===========================================
PAT : table_id                  : 0x0
PAT : section_syntax_indicator  : 1
PAT : section_length            : 13
PAT : transport_stream_id       : 1
PAT : version_number            : 0
PAT : current_next_indicator    : 1
PAT : section_number            : 0
PAT : last_section_number       : 0
PAT : program_number            : 1
PAT : program_map_PID           : 0x1000
PAT : CRC_32                    : 2ab104b2
Detected PMT

===========================================
 PMT
===========================================
PMT : table_id                          : 0x2
PMT : section_syntax_indicator          : 1
PMT : section_length                    : 29
PMT : program_number                    : 1
PMT : version_number                    : 0
PMT : current_next_indicator            : 1
PMT : section_number                    : 0
PMT : last_section_number               : 0
PMT : PCR_PID                           : 0x100
PMT : program_info_length               : 0
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x02 (13818-2 video or 11172-2 constrained parameter video stream)
PMT : Program Info : elementary_PID     : 0x101, stream_type : 0x03 (11172 audio)
PMT :   descriptor                      : ISO 639 language descriptor
PMT :     language_code                 : eng
PMT :     audio_type                    : 0x00 (undefined)
PMT : CRC_32                            : 11625f80
```

Descriptors found in a program's ES info loop are decoded and printed under the
corresponding `Program Info` line (as seen above for the audio stream's ISO 639
language descriptor). Other descriptor types are decoded similarly; for example
an AVC video descriptor on an H.264 stream is dumped as:

```
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x1b (AVC video stream as defined in ITU-T Rec. H.264|ISO/IEC 14496-10 Video)
PMT :   descriptor                      : AVC video descriptor
PMT :     profile_idc                   : 100 (High)
PMT :     level_idc                     : 40 (4.0)
```

mpeg-ts-analyzer decodes a curated set of the most commonly encountered
descriptors rather than aiming for exhaustive coverage. Descriptors outside this
set are still reported by tag with their raw payload, so nothing is silently
dropped.

Decoded descriptors, grouped by the standard that defines them (the
descriptor_tag range decides which one applies):

- ISO/IEC 13818-1 (ITU-T H.222.0), clause 2.6 — registration (0x05),
  ISO 639 language (0x0A), AVC video (0x28), HEVC video (0x38)
- ETSI EN 300 468 (DVB), clause 6.2 — teletext (0x56), subtitling (0x59),
  AAC (0x7C)

## Dump timestamp

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --dump-timestamp
Input file:  sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
Packet size: 188 bytes
Detected PAT: 1 program(s)
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

## Dump PCR jitter

`--dump-pcr-jitter` reports **PCR jitter** — the error in each PCR *value*, i.e.
how far the stamped clock is from the time that byte was actually delivered. This
is a different concern from the PCR *interval* check above (how often a PCR
appears): a PCR can arrive on time yet carry a wrong value, which upsets the
decoder's clock recovery (audio pops, video stutter).

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --dump-pcr-jitter
...
-----------------------------
PCR Jitter Summary (per-interval, byte-position model):
  (file-based estimate; assumes ~constant delivery rate, not a TR 101 290 measurement)
  Samples          : 60 PCR in 1 segment(s)
  Measured         : 58 interior samples
  Max jitter       : +23.333333ms at 0x00026244
  Min jitter       : -28.679245ms at 0x00008664
  Avg |jitter|     : 16.822812ms
  Within +/-500ns  : 0.0% (ISO/IEC 13818-1 accuracy limit)
  Status           : NG (max |jitter| exceeds the +/-25 microsec DVB limit)
  Discontinuities  : 0
```

Jitter magnitudes are printed in milliseconds (six decimals, so 1 ns is resolvable).
`Status` grades the worst-case `|jitter|` against the two standard limits:

- **OK** — within ±500 ns (ISO/IEC 13818-1 accuracy limit)
- **WARNING** — beyond ±500 ns but within ±25 µs (DVB receiver tolerance)
- **NG** — beyond ±25 µs

The result is a file-based estimate (there are no real arrival timestamps in a
file), not a hardware TR 101 290 measurement, as noted in the output header.

> The bundled sample shows large jitter because its PCR PID also carries VBR video,
> so the byte rate between PCRs varies a lot — not a clean CBR mux. A compliant
> CBR broadcast multiplex reports sub-microsecond jitter.

## Dump bitrate

`--dump-bitrate` summarizes how bandwidth is distributed across the analyzed
program's PIDs: the **average** and **peak** bitrate of each elementary stream,
the PSI overhead (PAT/PMT), and the program total. It answers questions like
"which stream is eating the bandwidth", "does the video meet its CBR/VBR target",
and "where does a live HLS/DASH pipeline buffer".

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts --dump-bitrate
...
-----------------------------
Bitrate Summary (PCR time base):
  PID     Type                                                                  Avg(bps)    Peak/1s(bps)
  0x0000  PAT                                                                     12,427          13,536
  0x0100  13818-2 video or 11172-2 constrained parameter video stream             59,905          60,160
  0x0101  11172 audio                                                            405,952         430,144
  0x1000  PMT                                                                     12,427          13,536
          Total (program)                                                        490,711         514,368
  0x1fff  NULL (mux-wide)                                                              0               0
  Duration: 4.72s (PCR, 1 segment(s))
```

Reading the output:

- **Avg** is the mean bitrate over the measured `Duration`; **Peak/1s** is the
  busiest single 1-second window (`N/A` for streams shorter than one second).
- **Total (program)** sums the analyzed program's PIDs. **NULL** (`0x1FFF`)
  stuffing is mux-wide, so it is listed separately and left out of the total.
- Byte counts are TS-layer: 188 bytes per packet, including for 192-byte M2TS.
- The segment count next to the duration reflects PCR discontinuities found in
  the stream.
