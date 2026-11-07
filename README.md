# mpeg-ts-analyzer

[![Go](https://github.com/small-teton/mpeg-ts-analyzer/actions/workflows/go.yml/badge.svg)](https://github.com/small-teton/mpeg-ts-analyzer/actions/workflows/go.yml)
![Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/small-teton/9d60b1e4226ac2926940b20ce3381621/raw/coverage.json)
[![Go Reference](https://pkg.go.dev/badge/github.com/small-teton/mpeg-ts-analyzer/v2.svg)](https://pkg.go.dev/github.com/small-teton/mpeg-ts-analyzer/v2)
[![Release](https://img.shields.io/github/v/release/small-teton/mpeg-ts-analyzer)](https://github.com/small-teton/mpeg-ts-analyzer/releases)
![License](https://img.shields.io/github/license/small-teton/mpeg-ts-analyzer)
[![Mentioned in Awesome Go](https://awesome.re/mentioned-badge.svg)](https://github.com/avelino/awesome-go)

mpeg-ts-analyzer is an MPEG-2 Transport Stream analyzer (ISO/IEC 13818-1) for
188-byte TS and 192-byte M2TS files. It checks transport-layer timing and gives
a direct pass/fail answer, and it decodes every packet down to its fields and
bits.

![mpeg-ts-analyzer terminal demo](docs/demo.gif)

## What it checks

- **Max PCR interval**: no greater than 100 ms (ISO/IEC 13818-1, 2.7.2).
- **PCR → PTS/DTS gap** (decode delay): no greater than 1000 ms.
- **PTS/DTS anomalies**: timestamps going backward, wraparound, large forward
  jumps, or DTS later than PTS.
- **Continuity counters** of every PID, and the **PAT/PMT layout** of the range,
  including tables split into sections and version changes.

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
when a field such as the 13-bit PID spans two bytes:

<p align="center">
  <a href="docs/images/gui-inspectors.png">
    <img src="docs/images/gui-inspectors.png" alt="Byte and Bit inspectors with the 13-bit PID 0x1001 selected across bytes 1 and 2" width="70%">
  </a>
</p>

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
```

```text
Compliance Check Results:
Max PCR interval: 80.000000ms [OK, limit: <= 100.000000ms]
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

mpeg-ts-analyzer is purpose-built for one job: **checking transport-layer timing compliance** — the Max PCR interval and the PCR-PTS/DTS (decode-timeline) gap — and reporting a direct pass/fail answer.

### Compared to other tools

- **ffprobe / FFmpeg** work at the elementary-stream (codec) level. They expose PTS/DTS but **not the PCR**, which lives in the transport layer (adaptation field). PCR interval and PCR-PTS/DTS gap checks are therefore out of reach — ffprobe simply never surfaces the PCR.
- **TSDuck** is an excellent, comprehensive TS toolkit and it *can* obtain these values (e.g. `tsp -P pcrextract` dumps PCR/PTS/DTS to CSV). However, it hands you the raw timestamps — you still have to script the interval/gap computation and make the pass/fail decision yourself. TSDuck gives you the parts; it does not directly give you the answer.

mpeg-ts-analyzer instead gives you:

- **Spec-level field dump** — Every field in TS headers, adaptation fields, PSI tables, and PES headers is printed exactly as defined in ISO/IEC 13818-1, making it easy to cross-reference with the specification.
- **Compliance checks out of the box** — Max PCR interval (≤ 100 ms) and PCR-PTS/DTS gap (≤ 1000 ms) are validated automatically and reported as a direct result. No CSV, no scripting.

### Which tool should I use?

- **Transport-layer timing compliance** (Max PCR interval / PCR-PTS/DTS gap), or a lightweight pass/fail check you can drop into CI → **mpeg-ts-analyzer**. This is what it is built for.
- **Broad, general analysis of a stream** — full PSI/SI tables (NIT/SDT/EIT/…), bitrate breakdown, service names, scrambling state, network information, deep TR 101 290 conformance monitoring → **[TSDuck](https://tsduck.io/)** is the far more capable tool, and we recommend it for that use case.

mpeg-ts-analyzer deliberately stays small and focused on the compliance check rather than duplicating what TSDuck already does well.

### Background

mpeg-ts-analyzer started in 2016 as a small tool for checking timestamp gaps,
written partly to learn Go. To learn MPEG-2 TS itself, it also printed every
header field in detail — the origin of today's spec-level dumps. It has since
grown into a timing-compliance checker with machine-readable output and a
desktop GUI.

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
