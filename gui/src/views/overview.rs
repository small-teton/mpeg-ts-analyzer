use eframe::egui;

use super::metric_help::{self, Metric};
use super::{Screen, badge, program_selector_labels, status_verdict};
use crate::icons;
use crate::psi;
use crate::range_bar;
use crate::report::{AnalysisReport, Check, Program, stream_type_name};

/// What the Overview keeps between frames.
#[derive(Default)]
pub struct OverviewState {
    /// The card whose explanation is open.
    pub help: Option<Metric>,
    /// The explanation opened in this frame, so the click that opened it does
    /// not count as a click outside it.
    help_just_opened: bool,
}

impl OverviewState {
    pub fn open_help(&mut self, metric: Metric) {
        self.help = Some(metric);
        self.help_just_opened = true;
    }

    /// The explanation window, drawn above the screens. Returns the screen
    /// to open when its link is followed.
    pub fn help_window(&mut self, ctx: &egui::Context) -> Option<Screen> {
        let navigate = metric_help::window(ctx, &mut self.help, self.help_just_opened);
        self.help_just_opened = false;
        navigate
    }
}

pub fn show_report(
    ui: &mut egui::Ui,
    report: &AnalysisReport,
    program: &mut usize,
    state: &mut OverviewState,
) -> Option<Screen> {
    let labels: Vec<String> = report.programs.iter().map(Program::label).collect();
    ui.horizontal_wrapped(|ui| {
        program_selector_labels(ui, &labels, program);
    });
    ui.separator();
    let Some(selected) = report.programs.get(*program) else {
        ui.weak("No program was found in this range.");
        return None;
    };
    egui::ScrollArea::vertical().show(ui, |ui| {
        report_stream(ui, report, selected);
        ui.add_space(12.0);

        ui.horizontal_wrapped(|ui| {
            ui.heading("Compliance");
            ui.weak("ISO/IEC 13818-1 timing checks");
        });
        ui.add_space(6.0);
        card_grid(ui, &cards(report, selected), state);
        ui.add_space(10.0);

        ui.strong("Elementary streams");
        // Scrolls on its own in a narrow window instead of widening the page.
        egui::ScrollArea::horizontal()
            .id_salt("overview_report_streams_scroll")
            .show(ui, |ui| {
                egui::Grid::new("overview_report_streams")
                    .striped(true)
                    .num_columns(4)
                    .spacing([24.0, 4.0])
                    .show(ui, |ui| {
                        for header in ["PID", "Type", "Avg bitrate", "Peak bitrate (1 s)"] {
                            ui.strong(header);
                        }
                        ui.end_row();
                        for stream in &selected.elementary_streams {
                            let rate = selected
                                .bitrate
                                .as_ref()
                                .and_then(|b| b.pids.iter().find(|p| p.pid == stream.pid));
                            ui.monospace(&stream.pid);
                            ui.label(stream_type_name(&stream.stream_type));
                            ui.monospace(rate.map_or("—".to_owned(), |r| mbps(r.average_bps)));
                            ui.monospace(
                                rate.and_then(|r| r.peak_1s_bps)
                                    .map_or("—".to_owned(), mbps),
                            );
                            ui.end_row();
                        }
                    });
            });
        ui.add_space(10.0);

        let (findings, unavailable) = findings(report, selected);
        egui::CollapsingHeader::new(format!("Findings ({})", findings.len()))
            .id_salt("overview_findings")
            .default_open(true)
            .show(ui, |ui| {
                if findings.is_empty() {
                    ui.weak("None.");
                }
                for finding in &findings {
                    finding_row(ui, finding, state);
                }
            });
        if !unavailable.is_empty() {
            egui::CollapsingHeader::new(format!("Unavailable analysis ({})", unavailable.len()))
                .id_salt("overview_unavailable")
                .default_open(true)
                .show(ui, |ui| {
                    for (metric, reason) in &unavailable {
                        ui.horizontal_wrapped(|ui| {
                            badge(ui, "SKIPPED", status_verdict("skipped"));
                            help_link(ui, metric.title(), *metric, state);
                            ui.label(reason);
                        });
                    }
                });
        }
    });
    None
}

