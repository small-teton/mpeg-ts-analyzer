# GUI ↔ CLI interface

How the GUI ([design.md](design.md)) calls the CLI and what it reads back.
Flag names and record shapes are not final; they are settled in the linked
issues. This document records the requirements those issues have to meet.

## Principles

- **One process per request.** The GUI starts the CLI, reads stdout, and waits
  for it to exit. There is no resident process or custom protocol.
- **Cancellation** is killing the process. Results of a superseded request are
  discarded.
- **Progress** of a range analysis is reported on stderr as structured events
  (bytes processed), distinguishable from ordinary diagnostics.
- **stdout carries data only**; diagnostics go to stderr. This already holds for
  `--format json` and applies to every new output.
- **One JSON document per invocation**, sharing the envelope of the existing
  report: `schema_version`, `input`, `status`, `reason`, `error`, `warnings`,
  plus a field naming the output kind. Each request is bounded (by the scan
  range or by a page size), so streaming formats such as JSON Lines are not
  needed. The existing `--format json` summary stays as it is.
- **Verdicts are the CLI's.** The GUI shows every `status` as reported and
  does not recompute or reinterpret it; it treats an unknown status as a
  finding with that status rather than as `ok` (Overview findings in
  [design.md](design.md#overview)).
- **Exit codes and `status` / `reason` values** follow the existing contract in
  [CLI usage](../cli/usage.md) and [Machine-readable output](../reference/json-output.md).
- **Byte offset** is the key shared by all outputs; every record that refers to
  a place in the file carries `offset_bytes`.
- **The CLI reads only `[offset, offset + limit)`**, as today. (Detecting a
  truncated tail exactly at the limit reads one byte past it.)

## Requests

| Request | Used by | Input | Output |
|---|---|---|---|
| Range analysis | Overview, Timeline, Bitrate, Packets labels | file, `--offset`, `--limit` | summaries, time series and PSI layout for **all programs** in the range |
| Packet page | Packets list | file, start offset, packet count | light per-packet fields and paging metadata |
| Packet detail | Packets detail pane | file, packet offset | full decode of one packet and the unit it belongs to |
| Capabilities | startup check | none | CLI version, schema versions, supported outputs |

### Range analysis

One scan of the range returns everything the analysis screens need, so
switching programs or screens never runs the CLI again.

- **All programs.** Today the CLI analyzes one program and stops with
  `programs_listed` when there are several. The GUI needs every program's
  results from one scan:
  - per program: the existing summary (compliance, including the PCR
    discontinuity check, continuity, timestamp anomalies, PCR jitter,
    bitrate) and the average bitrate;
  - per program and PID: PCR samples and intervals, PES timestamps and the
    bracketed PCR → PTS/DTS gap, bitrate per 1-second window, PCR jitter
    samples, CC and timestamp events.
- **Per program vs. per multiplex**: PCR, PTS/DTS, timestamp anomalies,
  bitrate and PCR jitter are per program; continuity-counter results,
  transport errors and scrambled packets (`transport_errors`,
  `scrambled_packets`), TS synchronization (`sync`), referenced and
  unreferenced PIDs (`pid_references`) and warnings are per range, as the
  CLI checks them
  across the multiplex. PES header and PTS repetition results (`pes_headers`,
  `pts_repetition`) are reported once per range and list every elementary
  stream by `program_number` and PID. Bitrate and jitter are collected without the `--dump-*` flags.
- **PSI layout**: each PAT/PMT section seen (offset, version), the interval each
  layout applies to, the PIDs that change, and every PID observed in the range.
  The GUI keeps this to label packets and to mark changes on the plots (#93,
  building on #59 and #60). It is `psi_layout` (`sections`, `layouts`, `pids`)
  next to `psi_transitions` in the range analysis document: a packet's PID is
  looked up in the layout whose `[offset_bytes, end_offset_bytes)` contains the
  packet, and `transition_indices` links a layout to the changes that started
  it. `psi_layout.sections` is a bounded list of runs (first and last copy,
  count), not a per-packet index; the packet page carries what each
  section-start packet says (`sections`: `table_id`, `table_id_extension`,
  version and section numbers), which matches at most one run by PID, header
  and offset, giving the copy's status (including invalid). See
  [PSI layout of the range](../reference/json-output.md#psi-layout-of-the-range).
- **Analysis start**: statistics start after the PAT section and the needed
  PMTs are complete, not at the first PAT. The output reports that offset (per
  program), so the GUI can show the unanalyzed lead.
- Size is bounded by the range (the GUI asks before scanning a large range).

### Packet page

Reads packets directly, without PAT/PMT discovery, so a page shows every packet
from its first one.

- Request: start offset and a maximum packet count (the GUI uses 100). Pages
  carry no PSI context; the GUI labels them with the PSI layout of the range
  analysis.
- Per packet: offset, PID, PUSI, TEI, scrambling control, adaptation field
  control, CC, PCR presence, and the headers of the PSI sections that start in
  the packet (from the packet's own bytes).
- Metadata: packet size (188/192), the synchronized start offset,
  `next_offset_bytes`, end of file, file size and total packet count.
- Defined behavior for offsets that are not on a packet boundary, for the M2TS
  4-byte header, and for a truncated last packet.

### Packet detail

- TS header, adaptation field (PCR, flags), payload kind, PAT/PMT section
  fields and tables, PES header fields, and the 188/192 raw bytes.
- The request includes the scan range, so the CLI can search back for the
  unit's first packet within it.
- The **unit** the packet belongs to: the PES packet or PSI section, its first
  packet offset (or that it started before the range), the packet's position,
  the number of packets in the unit within the range, and the previous and
  next member offsets. The full member list is not returned: a video PES can
  span thousands of packets.
- Every decoded field carries its byte span within the packet, so the GUI can
  highlight it in the hex dump. Fields that do not fill whole bytes (flags,
  PID, lengths, PCR, PTS/DTS) also carry packet-relative bit ranges in wire
  order, so a bit inspector can highlight exactly their bits; a field without
  ranges uses every bit of its byte span.

### Capabilities

An input-free command returning the CLI version, schema versions and supported
outputs, so the GUI can reject an incompatible CLI found on `PATH`. `--version`
is Cobra's built-in text output and is not used for this. The GUI reads analysis
schema 2.x (PCR jitter with `estimate` and `delivery`) and packet schema 1.x;
`compliance.pcr_discontinuities`, added within 2.0, is optional, and an older
2.0 CLI without it simply has no PCR discontinuities row. A
CLI that writes analysis schema 1.x is rejected with a request to update the
CLI.

## Output limits

Settings accepts any executable as the CLI, so the GUI bounds what it reads
from every run (`Limits` in `gui/src/cli/bounded.rs`). JSON documents are
parsed as they are read; a document is read up to its limit and rejected at the
first byte past it, never truncated. The rejected run is killed, and the error
names the output and the limit, for example *The CLI wrote more than 320 MiB of
--output analysis JSON (the limit for a 64 MiB range); the output was rejected.
Analyze a smaller range, or check the CLI chosen in Settings.*

| Output | Limit | Today's CLI writes |
|---|---|---|
| `--capabilities` | 64 KiB | about 500 bytes |
| `--output packets` | 1 MiB + 4 KiB per requested packet (1.4 MiB for the default page of 100, about 40 MiB for the largest of 10,000) | about 300 bytes per packet |
| `--output packet` | 1 MiB | under 8 KiB |
| `--output analysis` | 64 MiB + 4 bytes per byte of the range (320 MiB for the default 64 MiB) | 0.22–0.24 bytes per input byte on the samples |
| stderr diagnostics | the latest 64 KiB of lines, each cut at 4 KiB | — |
| progress events | the latest event only | one per progress step |

The analysis limit follows the range because its series (PCR, PES, PCR jitter,
bitrate windows) grow with it: one program with a PCR, a jitter sample and a
timestamped PES start in every 188-byte packet writes about 1.3 bytes per input
byte, and the factor of 4 leaves room for several programs sharing PIDs. The
fixed part covers the PSI layout and summaries.

Dropped diagnostics are replaced by a marker such as `[… 120 earlier diagnostic
lines (6.4 KiB) omitted …]`, and a cut line ends with `[… N more bytes of this
line omitted]`. Progress events are coalesced to the newest one, and the UI is
asked to repaint only when it has taken the previous one. A process that keeps
a pipe open after being killed (for example through a child of its own that
does not write) can leave its reader thread waiting, holding at most the limit
for that output.

## Related issues

Tracking: #101 (milestone GUI v0.1).

| Issue | Topic |
|---|---|
| #83 | Output contract: one JSON document per request, shared envelope |
| #91 | Analyze all programs in one scan |
| #93 | PSI layout of an analyzed range (builds on #59, #60) |
| #87 | Timing and event series |
| #88 | Bitrate window and PCR jitter series |
| #84 | Packet paging |
| #85 | Packet detail with byte spans and unit membership |
| #109 | Bit ranges of packet detail fields (bit inspector) |
| #89 | Capabilities query |
| #90 | Progress events on stderr (`--progress json`) |
| #92 | SDT service names (`programs[].service`; ARIB excluded) |
| #86 | Closed: separate scan and emission ranges are not needed |
