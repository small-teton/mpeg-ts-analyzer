//! Plain-language explanations of the Overview transport-health checks, kept
//! in one table so the table rows, the check detail pages and the findings
//! list all show the same text. The thresholds are the CLI's
//! (internal/analysis/pcr.go, tsparser/compliance.go and tsparser/pcr_jitter.go);
//! the GUI only explains
//! them.
//!
//! A new check is one [`Check`] variant, one [`HELP`] entry (which also places
//! it in the table) and one arm in `overview::row`.

use super::Screen;

/// A group of the Transport health table, in table order.
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord)]
pub enum Category {
    Timing,
    PesTimestamps,
    Integrity,
}

impl Category {
    /// The categories in table order.
    #[cfg(test)]
    pub const ALL: [Category; 3] = [
        Category::Timing,
        Category::PesTimestamps,
        Category::Integrity,
    ];

    pub fn title(self) -> &'static str {
        match self {
            Category::Timing => "Timing",
            Category::PesTimestamps => "PES / Timestamps",
            Category::Integrity => "Integrity",
        }
    }
}

/// One transport-health check of the Overview table.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Check {
    MaxPcrInterval,
    PcrDiscontinuities,
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
        let mut all = [Check::MaxPcrInterval; HELP.len()];
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
pub const HELP: [CheckHelp; 9] = [
    CheckHelp {
        check: Check::MaxPcrInterval,
        category: Category::Timing,
        title: "Max PCR interval",
        summary: "The longest time the program goes without a PCR.",
        measures: "How long the program goes without a PCR (program clock reference). The value is the largest difference between two consecutive comparable PCRs, read from the PCR values themselves; a stored file has no arrival times. A decoder needs a PCR at least every 100 ms to keep its clock locked to the encoder.",
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
        limitations: "Two consecutive PCRs are comparable when they come from the same PID, the second has no discontinuity_indicator, and it is 0 to 1000 ms later, counted across the 33-bit wraparound. Every other pair is a PCR discontinuity and is not measured; see PCR discontinuities. A step between 100 ms and 1000 ms is measured and fails: a file cannot tell a late PCR (ETSI TR 101 290 PCR_repetition_error) from a small undeclared jump. The first PCR of the range has no interval.",
        navigation: Some((
            Screen::Timeline,
            "Timeline plots every interval against the limit.",
        )),
    },
    CheckHelp {
        check: Check::PcrDiscontinuities,
        category: Category::Timing,
        title: "PCR discontinuities",
        summary: "PCR clock jumps, and whether the stream declared them.",
        measures: "How each PCR of the program relates to the one before it. A program may start a new time base, for example at a splice, but must signal it with discontinuity_indicator (ISO/IEC 13818-1, 2.4.3.5); an unsignalled jump makes a decoder's clock lose lock. Each PCR is one of: continuous (0 to 1000 ms later), wraparound (the 33-bit PCR counter wrapped to zero; a normal interval), declared (discontinuity_indicator on the PCR packet, or on an earlier packet of the PCR PID without a PCR), PCR PID change (a new PMT version moved the PCR), backward (smaller, without discontinuity_indicator) or forward jump (more than 1000 ms later, without discontinuity_indicator).",
        applies_to: "The selected program: the PCR PID named in its PMT, following PMT version changes.",
        value: "The number of undeclared discontinuities: backward steps and forward jumps. Current result gives the number of PCRs and each kind that occurred with its count, first offset and how far the PCR jumped there.",
        statuses: &[
            (
                "ok",
                "No undeclared discontinuity. Declared discontinuities, PCR PID changes and wraparounds are legal and only listed.",
            ),
            (
                "ng",
                "At least one PCR jumped backward, or more than 1000 ms forward, without discontinuity_indicator (ETSI TR 101 290 PCR_discontinuity_indicator_error).",
            ),
            ("skipped", "Not evaluated; see When it is skipped."),
        ],
        skipped: "Skipped when the range holds fewer than two PCRs of the program.",
        limitations: "A forward step of 100 to 1000 ms without discontinuity_indicator is not counted here: a file cannot tell it from a PCR that arrived late, so it is measured as a PCR interval and fails Max PCR interval instead. Every discontinuity also splits the PCR jitter estimate, the bitrate time base and the elapsed time of the plots into segments, and no PCR → PTS/DTS gap is measured across one.",
        navigation: Some((
            Screen::Timeline,
            "Timeline plots every measured PCR interval; its time axis stops at each discontinuity, so no interval is drawn across one.",
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

    /// The table order: Timing, PES / Timestamps, Integrity, with each
    /// category's checks together.
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
                Check::MaxPcrInterval,
                Check::PcrDiscontinuities,
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
        let discontinuities = text(Check::PcrDiscontinuities);
        for phrase in [
            "discontinuity_indicator",
            "wraparound",
            "1000 ms",
            "PCR_discontinuity_indicator_error",
        ] {
            assert!(discontinuities.contains(phrase), "{phrase}");
        }
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
    }
}
