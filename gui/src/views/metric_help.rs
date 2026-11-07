//! Plain-language explanations of the five Overview compliance cards, kept in
//! one table so the cards, their information buttons and the findings list
//! all show the same text. The thresholds are the CLI's (tsparser/compliance.go
//! and tsparser/pcr_jitter.go); the GUI only explains them.

use eframe::egui;

use super::{Screen, badge, status_verdict};

/// One Overview compliance card.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Metric {
    MaxPcrInterval,
    PcrPtsDtsGap,
    TimestampAnomalies,
    ContinuityCounter,
    PcrJitter,
}

impl Metric {
    /// The cards in Overview order.
    #[cfg(test)]
    pub const ALL: [Metric; 5] = [
        Metric::MaxPcrInterval,
        Metric::PcrPtsDtsGap,
        Metric::TimestampAnomalies,
        Metric::ContinuityCounter,
        Metric::PcrJitter,
    ];

    pub fn help(self) -> &'static MetricHelp {
        HELP.iter()
            .find(|help| help.metric == self)
            .expect("every metric has an explanation")
    }

    pub fn title(self) -> &'static str {
        self.help().title
    }

    /// Accessible name and tooltip of the card's information button.
    pub fn about(self) -> String {
        format!("About {}", self.title())
    }
}

