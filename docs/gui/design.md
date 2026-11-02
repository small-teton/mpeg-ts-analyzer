# GUI design

Design of the desktop GUI for `mpeg-ts-analyzer`. This document describes the
current design; [decisions](#decisions) records what was chosen and why. The
interface between the GUI and the CLI is in [cli-interface.md](cli-interface.md).

Status: once a range has been analyzed, Overview, Timeline and Bitrate show the
CLI's range analysis and Packets reads the range's packets from the CLI; before
that the screens show sample data from `gui/src/mock*.rs`. PMT-update markers,
the Unit column and layout-change rows wait for the PSI layout output (#93).
Work is tracked in #101 (milestone GUI v0.1).

## Architecture

```
┌──────────────────────────┐  spawn, read stdout (JSON)  ┌───────────────────────────┐
│ gui/ (Rust, eframe/egui) │ ──────────────────────────▶ │ mpeg-ts-analyzer (Go CLI) │
│ egui_plot, rfd           │ ◀────────────────────────── │ the only analysis logic   │
└──────────────────────────┘                             └───────────────────────────┘
```

- The GUI runs the CLI as a child process and reads JSON from stdout. There is
  no FFI (cgo / c-shared).
- Analysis logic and verdicts live only in the CLI, so the GUI and the CLI
  cannot disagree. CLI changes for the GUI add output surfaces; they do not
  change analysis results.
- The CLI binary is built from the same commit and bundled with the GUI.

### CLI discovery order

1. `MPEG_TS_ANALYZER_CLI` environment variable
2. CLI path from Settings (after v0.1, #99)
3. CLI bundled with the GUI, located relative to the GUI executable (after
   v0.1, #99)
4. `mpeg-ts-analyzer` on `PATH`

The first release uses 1 and 4 only. At startup the GUI queries the CLI's
capabilities (#89) and reports a missing or incompatible CLI.

On Windows the CLI is started with `CREATE_NO_WINDOW`, and the GUI binary uses
`windows_subsystem = "windows"`, so no console window appears.

## Workflow

1. **Open a file** (Open… or drag and drop). Nothing is analyzed yet.
2. **Choose the scan range** in bytes: drag it on the range bar or type exact
   Offset / Limit values.
3. **Start.** The CLI scans the range once. Overview, Timeline and Bitrate show
   the result of that scan.
4. **Packets** fetches packets of the same range page by page.
5. To look elsewhere, change the range and Start again.

There is no pre-scan when a file is opened, no time-based range, and no
separate Analyze step per screen.

### Scan range

- Byte based; maps directly to the CLI's `--offset` / `--limit`. Values stay
  inside the file but are not snapped to packets: the packet size (188 or 192)
  is unknown until the CLI has read the file, and the CLI synchronizes on
  packets itself. After Start the GUI uses the packet size and sync offset the
  CLI reports. A file shorter than one 192-byte packet, or one whose size cannot
  be read, is shown with the reason and Start stays disabled.
- **Range bar**: the whole file, with the selected range drawn between two
  handles. Drag a handle to resize, drag the middle to move, click elsewhere to
  jump there, scroll to grow or shrink the Limit. The bar shows the range size
  in the middle (`64.0 MiB of 3.9 GiB`) and is labeled "Scan range".
- **Offset / Limit fields** accept decimal (commas allowed) or `0x` hex.
- **Load color**: the range is colored by its size relative to the
  confirmation threshold (Settings, default 1024 MiB): blue (≤ 1/8), teal
  (≤ 1/4), yellow (≤ 1/2), orange (≤ 1), red (above). Start on a red range
  asks for confirmation.
- The initial range after opening a file is 64 MiB from the start.
- The range is scanned from its first byte. Statistics start once the PAT
  section and each program's PMT are complete, so the beginning of the range
  is not analyzed; the CLI reports where collection started (per program) and
  the GUI shows that lead. This happens only at the start of the range: the
  GUI does not read ahead of the range (no lead-in).

### Analysis in progress

- Start opens a modal progress dialog; the rest of the window is grayed out
  and does not accept input.
- The dialog shows the range, a progress bar with percentage, bytes scanned of
  the range, elapsed time, and **Cancel**. Only Cancel closes it; clicking the
  backdrop or pressing Escape does not.
- Cancel kills the CLI. The previous result, if any, stays shown.
- Progress comes from the CLI's progress events. Without them the bar is
  indeterminate and only elapsed time is shown.
- Fetching a packet page is short and does not use the dialog; the list shows
  a small loading indicator, and only the most recently requested page is
  displayed.

## Screens

Navigation is a left rail of tiles (large icon over a short label): Overview,
Timeline, Bitrate, Packets. Settings and About are icons at the bottom of the
rail. There is no menu bar.

### Program selection

A transport stream can carry several programs (for example a broadcast capture
with a main service and a second service). Overview, Timeline and Bitrate each
show a **Program** selector; the choice is shared among the three. Packets is
not affected by it.

- Programs are labeled from what the range analysis knows, since PAT/PMT do
  not name services:
  `1 · H.264 video + AAC audio · 1.70 Mbps (PMT 0x1000)`.
  The description comes from PMT stream types; the bitrate is the program's
  average over the range. Service names from the SDT may be added later (#92).
- Switching programs does not run the CLI again: the range scan returns every
  program.

### Overview

Stream section first (program tree with PIDs, PCR PID, PIDs that appear or
disappear within the range, PMT updates; file information), then compliance
check cards, the elementary stream table, and warnings with links to the
packet.

### Timeline

Per program, per PID:

- PCR interval of the program's PCR PID, with the 100 ms limit line.
- PCR → PTS/DTS gap, one line per elementary-stream PID, with the 1000 ms
  limit line. When a PID changes mid-range (PMT update), the old PID's line
  ends and the new one starts; there is no merging by role.
- PMT updates are drawn as vertical dashed lines across all plots.
- Event lane (CC errors, PCR NG).
- Zoom and pan are horizontal only; the plots share the x axis. Reset zoom sits
  in the toolbar above the plots.
- Clicking a point selects it; the side panel shows its details and "Open
  packet". Right-click offers the same actions.
- Time is the CLI's elapsed program time (PCR time since the first PCR of the
  program, frozen across discontinuities). Events and jitter samples, which
  carry only a byte offset, sit at the time of the last PCR before them.
- Series longer than 4000 points are drawn decimated, keeping each bucket's
  minimum and maximum so spikes stay visible; picking uses every point.

### Bitrate

Per program: bitrate per PID over time as steps over the CLI's windows (the
last one may be shorter than 1 s), the program total without null packets;
PCR jitter over time and its histogram; table of averages, 1-second peaks and
shares of the program average.

### Packets

The range is listed packet by packet, **100 packets per page** (Settings). Each
page is fetched from the CLI on demand; only a few pages are cached. Per-packet
data is large, so the whole range is never held in memory.

- Pager: First / previous / `Page n / N` / next / Last, the packet span of the
  page, and the PID filter at the right end.
- **PID filter**: a checkbox per PID found in the range, ordered PAT, PMT,
  elementary streams, null, PID order within each group; All / None.
- **List**: whole rows are selectable. Columns: #, Offset, PID, Type, PUSI, AF
  (PCR), CC, Unit, Notes; every header explains its column on hover.
  - Type comes from the PSI layout in effect at that packet.
  - Unit is the PES packet or PSI section the packet belongs to and its
    position in it (`Video PES #12 · 7/48`).
  - A highlighted row marks where the PMT was updated.
- **Detail pane**:
  - "Belongs to": the PES/section, the packet's position in it, first packet,
    PTS/DTS, and First / Previous / Next to move within that unit only
    (switching pages as needed).
  - "This packet": TS header, adaptation field (PCR and interval verdict),
    payload kind, PAT/PMT section fields and tables, PES header fields.
  - Hex dump colored by layer. Clicking a field (or a PAT/PMT table row)
    highlights its bytes in the hex dump.
- The GUI keeps the **PSI layout** (where PAT/PMT occur and what they say,
  including mid-range changes) from the range scan, and uses it to label
  packets. Packet pages themselves are read without PSI context.
- Implementation: the first request reads page 1 from the range start and
  learns the first packet boundary; page *n* is then read from
  `first + (n - 1) × 100 × packet size`, with `--packet-size` from the analysis,
  so paging visits every packet of the range once. The current page and the 4
  most recently viewed are kept; a page that is still loading when it drops
  out is cancelled, so paging quickly leaves only the last request running.
  Until #93, Type comes from the analysis (PAT, each program's PMT and
  elementary streams, null, and the continuity check's labels for other PIDs)
  and the Unit column is shown only in the detail pane.

## Settings

| Setting | Default | Notes |
|---|---|---|
| Language | System default | UI is English only for now |
| Confirm ranges above | 1024 MiB | Start asks for confirmation above this Limit |
| Packets per page | 100 | |
| Cached pages | 5 | Packet pages kept in memory |
| CLI path | empty | See [CLI discovery order](#cli-discovery-order) |

Settings are not persisted yet (eframe `persistence` is planned).

## Look and feel

- Fonts: Inter (UI; SemiBold for headings), JetBrains Mono (data), Phosphor
  icons. All bundled under `gui/assets/fonts/` with their licenses.
- Phosphor is registered as its own font family `icons` and always selected
  explicitly (`icons::text`, `icons::font`). As a fallback font its Private Use
  Area codepoints collide with glyphs in other fonts.
- Text and controls are larger than egui's defaults (body 15, heading 22,
  monospace 13.5, control height 26). Data-dense areas (packet list, detail,
  hex dump) use a compact style.

## Development

- Rust is pinned in `gui/rust-toolchain.toml`. egui 0.37 changed many APIs
  (`App::ui`, `Panel`, `show` instead of `show_inside`, `Id::unique`); older
  examples do not apply as-is.
- `make gui-run` builds the CLI into `out/gui/` and runs the GUI against it
  (through `MPEG_TS_ANALYZER_CLI`). Checks: `make gui-lint` (rustfmt, clippy
  `-D warnings`) and `make gui-test`. See `docs/DEVELOPMENT.md`.
- The GUI runs on the host; a container development environment is not
  planned for now.

## Distribution (planned)

| OS | Format |
|---|---|
| macOS | `.app` in a `.dmg`; Homebrew cask later |
| Linux | `.deb` and AppImage, built on an older Ubuntu for glibc compatibility |
| Windows | NSIS installer (per-user, no admin) and a portable zip; VC++ runtime linked statically |

- Packaged with cargo-packager from a `gui-release.yml` workflow on `gui-v*`
  tags. `gui/v*` is avoided because Go reads it as a sub-module version.
- Binaries are not signed for now; the README will explain how to open them on
  macOS and Windows.

## Decisions

| Decision | Why | Rejected |
|---|---|---|
| egui (eframe) | Immediate-mode UI suited to analysis tools; good plotting; single binary | Qt (dated), Electron/Tauri (web stack), browser/WASM build (hard to get running) |
| CLI as a subprocess, JSON over stdout | One implementation of the analysis; the JSON schema is the boundary; easy cross-builds | cgo/c-shared FFI |
| GUI in this repository under `gui/` | Schema and GUI change in one PR; bundled CLI from the same commit | Separate repository |
| cargo-packager | Covers `.app`/`.dmg`/`.deb`/AppImage/NSIS/MSI from one config | cargo-dist (CLI oriented) |
| No code signing for now | Personal OSS; users are engineers | Paid signing services until requested |
| One scan of a byte range for Overview/Timeline/Bitrate | Results are small (summaries, time series) and bounded by the range; no paging inside these screens | Calling the CLI per screen; paging Timeline/Bitrate |
| No lead-in, no pre-scan | The range start is the only place PSI is missing; simpler for both GUI and CLI | Reading ahead of the range; analyzing before Start |
| Byte-based range, no time input | Maps directly to `--offset`/`--limit`; time needs a bitrate estimate | Time-based range |
| Packets paged on demand | Per-packet data for a large range does not fit comfortably in memory | Holding all packets of the range |
| Program selection inside Overview/Timeline/Bitrate | Those results are per program; Packets is per packet | Global program selector in the top bar |
| Lines per PID, not per role | Shows what is actually in the stream; PID changes need no special handling | Merging PIDs by role (video, audio 1) |
| Packets: one flat list | The PES view had no clear use; unit info is in the Unit column and detail pane | Separate PES/section list; lanes connecting packets of a unit |
| English UI only for now | Most text is standard field names and values that are not translated | Fluent-based i18n (kept as a later option) |
| No menu bar | Every action is reachable from the screens; a menu only duplicated them | File/View/Analyze/Tools/Help menus |
| Modal progress dialog during analysis | Results of every analysis screen are being replaced; one clear place to wait and cancel | Per-screen overlays while other screens stay usable |

## Open questions

- Japanese file names: bundle a CJK font or load OS fonts at runtime (#98).
- Multi-program and PMT changes need CLI support (see
  [cli-interface.md](cli-interface.md)).
- Service names from the SDT (#92); ARIB text for Japanese broadcasts is not
  part of the first release.
- Application identifier and display name before the first release.
- Renderer choice (wgpu or glow) after testing on older GPUs, virtual machines
  and remote desktops.
