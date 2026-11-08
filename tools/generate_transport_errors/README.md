# Transport-error and scrambling fixture

Write a short constant-rate stream with packets marked by
`transport_error_indicator` (TEI) and scrambled packets on the PAT, PMT, PCR,
elementary-stream, unreferenced and null PIDs, then check them with TSDuck. The
stream is **intentionally invalid**: use it to test how the analyzer counts
these packets and keeps their data out of the analysis.

```sh
go run ./tools/generate_transport_errors -output sample_data/diagnostics/sample_188byte_transport_errors.ts
go run ./tools/generate_transport_errors -m2ts -output sample_data/diagnostics/sample_192byte_transport_errors.ts
go run ./tools/generate_transport_errors -malformed -output sample_data/diagnostics/sample_188byte_transport_errors_malformed.ts
go run ./tools/generate_transport_errors -malformed -m2ts -output sample_data/diagnostics/sample_192byte_transport_errors_malformed.ts
tsdump --headers-only sample_data/diagnostics/sample_188byte_transport_errors.ts
tsdump --format m2ts --headers-only sample_data/diagnostics/sample_192byte_transport_errors.ts
tsanalyze --json sample_data/diagnostics/sample_188byte_transport_errors.ts
tsp -I file sample_data/diagnostics/sample_188byte_transport_errors.ts -P filter --valid --negate -P count --summary -O drop
tsp -I file sample_data/diagnostics/sample_188byte_transport_errors.ts -P continuity -O drop
```

100 packets of one 20 ms slot each (75.2 kbit/s, 2 s): program 1 (PMT PID
0x1000) with MPEG-2 video on PID 0x0100, which carries a PCR in every packet,
and MPEG-1 audio on PID 0x0101. Packet 0 is a null packet, the PAT is in
packets 1 and 2 and the PMT in 3 and 4, so packets 0 to 4 are read while the
PAT and PMT are acquired and the analysis starts at packet 5. The PAT repeats
in packets 19, 39, 59, 79 and 99 and the PMT in 23, 43, 63 and 83. Video takes
the even packets from 6, audio the packets 4k+1 from 5, null packets the rest.
`-clean` writes the same stream without the affected packets.

Each affected packet is built so that using it would show: its continuity
counter follows its PID's sequence, a TEI packet carries a wrong PTS, PCR or
table, and a scrambled payload is XORed with 0x5A.

| Packet | PID | Condition | Content if it were used | Excluded |
| --- | --- | --- | --- | --- |
| 0 | 0x1FFF null | TEI | — (read during acquisition) | no |
| 7 | 0x0200 unreferenced | scrambled (2) | — | no |
| 25 | 0x0101 audio | TEI | PTS 1 s early | yes |
| 30 | 0x0100 video | TEI | PTS 1 s early, PCR 1 s late | yes |
| 39 | 0x0000 PAT | TEI | version 1 moving the PMT to 0x1001 (valid CRC) | yes |
| 43 | 0x1000 PMT | TEI | version 1 retyping 0x0100 to 0x1B (valid CRC) | yes |
| 45 | 0x0101 audio | scrambled (3) | scrambled PES | yes |
| 50 | 0x0100 video | scrambled (2) | scrambled PES; its clear PCR is used | yes |
| 59 | 0x0000 PAT | scrambled (2) | scrambled section | yes |
| 63 | 0x1000 PMT | scrambled (2) | scrambled section | yes |
| 70 | 0x0100 video | TEI and scrambled (2) | both | yes |

Expected analysis (both packet sizes; `tsparser` tests pin it and check that
the committed files equal the generator output): `transport_errors` and
`scrambled_packets` both `data_excluded` with 6 packets, 5 excluded; no PSI
transition, the four PAT/PMT warnings, no timestamp anomaly, no continuity
error, 45 PCRs (packets 30 and 70 left out), max PCR interval 80 ms, and every
PES except those of packets 25, 30, 45, 50 and 70. The analyzer before this
check reported a 1040 ms PCR interval, two PCR discontinuities and four PTS
anomalies on the same file.

TSDuck 3.45 agrees for both files:

- `tsdump --headers-only` (with `--format m2ts` for the 192-byte file): the
  same packets with `Error: 1` and `Scrambling: 2/3` on the same PIDs; each
  PID's count and first packet times the packet size equal the analyzer's
  `packet_count` and `first_offset_bytes` exactly.
- `tsp -P filter --valid --negate -P count`: TEI packets PID 0x0000 1,
  0x0100 2, 0x0101 1, 0x1000 1, 0x1FFF 1.
- `tsanalyze`: 100 packets, 6 with transport errors. It leaves TEI packets out
  of its per-PID statistics, so it counts one scrambled packet on PID 0x0100
  (packet 50) where the analyzer counts two (packets 50 and 70, the second
  also with TEI), 45 PCRs on PID 0x0100 as the analyzer uses, and it also
  ignores the PAT in packet 1 as "suspect" because it is the first packet of a
  new PID right after an error packet.
- `continuity`: no discontinuity; `pcrextract` lists 47 PCRs because it does
  not skip TEI packets.

## Malformed TEI packets (`-malformed`)

`-malformed` writes the same stream, but also damages each TEI packet where a
parser reading past its fixed 4-byte header would fail, as uncorrectable bit
errors do in real captures. PIDs, continuity counters and payload presence stay
as above:

| Packet | PID | Damage |
| --- | --- | --- |
| 0 | 0x1FFF null | `adaptation_field_control` 0 (reserved) |
| 25 | 0x0101 audio | `adaptation_field_length` 200, past the packet |
| 30 | 0x0100 video | `transport_private_data_flag` with `transport_private_data_length` 200 after the (1 s late) PCR |
| 39 | 0x0000 PAT | `adaptation_field_control` 0, with the PAT's previous `continuity_counter` (control 0 does not increment it) |
| 43 | 0x1000 PMT | `adaptation_field_control` 3 with `adaptation_field_length` 255 |
| 70 | 0x0100 video, also scrambled | `OPCR_flag` and `transport_private_data_flag`, `transport_private_data_length` 200 |

With `transport_error_indicator` cleared, each of these packets is a parse
error, and the analyzer before
[#183](https://github.com/small-teton/mpeg-ts-analyzer/issues/183) stopped at
packet 25 (`failed to parse TS packet in BufferPes: invalid
adaptation_field_length: 200`). Since only the fixed header of a TEI packet is
read, the expected analysis, summary and text output are exactly those of the
default stream; `tsparser` tests pin this for both packet sizes, and also that
the packet list shows every packet as valid.

TSDuck 3.45 agrees for both files (`--format m2ts` for the 192-byte one):

- `tsdump --headers-only`: 100 packets, `Error: 1` on the same six packets and
  PIDs, and the same scrambled packets; each PID's count and first packet times
  the packet size equal the analyzer's `packet_count` and `first_offset_bytes`.
- `tsp -P filter --valid --negate -P count`: 6 packets.
- `tsanalyze`: 100 packets, 6 with transport errors.
- `tsp -P continuity`: no discontinuity, as the analyzer reports.