fn report_stream(ui: &mut egui::Ui, report: &AnalysisReport, program: &Program) {
    ui.heading("Stream");
    ui.add_space(4.0);
    // Side by side when both fit; otherwise the file information goes below,
    // so a narrow window does not widen the page.
    let available = ui.available_width();
    if available >= STREAM_SIDE_BY_SIDE {
        ui.horizontal_top(|ui| {
            program_group(ui, report, program, available - FILE_GROUP_WIDTH);
            file_group(ui, report, program);
        });
    } else {
        program_group(ui, report, program, available);
        file_group(ui, report, program);
    }
}

/// Window width from which the program and the file information sit side by side.
const STREAM_SIDE_BY_SIDE: f32 = 760.0;
/// Room kept for the file information next to the program.
const FILE_GROUP_WIDTH: f32 = 340.0;

/// The program tree, at most `max_width` wide; its text wraps.
fn program_group(ui: &mut egui::Ui, report: &AnalysisReport, program: &Program, max_width: f32) {
    let frame = egui::Frame::group(ui.style());
    let inner = (max_width - frame.total_margin().sum().x).max(0.0);
    frame.show(ui, |ui| {
        ui.style_mut().wrap_mode = Some(egui::TextWrapMode::Wrap);
        ui.set_max_width(inner);
        ui.set_min_width(320.0_f32.min(inner));
        ui.vertical(|ui| {
            egui::CollapsingHeader::new(format!("Program {}", program.label()))
                .id_salt("overview_report_program")
                .default_open(true)
                .show(ui, |ui| {
                    if let Some(service) = &program.service {
                        ui.label(service.summary());
                    }
                    for stream in &program.elementary_streams {
                        let pcr = if program.pcr_pid.as_deref() == Some(stream.pid.as_str()) {
                            " (PCR)"
                        } else {
                            ""
                        };
                        ui.monospace(format!(
                            "{}  {}{pcr}",
                            stream.pid,
                            stream_type_name(&stream.stream_type)
                        ));
                    }
                    if let Some(error) = &program.error {
                        ui.colored_label(ui.visuals().warn_fg_color, error);
                    }
                    let changes: Vec<_> = report
                        .psi_transitions
                        .iter()
                        .filter(|t| psi::concerns(t, program))
                        .collect();
                    if !changes.is_empty() {
                        ui.add_space(4.0);
                        ui.weak("PAT/PMT changes in this range");
                        for change in changes {
                            ui.monospace(format!(
                                "0x{:08X}  {}",
                                change.offset_bytes,
                                psi::describe(change)
                            ));
                        }
                    }
                });
        });
    });
}

fn file_group(ui: &mut egui::Ui, report: &AnalysisReport, program: &Program) {
    ui.group(|ui| {
        egui::Grid::new("overview_report_file")
            .num_columns(2)
            .spacing([16.0, 4.0])
            .show(ui, |ui| {
                let packet_size = report
                    .input
                    .packet_size_bytes
                    .map_or("—".to_owned(), |size| format!("{size} bytes"));
                let end = report.input.offset_bytes
                    + report
                        .input
                        .limit_bytes
                        .unwrap_or(report.input.file_size_bytes);
                let start = program
                    .analysis_start_offset_bytes
                    .map_or("—".to_owned(), |offset| format!("0x{offset:08X}"));
                for (key, value) in [
                    ("Packet", packet_size),
                    ("File size", range_bar::bytes(report.input.file_size_bytes)),
                    (
                        "Scan range",
                        format!(
                            "{} – {}",
                            range_bar::bytes(report.input.offset_bytes),
                            range_bar::bytes(end)
                        ),
                    ),
                    ("Analysis starts", start),
                ] {
                    ui.weak(key);
                    ui.label(value);
                    ui.end_row();
                }
            });
    });
}

fn mbps(bps: f64) -> String {
    format!("{:.3} Mbps", bps / 1e6)
}

/// One compliance card as the CLI reported it.
pub(crate) struct Card {
    pub metric: Metric,
    pub status: String,
    pub value: String,
    pub note: String,
}

