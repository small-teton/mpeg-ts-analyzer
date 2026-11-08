# PCR-to-PTS/DTS gap wraparound fixture

Write a short constant-rate stream whose PCR-to-PTS/DTS leads cross the 33-bit
wraparounds in both orders, then check it with TSDuck. The stream is
**intentionally unusual** (filler elementary-stream payload, an AAC stream
multiplexed 150 ms after its PTS): use it to test the PCR-PTS/DTS gap check,
`--dump-timestamp` and the mux lead across a wraparound
([#193](https://github.com/small-teton/mpeg-ts-analyzer/issues/193)), never as
an example of a compliant stream.

```sh
go run ./tools/generate_pts_gap_wraparound -output sample_data/diagnostics/sample_188byte_pts_gap_wraparound.ts
go run ./tools/generate_pts_gap_wraparound -m2ts -output sample_data/diagnostics/sample_192byte_pts_gap_wraparound.ts
go run ./tools/generate_pts_gap_wraparound -trailer -output sample_data/diagnostics/sample_204byte_pts_gap_wraparound.ts
tsp -I file sample_data/diagnostics/sample_188byte_pts_gap_wraparound.ts -P pcrextract --pcr --pts --dts --csv -O drop
tsp -I file --format m2ts sample_data/diagnostics/sample_192byte_pts_gap_wraparound.ts -P pcrextract --pcr --pts --dts --csv -O drop
tsp -I file --format rs204 sample_data/diagnostics/sample_204byte_pts_gap_wraparound.ts -P pcrextract --pcr --pts --dts --csv -O drop
```

200 packets of one 20 ms slot each (75.2 kbit/s, 4 s): program 1 (PMT PID
0x1000) with MPEG-2 video (stream_type 0x02) on PID 0x0100, which carries the
PCR, MPEG-1 audio (0x03) on 0x0101 and AAC (0x0F) on 0x0102. Packet 0 is a null
packet, the PAT is in packets 1 and 2 and the PMT in 3 and 4; the PAT repeats
in packets 20, 40, … and the PMT in 28, 48, …. Video takes the packets 4k+2
from 6 (every 80 ms, each with a PCR), AAC the packets 4k+1 from 5 and audio
the packets 4k+3 from 7. Every PES packet fits in one TS packet; the other
packets are null packets. With `-m2ts` each packet gets a `TP_extra_header`
whose `arrival_time_stamp` advances by one slot; with `-trailer` each packet is
followed by a 16-byte trailer of deterministic filler (never a sync byte),
making 204-byte packets. The lead is interpolated by file position between
PCRs and every packet has the same size, so the leads are the same for all
three packet sizes.

The 90 kHz clock of packet s is 2^33 − 100 × 1800 + s × 1800 (modulo 2^33), so
the PCR wraps at packet 100, between the PCRs of packets 98 and 102. Every
PTS/DTS is the clock of its packet plus a constant lead:

| PID | Lead | Wraps at | Order |
| --- | --- | --- | --- |
| 0x0100 video | DTS 700 ms, PTS 40 ms later | packet 66 (DTS) | PTS/DTS before PCR: packets 66 to 98 have a wrapped DTS and a PCR that has not wrapped; packet 98 is bracketed by the PCRs of 98 and 102 |
| 0x0101 audio | PTS 30 ms (small positive) | packet 99 | PTS before PCR, within the PCR bracket 98 to 102 that spans the PCR wrap |
| 0x0102 AAC | PTS −150 ms (negative) | packet 109 | PCR before PTS: packets 101 (bracket 98 to 102) and 105 (bracket 102 to 106) have a wrapped PCR and a PTS that has not wrapped |

Expected analysis (all three packet sizes; `tsparser` tests pin it and check that
the committed files equal the generator output):

- Every delay of `--dump-timestamp` is its stream's lead: 700 ms (video),
  30 ms (audio) and −150 ms (AAC). Exceptions: the AAC PES packet of packet 5
  lies before the first PCR (no delay), and the audio PES packet of packet 199
  ends after the last PCR, so it is shown against that PCR alone (50 ms) and
  not counted by the gap check.
- `compliance.max_pcr_pts_dts_gap` is `ok`, 700 ms over 144 observations: the
  48 bracketed PES packets of each PID, the same PES packets and leads as
  `multiplexing.mux_lead` (48 samples and 1 unbracketed per PID, −150 ms
  leads counted as negative).
- `timestamp_anomalies` lists one wraparound per PID (packets 66, 99 and 109).

Before #193 the analyzer compared raw values: the PES packets of packets 66 to
98 (video) and 99 (audio) got a gap of about −95443017 ms and −95443667 ms, the
AAC PES packet of packet 105 a gap of +95443567.688889 ms, which failed the
check (`ng`), and PES packets whose PCRs straddle the wrap were not measured.

TSDuck 3.45 (`pcrextract --pcr --pts --dts`; `--format m2ts` and
`--format rs204` for the 192- and 204-byte files) for all three files: its PCR and
PTS/DTS rows, interpolated by packet index between the PCRs before and after
each PES packet by an independent script (a step outside 0 to 1000 ms modulo
2^33 × 300 starts a new segment; the lead is read modulo 2^33 × 300 as the
signed difference nearest to zero), give 144 bracketed PES packets whose leads
equal the analyzer's `--dump-timestamp` delays within 10⁻⁶ ms (0 mismatches;
the analyzer before #193 differs on 12 of them). Its "Offset from PCR" column,
which it fills for the PES packets that carry a PCR (the video PID), is 63000
(DTS, 700 ms) and 66600 (PTS, 740 ms) for 40 video PES packets; for the other 9
(packets 66 to 98, whose DTS has wrapped while the PCR has not) it prints the
plain difference, 63000 − 2^33 and 66600 − 2^33, which is the same 700 ms and
740 ms modulo 2^33. For the 204-byte file `pcrextract` writes exactly the same
CSV as for the 188-byte file.
