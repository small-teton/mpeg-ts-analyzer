# GUI release security (design notes)

Status: not implemented. The GUI is not released yet (#100); these notes record
what a release should add and what was deliberately left out. The CLI releases
through GoReleaser and is not covered here.

## Free measures to add with the first GUI release

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
- **Release workflow hardening.** Build from a tag on `master` through
  `workflow_dispatch`, as the CLI release does, with per-job minimal
  permissions and no caches restored from pull requests.

## Code signing (excluded for now)

macOS Gatekeeper and Windows SmartScreen warn about unsigned applications, but
signing needs paid accounts and long-lived secrets:

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
