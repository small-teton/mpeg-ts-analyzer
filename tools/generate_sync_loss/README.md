# Sync-loss diagnostic fixture

Write a short constant-rate stream whose packet alignment is damaged in four
places and that ends with a partial packet, then check the losses and the
recovery with TSDuck. The stream is **intentionally invalid**: use it to test
and demonstrate the TS synchronization check, never as an example of a
compliant stream.

```sh
go run ./tools/generate_sync_loss -output sample_data/sample_188byte_sync_loss.ts
go run ./tools/generate_sync_loss -m2ts -output sample_data/sample_192byte_sync_loss.ts
./mpeg-ts-analyzer sample_data/sample_188byte_sync_loss.ts
tsp -I file sample_data/sample_188byte_sync_loss.ts -P count -O drop
tsresync -c -m 960 -s 1024 -v -d3 -k -o resynced.ts sample_data/sample_188byte_sync_loss.ts
tsp -I file resynced.ts -P continuity -P count -O drop
```

The undamaged stream has 200 packets of one 10 ms slot each (150.4 kbit/s,
2 s): program 1 (PMT PID 0x1000) with MPEG-2 video on PID 0x0100, which
carries the PCR. PAT and PMT are sent twice at the start (so the analysis
starts at slot 4) and then every 500 ms (PAT in slots 25, 75, ..., PMT in
slots 27, 77, ...). From slot 4 the other even slots carry one video PES packet
each with a PCR and a PTS, and the other odd slots are null packets. The
damage, by slot of the undamaged stream, with the result for 188-byte packets
(`ps` is the packet size, 188 or 192; offsets are in the damaged file):

| Damage | What is written | Loss at | Skipped |
|---|---|---|---|
| `sync-byte` | the sync byte of video slot 40 is 0x00 | 40·ps | ps (slot 40 is lost) |
| `insert-byte` | one 0xFF byte after byte 100 of null slot 61 | 62·ps | 1 |
| `delete-byte` | byte 100 of null slot 81 removed | 82·ps + 1 | ps − 1 (slot 82 is lost) |
| `garbage` | 1000 bytes before slot 121, zero except four false sync bytes one packet apart | 121·ps | 1000 |
| `truncated-tail` | the first 100 bytes of a null packet after slot 199 | — | trailing partial packet at 200·ps + 1000 |

The analyzer reports `sync.status` `recovered`: 4 losses, 4 resyncs, 2·ps +
1000 skipped bytes (1376 or 1384), the largest 1000 at 121·ps, and a 100-byte
trailing partial packet. The video packets lost in slots 40 and 82 give one
continuity error each, at slots 42 and 84 (42·ps and 84·ps: the inserted and
deleted bytes cancel out). The four false sync bytes in the garbage block are
one fewer than the five consecutive sync bytes a new alignment requires, so
they are skipped. `-omit` leaves damage out (`sync-byte`, `insert-byte`,
`delete-byte`, `garbage`, `truncated-tail` or `all`). `-unrecoverable` also sets
the sync byte of slot 197 to 0x00: fewer than five aligned packets follow, so
the analysis stops there with `sync.status` `lost`, top-level `status`
`incomplete` and `reason` `sync_lost`. `tsparser` tests check that the
committed files equal the generator output and pin these results for both
packet sizes and for each kind of damage alone.

With `-m2ts`, each packet gets a 4-byte TP_extra_header whose
arrival_time_stamp advances by one slot; the damage is at the same positions
relative to the sync byte.

TSDuck 3.45 agrees for both files:

- `tsp` (which does not resynchronize) stops with `synchronization lost after
  40 packets, got 0x00 instead of 0x47`, the analyzer's first loss at 40·ps.
- `tsresync -c -m 960` (`--continue` after each loss, at least 960 bytes of
  contiguous packets, which is five 192-byte packets) reports losses after 40,
  61, 81 and 119 output packets, finding synchronization again after 188 (192),
  1, 187 (191) and 1000 bytes, and writes 198 packets: the analyzer's four
  losses and skipped spans, and its 198 analyzed packets. TSDuck counts the
  packets it has written, so the second and third losses are "after 61" and
  "after 81" although they are at slot boundaries 62 and 82 of the undamaged
  stream, and the fourth is "after 119" because two packets were dropped.
- On the resynchronized output, `tsp -P continuity` reports `PID 0x0100,
  missing 1 packets` at packet indexes 41 and 82, slots 42 and 84 (two packets
  were dropped before index 82), and `-P count` gives PAT 6, PMT 6, PID 0x0100
  96 and null 90 packets, as `psi_layout.pids` of the analyzer.
- With `-unrecoverable`, `tsresync -c -p 188` reports the fifth loss after 195
  packets and `Cannot find MPEG TS packets` for the rest; the analyzer stops at
  the same boundary, 197·ps + 1000.

Methodology differences:

- `tsresync` accepts a new alignment after a minimum number of contiguous
  bytes (`-m`); with `-m 940`, which is five 188-byte packets but fewer than
  five 192-byte ones, it accepts the four false sync bytes of the M2TS garbage
  block as packets and loses synchronization again after them. The analyzer
  always requires five sync bytes one packet apart, whatever the packet size.
- ETSI TR 101 290 distinguishes a single corrupted sync byte (Sync_byte_error)
  from a loss of synchronization (two or more consecutive corrupted sync bytes,
  TS_sync_loss). The analyzer reports every boundary without a sync byte as a
  loss and does not trust the packet: a corrupted sync byte with an unchanged
  alignment is one loss that skips one packet, like `sync-byte` above.
