# Agent guide — mpeg-ts-analyzer

A CLI analyzer for MPEG-2 Transport Streams (ISO/IEC 13818-1). Parses TS header,
adaptation field, PSI (PAT/PMT), PES header; validates continuity_counter and
CRC32; and reports PCR interval, PCR jitter, and per-PID bitrate.

## Validation policy — cross-check against reference tools

**Actively validate analyzer output against established reference tools.** When
you add or change any analysis (bitrate, PCR jitter, timestamps, PSI parsing,
stream-type detection), do not trust the tool's own numbers alone — reproduce
the same measurement with an independent implementation and reconcile any
difference. A discrepancy you cannot explain is a bug until proven otherwise.

Reference tools available in this environment:

- **TSDuck** (`tsanalyze`, `tsbitrate`, `tsp`) — the primary reference. Use
  `tsanalyze --json <file>` for machine-readable per-PID bitrate, packet counts,
  and stream descriptions. Example:
  ```
  tsanalyze --json sample_data/xxx.ts | python3 -c "import json,sys; d=json.load(sys.stdin); [print(hex(p['id']), p.get('bitrate'), p['packets']['total']) for p in d['pids']]"
  ```
- **gots** (`github.com/Comcast/gots`) and **astits**
  (`github.com/asticode/go-astits`) — Go libraries already in the module cache;
  use for parser-level cross-checks (PID demux, PES/PSI fields) by writing a
  small throwaway program.
- **ffprobe** — quick sanity check of codec/stream layout
  (`ffprobe -show_streams -show_format`).

Prefer packet-count agreement as the strongest signal: if per-PID packet counts
match the reference exactly, the byte accounting is correct and any remaining
bitrate delta is a methodology difference, not a bug.

### Known methodology difference (bitrate)

This tool measures over the **PCR-bracketed window it actually processes**:
counting begins where PES parsing starts (after PAT/PMT acquisition) and ends at
the last PCR; bytes before the first observed PCR and after the last PCR are not
counted, and the reported duration is `lastPCR − firstObservedPCR`. TSDuck, by
contrast, covers the **whole file** (two-pass). For production-length streams the
two agree within ~1%; for very short clips (a couple of seconds) this tool reads
a few percent lower because the leading/trailing regions are a larger fraction.
This is expected — reconcile short-clip deltas against this window difference
before suspecting a bug.

## Build, test, lint

```
go build ./...
go test ./...                 # unit + end-to-end
go vet ./...
gofmt -l .                    # must be empty
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...
```

Expectations enforced by the pre-push hook (`.githooks/pre-push`):

- `bitbuffer` and `tsparser` must stay at **100% coverage** (`make coverage`).
- `gofmt` clean and `golangci-lint` clean.

Sample streams live in `sample_data/` (188- and 192-byte) and `testdata/`
(ISDB-T, M2TS, etc.); use them for manual runs and comparisons.
