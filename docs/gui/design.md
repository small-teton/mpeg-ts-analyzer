# GUI design

Design of the desktop GUI for `mpeg-ts-analyzer`. This document describes the
current design; [decisions](#decisions) records what was chosen and why. The
interface between the GUI and the CLI is in [cli-interface.md](cli-interface.md).

Status: once a range has been analyzed, Overview, Timeline and Bitrate show the
CLI's range analysis and Packets reads the range's packets from the CLI. Before
that every screen says it is not analyzed yet and what to do; no placeholder
data is shown. PAT/PMT changes and packet labels come from the PSI layout of
the range analysis (#93).
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
- The GUI packages bundle a published CLI release, named in
  `gui/BUNDLED_CLI_VERSION`. A GUI change that needs new CLI output is released
  after the CLI release that provides it.

### CLI discovery order

1. `MPEG_TS_ANALYZER_CLI` environment variable, when set and not empty
2. CLI path from Settings, when not empty
3. CLI bundled with the GUI, located relative to the GUI executable: in
   `<prefix>/lib/mpeg-ts-analyzer-gui/` for a GUI in `<prefix>/bin` (`.deb`
   and AppImage), next to it (Windows install directory or portable zip, macOS
   `.app/Contents/MacOS`), in the macOS bundle's `Contents/Resources`, or under
   `$APPDIR/usr/lib/mpeg-ts-analyzer-gui/` in an AppImage; the first file that
   exists is used. The Linux packages keep it out of `/usr/bin`, where the
   CLI's own `.deb` installs `mpeg-ts-analyzer`, and look in the lib directory
   first so that a co-installed CLI does not replace the bundled one.
4. `mpeg-ts-analyzer` on `PATH`

The first step that applies wins; the GUI does not fall through to a later
step when that CLI turns out to be unusable, so a wrong setting is reported
instead of silently replaced. At startup, and whenever the path in Settings
changes, the GUI queries the CLI's capabilities (#89) and reports a missing or
incompatible CLI. Settings shows the CLI in use, which step found it, and its
version.

On Windows the CLI is started with `CREATE_NO_WINDOW`, and the GUI binary uses
`windows_subsystem = "windows"`, so no console window appears.

`mpeg-ts-analyzer-gui --check-cli` runs the same lookup (without the Settings
path), checks the CLI and prints what it found, without opening a window. The
release workflow uses it to check every package after installing or unpacking
it.

## Workflow

1. **Open a file** (Open… or drag and drop). Nothing is analyzed yet: each
   screen shows "Not analyzed yet" with the next step, and the status bar says
   so too. Opening another file returns to this state.
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
- **Start** is filled green while it can be pressed, and so is Analyze in the
  confirmation. Otherwise (already analyzed, no usable file or CLI) it is a
  plain button, with the reason on hover.
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

- Programs are labeled from what the range analysis knows:
  `1 · News · H.264 video + AAC audio · 1.70 Mbps (PMT 0x1000)`.
  The name is the service name from the SDT (`programs[].service`, #92) and is
  left out when the stream has no SDT or the CLI could not decode the name. The
  description comes from PMT stream types; the bitrate is the program's
  average over the range. Overview also shows the service with its provider
  and service type, or the raw bytes of a name that could not be decoded.
- Switching programs does not run the CLI again: the range scan returns every
  program.

### Overview

Stream section first (program tree with PIDs and PCR PID; the PAT changes and
the program's PMT changes in the range, with the PIDs they add, remove or
retype and PMT PID moves; file information), then compliance
check cards, the elementary stream table, and warnings with links to the
packet.

### Timeline

Per program, per PID:

- PCR interval of the program's PCR PID, with the 100 ms limit line.
- PCR → PTS/DTS gap, one line per elementary-stream PID, with the 1000 ms
  limit line. When a PID changes mid-range (PMT update), the old PID's line
  ends and the new one starts; there is no merging by role. A PID's line is
  drawn only where a layout of the program lists it, so a PID that leaves and
  comes back is not joined across the gap. A PID whose stream type changes
  keeps one line, labeled with every type it had (`MPEG-1 audio → MPEG-2
  audio`).
- PAT changes and the program's PMT changes are drawn as vertical dashed lines
  across all plots, at the time of the packet that completed the new table.
  The side panel lists them with what changed; selecting one allows "Open
  packet".
- Event lane (CC errors, PCR NG). A CC error counts for the program where the
  layout in effect at its offset lists the PID as the program's PMT, PCR or
  elementary stream.
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
last one may be shorter than 1 s), the program total without null packets
(it includes PAT and every PMT PID the program used, which are not drawn or
listed on their own: a few
kbit/s next to megabits of audio and video only crowd the plot and legend);
PCR jitter over time and its histogram; table of averages, 1-second peaks and
shares of the program average.
- Like Timeline, the bitrate and PCR jitter plots zoom, scroll and pan along
  time only. Their Y axes are fixed: from 0 to above the highest window for
  bitrate, symmetric around 0 for jitter, computed once per program of an
  analysis (a reset returns to them). The histogram does not zoom or pan.

### Packets

The range is listed packet by packet, **100 packets per page** (Settings). Each
page is fetched from the CLI on demand; only a few pages are cached. Per-packet
data is large, so the whole range is never held in memory.

- Pager: First / previous / `Page n / N` / next / Last, the packet span of the
  page, and the PID filter at the right end.
- **PID filter**: a checkbox per PID found in the range, ordered PAT, PMT,
  elementary streams, null, PID order within each group; All / None.
- **List**: whole rows are selectable. Columns: #, Offset, PID, Type, PUSI, AF
  (PCR), CC, Sections, Notes; every header explains its column on hover.
  - Type comes from the PSI layout in effect at that packet, so a PID shows
    what it carried at that point (`unannounced` where no layout lists it).
  - Sections lists the PAT/PMT sections that start in the packet with their
    version and what the analysis did with that copy: `applied` for the copy
    that made the table take effect, `repeat` for later identical copies, `not
    applied`, or `invalid (reason)`. Other tables show only their table id and
    version.
  - The packet that completed a PAT or PMT change is highlighted, and Notes
    says what changed.
- **Detail pane**:
  - Keeps the width it was dragged to for every packet. A packet whose fields
    are wider than the pane scrolls inside it, horizontally as well as
    vertically, instead of widening the pane over the packet list.
  - "Belongs to": the PES/section, the packet's position in it, first packet,
    PTS/DTS, and First / Previous / Next to move within that unit only
    (switching pages as needed).
  - "This packet": TS header, adaptation field (PCR and interval verdict),
    payload kind, PAT/PMT section fields and tables, PES header fields.
  - Byte inspector: the hex dump colored by layer. Clicking a field (or a
    PAT/PMT table row) highlights its bytes.
  - Bit inspector below it: one row per byte of the selected field, in packet
    order, with bits 7 to 0 (MSB to LSB) and the hex value; only the field's
    bits are marked, using the `bits` ranges of the packet detail (#109). A
    field without `bits` uses every bit of its bytes; a table row marks the bits
    of all its fields. At most 8 bytes are shown, with a note of how many more
    there are. The selection is the field, not its bytes, so fields that share a
    byte (`version_number`, `current_next_indicator`) are selected one at a
    time.
- The GUI keeps the **PSI layout** (where PAT/PMT occur and what they say,
  including mid-range changes) from the range scan, and uses it to label
  packets. Packet pages themselves are read without PSI context: a section in
  a page is matched to its run in the layout by PID, table, header and offset
  ([PSI layout of the range](../reference/json-output.md#psi-layout-of-the-range)). With a CLI that does not report the
  layout, Type comes from the tables the analysis started with.
- Implementation: the first request reads page 1 from the range start and
  learns the first packet boundary; page *n* is then read from
  `first + (n - 1) × 100 × packet size`, with `--packet-size` from the analysis,
  so paging visits every packet of the range once. The current page and the 4
  most recently viewed are kept; a page that is still loading when it drops
  out is cancelled, so paging quickly leaves only the last request running.

## Settings

| Setting | Default | Notes |
|---|---|---|
| Language | System default | UI is English only for now |
| Confirm ranges above | 1024 MiB | Start asks for confirmation above this Limit |
| Packets per page | 100 | |
| Cached pages | 5 | Packet pages kept in memory |
| CLI path | empty | See [CLI discovery order](#cli-discovery-order) |
| Check for updates at startup | On | See [Updates](#updates) |

Settings are saved with eframe's `persistence` feature when the GUI exits and
loaded at startup; fields missing from an older save take their defaults.
Language, packets per page and cached pages are laid out but not applied yet.

## Look and feel

- Fonts: Inter (UI; SemiBold for headings), JetBrains Mono (data), Phosphor
  icons. All bundled under `gui/assets/fonts/` with their licenses.
- Japanese file names and paths use a font the OS ships, added as the last
  fallback of every family: Hiragino Sans (or Hiragino Sans GB) on macOS; Yu Gothic, Meiryo or MS
  Gothic on Windows; Noto Sans CJK or Droid Sans Fallback on Linux, then
  whatever `fc-match sans-serif:lang=ja` returns (run only when none of those
  paths can be read). Only one font is added, so Chinese and Korean text shows
  as far as that font covers it (Noto Sans CJK covers both; Hiragino Sans does
  not cover Hangul). Without a font, Japanese characters show as boxes; the
  rest of the GUI is unaffected.
- Phosphor is registered as its own font family `icons` and always selected
  explicitly (`icons::text`, `icons::font`). As a fallback font its Private Use
  Area codepoints collide with glyphs in other fonts.
- Text and controls are larger than egui's defaults (body 15, heading 22,
  monospace 13.5, control height 26). Data-dense areas (packet list, detail,
  hex dump) use a compact style.

## Development

- Rust is pinned in the repository-root `rust-toolchain.toml`. egui 0.37
  changed many APIs (`App::ui`, `Panel`, `show` instead of `show_inside`,
  `Id::unique`); older examples do not apply as-is.
- `make gui-run` builds the CLI into `out/gui/` and runs the GUI against it
  (through `MPEG_TS_ANALYZER_CLI`). Checks: `make gui-lint` (rustfmt, clippy
  `-D warnings`) and `make gui-test`. See [Development](../development/index.md).
- The GUI runs on the host; a container development environment is not
  planned for now.

## Distribution

| OS | Format |
|---|---|
| macOS (Apple silicon) | `.app` in a `.dmg`; Homebrew cask later |
| Linux (x86_64) | `.deb` (package `mpeg-ts-analyzer-gui`) and AppImage, built on Ubuntu 22.04 for glibc compatibility |
| Windows (x86_64) | NSIS installer (per-user into `%LOCALAPPDATA%\mpeg-ts-analyzer`, no admin) and a portable zip; VC++ runtime linked statically |

- The app is named `mpeg-ts-analyzer` (identifier
  `io.github.small-teton.mpeg-ts-analyzer`); its executable is
  `mpeg-ts-analyzer-gui`. The icon is `gui/assets/icons/icon.svg`.
- Packaged with cargo-packager (configuration in `gui/Cargo.toml`) by the
  `gui-release.yml` workflow, which releases `gui-v<version>` tags; see
  [Release](../development/release.md#gui-release).
  `gui/v*` is avoided because Go reads it as a sub-module version. The GUI
  version (`gui/Cargo.toml`) is independent of the CLI's `VERSION`; GUI
  releases are not marked "latest", which stays on the CLI.
- The CLI release workflow names its current and previous `v*` tags for
  GoReleaser, which would otherwise take a `gui-v*` tag on the same branch as
  the previous release.
- Every package carries `LICENSE`, the bundled fonts' licenses and
  `THIRD-PARTY-NOTICES.txt`, generated at release time by
  `gui/scripts/third-party-notices.sh`: the Rust crates of the GUI for all
  three targets (cargo-about, `gui/about.toml`), including the fonts egui
  embeds, and the Go standard library and the modules recorded in the bundled
  CLI binary of each of the three targets (cobra uses mousetrap only on
  Windows).
- The macOS app is signed ad hoc (`gui/scripts/sign-macos-app.sh`, run by
  cargo-packager before the `.dmg` is made), which seals the bundle but leaves
  the bundled CLI unchanged; without it macOS reports the downloaded app as
  "damaged". Nothing is signed with a Developer ID or notarized; the release notes
  (`.github/gui-release-notes.md`) explain how to open them on macOS and
  Windows. Checksums, an SBOM and build provenance:
  [release-security.md](release-security.md).

## Updates

Status: design only (#150); nothing below is implemented yet.

The GUI can update itself from GitHub Releases. The main reason is macOS: the
app is signed ad hoc, so a `.dmg` downloaded in a browser carries
`com.apple.quarantine` and macOS 15 and later only open it through System
Settings → Privacy & Security → "Open Anyway". An update the GUI downloads
itself is not quarantined (the GUI does not set `LSFileQuarantineEnabled`), so
that detour is needed only for the first install. Windows and Linux users get
the same one-click update instead of picking the right asset by hand.

The bundled CLI is a resource inside each package, so it is updated together
with the app. A CLI installed separately (Homebrew, `go install`, a release
archive) is never touched.

### Finding a release

GUI releases are never marked "latest", so `releases/latest` cannot be used.

1. `GET https://api.github.com/repos/small-teton/mpeg-ts-analyzer/releases`
   (unauthenticated; one request per check, well under the rate limit).
2. Keep published releases whose tag is `gui-vX.Y.Z` (not drafts, not
   prereleases), and take the highest version.
3. If it is not higher than the running version, the GUI is up to date. Lower
   or equal versions are never offered.
4. Otherwise read the release's update manifest asset,
   `mpeg-ts-analyzer-gui_<version>_update.json`, which the
   [release workflow](release-security.md#update-signing) writes in the format
   `cargo-packager-updater` expects (version, notes, and per platform the
   package URL, its signature and its format).

Only `api.github.com` and GitHub's release-download hosts are contacted, over
HTTPS. Nothing about the user or the machine is sent beyond what an HTTP
request carries (the GUI's `User-Agent` names the GUI and its version).

### Checking

- **At startup**, when the setting is on (default), once, in the background.
  A failure is silent apart from a line in the log. When an update exists,
  an "Update available X.Y.Z" button appears in the top bar next to About;
  nothing pops up.
- **Manually**, from a "Check for Updates…" button in the About window. The
  result is shown in the update window: up to date, an update, or the error.

The update window shows the running and new GUI versions, the CLI version the
new release bundles, and a link to the release notes. It offers Install (when
this installation can be updated, see below) or Open release page.

### Installing

`cargo-packager-updater` downloads the package, verifies its signature against
the public key compiled into the GUI, and installs it:

| Installation | How it is detected | Update |
|---|---|---|
| macOS `.app` | Running from `<name>.app/Contents/MacOS` | Replace the bundle with the signed `.app.tar.gz`, then offer to restart |
| Windows NSIS install | Executable in `%LOCALAPPDATA%\mpeg-ts-analyzer` next to the NSIS uninstaller | Run the new installer (passive mode); the GUI exits first |
| Linux AppImage | `APPIMAGE` is set | Replace the AppImage file, then offer to restart |
| Linux `.deb`, Windows portable zip | Anything else | Notify only: show the release page |
| Not writable | macOS app started from the mounted `.dmg` (`/Volumes/…`) or from an App Translocation path, or a failed write test of the install location | Notify only, saying why (for example "move the app to Applications first") |

- Install is disabled while an analysis is running.
- Settings are saved before installing, because the Windows path exits the
  process right after starting the installer.
- If the macOS bundle cannot be moved by the user, the library asks for an
  administrator password through `osascript`; cancelling leaves the old app in
  place.
- A signature that does not verify, or a download that fails, aborts the
  update and leaves the installation unchanged; the window shows the error.

### Not covered

- Updating a separately installed CLI.
- Background downloads, delta updates, or update channels (prereleases).
- Developer ID signing and notarization (still excluded, see
  [release-security.md](release-security.md)).

## Decisions

| Decision | Why | Rejected |
|---|---|---|
| egui (eframe) | Immediate-mode UI suited to analysis tools; good plotting; single binary | Qt (dated), Electron/Tauri (web stack), browser/WASM build (hard to get running) |
| CLI as a subprocess, JSON over stdout | One implementation of the analysis; the JSON schema is the boundary; easy cross-builds | cgo/c-shared FFI |
| GUI in this repository under `gui/` | Schema and GUI change in one PR; the CLI release a GUI bundles is named in the same tree | Separate repository |
| cargo-packager | Covers `.app`/`.dmg`/`.deb`/AppImage/NSIS/MSI from one config | cargo-dist (CLI oriented) |
| No identity-backed code signing or notarization for now (the macOS app is signed ad hoc) | Personal OSS; users are engineers | Developer ID or certificate signing and notarization until requested |
| One scan of a byte range for Overview/Timeline/Bitrate | Results are small (summaries, time series) and bounded by the range; no paging inside these screens | Calling the CLI per screen; paging Timeline/Bitrate |
| No lead-in, no pre-scan | The range start is the only place PSI is missing; simpler for both GUI and CLI | Reading ahead of the range; analyzing before Start |
| Byte-based range, no time input | Maps directly to `--offset`/`--limit`; time needs a bitrate estimate | Time-based range |
| Packets paged on demand | Per-packet data for a large range does not fit comfortably in memory | Holding all packets of the range |
| Program selection inside Overview/Timeline/Bitrate | Those results are per program; Packets is per packet | Global program selector in the top bar |
| Lines per PID, not per role | Shows what is actually in the stream; PID changes need no special handling | Merging PIDs by role (video, audio 1) |
| Packets: one flat list | The PES view had no clear use; unit info is in the detail pane | Separate PES/section list; lanes connecting packets of a unit |
| Packets: no Unit column | Pages carry no unit membership; a position such as `7/48` would need a detail request per row | A Unit column filled from per-packet detail requests |
| English UI only for now | Most text is standard field names and values that are not translated | Fluent-based i18n (kept as a later option) |
| No menu bar | Every action is reachable from the screens; a menu only duplicated them | File/View/Analyze/Tools/Help menus |
| Japanese from an OS font at runtime, by file path (no new dependency) | No size cost; every desktop OS the GUI targets ships one; reading it (about 8 MB for Hiragino W3) adds only a file read at startup | Bundling Noto Sans CJK (10–20 MB, cannot be subset in advance because file names are arbitrary); `fontdb` (another dependency for the same lookup) |
| In-app update with `cargo-packager-updater` (#150) | Avoids the macOS "Open Anyway" detour after the first install; already part of the packaging toolchain; verifies a minisign signature and handles `.app`, NSIS and AppImage | Own downloader and installer; Sparkle (macOS only); only linking to the release page |
| Find updates through the Releases API and a manifest asset of each release | No extra hosting; works with GUI releases that are not "latest" | A fixed-URL manifest on GitHub Pages (the docs workflow owns the Pages deployment) |
| Startup update check on by default, with a setting | Users learn about releases without action; one unauthenticated request | Manual check only |
| Modal progress dialog during analysis | Results of every analysis screen are being replaced; one clear place to wait and cancel | Per-screen overlays while other screens stay usable |

## Open questions

- ARIB text for Japanese broadcasts (ISDB) is not decoded, so those services
  are labeled without a name.
- Renderer choice (wgpu or glow) after testing on older GPUs, virtual machines
  and remote desktops.
- Windows shows the icon on the window and taskbar (set at runtime), but the
  `.exe` file and its shortcuts use the default icon until an icon resource is
  embedded at build time (a build script and one more build dependency).
- Intel Mac packages (only Apple silicon is built).
