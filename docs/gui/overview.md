# Desktop GUI

[Documentation home](../index.md)

A desktop GUI is under active development in [`gui/`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/gui). It runs the CLI
as a subprocess and connects the Overview, Timeline, Bitrate, and Packets views
to the same analysis results.

Packages for macOS, Linux and Windows are described in
[Installation](../getting-started/installation.md#desktop-gui).

[![The four GUI views: Overview and Timeline on the top row, Bitrate and Packets on the bottom row](../images/gui-tabs.png)](../images/gui-tabs.png)

- **Overview** brings the detected program layout, stream properties, timing
  compliance verdicts, continuity errors, PCR jitter, and per-stream bitrate
  into one screen.
- **Timeline** plots the PCR interval and the PCR → PTS/DTS gap of every
  elementary stream against their limits, with continuity and PAT/PMT events.
- **Bitrate** shows the bitrate of each PID over the range, the PCR jitter and
  its histogram.
- **Packets** pages through the packets of the range and decodes the selected
  one, with the PES or section it belongs to.

## Read the Overview verdicts

Overview shows five compliance cards for the selected program. Each card has a
title, the verdict the CLI reported (`OK`, `NG`, `WARNING`, `ANOMALIES`,
`ERRORS` or `SKIPPED`), the value and a short note. The **ⓘ** button next to
the title opens an explanation of the card: what it measures, whether it covers
the selected program or the whole multiplex, what the value means, the
threshold behind every verdict, why it can be skipped, its limitations, and a
link to the Timeline or Bitrate view with the details. The button can be
reached with Tab and opened with Enter or Space; Escape, the close button or a
click outside the window closes it. Opening it does not change the selected
program or the analysis.

| Card | Scope | Verdicts |
|---|---|---|
| Max PCR interval | selected program (its PCR PID) | `OK` up to 100 ms, `NG` above |
| PCR → PTS/DTS gap | selected program | `OK` up to 1000 ms, `NG` above |
| PTS/DTS anomalies | selected program, per PID | `OK` or `ANOMALIES` |
| Continuity counter | whole multiplex | `OK` or `ERRORS` |
| PCR jitter | selected program (its PCR PID) | `OK` within ±500 ns, `WARNING` within ±25 µs, `NG` beyond |

PCR jitter is an **estimate**. A file has no packet-arrival timestamps, so the
byte position stands in for time, assuming the stream is delivered at an
approximately constant rate. It is not a direct hardware or TR 101 290
measurement: a variable-bitrate stream can show milliseconds of estimated
jitter, and an `NG`, even when its PCRs were accurate on the wire. The card's
note says `max |estimated jitter|`.

The cards wrap to more rows as the window narrows, keeping their order, so no
verdict is hidden off screen.

**Findings** lists everything that needs attention, and its count includes
every card that is not `OK`: each failed check, each PTS/DTS anomaly kind per
PID, each continuity error (with its offset), PCR jitter marked as an estimate,
and the analyzer's warnings. Each row shows the verdict, the metric, the reason
and whether it concerns the selected program or the whole multiplex; selecting
the metric opens its explanation. A card counts once through its own events,
not again for its total. A check the CLI skipped is listed under **Unavailable
analysis** instead, with the CLI's reason. The GUI shows the CLI's verdicts as
they are and never changes them.

To see every verdict at once, open the intentionally invalid
[diagnostic sample](../development/sample-data.md#diagnostic-sample-with-every-overview-finding):

```sh
make gui-run GUI_ARGS='sample_data/sample_188byte_all_findings.ts'
```

## Open a file from the command line

The GUI normally opens with a file picker. For repeatable inspection, pass a
file and optional byte range to open it and start analysis immediately:

```sh
mpeg-ts-analyzer-gui capture.ts --offset 0 --limit 67108864
```

`--limit 0` analyzes from the offset through the end of the file. Development
builds accept the same arguments:

```sh
make gui-run GUI_ARGS='capture.ts --offset 0 --limit 67108864'
```

## Inspect packets down to individual bits

Select any decoded TS, adaptation-field, PSI, or PES field to highlight both
its source bytes and its exact bit range. Multi-byte, non-byte-aligned fields
are shown across byte boundaries; the 13-bit PID in the packet view spans the
low five bits of one byte and all eight bits of the next byte.

[![Byte and Bit inspectors with the 13-bit PID 0x1001 selected across bytes 1 and 2](../images/gui-inspectors.png)](../images/gui-inspectors.png)

The Bit inspector follows packet/file order from top to bottom and displays
each byte from MSB (`bit 7`) to LSB (`bit 0`), making it straightforward to
compare raw packet data with ISO/IEC 13818-1 field layouts. The close-up above
is the same Byte and Bit inspector shown in Packets: the PID `0x1001` of a PMT
packet is selected, so its bytes are highlighted and its 13 bits are marked
across the two bytes they span.

Both images are rendered from fixtures by the GUI's tests rather than captured
by hand; see [README screenshots](../development/index.md#readme-screenshots).
