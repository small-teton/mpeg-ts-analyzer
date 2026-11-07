//! Plain-language explanations of the Overview transport-health checks, kept
//! in one table so the table rows, the check detail pages and the findings
//! list all show the same text. The thresholds are the CLI's
//! (tsparser/compliance.go, tsparser/pcr_jitter.go and
//! internal/analysis/psi_health.go); the GUI only explains them.
//!
//! A new check is one [`Check`] variant, one [`HELP`] entry (which also places
//! it in the table) and one arm in `overview::row`.

use super::Screen;

/// A group of the Transport health table, in table order.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub enum Category {
    Structure,
    Timing,
    PesTimestamps,
    Integrity,
}

impl Category {
    /// The categories in table order.
    #[cfg(test)]
    pub const ALL: [Category; 4] = [
        Category::Structure,
        Category::Timing,
        Category::PesTimestamps,
        Category::Integrity,
    ];

    pub fn title(self) -> &'static str {
        match self {
            Category::Structure => "Structure",
            Category::Timing => "Timing",
            Category::PesTimestamps => "PES / Timestamps",
            Category::Integrity => "Integrity",
        }
    }
}

/// One transport-health check of the Overview table.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Check {
    PatHealth,
    PmtHealth,
    MaxPcrInterval,
    PcrJitter,
    PcrPtsDtsGap,
    TimestampAnomalies,
    ContinuityCounter,
    TransportErrors,
    ScrambledPackets,
    Synchronization,
}

impl Check {
    /// The checks in table order: the order of [`HELP`].
    pub const ALL: [Check; HELP.len()] = {
        let mut all = [Check::PatHealth; HELP.len()];
        let mut index = 0;
        while index < HELP.len() {
            all[index] = HELP[index].check;
            index += 1;
        }
        all
    };

    pub fn help(self) -> &'static CheckHelp {
        HELP.iter()
            .find(|help| help.check == self)
            .expect("every check has an explanation")
    }

    pub fn title(self) -> &'static str {
        self.help().title
    }

    pub fn category(self) -> Category {
        self.help().category
    }

    /// Accessible name and tooltip of the check's information button.
    pub fn about(self) -> String {
        format!("About {}", self.title())
    }

    /// The meaning of `status` for this check, if this GUI knows the status.
    pub fn verdict_meaning(self, status: &str) -> Option<&'static str> {
        self.help()
            .statuses
            .iter()
            .find(|(known, _)| *known == status)
            .map(|(_, meaning)| *meaning)
    }
}

