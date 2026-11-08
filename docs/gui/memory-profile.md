# GUI memory profile

How much heap the desktop GUI keeps for an analysis, how to measure it again,
and the measurements behind the decisions in [GUI design](design.md). The GUI
runs the CLI as a child process; this page covers the GUI process only.

## What a completed analysis keeps

An accepted analysis is one `AnalysisSession` (`gui/src/app/state.rs`). When
it is created, each program's raw series (`series` of the analysis JSON) is
taken out of the report and consumed by `plot_data::derive`, one program at a
time, so the raw series and the plot data made from them never stay side by
side.

| Data | Kept | Why |
|---|---|---|
| Summaries, checks, PSI layout and transitions, continuity events, warnings | In the report | Overview, the program selectors and Packets read them |
| Raw PCR, PES, timestamp-event, PCR-jitter and bitrate-window series | No; dropped once the plots of their program exist | Replaced by the plot data below |
| PCR intervals, PCR → PTS/DTS gaps and events, every sample | In the Timeline data | Clicking a plot picks the nearest real sample, with its byte offset |
| Drawn series (at most about 4,000 points per line) | In the Timeline and Bitrate data | Frames draw them without a copy |
| PCR jitter | Drawn form, 30-bin histogram and sample count only | Nothing picks a jitter sample |
| Per-PID and program bitrate steps | Drawn form only | Nothing picks a bitrate window |
| Packet pages | At most the cached pages of Settings (5 by default) | Packets reads the range page by page; it never holds every packet |

Plots are derived for every program when the analysis is accepted, not when a
program is first shown. Deriving a program later would mean keeping its raw
series until then, and those take more memory than the plot data made from
them; deriving all of them takes about 14 ms for the 1.1 GB range below.

## Procedure

The test binary's global allocator (`gui/src/app/heap.rs`) forwards to the
system allocator and counts the process-wide live heap bytes and their peak.
The ignored test `memory_profile` (`gui/src/app/memory_profile.rs`) drives the
app through egui_kittest against the real CLI, without a native window, and
prints the heap above the idle app's after each step. Run it with:

```sh
gui/scripts/memory-profile.sh [<input directory>]
```

The script generates the inputs with ffmpeg the first time (about 1.2 GB,
in `$TMPDIR/mtsa-memory-profile` unless a directory is given; they are never
committed), builds the CLI from this tree into `out/gui/`, and runs the test
alone (`--release`, `--test-threads=1`), since the counters see every thread
of the process:

- `single.ts`: one program (MPEG-2 video 320×240, MPEG-1 Layer II audio) at a
  constant 2 Mbit/s for 4,400 s, 1.1 GB.
- `multi.ts`: two such programs at 2 Mbit/s in all for 400 s, 100 MB.

The scenarios run in this order in one app:

1. The default 64 MiB range of `single.ts` (about 270 s), then Overview,
   Timeline and Bitrate drawn.
2. Packets: 40 pages across the range, each with its first packet decoded.
3. An analysis of the whole file started and cancelled at 50 % progress; the
   previous session stays.
4. A replacement file: `multi.ts` opened (the session is dropped) and its
   default range analyzed, then both programs' screens drawn.
5. A large range: `single.ts` opened again and analyzed whole (1.1 GB, 4,400
   s), its screens drawn, and another file opened.

Each row gives the highest heap while the step ran (Peak) and the heap after
it (Retained), both above the idle app with every screen drawn once, and
what the step added or freed (Change). The numbers are heap bytes requested
from the allocator, not resident memory: allocator slack, code and GPU
memory are not included. The retained heap that outlives a dropped session
(about 2.5 MiB here) is state of egui and the views, such as laid-out text
and plot memory, kept after the screens were shown with data; it does not
grow from one analysis to the next.

The test asserts nothing about the sizes, which depend on the platform, the
CLI and the inputs; ordinary tests check instead that a session no longer
holds the raw series (`session_keeps_plots_instead_of_the_raw_series`).

## Results

Measured on macOS 27.0.1, Apple M2 with 24 GB, Rust 1.99.1, Go 1.27.3 and
ffmpeg 9.0.2, before and after the session consumed the raw series (#154).
Repeated runs agreed within 0.1 MiB.

| Scenario | Peak before | Peak after | Retained before | Retained after |
|---|---:|---:|---:|---:|
| Default 64 MiB range, 1 program | 5.5 MiB | 3.6 MiB | 5.4 MiB | 2.4 MiB |
| + Overview, Timeline, Bitrate drawn | 7.2 MiB | 4.2 MiB | 5.8 MiB | 2.8 MiB |
| + Packets, 40 pages visited | 7.2 MiB | 4.2 MiB | 6.5 MiB | 3.5 MiB |
| + whole file started, cancelled at 50 % | 6.8 MiB | 3.8 MiB | 6.5 MiB | 3.5 MiB |
| Replacement file opened (session dropped) | 6.5 MiB | 3.5 MiB | 2.6 MiB | 2.6 MiB |
| Default 64 MiB range, 2 programs | 8.9 MiB | 6.4 MiB | 8.8 MiB | 4.4 MiB |
| + Overview, Timeline, Bitrate drawn | 9.2 MiB | 4.8 MiB | 8.8 MiB | 4.3 MiB |
| Large file opened (session dropped) | 8.8 MiB | 4.3 MiB | 2.5 MiB | 2.5 MiB |
| Large range (whole file), 1 program | 62.3 MiB | 49.8 MiB | 61.4 MiB | 13.5 MiB |
| + Overview, Timeline, Bitrate drawn | 61.8 MiB | 13.9 MiB | 61.3 MiB | 13.5 MiB |
| Another file opened (session dropped) | 61.3 MiB | 13.5 MiB | 2.9 MiB | 2.9 MiB |

What a session holds, from the heap freed when it is dropped:

| Session | Before | After |
|---|---:|---:|
| Default 64 MiB range, 1 program | 3.9 MiB | 0.9 MiB |
| Default 64 MiB range, 2 programs | 6.3 MiB | 1.8 MiB |
| Large range (whole file), 1 program | 58.4 MiB | 10.5 MiB |

- The peak of an analysis is the parsed report with its raw series, when the
  CLI's JSON has just been read. Deriving the plots adds little to it, since
  each program's raw series are freed as soon as its plots exist. Lowering it
  further would mean deriving the plots while the JSON is parsed.
- Paging adds about 0.7 MiB, bounded by the page cache, and does not grow with
  the pages visited.
- A cancelled analysis keeps nothing: the CLI writes its JSON at the end, and
  progress is coalesced to the latest event.
- Opening another file frees the whole session.

For scale, the CLI process itself (`/usr/bin/time -l`, maximum resident set
size) reached about 45 MiB for the default range of `single.ts`, 77 MiB for
the default range of `multi.ts` and 472 MiB for the whole of `single.ts`.
