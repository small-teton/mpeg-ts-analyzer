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
`gui-v<version>` tags, separately from the CLI releases. Each package bundles a
published CLI release, named in the release title and in its manifest file. The packages are not code-signed; the release notes
explain how to open them and how to verify a download.

### Updating the GUI

The GUI checks for a newer GUI release once at startup (Settings → Updates
turns this off) and shows "Update available" in the status bar when there is
one. About → **Check for Updates…** checks on demand. The update window shows
the new version and the CLI version it bundles; **Install** downloads the
package, verifies its signature and installs it:

- **macOS** (`.app` in Applications): the app is replaced, then **Restart
  now** starts the new version. Because the GUI downloads the update itself,
  macOS does not ask for "Open Anyway" again. Move the app out of the disk
  image first; an app started from the `.dmg` cannot update itself.
- **Windows** (installer): the GUI closes, the new installer runs with a
  progress bar, and the GUI starts again.
- **Linux** (AppImage): the AppImage file is replaced, then **Restart now**.

The `.deb` package and the Windows portable zip cannot update themselves; for
them the update window links to the release page. The bundled CLI is updated
with the GUI; a CLI installed separately (Homebrew, `go install`, packages) is
not changed. The GUI contacts only GitHub (`api.github.com` and the release
downloads) for this.
