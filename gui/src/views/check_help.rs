//! Plain-language explanations of the Overview transport-health checks, kept
//! in one table so the table rows, the check detail pages and the findings
//! list all show the same text. The thresholds are the CLI's
//! (tsparser/compliance.go and tsparser/pcr_jitter.go); the GUI only explains
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
    PcrJitter,
    PcrPtsDtsGap,
    TimestampAnomalies,
    ContinuityCounter,
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
pub const HELP: [CheckHelp; 6] = [
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
                Check::PcrJitter,
                Check::PcrPtsDtsGap,
                Check::TimestampAnomalies,
                Check::ContinuityCounter,
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
    }
}
