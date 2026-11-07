# Mux lead fixture

Write a short constant-rate stream with known mux leads, then check it with
TSDuck. The stream is **intentionally unusual** (filler elementary-stream
payload, a large audio preload that fails the PCR-PTS/DTS gap check, a
transport error, a PES packet multiplexed after its PTS): use it to test the
report-only mux lead, never as an example of a compliant stream.

```sh
go run ./tools/generate_mux_lead -output sample_data/sample_188byte_mux_lead.ts
go run ./tools/generate_mux_lead -m2ts -output sample_data/sample_192byte_mux_lead.ts
tsp -I file sample_data/sample_188byte_mux_lead.ts -P pcrextract --pcr --pts --dts --csv -O drop
tsp -I file --format m2ts sample_data/sample_192byte_mux_lead.ts -P pcrextract --pcr --pts --dts --csv -O drop
```

240 packets of one 20 ms slot each (75.2 kbit/s, 4.8 s): program 1 (PMT PID
0x1000) with MPEG-2 video (stream_type 0x02) on PID 0x0100, which carries a PCR
in every packet, MPEG-1 audio (0x03) on 0x0101, AAC (0x0F) on 0x0102 and PES
private data (0x06) on 0x0103 with a single PES packet (packet 15). Packet 0 is
a null packet, the PAT is in packets 1 and 2 and the PMT in 3 and 4, so the
analysis starts at packet 5; the PAT repeats in packets 19, 39, … and the PMT
in 23, 43, …. Video takes the even packets from 6, audio the packets 4k+1 from
5, AAC the packets 8k+3 from 11 that PSI does not use. Every PES packet fits in
one TS packet.

The 90 kHz clock of packet s is 2^33 − 100 × 1800 + s × 1800 (modulo 2^33), so
the PCR wraps at packet 100 and each PTS/DTS wraps its lead earlier; from
packet 150 the clock is 10 s later and the PCR of packet 150 carries
`discontinuity_indicator`. Every PTS/DTS is the clock of its packet plus:

| PID | Lead |
| --- | --- |
| 0x0100 | DTS 700 + 2 × ((s / 2) mod 5) ms (700 to 708 ms), PTS 40 ms after the DTS |
| 0x0101 | PTS 1500 ms (a large audio preload); packet 61 has `transport_error_indicator` set |
| 0x0102 | PTS 650 ms, except packet 51 (−20 ms) and packet 67 (no PTS) |
| 0x0103 | PTS 300 ms |

Expected analysis (both packet sizes; `tsparser` tests pin it and check that
the committed files equal the generator output), `multiplexing.mux_lead`:

| PID | Samples | Unbracketed | Min (packet) | Max (packet) | Spread |
| --- | --- | --- | --- | --- | --- |
| 0x0100 video | 115 (DTS) | 2: packet 148 (the next PCR is a new segment), 238 (no PCR after it) | 700 ms (10) | 708 ms (8) | 8 ms |
| 0x0101 audio | 56 | 2: packet 5 (before the first PCR), 149 (across the discontinuity) | 1500 ms (9) | 1500 ms (9) | 0 ms |
| 0x0102 audio | 17, 1 negative | 0 | −20 ms (51) | 650 ms (11) | 670 ms |
| 0x0103 other | 1 | 0 | 300 ms (15) | 300 ms (15) | 0 ms |

The transport error excludes the audio PES of packet 61 (59 audio PES packets
= 56 samples + 2 unbracketed + 1 excluded). The A/V lead difference of program
1 is 800 ms (audio PID 0x0101, lead 1500 ms at packet 9, against video lead
700 ms at packet 10), with 728 ms for the AAC PID (−20 ms at packet 51 against
708 ms at packet 68). The PCR-PTS/DTS gap check is `ng` at 1500 ms, the
largest of the same leads, over 189 observations, the same PES packets as the
mux lead samples (before
[#193](https://github.com/small-teton/mpeg-ts-analyzer/issues/193) it skipped
the video PES packet of packet 98, whose PCRs straddle the wrap, and
`--dump-timestamp` showed the PES packets whose PTS/DTS had wrapped before the
PCR about 2^33 / 90 kHz off); `--fail-on-error` names only that check, and the
mux lead adds no verdict. The timestamp anomaly report lists the wraps, the
10 s jump and the −20 ms AAC PTS that goes backward.

TSDuck 3.45 (`pcrextract --pcr --pts --dts`) for both files: its PCR and
PTS/DTS rows, interpolated by packet index between the PCRs before and after
each PES packet by an independent script (a step outside 0 to 1000 ms starts a
new segment; the lead is read modulo 2^33 × 300), give exactly the analyzer's
sample counts, unbracketed counts, minimum and maximum leads and their offsets
for every PID. `pcrextract` also skips the audio PES of packet 61. Its "Offset
from PCR" column, which it fills for the PES packets that carry a PCR (the
video PID), is 63000 + 180 × ((s / 2) mod 5) (DTS) and 3600 more (PTS), that
is 700 to 708 ms and 740 to 748 ms, as the analyzer's video leads, for 100 of
the 117 video PES packets. For the other 17 (packets 66 to 98, whose DTS has
wrapped while the PCR has not yet) it prints the plain difference, about
−2^33; modulo 2^33 it is the same 700 to 708 ms, which the analyzer reports.

ffprobe 9.0 finds the four streams on the same PIDs (mpeg2video, mp3, aac,
bin_data), but its parsers cannot frame the filler payload and merge the PES
packets, so it is not a per-PES reference here; see the pull request of
issue #177 for the ffprobe cross-check on the encoded samples in
`sample_data`.
