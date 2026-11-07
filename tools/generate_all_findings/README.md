# All-findings diagnostic fixture

Write a short constant-rate stream in which every Overview compliance card
reports a problem, then check its defects with TSDuck. The stream is
**intentionally invalid**: use it to demonstrate and test verdicts, never as an
example of a compliant stream.

```sh
go run ./tools/generate_all_findings -output sample_data/sample_188byte_all_findings.ts
go run ./tools/generate_all_findings -m2ts -output sample_data/sample_192byte_all_findings.ts
tsanalyze --json sample_data/sample_188byte_all_findings.ts
tsp -I file sample_data/sample_188byte_all_findings.ts -P continuity -O drop
tsp -I file sample_data/sample_188byte_all_findings.ts -P pcrextract --pcr --pts --csv -O drop
tsp -I file sample_data/sample_188byte_all_findings.ts -P pcrverify -b 150400 -j 1 -O drop
tsp -I file --format m2ts sample_data/sample_192byte_all_findings.ts -P pcrverify -i -j 1 -O drop
```

80 packets of one 10 ms slot each (150.4 kbit/s): program 1 (PMT PID 0x1000)
with MPEG-2 video on PID 0x0100, which carries the PCR, and MPEG-1 audio on PID
0x0101; the remaining slots are null packets. PAT and PMT are sent twice at the
start (the analyzer treats a table as complete at its next copy, so the
analysis starts at packet 4) and again at packets 40 and 41. Each PES packet
fits in one TS packet with filler payload. PCR and PTS follow the slot clock
(10 s + slot × 10 ms), so the byte position is an exact time proxy. The defects,
by packet index:

| Card | Defect | Expected analysis |
|---|---|---|
| Max PCR interval | packets 26, 30 and 34 carry no PCR | `ng`, 160 ms (limit 100 ms), 15 intervals |
| PCR → PTS/DTS gap | every audio PTS is 1200 ms ahead of its PCR time | `ng`, 1200 ms (limit 1000 ms), 23 timestamps |
| PTS/DTS anomalies | video PTS in packet 46 is 40 ms before the previous one | `anomalies`: PID 0x0100 `backward` ×1 |
| Continuity counter | audio packet 44 skips counter 5 (carries 6), then continues | `errors`: 1 on PID 0x0101 (expected 5, actual 6) |
| PCR jitter | the PCR in packet 58 is 100 µs late | `ng`: max +100 µs at packet 58, −50 µs at packets 54 and 62 |

`-omit` leaves defects out (`pcr-interval`, `pcr-pts-gap`, `pts-backward`,
`cc-gap`, `pcr-jitter`, or `all`); `tsparser` tests use it to show that each
defect turns only its own card non-OK, and that the stream without defects is
OK everywhere, with zero estimated jitter. They also check that the committed
files equal the generator output and pin the results above for both packet
sizes.

The 192-byte form adds a TP_extra_header per packet whose arrival_time_stamp
advances by one slot (270,000 ticks of 27 MHz), the intended constant delivery.

TSDuck 3.45 agrees for both files:

- `tsanalyze`: 80 packets, 150,400 b/s from PCRs; PID 0x0100 19 packets with
  16 PCRs and 19 PTS; PID 0x0101 10 packets, 1 discontinuity; PAT 3, PMT 3,
  null 45.
- `continuity`: `packet index: 44, PID: 0x0101 (257), missing 1 packets`.
- `pcrextract`: 16 PCRs, largest difference 160 ms (packets 22 → 38); the
  video PTS of packet 46 (943,200) is below that of packet 42 (946,800);
  audio PTS minus the slot clock is 1200 ms.
- `pcrverify` at 150,400 b/s (and with the M2TS arrival timestamps, `-i`):
  `13 PCR OK, 2 with jitter > 27 (1 micro-seconds)`, ±2,700 PCR units
  (100 µs). TSDuck compares consecutive PCRs, so the late PCR shows on the
  steps into and out of packet 58; this analyzer compares each PCR with its two
  neighbors interpolated by byte position, so it reports +100 µs at packet 58
  and −50 µs at its neighbors. Both find the same 100 µs error.
