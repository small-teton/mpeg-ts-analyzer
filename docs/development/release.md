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

GUI releases use the `GUI release` workflow and `gui-vX.Y.Z` tags; see
[Development](index.md) for how it is run and checked.
