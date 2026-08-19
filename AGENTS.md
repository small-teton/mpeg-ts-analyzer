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
- Add or update tests whenever a change would otherwise reduce coverage.
- Before pushing or opening a pull request, run `.githooks/pre-push`; do not
  treat the work as complete unless every check passes.