/// What a check measures and how to read its result.
pub struct CheckHelp {
    pub check: Check,
    pub category: Category,
    pub title: &'static str,
    /// One line under the title of the detail page.
    pub summary: &'static str,
    pub measures: &'static str,
    pub applies_to: &'static str,
    pub value: &'static str,
    /// Every status the CLI can report, with its meaning.
    pub statuses: &'static [(&'static str, &'static str)],
    pub skipped: &'static str,
    pub limitations: &'static str,
    /// A screen with the details, and what it shows.
    pub navigation: Option<(Screen, &'static str)>,
}

/// The explanations in table order: grouped by [`Category`], in
/// [`Category::ALL`] order.
pub const HELP: [CheckHelp; 10] = [
    CheckHelp {
        check: Check::PatHealth,
        category: Category::Structure,
        title: "PAT health",
        summary: "Whether the PAT is found, repeats at least every 500 ms and can be read.",
        measures: "Whether the program association table (PAT) is found on PID 0x0000 and how often it repeats. A PAT counts when every section of one version arrives valid (CRC_32, syntax), at the packet that completes it; sections are collected exactly as the analysis collects them. The time of each PAT is interpolated by byte position between the PCRs around it, and the time between two consecutive PATs (a complete interval) is compared with 500 ms. The time from the start of a PCR segment to the first PAT, and from the last PAT to the end of the segment, are open intervals: lower bounds of the real gap, also compared with 500 ms. Sections that cannot be used are counted: an invalid CRC_32, a malformed section, sections discarded before the table completed, a table_id other than 0x00 on PID 0x0000, and packets not read because of transport_error_indicator or scrambling.",
        applies_to: "The whole multiplex: PID 0x0000, over every packet of the range from its first packet. Times use the PCR of the selected program in a single-program run, or of the first analyzed program that has a PCR in the GUI's analysis.",
        value: "The longest time without a PAT in milliseconds, complete or open. Current result gives the number of PATs, the complete and late intervals, version changes, every problem count, the first problem offset and the PCR time base.",
        statuses: &[
            (
                "ok",
                "The PAT repeats: at least one complete interval was measured, none is longer than 500 ms (ETSI TR 101 290 1.3.a), and every section could be used.",
            ),
            (
                "ng",
                "At least one reason: late (an interval longer than 500 ms), missing, crc_error, malformed_section, incomplete, wrong_table_id or unusable_packets. Current result lists them with their counts.",
            ),
            (
                "insufficient_data",
                "No problem was found, but the range does not show the PAT repeating: no complete interval within one PCR segment, or no PCR time base. Listed under Unavailable analysis, not as a finding.",
            ),
        ],
        skipped: "This check is never skipped while a program is analyzed. Without a PCR time base the repetition is not measured, and a PAT without any problem is insufficient_data.",
        limitations: "ISO/IEC 13818-1 defines the PAT on PID 0x0000 with table_id 0x00, its section syntax and CRC_32, but no repetition interval: 500 ms is the value of ETSI TR 101 290, a DVB measurement guideline. Stricter broadcast profiles are not applied, so a stream that passes may still fail one. A file has no packet-arrival times; time comes from PCRs and byte position. A version change does not restart the intervals. A continuity counter error inside a section is reported by Continuity counter; the PAT it breaks shows as a longer interval.",
        navigation: Some((
            Screen::Packets,
            "Packets lists every PAT packet at its offset.",
        )),
    },
    CheckHelp {
        check: Check::PmtHealth,
        category: Category::Structure,
        title: "PMT health",
        summary: "Whether the program's PMT is found, repeats at least every 500 ms and can be read.",
        measures: "Whether the program map table (PMT) of the program is found on the PID the PAT assigns to it, and how often it repeats. A PMT counts when every section of one version arrives valid (CRC_32, syntax), at the packet that completes it. Its time is interpolated by byte position between PCRs, and the time between two consecutive PMTs (a complete interval) is compared with 500 ms. The time from the first PCR after the PAT announced the program, or from the start of a PCR segment, to the first PMT, and from the last PMT to the end of the segment, are open intervals: lower bounds of the real gap, also compared with 500 ms. Sections that cannot be used are counted: an invalid CRC_32, a malformed section, sections discarded before the table completed (for example a section of a multi-section PMT that never arrives), and packets not read because of transport_error_indicator or scrambling.",
        applies_to: "The selected program's PMT, from the packet after the PAT that announced the program, over the rest of the range. A PMT that a later PAT moves to another PID is followed there, and its intervals restart. The CLI reports the PMT of every announced program; select a program to see its row.",
        value: "The longest time without the PMT in milliseconds, complete or open. Current result gives the number of PMTs, the complete and late intervals, version changes, every problem count, the first problem offset and the PCR time base.",
        statuses: &[
            (
                "ok",
                "The PMT repeats: at least one complete interval was measured, none is longer than 500 ms (ETSI TR 101 290 1.5.a), and every section could be used.",
            ),
            (
                "ng",
                "At least one reason: missing (no PMT while the program was followed for more than 500 ms), late (an interval longer than 500 ms), crc_error, malformed_section, incomplete or unusable_packets. Current result lists them with their counts.",
            ),
            (
                "insufficient_data",
                "No problem was found, but the range does not show the PMT repeating: no complete interval within one PCR segment, or no PCR time base. Listed under Unavailable analysis, not as a finding.",
            ),
        ],
        skipped: "This check is never skipped while a program is analyzed. Without a PCR time base the repetition is not measured, and a PMT without any problem is insufficient_data, even one that was never found.",
        limitations: "ISO/IEC 13818-1 defines the PMT with table_id 0x02 on the PID the PAT assigns, its section syntax and CRC_32, but no repetition interval: 500 ms is the value of ETSI TR 101 290, a DVB measurement guideline. Stricter broadcast profiles are not applied. A file has no packet-arrival times; time comes from PCRs and byte position. A version change does not restart the intervals. A section that cannot be attributed to one program of a PID shared by several PMTs counts for each of them.",
        navigation: Some((
            Screen::Packets,
            "Packets lists every PMT packet at its offset.",
        )),
    },
    CheckHelp {
        check: Check::MaxPcrInterval,
        category: Category::Timing,
        title: "Max PCR interval",
        summary: "The longest time the program goes without a PCR.",
        measures: "How long the program goes without a PCR (program clock reference). The value is the largest difference between two consecutive PCRs, read from the PCR values themselves. A decoder needs a PCR at least every 100 ms to keep its clock locked to the encoder.",
        applies_to: "The selected program: the PCR PID named in its PMT.",
        value: "The longest interval in milliseconds. Current result gives the limit and how many intervals were measured (observations).",
        statuses: &[
            (
                "ok",
                "Every measured interval is 100 ms or less (ISO/IEC 13818-1). Exactly 100 ms is OK.",
            ),
            ("ng", "At least one interval is longer than 100 ms."),
            ("skipped", "Not measured; see When it is skipped."),
        ],
        skipped: "Skipped when the range holds fewer than two comparable PCRs: no PCR, a single PCR, or PCRs that are all separated by discontinuities.",
        limitations: "No interval is measured across a PCR packet with discontinuity_indicator set, or across an undeclared backward jump of the clock. A 33-bit wraparound is measured as a normal interval. An undeclared forward jump is measured as is, because a file cannot tell it apart from a genuinely late PCR.",
        navigation: Some((
            Screen::Timeline,
            "Timeline plots every interval against the limit.",
        )),
    },
    CheckHelp {
        check: Check::PcrJitter,
        category: Category::Timing,
        title: "PCR jitter estimate",
        summary: "Estimated from byte position; a stored TS has no packet-arrival timestamps.",
        measures: "An estimate of how far each PCR value is from the time its packet would be delivered. A file has no packet-arrival timestamps, so the byte position stands in for time (the byte_position model): each PCR is compared with the value interpolated from its two neighbors by byte position. That is exact only when the stream is delivered at a constant rate, so the CLI first assesses the delivery from the stream itself. It groups the PCR intervals into windows of at least 1 s of PCR time and compares their multiplex rates: delivery is constant when every window is within ±1 % of the average rate and no estimated jitter exceeds 1 ms, variable when a window deviates more or the estimate reaches milliseconds (no PCR error is that large; only a varying rate explains it), and unknown with fewer than two windows.",
        applies_to: "The selected program: the PCR PID named in its PMT. The delivery windows count every packet of the multiplex.",
        value: "The worst absolute estimated jitter (the largest deviation either way). It is an estimate from the file, not a measurement of the PCRs as they arrived. Current result gives the delivery assessment and its evidence: the number of 1-second windows, the largest deviation of a window's rate from the average, and the average rate.",
        statuses: &[
            (
                "ok",
                "Delivery is constant and max |estimated jitter| is within ±500 ns (ISO/IEC 13818-1 PCR accuracy).",
            ),
            (
                "warning",
                "Delivery is constant; beyond ±500 ns but within ±25 µs (the tolerance DVB expects receivers to handle).",
            ),
            ("ng", "Delivery is constant; beyond ±25 µs."),
            (
                "not_assessable",
                "Delivery is variable or unknown, so the estimate is shown but not graded. Listed under Unavailable analysis, not as a finding.",
            ),
            ("skipped", "Not estimated; see When it is skipped."),
        ],
        skipped: "Skipped with fewer than three PCRs, when no segment between discontinuities has three PCRs, or when more than 30% of the PCRs start a new segment. Skipped takes precedence over not assessable.",
        limitations: "This is not a direct hardware or TR 101 290 measurement. The delivery assessment can only support the constant-rate premise: a window rate within ±1 % does not rule out rate changes between two PCRs, so a graded estimate between 25 µs and 1 ms can still come from the delivery rather than the PCR values. A range shorter than about two seconds of PCR time is not assessable. Neither the file extension nor null packets are taken as evidence of constant delivery.",
        navigation: Some((
            Screen::Bitrate,
            "Bitrate plots the estimated jitter over time and its histogram.",
        )),
    },
    CheckHelp {
        check: Check::PcrPtsDtsGap,
        category: Category::PesTimestamps,
        title: "PCR → PTS/DTS gap",
        summary: "How far the PES timestamps run ahead of the program clock.",
        measures: "How far ahead of the program clock the PES timestamps are, which is how long data waits in the decoder's buffer. For each PES packet, its DTS (or PTS when it has no DTS) is compared with the PCR time at the packet's position, interpolated between the PCRs before and after it.",
        applies_to: "The selected program: every elementary stream of the program against its PCR PID.",
        value: "The largest gap in milliseconds. Current result gives the limit and how many timestamps were compared (observations).",
        statuses: &[
            (
                "ok",
                "Every gap is 1000 ms or less (ISO/IEC 13818-1). Exactly 1000 ms is OK.",
            ),
            (
                "ng",
                "At least one timestamp is more than 1000 ms ahead of its PCR.",
            ),
            ("skipped", "Not measured; see When it is skipped."),
        ],
        skipped: "Skipped when no parsed PTS/DTS has a PCR both before and after it in the range.",
        limitations: "The PCR time at a PES packet is interpolated by byte position between the surrounding PCRs. The last PES packets of the range, after the final PCR, are not compared.",
        navigation: Some((
            Screen::Timeline,
            "Timeline plots the gap of every elementary stream against the limit.",
        )),
    },
    CheckHelp {
        check: Check::TimestampAnomalies,
        category: Category::PesTimestamps,
        title: "PTS/DTS anomalies",
        summary: "Decode timestamps that go backward, wrap, jump or come after their PTS.",
        measures: "Whether each elementary stream's decode timestamps (DTS, or PTS when there is no DTS) advance as they should. Detected problems are timestamps going backward, a 33-bit wraparound, a forward jump much larger than the stream's frame interval, and a DTS later than its PTS.",
        applies_to: "The selected program: each of its elementary-stream PIDs separately.",
        value: "The number of anomalies found. Findings list each kind per PID with its first occurrence.",
        statuses: &[
            ("ok", "No anomaly was found."),
            ("anomalies", "At least one anomaly was found; see Findings."),
        ],
        skipped: "This check is never skipped; a stream without timestamps simply has no anomaly.",
        limitations: "Presentation order legitimately reorders PTS between B-frames, so only the decode timeline is checked. A forward jump is reported only when it is at least five frame intervals and more than 0.5 s longer than one frame interval.",
        navigation: Some((
            Screen::Timeline,
            "Timeline marks each anomaly at its position.",
        )),
    },
    CheckHelp {
        check: Check::ContinuityCounter,
        category: Category::Integrity,
        title: "Continuity counter",
        summary: "Lost, repeated or out-of-order packets on any PID.",
        measures: "Whether packets are missing, repeated or out of order. Each PID's 4-bit continuity_counter must increase by one for every packet with payload; a gap usually means lost packets.",
        applies_to: "The whole multiplex: every PID in the range except null packets, not only the selected program.",
        value: "The number of continuity errors over all PIDs. Findings list each error with its PID and position.",
        statuses: &[
            ("ok", "No continuity error was found."),
            (
                "errors",
                "At least one continuity error was found; see Findings.",
            ),
        ],
        skipped: "This check is never skipped.",
        limitations: "One legal duplicate of a packet is accepted, and the counter may jump at a packet with discontinuity_indicator set. Counting starts after the PAT and PMTs are complete, so the lead of the range is not checked.",
        navigation: Some((
            Screen::Timeline,
            "Timeline marks continuity errors on the program's PIDs.",
        )),
    },
    CheckHelp {
        check: Check::TransportErrors,
        category: Category::Integrity,
        title: "Transport errors",
        summary: "Packets marked as damaged (transport_error_indicator) on any PID.",
        measures: "How many packets have transport_error_indicator set. A demodulator or other upstream device sets this bit when a packet has at least one bit error it could not correct. Such a packet cannot be trusted, so the analysis does not use it: its PSI section, its PES payload and its PCR are left out. A PES packet in progress on the PID ends at the damaged packet and is read from the packets before it.",
        applies_to: "The whole multiplex: every packet of the range, including the packets read while the first PAT and PMT were acquired and null packets. The PID is the one in the damaged packet's own header, which the error may have changed.",
        value: "The number of packets with transport_error_indicator set. Current result gives how many of them carried data the analysis would have read (excluded), the first offset, and each PID with its count and first offset.",
        statuses: &[
            ("ok", "No packet has transport_error_indicator set."),
            (
                "affected",
                "Damaged packets were found, but none on a PID whose data the analysis reads (for example null packets or other programs). The other results are complete.",
            ),
            (
                "data_excluded",
                "At least one damaged packet was on the PAT, a PMT, the PCR PID or an elementary stream of an analyzed program. Its data was left out, so the other results are based on less data than the stream carried.",
            ),
        ],
        skipped: "This check is never skipped while a program is analyzed. When a PAT or PMT arrives only in damaged packets, it is never acquired: the analysis is incomplete and no check is reported.",
        limitations: "A damaged packet still counts for the continuity counter check with the counter in its header, and for the bitrate. A damaged packet whose header cannot be parsed at all interrupts the analysis as any malformed packet does, and the analysis restarts at the next PAT. Errors corrected upstream, or not flagged by the device, are invisible in the file.",
        navigation: Some((
            Screen::Packets,
            "Packets marks each packet with transport_error_indicator set as a transport error.",
        )),
    },
    CheckHelp {
        check: Check::ScrambledPackets,
        category: Category::Integrity,
        title: "Scrambled packets",
        summary: "Packets whose payload is scrambled (transport_scrambling_control).",
        measures: "How many packets have a non-zero transport_scrambling_control, which marks a payload encrypted by conditional access. A scrambled payload cannot be read without decryption, so its PSI section or PES payload is left out; a PES packet in progress on the PID ends at it. The header and adaptation field are never scrambled, so a PCR in a scrambled packet is still used.",
        applies_to: "The whole multiplex: every packet of the range, including the packets read while the first PAT and PMT were acquired and null packets.",
        value: "The number of scrambled packets. Current result gives how many of them carried a payload the analysis would have read (excluded), the first offset, and each PID with its count and first offset.",
        statuses: &[
            ("ok", "No packet is scrambled."),
            (
                "affected",
                "Scrambled packets were found, but none carried a payload the analysis reads (for example other programs, unreferenced PIDs or packets without payload). The other results are complete.",
            ),
            (
                "data_excluded",
                "At least one scrambled payload was on the PAT, a PMT or an elementary stream of an analyzed program. It was left out, so PES timestamps and PSI tracking are based on less data than the stream carried.",
            ),
        ],
        skipped: "This check is never skipped while a program is analyzed. A PAT or PMT that arrives only scrambled is never acquired: the analysis is incomplete and no check is reported.",
        limitations: "Scrambling is not an error in itself; an encrypted service is normal in a broadcast capture. The analyzer does not decrypt, and it does not tell even-key from odd-key packets. PAT and PMT are never scrambled in a valid stream, and neither are null packets.",
        navigation: Some((Screen::Packets, "Packets marks each scrambled packet.")),
    },
    CheckHelp {
        check: Check::Synchronization,
        category: Category::Integrity,
        title: "TS synchronization",
        summary: "Packets that do not start with the sync byte, and how the analysis realigned.",
        measures: "Whether every packet starts with the sync byte 0x47 (in 192-byte M2TS, after the 4-byte TP_extra_header). At a packet boundary with another value, synchronization is lost: the CLI then looks for the next offset where five consecutive packets, one packet size apart, start with 0x47, and continues the analysis from there. The bytes in between are skipped, never read as packets.",
        applies_to: "The whole multiplex: every packet from the start of the analysis, not only the selected program.",
        value: "The number of losses. Current result gives the realignments, the bytes skipped, the first loss and the largest skipped span with their offsets, and an incomplete packet at the end of the input. Every offset is a position in the file.",
        statuses: &[
            ("ok", "Every packet started with the sync byte."),
            (
                "recovered",
                "Synchronization was lost at least once and the analysis realigned after each loss; the skipped bytes were not analyzed.",
            ),
            (
                "lost",
                "Synchronization was lost and no new alignment was found, so the analysis stopped there and is incomplete.",
            ),
        ],
        skipped: "This check is never skipped. A file without any packet alignment is not analyzed at all; the analysis fails instead.",
        limitations: "A new alignment must start within 1 MiB of the loss; after that, or when fewer than five aligned packets remain, the analysis stops. A single corrupted sync byte counts as a loss and its packet is skipped. Packets lost in a skipped span usually also cause continuity errors, and timing measured across the span includes it. Counting starts after the PAT and PMTs are complete, so the lead of the range is not checked. An incomplete packet at the end of the input is not a loss.",
        navigation: Some((
            Screen::Packets,
            "Packets lists the packets of the range at the alignment of its start, so packets after a realignment can appear there as invalid.",
        )),
    },
];

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_check_has_a_complete_explanation() {
        for check in Check::ALL {
            let help = check.help();
            assert_eq!(help.check, check);
            for text in [
                help.title,
                help.summary,
                help.measures,
                help.applies_to,
                help.value,
                help.skipped,
                help.limitations,
            ] {
                assert!(!text.trim().is_empty(), "{check:?}");
            }
            assert!(help.statuses.iter().any(|(s, _)| *s == "ok"), "{check:?}");
            assert!(help.navigation.is_some(), "{check:?}");
        }
        // Each check exactly once.
        for (index, check) in Check::ALL.iter().enumerate() {
            assert!(!Check::ALL[..index].contains(check), "{check:?} twice");
        }
    }

