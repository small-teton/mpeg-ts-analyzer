# mpeg-ts-analyzer

mpeg-ts-analyzer is the Analyzer of MPEG2 Transport Stream(ISO_IEC_13818-1).
It can parse TS header, Adaptation Field, PSI(PAT/PMT) and PES header. Then, it can check continuity_counter(TS header), CRC32(PSI). 


# Usage

Default, it is dump each timestamps(PCR/PTS/DTS) that include PCR interval and PTS PCR gap. If you want to dump more detail, please add each command line flags.

```
usage: mpeg-ts-analyzer [<flags>] [<input>]


Flags:
      --[no-]help             Show context-sensitive help (also try --help-long and --help-man).
      --[no-]dump-ts-header   Dump TS packet header.
      --[no-]dump-ts-payload  Dump TS packet payload binary.
      --[no-]dump-adaptation-field  
                              Dump TS packet adaptation_field detail.
      --[no-]dump-psi         Dump PSI(PAT/PMT) detail.
      --[no-]dump-pes-header  Dump PES packet header detail.
  -t, --[no-]dump-timestamp   Dump PCR/PTS/DTS timestamps.
      --[no-]version          Show app version.

Args:
  [<input>]  Input file name.
```

# Result Examples

## No option

```
$ ./mpeg-ts-analyzer ColorBar_4Mbps_1280x720_2997p.m2t
Input file:  ColorBar_4Mbps_1280x720_2997p.m2t
Detected PAT: PMT pid = 0x100
Detected PMT
PMT : Program Info : elementary_PID	: 0x200, stream_type : 0x1b (AVC video stream as defined in ITU-T Rec. H.264|ISO/IEC 14496-10 Video)
PMT : Program Info : elementary_PID	: 0x201, stream_type : 0x11 (14496-3 audio with LATM transport syntax as defined in ISO/IEC 14496-3 / AMD 1)
```

## Dump TS header

```
$ ./mpeg-ts-analyzer ColorBar_4Mbps_1280x720_2997p.m2t --dump-ts-header 
Input file:  ColorBar_4Mbps_1280x720_2997p.m2t
===============================================================
 TS Header
===============================================================
transport_error_indicator	: 0
payload_unit_start_indicator	: 1
transport_priority		: 1
pid				: 0x0
transport_scrambling_control	: 0
adaptation_field_control	: 1
continuity_counter		: 0
===============================================================
 TS Header
===============================================================
transport_error_indicator	: 0
payload_unit_start_indicator	: 1
transport_priority		: 1
pid				: 0x100
transport_scrambling_control	: 0
adaptation_field_control	: 1
continuity_counter		: 0
===============================================================
 TS Header
===============================================================
transport_error_indicator	: 0
payload_unit_start_indicator	: 0
transport_priority		: 0
pid				: 0x101
transport_scrambling_control	: 0
adaptation_field_control	: 2
continuity_counter		: 0
===============================================================
 TS Header
===============================================================
transport_error_indicator	: 0
payload_unit_start_indicator	: 1
transport_priority		: 0
pid				: 0x200
transport_scrambling_control	: 0
adaptation_field_control	: 1
continuity_counter		: 0
```

## Dump

```
$ ./mpeg-ts-analyzer ColorBar_4Mbps_1280x720_2997p.m2t --dump-ts-payload
Input file:  ColorBar_4Mbps_1280x720_2997p.m2t
===============================================================
 Dump TS Data
===============================================================

 1: 47 60 00 10 00 00 b0 0d 00 00 c1 00 00 00 01 e1 00 b3 58 82 
 2: b7 ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 3: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 4: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 5: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 6: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 7: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 8: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 9: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
10: ff ff ff ff ff ff ff ff 
===============================================================
 Dump TS Data
===============================================================

 1: 47 61 00 10 00 02 b0 17 00 01 c1 00 00 e1 01 f0 00 1b e2 00 
 2: f0 00 11 e2 01 f0 00 c5 82 fb 7e ff ff ff ff ff ff ff ff ff 
 3: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 4: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 5: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 6: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 7: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 8: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
 9: ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff ff 
10: ff ff ff ff ff ff ff ff 
```

## Dump adaptation field

```
$ ./mpeg-ts-analyzer ColorBar_4Mbps_1280x720_2997p.m2t --dump-adaptation-field
Input file:  ColorBar_4Mbps_1280x720_2997p.m2t

===========================================
 Adaptation Field
===========================================
Adaptation Field : adaptation_field_length                      : 183
Adaptation Field : discontinuity_indicator                      : 0
Adaptation Field : random_access_indicator                      : 0
Adaptation Field : elementary_stream_priority_indicator         : 0
Adaptation Field : PCR_flag                                     : 1
Adaptation Field : OPCR_flag                                    : 0
Adaptation Field : splicing_point_flag                          : 0
Adaptation Field : adaptation_field_extension_flag              : 0
Adaptation Field : program_clock_reference_base                 : 45
Adaptation Field : program_clock_reference_extension            : 185
Adaptation Field : PCR 0x3575[0.506852ms]

===========================================
 Adaptation Field
===========================================
Adaptation Field : adaptation_field_length                      : 8
Adaptation Field : discontinuity_indicator                      : 0
Adaptation Field : random_access_indicator                      : 0
Adaptation Field : elementary_stream_priority_indicator         : 0
Adaptation Field : PCR_flag                                     : 0
Adaptation Field : OPCR_flag                                    : 0
Adaptation Field : splicing_point_flag                          : 0
Adaptation Field : adaptation_field_extension_flag              : 0
```

