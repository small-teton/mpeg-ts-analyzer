# Continuity reference fixture

Generate the deterministic fixture and check it with TSDuck:

```sh
go run ./tools/generate_continuity -output /tmp/continuity.ts
tsp -I file /tmp/continuity.ts -P continuity --json-line -O drop
```

The stream includes legal 15-to-0 wraparound, an exact duplicate, an invalid
same-counter packet, adaptation-only packets, a declared discontinuity, PUSI,
one counter gap, recovery after each error, and arbitrary null-packet counters.
TSDuck 3.44 reports two discontinuity events on PID `0x0100`: the invalid
same-counter packet and the gap. It reports no cascade after either event.
