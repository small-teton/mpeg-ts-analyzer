# mpeg-ts-analyzer

[![Go](https://github.com/small-teton/mpeg-ts-analyzer/actions/workflows/go.yml/badge.svg)](https://github.com/small-teton/mpeg-ts-analyzer/actions/workflows/go.yml)
![Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/small-teton/9d60b1e4226ac2926940b20ce3381621/raw/coverage.json)
[![Go Reference](https://pkg.go.dev/badge/github.com/small-teton/mpeg-ts-analyzer/v2.svg)](https://pkg.go.dev/github.com/small-teton/mpeg-ts-analyzer/v2)
[![Release](https://img.shields.io/github/v/release/small-teton/mpeg-ts-analyzer)](https://github.com/small-teton/mpeg-ts-analyzer/releases)
![License](https://img.shields.io/github/license/small-teton/mpeg-ts-analyzer)
[![Mentioned in Awesome Go](https://awesome.re/mentioned-badge.svg)](https://github.com/avelino/awesome-go)

mpeg-ts-analyzer is an MPEG-2 Transport Stream analyzer (ISO/IEC 13818-1) for
188-byte TS, 192-byte M2TS and 204-byte TS (with a 16-byte trailer) files. It checks transport-layer timing and gives
a direct pass/fail answer, and it decodes every packet down to its fields and
bits. In the desktop GUI, [Detailed inspection](#desktop-gui) shows every bit
of a packet with its role, and checks the fixed bits against the
specification.

![mpeg-ts-analyzer terminal demo](docs/demo.gif)

## What it checks

- **Max PCR interval**: no greater than 100 ms (ISO/IEC 13818-1, 2.7.2).
- **PCR discontinuities**: a PCR that jumps backward, or more than 1000 ms
  forward, without `discontinuity_indicator` (ISO/IEC 13818-1, 2.4.3.5);
  declared discontinuities, `PCR_PID` changes and wraparounds are listed.
- **PCR → PTS/DTS gap** (decode delay): no greater than 1000 ms.
- **PTS/DTS anomalies**: timestamps going backward, wraparound, large forward
  jumps, or DTS later than PTS.
- **Continuity counters** of every PID, and the **PAT/PMT layout** of the range,
  including tables split into sections and version changes.
- **TS synchronization**: packets without their sync byte, with byte offsets,
  and bounded realignment so that later results are still reported.
- **Transport errors and scrambled packets** per PID: affected packets are
  counted; the PSI/PES payload of both is kept out of the analysis, and so is
  the PCR of a packet marked with `transport_error_indicator`, while a
  scrambled packet's PCR (in its clear adaptation field) is still used. Only
  the 4-byte header of a marked packet is read, so one with a malformed
  adaptation field is counted instead of stopping the analysis.
- **PAT/PMT health**: whether the PAT and the PMT of every announced program
  were found, repeat at least every 500 ms (the ETSI TR 101 290 1.3.a/1.5.a
  value, timed by PCR), and arrive with valid CRC, syntax and table_id.
- **PID references**: PMT, PCR and elementary-stream PIDs that the PAT/PMT
  reference but that are absent or silent for more than 5 s, and PIDs that
  carry packets without being referenced (after a 0.5 s grace period).
- **PES headers** of every elementary stream (truncated, bad start code,
  flags, lengths or PTS/DTS marker bits), and **PTS repetition**: a PTS at
  least every 700 ms on video and audio streams (ISO/IEC 13818-1, 2.7.4).
- **Packet gaps** (report only): for every PID, the longest run of other
  packets between two of its packets, in packets and estimated milliseconds,
  to spot poorly interleaved (bursty) multiplexes. No verdict is attached.
- **Mux lead** (report only, no verdict): how far ahead of its DTS/PTS each
  elementary stream is multiplexed (minimum, maximum and spread per PID, with
  offsets), and the largest audio/video lead difference of each program.

Results are text for people, or JSON with a published schema for CI and other
programs. See [CLI usage][docs-cli-usage].

## Desktop GUI

The desktop GUI runs the CLI and shows one analysis in four linked views:
Overview, Timeline, Bitrate and Packets.

<p align="center">
  <a href="docs/images/gui-tabs.png">
    <img src="docs/images/gui-tabs.png" alt="The four GUI views: Overview and Timeline on the top row, Bitrate and Packets on the bottom row" width="100%">
  </a>
</p>

Select any decoded field of a packet to see its bytes and its exact bits, even
when a field such as the 13-bit PID spans two bytes. **Detailed inspection**
then opens the packet's complete wire layout on a full-width page:

<table>
  <tr>
    <td width="36%">
      <a href="docs/images/gui-byte-inspector.png"><img src="docs/images/gui-byte-inspector.png" alt="Byte inspector with the bytes 1 and 2 of the PID 4097 (0x1001) selected"></a>
    </td>
    <td width="64%" rowspan="2">
      <a href="docs/images/gui-wire-layout.png"><img src="docs/images/gui-wire-layout.png" alt="Detailed inspection of a PMT packet with its 13-bit PID selected: the PMT badge, the field list, the selected part's facts and every bit of the packet colored by role"></a>
    </td>
  </tr>
  <tr>
    <td>
      <a href="docs/images/gui-bit-inspector.png"><img src="docs/images/gui-bit-inspector.png" alt="Bit inspector with the 13 bits of the PID 4097 (0x1001) marked across bytes 1 and 2"></a>
    </td>
  </tr>
</table>

<p align="center"><em>Left: the Byte and Bit inspectors of the Packets view. Right: Detailed inspection of the same PMT packet with its PID selected.</em></p>

Detailed inspection lays a packet out bit by bit, the way ISO/IEC 13818-1
draws its syntax, so you can read a real packet against the specification:

- **Every bit has a role.** Each bit of the packet belongs to one part,
  shown in a colored grid: decoded values and flags, and also reserved bits,
  marker bits, prefixes, stuffing and bytes the analyzer does not decode.
- **Fixed bits are checked.** Bits whose value the specification fixes (sync
  byte, start code, reserved and marker bits, the `'10'` and PTS/DTS
  prefixes, stuffing bytes) are marked ✓ or flagged in red with the expected
  value, and the rule is cited.
- **Select a field, see its bits.** A field's exact bits are highlighted,
  with its value in decimal and hexadecimal, its packet bit and byte/bit
  position, and the structure it belongs to.
- **188, 192 and 204-byte packets** straight from a `.ts`/`.m2ts` file: the
  M2TS header and the 204-byte trailer are part of the layout. The GUI is
  open source, for macOS, Linux and Windows.

See the [GUI guide][docs-gui].

## Install

```bash
brew install small-teton/tap/mpeg-ts-analyzer        # CLI, macOS / Linux
go install github.com/small-teton/mpeg-ts-analyzer/v2@latest
```

Linux packages, pre-built binaries and the GUI packages for macOS, Linux and
Windows are on the [releases page](https://github.com/small-teton/mpeg-ts-analyzer/releases);
see [Installation][docs-installation].

## Quick start

```bash
mpeg-ts-analyzer input.ts                  # timing compliance verdicts
mpeg-ts-analyzer input.ts --format json    # the same, as one JSON document
mpeg-ts-analyzer input.ts --fail-on any    # CI: exit 2 on any transport-health finding
```

```text
Compliance Check Results:
Max PCR interval: 80.000000ms [OK, limit: <= 100.000000ms]
PCR discontinuities: 0 undeclared in 60 PCRs [OK]
PCR-PTS/DTS max gap: 726.666667ms [OK, limit: <= 1000.000000ms]
```

In the GUI, open a file (or drop it on the window), choose the scan range and
press **Start**. More in the [Quick start][docs-quickstart].

## Documentation

The [documentation site][docs-site] has every guide, with navigation and
search. Its pages are the Markdown under [`docs/`](docs/), which also reads on
GitHub.

- [Installation][docs-installation] and [Quick start][docs-quickstart]
- [CLI usage][docs-cli-usage], [PSI tables][docs-psi] and
  [dump output examples][docs-dumps]
- [Desktop GUI][docs-gui]
- [Machine-readable output][docs-json-output] and the [JSON schema][docs-json-schema]
- [Development][docs-development], [release][docs-release] and
  [security policy](SECURITY.md)

## Why mpeg-ts-analyzer?

mpeg-ts-analyzer combines transport-stream checks with detailed inspection. It
reports timing, continuity and PSI/PES findings, plots timing and bitrate, and
lets you follow a finding down to the exact packet, field, byte and bit.

Its main strength is not continuous monitoring or exhaustive validation of an
entire long-running service. Instead, you can select a bounded range around the
behavior you want to understand and examine it from a high-level verdict down
to its binary representation. This makes the tool particularly useful for
validating muxer output, investigating a suspicious section of a stream, and
learning how MPEG-2 TS structures are encoded.

### Compared to other tools

- **ffprobe / FFmpeg** are a natural choice for codecs, elementary streams and
  playback-oriented metadata. They do not provide the same packet-field-to-bit
  inspection workflow for the MPEG-2 TS transport layer.
- **TSDuck** is an excellent, comprehensive TS toolkit for broad stream
  analysis, processing, monitoring and deep conformance work. Use it when you
  need that breadth, especially across a complete or live service.

mpeg-ts-analyzer instead gives you:

- **A focused range** — Use a byte offset and limit to isolate the section
  you want to verify instead of starting with the entire stream.
- **Checks and context together** — See transport findings alongside timing,
  bitrate, PSI/PES and packet data rather than receiving only a verdict.
- **Spec-level inspection** — Decode TS headers, adaptation fields, PSI tables
  and PES headers, then map a selected field to its exact bytes and bits.
- **Every bit accounted for** — The GUI's Detailed inspection gives each bit
  of a packet its role, including reserved, marker, prefix, stuffing and
  undecoded bits, and checks the fixed ones against ISO/IEC 13818-1.

### Which tool should I use?

- To verify or learn from a specific range, correlate transport checks with the
  timeline, or inspect a field down to its bits → **mpeg-ts-analyzer**.
- For continuous or whole-service monitoring, stream processing, complete
  PSI/SI coverage, or deep TR 101 290 conformance work →
  **[TSDuck](https://tsduck.io/)**.
- For codec and elementary-stream inspection → **ffprobe / FFmpeg**.

### Background

mpeg-ts-analyzer started in 2016 as a small tool for checking timestamp gaps,
written partly to learn Go. To learn MPEG-2 TS itself, it also printed every
header field in detail — the origin of today's spec-level dumps. It has since
grown to include transport checks, machine-readable output and a desktop GUI,
while keeping that original emphasis on understanding a selected part of a
stream in detail.

**Note:** The accuracy of the output is not guaranteed.

## License

Apache License 2.0; see [LICENSE](LICENSE).

[docs-cli-usage]: https://small-teton.github.io/mpeg-ts-analyzer/cli/usage/
[docs-gui]: https://small-teton.github.io/mpeg-ts-analyzer/gui/overview/
[docs-installation]: https://small-teton.github.io/mpeg-ts-analyzer/getting-started/installation/
[docs-quickstart]: https://small-teton.github.io/mpeg-ts-analyzer/getting-started/quickstart/
[docs-site]: https://small-teton.github.io/mpeg-ts-analyzer/
[docs-psi]: https://small-teton.github.io/mpeg-ts-analyzer/cli/psi/
[docs-dumps]: https://small-teton.github.io/mpeg-ts-analyzer/cli/dumps/
[docs-json-output]: https://small-teton.github.io/mpeg-ts-analyzer/reference/json-output/
[docs-json-schema]: https://small-teton.github.io/mpeg-ts-analyzer/json-schema-v2.json
[docs-development]: https://small-teton.github.io/mpeg-ts-analyzer/development/
[docs-release]: https://small-teton.github.io/mpeg-ts-analyzer/development/release/
