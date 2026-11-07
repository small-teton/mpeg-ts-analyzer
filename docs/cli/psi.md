# PSI tables: PAT, PMT and SDT

[Documentation home](../index.md)

How the analyzer collects PAT and PMT sections, names services from the SDT
and follows PAT and PMT version changes during an analysis.

## Multi-section PAT and PMT

A PAT may be split into several sections (`section_number` 0 to
`last_section_number`). ISO/IEC 13818-1 requires a PMT's `section_number` and
`last_section_number` to be 0, but the analyzer also assembles multi-section
PMTs found in non-conforming streams. Before it selects or lists programs, the
analyzer collects every section of the table, so programs and elementary
streams in any section are found. The sample
`sample_data/sample_188byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts`
(and its 192-byte twin) carries the two-program sample's PAT as one section per
program and each PMT as one section per elementary stream, sent from the last
section to section 0; it lists the same programs as the single-section sample:

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts --list-programs
Input file:  sample_data/sample_188byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts
Packet size: 188 bytes
Detected PAT: 2 program(s)
Program 1: PMT PID 0x1000, PCR PID 0x100
Service: name "Main", provider "FFmpeg", service_type 0x01 (digital television service)
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x02 (13818-2 video or 11172-2 constrained parameter video stream)
PMT : Program Info : elementary_PID     : 0x101, stream_type : 0x03 (11172 audio)
Program 2: PMT PID 0x1001, PCR PID 0x102
Service: name "Second", provider "FFmpeg", service_type 0x01 (digital television service)
PMT : Program Info : elementary_PID     : 0x102, stream_type : 0x02 (13818-2 video or 11172-2 constrained parameter video stream)
PMT : Program Info : elementary_PID     : 0x103, stream_type : 0x03 (11172 audio)
```

How a table is assembled:

- Sections are combined only within one table identity and version: `table_id`,
  `transport_stream_id` (PAT) or `program_number` (PMT), and `version_number`.
  A PMT is taken only from sections whose `program_number` is the program's;
  other tables on the same PID are skipped. The first valid PAT section fixes
  the `transport_stream_id`; PAT sections with another one are ignored.
- Only sections with `current_next_indicator` 1 are used; a "next" table is
  ignored. The first version whose sections 0 to `last_section_number` have all
  arrived is used, whatever order they arrived in. Retransmitted sections are
  counted once.
- Programs are listed in `program_number` order, independent of the section
  they came from. A program listed twice keeps its first PMT PID in section
  order.
- A PMT uses the `PCR_PID` of section 0. Elementary streams are listed in
  section order, each PID once; program descriptors of every section are kept.
- The analysis starts after the packet that begins the next section on the
  table's PID, as it does for a single-section table. A multi-section table
  therefore takes longer to acquire, and the analyzed window can start later
  than for the same stream with single-section tables.
- `--dump-psi` prints every section of the table in `section_number` order.

Problems found while collecting a table are reported as warnings (JSON
`warnings`, or a text line starting with the offset of the affected packet).
The affected section is not used; collection continues with later
retransmissions:

| Warning | Meaning |
| --- | --- |
| `<table> section N (version V) has an invalid CRC32; section ignored` | CRC32 check failed |
| `<table> section is malformed (too short or section_syntax_indicator 0); section ignored` | Not a valid long-form section |
| `<table> section N (version V) is beyond last_section_number L; section ignored` | `section_number` greater than `last_section_number` |
| `<table> section N (version V) cannot be parsed: ...; section ignored` | Fields run past the end of the section |
| `<table> version V changed last_section_number from A to B; discarding K collected section(s)` | Inconsistent section set; collection starts over |
| `<table> section N changed without a version change (version V); discarding K collected section(s)` | Same section, different content; collection starts over |
| `<table>: continuity_counter jumped from A to B inside a section; partial section discarded` | Packet loss inside a section |
| `<table>: section ended early by the next payload_unit_start_indicator; partial section discarded` | A section was cut short |
| `<table>: invalid pointer_field N; packet ignored` | `pointer_field` runs past the payload |
| `<table>: transport_error_indicator set; packet ignored and any partial section discarded` | The packet has an uncorrectable error, so nothing it carries is used |
| `<table>: transport_scrambling_control set; scrambled payload ignored and any partial section discarded` | PAT and PMT are never scrambled; a scrambled payload cannot be read as a section |
| `PAT lists program N with PMT PIDs ...`, `PMT of program N has PCR_PID ...`, `PMT of program N lists elementary PID ... with stream types ...` | Sections disagree; the first value in section order is used |

`<table>` is `PAT` or `PMT of program N on PID 0xXXXX`. A table that is still
missing sections at the end of the input or `--limit` range is never used in
part. The analyzer reports, for example, `incomplete PAT (version 0): missing
section(s) 1 of 0..1; retrying PAT discovery`, or `PAT: no valid section found`
when no usable section arrived. For an incomplete PMT, `--list-programs` and
`--output analysis` show the program with `status: "incomplete"`,
`reason: "pmt_parse_failed"`, and the message as `error`. Version changes
after the tables have been collected are described in
[PAT and PMT version changes](#pat-and-pmt-version-changes).

## Service names (SDT)

DVB streams, and streams written by FFmpeg (`-metadata service_name=...`,
`-metadata service_provider=...`), name their programs in the SDT (Service
Description Table, ETSI EN 300 468) on PID 0x0011. The analyzer reads the SDT
of the analyzed transport stream (SDT actual, `table_id` 0x42); the SDT of
other transport streams (`table_id` 0x46) and other tables on the PID are
ignored. A service is matched to the program whose `program_number` equals its
`service_id`. Sections are assembled as described in
[Multi-section PAT and PMT](#multi-section-pat-and-pmt), so a multi-section
SDT is used once every section has arrived.

- `--list-programs` and the automatic multi-program listing print a `Service:`
  line under each program the SDT names, and add `service` to each program in
  JSON. The listing reads from the PAT until it has a complete SDT, for at most
  3 s of PCR time (EN 300 468 repeats the SDT at least every 2 s), or 100,000
  packets when no PCR has been seen.
- `--format json` (`programs[]` of the summary) and `--output analysis` report
  `service` from the SDT version in effect at the end of the analyzed range.
  New versions are applied as PAT and PMT versions are: a table with the
  active `version_number` and other content is not applied, and a late
  retransmission of a superseded version does not bring back its names.
- The text output of an analysis does not change.
- A stream without an SDT, or whose SDT does not list a program, has no
  `service` for it. Problems with SDT sections are not reported as warnings;
  the names are left out and no verdict depends on them.

```json
"service": {
  "service_type": "0x01",
  "service_type_description": "digital television service",
  "provider_name": "FFmpeg",
  "name": "Service01"
}
```

`service_type`, `service_type_description`, `provider_name` and `name` come
from the service's service_descriptor (tag 0x48) and are absent without one;
an empty name is also absent. Names are decoded from the DVB character tables
(EN 300 468 Annex A): the default table (ISO/IEC 6937, with the euro sign at
0xA4), ISO/IEC 8859 parts selected by the first byte (0x01-0x0B, or 0x10 0x00
0xNN) and UTF-8 (0x15). Control codes 0x8A (line break) become `\n`; the other
control codes, such as character emphasis, are dropped. A name in another
table (UCS-2, KS X 1001, GB2312, Big5, an `encoding_type_id`, ARIB STD-B24 text
of Japanese broadcasts), or with a code its table does not assign, is reported
as `name_hex` or `provider_name_hex` instead: the raw bytes, including the
table selector, as lowercase hex.

```
Service: name (undecodable) 0x110041, provider "Prö", service_type 0x02 (digital radio sound service)
```

## PAT and PMT version changes

The analysis starts from the first complete PAT and PMT, then keeps collecting
the PAT and the PMT of each analyzed program for the rest of the input, with the
same assembly rules as above. A complete table with `current_next_indicator` 1
and a `version_number` different from the active one replaces the layout from
the packet after the one that completed it. Each applied change is printed in
text mode as a line starting with the offset of that packet, and listed in JSON
as `psi_transitions`.

The sample
`sample_data/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts`
(and its 192-byte twin, generated by `tools/generate_psi_version_change`) sends
PMT version 1 in packet 545, which moves the video and the PCR from PID 0x100 to
0x200 and changes the audio `stream_type` from 0x03 to 0x04, and PAT version 1
in packet 1131, which moves the PMT to PID 0x1100:

```
$ ./mpeg-ts-analyzer sample_data/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts --dump-bitrate
Input file:  sample_data/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts
Packet size: 188 bytes
Detected PAT: 1 program(s)
Detected PMT
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x02 (13818-2 video or 11172-2 constrained parameter video stream)
PMT : Program Info : elementary_PID     : 0x101, stream_type : 0x03 (11172 audio)
0x0001903c PMT of program 1 on PID 0x1000 version 0 -> 1: PCR_PID 0x0100 -> 0x0200, added 0x0200 (stream_type 0x02), removed 0x0100 (stream_type 0x02), stream_type of 0x0101 0x03 -> 0x04
0x00033e94 PAT version 0 -> 1: program 1 PMT PID 0x1000 -> 0x1100
-----------------------------
TS Sync: no sync loss detected
-----------------------------
Compliance Check Results:
Max PCR interval: 80.000000ms [OK, limit: <= 100.000000ms]
PCR discontinuities: 0 undeclared in 60 PCRs [OK]
  - pcr_pid_change: 1, first at 0x000190f8 (PCR jumped +80.000ms)