/// The compliance cards of a program, in Overview order. A card is left out
/// when the CLI did not report its result.
pub(crate) fn cards(report: &AnalysisReport, program: &Program) -> Vec<Card> {
    let mut cards = Vec::new();
    if let Some(compliance) = &program.compliance {
        cards.push(check_card(
            Metric::MaxPcrInterval,
            &compliance.max_pcr_interval,
        ));
        cards.push(check_card(
            Metric::PcrPtsDtsGap,
            &compliance.max_pcr_pts_dts_gap,
        ));
    }
    if let Some(anomalies) = &program.timestamp_anomalies {
        let count: u64 = anomalies.items.iter().map(|a| a.count).sum();
        cards.push(Card {
            metric: Metric::TimestampAnomalies,
            status: anomalies.status.clone(),
            value: count.to_string(),
            note: Metric::TimestampAnomalies.help().note.to_owned(),
        });
    }
    if let Some(continuity) = &report.continuity_counter {
        cards.push(Card {
            metric: Metric::ContinuityCounter,
            status: continuity.status.clone(),
            value: continuity.total_error_count.to_string(),
            note: Metric::ContinuityCounter.help().note.to_owned(),
        });
    }
    if let Some(jitter) = &program.pcr_jitter {
        cards.push(Card {
            metric: Metric::PcrJitter,
            status: jitter.status.clone(),
            value: max_abs_jitter_ns(jitter).map_or("—".to_owned(), duration_ns),
            note: jitter
                .reason
                .clone()
                .unwrap_or_else(|| Metric::PcrJitter.help().note.to_owned()),
        });
    }
    cards
}

fn check_card(metric: Metric, check: &Check) -> Card {
    let value = check
        .value_ms
        .map_or("—".to_owned(), |ms| format!("{ms:.1} ms"));
    let note = match &check.reason {
        Some(reason) => reason.clone(),
        None => format!(
            "{} · limit {:.0} ms · {} observations",
            metric.help().note,
            check.limit_ms,
            check.observations
        ),
    };
    Card {
        metric,
        status: check.status.clone(),
        value,
        note,
    }
}

fn max_abs_jitter_ns(jitter: &crate::report::PcrJitter) -> Option<f64> {
    jitter
        .max_jitter_ns
        .zip(jitter.min_jitter_ns)
        .map(|(max, min)| max.abs().max(min.abs()))
}

/// A duration in nanoseconds, in the unit that keeps it short.
pub(crate) fn duration_ns(ns: f64) -> String {
    if ns < 1_000.0 {
        format!("{ns:.0} ns")
    } else if ns < 1_000_000.0 {
        format!("{:.1} µs", ns / 1e3)
    } else {
        format!("{:.2} ms", ns / 1e6)
    }
}

/// Preferred width of a card and the space between cards.
const CARD_WIDTH: f32 = 220.0;
const CARD_SPACING: f32 = 8.0;

/// How many cards fit side by side in `available` points, at least one.
pub(crate) fn card_columns(available: f32, cards: usize) -> usize {
    let fit = ((available + CARD_SPACING) / (CARD_WIDTH + CARD_SPACING)).floor();
    (fit.max(1.0) as usize).min(cards.max(1))
}

/// The cards in rows of equal-width cards, in order. Narrower than one
/// preferred card width, each card takes the available width and its text
/// wraps.
fn card_grid(ui: &mut egui::Ui, cards: &[Card], state: &mut OverviewState) {
    // The visible width: content wider than the window must not widen the cards.
    let available = ui
        .available_width()
        .min(ui.clip_rect().right() - ui.cursor().left());
    let columns = card_columns(available, cards.len());
    let width = ((available - CARD_SPACING * (columns - 1) as f32) / columns as f32).floor();
    for row in cards.chunks(columns) {
        ui.horizontal_top(|ui| {
            ui.spacing_mut().item_spacing.x = CARD_SPACING;
            for card in row {
                card_ui(ui, card, width, state);
            }
        });
    }
}

