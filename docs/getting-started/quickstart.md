# Quick start

[Documentation home](../index.md)

## Command line

Analyze a file. The result is a verdict for each timing check:

```bash
mpeg-ts-analyzer sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
```

```text
-----------------------------
Compliance Check Results:
Max PCR interval: 80.000000ms [OK, limit: <= 100.000000ms]
PCR discontinuities: 0 undeclared in 60 PCRs [OK]
PCR-PTS/DTS max gap: 726.666667ms [OK, limit: <= 1000.000000ms]
```

A stream with several programs is listed instead; pick one with `--program`:

```bash
mpeg-ts-analyzer capture.ts --list-programs
mpeg-ts-analyzer capture.ts --program 1
```

For CI or another program, ask for JSON, and make an `NG` verdict fail the
command:

```bash
mpeg-ts-analyzer input.ts --format json --fail-on-error
```

To fail on other transport-health findings too, select them with `--fail-on`
(`any` selects every category):

```bash
mpeg-ts-analyzer input.ts --format json --fail-on timing,continuity,psi_health
mpeg-ts-analyzer input.ts --fail-on any
```

Next: [CLI usage](../cli/usage.md) explains every flag, the verdicts and the
exit codes; [Machine-readable output](../reference/json-output.md) describes
the JSON.

## Desktop GUI

1. Install a GUI package from the
   [releases page](https://github.com/small-teton/mpeg-ts-analyzer/releases)
   ([Installation](installation.md#desktop-gui)). It bundles the CLI.
2. Open a TS or M2TS file with **Open…**, or drop it on the window.
3. Choose the scan range (the first 64 MiB by default) and press **Start**.
   Or start the GUI with the file and range as arguments, for example
   `mpeg-ts-analyzer-gui capture.ts --offset 0 --limit 67108864`, to analyze
   right away.
4. Switch between Overview, Timeline, Bitrate and Packets; they all show the
   same analysis.

Next: [Desktop GUI](../gui/overview.md).
