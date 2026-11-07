# PAT/PMT health fixtures

Write short constant-rate streams for the PAT/PMT health check (`psi_health`)
and cross-check them with TSDuck. Every scenario except `healthy` is
**intentionally invalid**: use them to test the check, never as examples of a
compliant stream.

```sh
go run ./tools/generate_psi_health -output sample_data/sample_188byte_psi_health.ts
go run ./tools/generate_psi_health -m2ts -output sample_data/sample_192byte_psi_health.ts
go run ./tools/generate_psi_health -scenario pat-late -output /tmp/pat-late.ts
tsanalyze --table-analysis sample_data/sample_188byte_psi_health.ts
tsanalyze --table-analysis --format m2ts sample_data/sample_192byte_psi_health.ts
tstables --pid 0x1010 --log sample_data/sample_188byte_psi_health.ts
tsp -I file sample_data/sample_188byte_psi_health.ts -P tables --invalid-sections --all-sections -O drop
```

150 packets of one 20 ms slot each (75.2 kbit/s, 3 s). Program 1 (PMT PID
0x1000) has MPEG-2 video on PID 0x0100, which carries a PCR every 40 ms.
Program 2 (PMT PID 0x1010) uses the same video and PCR and adds MPEG-1 audio on
PID 0x0101, which is never sent; its PMT has two sections. The PAT is sent in
slots 1 and 2 and then in the slots 10k+9 (every 200 ms), program 1's PMT in
slots 3 and 4 and then in the slots 10k+3, and program 2's PMT sections 0 and
1 in the slots 10k+5 and 10k+7. PCR and PTS follow the slot clock
(10 s + slot × 20 ms), so byte position is an exact time proxy. The first PCR
is in slot 6 and the last in slot 148, so the time base covers 2840 ms.

| Scenario (`-scenario`) | Defect | Expected `psi_health` |
|---|---|---|
| `healthy` | none | `ok`; PAT 17 tables, PMT 1 16, PMT 2 15; every max interval 200 ms |
| `pat-late` | no PAT in slots 49 to 89 | PAT `ng` `late`: 12 tables, 1200 ms ending at slot 99 |
| `pat-stops` | no PAT after slot 2 | PAT `ng` `late`: 2 tables, open interval 2840 ms (slot 6 to 148), no complete interval |
| `pmt-missing` | program 2's PMT never sent | PMT 2 `ng` `missing`: 0 tables, followed for 2840 ms |
| `incomplete` | section 1 of program 2's PMT never sent | PMT 2 `ng` `missing`, `incomplete` (once, at the end of the range) |
| `crc` | invalid CRC_32 on the PAT in slot 59 and program 1's PMT in slot 63 | PAT and PMT 1 `ng` `crc_error` ×1, max interval 400 ms (not late) |
| `wrong-table-id` | a PMT section on PID 0 in slot 59 instead of the PAT | PAT `ng` `wrong_table_id` ×1, max interval 400 ms |
| `version-change` | PMT 1 version 1 from slot 73, PAT version 1 from slot 79, same content | `ok`; one version change each |
| `no-pcr` | the video packets carry no PCR | `insufficient_data`; time base `none`, repetition `skipped` |
| `discontinuity` | PCR and PTS 10 s later from slot 70, with `discontinuity_indicator` | `ok`; 2 PCR segments, no interval spans slot 70 |
| `unusable` | `transport_error_indicator` on the PAT in slot 59, program 1's PMT in slot 63 scrambled | PAT and PMT 1 `ng` `unusable_packets` ×1, max interval 400 ms |
| `defects` (default, committed) | `pat-late`, `wrong-table-id`, `crc` on PMT 1 only, PMT 1 version 1 from slot 73, and `incomplete` | PAT `ng` `late`, `wrong_table_id`; PMT 1 `ng` `crc_error` with one version change; PMT 2 `ng` `missing`, `incomplete` |

`tsparser` tests build the generator and pin the complete `psi_health` result
of every scenario for both packet sizes, check that the committed files equal
its output, and check the summary (`--program 1`) and `--output analysis`
documents of the committed files.

With `-m2ts` each packet gets a TP_extra_header whose arrival_time_stamp
advances by one slot (540,000 ticks of 27 MHz).

## TSDuck cross-check

TSDuck 3.45, every scenario, 188-byte (`tsanalyze --table-analysis`) and
192-byte (`--format m2ts`) forms alike:

- **Table counts** (`Table cnt`) equal `instance_count` for every PAT and every
  single-section PMT in every scenario, for example 17/16/15 (`healthy`),
  12 PATs (`pat-late`, `defects`), 16 PATs and 15 PMTs of program 1 (`crc`,
  `unusable`).
- **Maximum repetition** (`Max repet.`) equals `max_interval_ms` wherever an
  interval between two tables exists: 200 ms (`healthy`, `version-change`,
  `discontinuity`), 1200 ms for the PAT (`pat-late`, `defects`), 400 ms for the
  PAT and PMT 1 (`crc`, `wrong-table-id`, `unusable`). TSDuck times packets
  from the PCR-derived bitrate; on this constant-rate stream it gives the same
  values as the analyzer's PCR interpolation.
- **CRC errors**: `tsp -P tables --invalid-sections --all-sections` reports
  `Invalid section, invalid CRC32` twice in `crc` (PAT and PMT 1) and once in
  `defects` (PMT 1), as `crc_error_count` does.
- **Wrong table_id**: `tsanalyze` lists a `0x02 (PMT)` table on PID 0x0000 in
  `wrong-table-id` and `defects`; the analyzer counts it as
  `wrong_table_id_count` 1 and excludes it from the PAT count.
- **Incomplete PMT**: `tstables --pid 0x1010`, which prints complete tables
  only, prints none in `incomplete` and `defects`, as `instance_count` 0.
  `tsanalyze` still reports `Table cnt 15` there: it counts each section 0 it
  receives as a table.

Methodology differences:

- `pat-stops`: TSDuck's maximum repetition is 20 ms, between the only two PATs
  (slots 1 and 2). The analyzer also measures the open interval from the last
  PAT to the last PCR, 2840 ms, which proves that no PAT was sent for that
  long, and reports it as late.
- `pmt-missing`: TSDuck has no entry for a PID that carries no section; the
  analyzer reports program 2 as announced by the PAT and `missing`.
- `no-pcr`: TSDuck reports no repetition without a bitrate; the analyzer
  reports `skipped` repetition and `insufficient_data`.