fn card_ui(ui: &mut egui::Ui, card: &Card, width: f32, state: &mut OverviewState) {
    let frame = egui::Frame::group(ui.style());
    let inner = (width - frame.total_margin().sum().x).max(0.0);
    frame.show(ui, |ui| {
        ui.set_width(inner);
        ui.vertical(|ui| {
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Min), |ui| {
                badge(
                    ui,
                    &card.status.to_uppercase(),
                    status_verdict(&card.status),
                );
                if info_button(ui, card.metric).clicked() {
                    state.open_help(card.metric);
                }
                ui.with_layout(egui::Layout::left_to_right(egui::Align::Min), |ui| {
                    ui.add(egui::Label::new(card.metric.title()).wrap());
                });
            });
            ui.add(egui::Label::new(egui::RichText::new(&card.value).size(24.0).strong()).wrap());
            ui.add(egui::Label::new(egui::RichText::new(&card.note).small().weak()).wrap());
        });
    });
}

/// The focusable button that opens the explanation of a card.
fn info_button(ui: &mut egui::Ui, metric: Metric) -> egui::Response {
    let name = metric.about();
    let button = egui::Button::new(icons::text(icons::INFO).size(16.0)).frame(false);
    let response = ui.add(button).on_hover_text(&name);
    response.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Button, true, &name));
    response
}

/// A link that opens the explanation of `metric`.
fn help_link(ui: &mut egui::Ui, text: &str, metric: Metric, state: &mut OverviewState) {
    if ui.link(text).on_hover_text(metric.about()).clicked() {
        state.open_help(metric);
    }
}

/// Where a finding applies.
#[derive(Clone, Copy, Debug, PartialEq)]
pub(crate) enum Scope {
    /// The selected program, by program_number.
    Program(u16),
    /// Every PID of the range.
    Multiplex,
}

/// One user-visible problem of the analysis.
#[derive(Debug, PartialEq)]
pub(crate) struct Finding {
    /// The CLI's status, shown as it is; `warning` for an analyzer warning.
    pub status: String,
    /// The card it belongs to; analyzer warnings belong to none.
    pub metric: Option<Metric>,
    pub label: String,
    pub reason: String,
    pub scope: Scope,
    pub offset: Option<u64>,
}

/// Whether a card status is a finding. `skipped` is unavailable analysis,
/// not a finding; any other status than `ok`, including one this GUI does not
/// know, is shown as the CLI reported it.
fn is_finding(status: &str) -> bool {
    !matches!(status, "ok" | "skipped")
}

