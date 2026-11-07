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
  - per program: the existing summary (compliance, continuity, timestamp
    anomalies, PCR jitter, bitrate) and the average bitrate;
  - per program and PID: PCR samples and intervals, PES timestamps and the
    bracketed PCR → PTS/DTS gap, bitrate per 1-second window, PCR jitter
    samples, CC and timestamp events.
- **Per program vs. per multiplex**: PCR, PTS/DTS, timestamp anomalies,
  bitrate and PCR jitter are per program; continuity-counter results and
  warnings are per range, as the CLI already checks continuity across the
  multiplex. Bitrate and jitter are collected without the `--dump-*` flags.
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
is Cobra's built-in text output and is not used for this.

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