    /// The table order: Structure, Timing, PES / Timestamps, Integrity, with
    /// each category's checks together.
    #[test]
    fn checks_are_grouped_in_category_order() {
        let categories: Vec<Category> = Check::ALL.iter().map(|c| c.category()).collect();
        assert!(categories.is_sorted(), "{categories:?}");
        for category in Category::ALL {
            assert!(categories.contains(&category), "{category:?} is empty");
        }
        assert!(Category::ALL.is_sorted());
        assert_eq!(
            Check::ALL,
            [
                Check::PatHealth,
                Check::PmtHealth,
                Check::MaxPcrInterval,
                Check::PcrJitter,
                Check::PcrPtsDtsGap,
                Check::TimestampAnomalies,
                Check::ContinuityCounter,
                Check::TransportErrors,
                Check::ScrambledPackets,
                Check::Synchronization,
            ]
        );
    }

    /// The thresholds the CLI uses, and the file-based model of PCR jitter.
    #[test]
    fn explanations_state_the_cli_thresholds_and_limitations() {
        let text = |check: Check| {
            let help = check.help();
            let statuses: Vec<&str> = help.statuses.iter().map(|(_, m)| *m).collect();
            [help.measures, help.value, help.limitations]
                .into_iter()
                .chain(statuses)
                .collect::<Vec<_>>()
                .join(" ")
        };
        assert!(text(Check::MaxPcrInterval).contains("100 ms"));
        assert!(text(Check::PcrPtsDtsGap).contains("1000 ms"));
        let sync = text(Check::Synchronization);
        for phrase in ["0x47", "five consecutive packets", "1 MiB", "skipped"] {
            assert!(sync.contains(phrase), "{phrase}");
        }
        assert!(
            Check::ContinuityCounter
                .help()
                .applies_to
                .contains("whole multiplex")
        );
        let jitter = text(Check::PcrJitter);
        for phrase in [
            "±500 ns",
            "±25 µs",
            "no packet-arrival timestamps",
            "byte position stands in for time",
            "delivered at a constant rate",
            "±1 %",
            "1 ms",
            "fewer than two windows",
            "not a direct hardware or TR 101 290 measurement",
        ] {
            assert!(jitter.contains(phrase), "{phrase}");
        }
        for status in ["skipped", "not_assessable"] {
            assert!(
                Check::PcrJitter.verdict_meaning(status).is_some(),
                "{status}"
            );
        }
        assert_eq!(Check::PcrJitter.verdict_meaning("indeterminate"), None);
        for check in [Check::PatHealth, Check::PmtHealth] {
            let help = text(check);
            for phrase in [
                "500 ms",
                "ETSI TR 101 290",
                "no repetition interval",
                "open intervals",
                "PCR time base",
            ] {
                assert!(
                    help.contains(phrase) || check.help().skipped.contains(phrase),
                    "{check:?}: {phrase}"
                );
            }
            assert!(check.verdict_meaning("insufficient_data").is_some());
        }
    }
}