/// What a card shows and how to read it.
pub struct MetricHelp {
    pub metric: Metric,
    pub title: &'static str,
    /// Short note on the card when the CLI gives no reason of its own.
    pub note: &'static str,
    pub measures: &'static str,
    pub scope: &'static str,
    pub value: &'static str,
    /// Every status the CLI can report, with its meaning.
    pub statuses: &'static [(&'static str, &'static str)],
    pub skipped: &'static str,
    pub limitations: &'static str,
    /// A screen with the details, and what it shows.
    pub detail: Option<(Screen, &'static str)>,
}

pub const HELP: [MetricHelp; 5] = [
    MetricHelp {
        metric: Metric::MaxPcrInterval,
        title: "Max PCR interval",
        note: "longest PCR-to-PCR interval",
        measures: "How long the program goes without a PCR (program clock reference). The value is the largest difference between two consecutive PCRs, read from the PCR values themselves. A decoder needs a PCR at least every 100 ms to keep its clock locked to the encoder.",
        scope: "The selected program: the PCR PID named in its PMT.",
        value: "The longest interval in milliseconds. The note gives the limit and how many intervals were measured (observations).",
        statuses: &[
            (
                "ok",
                "Every measured interval is 100 ms or less (ISO/IEC 13818-1). Exactly 100 ms is OK.",
            ),
            ("ng", "At least one interval is longer than 100 ms."),
            ("skipped", "Not measured; see below."),
        ],
        skipped: "Skipped when the range holds fewer than two comparable PCRs: no PCR, a single PCR, or PCRs that are all separated by discontinuities.",
        limitations: "No interval is measured across a PCR packet with discontinuity_indicator set, or across an undeclared backward jump of the clock. A 33-bit wraparound is measured as a normal interval. An undeclared forward jump is measured as is, because a file cannot tell it apart from a genuinely late PCR.",
        detail: Some((
            Screen::Timeline,
            "Timeline plots every interval against the limit.",
        )),
    },
    MetricHelp {
        metric: Metric::PcrPtsDtsGap,
        title: "PCR → PTS/DTS gap",
        note: "largest timestamp ahead of the PCR",
        measures: "How far ahead of the program clock the PES timestamps are, which is how long data waits in the decoder's buffer. For each PES packet, its DTS (or PTS when it has no DTS) is compared with the PCR time at the packet's position, interpolated between the PCRs before and after it.",
        scope: "The selected program: every elementary stream of the program against its PCR PID.",
        value: "The largest gap in milliseconds. The note gives the limit and how many timestamps were compared (observations).",
        statuses: &[
            (
                "ok",
                "Every gap is 1000 ms or less (ISO/IEC 13818-1). Exactly 1000 ms is OK.",
            ),
            (
                "ng",
                "At least one timestamp is more than 1000 ms ahead of its PCR.",
            ),
            ("skipped", "Not measured; see below."),
        ],
        skipped: "Skipped when no parsed PTS/DTS has a PCR both before and after it in the range.",
        limitations: "The PCR time at a PES packet is interpolated by byte position between the surrounding PCRs. The last PES packets of the range, after the final PCR, are not compared.",
        detail: Some((
            Screen::Timeline,
            "Timeline plots the gap of every elementary stream against the limit.",
        )),
    },
    MetricHelp {
        metric: Metric::TimestampAnomalies,
        title: "PTS/DTS anomalies",
        note: "occurrences in this program",
        measures: "Whether each elementary stream's decode timestamps (DTS, or PTS when there is no DTS) advance as they should. Detected problems are timestamps going backward, a 33-bit wraparound, a forward jump much larger than the stream's frame interval, and a DTS later than its PTS.",
        scope: "The selected program: each of its elementary-stream PIDs separately.",
        value: "The number of anomalies found. Findings list each kind per PID with its first occurrence.",
        statuses: &[
            ("ok", "No anomaly was found."),
            ("anomalies", "At least one anomaly was found; see Findings."),
        ],
        skipped: "This check is never skipped; a stream without timestamps simply has no anomaly.",
        limitations: "Presentation order legitimately reorders PTS between B-frames, so only the decode timeline is checked. A forward jump is reported only when it is at least five frame intervals and more than 0.5 s longer than one frame interval.",
        detail: Some((
            Screen::Timeline,
            "Timeline marks each anomaly at its position.",
        )),
    },
    MetricHelp {
        metric: Metric::ContinuityCounter,
        title: "Continuity counter",
        note: "errors in the whole multiplex",
        measures: "Whether packets are missing, repeated or out of order. Each PID's 4-bit continuity_counter must increase by one for every packet with payload; a gap usually means lost packets.",
        scope: "The whole multiplex: every PID in the range except null packets, not only the selected program.",
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
        detail: Some((
            Screen::Timeline,
            "Timeline marks continuity errors on the program's PIDs.",
        )),
    },
    MetricHelp {
        metric: Metric::PcrJitter,
        title: "PCR jitter",
        note: "max |estimated jitter|",
        measures: "An estimate of how far each PCR value is from the time its packet would be delivered. A file has no packet-arrival timestamps, so the byte position stands in for time: each PCR is compared with the value interpolated from its two neighbors by byte position. This assumes the stream is delivered at an approximately constant rate.",
        scope: "The selected program: the PCR PID named in its PMT.",
        value: "The worst absolute estimated jitter (the largest deviation either way). It is an estimate from the file, not a measurement of the PCRs as they arrived.",
        statuses: &[
            (
                "ok",
                "Max |estimated jitter| is within ±500 ns (ISO/IEC 13818-1 PCR accuracy).",
            ),
            (
                "warning",
                "Beyond ±500 ns but within ±25 µs (the tolerance DVB expects receivers to handle).",
            ),
            ("ng", "Beyond ±25 µs."),
            ("skipped", "Not estimated; see below."),
        ],
        skipped: "Skipped with fewer than three PCRs, when no segment between discontinuities has three PCRs, or when more than 30% of the PCRs start a new segment.",
        limitations: "This is not a direct hardware or TR 101 290 measurement. When the delivery rate varies between PCRs (variable-bitrate content on the PCR PID, or a stream that was remultiplexed), the byte position is a poor clock and the estimate can be milliseconds even if the PCRs were accurate on the wire. Read a WARNING or NG together with the delivery: it is meaningful for a constant-rate multiplex.",
        detail: Some((
            Screen::Bitrate,
            "Bitrate plots the estimated jitter over time and its histogram.",
        )),
    },
];

/// The window with the explanation of `metric`. Returns the screen to open
/// when its link is followed. `open` is cleared when the window is closed.
pub fn window(ctx: &egui::Context, open: &mut Option<Metric>, just_opened: bool) -> Option<Screen> {
    let metric = (*open)?;
    let help = metric.help();
    let screen = ctx.content_rect();
    let width = (screen.width() - 32.0).clamp(160.0, 460.0);
    let mut keep_open = true;
    let mut navigate = None;
    let response = egui::Window::new(help.title)
        .id(egui::Id::unique("overview_metric_help"))
        .open(&mut keep_open)
        .collapsible(false)
        .resizable(false)
        .default_width(width)
        .max_width(width)
        .default_height((screen.height() - 64.0).clamp(120.0, 620.0))
        .max_height((screen.height() - 64.0).max(120.0))
        .vscroll(true)
        .pivot(egui::Align2::CENTER_CENTER)
        .default_pos(screen.center())
        .show(ctx, |ui| {
            for (heading, text) in [
                ("What it measures", help.measures),
                ("Scope", help.scope),
                ("Value", help.value),
            ] {
                section(ui, heading, text);
            }
            ui.strong("Verdicts");
            for (status, meaning) in help.statuses {
                ui.horizontal_top(|ui| {
                    badge(ui, &status.to_uppercase(), status_verdict(status));
                    ui.add(egui::Label::new(*meaning).wrap());
                });
            }
            ui.add_space(6.0);
            section(ui, "When it is skipped", help.skipped);
            section(ui, "Limitations", help.limitations);
            if let Some((target, text)) = help.detail {
                ui.add(egui::Label::new(text).wrap());
                if ui.button(format!("Open {}", target.title())).clicked() {
                    navigate = Some(target);
                }
            }
        });
    let escape = ctx.input(|i| i.key_pressed(egui::Key::Escape));
    let clicked_outside = !just_opened && response.is_some_and(|r| r.response.clicked_elsewhere());
    if !keep_open || escape || clicked_outside || navigate.is_some() {
        *open = None;
    }
    navigate
}

fn section(ui: &mut egui::Ui, heading: &str, text: &str) {
    ui.strong(heading);
    ui.add(egui::Label::new(text).wrap());
    ui.add_space(6.0);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_card_has_a_complete_explanation() {
        for metric in Metric::ALL {
            let help = metric.help();
            assert_eq!(help.metric, metric);
            for text in [
                help.title,
                help.note,
                help.measures,
                help.scope,
                help.value,
                help.skipped,
                help.limitations,
            ] {
                assert!(!text.trim().is_empty(), "{metric:?}");
            }
            assert!(help.statuses.iter().any(|(s, _)| *s == "ok"), "{metric:?}");
            assert!(help.detail.is_some(), "{metric:?}");
        }
        assert_eq!(HELP.len(), Metric::ALL.len());
    }

    /// The thresholds the CLI uses, and the file-based model of PCR jitter.
    #[test]
    fn explanations_state_the_cli_thresholds_and_limitations() {
        let text = |metric: Metric| {
            let help = metric.help();
            let statuses: Vec<&str> = help.statuses.iter().map(|(_, m)| *m).collect();
            [help.measures, help.value, help.limitations]
                .into_iter()
                .chain(statuses)
                .collect::<Vec<_>>()
                .join(" ")
        };
        assert!(text(Metric::MaxPcrInterval).contains("100 ms"));
        assert!(text(Metric::PcrPtsDtsGap).contains("1000 ms"));
        assert!(
            Metric::ContinuityCounter
                .help()
                .scope
                .contains("whole multiplex")
        );
        let jitter = text(Metric::PcrJitter);
        for phrase in [
            "±500 ns",
            "±25 µs",
            "no packet-arrival timestamps",
            "byte position stands in for time",
            "approximately constant rate",
            "not a direct hardware or TR 101 290 measurement",
        ] {
            assert!(jitter.contains(phrase), "{phrase}");
        }
        assert!(
            Metric::PcrJitter
                .help()
                .statuses
                .iter()
                .any(|(s, _)| *s == "skipped")
        );
    }
}
