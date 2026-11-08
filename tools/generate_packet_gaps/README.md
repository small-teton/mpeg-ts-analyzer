# Packet-gap fixture

Write a 4 s constant-rate, poorly interleaved stream (a bursty remux) for the
report-only per-PID maximum packet gaps, then cross-check it. It is a test
fixture, not an example of good multiplexing.

```sh
go run ./tools/generate_packet_gaps -output sample_data/diagnostics/sample_188byte_packet_gaps.ts
go run ./tools/generate_packet_gaps -m2ts -output sample_data/diagnostics/sample_192byte_packet_gaps.ts
go run ./tools/generate_packet_gaps -trailer -output sample_data/diagnostics/sample_204byte_packet_gaps.ts
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_packet_gaps.ts
tsanalyze --json sample_data/diagnostics/sample_188byte_packet_gaps.ts
tsanalyze --format m2ts --json sample_data/diagnostics/sample_192byte_packet_gaps.ts
```

100 frames of 40 ms, each of eight 5 ms packet slots (800 packets,
300.8 kbit/s); packet `n` is slot `n % 8` of frame `n / 8`. Slot 0 is a
PCR-only packet on the PCR_PID 0x01FF (PCR of frame `f` = 10 s + `f` × 40 ms,
plus 10 s from frame 50, where `discontinuity_indicator` declares the jump),
slots 1–3 MPEG-2 video on 0x0100, slot 4 the PAT (`f % 4 == 0`), the PMT on
0x1000 (`f % 4 == 2`), the SDT PID 0x0011 (`f % 8 == 1`) or the single packet
of 0x0012 (frame 43), and slots 5–7 the bursts: three MPEG-1 audio packets on
0x0101 in the frames `f % 8 == 0` except frame 56, and one private-data packet
on 0x0102 in slot 5 of the frames `f % 8 == 4` while the PMT references it.
PMT version 1 (from frame 30) removes 0x0102 and version 2 (from frame 70)
adds it back. The analysis, and with it the PCR time basis, starts at packet
85 after the PAT and PMT acquisition; its first PCR is packet 88.

| PID | Packets | Expected largest gap |
| --- | --- | --- |
| 0x0000 PAT | 25 | 31 packets, 160 ms (packets 100–132; the equal gaps before the first PCR are passed over) |
| 0x0011 SDT | 13 | 63 packets, 320 ms |
| 0x0012 | 1 | none: `insufficient_data` (`no_consecutive_packets`) |
| 0x0100 video | 300 | 5 packets, 30 ms |
| 0x0101 audio | 36 | 125 packets (bursts of frames 48 and 64), at least 590 ms: the PCR interval across the discontinuity is not counted (`max_gap_exact: false`) |
| 0x0102 private data | 7 | 63 packets, 320 ms; the 383 packets across its absence (frames 28 to 76) are not a gap |
| 0x01FF PCR | 100 | 7 packets, 40 ms |
| 0x1000 PMT | 25 | 31 packets, 160 ms |

`tsparser` tests pin these results for every packet size and check that the
committed files equal the generator output. The 204-byte form (`-trailer`)
follows each packet with a 16-byte trailer of deterministic filler (never a
sync byte); its gaps are the same numbers of packets and milliseconds, with
offsets at 204·packet. The timestamp anomaly check
reports the PTS jumps at the declared PCR discontinuity.

Cross-check (every file):

- `tsanalyze --json` (TSDuck 3.45): the per-PID packet counts equal the
  analyzer's `packet_count` for every PID (800 packets, 300,800 b/s; for the
  204-byte file also `bitrate-204` 326,400 b/s, the physical rate).
- An independent raw scan (Python, packet index of each PID, packets with
  `transport_error_indicator` skipped, no analyzer code) gives the same
  `max_gap_packets` for every PID except 0x0102, where it finds 383 across the
  absence. Restarting its chain at the packets from which PMT versions 1 and
  2 apply (packets 245 and 565) gives 63, the analyzer's value.
- The times follow from the slot clock: a gap of `n` packets inside one
  timing segment spans `(n + 1)` × 5 ms.
