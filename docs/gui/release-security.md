# GUI release security (design notes)

Status: the free measures below are implemented in
`.github/workflows/gui-release.yml` (#100), and so is the signing for the
in-app update and its verification in the GUI (#150); code signing with a Developer ID or a
commercial certificate is not. The macOS app is only signed ad hoc. The CLI
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

## Update signing

Status: implemented (#150): the release workflow signs the update packages
and publishes the update manifest, and the GUI verifies them. The GUI's in-app update
([design](design.md#updates)) installs packages only when their signature
verifies against a public key compiled into the GUI. The signature is a
minisign signature made with `cargo packager signer`, the format
`cargo-packager-updater` verifies. It is independent of OS code signing: it
says the package came from this repository's release workflow, which is what
the updater needs to trust, and it costs nothing.

### Keys

- The repository owner generates the key pair once with
  `cargo packager signer generate --path <file>`, protected by a password
  (setup: [GUI release](../development/release.md#gui-release)).
- The public key is committed as `gui/updater.pub` and compiled into the GUI
  (`include_str!`).
- The private key and its password are stored only as the secrets
  `GUI_UPDATER_PRIVATE_KEY` and `GUI_UPDATER_PRIVATE_KEY_PASSWORD` of a GitHub
  environment, `gui-release`, plus the owner's offline backup. The
  environment's deployment rules allow the `master` branch and `gui-v*` tags,
  the two ways a release starts; GitHub matches them against the triggering
  ref, not the tagged commit. The job that uses the environment depends on the
  `version` job, which already requires the commit to be on `master`
  (`release-state.sh on-master`). Pull requests never reference the
  environment.
- Rotation: a release built with a new public key is still signed with the old
  key, so existing installations accept it; from then on releases use the new
  key. A leaked key cannot be revoked in installations that still embed it:
  publish an advisory and ask users to reinstall from the release page.

### Release workflow

1. The packaging jobs also produce the update packages, unsigned:
   `mpeg-ts-analyzer-gui_<version>_macos_arm64.app.tar.gz` (the ad-hoc signed
   `mpeg-ts-analyzer.app`, as its top-level directory), the AppImage, and the
   NSIS installer. The macOS packaging job checks that the archive holds only
   the signed `.app`, without AppleDouble files, that its signature verifies
   and that it finds its bundled CLI.
2. A separate job on Ubuntu, `sign-updates`, the only one that uses the
   `gui-release` environment, signs those three files with
   `cargo packager signer sign` and writes
   `mpeg-ts-analyzer-gui_<version>_update.json` with each URL, signature and
   format. It then verifies every signature against `gui/updater.pub`, so a
   key that does not match the GUI's public key fails the release before
   anything is published. cargo-packager stores the public key and each
   signature as Base64 of the minisign text, so the job decodes both before
   running `minisign -V`, and it also requires the signed trusted comment to
   name the file. The script is `.github/scripts/sign-gui-updates.sh`.
3. The release job publishes the update packages, their `.sig` files and the
   manifest with the other assets, so they are covered by `SHA256SUMS` and the
   build provenance like everything else.

On pull requests a separate job, which does not reference the environment,
signs with a key generated in the job and verifies against that key's public
half; no secret is available and nothing is published.

### What the signature does and does not cover

- It covers the package bytes. A package altered on its way from GitHub, or
  uploaded by someone without the private key, is rejected.
- The manifest itself is not signed; its version and URLs are trusted because
  they come from this repository's releases over HTTPS, and the GUI only
  requests GitHub (see [Updates](design.md#updates)).
- The signed trusted comment names the signed file, whose name contains the
  version and platform, and the GUI requires it to be exactly the package it
  expects. A genuinely signed package of another version or platform, offered
  under a newer version number, is therefore rejected as well.
- The GUI never offers a version lower than or equal to the running one.

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

## Identity-backed code signing and notarization (excluded for now)

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
