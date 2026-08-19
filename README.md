# mpeg-ts-analyzer

[![Go](https://github.com/small-teton/mpeg-ts-analyzer/actions/workflows/go.yml/badge.svg)](https://github.com/small-teton/mpeg-ts-analyzer/actions/workflows/go.yml)
![Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/small-teton/9d60b1e4226ac2926940b20ce3381621/raw/coverage.json)
[![Go Reference](https://pkg.go.dev/badge/github.com/small-teton/mpeg-ts-analyzer/v2.svg)](https://pkg.go.dev/github.com/small-teton/mpeg-ts-analyzer/v2)
[![Release](https://img.shields.io/github/v/release/small-teton/mpeg-ts-analyzer)](https://github.com/small-teton/mpeg-ts-analyzer/releases)
![License](https://img.shields.io/github/license/small-teton/mpeg-ts-analyzer)
[![Mentioned in Awesome Go](https://awesome.re/mentioned-badge.svg)](https://github.com/avelino/awesome-go)

mpeg-ts-analyzer is an MPEG-2 Transport Stream analyzer (ISO/IEC 13818-1).

It parses TS packets and checks whether the stream conforms to the following requirements defined in the specification:

- **Max PCR interval** should be no greater than 100 ms (ISO/IEC 13818-1, Section 2.7.2)
- **PCR-PTS max gap** (end-to-end delay) should be no greater than 1000 ms

In addition, it can dump various MPEG-2 TS internal structures for stream investigation:

- TS header and payload
- Adaptation field (including PCR)
- PSI tables (PAT/PMT) with CRC32 validation
- PMT ES info descriptors (ISO 639 language, registration, AVC/HEVC video, AAC audio, teletext, DVB subtitling)
- PES header with PTS/DTS timestamps
- PCR jitter analysis (per-interval deviation from the expected PCR)
- `continuity_counter` validation

Both 188-byte TS packets and 192-byte M2TS packets (BDAV format with TP_extra_header) are supported. The packet size is auto-detected from the stream.

**Note:** The accuracy of the output is not guaranteed.

# Install

## Homebrew (macOS / Linux)

```bash
brew install small-teton/tap/mpeg-ts-analyzer
```

## Linux packages (deb / rpm / apk)

Download `.deb`, `.rpm`, or `.apk` packages from the [Releases](https://github.com/small-teton/mpeg-ts-analyzer/releases) page.

```bash
# Debian / Ubuntu
curl -LO https://github.com/small-teton/mpeg-ts-analyzer/releases/latest/download/mpeg-ts-analyzer_<version>_linux_amd64.deb
sudo dpkg -i mpeg-ts-analyzer_<version>_linux_amd64.deb

# RHEL / CentOS / Fedora
curl -LO https://github.com/small-teton/mpeg-ts-analyzer/releases/latest/download/mpeg-ts-analyzer_<version>_linux_amd64.rpm
sudo rpm -i mpeg-ts-analyzer_<version>_linux_amd64.rpm

# Alpine
curl -LO https://github.com/small-teton/mpeg-ts-analyzer/releases/latest/download/mpeg-ts-analyzer_<version>_linux_amd64.apk
sudo apk add --allow-untrusted mpeg-ts-analyzer_<version>_linux_amd64.apk
```

## Pre-built binaries

Download from the [Releases](https://github.com/small-teton/mpeg-ts-analyzer/releases) page. No additional tools are required.

## Install with Go

If you have a [Go](https://go.dev/dl/) environment (1.26+):

```bash
go install github.com/small-teton/mpeg-ts-analyzer/v2@latest
```

# Usage

By default, mpeg-ts-analyzer dumps all timestamps (PCR/PTS/DTS), including the PCR interval and PCR-PTS gap. To dump more details, add the corresponding command-line flags.

```
Usage:
  mpeg-ts-analyzer [input file path] [flags]

Flags:
      --dump-adaptation-field   Dump TS packet adaptation_field detail.
      --dump-bitrate            Summarize per-PID average/peak bitrate (PCR time base) for the analyzed program.
      --dump-pcr-jitter         Analyze PCR jitter (per-interval deviation from the expected PCR).
      --dump-pes-header         Dump PES packet header detail.
      --dump-psi                Dump PSI(PAT/PMT) detail.
      --dump-timestamp          Dump PCR/PTS/DTS timestamps.
      --dump-ts-header          Dump TS packet header.
      --dump-ts-payload         Dump TS packet payload binary.
  -h, --help                    help for mpeg-ts-analyzer
      --limit int               Stop reading after this many bytes (0 = no limit).
      --list-programs           List every program (program_number, PMT PID, elementary streams) and exit.
      --offset int              Start reading from this byte offset.
      --program int             Analyze only this program_number (default: the sole program; a multi-program stream is listed instead).
      --version                 show mpeg-ts-analyzer version.
```

## Multi-program transport streams

A single-program stream (a recording or an extracted service) is analyzed
directly. A **multi-program** stream — e.g. a raw broadcast capture (ISDB-T
carries a main service plus 1seg; BS/CS muxes carry many channels) — is not
analyzed program-by-program by default: the tool lists the programs and stops,
so you can pick one.

```
$ ./mpeg-ts-analyzer capture.ts
Detected PAT: 2 program(s)
Multiple programs detected; pass --program <program_number> to analyze one:
Program 1: PMT PID 0x1000, PCR PID 0x0100
PMT : Program Info : elementary_PID     : 0x100, stream_type : 0x1b (AVC video ...)
PMT : Program Info : elementary_PID     : 0x110, stream_type : 0x0f (AAC audio ...)
Program 2: PMT PID 0x1001, PCR PID 0x0110
PMT : Program Info : elementary_PID     : 0x120, stream_type : 0x02 (1seg video ...)

$ ./mpeg-ts-analyzer capture.ts --program 1     # analyze one program fully
$ ./mpeg-ts-analyzer capture.ts --list-programs # list only the programs, even when there is just one
```

Listing only parses each program's PMT (no PES analysis), so it stays lightweight.

**Tip:** For large files, dump options can produce a huge amount of output. Use `--limit` to restrict the byte range, or redirect output to a file to avoid losing the beginning of the output in your terminal scrollback:

```bash
mpeg-ts-analyzer large.ts --dump-ts-header --limit 1000000 > dump.txt
```

# Options & output examples

Every flag has a full, annotated output example — TS header, PSI, timestamps,
PCR jitter, and per-PID bitrate — in **[OPTIONS.md](OPTIONS.md)**.

# Why mpeg-ts-analyzer?

mpeg-ts-analyzer is purpose-built for one job: **checking transport-layer timing compliance** — the Max PCR interval and the PCR-PTS (end-to-end) gap — and reporting a direct pass/fail answer.

## Compared to other tools

- **ffprobe / FFmpeg** work at the elementary-stream (codec) level. They expose PTS/DTS but **not the PCR**, which lives in the transport layer (adaptation field). PCR interval and PCR-PTS gap checks are therefore out of reach — ffprobe simply never surfaces the PCR.
- **TSDuck** is an excellent, comprehensive TS toolkit and it *can* obtain these values (e.g. `tsp -P pcrextract` dumps PCR/PTS/DTS to CSV). However, it hands you the raw timestamps — you still have to script the interval/gap computation and make the pass/fail decision yourself. TSDuck gives you the parts; it does not directly give you the answer.

mpeg-ts-analyzer instead gives you:

- **Spec-level field dump** — Every field in TS headers, adaptation fields, PSI tables, and PES headers is printed exactly as defined in ISO/IEC 13818-1, making it easy to cross-reference with the specification.
- **Compliance checks out of the box** — Max PCR interval (≤ 100 ms) and PCR-PTS gap (≤ 1000 ms) are validated automatically and reported as a direct result. No CSV, no scripting.

## Which tool should I use?

- **Transport-layer timing compliance** (Max PCR interval / PCR-PTS gap), or a lightweight pass/fail check you can drop into CI → **mpeg-ts-analyzer**. This is what it is built for.
- **Broad, general analysis of a stream** — full PSI/SI tables (NIT/SDT/EIT/…), bitrate breakdown, service names, scrambling state, network information, deep TR 101 290 conformance monitoring → **[TSDuck](https://tsduck.io/)** is the far more capable tool, and we recommend it for that use case.

mpeg-ts-analyzer deliberately stays small and focused on the compliance check rather than duplicating what TSDuck already does well.

Sample TS files are included in `sample_data/` for quick testing:

```bash
# 188-byte TS
ffmpeg -f lavfi -i "color=c=blue:s=320x240:d=5,format=yuv420p" \
       -f lavfi -i "anullsrc=r=48000:cl=stereo" \
       -t 5 -c:v mpeg2video -c:a mp2 -metadata:s:a:0 language=eng \
       -f mpegts sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts

# 192-byte M2TS
ffmpeg -f lavfi -i "color=c=red:s=320x240:d=2,format=yuv420p" \
       -f lavfi -i "anullsrc=r=48000:cl=stereo" \
       -t 2 -c:v mpeg2video -c:a mp2 -metadata:s:a:0 language=eng \
       -f mpegts -mpegts_m2ts_mode 1 sample_data/sample_192byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
```

# Development

## Test & Coverage

```bash
make setup      # configure git hooks (run once after clone)
make build      # build binary
make test       # run all tests
make coverage   # run tests with coverage report
make install    # install to $GOPATH/bin
make uninstall  # remove from $GOPATH/bin
make clean      # remove build/coverage artifacts
```

The version string is managed in the `VERSION` file and injected at build time.

Coverage is measured for `bitbuffer` and `tsparser` packages only. The CLI entry point (`cmd`, `main.go`) is excluded from coverage targets. Both packages should maintain 100% coverage.

A pre-push hook (`make setup` to enable) runs the build, test, and coverage checks before every push. Push is rejected if coverage drops below 100%.

## Release

Releases are cut from the `VERSION` file. The tag is always `v<VERSION>`, so
`VERSION` must contain a bare semver (`X.Y.Z`) with no leading `v` and no extra
dots — a malformed value like `.1.5.0` produces the invalid tag `v.1.5.0` and
fails the release.

1. **Bump the version in a PR.** Edit `VERSION` (e.g. `1.4.0` → `1.5.0`) and merge
   the PR to `master`. Do not tag by hand.
2. **Run the Release workflow.** In GitHub → Actions → **Release** → *Run workflow*
   (`workflow_dispatch`, so only users with write access can start it). It reads
   `VERSION`, checks that `v<VERSION>` does not already exist, creates and pushes
   the tag, then runs GoReleaser in the same job to cross-compile for
   Linux, Windows, and Darwin and publish the GitHub Release with the archives attached.

That is the whole flow — bump `VERSION` in a PR, then trigger the workflow.

GoReleaser runs inside the Release job (not as a reaction to the tag push)
because a tag pushed with the workflow's `GITHUB_TOKEN` does not start other
workflow runs. The separate **goreleaser** workflow only fires for well-formed
`vX.Y.Z` tags pushed by a user (e.g. `make release`, which tags locally instead
of going through the Release workflow — prefer the workflow so releases are
always cut from `master`).

If a release is ever created with a bad tag, delete the release and its tag
before retrying — e.g. `gh release delete v.1.5.0 --cleanup-tag`.
