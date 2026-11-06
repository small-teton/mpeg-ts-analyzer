# GUI release security (design notes)

Status: the free measures below are implemented in
`.github/workflows/gui-release.yml` (#100); code signing with a Developer ID
or a commercial certificate is not. The macOS app is only signed ad hoc. The CLI
releases through GoReleaser and is not covered here.

## Free measures in every GUI release

- **Checksums.** Publish a `SHA256SUMS` file next to the archives, generated in
  the release job from the uploaded files. It detects corrupted or swapped
  downloads; on its own it does not prove who built them.
- **SBOM.** Generate an SPDX or CycloneDX SBOM from `gui/Cargo.lock` (for
  example with `cargo-cyclonedx`, pinned with `--locked` like `cargo-audit`) and
  attach it to the release. The dependency graph's SBOM export is a fallback but
  covers the whole repository, not one artifact.
- **Build provenance.** Use GitHub Artifact Attestations
  (`actions/attest-build-provenance`, pinned to a commit SHA) for each archive
  and for the SBOM. They are signed through Sigstore with the workflow's OIDC
  identity, need no stored key, and users can verify them with
  `gh attestation verify <file> --repo small-teton/mpeg-ts-analyzer`. The
  release job then needs `id-token: write` and `attestations: write`, and only
  that job.
- **Release workflow hardening.** Build the commit `workflow_dispatch` was
  started on (`master` only), and create the tag only after every package has
  passed its checks, as the CLI release does. Per-job minimal permissions, and
  no caches restored in release runs.
- **Bundled CLI.** The packages bundle the published CLI release named in
  `gui/BUNDLED_CLI_VERSION`. Its archives are checked against the release's
  checksums and its build provenance from the CLI `Release` workflow, and the
  binary must report that version before it is packaged.

## Packaging tools fetched at build time

cargo-packager and cargo-cyclonedx are installed at exact versions with
`--locked` (updated by Renovate). cargo-packager itself downloads further tools
while packaging:

- NSIS for the Windows installer, from fixed release URLs; cargo-packager
  checks the NSIS archive and its utility plugin against SHA-1 hashes built
  into it.
- linuxdeploy and its AppImage plugin for the AppImage, from `continuous`
  release tags, without a hash check.

They run only in the packaging jobs, and the build provenance records the
workflow run that used them, but a replaced linuxdeploy download would end up
in the AppImage. Pinning it is left for later; the `.deb` does not use it.

## Code signing (excluded for now)

The macOS app is signed ad hoc (`gui/scripts/sign-macos-app.sh`): the
signature seals `Info.plist` and the resources, including the bundled CLI,
which keeps its own linker signature and stays byte for byte the published
release binary. That is required: a downloaded app with an unsigned bundle is
reported by macOS as "damaged", with no way to open it. An ad-hoc signature
proves nothing about who built the app, so Gatekeeper still asks before the
first start (`spctl` assesses it as "rejected", and "Open Anyway" is offered).
The release job checks the signature with `codesign --verify --deep --strict`
and that the bundled CLI equals the verified release binary.

macOS Gatekeeper and Windows SmartScreen warn about applications that are not
signed by a known developer, but such signing needs paid accounts and
long-lived secrets:

- **macOS:** an Apple Developer Program membership (yearly fee), a Developer ID
  Application certificate, and notarization with an App Store Connect API key.
- **Windows:** an OV or EV code-signing certificate from a commercial CA, or a
  cloud signing service; both cost money and need key custody.

Conditions for adding it: the GUI is distributed to general users (not only
developers who can build it or bypass the warning), someone owns the yearly
renewal, and the secrets can be held in GitHub environments with required
reviewers so that only the release job on `master` can use them. Until then,
release notes should explain the warnings and point to the checksums and
attestations for verification.
