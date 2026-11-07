# All-findings diagnostic fixture

Write a short constant-rate stream in which every Overview transport-health check
reports a problem, then check its defects with TSDuck. The stream is
**intentionally invalid**: use it to demonstrate and test verdicts, never as an
example of a compliant stream.

```sh
go run ./tools/generate_all_findings -output sample_data/sample_188byte_all_findings.ts
go run ./tools/generate_all_findings -m2ts -output sample_data/sample_192byte_all_findings.ts
tsanalyze --json sample_data/sample_188byte_all_findings.ts
tsp -I file sample_data/sample_188byte_all_findings.ts -P continuity -O drop
tsp -I file sample_data/sample_188byte_all_findings.ts -P pcrextract --pcr --pts --csv -O drop
tsp -I file sample_data/sample_188byte_all_findings.ts -P pcrverify -b 75200 -j 1 -O drop
tsp -I file --format m2ts sample_data/sample_192byte_all_findings.ts -P pcrverify -i -j 1 -O drop
```

120 packets of one 20 ms slot each (75.2 kbit/s, 2.4 s): program 1 (PMT PID
0x1000) with MPEG-2 video on PID 0x0100, which carries the PCR, and MPEG-1
audio on PID 0x0101. PAT and PMT are sent twice at the start (the analyzer
treats a table as complete at its next copy, so the analysis starts at packet
4) and then every 400 ms (PAT in packets 19, 39, ..., PMT in packets 23, 43,
...). From packet 4, video takes the even packets (every 40 ms), audio the
packets 4k+1 (every 80 ms), and the other packets 4k+3 are null packets. Each
PES packet fits in one TS packet with filler payload. PCR and PTS follow the
slot clock (10 s + slot × 20 ms), so the byte position is an exact time proxy.
The stream is long enough for two 1-second windows of PCR time, so the PCR
jitter analysis can establish constant delivery. The defects, by packet index:

| Check | Defect | Expected analysis |
|---|---|---|
| Max PCR interval | packets 22, 24 and 26 carry no PCR | `ng`, 160 ms (limit 100 ms), 54 intervals |
| PCR → PTS/DTS gap | every audio PTS is 1200 ms ahead of its PCR time | `ng`, 1200 ms (limit 1000 ms), 82 timestamps |
| PTS/DTS anomalies | video PTS in packet 46 is 40 ms before the previous one | `anomalies`: PID 0x0100 `backward` ×1 |
| Continuity counter | audio packet 25 skips counter 5 (carries 6), then continues | `errors`: 1 on PID 0x0101 (expected 5, actual 6) |
| PCR jitter | the PCR in packet 58 is 100 µs late | delivery `constant` (2 windows at 75,200 b/s, deviation 0.00 %), `ng`: max +100 µs at packet 58, −50 µs at packets 56 and 60 |
| Transport errors | null packet 7 has `transport_error_indicator` set | `affected`: 1 packet on PID 0x1fff (null), 0 excluded |
| Scrambled packets | null packet 11 has `transport_scrambling_control` 2 | `affected`: 1 packet on PID 0x1fff (null), 0 excluded |

Both new defects are on the null PID, whose payload the analysis never reads,
so they change no other result.

`-omit` leaves defects out (`pcr-interval`, `pcr-pts-gap`, `pts-backward`,
`cc-gap`, `pcr-jitter`, `tei`, `scrambled`, or `all`); `tsparser` tests use it to show that each
defect turns only its own check non-OK, and that the stream without defects is
OK everywhere, with constant delivery and zero estimated jitter. They also
check that the committed files equal the generator output and pin the results
above for both packet sizes.

The 192-byte form adds a TP_extra_header per packet whose arrival_time_stamp
advances by one slot (540,000 ticks of 27 MHz), the intended constant delivery.

TSDuck 3.45 agrees for both files:

- `tsanalyze`: 120 packets, 75,200 b/s from PCRs; PID 0x0100 58 packets with
  55 PCRs and 58 PTS; PID 0x0101 29 packets, 1 discontinuity; PAT 8, PMT 7,
  null 17 (tsanalyze leaves the packet with a transport error out of the PID
  statistics), 1 of them scrambled; 1 packet with a transport error.
  `tsdump --headers-only` shows `Error: 1` on packet 7 and `Scrambling: 2` on
  packet 11, both PID 0x1FFF.
- `continuity`: `packet index: 25, PID: 0x0101 (257), missing 1 packets`.
- `pcrextract`: 55 PCRs, largest difference 160 ms (packets 20 → 28); the
  video PTS of packet 46 (984,600) is below that of packet 44 (988,200);
  audio PTS minus the slot clock is 1200 ms. Grouping its PCRs into 1-second
  windows gives 75,200 b/s in both windows, as the analyzer's delivery
  assessment does.
- `pcrverify` at 75,200 b/s (and with the M2TS arrival timestamps, `-i`):
  `52 PCR OK, 2 with jitter > 27 (1 micro-seconds)`, ±2,700 PCR units
  (100 µs). TSDuck compares consecutive PCRs, so the late PCR shows on the
  steps into and out of packet 58; this analyzer compares each PCR with its two
  neighbors interpolated by byte position, so it reports +100 µs at packet 58
  and −50 µs at its neighbors. Both find the same 100 µs error. Without
  defects (`-omit all`), `pcrverify` reports `57 PCR OK, 0 with jitter > 27`
  in both forms and the analyzer `constant` / `ok` with zero jitter.
