# PID-reference fixture

Write a 16 s constant-rate stream whose PSI layout references absent, silent
and intermittent PIDs, and which carries packets on PIDs that nothing
references, then check it with TSDuck. The stream is **intentionally
invalid**: use it to test the PID reference check, never as an example of a
compliant stream.

```sh
go run ./tools/generate_pid_references -output sample_data/diagnostics/sample_188byte_pid_references.ts
go run ./tools/generate_pid_references -m2ts -output sample_data/diagnostics/sample_192byte_pid_references.ts
./mpeg-ts-analyzer sample_data/diagnostics/sample_188byte_pid_references.ts
tsanalyze --json sample_data/diagnostics/sample_188byte_pid_references.ts
tsanalyze --format m2ts --json sample_data/diagnostics/sample_192byte_pid_references.ts
tsp -I file sample_data/diagnostics/sample_188byte_pid_references.ts -P count --summary -O drop
tsp -I file sample_data/diagnostics/sample_188byte_pid_references.ts -P continuity -O drop
```

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
571. Expected analysis (both packet sizes; `tsparser` tests pin it and check
that the committed files equal the generator output): `pid_references` with
`referenced.status` `absent` and `unreferenced.status` `unreferenced`, one PSI
transition, no continuity error; the timestamp anomaly check reports the 6.1 s
PTS jump of PID 0x0101 caused by the silence.

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
are compared after `tsresync`, as described in `tools/generate_sync_loss`.
