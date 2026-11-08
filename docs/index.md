# mpeg-ts-analyzer documentation

mpeg-ts-analyzer checks the transport-layer timing of MPEG-2 Transport Streams
(ISO/IEC 13818-1) and decodes their packets, from the command line or in a
desktop GUI. The [README](https://github.com/small-teton/mpeg-ts-analyzer#readme) gives a short overview.

## Get started

- [Installation](getting-started/installation.md): Homebrew, Linux packages,
  binaries, `go install`, and the GUI packages.
- [Quick start](getting-started/quickstart.md): analyze a file from the command
  line or in the GUI.

## Use the command-line analyzer

- [CLI usage](cli/usage.md): what is checked, every flag, verdicts and exit
  codes, multi-program streams, continuity, TS synchronization and timestamp
  reports.
- [PSI tables](cli/psi.md): PAT and PMT split into sections, service names from
  the SDT, and PAT/PMT version changes during an analysis.
- [Dump output examples](cli/dumps.md): annotated output of the dump flags.

## Use the desktop GUI

- [Desktop GUI](gui/overview.md): the Overview, Timeline, Bitrate and Packets
  views, the Byte and Bit inspectors, and Detailed inspection: every bit of a
  packet with its role (value, flag, reserved, marker, prefix, stuffing, not
  decoded), fixed bits checked against ISO/IEC 13818-1 with the rule cited.

## Reference

- [Machine-readable output](reference/json-output.md): the JSON summary, range
  analysis, PSI layout, packet pages and packet detail, progress events and
  capabilities.
- JSON schemas: [summary](json-schema-v2.json),
  [range analysis](json-schema-analysis-v2.json),
  [packets](json-schema-packets-v1.json).

## Develop and release

These pages are for contributors.

- [Development](development/index.md): environment, tests and coverage, the
  GUI, dependency checks and the README demo.
- [Architecture](development/architecture.md): the Go packages, the boundary
  between parsing and analysis, and how to add a transport check.
- [Fuzz testing](development/fuzzing.md)
- [Sample data](development/sample-data.md): how the streams in `sample_data/`
  were made.
- [Release](development/release.md): the CLI release workflow.
- GUI design notes: [design](gui/design.md), [CLI interface](gui/cli-interface.md)
  and [release security](gui/release-security.md).
- [README demo](demo/README.md): how `docs/demo.gif` is rendered.
