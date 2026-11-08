# Sample data

[Documentation home](../index.md)

This page is the one description of every committed sample and of the
generator that made it: what each stream contains, where its defects are, the
results the analyzer must give, and how they were cross-checked with
reference tools. The generators themselves are listed in
[`tools/`](https://github.com/small-teton/mpeg-ts-analyzer/blob/master/tools/README.md).

## Layout

- `sample_data/` holds the ordinary, healthy streams: the single-program
  video samples below, the two-program samples
  (`sample_<size>byte_two_programs_mpeg2_160x120_25fps_mp2.ts`, two programs
  of MPEG-2 video and MPEG-1 audio made with FFmpeg) and their
  [multi-section PSI rewrite](#multi-section-psi-sample)
  (`…_two_programs_multi_section_psi_…`, made by
  `tools/generate_multi_section_psi`).
- `sample_data/diagnostics/` holds the streams built to contain faults or edge
  cases, described in the sections below, and the
  [PAT/PMT version-change rewrite](#patpmt-version-change-sample) of the video
  sample (`sample_<size>byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts`,
  made by `tools/generate_psi_version_change`).

Each directory has a README with a one-line summary of every file:
[`sample_data/`](https://github.com/small-teton/mpeg-ts-analyzer/blob/master/sample_data/README.md),
[`sample_data/diagnostics/`](https://github.com/small-teton/mpeg-ts-analyzer/blob/master/sample_data/diagnostics/README.md).

Every generated file is deterministic: `tsparser` tests check that each
committed file equals the output of its generator, and pin the results given
below. The TSDuck, ffprobe and Python cross-checks were run when each sample
was added; rerun them with the commands shown when a generator or the
analysis it exercises changes.

## Healthy samples

Sample TS files are included in `sample_data/` for quick testing:

```bash
# 188-byte TS
ffmpeg -f lavfi -i "color=c=blue:s=320x240:d=5,format=yuv420p" \
       -f lavfi -i "anullsrc=r=48000:cl=stereo" \
       -t 5 -c:v mpeg2video -c:a mp2 -metadata:s:a:0 language=eng \
       -f mpegts sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts

# 192-byte M2TS
ffmpeg -f lavfi -i "color=c=red:s=320x240:d=2,format=yuv420p" \
       -f lavfi -i "anullsrc=r=48000:cl=stereo" \
       -t 2 -c:v mpeg2video -c:a mp2 -metadata:s:a:0 language=eng \
       -f mpegts -mpegts_m2ts_mode 1 sample_data/sample_192byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
```

| Sample | Packet sizes | Contents | Use |
| --- | --- | --- | --- |
| `sample_<size>byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts` | 188 (311 KB, 5 s), 192 (129 KB, 2 s) | One program: MPEG-2 video 320×240 at 25 fps and MP2 audio 48 kHz stereo (188-byte: PMT 0x1000, video 0x0100, audio 0x0101; 192-byte, FFmpeg's M2TS mode: PMT 0x0100, video 0x1011, audio 0x1100 as stream_type 0x06) | First look at the analyzer: no check reports a problem. Used by the README demo and `make gui-run-sample` |
| `sample_<size>byte_two_programs_mpeg2_160x120_25fps_mp2.ts` | 188 (203 KB), 192 (207 KB) | Two programs ("Main", PMT 0x1000, PIDs 0x0100/0x0101; "Second", PMT 0x1001, PIDs 0x0102/0x0103), each MPEG-2 video 160×120 at 25 fps with MPEG-1 audio, about 1.7 s; made with FFmpeg (the exact command was not recorded) | Multi-program listing and `--program`; GUI fixtures and screenshots |

## Multi-section PSI sample

`sample_data/sample_188byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts`
and its 192-byte M2TS twin rewrite the two-program samples so that the PAT has
one section per program and each PMT one section per elementary stream, sent
from the last section to section 0 ([PSI tables](../cli/psi.md)).

```bash
go run ./tools/generate_multi_section_psi \
  -input sample_data/sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts \
  -output sample_data/sample_188byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts
go run ./tools/generate_multi_section_psi \
  -input sample_data/sample_192byte_two_programs_mpeg2_160x120_25fps_mp2.ts \
  -output sample_data/sample_192byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts
tsp -I file sample_data/sample_188byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts \
  -P tables --pid 0 --pid 0x1000 --pid 0x1001 --max-tables 3 -O drop
tsanalyze --json sample_data/sample_188byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts
tsp -I file --format m2ts sample_data/sample_192byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts \
  -P analyze --json -o analyze.json -O drop
```

Only PSI payloads change, so packet counts, PCRs and timestamps match the
input. TSDuck 3.45 reports a PAT of 2 sections and two PMTs of 2 sections
each, 2 services and 8 PIDs, with the same per-PID packet counts as the
single-section samples.

## 204-byte packets

Five samples have a 204-byte twin, `sample_data/diagnostics/sample_204byte_*.ts`: the
[all-findings sample](#diagnostic-sample-with-every-overview-finding), the
[sync-loss sample](#diagnostic-sample-with-ts-synchronization-loss), the
[packet-gap sample](#packet-gap-sample), the [mux lead sample](#mux-lead-sample)
and the [PCR-to-PTS/DTS gap wraparound sample](#pcr-to-ptsdts-gap-wraparound-sample). Their generators' `-trailer` flag
follows every 188-byte TS packet with a 16-byte trailer, the layout of 204-byte
broadcast streams whose trailer is commonly `RS(204,188)` parity. The trailer
is deterministic filler (bytes 0x80–0xBF, never a sync byte), not real parity;
the sync-loss twin adds false sync bytes to four trailers. The analyzer gives
the same results as for the 188-byte files, with offsets 204 bytes per packet,
and tests also run every other aligned 188-byte sample with trailers added and
compare the analysis and summary documents. The public TSDuck corpus file
[`test-170.ts`](https://github.com/tsduck/tsduck-test/blob/f42b2f2b14c27a065eece1fd329d171a9b79c86f/input/test-170.ts)
(a 204-byte ISDB-Tb capture, not committed here) was used to cross-check the
analyzer with TSDuck.

## Diagnostic sample with every Overview finding

`sample_data/diagnostics/sample_188byte_all_findings.ts` and its 192-byte M2TS twin
`sample_data/diagnostics/sample_192byte_all_findings.ts` (23 KiB each) are **intentionally
invalid**. They are not examples of a good stream: each carries deliberate
defects so that every Overview transport-health check reports a problem in one
analysis — a 160 ms PCR interval, a 1200 ms PCR → PTS/DTS gap, a backward PTS,
one continuity-counter gap, a PCR that is 100 µs late, a null packet with
`transport_error_indicator` set, a scrambled null packet, an 800 ms gap
between two PATs and between two PMTs, a video PES header with a bad PTS
marker bit and 800 ms of audio PES packets without a PTS. The stream is
delivered at a constant 75.2 kbit/s for 2.4 s, long enough for two 1-second
windows, so the PCR jitter analysis finds constant delivery and judges the
late PCR (`NG`). Its packets are aligned and its PCR clock is continuous, so
TS synchronization and PCR discontinuities stay `OK`; the
[sync-loss sample](#diagnostic-sample-with-ts-synchronization-loss) and the
[PCR discontinuity sample](#pcr-discontinuity-sample) cover them. Use the normal sample above for a first look at the
analyzer.

```bash
go run ./tools/generate_all_findings -output sample_data/diagnostics/sample_188byte_all_findings.ts
go run ./tools/generate_all_findings -m2ts -output sample_data/diagnostics/sample_192byte_all_findings.ts
go run ./tools/generate_all_findings -trailer -output sample_data/diagnostics/sample_204byte_all_findings.ts
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_all_findings.ts --dump-pcr-jitter
make gui-run GUI_ARGS='sample_data/diagnostics/sample_188byte_all_findings.ts'
```

Tests check that the committed files equal the generator output and pin the
verdicts for every packet size; the GUI's Overview tests use the CLI's analysis
of the 188-byte file (`gui/tests/fixtures/analysis_all_findings_188.json`),
which `make gui-test` compares with the output of the CLI built from the tree.

### All-findings layout and expected results

120 packets of one 20 ms slot each (75.2 kbit/s, 2.4 s): program 1 (PMT PID
0x1000) with MPEG-2 video on PID 0x0100, which carries the PCR, and MPEG-1
audio on PID 0x0101. PAT and PMT are sent twice at the start (the analyzer
treats a table as complete at its next copy, so the analysis starts at packet
4) and then every 400 ms (PAT in packets 19, 39, ..., PMT in packets 23, 43,
...). From packet 4, video takes the even packets (every 40 ms), audio the
packets 4k+1 (every 80 ms), and the other packets 4k+3 are null packets. Each
PES packet fits in one TS packet with filler payload. PCR and PTS follow the
slot clock (10 s + slot × 20 ms), so the byte position is an exact time proxy.
The defects, by packet index:

| Check | Defect | Expected analysis |
|---|---|---|
| Max PCR interval | packets 22, 24 and 26 carry no PCR | `ng`, 160 ms (limit 100 ms), 54 intervals |
| PCR → PTS/DTS gap | every audio PTS is 1200 ms ahead of its PCR time | `ng`, 1200 ms (limit 1000 ms), 72 timestamps (the malformed header and the nine PES packets without PTS give none) |
| PTS/DTS anomalies | video PTS in packet 46 is 40 ms before the previous one | `anomalies`: PID 0x0100 `backward` ×1 |
| Continuity counter | audio packet 25 skips counter 5 (carries 6), then continues | `errors`: 1 on PID 0x0101 (expected 5, actual 6) |
| PCR jitter | the PCR in packet 58 is 100 µs late | delivery `constant` (2 windows at 75,200 b/s, deviation 0.00 %), `ng`: max +100 µs at packet 58, −50 µs at packets 56 and 60 |
| Transport errors | null packet 7 has `transport_error_indicator` set | `affected`: 1 packet on PID 0x1fff (null), 0 excluded |
| Scrambled packets | null packet 11 has `transport_scrambling_control` 2 | `affected`: 1 packet on PID 0x1fff (null), 0 excluded |
| PAT health, PMT health | packets 59 and 63 are null packets instead of the PAT and the PMT | `ng`: PAT `late`, 800 ms between the PATs in packets 39 and 79; PMT `late`, 800 ms between the PMTs in packets 43 and 83 (limit 500 ms) |
| PES headers | the last PTS `marker_bit` of the video PES in packet 106 is 0 | `errors`: 1 `bad_marker_bits` on PID 0x0100, 87 headers checked |
| PTS repetition | the audio PES packets 65 to 97 carry no PTS | `ng`: 800 ms from packet 61 to 101 on PID 0x0101 (limit 700 ms) |

The transport-error and scrambling defects are on the null PID, whose payload
the analysis never reads, and the missing PAT and PMT copies leave the layout
unchanged, so none of them changes another result.

`-omit` leaves defects out (`pcr-interval`, `pcr-pts-gap`, `pts-backward`,
`cc-gap`, `pcr-jitter`, `tei`, `scrambled`, `psi-late`, `pes-header`,
`pts-repetition`, or `all`); `tsparser` tests use it to show that each defect
turns only its own check non-OK, and that the stream without defects is OK
everywhere, with constant delivery and zero estimated jitter.

The 192-byte form adds a TP_extra_header per packet whose arrival_time_stamp
advances by one slot (540,000 ticks of 27 MHz), the intended constant delivery.
The 204-byte form (`-trailer`) gives the analysis of the 188-byte form with
every offset at 204·slot, and the same bitrates (188 bytes per packet).

### All-findings cross-check

```bash
tsanalyze --json sample_data/diagnostics/sample_188byte_all_findings.ts
tsp -I file sample_data/diagnostics/sample_188byte_all_findings.ts -P continuity -O drop
tsp -I file sample_data/diagnostics/sample_188byte_all_findings.ts -P pcrextract --pcr --pts --csv -O drop
tsp -I file sample_data/diagnostics/sample_188byte_all_findings.ts -P pcrverify -b 75200 -j 1 -O drop
tsp -I file --format m2ts sample_data/diagnostics/sample_192byte_all_findings.ts -P pcrverify -i -j 1 -O drop
```

TSDuck 3.45 agrees for the 188- and 192-byte files:

- `tsanalyze`: 120 packets, 75,200 b/s from PCRs; PID 0x0100 58 packets with
  55 PCRs and 57 PTS (it also rejects the PTS with a bad marker bit in
  packet 106); PID 0x0101 29 packets with 20 PTS (none in packets 65 to 97),
  1 discontinuity; PAT 7, PMT 6,
  null 19 (tsanalyze leaves the packet with a transport error out of the PID
  statistics), 1 of them scrambled; 1 packet with a transport error.
  `tsanalyze --table-analysis` gives 7 PATs and 6 PMTs, each with a maximum
  repetition of 800 ms, as `psi_health` does.
  `tsdump --headers-only` shows `Error: 1` on packet 7 and `Scrambling: 2` on
  packet 11, both PID 0x1FFF.
- `continuity`: `packet index: 25, PID: 0x0101 (257), missing 1 packets`.
- `pcrextract`: 55 PCRs, largest difference 160 ms (packets 20 → 28); the
  video PTS of packet 46 (984,600) is below that of packet 44 (988,200);
  audio PTS minus the slot clock is 1200 ms. Grouping its PCRs into 1-second
  windows gives 75,200 b/s in both windows, as the analyzer's delivery
  assessment does. Its audio PTS go from packet 61 to 101 (800 ms of PCR
  time), as the analyzer's PTS repetition reports.
- `pcrverify` at 75,200 b/s (and with the M2TS arrival timestamps, `-i`):
  `52 PCR OK, 2 with jitter > 27 (1 micro-seconds)`, ±2,700 PCR units
  (100 µs). TSDuck compares consecutive PCRs, so the late PCR shows on the
  steps into and out of packet 58; this analyzer compares each PCR with its two
  neighbors interpolated by byte position, so it reports +100 µs at packet 58
  and −50 µs at its neighbors. Both find the same 100 µs error. Without
  defects (`-omit all`), `pcrverify` reports `57 PCR OK, 0 with jitter > 27`
  in both forms and the analyzer `constant` / `ok` with zero jitter.

TSDuck 3.45 also gives the same results for the 204-byte file (`tsanalyze
--json`, `-P continuity`, `-P pcrverify -b 75200 -j 1`): the same per-PID
counts, 75,200 b/s as `bitrate` and 81,600 b/s as `bitrate-204`, the same
continuity error at packet index 25 and the same `52 PCR OK, 2 with jitter >
27`; `-P pcrextract` writes the same CSV as for the 188-byte file.

## Diagnostic sample with TS synchronization loss

`sample_data/diagnostics/sample_188byte_sync_loss.ts` and its 192-byte M2TS twin
`sample_data/diagnostics/sample_192byte_sync_loss.ts` (39 KiB each) are also
**intentionally invalid**. A clean 2-second stream is damaged in four places:
a corrupted sync byte, one inserted byte, one deleted byte and a 1000-byte
block of garbage with false sync bytes; it ends with a 100-byte partial packet.
The analyzer reports four losses, each recovered, with their byte offsets and
the skipped bytes, and continues the analysis after each
([TS synchronization](../cli/usage.md#ts-synchronization-always-on)).

```bash
go run ./tools/generate_sync_loss -output sample_data/diagnostics/sample_188byte_sync_loss.ts
go run ./tools/generate_sync_loss -m2ts -output sample_data/diagnostics/sample_192byte_sync_loss.ts
go run ./tools/generate_sync_loss -trailer -output sample_data/diagnostics/sample_204byte_sync_loss.ts
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_sync_loss.ts
make gui-run GUI_ARGS='sample_data/diagnostics/sample_188byte_sync_loss.ts'
```

Tests check that the committed files equal the generator output and pin the
results for every packet size and for each kind of damage alone; the GUI's
tests use the CLI's analysis of the 188-byte file
(`gui/tests/fixtures/analysis_sync_loss_188.json`), which `make gui-test`
compares with the output of the CLI built from the tree.

### Sync-loss layout and expected results

The undamaged stream has 200 packets of one 10 ms slot each (150.4 kbit/s,
2 s): program 1 (PMT PID 0x1000) with MPEG-2 video on PID 0x0100, which
carries the PCR. PAT and PMT are sent twice at the start (so the analysis
starts at slot 4) and then every 500 ms (PAT in slots 25, 75, ..., PMT in
slots 27, 77, ...). From slot 4 the other even slots carry one video PES packet
each with a PCR and a PTS, and the other odd slots are null packets. The
damage, by slot of the undamaged stream, with the result for 188-byte packets
(`ps` is the packet size, 188, 192 or 204; offsets are in the damaged file):

| Damage | What is written | Loss at | Skipped |
|---|---|---|---|
| `sync-byte` | the sync byte of video slot 40 is 0x00 | 40·ps | ps (slot 40 is lost) |
| `insert-byte` | one 0xFF byte after byte 100 of null slot 61 | 62·ps | 1 |
| `delete-byte` | byte 100 of null slot 81 removed | 82·ps + 1 | ps − 1 (slot 82 is lost) |
| `garbage` | 1000 bytes before slot 121, zero except four false sync bytes one packet apart | 121·ps | 1000 |
| `truncated-tail` | the first 100 bytes of a null packet after slot 199 | — | trailing partial packet at 200·ps + 1000 |

The analyzer reports `sync.status` `recovered`: 4 losses, 4 resyncs, 2·ps +
1000 skipped bytes (1376, 1384 or 1408), the largest 1000 at 121·ps, and a 100-byte
trailing partial packet. The video packets lost in slots 40 and 82 give one
continuity error each, at slots 42 and 84 (42·ps and 84·ps: the inserted and
deleted bytes cancel out). The four false sync bytes in the garbage block are
one fewer than the five consecutive sync bytes a new alignment requires, so
they are skipped. `-omit` leaves damage out (`sync-byte`, `insert-byte`,
`delete-byte`, `garbage`, `truncated-tail` or `all`). `-unrecoverable` also sets
the sync byte of slot 197 to 0x00: fewer than five aligned packets follow, so
the analysis stops there with `sync.status` `lost`, top-level `status`
`incomplete` and `reason` `sync_lost`.

With `-m2ts`, each packet gets a 4-byte TP_extra_header whose
arrival_time_stamp advances by one slot; the damage is at the same positions
relative to the sync byte.

With `-trailer`, each packet is followed by a 16-byte trailer, making 204-byte
packets. The trailer is deterministic filler in 0x80–0xBF (not Reed-Solomon
parity), except that byte 5 of the trailers of slots 82 to 85 is a false sync
byte: four `0x47` bytes 204 bytes apart, one fewer than a new alignment needs.
The search after the `delete-byte` loss starts in slot 82 and passes over them,
so the 204-byte file gives the same results with `ps` = 204: losses at
0x1fe0, 0x3168, 0x4159 and 0x606c, 204, 1, 203 and 1000 skipped bytes, and
continuity errors at 42·204 and 84·204.

### Sync-loss cross-check

```bash
tsp -I file sample_data/diagnostics/sample_188byte_sync_loss.ts -P count -O drop
tsresync -c -m 960 -s 1024 -v -d3 -k -o resynced.ts sample_data/diagnostics/sample_188byte_sync_loss.ts
tsp -I file resynced.ts -P continuity -P count -O drop
```

TSDuck 3.45 agrees for all three files:

- `tsp` (which does not resynchronize) stops with `synchronization lost after
  40 packets, got 0x00 instead of 0x47`, the analyzer's first loss at 40·ps.
- For the 204-byte file, `tsresync -c -m 1020 -s 1024` (five 204-byte packets)
  detects `Packet size is 204 bytes` and reports the same four losses after 40,
  61, 81 and 119 packets, finding synchronization again after 204, 1, 203 and
  1000 bytes, and writes 198 188-byte packets; on its output `tsp -P
  continuity -P count` gives the same continuity errors and per-PID counts as
  below.
- `tsresync -c -m 960` (`--continue` after each loss, at least 960 bytes of
  contiguous packets, which is five 192-byte packets) reports losses after 40,
  61, 81 and 119 output packets, finding synchronization again after 188 (192),
  1, 187 (191) and 1000 bytes, and writes 198 packets: the analyzer's four
  losses and skipped spans, and its 198 analyzed packets. TSDuck counts the
  packets it has written, so the second and third losses are "after 61" and
  "after 81" although they are at slot boundaries 62 and 82 of the undamaged
  stream, and the fourth is "after 119" because two packets were dropped.
- On the resynchronized output, `tsp -P continuity` reports `PID 0x0100,
  missing 1 packets` at packet indexes 41 and 82, slots 42 and 84 (two packets
  were dropped before index 82), and `-P count` gives PAT 6, PMT 6, PID 0x0100
  96 and null 90 packets, as `psi_layout.pids` of the analyzer.
- With `-unrecoverable`, `tsresync -c -p 188` reports the fifth loss after 195
  packets and `Cannot find MPEG TS packets` for the rest; the analyzer stops at
  the same boundary, 197·ps + 1000.

Methodology differences:

- `tsresync` accepts a new alignment after a minimum number of contiguous
  bytes (`-m`); with `-m 940`, which is five 188-byte packets but fewer than
  five 192-byte ones, it accepts the four false sync bytes of the M2TS garbage
  block as packets and loses synchronization again after them. The analyzer
  always requires five sync bytes one packet apart, whatever the packet size.
- ETSI TR 101 290 distinguishes a single corrupted sync byte (Sync_byte_error)
  from a loss of synchronization (two or more consecutive corrupted sync bytes,
  TS_sync_loss). The analyzer reports every boundary without a sync byte as a
  loss and does not trust the packet: a corrupted sync byte with an unchanged
  alignment is one loss that skips one packet, like `sync-byte` above.

## PCR discontinuity sample

`sample_data/diagnostics/sample_188byte_pcr_discontinuities.ts` and its 192-byte M2TS twin
`sample_data/diagnostics/sample_192byte_pcr_discontinuities.ts` (48 KiB each) are
**intentionally invalid** (two steps have no `discontinuity_indicator`, which
ISO/IEC 13818-1, 2.4.3.5 requires). Their PCR, one every 40 ms of a constant
75.2 kbit/s multiplex, passes through every kind of PCR step the analyzer
distinguishes: a 33-bit wraparound, two declared discontinuities (one declared
on an earlier PCR-less packet), an undeclared backward step, an undeclared
forward jump of 4 s and a `PCR_PID` change. Max PCR interval stays `OK`
(40 ms) while PCR discontinuities is `NG`; see
[PCR steps](../cli/usage.md#pcr-steps).

```bash
go run ./tools/generate_pcr_discontinuities -output sample_data/diagnostics/sample_188byte_pcr_discontinuities.ts
go run ./tools/generate_pcr_discontinuities -m2ts -output sample_data/diagnostics/sample_192byte_pcr_discontinuities.ts
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_pcr_discontinuities.ts --dump-pcr-jitter --dump-bitrate
```

The GUI's Overview tests use the CLI's analysis of the 188-byte file
(`gui/tests/fixtures/analysis_pcr_discontinuities_188.json`), which
`make gui-test` compares with the output of the CLI built from the tree.

### PCR discontinuity layout and expected results

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

Expected analysis (both packet sizes): 127 PCRs, max PCR interval 40 ms over
121 intervals (`ok`), `pcr_discontinuities` `ng` with 2 undeclared steps and
the five kinds above, PCR jitter estimate in 6 segments split at packets 80,
110, 140, 170 and 202 (not at the wraparound), bitrate in 6 segments, and
`--fail-on-error` exits 2 naming only `PCR discontinuities`. The `-clean`
stream has one segment everywhere, a `wraparound` item and status `ok`. Before
the PCR semantics were aligned, the analyzer reported a 4040 ms `NG` PCR
interval for the forward jump, and split the jitter estimate and the bitrate
at the wraparound, also in the clean stream.

### PCR discontinuity cross-check

```bash
tsp -I file sample_data/diagnostics/sample_188byte_pcr_discontinuities.ts -P pcrextract --pcr --csv -O drop
tsp -I file sample_data/diagnostics/sample_188byte_pcr_discontinuities.ts -P pcrverify -P timewrap -O drop
tsp -I file --format m2ts sample_data/diagnostics/sample_192byte_pcr_discontinuities.ts -P pcrverify -i -P timewrap -O drop
```

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

## Transport-error and scrambling sample

`sample_data/diagnostics/sample_188byte_transport_errors.ts` and its 192-byte M2TS twin
`sample_data/diagnostics/sample_192byte_transport_errors.ts` (19 KiB each) are also
**intentionally invalid**. They carry packets with `transport_error_indicator`
(TEI) set and scrambled packets on the PAT, PMT, PCR, elementary-stream,
unreferenced and null PIDs, each built so that using its data would show as a
wrong PTS, PCR or table, to test how the analyzer counts them and keeps their
data out of the analysis.

```bash
go run ./tools/generate_transport_errors -output sample_data/diagnostics/sample_188byte_transport_errors.ts
go run ./tools/generate_transport_errors -m2ts -output sample_data/diagnostics/sample_192byte_transport_errors.ts
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_transport_errors.ts
```

`sample_data/diagnostics/sample_188byte_transport_errors_malformed.ts` and
`sample_data/diagnostics/sample_192byte_transport_errors_malformed.ts` are the same streams
with each transport-error packet also malformed after its 4-byte header (a
reserved `adaptation_field_control`, or adaptation-field lengths that run past
the packet). Since only that header of such a packet is read, they give the
same results; see [Malformed TEI packets](#malformed-tei-packets).

```bash
go run ./tools/generate_transport_errors -malformed -output sample_data/diagnostics/sample_188byte_transport_errors_malformed.ts
go run ./tools/generate_transport_errors -malformed -m2ts -output sample_data/diagnostics/sample_192byte_transport_errors_malformed.ts
```

### Transport-error layout and expected results

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

Expected analysis (both packet sizes): `transport_errors` and
`scrambled_packets` both `data_excluded` with 6 packets, 5 excluded; no PSI
transition, the four PAT/PMT warnings, no timestamp anomaly, no continuity
error, 45 PCRs (packets 30 and 70 left out), max PCR interval 80 ms, and every
PES except those of packets 25, 30, 45, 50 and 70. The analyzer before this
check reported a 1040 ms PCR interval, two PCR discontinuities and four PTS
anomalies on the same file.

### Transport-error cross-check

```bash
tsdump --headers-only sample_data/diagnostics/sample_188byte_transport_errors.ts
tsdump --format m2ts --headers-only sample_data/diagnostics/sample_192byte_transport_errors.ts
tsanalyze --json sample_data/diagnostics/sample_188byte_transport_errors.ts
tsp -I file sample_data/diagnostics/sample_188byte_transport_errors.ts -P filter --valid --negate -P count --summary -O drop
tsp -I file sample_data/diagnostics/sample_188byte_transport_errors.ts -P continuity -O drop
```

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

### Malformed TEI packets

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

## PAT/PMT health sample

`sample_data/diagnostics/sample_188byte_psi_health.ts` and its 192-byte M2TS twin
`sample_data/diagnostics/sample_192byte_psi_health.ts` (28 KiB each) are also
**intentionally invalid**. Two programs share one video PID; the stream has
no PAT for 1.2 s, a PMT section on the PAT PID, a PMT with an invalid CRC_32
and a version change, and a two-section PMT whose second section is never
sent, so every table of the PAT/PMT health check is `NG` for a different
reason.

```bash
go run ./tools/generate_psi_health -output sample_data/diagnostics/sample_188byte_psi_health.ts
go run ./tools/generate_psi_health -m2ts -output sample_data/diagnostics/sample_192byte_psi_health.ts
go run ./tools/generate_psi_health -scenario pat-late -output /tmp/pat-late.ts
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_psi_health.ts --program 1
```

The generator also writes one focused scenario at a time (`-scenario`, below),
which the tests use; every scenario except `healthy` is intentionally invalid.
`tsparser` tests build the generator and pin the complete `psi_health` result
of every scenario for both packet sizes, check that the committed files equal
its output, and check the summary (`--program 1`) and `--output analysis`
documents of the committed files.

### PAT/PMT health layout and scenarios

150 packets of one 20 ms slot each (75.2 kbit/s, 3 s). Program 1 (PMT PID
0x1000) has MPEG-2 video on PID 0x0100, which carries a PCR every 40 ms.
Program 2 (PMT PID 0x1010) uses the same video and PCR and adds MPEG-1 audio on
PID 0x0101, which is never sent; its PMT has two sections. The PAT is sent in
slots 1 and 2 and then in the slots 10k+9 (every 200 ms), program 1's PMT in
slots 3 and 4 and then in the slots 10k+3, and program 2's PMT sections 0 and
1 in the slots 10k+5 and 10k+7. PCR and PTS follow the slot clock
(10 s + slot × 20 ms), so byte position is an exact time proxy. The first PCR
is in slot 6 and the last in slot 148, so the time base covers 2840 ms. With
`-m2ts` each packet gets a TP_extra_header whose arrival_time_stamp advances
by one slot (540,000 ticks of 27 MHz).

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

### PAT/PMT health cross-check

```bash
tsanalyze --table-analysis sample_data/diagnostics/sample_188byte_psi_health.ts
tsanalyze --table-analysis --format m2ts sample_data/diagnostics/sample_192byte_psi_health.ts
tstables --pid 0x1010 --log sample_data/diagnostics/sample_188byte_psi_health.ts
tsp -I file sample_data/diagnostics/sample_188byte_psi_health.ts -P tables --invalid-sections --all-sections -O drop
```

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

## PAT/PMT version-change sample

`sample_data/diagnostics/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts`
and its 192-byte twin rewrite the 188-byte single-program sample so that its
PMT and then its PAT change version mid-stream (PIDs move, a stream type
changes); see [PSI tables](../cli/psi.md).

```bash
go run ./tools/generate_psi_version_change \
  -input sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts \
  -output sample_data/diagnostics/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts
go run ./tools/generate_psi_version_change -m2ts \
  -input sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts \
  -output sample_data/diagnostics/sample_192byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts
tsp -I file sample_data/diagnostics/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts \
  -P tables --pid 0 --pid 0x1000 --pid 0x1100 --diversified-payload --packet-index -O drop
tsp -I file --format m2ts sample_data/diagnostics/sample_192byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts \
  -P tables --pid 0 --pid 0x1000 --pid 0x1100 --diversified-payload --packet-index -O drop
tsanalyze --json sample_data/diagnostics/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts
```

- PMT version 1, from packet 545 on PID 0x1000: `PCR_PID` 0x0200, video moved
  from PID 0x0100 to 0x0200, audio PID 0x0101 `stream_type` 0x03 -> 0x04.
  Video packets move to 0x0200 from the first video `payload_unit_start_indicator`
  after that packet.
- PAT version 1, from packet 1131: program 1's PMT moves to PID 0x1100, which
  carries the same PMT version 1 from packet 1132.

The 192-byte fixture is the 188-byte one with a 4-byte TP_extra_header per
packet (constant arrival_time_stamp step), because the 192-byte sample uses
other PIDs. Packet count, continuity counters, PCRs and timestamps stay as in
the input. TSDuck 3.45 reports, for both files, PAT version 0 in packet 1, PMT
version 0 in packet 2, PMT version 1 in packet 545, PAT version 1 in packet
1131 and PMT version 1 on PID 0x1100 in packet 1132, and per-PID packet counts
PAT 43, 0x0100 70, 0x0101 1359, 0x0200 132, 0x1000 29, 0x1100 14 (service 1:
PCR PID 0x0200, PMT PID 0x1100 at the end).

## PID-reference sample

`sample_data/diagnostics/sample_188byte_pid_references.ts` and its 192-byte M2TS twin
`sample_data/diagnostics/sample_192byte_pid_references.ts` (120 KiB each, 16 s) are also
**intentionally invalid**. Their PSI layout references an audio PID that is
silent for 6.1 s, an audio PID that never carries a packet, a private-data PID
that never carries a packet, and streams that a PMT change adds and removes;
other packets are on PIDs that nothing references. The capture starts in the
middle of the stream, before the first PAT, to test that acquisition and the
PMT change cause no false finding.

```bash
go run ./tools/generate_pid_references -output sample_data/diagnostics/sample_188byte_pid_references.ts
go run ./tools/generate_pid_references -m2ts -output sample_data/diagnostics/sample_192byte_pid_references.ts
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_pid_references.ts
```

### PID-reference layout and expected results

160 frames of 100 ms, each of four 25 ms packet slots (640 packets,
60.16 kbit/s); packet `n` is slot `n % 4` of frame `n / 4`. Slot 0 is MPEG-2
video on PID 0x0100 with a PCR (PCR of frame `f` = 10 s + `f` × 100 ms), slot
1 MPEG-1 audio on 0x0101, slot 2 the PAT (frames `f % 4 == 0`), the PMT on
0x1000 (`f % 4 == 2`) or another PID, and slot 3 the streams that the PMT
change adds and removes. The file starts with video and audio before the first
PAT (packet 2), like a capture started in the middle of a stream: the analysis
starts at packet 43, after the PAT and PMT were acquired.

| PID | Layout | Packets | Expected |
| --- | --- | --- | --- |
| 0x1000 | PMT of program 1 | every 400 ms | `ok` |
| 0x0100 | PCR_PID and MPEG-2 video (0x02) | every frame | `ok` for both roles |
| 0x0101 | MPEG-1 audio (0x03) | every frame except frames 60–119 | `silent`: 6100 ms from packet 237 to packet 481 |
| 0x0102 | private PES data (0x06) | none | `insufficient_data` (`intermittent_stream_type`): not judged |
| 0x0103 | MPEG-2 audio (0x04) in PMT version 0 | none | `absent`: 13175 ms from the analysis start (packet 43) to the PMT change (packet 571), a lower bound since the first PCR of the analysis is packet 44 |
| 0x0106 | MPEG-1 audio (0x03) in version 0 | even frames up to 146 | `ok`; its packets after the PMT change (frames 144, 146) are within the 500 ms grace |
| 0x0105 | MPEG-1 audio (0x03) in version 1 | odd frames from 139 | `ok`; its packets before the PMT change (frames 139, 141) are within the grace |
| 0x0011 | SDT PID (reserved PSI/SI range) | frames `f % 12 == 1` | never unreferenced |
| 0x0020 | the PAT's network_PID | frames `f % 12 == 5` | never unreferenced |
| 0x0400 | CA_PID of the PMT's CA_descriptor | frames `f % 12 == 9` | never unreferenced |
| 0x0300 | none | frames 31–47 (`f % 4 == 3`) | `unreferenced`: 5 packets, run of 12750 ms to the end of the range |
| 0x0500 | none | frames 155 and 159 | `insufficient_data`: 350 ms before the end of the range, too short to judge |
| 0x0777 | none | frame 61, `transport_error_indicator` set | not counted |

PMT version 1 is first sent in frame 142 (packet 570) and applies from packet
571. Expected analysis (both packet sizes): `pid_references` with
`referenced.status` `absent` and `unreferenced.status` `unreferenced`, one PSI
transition, no continuity error; the timestamp anomaly check reports the 6.1 s
PTS jump of PID 0x0101 caused by the silence.

### PID-reference cross-check

```bash
tsanalyze --json sample_data/diagnostics/sample_188byte_pid_references.ts
tsanalyze --format m2ts --json sample_data/diagnostics/sample_192byte_pid_references.ts
tsp -I file sample_data/diagnostics/sample_188byte_pid_references.ts -P count --summary -O drop
tsp -I file sample_data/diagnostics/sample_188byte_pid_references.ts -P continuity -O drop
```

TSDuck 3.45 agrees for both files:

- `tsanalyze --json`: the per-PID packet counts equal the analyzer's
  `packet_count` for every referenced PID (0x1000 40, 0x0100 160, 0x0101 100,
  0x0102 0, 0x0103 0, 0x0105 11, 0x0106 74). Like the analyzer, it leaves the
  packet with `transport_error_indicator` out (`tsp -P count` counts it: one
  packet on 0x0777). Its service PIDs are the union of both PMT versions plus
  the ECM PID 0x0400.
- `tsanalyze` lists 0x0300, 0x0500 and 0x0020 as unreferenced. The analyzer
  reports 0x0500 as `insufficient_data` because it appears less than 500 ms
  before the end of the range, where TSDuck's whole-file listing has no grace
  period; and it treats the PAT's `network_PID` 0x0020 as referenced, as
  ETSI TR 101 290 (check 3.4) excludes the NIT PID from Unreferenced_PID,
  whereas `tsanalyze` only marks a PID referenced through a table it parsed.
- `tsp -P continuity`: no discontinuity.
- An independent raw scan (PCR times of PID 0x0100 interpolated by packet
  index) gives the longest gap of PID 0x0101 as 6100 ms from offset 237·ps to
  481·ps and of PID 0x0500 as 350 ms, the analyzer's `max_silence_ms` and
  `longest_run_ms`.

The same `tsanalyze` comparison on every other sample in `sample_data` gives
equal per-PID counts and the same referenced and unreferenced PIDs (PID 0x0200
of the transport-error sample is unreferenced in both). The sync-loss samples
are compared after `tsresync`, as in the
[sync-loss cross-check](#sync-loss-cross-check).

## PES header and PTS repetition sample

`sample_data/diagnostics/sample_188byte_pes_health.ts` and its 192-byte M2TS twin
`sample_data/diagnostics/sample_192byte_pes_health.ts` (37 KiB each) are also
**intentionally invalid**. They carry PES headers that are truncated or have a
bad start code, bad flags, inconsistent lengths or bad PTS marker bits, a
valid header split over two TS packets, `PES_packet_length` 0 on video, and a
video stream without a PTS for 1080 ms, to test the PES header and PTS
repetition checks.

```bash
go run ./tools/generate_pes_health -output sample_data/diagnostics/sample_188byte_pes_health.ts
go run ./tools/generate_pes_health -m2ts -output sample_data/diagnostics/sample_192byte_pes_health.ts
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_pes_health.ts
```

### PES health layout and expected results

200 packets of one 20 ms slot each (75.2 kbit/s, 4 s): program 1 (PMT PID
0x1000) with MPEG-2 video (stream_type 0x02) on PID 0x0100, which carries a PCR
in every packet, MPEG-1 audio (0x03) on 0x0101, PES private data (0x06) on
0x0102, MPEG-2 audio (0x04) on 0x0103 with a single PES packet (slot 15), and
AVC video (0x1B) on 0x0104, which the PMT announces but which carries nothing.
Packet 0 is a null packet, the PAT is in packets 1 and 2 and the PMT in 3 and
4, so the analysis starts at packet 5. Video takes the even packets from 6,
audio the packets 4k+1 from 5, private data the packets 8k+3 from 11 that PSI
does not use. Video headers carry PTS and DTS with `PES_packet_length` 0,
audio and private headers a PTS alone. The video PES of packet 10 is split:
its first packet holds only 12 header bytes and packet 12 the rest. `-clean`
writes the same stream without the defects.

| Packet | PID | Defect | Expected `kind` |
| --- | --- | --- | --- |
| 27 | 0x0102 | `packet_start_code_prefix` 0x000002 | `bad_start_code` |
| 35 | 0x0102 | only 8 header bytes before the next PES of the PID (packet 51) | `truncated` |
| 61 | 0x0101 | last PTS `marker_bit` 0 | `bad_marker_bits` |
| 67 | 0x0102 | `PTS_DTS_flags` `'01'` | `bad_flags` |
| 75 | 0x0102 | optional header starts with `'01'` | `bad_flags` |
| 91 | 0x0102 | PTS flag with `PES_header_data_length` 3 | `inconsistent_length` |
| 107 | 0x0102 | `PES_packet_length` 4, shorter than the 8-byte header | `inconsistent_length` |
| 115 | 0x0102 | PTS prefix `'0011'` without a DTS | `bad_marker_bits` |
| 100–150 | 0x0100 | PES packets without PTS or DTS | PTS interval 1080 ms (98 → 152) |

Expected analysis (both packet sizes):

- `pes_headers`: `errors`, 161 headers checked, 8 errors (video 96 checked, 0
  errors; audio 49 checked, 1; private 15 checked, 7; PID 0x0103 1 checked;
  PID 0x0104 `insufficient_data` / `no_pes`).
- `pts_repetition`: `ng`, one interval of 1080 ms from packet 98 to 152 on
  PID 0x0100 (68 intervals measured). Audio is `ok` with a maximum of 80 ms:
  the malformed header of packet 61 breaks the interval, so 57 → 65 (160 ms)
  is not measured. Private data is `skipped` (`not_video_or_audio`), PID
  0x0103 `insufficient_data` (`single_pts`) and 0x0104 `no_pes`.
- No timestamp anomaly: the 1080 ms DTS step over PES packets without
  timestamps is not reported as a jump, and malformed headers give no
  timestamps. PCR-PTS/DTS gap 300 ms, from valid headers only.
- Without the defects (`-clean`), every header is valid and the largest video
  interval is 80 ms (around the split PES).

### PES health cross-check

```bash
tsp -I file sample_data/diagnostics/sample_188byte_pes_health.ts -P pes --trace-packets -O drop
tsp -I file sample_data/diagnostics/sample_188byte_pes_health.ts -P pcrextract --pcr --pts --dts --csv -O drop
tsp -I file sample_data/diagnostics/sample_188byte_pes_health.ts -P continuity -O drop
ffprobe -show_streams sample_data/diagnostics/sample_188byte_pes_health.ts
```

TSDuck 3.45 for both files (`--format m2ts` for the 192-byte one):

- `pes --trace-packets`: 95 video PES (the last, unbounded one is not flushed
  without `-f`; the analyzer checks it at the end of the range, 96) and 49
  audio PES, as the analyzer. On PID 0x0102 it reports the packets 35
  (`no PES header found`), 91 (`truncated, missing 2 bytes`, from the length
  mismatch) and 107 (`expected header size: 14 bytes`) as invalid, and 11
  valid PES packets. It does not see packet 27 as a PES packet at all (no
  start code), and it does not check the `'10'` bits, `PTS_DTS_flags` `'01'`
  or marker bits, so it accepts 67 and 75.
- `pcrextract`: video PTS in packet 98 then 152, so the 1080 ms interval
  matches. Its audio PTS skip packet 61 as well, and its private-data PTS skip
  67 and 115. It lists 69 video PTS where the analyzer counts 70: it does not
  decode the PTS of the header split over packets 10 and 12. Interpolating its
  PCRs by packet index gives the same per-PID maximum interval as the analyzer
  for every sample in `sample_data`, except where the analyzer breaks an
  interval (audio 160 ms here, around the malformed header) or starts after the
  PAT/PMT acquisition.
- `continuity`: no discontinuity.

ffprobe 9.0 finds the same five streams on the same PIDs (mpeg2video, mp3,
bin_data, mp3, h264). Its parsers cannot frame the filler elementary-stream
payload, so it is not a per-PES reference for video and audio; for the private
data it demuxes 13 PES packets (without packets 27 and 35), 12 of them with a
PTS, so it also accepts the flag, length and marker-bit problems.

## Packet-gap sample

`sample_data/diagnostics/sample_188byte_packet_gaps.ts` and its 192-byte M2TS twin
`sample_data/diagnostics/sample_192byte_packet_gaps.ts` (147 KiB and 150 KiB, 4 s) are a
**poorly interleaved** remux for the report-only packet gaps: audio arrives in
bursts of three packets every 320 ms, one burst is left out around a declared
PCR discontinuity, a PCR-only PID carries the PCR, and a private-data stream
is removed by one PMT change and added back by a later one. An SDT PID and a
single-packet PID 0x0012 complete it. The capture starts before the first PAT.
It is a test fixture, not an example of good multiplexing.

```bash
go run ./tools/generate_packet_gaps -output sample_data/diagnostics/sample_188byte_packet_gaps.ts
go run ./tools/generate_packet_gaps -m2ts -output sample_data/diagnostics/sample_192byte_packet_gaps.ts
go run ./tools/generate_packet_gaps -trailer -output sample_data/diagnostics/sample_204byte_packet_gaps.ts
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_packet_gaps.ts
```

### Packet-gap layout and expected results

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

`tsparser` tests pin these results for every packet size. The 204-byte form
gives the same numbers of packets and milliseconds, with offsets at
204·packet. The timestamp anomaly check reports the PTS jumps at the declared
PCR discontinuity.

### Packet-gap cross-check

```bash
tsanalyze --json sample_data/diagnostics/sample_188byte_packet_gaps.ts
tsanalyze --format m2ts --json sample_data/diagnostics/sample_192byte_packet_gaps.ts
```

For every file:

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

## Mux lead sample

`sample_data/diagnostics/sample_188byte_mux_lead.ts` and its 192-byte M2TS twin
`sample_data/diagnostics/sample_192byte_mux_lead.ts` (44 and 45 KiB) have known mux leads: a
remux with a large audio preload (1500 ms against 700 to 708 ms of video), an
AAC stream with one PES packet multiplexed 20 ms after its PTS and one without
a PTS, a PCR and PTS/DTS wraparound and a declared PCR discontinuity between a
PES packet and the next PCR, to test the report-only mux lead. They also carry
a transport error on one audio packet, and the 1500 ms lead fails the
PCR-PTS/DTS gap check. The stream is **intentionally unusual** (filler
elementary-stream payload): never use it as an example of a compliant stream.

```bash
go run ./tools/generate_mux_lead -output sample_data/diagnostics/sample_188byte_mux_lead.ts
go run ./tools/generate_mux_lead -m2ts -output sample_data/diagnostics/sample_192byte_mux_lead.ts
go run ./tools/generate_mux_lead -trailer -output sample_data/diagnostics/sample_204byte_mux_lead.ts
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_mux_lead.ts
```

### Mux lead layout and expected results

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

Expected analysis (every packet size, with offsets at packet × 188, 192 or
204), `multiplexing.mux_lead`:

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

The lead is interpolated by file position between PCRs, and every packet has
the same size, so the 204-byte form gives the same leads as the 188-byte file.

### Mux lead cross-check

```bash
tsp -I file sample_data/diagnostics/sample_188byte_mux_lead.ts -P pcrextract --pcr --pts --dts --csv -O drop
tsp -I file --format m2ts sample_data/diagnostics/sample_192byte_mux_lead.ts -P pcrextract --pcr --pts --dts --csv -O drop
```

TSDuck 3.45 (`pcrextract --pcr --pts --dts`) for the 188- and 192-byte files:
its PCR and PTS/DTS rows, interpolated by packet index between the PCRs before
and after each PES packet by an independent script (a step outside 0 to
1000 ms starts a new segment; the lead is read modulo 2^33 × 300), give
exactly the analyzer's sample counts, unbracketed counts, minimum and maximum
leads and their offsets for every PID. `pcrextract` also skips the audio PES
of packet 61. Its "Offset from PCR" column, which it fills for the PES packets
that carry a PCR (the video PID), is 63000 + 180 × ((s / 2) mod 5) (DTS) and
3600 more (PTS), that is 700 to 708 ms and 740 to 748 ms, as the analyzer's
video leads, for 100 of the 117 video PES packets. For the other 17 (packets
66 to 98, whose DTS has wrapped while the PCR has not yet) it prints the plain
difference, about −2^33; modulo 2^33 it is the same 700 to 708 ms, which the
analyzer reports.

For the 204-byte file, `pcrextract --pcr --pts --dts` writes the same CSV as
for the 188-byte file, and `tsanalyze --json` the same per-PID counts
(75,200 b/s; `bitrate-204` 81,600 b/s).

ffprobe 9.0 finds the four streams on the same PIDs (mpeg2video, mp3, aac,
bin_data), but its parsers cannot frame the filler payload and merge the PES
packets, so it is not a per-PES reference here; see the pull request of
issue #177 for the ffprobe cross-check on the encoded samples in
`sample_data`.

## PCR-to-PTS/DTS gap wraparound sample

`sample_data/diagnostics/sample_188byte_pts_gap_wraparound.ts`, its 192-byte M2TS twin
`sample_data/diagnostics/sample_192byte_pts_gap_wraparound.ts` and its 204-byte twin
`sample_data/diagnostics/sample_204byte_pts_gap_wraparound.ts` (37, 38 and 40 KiB) cross the
33-bit wraparounds in both orders: a video DTS (lead 700 ms) and an audio PTS
(lead 30 ms) that wrap before the PCR, and an AAC PTS (lead −150 ms) that wraps
after it, with PES packets whose bracketing PCRs span the PCR wrap. Every
PCR-to-PTS/DTS gap stays its stream's lead, and the gap check is `ok` at
700 ms; before the gap was made wraparound-aware
([#193](https://github.com/small-teton/mpeg-ts-analyzer/issues/193)) it failed
at about +95443568 ms. The stream is **intentionally unusual** (filler
elementary-stream payload, an AAC stream multiplexed 150 ms after its PTS):
use it to test the PCR-PTS/DTS gap check, `--dump-timestamp` and the mux lead
across a wraparound, never as an example of a compliant stream.

```bash
go run ./tools/generate_pts_gap_wraparound -output sample_data/diagnostics/sample_188byte_pts_gap_wraparound.ts
go run ./tools/generate_pts_gap_wraparound -m2ts -output sample_data/diagnostics/sample_192byte_pts_gap_wraparound.ts
go run ./tools/generate_pts_gap_wraparound -trailer -output sample_data/diagnostics/sample_204byte_pts_gap_wraparound.ts
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_pts_gap_wraparound.ts --dump-timestamp
```

### Wraparound layout and expected results

200 packets of one 20 ms slot each (75.2 kbit/s, 4 s): program 1 (PMT PID
0x1000) with MPEG-2 video (stream_type 0x02) on PID 0x0100, which carries the
PCR, MPEG-1 audio (0x03) on 0x0101 and AAC (0x0F) on 0x0102. Packet 0 is a null
packet, the PAT is in packets 1 and 2 and the PMT in 3 and 4; the PAT repeats
in packets 20, 40, … and the PMT in 28, 48, …. Video takes the packets 4k+2
from 6 (every 80 ms, each with a PCR), AAC the packets 4k+1 from 5 and audio
the packets 4k+3 from 7. Every PES packet fits in one TS packet; the other
packets are null packets. With `-m2ts` each packet gets a `TP_extra_header`
whose `arrival_time_stamp` advances by one slot; with `-trailer` each packet is
followed by a 16-byte trailer, making 204-byte packets. The lead is
interpolated by file position between PCRs and every packet has the same size,
so the leads are the same for all three packet sizes.

The 90 kHz clock of packet s is 2^33 − 100 × 1800 + s × 1800 (modulo 2^33), so
the PCR wraps at packet 100, between the PCRs of packets 98 and 102. Every
PTS/DTS is the clock of its packet plus a constant lead:

| PID | Lead | Wraps at | Order |
| --- | --- | --- | --- |
| 0x0100 video | DTS 700 ms, PTS 40 ms later | packet 66 (DTS) | PTS/DTS before PCR: packets 66 to 98 have a wrapped DTS and a PCR that has not wrapped; packet 98 is bracketed by the PCRs of 98 and 102 |
| 0x0101 audio | PTS 30 ms (small positive) | packet 99 | PTS before PCR, within the PCR bracket 98 to 102 that spans the PCR wrap |
| 0x0102 AAC | PTS −150 ms (negative) | packet 109 | PCR before PTS: packets 101 (bracket 98 to 102) and 105 (bracket 102 to 106) have a wrapped PCR and a PTS that has not wrapped |

Expected analysis (all three packet sizes):

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

### Wraparound cross-check

```bash
tsp -I file sample_data/diagnostics/sample_188byte_pts_gap_wraparound.ts -P pcrextract --pcr --pts --dts --csv -O drop
tsp -I file --format m2ts sample_data/diagnostics/sample_192byte_pts_gap_wraparound.ts -P pcrextract --pcr --pts --dts --csv -O drop
tsp -I file --format rs204 sample_data/diagnostics/sample_204byte_pts_gap_wraparound.ts -P pcrextract --pcr --pts --dts --csv -O drop
```

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

## Continuity reference stream

`tools/generate_continuity` writes a 14-packet, 188-byte stream for the
continuity-counter check. It is not committed: the `tsparser` test
`TestContinuityGeneratedFixture` runs the generator into a temporary directory
and pins the two events below.

```bash
go run ./tools/generate_continuity -output /tmp/continuity.ts
tsp -I file /tmp/continuity.ts -P continuity --json-line -O drop
```

The stream includes legal 15-to-0 wraparound, an exact duplicate, an invalid
same-counter packet, adaptation-only packets, a declared discontinuity, PUSI,
one counter gap, recovery after each error, and arbitrary null-packet counters.
TSDuck 3.44 reports two discontinuity events on PID `0x0100`: the invalid
same-counter packet and the gap. It reports no cascade after either event.
