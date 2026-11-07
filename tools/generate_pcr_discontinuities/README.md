# PCR discontinuity fixture

Write a short constant-rate stream whose PCR passes through every kind of PCR
step the analyzer distinguishes, then check it with TSDuck. The stream is
**intentionally invalid** (two steps have no `discontinuity_indicator`, which
ISO/IEC 13818-1, 2.4.3.5 requires): use it to test PCR timing, never as an
example of a compliant stream.

```sh
go run ./tools/generate_pcr_discontinuities -output sample_data/sample_188byte_pcr_discontinuities.ts
go run ./tools/generate_pcr_discontinuities -m2ts -output sample_data/sample_192byte_pcr_discontinuities.ts
tsp -I file sample_data/sample_188byte_pcr_discontinuities.ts -P pcrextract --pcr --csv -O drop
tsp -I file sample_data/sample_188byte_pcr_discontinuities.ts -P pcrverify -P timewrap -O drop
tsp -I file --format m2ts sample_data/sample_192byte_pcr_discontinuities.ts -P pcrverify -i -P timewrap -O drop
```

260 packets of one 20 ms slot each (75.2 kbit/s, 5.2 s): program 1 (PMT PID
0x1000) with MPEG-2 video announced on PID 0x0100. Packet 0 is a null packet,
the PAT is in packets 1 and 2 and the PMT in 3 and 4, so the analysis starts at
packet 5; the PAT repeats in packets 19, 39, … and the PMT in 23, 43, …. Every
even packet from 6 is an adaptation-field-only packet with a PCR (every 40 ms);
there is no PES. The PCR follows the slot clock, starting 1 s before the
2^33 × 300 wrap. `-clean` writes the same stream without the events below (one
continuous clock through the wraparound, on PID 0x0100).

| Packet | Event | Analyzer step kind |
| --- | --- | --- |
| 50 | the PCR wraps from 2^33 × 300 − 40 ms to 0 | `wraparound` (a 40 ms interval) |
| 80 | +5000 ms, `discontinuity_indicator` on the PCR packet | `declared` |
| 109, 110 | `discontinuity_indicator` on a PCR-less packet of PID 0x0100, then −3000 ms | `declared` |
| 140 | −2000 ms without `discontinuity_indicator` | `backward` |
| 170 | +4000 ms without `discontinuity_indicator` | `forward_jump` |
| 201, 203, 202 | PMT version 1 with `PCR_PID` 0x0101, applied from packet 202; the PCR moves to PID 0x0101 with the same clock | `pcr_pid_change` |

Expected analysis (both packet sizes; `tsparser` tests pin it and check that
the committed files equal the generator output): 127 PCRs, max PCR interval
40 ms over 121 intervals (`ok`), `pcr_discontinuities` `ng` with 2 undeclared
steps and the five kinds above, PCR jitter estimate in 6 segments split at
packets 80, 110, 140, 170 and 202 (not at the wraparound), bitrate in 6
segments, and `--fail-on-error` exits 2 naming only `PCR discontinuities`. The
`-clean` stream has one segment everywhere, a `wraparound` item and status
`ok`. Before the PCR semantics were aligned, the analyzer reported a 4040 ms
`NG` PCR interval for the forward jump, and split the jitter estimate and the
bitrate at the wraparound, also in the clean stream.

TSDuck 3.45 agrees for both files:

- `pcrextract --pcr`: the same 127 PCRs, with packet index × packet size equal
  to the analyzer's PCR offsets and identical values.
- `timewrap`: one PCR wrap-down at packet 50 on PID 0x0100.
- `pcrverify` (byte-rate timing for 188 bytes, `-i` M2TS arrival timestamps
  for 192 bytes): four PCRs with a jitter of about 5, 3, 2 and 4 seconds at
  packets 80, 110, 140 and 170, and 2 unchecked PCRs (the first on each PID).
  `pcrverify` does not use `discontinuity_indicator`, so it reports the
  declared steps as well; the analyzer separates them. With M2TS arrival
  timestamps the jumps measure exactly 5,000,000, 3,000,000, 2,000,000 and
  4,000,000 µs, the clock offsets the generator applied.
- The clean stream: `pcrverify` 126 PCRs OK, `timewrap` one wrap-down.
