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

Each archive contains `LICENSE` and `THIRD-PARTY-NOTICES.txt`, the licenses of
the Go standard library and the Go modules compiled into the binary. The Linux
packages install both files in `/usr/share/doc/mpeg-ts-analyzer/`.

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
published CLI release, named in the release title and in its manifest file.

The macOS bundle has an ad-hoc signature that seals the app contents, but it is
not signed with an Apple Developer ID or notarized. macOS therefore warns
before the first launch; this warning does not mean that Apple has verified the
publisher or checked this release for malware. See
[Release security](../gui/release-security.md#identity-backed-code-signing-and-notarization-excluded-for-now)
for the current signing model.

This is an intentional trade-off for this personal OSS project. Distributing a
Developer ID-signed and notarized build requires an Apple Developer Program
membership, currently **US$99 per membership year** (or the local-currency
equivalent). The project does not currently pay that recurring fee, so macOS
users need the one-time first-launch override below. This can be reconsidered
if project funding or user demand justifies it. See
[Apple's membership details](https://developer.apple.com/programs/whats-included/)
for the current fee.

### Opening the macOS GUI for the first time

1. Open the downloaded `.dmg`, drag `mpeg-ts-analyzer.app` to **Applications**,
   and open the copied app from Applications. Do not run it from the mounted
   disk image.
2. macOS blocks the first launch because the app is not Developer ID-signed and
   notarized. Dismiss the warning, then open **System Settings → Privacy &
   Security**.
3. Scroll down to **Security** and click **Open Anyway** for
   `mpeg-ts-analyzer`. The button appears only after macOS has blocked a launch
   attempt and is available for about an hour.
4. Confirm the warning by clicking **Open** and authenticate if macOS asks.

macOS saves the app as an exception, so later launches work normally. On a Mac
managed by an organization, its security policy may prevent this override. See
[Apple's official instructions](https://support.apple.com/guide/mac-help/mh40617/mac)
for the current System Settings workflow.

### Updating the GUI

The GUI checks for a newer GUI release once at startup (Settings → Updates
turns this off). When it finds one, it opens the **Software update** window
once per launch; you can keep working while it is open. The update button at
the bottom of the left rail (above Settings) checks on demand. Once an update is known, the rail button shows
a colored dot and its tooltip names the version.

The update window shows the new version and the CLI version it bundles.
**Later** closes it until the next launch; **Skip this version** stops the
startup prompt for that version (a newer release prompts again, and a manual
check still shows it). **Install** downloads the package, verifies its
signature and installs it; it waits while an analysis is running:

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
