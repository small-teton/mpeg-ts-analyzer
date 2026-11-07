# Release

[Documentation home](../index.md)

Releases are cut from the `VERSION` file. The tag is always `v<VERSION>`, so
`VERSION` must contain a bare semver (`X.Y.Z`) with no leading `v` and no extra
dots — a malformed value like `.1.5.0` produces the invalid tag `v.1.5.0` and
fails the release.

1. **Bump the version in a PR.** Edit `VERSION` (e.g. `1.4.0` → `1.5.0`) and merge
   the PR to `master`. Do not tag by hand.
2. **Run the Release workflow.** In GitHub → Actions → **Release** → *Run workflow*
   on `master` (`workflow_dispatch`, so only users with write access can start
   it). The workflow refuses other branches. In one job it:
   1. checks `VERSION`, that the commit is on `master`, and the state of the
      `v<VERSION>` tag and release;
   2. builds every archive and package with GoReleaser without publishing, and
      checks that the built CLI reports `VERSION`;
   3. creates the tag only then;
   4. builds again with GoReleaser and uploads a **draft** release (archives,
      Linux packages, checksums and the Homebrew cask);
   5. publishes the draft only if its checksums equal those of the checked
      build, with build provenance for each archive and package.

That is the whole flow — bump `VERSION` in a PR, then trigger the workflow.

The two builds are identical because `.goreleaser.yaml` pins every timestamp
and file owner. They run in the same job because the rpm records the build
host. GoReleaser builds twice since its open-source edition cannot publish an
earlier build.

Pull requests that change `VERSION`, the Go modules, `.goreleaser.yaml` or the
workflow run the same checks and first build, without a tag or a release. A
`VERSION` that a pull request changes must not have been released yet.

A tag `vX.Y.Z` pushed by a user (`make release`) starts the same workflow. The
tag must match `VERSION` and point at a commit on `master`. Prefer the
workflow, so the tag is created only after the build has passed.

A failed run can be started again:

- If the tag does not exist yet, nothing was published.
- If the tag points at the checked commit but the release is still a draft or
  missing, the rerun builds again and replaces the draft.
- If the release is already published, the run stops without changing it.
- If the tag points at another commit, the run fails. The workflow never moves
  or deletes a tag.

## GUI release

GUI releases are independent of the CLI's `VERSION`: the GUI version is the
`version` in `gui/Cargo.toml`, released as `gui-vX.Y.Z` and never marked
"latest". The packages bundle a published CLI release, never a CLI built from
the GUI's commit.

1. **Release the CLI first** when the GUI needs CLI output that is not
   published yet (see above).
2. **Set the versions in a PR.** `version` in `gui/Cargo.toml` (and
   `gui/Cargo.lock`) and the CLI version to bundle in
   `gui/BUNDLED_CLI_VERSION` (a bare `X.Y.Z`). Merge the PR to `master`.

   GitHub → Actions → **GUI Bump Version** → *Run workflow* opens this PR:
   choose patch, minor or major, and optionally the CLI version to bundle
   (empty keeps the current one). It accepts a published CLI release, or the
   version in `VERSION` with a warning when that is not released yet. The PR
   is opened with `GITHUB_TOKEN`, so its checks do not start on their own:
   close and reopen it to run them.
3. **Run the GUI release workflow.** In GitHub → Actions → **GUI release** →
   *Run workflow* on `master`. It:
   1. checks the versions, that the commit is on `master`, and the state of the
      `gui-v<version>` tag and release;
   2. downloads the CLI archives of release `v<BUNDLED_CLI_VERSION>`, checks
      them against that release's checksums and build provenance from the CLI
      `Release` workflow, and checks the tag's `VERSION`;
   3. builds the packages on macOS, Linux and Windows, checks that the bundled
      CLI reports that version, installs or unpacks each package and checks
      that it finds that CLI;
   4. signs the update packages for the in-app update with the key of the
      `gui-release` environment and verifies them against `gui/updater.pub`
      (see [Update signing](../gui/release-security.md#update-signing));
   5. only then creates the tag at the built commit, and publishes the packages
      with `SHA256SUMS`, an SBOM, build provenance,
      `mpeg-ts-analyzer-gui_<version>_manifest.json`, which names the GUI and
      CLI versions, tags and commits, and the update packages with their
      signatures and `mpeg-ts-analyzer-gui_<version>_update.json`.

A tag `gui-vX.Y.Z` pushed by a user starts the same workflow; it must match
`gui/Cargo.toml` and point at a commit on `master`. Reruns behave as for the
CLI: they continue when the tag points at the built commit and no published
release exists (a draft is replaced), stop when the release is published, and
fail when the tag points elsewhere. Tags are never moved or deleted.

Pull requests that change the packaging, the versions or the release scripts
run the same checks and build the packages without a tag or a release; they
sign the update packages with a key generated in the run, never with the
release key. When
the declared CLI release is not published yet but `VERSION` already names it,
those pull requests bundle a CLI built from their commit instead, with a
warning; a release always requires the published CLI.

### Update signing key (one-time setup)

The `sign-updates` job reads the key for the in-app update from the
`gui-release` environment. Before the first release with update signing:

1. Generate the key pair outside the repository and keep it private:

   ```sh
   mkdir -p ~/keys && chmod 700 ~/keys
   cargo packager signer generate --path ~/keys/mpeg-ts-analyzer-gui-updater.key
   ```

   This writes the password-protected private key and `<file>.pub`. The
   public key must equal `gui/updater.pub`; a new key pair also needs a pull
   request that replaces that file (see
   [Keys](../gui/release-security.md#keys) for rotation).
2. In GitHub → Settings → Environments, create `gui-release`. Under
   *Deployment branches and tags*, choose *Selected branches and tags* and add
   the branch `master` and the tag pattern `gui-v*`.
3. Add two environment secrets: `GUI_UPDATER_PRIVATE_KEY`, the content of the
   private key file, and `GUI_UPDATER_PRIVATE_KEY_PASSWORD`, its password.
4. Back up the private key file and the password offline. Without them, later
   releases cannot be signed with the key that installed GUIs trust.

A release run without these secrets fails in `sign-updates`, before anything is
tagged or published.
