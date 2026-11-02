# Multi-section PAT/PMT reference fixture

Rewrite the two-program samples so that the PAT has one section per program and
each PMT one section per elementary stream (sent from the last section to
section 0), then check the tables with TSDuck:

```sh
go run ./tools/generate_multi_section_psi \
  -input sample_data/sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts \
  -output sample_data/sample_188byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts
go run ./tools/generate_multi_section_psi \
  -input sample_data/sample_192byte_two_programs_mpeg2_160x120_25fps_mp2.ts \
  -output sample_data/sample_192byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts
tsp -I file sample_data/sample_188byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts \
  -P tables --pid 0 --pid 0x1000 --pid 0x1001 --max-tables 3 -O drop
tsanalyze --json sample_data/sample_188byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts
tsp -I file --format m2ts sample_data/sample_192byte_two_programs_multi_section_psi_mpeg2_160x120_25fps_mp2.ts \
  -P analyze --json -o analyze.json -O drop
```

Only PSI payloads change, so packet counts, PCRs and timestamps match the input.
TSDuck 3.45 reports a PAT of 2 sections and two PMTs of 2 sections each, 2
services and 8 PIDs, with the same per-PID packet counts as the single-section
samples. `tsparser` tests check that the committed fixtures equal the generator
output.
