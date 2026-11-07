# Sample data

[Documentation home](../index.md)

Sample TS files are included in `sample_data/` for quick testing:

```bash
# 188-byte TS
ffmpeg -f lavfi -i "color=c=blue:s=320x240:d=5,format=yuv420p" \
       -f lavfi -i "anullsrc=r=48000:cl=stereo" \
       -t 5 -c:v mpeg2video -c:a mp2 -metadata:s:a:0 language=eng \
       -f mpegts sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts

# 192-byte M2TS
ffmpeg -f lavfi -i "color=c=red:s=320x240:d=2,format=yuv420p" \
       -f lavfi -i "anullsrc=r=48000:cl=stereo" \
       -t 2 -c:v mpeg2video -c:a mp2 -metadata:s:a:0 language=eng \
       -f mpegts -mpegts_m2ts_mode 1 sample_data/sample_192byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts
```

## 204-byte packets

Three samples have a 204-byte twin, `sample_data/sample_204byte_*.ts`: the
[all-findings sample](#diagnostic-sample-with-every-overview-finding), the
[sync-loss sample](#diagnostic-sample-with-ts-synchronization-loss) and the
[packet-gap sample](#packet-gap-sample). Their generators' `-trailer` flag
follows every 188-byte TS packet with a 16-byte trailer, the layout of 204-byte
broadcast streams whose trailer is commonly `RS(204,188)` parity. The trailer
is deterministic filler (bytes 0x80–0xBF, never a sync byte), not real parity;
the sync-loss twin adds false sync bytes to four trailers. The analyzer gives
the same results as for the 188-byte files, with offsets 204 bytes per packet,
and tests also run every other aligned 188-byte sample with trailers added and
compare the analysis and summary documents. The public TSDuck corpus file
[`test-170.ts`](https://github.com/tsduck/tsduck-test/blob/f42b2f2b14c27a065eece1fd329d171a9b79c86f/input/test-170.ts)
(a 204-byte ISDB-Tb capture, not committed here) was used to cross-check the
analyzer with TSDuck.

## Diagnostic sample with every Overview finding

`sample_data/sample_188byte_all_findings.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_all_findings.ts` (23 KiB each) are **intentionally
invalid**. They are not examples of a good stream: each carries deliberate
defects so that every Overview transport-health check reports a problem in one
analysis — a 160 ms PCR interval, a 1200 ms PCR → PTS/DTS gap, a backward PTS,
one continuity-counter gap, a PCR that is 100 µs late, a null packet with
`transport_error_indicator` set, a scrambled null packet, an 800 ms gap
between two PATs and between two PMTs, a video PES header with a bad PTS
marker bit and 800 ms of audio PES packets without a PTS. The stream is
delivered at a constant 75.2 kbit/s for 2.4 s, long enough for two 1-second
windows, so the PCR jitter analysis finds constant delivery and judges the
late PCR (`NG`). Its packets are aligned and its PCR clock is continuous, so
TS synchronization and PCR discontinuities stay `OK`; the
[sync-loss sample](#diagnostic-sample-with-ts-synchronization-loss) and the
[PCR discontinuity sample](#pcr-discontinuity-sample) cover them. Use the normal sample above for a first look at the
analyzer.

```bash
go run ./tools/generate_all_findings -output sample_data/sample_188byte_all_findings.ts
go run ./tools/generate_all_findings -m2ts -output sample_data/sample_192byte_all_findings.ts
go run ./tools/generate_all_findings -trailer -output sample_data/sample_204byte_all_findings.ts
./mpeg-ts-analyzer sample_data/sample_188byte_all_findings.ts --dump-pcr-jitter
make gui-run GUI_ARGS='sample_data/sample_188byte_all_findings.ts'
```

The generator, the position of each defect, the expected results and the
TSDuck cross-check are described in
[`tools/generate_all_findings`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_all_findings).
Tests check that the committed files equal the generator output and pin the
verdicts for both packet sizes; the GUI's Overview tests use the CLI's analysis
of the 188-byte file (`gui/tests/fixtures/analysis_all_findings_188.json`),
which `make gui-test` compares with the output of the CLI built from the tree.

## Diagnostic sample with TS synchronization loss

`sample_data/sample_188byte_sync_loss.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_sync_loss.ts` (39 KiB each) are also
**intentionally invalid**. A clean 2-second stream is damaged in four places:
a corrupted sync byte, one inserted byte, one deleted byte and a 1000-byte
block of garbage with false sync bytes; it ends with a 100-byte partial packet.
The analyzer reports four losses, each recovered, with their byte offsets and
the skipped bytes, and continues the analysis after each
([TS synchronization](../cli/usage.md#ts-synchronization-always-on)).

```bash
go run ./tools/generate_sync_loss -output sample_data/sample_188byte_sync_loss.ts
go run ./tools/generate_sync_loss -m2ts -output sample_data/sample_192byte_sync_loss.ts
go run ./tools/generate_sync_loss -trailer -output sample_data/sample_204byte_sync_loss.ts
./mpeg-ts-analyzer sample_data/sample_188byte_sync_loss.ts
make gui-run GUI_ARGS='sample_data/sample_188byte_sync_loss.ts'
```

The generator, the position of each defect, the expected results and the
TSDuck cross-check are described in
[`tools/generate_sync_loss`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_sync_loss).
Tests check that the committed files equal the generator output and pin the
results for both packet sizes; the GUI's tests use the CLI's analysis of the
188-byte file (`gui/tests/fixtures/analysis_sync_loss_188.json`), which `make
gui-test` compares with the output of the CLI built from the tree.

## PCR discontinuity sample

`sample_data/sample_188byte_pcr_discontinuities.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_pcr_discontinuities.ts` (48 KiB each) are
**intentionally invalid**. Their PCR, one every 40 ms of a constant 75.2 kbit/s
multiplex, passes through every kind of PCR step the analyzer distinguishes: a
33-bit wraparound, two declared discontinuities (one declared on an earlier
PCR-less packet), an undeclared backward step, an undeclared forward jump of
4 s and a `PCR_PID` change. Max PCR interval stays `OK` (40 ms) while
PCR discontinuities is `NG`; see [PCR steps](../cli/usage.md#pcr-steps).

```bash
go run ./tools/generate_pcr_discontinuities -output sample_data/sample_188byte_pcr_discontinuities.ts
go run ./tools/generate_pcr_discontinuities -m2ts -output sample_data/sample_192byte_pcr_discontinuities.ts
./mpeg-ts-analyzer sample_data/sample_188byte_pcr_discontinuities.ts --dump-pcr-jitter --dump-bitrate
```

The generator, the expected results and the TSDuck cross-check are described
in
[`tools/generate_pcr_discontinuities`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_pcr_discontinuities).
The GUI's Overview tests use the CLI's analysis of the 188-byte file
(`gui/tests/fixtures/analysis_pcr_discontinuities_188.json`), which
`make gui-test` compares with the output of the CLI built from the tree.

## Transport-error and scrambling sample

`sample_data/sample_188byte_transport_errors.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_transport_errors.ts` (19 KiB each) are also
**intentionally invalid**. They carry packets with `transport_error_indicator`
set and scrambled packets on the PAT, PMT, PCR, elementary-stream,
unreferenced and null PIDs, each built so that using its data would show as a
wrong PTS, PCR or table, to test how the analyzer counts them and keeps their
data out of the analysis.

```bash
go run ./tools/generate_transport_errors -output sample_data/sample_188byte_transport_errors.ts
go run ./tools/generate_transport_errors -m2ts -output sample_data/sample_192byte_transport_errors.ts
./mpeg-ts-analyzer sample_data/sample_188byte_transport_errors.ts
```

`sample_data/sample_188byte_transport_errors_malformed.ts` and
`sample_data/sample_192byte_transport_errors_malformed.ts` are the same streams
with each transport-error packet also malformed after its 4-byte header (a
reserved `adaptation_field_control`, or adaptation-field lengths that run past
the packet). Since only that header of such a packet is read, they give the
same results.

```bash
go run ./tools/generate_transport_errors -malformed -output sample_data/sample_188byte_transport_errors_malformed.ts
go run ./tools/generate_transport_errors -malformed -m2ts -output sample_data/sample_192byte_transport_errors_malformed.ts
```

The packets, the expected results and the TSDuck cross-check are described in
[`tools/generate_transport_errors`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_transport_errors).

## PAT/PMT health sample

`sample_data/sample_188byte_psi_health.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_psi_health.ts` (28 KiB each) are also
**intentionally invalid**. Two programs share one video PID; the stream has
no PAT for 1.2 s, a PMT section on the PAT PID, a PMT with an invalid CRC_32
and a version change, and a two-section PMT whose second section is never
sent, so every table of the PAT/PMT health check is `NG` for a different
reason.

```bash
go run ./tools/generate_psi_health -output sample_data/sample_188byte_psi_health.ts
go run ./tools/generate_psi_health -m2ts -output sample_data/sample_192byte_psi_health.ts
./mpeg-ts-analyzer sample_data/sample_188byte_psi_health.ts --program 1
```

The generator also writes one focused scenario at a time (`-scenario
pat-late`, `pmt-missing`, `crc`, `no-pcr`, ...), which the tests use. The
scenarios, the expected results and the TSDuck cross-check are described in
[`tools/generate_psi_health`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_psi_health).

## PID-reference sample

`sample_data/sample_188byte_pid_references.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_pid_references.ts` (120 KiB each, 16 s) are also
**intentionally invalid**. Their PSI layout references an audio PID that is
silent for 6.1 s, an audio PID that never carries a packet, a private-data PID
that never carries a packet, and streams that a PMT change adds and removes;
other packets are on PIDs that nothing references. The capture starts in the
middle of the stream, before the first PAT, to test that acquisition and the
PMT change cause no false finding.

```bash
go run ./tools/generate_pid_references -output sample_data/sample_188byte_pid_references.ts
go run ./tools/generate_pid_references -m2ts -output sample_data/sample_192byte_pid_references.ts
./mpeg-ts-analyzer sample_data/sample_188byte_pid_references.ts
```

The packets, the expected results and the TSDuck cross-check are described in
[`tools/generate_pid_references`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_pid_references).

## PES header and PTS repetition sample

`sample_data/sample_188byte_pes_health.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_pes_health.ts` (37 KiB each) are also
**intentionally invalid**. They carry PES headers that are truncated or have a
bad start code, bad flags, inconsistent lengths or bad PTS marker bits, a
valid header split over two TS packets, `PES_packet_length` 0 on video, and a
video stream without a PTS for 1080 ms, to test the PES header and PTS
repetition checks.

```bash
go run ./tools/generate_pes_health -output sample_data/sample_188byte_pes_health.ts
go run ./tools/generate_pes_health -m2ts -output sample_data/sample_192byte_pes_health.ts
./mpeg-ts-analyzer sample_data/sample_188byte_pes_health.ts
```

The defects, the expected results and the TSDuck and ffprobe cross-checks are
described in
[`tools/generate_pes_health`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_pes_health).

## Packet-gap sample

`sample_data/sample_188byte_packet_gaps.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_packet_gaps.ts` (147 KiB and 150 KiB, 4 s) are a
**poorly interleaved** remux for the report-only packet gaps: audio arrives in
bursts of three packets every 320 ms, one burst is left out around a declared
PCR discontinuity, a PCR-only PID carries the PCR, and a private-data stream
is removed by one PMT change and added back by a later one. An SDT PID and a
single-packet PID 0x0012 complete it. The capture starts before the first PAT.

```bash
go run ./tools/generate_packet_gaps -output sample_data/sample_188byte_packet_gaps.ts
go run ./tools/generate_packet_gaps -m2ts -output sample_data/sample_192byte_packet_gaps.ts
go run ./tools/generate_packet_gaps -trailer -output sample_data/sample_204byte_packet_gaps.ts
./mpeg-ts-analyzer sample_data/sample_188byte_packet_gaps.ts
```

The packets, the expected gaps and the cross-check are described in
[`tools/generate_packet_gaps`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_packet_gaps).

## Mux lead sample

`sample_data/sample_188byte_mux_lead.ts` and its 192-byte M2TS twin
`sample_data/sample_192byte_mux_lead.ts` (44 and 45 KiB) have known mux leads: a
remux with a large audio preload (1500 ms against 700 to 708 ms of video), an
AAC stream with one PES packet multiplexed 20 ms after its PTS and one without
a PTS, a PCR and PTS/DTS wraparound and a declared PCR discontinuity between a
PES packet and the next PCR, to test the report-only mux lead. They also carry
a transport error on one audio packet, and the 1500 ms lead fails the
PCR-PTS/DTS gap check.

```bash
go run ./tools/generate_mux_lead -output sample_data/sample_188byte_mux_lead.ts
go run ./tools/generate_mux_lead -m2ts -output sample_data/sample_192byte_mux_lead.ts
./mpeg-ts-analyzer sample_data/sample_188byte_mux_lead.ts
```

The layout, the expected results and the TSDuck cross-check are described in
[`tools/generate_mux_lead`](https://github.com/small-teton/mpeg-ts-analyzer/tree/master/tools/generate_mux_lead).
