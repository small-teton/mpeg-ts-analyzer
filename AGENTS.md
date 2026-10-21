# Agent guide — mpeg-ts-analyzer

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
> low versus TSDuck's whole-file pass on very short clips. See README.md.

A local (gitignored) comparison helper lives at `local/compare_bitrate.py`.

## Testing and coverage

The `bitbuffer` and `tsparser` packages must each maintain **100.0% statement
coverage**. The CLI entry point (`cmd` and `main.go`) is excluded from the
coverage target.

- After changing Go code, run `make coverage` and confirm both packages report
  `100.0%`.
- Active fuzzing commands and the regression-corpus workflow are documented in
  [FUZZING.md](FUZZING.md).
- Add or update tests whenever a change would otherwise reduce coverage.
- Before pushing or opening a pull request, run `.githooks/pre-push`; do not
  treat the work as complete unless every check passes.

## Compatibility and documentation

- Preserve support for both 188-byte TS packets and 192-byte M2TS packets,
  including automatic packet-size detection.
- Preserve the multi-program default: list available programs and stop unless
  the user selects one with `--program`; `--list-programs` must remain a
  lightweight listing path.
- Treat the documented CLI syntax and existing flag placement as public
  compatibility. Do not replace Cobra or otherwise change argument parsing as
  part of an unrelated cleanup.
- When flags or user-visible output change, update both `README.md` and the
  annotated examples in `OPTIONS.md` in the same pull request.
- When the JSON contract changes, update `docs/json-schema-v1.json`, the README
  contract, the complete example in `OPTIONS.md`, and decoding/compatibility
  tests together. Preserve the documented additive-change policy within a
  schema version; breaking field, type, or enum-meaning changes require a
  schema-version increment.

## Pull request workflow

- Before implementation and again before opening a pull request, read the full
  linked issue and inspect recent related pull requests for established
  behavior, terminology, documentation style, and validation expectations.
- Open ordinary changes against `master` from a focused branch that contains no
  unrelated work. Use a concise imperative title. Write the pull request
  description and review replies in English.
- Follow [`.github/pull_request_template.md`](.github/pull_request_template.md)
  when composing the description. Keep its motivation, change, compatibility,
  and validation sections meaningful; do not leave placeholder comments as a
  substitute for documenting an applicable concern.
- Give the description enough context to review without reconstructing the
  change from the diff. Cover the motivation, concrete behavior and design,
  compatibility or migration impact, and the exact validation performed. For
  user-visible or structured output, include representative real examples and
  explain relevant output-size or resource characteristics.
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