PCR-PTS/DTS max gap: 726.666667ms [OK, limit: <= 1000.000000ms]
-----------------------------
Bitrate Summary (PCR time base):
  PID     Type                                                                  Avg(bps)    Peak/1s(bps)
  0x0000  PAT                                                                     12,317          13,536
  0x0100  13818-2 video or 11172-2 constrained parameter video stream             17,827          60,160
  0x0101  13818-3 audio                                                          404,524         430,144
  0x0200  13818-2 video or 11172-2 constrained parameter video stream             42,462          60,160
  0x1000  PMT                                                                      8,103          13,536
  0x1100  PMT                                                                      4,213          12,032
          Total (program)                                                        489,448         517,376
  0x1fff  NULL (mux-wide)                                                              0               0
  Duration: 4.64s (PCR, 2 segment(s))
-----------------------------
Continuity Counter: no errors detected
-----------------------------
Transport Error Indicator: no packets with transport_error_indicator set
Transport Scrambling: no scrambled packets
-----------------------------
PAT/PMT health: OK (repetition limit 500 ms, ETSI TR 101 290 1.3.a/1.5.a; time base: PCR PID 0x0200 of program 1)
  PAT PID 0x0000 : 43 tables, 38 intervals, max 150.222 ms ending at 0x00020388 [OK]
  PMT program 1 PID 0x1100 : 43 tables, 37 intervals, max 148.444 ms ending at 0x00020444 [OK]
