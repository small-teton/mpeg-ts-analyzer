# Installation

[Documentation home](../index.md)

The command-line analyzer and the desktop GUI are released separately: the CLI
under `vX.Y.Z` tags, the GUI under `gui-vX.Y.Z` tags.

## Command-line analyzer

### Homebrew (macOS / Linux)

```bash
brew install small-teton/tap/mpeg-ts-analyzer
```

### Linux packages (deb / rpm / apk)

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

### Pre-built binaries

Download from the [Releases](https://github.com/small-teton/mpeg-ts-analyzer/releases) page. No additional tools are required.

### Install with Go

If you have a [Go](https://go.dev/dl/) environment (1.26+):

```bash
go install github.com/small-teton/mpeg-ts-analyzer/v2@latest
```

## Desktop GUI

GUI packages for macOS (Apple silicon), Linux (`.deb`, AppImage) and Windows
(installer, portable zip) are published on the
[releases page](https://github.com/small-teton/mpeg-ts-analyzer/releases) under
`gui-v<version>` tags, separately from the CLI releases. Each package bundles
the CLI of the same commit. The packages are not code-signed; the release notes
explain how to open them and how to verify a download.
