# Desktop GUI

[Documentation home](../index.md)

A desktop GUI is under active development in [`gui/`](../../gui). It runs the CLI
as a subprocess and connects the Overview, Timeline, Bitrate, and Packets views
to the same analysis results.

Packages for macOS, Linux and Windows are described in
[Installation](../getting-started/installation.md#desktop-gui).

<p align="center">
  <a href="../images/gui-tabs.png">
    <img src="../images/gui-tabs.png" alt="The four GUI views: Overview and Timeline on the top row, Bitrate and Packets on the bottom row" width="100%">
  </a>
</p>

- **Overview** brings the detected program layout, stream properties, timing
  compliance verdicts, continuity errors, PCR jitter, and per-stream bitrate
  into one screen.
- **Timeline** plots the PCR interval and the PCR → PTS/DTS gap of every
  elementary stream against their limits, with continuity and PAT/PMT events.
- **Bitrate** shows the bitrate of each PID over the range, the PCR jitter and
  its histogram.
- **Packets** pages through the packets of the range and decodes the selected
  one, with the PES or section it belongs to.

## Inspect packets down to individual bits

Select any decoded TS, adaptation-field, PSI, or PES field to highlight both
its source bytes and its exact bit range. Multi-byte, non-byte-aligned fields
are shown across byte boundaries; the 13-bit PID in the packet view spans the
low five bits of one byte and all eight bits of the next byte.

<p align="center">
  <a href="../images/gui-inspectors.png">
    <img src="../images/gui-inspectors.png" alt="Byte and Bit inspectors with the 13-bit PID 0x1001 selected across bytes 1 and 2" width="70%">
  </a>
</p>

The Bit inspector follows packet/file order from top to bottom and displays
each byte from MSB (`bit 7`) to LSB (`bit 0`), making it straightforward to
compare raw packet data with ISO/IEC 13818-1 field layouts. The close-up above
is the same Byte and Bit inspector shown in Packets: the PID `0x1001` of a PMT
packet is selected, so its bytes are highlighted and its 13 bits are marked
across the two bytes they span.

Both images are rendered from fixtures by the GUI's tests rather than captured
by hand; see [README screenshots](../development/index.md#readme-screenshots).
