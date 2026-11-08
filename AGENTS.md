# Agent guide — mpeg-ts-analyzer

## Repository-local agent rules

- At the start of every task, and again immediately before creating a commit,
  check whether `local/rules/` exists. If it does, list and read every regular
  file matching `local/rules/*` in lexical order. Treat those files as private
  repository instructions in addition to this guide.
- `local/` is intentionally gitignored. Never stage or force-add files under
  `local/rules/`; they may contain machine-specific or private instructions.
- Do not commit until all applicable repository-local rules have been followed.
- If a repository-local rule conflicts with this file or a higher-priority
  instruction, follow the higher-priority instruction and report the conflict
  instead of silently ignoring either rule.

## Commit identity

An AI agent creating a human commit must preserve the identity of the human
operator it is acting for. Never replace that identity with the repository
owner's identity, another contributor's identity, a bot, a test identity, or
an identity inherited accidentally from another repository or machine.

The intended identity must come from the repository-local Git configuration or
from a private repository-local rule under `local/rules/`. Do not hard-code a
person's name or email address in this tracked file. If the intended identity
is missing, ambiguous, or conflicts with the repository-local configuration,
stop and ask the operator before committing instead of guessing or changing it.

Immediately before committing, inspect and verify the repository-local
identity:

```sh
git config --local --get user.name
git config --local --get user.email
git var GIT_AUTHOR_IDENT
git var GIT_COMMITTER_IDENT
```

Both `git var` results must begin with the intended human identity. Do not use
`git commit --author`, `GIT_AUTHOR_*`, or `GIT_COMMITTER_*` to bypass a missing
or incorrect repository-local configuration. Immediately after committing,
verify both recorded identities with:

```sh
git show -s --format='author=%an <%ae>%ncommitter=%cn <%ce>' HEAD
```

Before pushing, inspect every commit introduced by the branch relative to its
upstream, or relative to `origin/master` when it has no upstream:

```sh
git log --format='%H  author=%an <%ae>  committer=%cn <%ce>' '@{upstream}..HEAD'
```

If any human commit has an unintended identity, do not push. Correct unpublished
commits, re-run the checks, and then run the required pre-push validation.
GitHub-generated merge commits and explicitly declared bot commits may retain
their real platform identities. Never rewrite published history for an identity
repair without the affected contributors' explicit approval and a rollback
plan.

## Validate analyzer output against reference tools

When you add or change any analysis (bitrate, PCR jitter, timestamps, PSI, etc.),
**do not trust this tool's own numbers alone** — reproduce the same measurement
with an independent implementation and reconcile any difference. An unexplained
discrepancy is a bug until proven otherwise.

Tools available here:

- **TSDuck** (`tsanalyze --json`, `tsbitrate`) — primary reference for per-PID
  bitrate, packet counts, and stream types.
- **gots** (`github.com/Comcast/gots`), **astits** (`github.com/asticode/go-astits`)
  — Go libraries in the module cache for parser-level cross-checks.
- **ffprobe** — quick codec/stream-layout sanity check.

Packet-count agreement is the strongest signal: if per-PID counts match the
reference exactly, the byte accounting is right and any remaining bitrate delta
is a methodology difference, not a bug.