-----------------------------
Referenced PIDs: 8 references, silence limit 5000 ms [OK]
Unreferenced PIDs: none [OK]
-----------------------------
Max packet gaps (report only): 7 PIDs, time basis pcr_interpolated_file_position
  PID 0x0000 : 43 packets, max gap 44 packets, 150.222 ms from 0x0001e27c to 0x00020388
  PID 0x0011 : 10 packets, max gap 173 packets, at least 471.111 ms from 0x00016e74 to 0x0001ee3c
  PID 0x0100 (program 1 PCR, program 1 ES stream_type 0x02) : 70 packets, max gap 15 packets, 42.667 ms from 0x00006034 to 0x00006bf4
  PID 0x0101 (program 1 ES stream_type 0x03, program 1 ES stream_type 0x04) : 1359 packets, max gap 10 packets, 25.778 ms from 0x000069c0 to 0x000071d4
  PID 0x0200 (program 1 PCR, program 1 ES stream_type 0x02) : 132 packets, max gap 16 packets, 43.871 ms from 0x0002e094 to 0x0002ed10
  PID 0x1000 (program 1 PMT) : 29 packets, max gap 44 packets, 148.444 ms from 0x0001e338 to 0x00020444
  PID 0x1100 (program 1 PMT) : 14 packets, max gap 43 packets, 147.922 ms from 0x00033f50 to 0x00035fa0