/// Every finding of the selected program and the multiplex, and the cards
/// whose analysis was skipped with the CLI's reason. Each underlying event is
/// one finding: a card with events (anomaly groups, continuity errors) is
/// represented by those events, not counted again.
pub(crate) fn findings(
    report: &AnalysisReport,
    program: &Program,
) -> (Vec<Finding>, Vec<(Metric, String)>) {
    let mut findings = Vec::new();
    let mut unavailable = Vec::new();
    let in_program = Scope::Program(program.program_number);
    for card in cards(report, program) {
        if card.status == "skipped" {
            let reason = match card.metric {
                Metric::MaxPcrInterval | Metric::PcrPtsDtsGap | Metric::PcrJitter => card.note,
                _ => String::new(),
            };
            unavailable.push((card.metric, reason));
            continue;
        }
        if !is_finding(&card.status) {
            continue;
        }
        let metric = card.metric;
        let finding = |label: &str, reason: String, scope: Scope, offset: Option<u64>| Finding {
            status: card.status.clone(),
            metric: Some(metric),
            label: label.to_owned(),
            reason,
            scope,
            offset,
        };
        match metric {
            Metric::MaxPcrInterval | Metric::PcrPtsDtsGap => {
                let check = program.compliance.as_ref().map(|c| match metric {
                    Metric::MaxPcrInterval => &c.max_pcr_interval,
                    _ => &c.max_pcr_pts_dts_gap,
                });
                let reason = check.map_or(String::new(), |check| {
                    check.value_ms.map_or_else(
                        || check.reason.clone().unwrap_or_default(),
                        |ms| format!("{ms:.1} ms, limit {:.0} ms", check.limit_ms),
                    )
                });
                findings.push(finding(metric.title(), reason, in_program, None));
            }
            Metric::TimestampAnomalies => {
                let items = program
                    .timestamp_anomalies
                    .as_ref()
                    .map_or(&[][..], |a| &a.items[..]);
                if items.is_empty() {
                    findings.push(finding(metric.title(), String::new(), in_program, None));
                }
                for item in items {
                    findings.push(finding(
                        metric.title(),
                        format!(
                            "PID {} · {} · {}× · {}",
                            item.pid,
                            item.kind.replace('_', " "),
                            item.count,
                            item.message
                        ),
                        in_program,
                        None,
                    ));
                }
            }
            Metric::ContinuityCounter => {
                if report.continuity_events.is_empty() {
                    findings.push(finding(
                        metric.title(),
                        match card.value.as_str() {
                            "1" => "1 error".to_owned(),
                            count => format!("{count} errors"),
                        },
                        Scope::Multiplex,
                        None,
                    ));
                }
                for event in &report.continuity_events {
                    findings.push(finding(
                        metric.title(),
                        format!(
                            "PID {}: continuity_counter {} (expected {})",
                            event.pid, event.actual_cc, event.expected_cc
                        ),
                        Scope::Multiplex,
                        Some(event.offset_bytes),
                    ));
                }
            }
            Metric::PcrJitter => {
                let jitter = program.pcr_jitter.as_ref();
                let limit = match card.status.as_str() {
                    "ng" => " beyond ±25 µs",
                    "warning" => " beyond ±500 ns",
                    _ => "",
                };
                let reason = jitter.and_then(max_abs_jitter_ns).map_or(String::new(), |ns| {
                    format!(
                        "max |estimated jitter| {}{limit}; estimated from byte position, assuming a constant delivery rate",
                        duration_ns(ns)
                    )
                });
                findings.push(finding(
                    "PCR jitter (estimate)",
                    reason,
                    in_program,
                    jitter.and_then(|j| {
                        if j.max_jitter_ns.unwrap_or(0.0).abs()
                            >= j.min_jitter_ns.unwrap_or(0.0).abs()
                        {
                            j.max_jitter_offset_bytes
                        } else {
                            j.min_jitter_offset_bytes
                        }
                    }),
                ));
            }
        }
    }
    for warning in &report.warnings {
        findings.push(Finding {
            status: "warning".to_owned(),
            metric: None,
            label: "Analyzer warning".to_owned(),
            reason: warning.message.clone(),
            scope: Scope::Multiplex,
            offset: Some(warning.offset_bytes),
        });
    }
    (findings, unavailable)
}

