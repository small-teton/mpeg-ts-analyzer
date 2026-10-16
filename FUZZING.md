# Fuzz testing

The parser has focused Go fuzz targets for its untrusted-input boundaries:

- TS packets and adaptation fields
- PAT and PMT sections
- descriptors
- PES headers
- 188-byte TS / 192-byte M2TS packet-size detection and PAT discovery

The seed corpus includes valid minimal structures, truncated forms, and both
supported packet sizes. Every target rejects inputs larger than a
target-specific bound that still covers its parser-controlled length fields, so
a bounded fuzz run cannot allocate memory proportional to an unbounded input.

Normal tests execute every deterministic seed without starting active fuzzing:

```sh
go test ./...
```

For local active fuzzing, run one target at a time. A five-minute run per target
is recommended for routine changes:

```sh
go test ./tsparser -run '^$' -fuzz '^FuzzTsPacketAndAdaptationField$' -fuzztime 5m
go test ./tsparser -run '^$' -fuzz '^FuzzPsiSections$' -fuzztime 5m
go test ./tsparser -run '^$' -fuzz '^FuzzDescriptors$' -fuzztime 5m
go test ./tsparser -run '^$' -fuzz '^FuzzPesHeader$' -fuzztime 5m
go test ./tsparser -run '^$' -fuzz '^FuzzPacketDiscovery$' -fuzztime 5m
```

Before opening a pull request, use a longer duration (for example, `-fuzztime
30m`) for parser code that changed. Go writes inputs that reproduce a failure
under `testdata/fuzz/<target>/`; commit each minimized reproducer so it remains a
deterministic regression seed in `go test ./...`.
