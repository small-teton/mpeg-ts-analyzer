# Desktop GUI

[Documentation home](../index.md)

A desktop GUI is under active development in [`gui/`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/gui). It runs the CLI
as a subprocess and connects the Overview, Timeline, Bitrate, and Packets views
to the same analysis results.

Packages for macOS, Linux and Windows are described in
[Installation](../getting-started/installation.md#desktop-gui).

[![The four GUI views: Overview and Timeline on the top row, Bitrate and Packets on the bottom row](../images/gui-tabs.png)](../images/gui-tabs.png)

- **Overview** brings the detected program layout, stream properties, the
  transport-health checks (PAT/PMT structure, timing, PES timestamps,
  continuity, transport errors and scrambling, TS synchronization), and
  per-stream bitrate into one screen, with a full-page explanation of every
  check.
- **Timeline** plots the PCR interval and the PCR → PTS/DTS gap of every
  elementary stream against their limits, with continuity and PAT/PMT events.
- **Bitrate** shows the bitrate of each PID over the range, the PCR jitter and
  its histogram.
- **Packets** pages through the packets of the range and decodes the selected
  one, with the PES or section it belongs to.

## Read the Overview verdicts

Overview shows a **Transport health** table for the selected program. Each
row is one check with the value and the verdict the CLI reported (`OK`, `NG`,
`WARNING`, `ANOMALIES`, `ERRORS`, `AFFECTED`, `DATA EXCLUDED`, `RECOVERED`,
`LOST`, `NOT ASSESSABLE`, `INSUFFICIENT DATA` or `SKIPPED`), grouped
under categories that are always expanded:

| Category | Check | Applies to | Verdicts |
|---|---|---|---|
| Structure | PAT health | whole multiplex (PID 0x0000), every packet of the range | `OK` when the PAT repeats at least every 500 ms (ETSI TR 101 290 1.3.a) and every section is usable; `NG` for a late, missing or unusable PAT; `INSUFFICIENT DATA` when the range does not show it repeating |
| Structure | PMT health | selected program's PMT | `OK`, `NG` or `INSUFFICIENT DATA` as for the PAT (TR 101 290 1.5.a) |
| Timing | Max PCR interval | selected program (its PCR PID) | `OK` up to 100 ms, `NG` above |
| Timing | PCR jitter estimate | selected program (its PCR PID) | `OK` within ±500 ns, `WARNING` within ±25 µs, `NG` beyond, when delivery is constant; otherwise `NOT ASSESSABLE` |
| PES / Timestamps | PCR → PTS/DTS gap | selected program | `OK` up to 1000 ms, `NG` above |
| PES / Timestamps | PTS/DTS anomalies | selected program, per PID | `OK` or `ANOMALIES` |
| Integrity | Continuity counter | whole multiplex | `OK` or `ERRORS` |
| Integrity | Transport errors | whole multiplex, every packet of the range | `OK` without `transport_error_indicator`; `AFFECTED` when no marked packet carried data the analysis reads; `DATA EXCLUDED` when one did and its data was left out |
| Integrity | Scrambled packets | whole multiplex, every packet of the range | `OK` without scrambled packets; `AFFECTED` or `DATA EXCLUDED` as for transport errors |
| Integrity | TS synchronization | whole multiplex | `OK`; `RECOVERED` when packets lost their sync byte and the analysis realigned; `LOST` when it could not realign and stopped there |

The table always lists the checks in this order. The verdict is shown exactly
as the CLI reported it; a status this GUI does not know is still shown, with a
tooltip saying so. In a narrow window the check names wrap and the Value,
Status and Help columns keep their width, so every verdict stays visible
without scrolling sideways.

The **ⓘ** button in the Help column opens the check's page inside Overview;
Overview stays selected in the left rail. The button can be reached with Tab
and opened with Enter or Space. The page shows:

- the **current result** — only what the CLI reported for this check, such as
  the value, the limit, the program or PCR PID, the number of observations,
  the first or worst offset, and the reason a check was skipped or not
  assessed; for PCR jitter also the delivery assessment and its evidence; for
  TS synchronization the losses, realignments, skipped bytes, the first loss
  and the largest skipped span with their offsets, and a partial packet at
  the end of the input;
- the check's **findings**, each with **Open packet** when it has an offset
  (for transport errors and scrambled packets, the first affected packet of
  each PID)
  and **Show on timeline** when Timeline marks it (a continuity error on a PID
  of the program);
- the **verdicts**: every status the check can report and the condition behind
  it, with the current one highlighted;
- what the check measures, what it applies to, what the value means, when it
  is skipped and its limitations;
- a button that opens Timeline, Bitrate or Packets with the details.

**Back to Overview** (or the Overview tile) returns to the table at the same
scroll position, with the same program selected. Opening or leaving a page
never runs the analysis again.

PCR jitter is an **estimate**. A file has no packet-arrival timestamps, so the
byte position stands in for time, which is exact only when the stream is
delivered at a constant rate. The CLI therefore first assesses the delivery
from the stream itself — its multiplex rate over 1-second windows must stay
within ±1 % of the average, and no estimated jitter may reach 1 ms — and grades
the estimate only when delivery is `constant`. For a `variable` or `unknown`
delivery, such as a variable-bitrate stream without stuffing or a range shorter
than about two seconds, the row shows `NOT ASSESSABLE` with the estimate as its
value, and the page gives the CLI's reason. The page always gives the delivery
assessment and its evidence, for example `2 × 1 s windows, max deviation
0.00 % of 75.2 kbit/s`. Even a graded estimate is not a direct hardware or
TR 101 290 measurement.

TS synchronization covers every packet from the start of the analysis. When a
packet does not start with the sync byte, the CLI skips ahead to the next
offset where five consecutive packets do and continues there, so later checks
still report results. The skipped bytes are not analyzed, and their packets
usually also show as continuity errors. `LOST` means no new alignment was
found within 1 MiB or before the end of the range, so the analysis stopped
early. Packets pages through the range at the alignment of its start, so
packets after a realignment can appear there as invalid. The sample
`sample_data/sample_188byte_sync_loss.ts` shows a `RECOVERED` result.

**Findings** lists everything that needs attention, and its count includes
every check that is not `OK`: each failed check, each PTS/DTS anomaly kind per
PID, each continuity error (with its offset), PCR jitter, each PID with
transport errors or scrambled packets (with its first offset), a loss of TS
synchronization (at the offset of the first loss, with the number of losses
and skipped bytes, and the CLI's reason when the analysis stopped), a failed
PAT or PMT (with its first problem or late interval), and the
analyzer's warnings. Each row shows the verdict, the check, the reason and whether it
concerns the selected program or the whole multiplex; selecting the check
opens its page. A check counts once through its own events, not again for its
total. A check the CLI skipped, a PCR jitter estimate the CLI found
`not_assessable`, and a PAT or PMT with `insufficient_data`, are listed under **Unavailable analysis** instead, with the
CLI's reason; they are not findings. The GUI shows the CLI's verdicts as they
are and never changes them.

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
