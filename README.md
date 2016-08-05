# MpegTsAnalyzer

MpegTsAnalyzer is the Analyzer of MPEG2 Transport Stream(ISO_IEC_13818-1).
It can parse TS header, Adaptation Field, PSI(PAT/PMT) and PES header. Then, it can check continuity_counter(TS header), CRC32(PSI). 


# Usage

Default, it is dump each timestamps(PCR/PTS/DTS) that include PCR interval and PTS PCR gap. If you want to dump more detail, please add each command line flags.

```
usage: main.exe [<flags>] <input>

Flags:
      --help                   Show context-sensitive help (also try --help-long
                               and --help-man).
      --dump-ts-header         Dump TS packet header.
      --dump-ts-payload        Dump TS packet payload binary.
      --dump-adaptation-field  Dump TS packet adaptation_field detail.
      --dump-psi               Dump PSI(PAT/PMT) detail.
  -n, --not-dump-timestamp     Not Dump PCR/PTS/DTS timestamps.

Args:
  <input>  Input file name.
```