```

The [PAT/PMT health](usage.md#pat-and-pmt-health-always-on) summary counts the
PMT of program 1 on both PIDs as one table: its intervals restart on the new
PID, and the PCR_PID change starts a new PCR segment, so no interval spans
either change.

With `--dump-psi`, every section of an applied table is printed after its
line. In JSON, the same changes are (abridged):

```json
"psi_transitions": [
  {
    "offset_bytes": 102460,
    "table": "pmt",
    "pid": "0x1000",
    "previous_version": 0,
    "version": 1,
    "program_number": 1,
    "previous_pcr_pid": "0x0100",
    "pcr_pid": "0x0200",
    "elementary_streams": [ { "pid": "0x0200", "stream_type": "0x02", ... }, { "pid": "0x0101", "stream_type": "0x04", ... } ],
    "elementary_streams_added": [ { "pid": "0x0200", "stream_type": "0x02", ... } ],
    "elementary_streams_removed": [ { "pid": "0x0100", "stream_type": "0x02", ... } ],
    "stream_type_changes": [ { "pid": "0x0101", "previous_stream_type": "0x03", "stream_type": "0x04", "stream_type_description": "13818-3 audio" } ]
  },
  {
    "offset_bytes": 212628,
    "table": "pat",
    "pid": "0x0000",
    "previous_version": 0,
    "version": 1,
    "programs": [ { "program_number": 1, "pmt_pid": "0x1100" } ],
    "programs_added": [],
    "programs_removed": [],
    "pmt_pid_changes": [ { "program_number": 1, "previous_pmt_pid": "0x1000", "pmt_pid": "0x1100" } ]
  }
]
```

`offset_bytes` is the offset of the packet that completed the new table.
`elementary_streams` and `programs` are the complete new layout, so the layout
in effect at any offset follows from `programs` (the layout at the start) and
the transitions before it. `psi_transitions` is left out when no change was
applied, so the output for a stream without table updates is unchanged.
`--output analysis` also reports the layout in effect over each byte interval
(`psi_layout`, see [PSI layout of the range](../reference/json-output.md#psi-layout-of-the-range)).

What happens at a change:

| Change | Effect |
| --- | --- |
| `PCR_PID` changes | The first PCR on the new `PCR_PID` starts a new timing segment, as a `discontinuity_indicator` would: no PCR interval, jitter estimate or bitrate window spans the change, and the PCR jitter summary lists it as a discontinuity. PCRs on the old PID are ignored. A PES that has not seen a PCR after the change has no PCR-PTS/DTS gap. |
| Elementary PID removed | Its buffered PES is finalized at the change, as at the end of the input, and its PES and timestamp state is released. Later packets of the PID are not analyzed or counted for the program. |
| `stream_type` changes | As a removal followed by an addition of the PID. |
| PID added | An added elementary PID is analyzed from its next `payload_unit_start_indicator` with fresh timestamp state. The continuity state of a PID new to the layout (elementary, `PCR_PID` or PMT PID) is reset unless another analyzed program uses it, so a PID that returns after a pause is not a continuity error. |
| PID in several layouts | One row per PID in the bitrate and continuity summaries; the bytes of all its layouts are combined and the label comes from the latest layout listing it. A PID's average bitrate is over the whole measured duration. |
| PAT moves a program's PMT | The PMT is read from the new PID. The first complete PMT there is applied even with the same `version_number` when its content differs, descriptors included. |
| Program removed from the PAT | Its analysis stops after that packet and does not resume. `--output analysis` reports `analysis_end_offset_bytes` for it. |
| Program added to the PAT | Recorded in `programs_added`; not analyzed. |

Candidates that are never applied: a table still missing sections, a section
with an invalid CRC32 or syntax, a "next" table (`current_next_indicator` 0),
a table whose `version_number` equals the active one, and a late retransmission
of a superseded version, that is, a table byte-identical to the one last
applied with its `version_number`. The layout therefore never rolls back to an
older version; because `version_number` wraps after 31, a reused version with
different content is applied. Tables are compared by their section bytes, so a
change to descriptors alone counts as a change. Retransmitted sections are
counted once and sections may arrive in any order, so the result does not
depend on repetition or order. The following warnings are reported once per
message, at the offset of the first occurrence; a `continuity_counter` jump
inside a section is not repeated as a warning because the continuity summary
reports it:

| Warning | Meaning |
| --- | --- |
| `<table> section N (version V) has an invalid CRC32; section ignored` and the other assembly warnings above | A section of a later table is broken |
| `<table>: transport_error_indicator set; ...`, `<table>: invalid pointer_field N; packet ignored`, `<table>: section ended early by the next payload_unit_start_indicator; ...` | A later packet on the PAT or PMT PID was damaged; its data is not used |
| `PAT changed without a version change (version V); change ignored`, `<table> changed without a version change (version V); change ignored` | A complete table differs from the active one with the same `version_number` |
| `incomplete <table> (version V) at the end of the analysis: missing section(s) ... of 0..L; not applied` | A new version never completed; given at the end of the analysis |