fn finding_row(ui: &mut egui::Ui, finding: &Finding, state: &mut OverviewState) {
    ui.horizontal_wrapped(|ui| {
        badge(
            ui,
            &finding.status.to_uppercase(),
            status_verdict(&finding.status),
        );
        match finding.metric {
            Some(metric) => help_link(ui, &finding.label, metric, state),
            None => {
                ui.strong(&finding.label);
            }
        }
        ui.weak(match finding.scope {
            Scope::Program(number) => format!("program {number}"),
            Scope::Multiplex => "whole multiplex".to_owned(),
        });
        if let Some(offset) = finding.offset {
            ui.monospace(format!("0x{offset:08X}"));
        }
        ui.add(egui::Label::new(&finding.reason).wrap());
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    fn report(json: &str) -> AnalysisReport {
        serde_json::from_str(json).expect("fixture parses")
    }

    #[test]
    fn findings_of_the_all_findings_sample() {
        let report = report(include_str!(
            "../../tests/fixtures/analysis_all_findings_188.json"
        ));
        let (findings, unavailable) = findings(&report, &report.programs[0]);
        assert!(unavailable.is_empty());
        let summary: Vec<_> = findings
            .iter()
            .map(|f| (f.status.as_str(), f.label.as_str(), f.scope, f.offset))
            .collect();
        assert_eq!(
            summary,
            [
                ("ng", "Max PCR interval", Scope::Program(1), None),
                ("ng", "PCR → PTS/DTS gap", Scope::Program(1), None),
                ("anomalies", "PTS/DTS anomalies", Scope::Program(1), None),
                (
                    "errors",
                    "Continuity counter",
                    Scope::Multiplex,
                    Some(44 * 188)
                ),
                (
                    "ng",
                    "PCR jitter (estimate)",
                    Scope::Program(1),
                    Some(58 * 188)
                ),
            ]
        );
        assert_eq!(findings[0].reason, "160.0 ms, limit 100 ms");
        assert_eq!(findings[1].reason, "1200.0 ms, limit 1000 ms");
        assert!(
            findings[2]
                .reason
                .starts_with("PID 0x0100 · backward · 1× · PTS went backward")
        );
        assert_eq!(
            findings[3].reason,
            "PID 0x0101: continuity_counter 6 (expected 5)"
        );
        assert!(
            findings[4]
                .reason
                .starts_with("max |estimated jitter| 100.0 µs beyond ±25 µs")
        );
        // Each card is non-OK and has its finding.
        for card in cards(&report, &report.programs[0]) {
            assert!(is_finding(&card.status), "{:?}", card.metric);
            assert!(findings.iter().any(|f| f.metric == Some(card.metric)));
        }
    }

    /// Analyzer warnings, unknown statuses and skipped cards; a card without
    /// its events still counts once.
    #[test]
    fn findings_follow_the_cli_statuses() {
        let mut document: serde_json::Value = serde_json::from_str(include_str!(
            "../../tests/fixtures/analysis_all_findings_188.json"
        ))
        .unwrap();
        document["warnings"] =
            serde_json::json!([{"offset_bytes": 376, "message": "PMT section CRC mismatch"}]);
        document["continuity_events"] = serde_json::json!([]);
        let program = &mut document["programs"][0];
        program["compliance"]["max_pcr_interval"]["status"] = "ok".into();
        program["compliance"]["max_pcr_pts_dts_gap"] = serde_json::json!({
            "status": "skipped", "limit_ms": 1000.0, "observations": 0,
            "reason": "need a parsed PTS/DTS bracketed by PCR observations"
        });
        program["pcr_jitter"]["status"] = "indeterminate".into();
        let report = report(&document.to_string());
        let (findings, unavailable) = findings(&report, &report.programs[0]);
        let summary: Vec<_> = findings
            .iter()
            .map(|f| (f.status.as_str(), f.label.as_str(), f.scope))
            .collect();
        assert_eq!(
            summary,
            [
                ("anomalies", "PTS/DTS anomalies", Scope::Program(1)),
                ("errors", "Continuity counter", Scope::Multiplex),
                ("indeterminate", "PCR jitter (estimate)", Scope::Program(1)),
                ("warning", "Analyzer warning", Scope::Multiplex),
            ]
        );
        assert_eq!(findings[1].reason, "1 error");
        assert!(
            findings[2]
                .reason
                .starts_with("max |estimated jitter| 100.0 µs;")
        );
        assert_eq!(
            unavailable,
            [(
                Metric::PcrPtsDtsGap,
                "need a parsed PTS/DTS bracketed by PCR observations".to_owned()
            )]
        );
    }

    #[test]
    fn card_columns_follow_the_width() {
        assert_eq!(card_columns(1200.0, 5), 5);
        assert_eq!(card_columns(5.0 * CARD_WIDTH + 4.0 * CARD_SPACING, 5), 5);
        assert_eq!(
            card_columns(5.0 * CARD_WIDTH + 4.0 * CARD_SPACING - 1.0, 5),
            4
        );
        assert_eq!(card_columns(700.0, 5), 3);
        assert_eq!(card_columns(300.0, 5), 1);
        assert_eq!(card_columns(100.0, 5), 1);
        assert_eq!(card_columns(1200.0, 2), 2);
        assert_eq!(card_columns(1200.0, 0), 1);
    }

    #[test]
    fn formats_jitter_durations() {
        assert_eq!(duration_ns(420.0), "420 ns");
        assert_eq!(duration_ns(100_000.0), "100.0 µs");
        assert_eq!(duration_ns(28_679_245.0), "28.68 ms");
    }
}