> Bitrate caveat: this tool measures over the PCR-bracketed window it processes
> (first observed PCR after PSI acquisition → last PCR), so it reads a few percent
> low versus TSDuck's whole-file pass on very short clips. See
> [Dump bitrate](docs/cli/dumps.md#dump-bitrate).

A local (gitignored) comparison helper lives at `local/compare_bitrate.py`.

## Testing and coverage

The `bitbuffer`, `tsparser` and `internal/analysis` packages must each maintain
**100.0% statement coverage** through their own tests. The CLI entry point
(`cmd` and `main.go`) is excluded from the coverage target.

- After changing Go code, run `make coverage` and confirm all three packages
  report `100.0%`.
- Keep decoding, recovery and trust decisions in `tsparser` and aggregation,
  applicability, thresholds and statuses in `internal/analysis`, which imports
  only the standard library; see
  [docs/development/architecture.md](docs/development/architecture.md).
- Active fuzzing commands and the regression-corpus workflow are documented in
  [docs/development/fuzzing.md](docs/development/fuzzing.md).
- Add or update tests whenever a change would otherwise reduce coverage.
- Before pushing or opening a pull request, run `.githooks/pre-push`; do not
  treat the work as complete unless every check passes.
- Pull requests run the deterministic documentation policy in
  `.github/workflows/docs-policy.yml`. Blocking findings identify definite
  interface/documentation omissions; advisory findings are posted as a sticky
  PR comment and in the Actions summary. Resolve or explicitly assess both.
  Run `make check-docs-policy` locally when changing interfaces or docs.

## Compatibility and documentation

- Preserve support for 188-byte TS packets, 192-byte M2TS packets and
  204-byte TS packets (16-byte trailer), including automatic packet-size
  detection.
- Preserve the multi-program default: list available programs and stop unless
  the user selects one with `--program`; `--list-programs` must remain a
  lightweight listing path.
- Treat the documented CLI syntax and existing flag placement as public
  compatibility. Do not replace Cobra or otherwise change argument parsing as
  part of an unrelated cleanup.
- Treat CLI syntax, flag placement or defaults, help text, human-readable or
  JSON output, and exit codes as public interfaces. When any of them changes,
  audit and update the user documentation under `docs/` (`docs/cli/usage.md`
  for flags, help text, verdicts and exit codes; `docs/cli/psi.md` and
  `docs/cli/dumps.md` for their output; `docs/reference/json-output.md` for
  JSON), the README where its overview or quick start is affected, CLI
  help/examples, schemas, and the README demo as applicable in the same pull
  request. Explicitly explain in the pull request when an expected document or
  generated artifact does not need an update.
- The pages under `docs/` are also published as a site (`mkdocs.yml`,
  `.github/workflows/docs-pages.yml`). Add a new page to the `nav` in
  `mkdocs.yml`, and link to files outside `docs/` with their GitHub URLs; the
  strict site build fails otherwise. See the documentation site section of
  `docs/development/index.md`.
- `DIRECTORY_STRUCTURE.md` maps what each top-level directory and file is
  for. When a change adds, removes, renames or moves a top-level file or
  directory, or a directory the map describes (for example a new `tools/`
  program, a `sample_data/` subdirectory, a `docs/` area, a workflow or a
  configuration file), update `DIRECTORY_STRUCTURE.md` in the same pull
  request, and state the update, or that none was needed, in the
  documentation-impact audit. Keep each entry to one line and link to the
  detailed documentation instead of repeating it.
- When the JSON contract changes, update `docs/json-schema-v2.json`, the
  contract and the complete examples in `docs/reference/json-output.md`, and
  decoding/compatibility tests together. Preserve the documented additive-change policy within a
  schema version; breaking field, type, or enum-meaning changes require a
  schema-version increment.

## Contribution workflow

### Issue workflow

- Before opening or substantially editing an issue, read the applicable form
  under [`.github/ISSUE_TEMPLATE/`](.github/ISSUE_TEMPLATE/) and inspect recent
  related issues for established terminology and scope. When creating an issue
  through a tool that bypasses GitHub's form UI, reproduce the applicable
  template's sections and required information in the issue body.
- Write issue titles and bodies in English. Keep implementation issues focused
  and include the problem, proposed behavior, compatibility considerations,
  acceptance criteria, dependencies, validation expectations, and explicit
  non-goals as applicable. Never put confidential media, credentials, or a
  suspected vulnerability in a public issue; use GitHub's private security
  advisory flow for vulnerabilities.
- Use GitHub's native parent/sub-issue relationship for hierarchy and labels
  for search, Projects views, and automation. Apply `tracking` to a parent that
  coordinates multiple implementation issues and normally contains no
  implementation of its own.
- Apply the relevant area label to both a tracking issue and each related
  implementation issue. Apply `enhancement` to implementation issues, but
  normally not to a pure tracking issue. Do not add a redundant `sub-issue`
  label.
- Use a milestone only when work is committed to a specific release; do not
  use milestones merely to represent issue hierarchy. When a parent defines an
  implementation order, keep its native sub-issue order and body synchronized.

### Pull requests

- `.github/CODEOWNERS` assigns the repository to `@small-teton`. Because a PR
  author cannot approve their own PR, owner-authored PRs rely on the current
  administrator bypass (`enforce_admins: false`). Do not enable enforcement for
  administrators or remove that bypass until another eligible approver exists.
- Before implementation and again before opening a pull request, read the full
  linked issue and inspect recent related pull requests for established
  behavior, terminology, documentation style, and validation expectations.
- Open ordinary changes against `master` from a focused branch that contains no
  unrelated work. Use a concise imperative title. Write the pull request
  description and review replies in English.
- Follow [`.github/pull_request_template.md`](.github/pull_request_template.md)
  when composing or updating the description, including when a tool creates
  the pull request without loading the template automatically. Preserve its
  headings and checklist, keep the motivation, change, compatibility,
  documentation, and validation sections meaningful, and do not leave
  placeholder comments as a substitute for documenting an applicable concern.
- Give the description enough context to review without reconstructing the
  change from the diff. Cover the motivation, concrete behavior and design,
  compatibility or migration impact, and the exact validation performed. For
  user-visible or structured output, include representative real examples and
  explain relevant output-size or resource characteristics.
- For every public-interface change, include a documentation-impact audit in
  the description. Name the updated README, pages under `docs/`,
  help/examples, schema, and generated-demo artifacts, or state why each
  relevant item is not affected.
- Use `Closes #N` only when the pull request fully resolves the issue. Use
  `Follow-up to #N` or a plain reference when it extends earlier work without
  closing the original request.
- Report commands and observed results accurately; do not claim checks that
  were not run. When analyzer measurements change, include the independent
  reference-tool comparison required above and explain any discrepancy.
- After review fixes or scope changes, push the commits, update the pull request
  description and validation list so they still describe the final diff, and
  reply to review comments in English with the change and verification result.
- Before opening or handing off a pull request, ensure the final pushed commit
  passed `.githooks/pre-push`. Do not hand off a pull request with failing
  required checks or an unexplained dirty worktree.

## Desktop GUI

- The GUI lives in `gui/` (Rust, eframe/egui) and runs the CLI as a
  subprocess; it never contains analysis logic. Design and the CLI contract are
  in [`docs/gui/design.md`](docs/gui/design.md) and
  [`docs/gui/cli-interface.md`](docs/gui/cli-interface.md); keep them current
  when the design changes.
- Do not put Go code under `gui/`. Go tooling (`go build ./...`, coverage,
  docs policy) must stay unaffected by the directory.
- Before pushing GUI changes, `make gui-lint` (rustfmt and clippy with
  `-D warnings`) and `make gui-test` must pass; `.githooks/pre-push` runs them
  when `gui/` or the `Makefile` changed. The Rust toolchain is pinned in the repository-root `rust-toolchain.toml`.
- Write all repository content in English, including GUI strings, comments and
  documentation.
- GUI release tags are `gui-v*`, never `gui/v*` (Go would read the latter as a
  sub-module version). Do not create GUI tags unless explicitly requested.

## README demo workflow

- After changing CLI output, demo rendering code, or the demo container
  definition, run `make check-demo` to verify that the committed animation is
  current.
- If the check reports a stale or missing demo, follow
  [`docs/demo/README.md`](docs/demo/README.md), run `make demo`, inspect the
  complete animation, and commit both `docs/demo.gif` and
  `docs/demo/manifest.sha256`.
- Demo regeneration is container-based. On macOS it prefers Apple `container`
  and falls back to Docker; set `DEMO_CONTAINER_RUNTIME` only when an explicit
  runtime is needed.
- Keep the renderer within its documented 1 GB container budget. If future
  changes increase memory pressure, split work into smaller scenes or reduce
  retained frames before raising the limit; document any justified change in
  `docs/demo/README.md`.

## Release workflow

- `VERSION` must contain a bare `X.Y.Z` semantic version with no leading `v`.
- Version bumps belong in a pull request to `master`. Do not create or push a
  release tag unless the user explicitly requests a release.
- Prefer the GitHub **Release** workflow after the version bump is merged;
  do not tag manually as part of ordinary feature or maintenance work.
