# Release

[Documentation home](../index.md)

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

GUI releases use the `GUI release` workflow and `gui-vX.Y.Z` tags; see
[Development](index.md) for how it is run and checked.