## Dump PSI

```
$ ./mpeg-ts-analyzer ColorBar_4Mbps_1280x720_2997p.m2t --dump-psi
Input file:  ColorBar_4Mbps_1280x720_2997p.m2t
Detected PAT: PMT pid = 0x100

===========================================
 PAT
===========================================
PAT : table_id                  : 0x0
PAT : section_syntax_indicator  : 1
PAT : section_length            : 13
PAT : transport_stream_id       : 0
PAT : version_number            : 0
PAT : current_next_indicator    : 1
PAT : section_number            : 0
PAT : last_section_number       : 0
PAT : program_number            : 1
PAT : program_map_PID           : 0x100
PAT : CRC_32                    : b35882b7
Detected PMT

===========================================
 PMT
===========================================
PMT : table_id                  : 0x2
PMT : section_syntax_indicator  : 1
PMT : section_length            : 23
PMT : program_number            : 1
PMT : version_number            : 0
PMT : current_next_indicator    : 1
PMT : section_number            : 0
PMT : last_section_number       : 0
PMT : PCR_PID                   : 0x101
PMT : program_info_length       : 0
PMT : Program Info : elementary_PID     : 0x200, stream_type : 0x1b (AVC video stream as defined in ITU-T Rec. H.264|ISO/IEC 14496-10 Video)
PMT : Program Info : elementary_PID     : 0x201, stream_type : 0x11 (14496-3 audio with LATM transport syntax as defined in ISO/IEC 14496-3 / AMD 1)
PMT : CRC_32                    : c582fb7e
```

## Dump timestamp

```
$ ./mpeg-ts-analyzer ColorBar_4Mbps_1280x720_2997p.m2t --dump-timestamp
Input file:  ColorBar_4Mbps_1280x720_2997p.m2t
Detected PAT: PMT pid = 0x100
Detected PMT
PMT : Program Info : elementary_PID     : 0x200, stream_type : 0x1b (AVC video stream as defined in ITU-T Rec. H.264|ISO/IEC 14496-10 Video)
PMT : Program Info : elementary_PID     : 0x201, stream_type : 0x11 (14496-3 audio with LATM transport syntax as defined in ISO/IEC 14496-3 / AMD 1)
0x00000178 PCR: 0x00003575[00000.506852ms] (Interval:00000.506852ms)
0x00000814 PTS: 0x00013554[00879.866638ms] (pid:0x201) (delay:877.078979ms)
0x00001028 PTS: 0x00013cd4[00901.200012ms] (pid:0x201) (delay:895.624634ms)
0x00001b2c PTS: 0x00014454[00922.533325ms] (pid:0x201) (delay:913.156555ms)
0x00000234 DTS: 0x00012999[00846.500000ms] (pid:0x200) (delay:845.739746ms)
0x00002340 PTS: 0x00014bd4[00943.866638ms] (pid:0x201) (delay:931.702209ms)
0x00002574 DTS: 0x00013554[00879.866638ms] (pid:0x200) (delay:866.941895ms)
0x00002a98 PTS: 0x00015354[00965.200012ms] (pid:0x201) (delay:950.501282ms)
0x000032ac PTS: 0x00015ad4[00986.533325ms] (pid:0x201) (delay:969.046936ms)
0x000031f0 DTS: 0x0001410f[00913.233337ms] (pid:0x200) (delay:896.000366ms)
0x00003cf4 DTS: 0x00014cca[00946.599976ms] (pid:0x200) (delay:925.565613ms)
0x000058dc PCR: 0x000ca24c[00030.665926ms] (Interval:00030.159074ms)
0x00003a04 PTS: 0x00016254[01007.866638ms] (pid:0x201) (delay:987.846008ms)
0x000055ec DTS: 0x00015885[00979.966675ms] (pid:0x200) (delay:950.314514ms)

(Partially omitted)

0x01917fdc PCR: 0x3915c1de[35471.377704ms] (Interval:00030.159074ms)
0x01912644 PTS: 0x003157d4[35930.535156ms] (pid:0x201) (delay:490.078125ms)
0x0191d740 PCR: 0x39222eb5[35501.536778ms] (Interval:00030.159074ms)
0x0191a1a4 PTS: 0x00315f54[35951.867188ms] (pid:0x201) (delay:468.832031ms)
-----------------------------
Max PCR interval: 30.412519ms
PCR-PTS max gap: 990.001953ms
```
