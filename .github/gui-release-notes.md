Desktop GUI for mpeg-ts-analyzer {{VERSION}}. Each package bundles the
published `mpeg-ts-analyzer` CLI {{CLI_VERSION}} (the binary of release
`v{{CLI_VERSION}}`, checked against its checksums and build provenance); the
GUI finds it without any setup. `mpeg-ts-analyzer-gui_{{VERSION}}_manifest.json`
records the GUI and CLI versions and the commits they were built from.

| OS | Download |
|---|---|
| macOS (Apple silicon) | `mpeg-ts-analyzer-gui_{{VERSION}}_macos_arm64.dmg` |
| Linux (x86_64) | `mpeg-ts-analyzer-gui_{{VERSION}}_amd64.deb` or `mpeg-ts-analyzer-gui_{{VERSION}}_linux_x86_64.AppImage` |
| Windows (x86_64) | `mpeg-ts-analyzer-gui_{{VERSION}}_windows_x86_64_setup.exe` (per-user install, no administrator rights) or `mpeg-ts-analyzer-gui_{{VERSION}}_windows_x86_64_portable.zip` |

The `.app.tar.gz` archive, the `.sig` files and
`mpeg-ts-analyzer-gui_{{VERSION}}_update.json` are for the GUI's in-app update;
download the packages above instead. An installed GUI that has the in-app
update (About → Check for Updates…) can install this release itself.

## Opening the app the first time

The packages are not signed with a Developer ID or notarized (the macOS app is
signed ad hoc), so the OS warns before the first start.

- **macOS:** open the app once, then allow it under System Settings → Privacy &
  Security → "Open Anyway". Alternatively remove the quarantine flag:
  `xattr -dr com.apple.quarantine /Applications/mpeg-ts-analyzer.app`.
- **Windows:** SmartScreen shows "Windows protected your PC"; choose "More info"
  → "Run anyway".

## Verifying a download

```sh
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify <file> --repo small-teton/mpeg-ts-analyzer
```

`mpeg-ts-analyzer-gui_{{VERSION}}_sbom.cdx.json` lists the Rust crates of the
GUI (CycloneDX).
