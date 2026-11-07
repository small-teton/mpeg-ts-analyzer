use eframe::egui;

use super::check_detail;
use super::check_help::Check;
use super::{Screen, badge, program_selector_labels, status_verdict};
use crate::icons;
use crate::plot_data::Timeline;
use crate::psi;
use crate::range_bar;
use crate::report::{
    AffectedPackets, AnalysisReport, AvLeadProgram, MuxLeadPid, Program, PsiHealth, PsiTableHealth,
    ReferencedPid, UnreferencedPid, stream_type_name,
};

/// What Overview shows: the summary, or the full-page explanation of one
/// check.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum OverviewPage {
    #[default]
    Summary,
    CheckDetail(Check),
}

/// What Overview asks the app to do.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum OverviewAction {
    /// Switch to another screen.
    Open(Screen),
    /// Show the packet at this offset in Packets.
    OpenPacket(u64),
    /// Select the Timeline event at this offset.
    ShowOnTimeline(u64),
}

/// Where keyboard focus goes on the next frame.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum Focus {
    /// The back control of a detail page.
    Back,
    /// The information button of a check in the table.
    Info(Check),
}

/// What the Overview keeps between frames.
#[derive(Default)]
pub struct OverviewState {
    page: OverviewPage,
    /// Vertical scroll offset of the summary, kept while a detail page is shown.
    summary_offset: f32,
    /// The next frame of the page sets its scroll offset: the summary's saved
    /// offset, or the top of a detail page.
    scroll_pending: bool,
    /// Focus to move on the next frame.
    pub(crate) focus: Option<Focus>,
    /// Focus to restore when the detail page is left.
    return_focus: Option<Focus>,
    /// The analysis the page belongs to; a new analysis shows the summary.
    generation: u64,
}

impl OverviewState {
    #[cfg(test)]
    pub fn page(&self) -> OverviewPage {
        self.page
    }

    /// The vertical scroll offset of the summary.
    #[cfg(test)]
    pub fn summary_offset(&self) -> f32 {
        self.summary_offset
    }

    /// Opens the detail page of `check`. `from` is the control that opened
    /// it, which gets the focus back when the page is left.
    pub(crate) fn open(&mut self, check: Check, from: Option<Focus>) {
        if self.page == OverviewPage::Summary {
            self.return_focus = from;
        }
        self.page = OverviewPage::CheckDetail(check);
        self.scroll_pending = true;
        self.focus = Some(Focus::Back);
    }

    /// Back to the summary, at the scroll position it had.
    pub fn show_summary(&mut self) {
        if self.page != OverviewPage::Summary {
            self.page = OverviewPage::Summary;
            self.scroll_pending = true;
            self.focus = self.return_focus.take();
        }
    }

    /// Takes the focus request for `target`, if there is one.
    pub(crate) fn take_focus(&mut self, target: Focus) -> bool {
        let take = self.focus == Some(target);
        if take {
            self.focus = None;
        }
        take
    }
}

pub fn show_report(
    ui: &mut egui::Ui,
    report: &AnalysisReport,
    program: &mut usize,
    state: &mut OverviewState,
    timelines: &[Timeline],
    generation: u64,
) -> Option<OverviewAction> {
    if state.generation != generation {
        // A new analysis: its checks and findings may differ.
        *state = OverviewState {
            generation,
            ..OverviewState::default()
        };
    }
    let labels: Vec<String> = report.programs.iter().map(Program::label).collect();
    ui.horizontal_wrapped(|ui| {
        program_selector_labels(ui, &labels, program);
    });
    ui.separator();
    let Some(selected) = report.programs.get(*program) else {
        ui.weak("No program was found in this range.");
        return None;
    };
    let scroll_pending = std::mem::take(&mut state.scroll_pending);
    match state.page {
        OverviewPage::Summary => {
            let mut area = egui::ScrollArea::vertical().id_salt("overview_summary");
            if scroll_pending {
                area = area.vertical_scroll_offset(state.summary_offset);
            }
            let output = area.show(ui, |ui| summary(ui, report, selected, state));
            state.summary_offset = output.state.clamped_offset().y;
            output.inner
        }
        OverviewPage::CheckDetail(check) => {
            let context = check_detail::Context {
                report,
                program: selected,
                timeline: timelines.get(*program),
            };
            check_detail::show(ui, &context, check, state, scroll_pending)
        }
    }
}

fn summary(
    ui: &mut egui::Ui,
    report: &AnalysisReport,
    selected: &Program,
    state: &mut OverviewState,
) -> Option<OverviewAction> {
    report_stream(ui, report, selected);
    ui.add_space(12.0);

    ui.horizontal_wrapped(|ui| {
        heading(ui, "Transport health");
        ui.weak("Lightweight TS / PSI / PES checks");
    });
    ui.add_space(6.0);
    health_table(ui, &rows(report, selected), state);
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
                for item in &unavailable {
                    ui.horizontal_wrapped(|ui| {
                        status_badge(ui, &item.status, Some(item.check));
                        help_link(ui, item.check.title(), item.check, state);
                        ui.add(egui::Label::new(&item.reason).wrap());
                    });
                }
            });
    }
    None
}

/// A section heading, announced as a heading.
pub(crate) fn heading(ui: &mut egui::Ui, text: &str) -> egui::Response {
    let response = ui.heading(text);
    response.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Heading, true, text));
    response
}

fn report_stream(ui: &mut egui::Ui, report: &AnalysisReport, program: &Program) {
    heading(ui, "Stream");
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

/// One row of the Transport health table: a check as the CLI reported it.
pub(crate) struct Row {
    pub check: Check,
    /// The CLI's status, verbatim.
    pub status: String,
    /// The value column; `—` when the CLI gave no value.
    pub value: String,
    /// The current result: only the fields the CLI provided, as label and value.
    pub facts: Vec<(&'static str, String)>,
    /// Why the CLI skipped or did not grade the check, if it said.
    pub reason: Option<String>,
}

/// The rows of a program in table order. A check is left out when the CLI
/// did not report it.
pub(crate) fn rows(report: &AnalysisReport, program: &Program) -> Vec<Row> {
    Check::ALL
        .into_iter()
        .filter_map(|check| row(report, program, check))
        .collect()
}

/// The row of one check, if the CLI reported it.
pub(crate) fn row(report: &AnalysisReport, program: &Program, check: Check) -> Option<Row> {
    let number = ("Program", program.program_number.to_string());
    match check {
        Check::MaxPcrInterval | Check::PcrPtsDtsGap => {
            let compliance = program.compliance.as_ref()?;
            let result = if check == Check::MaxPcrInterval {
                &compliance.max_pcr_interval
            } else {
                &compliance.max_pcr_pts_dts_gap
            };
            let value = result.value_ms.map(|ms| format!("{ms:.1} ms"));
            let mut facts = Vec::new();
            facts.extend(value.clone().map(|v| ("Value", v)));
            facts.push(("Limit", format!("{:.0} ms", result.limit_ms)));
            facts.push(number);
            if check == Check::MaxPcrInterval
                && let Some(pid) = &program.pcr_pid
            {
                facts.push(("PCR PID", pid.clone()));
            }
            facts.push(("Observations", result.observations.to_string()));
            facts.extend(result.reason.clone().map(|r| ("Reason", r)));
            Some(Row {
                check,
                status: result.status.clone(),
                value: value.unwrap_or_else(|| "—".to_owned()),
                facts,
                reason: result.reason.clone(),
            })
        }
        Check::PcrDiscontinuities => {
            let result = program.compliance.as_ref()?.pcr_discontinuities.as_ref()?;
            let mut facts = vec![
                ("Undeclared", result.undeclared_count.to_string()),
                ("PCRs", result.pcr_count.to_string()),
                number,
            ];
            if let Some(pid) = &program.pcr_pid {
                facts.push(("PCR PID", pid.clone()));
            }
            facts.push(("Jump limit", format!("{:.0} ms", result.jump_limit_ms)));
            for item in &result.items {
                facts.push((
                    pcr_step_label(&item.kind),
                    format!(
                        "{}×, first at 0x{:08X} ({:+.3} ms)",
                        item.count, item.first_offset_bytes, item.first_jump_ms
                    ),
                ));
            }
            facts.extend(result.reason.clone().map(|r| ("Reason", r)));
            let value = if result.status == "skipped" {
                "—".to_owned()
            } else {
                result.undeclared_count.to_string()
            };
            Some(Row {
                check,
                status: result.status.clone(),
                value,
                facts,
                reason: result.reason.clone(),
            })
        }
        Check::TimestampAnomalies => {
            let anomalies = program.timestamp_anomalies.as_ref()?;
            let count: u64 = anomalies.items.iter().map(|a| a.count).sum();
            let mut facts = vec![("Anomalies", count.to_string()), number];
            let mut pids: Vec<&str> = anomalies.items.iter().map(|a| a.pid.as_str()).collect();
            pids.dedup();
            if !pids.is_empty() {
                facts.push(("Affected PIDs", pids.join(", ")));
            }
            Some(Row {
                check,
                status: anomalies.status.clone(),
                value: count.to_string(),
                facts,
                reason: None,
            })
        }
        Check::ContinuityCounter => {
            let continuity = report.continuity_counter.as_ref()?;
            let mut facts = vec![
                ("Errors", continuity.total_error_count.to_string()),
                ("Applies to", "the whole multiplex".to_owned()),
            ];
            let pids: Vec<String> = continuity
                .pids
                .iter()
                .filter(|p| p.error_count > 0)
                .map(|p| format!("{} ({}): {}", p.pid, p.label, p.error_count))
                .collect();
            if !pids.is_empty() {
                facts.push(("Affected PIDs", pids.join(", ")));
            }
            if let Some(first) = report
                .continuity_events
                .iter()
                .map(|e| e.offset_bytes)
                .min()
            {
                facts.push(("First offset", format!("0x{first:08X}")));
            }
            Some(Row {
                check,
                status: continuity.status.clone(),
                value: continuity.total_error_count.to_string(),
                facts,
                reason: None,
            })
        }
        Check::TransportErrors | Check::ScrambledPackets => {
            let result = affected_packets(report, check)?;
            let mut facts = vec![
                ("Packets", result.packet_count.to_string()),
                ("Excluded", result.excluded_packet_count.to_string()),
                ("Applies to", "the whole multiplex".to_owned()),
            ];
            let pids: Vec<String> = result
                .pids
                .iter()
                .map(|p| {
                    format!(
                        "{} ({}): {}, {} excluded",
                        p.pid, p.label, p.packet_count, p.excluded_packet_count
                    )
                })
                .collect();
            if !pids.is_empty() {
                facts.push(("Affected PIDs", pids.join(", ")));
            }
            if let Some(first) = result.first_offset_bytes {
                facts.push(("First offset", format!("0x{first:08X}")));
            }
            Some(Row {
                check,
                status: result.status.clone(),
                value: result.packet_count.to_string(),
                facts,
                reason: None,
            })
        }
        Check::PesHeaders => {
            let headers = report.pes_headers.as_ref()?;
            let counts = &headers.counts;
            let mut facts = vec![
                ("Errors", counts.error_count.to_string()),
                ("Checked", counts.checked_count.to_string()),
            ];
            if counts.unchecked_count > 0 {
                facts.push(("Unchecked", counts.unchecked_count.to_string()));
            }
            facts.push(("Applies to", ALL_STREAMS.to_owned()));
            if !counts.errors.is_empty() {
                facts.push(("Problems", header_problems(&counts.errors)));
            }
            let pids: Vec<String> = headers
                .pids
                .iter()
                .filter(|p| p.counts.error_count > 0)
                .map(|p| format!("{}: {}", p.stream.describe(), p.counts.error_count))
                .collect();
            if !pids.is_empty() {
                facts.push(("Affected PIDs", pids.join(", ")));
            }
            if let Some(first) = counts.first_error_offset_bytes {
                facts.push(("First offset", format!("0x{first:08X}")));
            }
            facts.extend(counts.reason.clone().map(|r| ("Reason", r)));
            Some(Row {
                check,
                status: counts.status.clone(),
                value: counts.error_count.to_string(),
                facts,
                reason: counts.reason.clone(),
            })
        }
        Check::PtsRepetition => {
            let repetition = report.pts_repetition.as_ref()?;
            let counts = &repetition.counts;
            let value = counts.max_interval_ms.map(|ms| format!("{ms:.1} ms"));
            let mut facts = Vec::new();
            facts.extend(value.clone().map(|v| ("Value", v)));
            facts.push(("Limit", format!("{:.0} ms", repetition.limit_ms)));
            facts.push((
                "Applies to",
                "every video and audio stream of the analyzed programs".to_owned(),
            ));
            facts.push(("Intervals", counts.interval_count.to_string()));
            facts.push(("PTS", counts.pts_count.to_string()));
            facts.push(("Exceeded", counts.exceeded_count.to_string()));
            if let (Some(start), Some(end)) = (
                counts.max_interval_start_offset_bytes,
                counts.max_interval_end_offset_bytes,
            ) {
                facts.push(("Worst interval", format!("0x{start:08X} → 0x{end:08X}")));
            }
            let pids: Vec<String> = repetition
                .pids
                .iter()
                .filter(|p| p.counts.status == "ng")
                .map(|p| p.stream.describe())
                .collect();
            if !pids.is_empty() {
                facts.push(("Affected PIDs", pids.join(", ")));
            }
            facts.extend(counts.reason.clone().map(|r| ("Reason", r)));
            Some(Row {
                check,
                status: counts.status.clone(),
                value: value.unwrap_or_else(|| "—".to_owned()),
                facts,
                reason: counts.reason.clone(),
            })
        }
        Check::MuxLead => {
            let number = program.program_number;
            let pids = program_mux_leads(report, program)?;
            let av = program_av_lead(report, program);
            let status = mux_lead_status(&pids);
            let mut facts = vec![(
                "A/V lead difference",
                match av {
                    Some(av) => av_difference_text(av),
                    None => "not measured: the program has no video and audio pair".to_owned(),
                },
            )];
            facts.extend(lead_range(&pids).map(|range| ("Lead range", range)));
            let negative: u64 = pids.iter().map(|p| p.negative_count).sum();
            if negative > 0 {
                facts.push((
                    "Negative leads",
                    format!("{negative} PES (multiplexed after their DTS/PTS)"),
                ));
            }
            let unbracketed: u64 = pids.iter().map(|p| p.unbracketed_count).sum();
            if unbracketed > 0 {
                facts.push(("Unbracketed", format!("{unbracketed} PES")));
            }
            facts.push(("Time basis", "PCR-interpolated file position".to_owned()));
            facts.push(("Verdict", "none: report only".to_owned()));
            facts.push((
                "Applies to",
                format!("the elementary streams of program {number}"),
            ));
            let reason = (status == "insufficient_data").then(|| {
                pids.iter()
                    .find_map(|p| p.reason.clone())
                    .unwrap_or_else(|| format!("program {number} has no elementary stream"))
            });
            Some(Row {
                check,
                status: status.to_owned(),
                value: av
                    .and_then(|av| av.difference.max_av_lead_difference_ms)
                    .map_or_else(|| "—".to_owned(), |ms| format!("{ms:.1} ms")),
                facts,
                reason,
            })
        }
        Check::PatHealth | Check::PmtHealth => {
            let health = report.psi_health.as_ref()?;
            let table = psi_table(health, program, check)?;
            let repetition = &table.repetition;
            let value = match (repetition.max_interval_ms, table.instance_count) {
                (Some(ms), _) => format!("{ms:.1} ms"),
                (None, 0) => "not found".to_owned(),
                (None, _) => "—".to_owned(),
            };
            let mut facts = vec![("Tables", table.instance_count.to_string())];
            if let Some(ms) = repetition.max_interval_ms {
                facts.push(("Longest interval", format!("{ms:.1} ms")));
            }
            if let Some(end) = repetition.max_interval_end_offset_bytes {
                facts.push(("Longest interval ends", format!("0x{end:08X}")));
            }
            facts.push(("Limit", format!("{:.0} ms", health.repetition_limit_ms)));
            facts.push(("Complete intervals", repetition.interval_count.to_string()));
            facts.push(("Late intervals", repetition.late_interval_count.to_string()));
            facts.push(("Version changes", table.version_change_count.to_string()));
            let problems = psi_problems(table);
            if !problems.is_empty() {
                facts.push(("Problems", problems.join(", ")));
            }
            if let Some(first) = table.first_problem_offset_bytes {
                facts.push(("First problem", format!("0x{first:08X}")));
            }
            if !table.reasons.is_empty() {
                facts.push(("Reasons", table.reasons.join(", ")));
            }
            facts.push(("PID", table.pid.clone()));
            if check == Check::PmtHealth {
                facts.push(number);
            } else {
                facts.push(("Applies to", "the whole multiplex".to_owned()));
            }
            let base = &health.time_base;
            facts.push((
                "Time base",
                match (&base.pcr_pid, base.status.as_str()) {
                    (Some(pid), "pcr") => format!(
                        "PCR PID {pid} of program {}, {} segment(s)",
                        base.program_number, base.segment_count
                    ),
                    (None, "pcr") => format!(
                        "PCR of program {}, {} segment(s)",
                        base.program_number, base.segment_count
                    ),
                    _ => format!("none ({})", base.status),
                },
            ));
            let reason = repetition.reason.as_deref().map(repetition_reason);
            facts.extend(reason.clone().map(|r| ("Reason", r)));
            Some(Row {
                check,
                status: table.status.clone(),
                value,
                facts,
                reason,
            })
        }
        Check::Synchronization => {
            let sync = report.sync.as_ref()?;
            let at = |offset: u64| format!("0x{offset:08X}");
            let mut facts = vec![
                ("Losses", sync.loss_count.to_string()),
                ("Resyncs", sync.resync_count.to_string()),
                ("Skipped bytes", sync.skipped_bytes.to_string()),
            ];
            facts.extend(sync.first_loss_offset_bytes.map(|o| ("First loss", at(o))));
            facts.extend(
                sync.first_resync_offset_bytes
                    .map(|o| ("First resync", at(o))),
            );
            if let (Some(bytes), Some(offset)) =
                (sync.max_skipped_bytes, sync.max_skipped_offset_bytes)
            {
                facts.push(("Largest skip", format!("{bytes} bytes at {}", at(offset))));
            }
            facts.extend(
                sync.unrecovered_offset_bytes
                    .map(|o| ("Analysis stopped at", at(o))),
            );
            if let Some(offset) = sync.trailing_partial_packet_offset_bytes {
                facts.push((
                    "Partial packet at end",
                    format!(
                        "{} bytes at {}",
                        sync.trailing_partial_packet_bytes,
                        at(offset)
                    ),
                ));
            }
            facts.push((
                "Realignment",
                format!(
                    "{} sync bytes one packet apart, within {} bytes",
                    sync.required_sync_bytes, sync.max_resync_scan_bytes
                ),
            ));
            facts.push(("Applies to", "the whole multiplex".to_owned()));
            facts.extend(sync.reason.clone().map(|r| ("Reason", r)));
            Some(Row {
                check,
                status: sync.status.clone(),
                value: sync.loss_count.to_string(),
                facts,
                reason: sync.reason.clone(),
            })
        }
        Check::ReferencedPids => {
            let references = report.pid_references.as_ref()?;
            let referenced = &references.referenced;
            let not_ok = referenced.pids.iter().filter(|p| p.status != "ok").count();
            let mut facts = vec![("References", referenced.pids.len().to_string())];
            for (status, label) in [
                ("absent", "Absent"),
                ("silent", "Silent"),
                ("insufficient_data", "Insufficient data"),
            ] {
                let pids: Vec<String> = referenced
                    .pids
                    .iter()
                    .filter(|p| p.status == status)
                    .map(reference_text)
                    .collect();
                if !pids.is_empty() {
                    facts.push((label, pids.join("; ")));
                }
            }
            facts.push((
                "Silence limit",
                format!("{:.0} ms", references.silence_limit_ms),
            ));
            facts.push(("Time basis", "PCR-interpolated file position".to_owned()));
            facts.push(("Applies to", "the whole multiplex".to_owned()));
            Some(Row {
                check,
                status: referenced.status.clone(),
                value: format!("{not_ok} of {}", referenced.pids.len()),
                facts,
                reason: undetermined_references(report),
            })
        }
        Check::UnreferencedPids => {
            let references = report.pid_references.as_ref()?;
            let unreferenced = &references.unreferenced;
            let mut facts = Vec::new();
            for (status, label) in [
                ("unreferenced", "Unreferenced PIDs"),
                ("insufficient_data", "Not judged"),
            ] {
                let pids: Vec<String> = unreferenced
                    .pids
                    .iter()
                    .filter(|p| p.status == status)
                    .map(|p| {
                        format!(
                            "{}: {}, {:.1} ms",
                            p.pid,
                            packets(p.packet_count),
                            p.longest_run_ms
                        )
                    })
                    .collect();
                if !pids.is_empty() {
                    facts.push((label, pids.join("; ")));
                }
            }
            facts.push((
                "Grace period",
                format!("{:.0} ms", references.unreferenced_grace_ms),
            ));
            facts.push(("Applies to", "the whole multiplex".to_owned()));
            facts.extend(unreferenced.reason.clone().map(|r| ("Reason", r)));
            Some(Row {
                check,
                status: unreferenced.status.clone(),
                value: unreferenced.pids.len().to_string(),
                facts,
                reason: undetermined_unreferenced(report),
            })
        }
        Check::PcrJitter => {
            let jitter = program.pcr_jitter.as_ref()?;
            let estimate = &jitter.estimate;
            let value = estimate.max_abs_jitter_ns.map(duration_ns);
            let mut facts = Vec::new();
            facts.extend(value.clone().map(|v| ("Value", v)));
            facts.push(number);
            if !estimate.pcr_pids.is_empty() {
                facts.push(("PCR PID", estimate.pcr_pids.join(", ")));
            }
            facts.push(("Observations", estimate.observations.to_string()));
            if let Some(offset) = estimate.max_abs_jitter_offset_bytes {
                facts.push(("Worst offset", format!("0x{offset:08X}")));
            }
            facts.push(("Model", estimate.model.clone()));
            facts.push(("Delivery", jitter.delivery.assessment.clone()));
            facts.push(("Delivery evidence", jitter.delivery.evidence()));
            facts.push(("Delivery reason", jitter.delivery.reason.clone()));
            facts.extend(jitter.reason.clone().map(|r| ("Reason", r)));
            Some(Row {
                check,
                status: jitter.status.clone(),
                value: value.unwrap_or_else(|| "—".to_owned()),
                facts,
                reason: jitter.reason.clone(),
            })
        }
    }
}

/// What the PES checks apply to.
const ALL_STREAMS: &str = "every elementary stream of the analyzed programs";

/// Header problems as "kind count" pairs, in the CLI's order.
fn header_problems(errors: &[crate::report::PesHeaderError]) -> String {
    errors
        .iter()
        .map(|e| format!("{} {}", e.kind, e.count))
        .collect::<Vec<_>>()
        .join(", ")
}

/// The PAT, or the selected program's PMT, of the CLI's `psi_health`.
fn psi_table<'a>(
    health: &'a PsiHealth,
    program: &Program,
    check: Check,
) -> Option<&'a PsiTableHealth> {
    match check {
        Check::PatHealth => health.table(None),
        _ => health.table(Some(program.program_number)),
    }
}

/// The non-zero problem counts of a PAT or PMT, in words.
fn psi_problems(table: &PsiTableHealth) -> Vec<String> {
    [
        (table.crc_error_count, "CRC error"),
        (table.malformed_section_count, "malformed section"),
        (table.incomplete_count, "incomplete table"),
        (table.wrong_table_id_count, "wrong table_id"),
        (table.unusable_packet_count, "unusable packet"),
    ]
    .into_iter()
    .filter(|(count, _)| *count > 0)
    .map(|(count, what)| match count {
        1 => format!("1 {what}"),
        count => format!("{count} {what}s"),
    })
    .collect()
}

/// Why the CLI could not show a table repeating.
fn repetition_reason(reason: &str) -> String {
    match reason {
        "no_complete_interval" => {
            "No complete interval: the table was not seen twice within one PCR segment".to_owned()
        }
        "no_pcr_time_base" => "No PCR time base: no two PCRs in one segment".to_owned(),
        other => other.replace('_', " "),
    }
}

/// The mux leads of the elementary streams of `program`, in PID order;
/// `None` when the CLI reported no mux lead.
pub(crate) fn program_mux_leads<'a>(
    report: &'a AnalysisReport,
    program: &Program,
) -> Option<Vec<&'a MuxLeadPid>> {
    let lead = report.multiplexing.as_ref()?.mux_lead.as_ref()?;
    Some(
        lead.pids
            .iter()
            .filter(|p| p.stream.program_number == program.program_number)
            .collect(),
    )
}

/// The A/V lead difference of `program`, if the CLI reported one.
pub(crate) fn program_av_lead<'a>(
    report: &'a AnalysisReport,
    program: &Program,
) -> Option<&'a AvLeadProgram> {
    let lead = report.multiplexing.as_ref()?.mux_lead.as_ref()?;
    lead.programs
        .iter()
        .find(|p| p.program_number == program.program_number)
}

/// The status of the mux-lead row: a PID status this GUI does not know (shown
/// as the CLI reported it), else `info` when a lead was measured, else
/// `insufficient_data`.
fn mux_lead_status<'a>(pids: &[&'a MuxLeadPid]) -> &'a str {
    pids.iter()
        .map(|p| p.status.as_str())
        .find(|s| !matches!(*s, "info" | "insufficient_data"))
        .or_else(|| pids.iter().any(|p| p.status == "info").then_some("info"))
        .unwrap_or("insufficient_data")
}

/// A lead in milliseconds, with one decimal.
pub(crate) fn lead_ms(ms: Option<f64>) -> String {
    ms.map_or_else(|| "—".to_owned(), |ms| format!("{ms:.1} ms"))
}

/// The worst A/V lead difference of a program, with both PES packets.
fn av_difference_text(av: &AvLeadProgram) -> String {
    let d = &av.difference;
    match (
        &av.audio_pid,
        &d.video_pid,
        d.max_av_lead_difference_ms,
        d.audio_offset_bytes,
        d.video_offset_bytes,
    ) {
        (Some(audio), Some(video), Some(ms), Some(audio_at), Some(video_at)) => format!(
            "{ms:.1} ms: audio PID {audio} (lead {} at 0x{audio_at:08X}) against video PID {video} (lead {} at 0x{video_at:08X})",
            lead_ms(d.audio_lead_ms),
            lead_ms(d.video_lead_ms)
        ),
        _ => format!(
            "not measured: {}",
            av.reason.as_deref().unwrap_or(&av.status).replace('_', " ")
        ),
    }
}

/// The smallest and largest lead of the program's streams, with their PIDs.
fn lead_range(pids: &[&MuxLeadPid]) -> Option<String> {
    let measured = || {
        pids.iter()
            .filter(|p| p.min_lead_ms.is_some() && p.max_lead_ms.is_some())
    };
    let min =
        measured().min_by(|a, b| a.min_lead_ms.unwrap().total_cmp(&b.min_lead_ms.unwrap()))?;
    let max =
        measured().max_by(|a, b| a.max_lead_ms.unwrap().total_cmp(&b.max_lead_ms.unwrap()))?;
    Some(format!(
        "{} (PID {}) to {} (PID {})",
        lead_ms(min.min_lead_ms),
        min.stream.pid,
        lead_ms(max.max_lead_ms),
        max.stream.pid
    ))
}

/// "1 packet" or "N packets".
fn packets(count: u64) -> String {
    match count {
        1 => "1 packet".to_owned(),
        count => format!("{count} packets"),
    }
}

/// The references the CLI could not judge (`insufficient_data`), with why:
/// unavailable analysis rather than findings.
fn undetermined_references(report: &AnalysisReport) -> Option<String> {
    let pids: Vec<String> = report
        .pid_references
        .as_ref()?
        .referenced
        .pids
        .iter()
        .filter(|p| p.status == "insufficient_data")
        .map(reference_text)
        .collect();
    (!pids.is_empty()).then(|| pids.join("; "))
}

/// Why unreferenced PIDs could not (all) be judged: the CLI's reason and the
/// PIDs it could not judge.
fn undetermined_unreferenced(report: &AnalysisReport) -> Option<String> {
    let unreferenced = &report.pid_references.as_ref()?.unreferenced;
    let mut parts: Vec<String> = unreferenced.reason.iter().cloned().collect();
    parts.extend(
        unreferenced
            .pids
            .iter()
            .filter(|p| p.status == "insufficient_data")
            .map(unreferenced_text),
    );
    (!parts.is_empty()).then(|| parts.join("; "))
}

/// One PID that carried unreferenced packets.
fn unreferenced_text(p: &UnreferencedPid) -> String {
    format!(
        "PID {}: {} without a reference for {:.1} ms",
        p.pid,
        packets(p.packet_count),
        p.longest_run_ms
    )
}

/// One referenced PID: its program, role, PID and what the CLI found.
fn reference_text(p: &ReferencedPid) -> String {
    let role = match (p.role.as_str(), &p.stream_type) {
        ("pmt", _) => "PMT".to_owned(),
        ("pcr", _) => "PCR".to_owned(),
        (_, Some(stream_type)) => format!("stream_type {stream_type}"),
        (role, None) => role.to_owned(),
    };
    let detail = match (p.status.as_str(), p.max_silence_ms, &p.reason) {
        ("ok", _, _) => "ok".to_owned(),
        ("absent", Some(ms), _) => format!("no packet for {ms:.1} ms"),
        ("silent", Some(ms), _) => format!("{ms:.1} ms without a packet"),
        (_, _, Some(reason)) => reason.replace('_', " "),
        (status, _, None) => status.replace('_', " "),
    };
    format!("program {} {} ({role}): {detail}", p.program_number, p.pid)
}

/// The label of a PCR step kind of the CLI; an unknown kind is shown as is.
fn pcr_step_label(kind: &str) -> &'static str {
    match kind {
        "wraparound" => "Wraparound",
        "declared" => "Declared",
        "pcr_pid_change" => "PCR PID change",
        "backward" => "Backward (undeclared)",
        "forward_jump" => "Forward jump (undeclared)",
        _ => "Other step",
    }
}

/// The CLI's result of a transport-error or scrambling check.
fn affected_packets(report: &AnalysisReport, check: Check) -> Option<&AffectedPackets> {
    match check {
        Check::TransportErrors => report.transport_errors.as_ref(),
        _ => report.scrambled_packets.as_ref(),
    }
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

/// Horizontal padding inside a table cell.
pub(crate) const CELL_PADDING: f32 = 8.0;
/// Vertical padding inside a table cell.
const CELL_PADDING_Y: f32 = 5.0;

/// Width of `text` on one line.
pub(crate) fn text_width(ui: &egui::Ui, text: egui::RichText) -> f32 {
    egui::WidgetText::from(text)
        .into_galley(
            ui,
            Some(egui::TextWrapMode::Extend),
            f32::INFINITY,
            egui::TextStyle::Body,
        )
        .size()
        .x
}

/// Width of a status badge.
pub(crate) fn badge_width(ui: &egui::Ui, status: &str) -> f32 {
    (text_width(ui, crate::theme::badge_text(&status_label(status)))
        + 2.0 * crate::theme::BADGE_MARGIN.x)
        .max(crate::theme::BADGE_MIN_WIDTH)
}

/// Column widths of the Transport health table: Check takes what Value,
/// Status and Help leave, and those three are as wide as their widest cell.
#[derive(Clone, Copy, Debug, PartialEq)]
pub(crate) struct Columns {
    pub check: f32,
    pub value: f32,
    pub status: f32,
    pub help: f32,
}

/// The narrowest the Check column gets; below it the table is cut off rather
/// than widening the page.
const MIN_CHECK_WIDTH: f32 = 64.0;

impl Columns {
    fn of(ui: &egui::Ui, rows: &[Row], width: f32) -> Self {
        let pad = 2.0 * CELL_PADDING;
        let header = |text: &str| text_width(ui, egui::RichText::new(text).strong());
        let value = rows
            .iter()
            .map(|r| text_width(ui, egui::RichText::new(&r.value)))
            .fold(header("Value"), f32::max)
            + pad;
        let status = rows
            .iter()
            .map(|r| badge_width(ui, &r.status))
            .fold(header("Status"), f32::max)
            + pad;
        let button = text_width(ui, icons::text(icons::INFO).size(INFO_SIZE))
            + 2.0 * ui.spacing().button_padding.x;
        let help = button.max(header("Help")) + pad;
        Self {
            check: (width - value - status - help).max(MIN_CHECK_WIDTH),
            value,
            status,
            help,
        }
    }
}

/// Size of the information icon.
const INFO_SIZE: f32 = 16.0;

/// The Transport health table: a header, then each category with its rows.
fn health_table(ui: &mut egui::Ui, rows: &[Row], state: &mut OverviewState) {
    // The visible width: content wider than the window must not widen the table.
    let width = ui
        .available_width()
        .min(ui.clip_rect().right() - ui.cursor().left());
    let stroke = crate::theme::table_border(ui.visuals());
    let frame = egui::Frame::new()
        .stroke(stroke)
        .corner_radius(4)
        .inner_margin(egui::Margin::same(stroke.width.ceil() as i8));
    let inner = (width - frame.total_margin().sum().x).max(0.0);
    let columns = Columns::of(ui, rows, inner);
    let builder = egui::UiBuilder::new()
        .accessibility_role(egui::Role::Table)
        .accessibility_label("Transport health");
    frame.show(ui, |ui| {
        ui.set_width(inner);
        accessible_scope(ui, builder, |ui| {
            ui.spacing_mut().item_spacing.y = 0.0;
            let header_fill = crate::theme::table_header_fill(ui.visuals());
            table_row(ui, &columns, Some(header_fill), None, |ui, cell| {
                let title = match cell {
                    Cell::Check => "Check",
                    Cell::Value => "Value",
                    Cell::Status => "Status",
                    Cell::Help => "Help",
                };
                ui.label(crate::theme::table_header_text(title, ui.visuals()));
            });
            rule(ui, inner, crate::theme::table_header_rule(ui.visuals()));
            let mut category = None;
            for row in rows {
                if category != Some(row.check.category()) {
                    if category.is_some() {
                        separator(ui, inner);
                    }
                    category = Some(row.check.category());
                    category_row(ui, row.check.category().title(), inner);
                }
                separator(ui, inner);
                let label = format!(
                    "{}: {}, {}",
                    row.check.title(),
                    row.value,
                    status_label(&row.status)
                );
                table_row(ui, &columns, None, Some(&label), |ui, cell| match cell {
                    Cell::Check => {
                        ui.add(egui::Label::new(row.check.title()).wrap());
                    }
                    Cell::Value => {
                        ui.label(&row.value);
                    }
                    Cell::Status => {
                        status_badge(ui, &row.status, Some(row.check));
                    }
                    Cell::Help => {
                        let button = info_button(ui, row.check);
                        if state.take_focus(Focus::Info(row.check)) {
                            button.request_focus();
                        }
                        if button.clicked() {
                            state.open(row.check, Some(Focus::Info(row.check)));
                        }
                    }
                });
            }
        });
    });
}

/// A cell of the Transport health table.
#[derive(Clone, Copy, PartialEq, Eq)]
enum Cell {
    Check,
    Value,
    Status,
    Help,
}

/// One table row: the header row without `label`, otherwise a row named
/// `label`. The check name wraps; the other cells are centered on its
/// height, and Value is right-aligned.
fn table_row(
    ui: &mut egui::Ui,
    columns: &Columns,
    fill: Option<egui::Color32>,
    label: Option<&str>,
    mut add: impl FnMut(&mut egui::Ui, Cell),
) {
    let background = ui.painter().add(egui::Shape::Noop);
    let mut builder = egui::UiBuilder::new()
        .layout(egui::Layout::left_to_right(egui::Align::Min))
        .accessibility_role(egui::Role::Row);
    if let Some(label) = label {
        builder = builder.accessibility_label(label);
    }
    let role = if label.is_some() {
        egui::Role::Cell
    } else {
        egui::Role::ColumnHeader
    };
    // A one-line check name is as tall as the information button and its
    // padding, so a one-line row is centered as a whole.
    let min_height = ui.spacing().interact_size.y + 2.0 * CELL_PADDING_Y;
    let text_height = ui.text_style_height(&egui::TextStyle::Body);
    let padding_y = ((min_height - text_height) / 2.0).max(CELL_PADDING_Y);
    let response = accessible_scope(ui, builder, |ui| {
        ui.spacing_mut().item_spacing = egui::Vec2::ZERO;
        let check = table_cell(
            ui,
            role,
            egui::vec2(columns.check, 0.0),
            egui::Layout::top_down(egui::Align::Min),
            |ui| {
                egui::Frame::new()
                    .inner_margin(egui::Margin::symmetric(
                        CELL_PADDING as i8,
                        padding_y.round() as i8,
                    ))
                    .show(ui, |ui| {
                        ui.set_width(columns.check - 2.0 * CELL_PADDING);
                        add(ui, Cell::Check);
                    });
            },
        );
        // The other cells are centered on the check name.
        let height = check.rect.height().max(min_height);
        for (cell, width, layout) in [
            (
                Cell::Value,
                columns.value,
                egui::Layout::right_to_left(egui::Align::Center),
            ),
            (
                Cell::Status,
                columns.status,
                egui::Layout::left_to_right(egui::Align::Center),
            ),
            (
                Cell::Help,
                columns.help,
                egui::Layout::left_to_right(egui::Align::Center),
            ),
        ] {
            table_cell(ui, role, egui::vec2(width, height), layout, |ui| {
                ui.add_space(CELL_PADDING);
                add(ui, cell);
            });
        }
    });
    if let Some(fill) = fill {
        ui.painter().set(
            background,
            egui::Shape::rect_filled(response.response.rect, 0, fill),
        );
    }
}

/// A table cell of exactly `size` at the cursor of a left-to-right row; a
/// zero height grows with the content. Announced with `role`, so its bounds
/// are the cell's.
pub(crate) fn table_cell(
    ui: &mut egui::Ui,
    role: egui::Role,
    size: egui::Vec2,
    layout: egui::Layout,
    add: impl FnOnce(&mut egui::Ui),
) -> egui::Response {
    let height = if size.y > 0.0 {
        size.y
    } else {
        CELL_MAX_HEIGHT
    };
    let rect = egui::Rect::from_min_size(ui.cursor().min, egui::vec2(size.x, height));
    let builder = egui::UiBuilder::new()
        .max_rect(rect)
        .layout(layout)
        .accessibility_role(role);
    accessible_scope(ui, builder, |ui| {
        ui.set_min_size(size);
        add(ui);
    })
    .response
}

/// `ui.scope_builder`, with the bounds of the scope on its accessibility
/// node, so a table, row or cell can be located like a widget.
pub(crate) fn accessible_scope<R>(
    ui: &mut egui::Ui,
    builder: egui::UiBuilder,
    add: impl FnOnce(&mut egui::Ui) -> R,
) -> egui::InnerResponse<R> {
    let mut id = None;
    let inner = ui.scope_builder(builder, |ui| {
        id = Some(ui.unique_id());
        add(ui)
    });
    if let Some(id) = id {
        let rect = inner.response.rect;
        ui.ctx().accesskit_node_builder(id, |node| {
            node.set_bounds(egui::accesskit::Rect {
                x0: rect.min.x.into(),
                y0: rect.min.y.into(),
                x1: rect.max.x.into(),
                y1: rect.max.y.into(),
            });
        });
    }
    inner
}

/// Room a growing cell may take; any real cell is far shorter.
const CELL_MAX_HEIGHT: f32 = 10_000.0;

/// A category subheader: the title in accent capitals across the table, with
/// room above it, announced as a heading with its title as written.
fn category_row(ui: &mut egui::Ui, title: &str, width: f32) {
    egui::Frame::new()
        .fill(crate::theme::table_category_fill(ui.visuals()))
        .inner_margin(egui::Margin {
            left: CELL_PADDING as i8,
            right: CELL_PADDING as i8,
            top: 6,
            bottom: 5,
        })
        .show(ui, |ui| {
            ui.set_width(width - 2.0 * CELL_PADDING);
            let response = ui.label(crate::theme::table_category_text(title, ui.visuals()));
            response.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Heading, true, title));
        });
}

/// A line between table rows.
fn separator(ui: &mut egui::Ui, width: f32) {
    rule(ui, width, crate::theme::table_divider(ui.visuals()));
}

/// A horizontal line across the table.
pub(crate) fn rule(ui: &mut egui::Ui, width: f32, stroke: egui::Stroke) {
    let (rect, _) =
        ui.allocate_exact_size(egui::vec2(width, stroke.width.ceil()), egui::Sense::hover());
    ui.painter().hline(rect.x_range(), rect.center().y, stroke);
}

/// The focusable button that opens the detail page of a check.
fn info_button(ui: &mut egui::Ui, check: Check) -> egui::Response {
    let name = check.about();
    let button = egui::Button::new(icons::text(icons::INFO).size(INFO_SIZE)).frame(false);
    let response = ui.add(button).on_hover_text(&name);
    response.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Button, true, &name));
    response
}

/// A link that opens the detail page of `check`.
fn help_link(ui: &mut egui::Ui, text: &str, check: Check, state: &mut OverviewState) {
    if ui.link(text).on_hover_text(check.about()).clicked() {
        state.open(check, None);
    }
}

/// A status as a badge text: upper case, with spaces for underscores.
pub(crate) fn status_label(status: &str) -> String {
    status.to_uppercase().replace('_', " ")
}

/// The CLI's status of `check` as a badge, verbatim. A status this GUI does
/// not describe for that check says so in its tooltip. Analyzer warnings
/// (`check` is `None`) carry the GUI's own `warning`.
pub(crate) fn status_badge(
    ui: &mut egui::Ui,
    status: &str,
    check: Option<Check>,
) -> egui::Response {
    let response = badge(ui, &status_label(status), status_verdict(status));
    if check.is_none_or(|check| is_known_status(check, status)) {
        response
    } else {
        response.on_hover_text(format!(
            "Status \"{status}\" as the CLI reported it; this GUI does not describe it for this check."
        ))
    }
}

/// Whether this GUI describes `status` for `check`. A status another check
/// knows (such as `warning`) is still unknown for a check without it.
pub(crate) fn is_known_status(check: Check, status: &str) -> bool {
    check.verdict_meaning(status).is_some()
}

/// Where a finding applies.
#[derive(Clone, Copy, Debug, PartialEq)]
pub(crate) enum Scope {
    /// The selected program, by program_number.
    Program(u16),
    /// Every PID of the range.
    Multiplex,
}

impl Scope {
    pub fn text(self) -> String {
        match self {
            Scope::Program(number) => format!("program {number}"),
            Scope::Multiplex => "whole multiplex".to_owned(),
        }
    }
}

/// One user-visible problem of the analysis.
#[derive(Debug, PartialEq)]
pub(crate) struct Finding {
    /// The CLI's status, shown as it is; `warning` for an analyzer warning.
    pub status: String,
    /// The check it belongs to; analyzer warnings belong to none.
    pub check: Option<Check>,
    pub label: String,
    pub reason: String,
    pub scope: Scope,
    pub offset: Option<u64>,
}

/// Whether a check status means its analysis is unavailable rather than a
/// result: `skipped`, a PCR jitter estimate the CLI did not grade
/// (`not_assessable`, because delivery is not constant), a PAT/PMT whose
/// repetition the range does not show, or PES checks and PID references the
/// CLI could not judge (`insufficient_data`).
/// Only a status this check describes counts: an unknown `not_assessable` on
/// another check stays a finding, shown as the CLI reported it.
pub(crate) fn is_unavailable(check: Check, status: &str) -> bool {
    matches!(status, "skipped" | "not_assessable" | "insufficient_data")
        && is_known_status(check, status)
}

/// Whether a check status is a report without a verdict: the `info` of a
/// report-only check (Mux lead), which is neither a finding nor unavailable
/// analysis.
pub(crate) fn is_report_only(check: Check, status: &str) -> bool {
    status == "info" && is_known_status(check, status)
}

/// Whether a check status is a finding. Unavailable analysis and a report
/// without a verdict are not findings; any other status than `ok`, including
/// one this GUI does not know, is shown as the CLI reported it.
pub(crate) fn is_finding(check: Check, status: &str) -> bool {
    status != "ok" && !is_unavailable(check, status) && !is_report_only(check, status)
}

/// A check whose analysis is unavailable, with the CLI's status and reason.
#[derive(Debug, PartialEq)]
pub(crate) struct Unavailable {
    pub check: Check,
    pub status: String,
    pub reason: String,
}

/// Every finding of the selected program and the multiplex, and the checks
/// whose analysis is unavailable with the CLI's reason. Each underlying event
/// is one finding: a check with events (anomaly groups, continuity errors) is
/// represented by those events, not counted again.
pub(crate) fn findings(
    report: &AnalysisReport,
    program: &Program,
) -> (Vec<Finding>, Vec<Unavailable>) {
    let mut findings = Vec::new();
    let mut unavailable = Vec::new();
    let in_program = Scope::Program(program.program_number);
    for row in rows(report, program) {
        if is_unavailable(row.check, &row.status) {
            let reason = match (row.check, program.pcr_jitter.as_ref()) {
                // The status reason, then the delivery assessment behind it.
                (Check::PcrJitter, Some(jitter)) if row.status == "not_assessable" => format!(
                    "{}. Delivery {}: {}",
                    jitter.reason.as_deref().unwrap_or("not assessable"),
                    jitter.delivery.assessment,
                    jitter.delivery.reason
                ),
                _ => row.reason.unwrap_or_default(),
            };
            unavailable.push(Unavailable {
                check: row.check,
                status: row.status,
                reason,
            });
            continue;
        }
        if !is_finding(row.check, &row.status) {
            continue;
        }
        // A PID reference result with findings can still contain PIDs the CLI
        // could not judge: those are unavailable analysis, with their reason.
        if matches!(row.check, Check::ReferencedPids | Check::UnreferencedPids)
            && let Some(reason) = &row.reason
        {
            unavailable.push(Unavailable {
                check: row.check,
                status: "insufficient_data".to_owned(),
                reason: reason.clone(),
            });
        }
        let check = row.check;
        let finding = |reason: String, scope: Scope, offset: Option<u64>| Finding {
            status: row.status.clone(),
            check: Some(check),
            label: check.title().to_owned(),
            reason,
            scope,
            offset,
        };
        match check {
            Check::MaxPcrInterval | Check::PcrPtsDtsGap => {
                let result = program.compliance.as_ref().map(|c| match check {
                    Check::MaxPcrInterval => &c.max_pcr_interval,
                    _ => &c.max_pcr_pts_dts_gap,
                });
                let reason = result.map_or(String::new(), |result| {
                    result.value_ms.map_or_else(
                        || result.reason.clone().unwrap_or_default(),
                        |ms| format!("{ms:.1} ms, limit {:.0} ms", result.limit_ms),
                    )
                });
                findings.push(finding(reason, in_program, None));
            }
            Check::PcrDiscontinuities => {
                // One finding per undeclared kind, at its first PCR.
                let items: Vec<_> = program
                    .compliance
                    .as_ref()
                    .and_then(|c| c.pcr_discontinuities.as_ref())
                    .map_or(&[][..], |d| &d.items[..])
                    .iter()
                    .filter(|item| matches!(item.kind.as_str(), "backward" | "forward_jump"))
                    .collect();
                if items.is_empty() {
                    findings.push(finding(String::new(), in_program, None));
                }
                for item in items {
                    findings.push(finding(
                        format!(
                            "{} without discontinuity_indicator · {}× · PCR jumped {:+.3} ms",
                            item.kind.replace('_', " "),
                            item.count,
                            item.first_jump_ms
                        ),
                        in_program,
                        Some(item.first_offset_bytes),
                    ));
                }
            }
            Check::TimestampAnomalies => {
                let items = program
                    .timestamp_anomalies
                    .as_ref()
                    .map_or(&[][..], |a| &a.items[..]);
                if items.is_empty() {
                    findings.push(finding(String::new(), in_program, None));
                }
                for item in items {
                    findings.push(finding(
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
            Check::ContinuityCounter => {
                if report.continuity_events.is_empty() {
                    findings.push(finding(
                        match row.value.as_str() {
                            "1" => "1 error".to_owned(),
                            count => format!("{count} errors"),
                        },
                        Scope::Multiplex,
                        None,
                    ));
                }
                for event in &report.continuity_events {
                    findings.push(finding(
                        format!(
                            "PID {}: continuity_counter {} (expected {})",
                            event.pid, event.actual_cc, event.expected_cc
                        ),
                        Scope::Multiplex,
                        Some(event.offset_bytes),
                    ));
                }
            }
            Check::TransportErrors | Check::ScrambledPackets => {
                // One finding per PID, at its first affected packet.
                for pid in affected_packets(report, check).map_or(&[][..], |r| &r.pids[..]) {
                    let packets = match pid.packet_count {
                        1 => "1 packet".to_owned(),
                        count => format!("{count} packets"),
                    };
                    findings.push(finding(
                        format!(
                            "PID {} ({}): {packets}, {} excluded from analysis",
                            pid.pid, pid.label, pid.excluded_packet_count
                        ),
                        Scope::Multiplex,
                        Some(pid.first_offset_bytes),
                    ));
                }
            }
            Check::PesHeaders => {
                // One finding per PID with errors, at its first one.
                let pids = report.pes_headers.as_ref().map_or(&[][..], |h| &h.pids[..]);
                for pid in pids.iter().filter(|p| p.counts.error_count > 0) {
                    let counts = &pid.counts;
                    findings.push(finding(
                        format!(
                            "PID {} ({}): {} of {} headers ({})",
                            pid.stream.pid,
                            pid.stream.label,
                            counts.error_count,
                            counts.checked_count,
                            header_problems(&counts.errors)
                        ),
                        Scope::Program(pid.stream.program_number),
                        counts.first_error_offset_bytes,
                    ));
                }
            }
            Check::PtsRepetition => {
                // One finding per PID over the limit, at the first interval
                // that exceeded it.
                let pids = report
                    .pts_repetition
                    .as_ref()
                    .map_or(&[][..], |r| &r.pids[..]);
                for pid in pids.iter().filter(|p| p.counts.status == "ng") {
                    let counts = &pid.counts;
                    let exceeded = match counts.exceeded_count {
                        1 => "1 interval".to_owned(),
                        count => format!("{count} intervals"),
                    };
                    findings.push(finding(
                        format!(
                            "PID {} ({}): {exceeded} over 700 ms, longest {:.1} ms",
                            pid.stream.pid,
                            pid.stream.label,
                            counts.max_interval_ms.unwrap_or_default()
                        ),
                        Scope::Program(pid.stream.program_number),
                        counts.first_exceeded_offset_bytes,
                    ));
                }
            }
            Check::MuxLead => {
                // Only a status this GUI does not know: one finding per PID
                // with it, at its largest lead.
                for pid in program_mux_leads(report, program)
                    .unwrap_or_default()
                    .into_iter()
                    .filter(|p| p.status == row.status)
                {
                    findings.push(finding(
                        format!(
                            "PID {} ({}): lead {} to {}",
                            pid.stream.pid,
                            pid.stream.label,
                            lead_ms(pid.min_lead_ms),
                            lead_ms(pid.max_lead_ms)
                        ),
                        Scope::Program(pid.stream.program_number),
                        pid.max_lead_offset_bytes,
                    ));
                }
            }
            Check::PatHealth | Check::PmtHealth => {
                let health = report.psi_health.as_ref();
                let table = health.and_then(|health| psi_table(health, program, check));
                let limit = health.map_or(0.0, |h| h.repetition_limit_ms);
                let (scope, table_name) = match check {
                    Check::PatHealth => (Scope::Multiplex, "PAT".to_owned()),
                    _ => (in_program, "PMT".to_owned()),
                };
                let reason = table.map_or(String::new(), |table| {
                    let mut parts = Vec::new();
                    let rep = &table.repetition;
                    if let Some(ms) = rep.max_interval_ms
                        && rep.late_interval_count > 0
                    {
                        let what = if table.instance_count == 0 {
                            format!("no {table_name} found")
                        } else {
                            format!("{} late interval(s)", rep.late_interval_count)
                        };
                        parts.push(format!("{what}, longest {ms:.1} ms (limit {limit:.0} ms)"));
                    }
                    parts.extend(psi_problems(table));
                    format!("PID {}: {}", table.pid, parts.join(", "))
                });
                let offset = table.and_then(|t| {
                    t.first_problem_offset_bytes
                        .or(t.repetition.first_late_end_offset_bytes)
                });
                findings.push(finding(reason, scope, offset));
            }
            Check::Synchronization => {
                let sync = report.sync.as_ref();
                let reason = sync.map_or(String::new(), |s| {
                    let losses = match s.loss_count {
                        1 => "1 loss".to_owned(),
                        n => format!("{n} losses"),
                    };
                    let mut reason = format!("{losses}, {} bytes skipped", s.skipped_bytes);
                    if let Some(text) = &s.reason {
                        reason = format!("{reason}; {text}");
                    }
                    reason
                });
                findings.push(finding(
                    reason,
                    Scope::Multiplex,
                    sync.and_then(|s| s.first_loss_offset_bytes),
                ));
            }
            Check::ReferencedPids => {
                // One finding per reference with a finding status, with its
                // own status, at the packet before its silence; references
                // the CLI could not judge are unavailable analysis.
                let pids = report
                    .pid_references
                    .as_ref()
                    .map_or(&[][..], |r| &r.referenced.pids[..]);
                for pid in pids.iter().filter(|p| is_finding(check, &p.status)) {
                    findings.push(Finding {
                        status: pid.status.clone(),
                        ..finding(
                            reference_text(pid),
                            Scope::Multiplex,
                            Some(
                                pid.max_silence_start_offset_bytes
                                    .unwrap_or(pid.referenced_from_offset_bytes),
                            ),
                        )
                    });
                }
            }
            Check::UnreferencedPids => {
                // One finding per PID with a finding status, at its first
                // unreferenced packet; PIDs the CLI could not judge are
                // unavailable analysis.
                let pids: Vec<&UnreferencedPid> = report
                    .pid_references
                    .as_ref()
                    .map_or(&[][..], |r| &r.unreferenced.pids[..])
                    .iter()
                    .filter(|p| is_finding(check, &p.status))
                    .collect();
                if pids.is_empty() {
                    findings.push(finding(
                        row.reason.clone().unwrap_or_default(),
                        Scope::Multiplex,
                        None,
                    ));
                }
                for pid in pids {
                    findings.push(Finding {
                        status: pid.status.clone(),
                        ..finding(
                            unreferenced_text(pid),
                            Scope::Multiplex,
                            Some(pid.first_offset_bytes),
                        )
                    });
                }
            }
            Check::PcrJitter => {
                let jitter = program.pcr_jitter.as_ref();
                let limit = match row.status.as_str() {
                    "ng" => " beyond ±25 µs",
                    "warning" => " beyond ±500 ns",
                    _ => "",
                };
                let reason = jitter.map_or(String::new(), |j| {
                    j.estimate.max_abs_jitter_ns.map_or(String::new(), |ns| {
                        format!(
                            "max |estimated jitter| {}{limit}; estimated from byte position, {}",
                            duration_ns(ns),
                            j.delivery.summary()
                        )
                    })
                });
                findings.push(finding(
                    reason,
                    in_program,
                    jitter.and_then(|j| j.estimate.max_abs_jitter_offset_bytes),
                ));
            }
        }
    }
    for warning in &report.warnings {
        findings.push(Finding {
            status: "warning".to_owned(),
            check: None,
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
        status_badge(ui, &finding.status, finding.check);
        match finding.check {
            Some(check) => help_link(ui, &finding.label, check, state),
            None => {
                ui.strong(&finding.label);
            }
        }
        ui.weak(finding.scope.text());
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
                (
                    "ng",
                    "PCR jitter estimate",
                    Scope::Program(1),
                    Some(58 * 188)
                ),
                ("ng", "PCR → PTS/DTS gap", Scope::Program(1), None),
                ("anomalies", "PTS/DTS anomalies", Scope::Program(1), None),
                ("errors", "PES headers", Scope::Program(1), Some(106 * 188)),
                ("ng", "PTS repetition", Scope::Program(1), Some(101 * 188)),
                (
                    "errors",
                    "Continuity counter",
                    Scope::Multiplex,
                    Some(25 * 188)
                ),
                (
                    "affected",
                    "Transport errors",
                    Scope::Multiplex,
                    Some(7 * 188)
                ),
                (
                    "affected",
                    "Scrambled packets",
                    Scope::Multiplex,
                    Some(11 * 188)
                ),
                ("ng", "PAT health", Scope::Multiplex, Some(79 * 188)),
                ("ng", "PMT health", Scope::Program(1), Some(83 * 188)),
            ]
        );
        assert_eq!(
            findings[9].reason,
            "PID 0x0000: 1 late interval(s), longest 800.0 ms (limit 500 ms)"
        );
        assert_eq!(
            findings[10].reason,
            "PID 0x1000: 1 late interval(s), longest 800.0 ms (limit 500 ms)"
        );
        let rest = &findings[..9];
        assert_eq!(rest[0].reason, "160.0 ms, limit 100 ms");
        assert_eq!(
            rest[1].reason,
            "max |estimated jitter| 100.0 µs beyond ±25 µs; estimated from byte position, delivery constant: 2 × 1 s windows, max deviation 0.00 % of 75.2 kbit/s"
        );
        assert_eq!(rest[2].reason, "1200.0 ms, limit 1000 ms");
        assert!(
            rest[3]
                .reason
                .starts_with("PID 0x0100 · backward · 1× · PTS went backward")
        );
        assert_eq!(
            rest[4].reason,
            "PID 0x0100 (video): 1 of 58 headers (bad_marker_bits 1)"
        );
        assert_eq!(
            rest[5].reason,
            "PID 0x0101 (audio): 1 interval over 700 ms, longest 800.0 ms"
        );
        assert_eq!(
            rest[6].reason,
            "PID 0x0101: continuity_counter 6 (expected 5)"
        );
        for finding in &rest[7..] {
            assert_eq!(
                finding.reason,
                "PID 0x1fff (null): 1 packet, 0 excluded from analysis"
            );
        }
        // Each row is non-OK and has its finding, except TS synchronization
        // (the packets of this sample are aligned; see the sync-loss sample),
        // PCR discontinuities (its PCR clock is continuous; see the
        // PCR-discontinuities sample) and the PID references (its PIDs are
        // all referenced and present; see the PID-reference sample).
        for row in rows(&report, &report.programs[0]) {
            if matches!(
                row.check,
                Check::Synchronization | Check::ReferencedPids | Check::UnreferencedPids
            ) {
                assert_eq!(row.status, "ok");
                continue;
            }
            if row.check == Check::PcrDiscontinuities {
                assert_eq!((row.status.as_str(), row.value.as_str()), ("ok", "0"));
                continue;
            }
            // A report without a verdict is never a finding.
            if row.check == Check::MuxLead {
                assert_eq!(row.status, "info");
                assert!(!is_finding(row.check, &row.status));
                assert!(!is_unavailable(row.check, &row.status));
                assert!(findings.iter().all(|f| f.check != Some(row.check)));
                continue;
            }
            assert!(is_finding(row.check, &row.status), "{:?}", row.check);
            assert!(findings.iter().any(|f| f.check == Some(row.check)));
        }
    }

    /// Mux lead: negative leads, a program without a video and audio pair,
    /// streams without a lead and a status this GUI does not know.
    #[test]
    fn mux_lead_rows() {
        let document = include_str!("../../tests/fixtures/analysis_mux_lead_188.json");
        let report = report(document);
        let program = &report.programs[0];
        let row = row(&report, program, Check::MuxLead).unwrap();
        assert_eq!(
            (row.status.as_str(), row.value.as_str()),
            ("info", "800.0 ms")
        );
        let fact = |label: &str| {
            row.facts
                .iter()
                .find(|(l, _)| *l == label)
                .map(|(_, v)| v.clone())
        };
        assert_eq!(
            fact("Lead range").as_deref(),
            Some("-20.0 ms (PID 0x0102) to 1500.0 ms (PID 0x0101)")
        );
        assert_eq!(
            fact("Negative leads").as_deref(),
            Some("1 PES (multiplexed after their DTS/PTS)")
        );
        assert_eq!(fact("Unbracketed").as_deref(), Some("4 PES"));
        let (all, unavailable) = findings(&report, program);
        assert!(all.iter().all(|f| f.check != Some(Check::MuxLead)));
        assert!(unavailable.iter().all(|u| u.check != Check::MuxLead));
        assert_eq!(program_mux_leads(&report, program).unwrap().len(), 4);

        // An audio stream never compared: not measured, with its reason.
        let mut edited: serde_json::Value = serde_json::from_str(document).unwrap();
        let av = &mut edited["multiplexing"]["mux_lead"]["programs"][0];
        av["status"] = "insufficient_data".into();
        av["reason"] = "no_comparable_leads".into();
        for key in [
            "audio_pid",
            "video_pid",
            "max_av_lead_difference_ms",
            "audio_lead_ms",
            "video_lead_ms",
            "audio_offset_bytes",
            "video_offset_bytes",
        ] {
            av.as_object_mut().unwrap().remove(key);
        }
        let report = self::report(&edited.to_string());
        let row = self::row(&report, &report.programs[0], Check::MuxLead).unwrap();
        assert_eq!(row.value, "—");
        assert_eq!(row.facts[0].1, "not measured: no comparable leads");

        // No video and audio pair, and no stream with a lead: unavailable
        // analysis with the first PID's reason.
        edited["multiplexing"]["mux_lead"]["programs"] = serde_json::json!([]);
        for pid in edited["multiplexing"]["mux_lead"]["pids"]
            .as_array_mut()
            .unwrap()
        {
            let pid = pid.as_object_mut().unwrap();
            pid.insert("status".to_owned(), "insufficient_data".into());
            pid.insert("reason".to_owned(), "no_pcr_timing".into());
            for key in ["min_lead_ms", "max_lead_ms", "spread_ms"] {
                pid.remove(key);
            }
        }
        let report = self::report(&edited.to_string());
        let program = &report.programs[0];
        let row = self::row(&report, program, Check::MuxLead).unwrap();
        assert_eq!(
            (row.status.as_str(), row.value.as_str()),
            ("insufficient_data", "—")
        );
        assert_eq!(
            row.facts[0].1,
            "not measured: the program has no video and audio pair"
        );
        assert!(row.facts.iter().all(|(l, _)| *l != "Lead range"));
        let (_, unavailable) = findings(&report, program);
        assert!(unavailable.contains(&Unavailable {
            check: Check::MuxLead,
            status: "insufficient_data".to_owned(),
            reason: "no_pcr_timing".to_owned()
        }));

        // A status this GUI does not know is a finding per PID.
        edited["multiplexing"]["mux_lead"]["pids"][1]["status"] = "ng".into();
        edited["multiplexing"]["mux_lead"]["pids"][1]["max_lead_offset_bytes"] = 376.into();
        let report = self::report(&edited.to_string());
        let program = &report.programs[0];
        assert_eq!(
            self::row(&report, program, Check::MuxLead).unwrap().status,
            "ng"
        );
        let (all, _) = findings(&report, program);
        let leads: Vec<_> = all
            .iter()
            .filter(|f| f.check == Some(Check::MuxLead))
            .map(|f| (f.status.as_str(), f.reason.as_str(), f.offset))
            .collect();
        assert_eq!(
            leads,
            [("ng", "PID 0x0101 (audio): lead — to —", Some(376))]
        );

        // A program without elementary streams in the report.
        edited["multiplexing"]["mux_lead"]["pids"] = serde_json::json!([]);
        let report = self::report(&edited.to_string());
        let row = self::row(&report, &report.programs[0], Check::MuxLead).unwrap();
        assert_eq!(
            row.reason.as_deref(),
            Some("program 1 has no elementary stream")
        );

        // No mux lead, or no multiplexing result: no row.
        edited["multiplexing"] = serde_json::json!({});
        let report = self::report(&edited.to_string());
        assert!(self::row(&report, &report.programs[0], Check::MuxLead).is_none());
        edited["multiplexing"] = serde_json::Value::Null;
        let report = self::report(&edited.to_string());
        assert!(self::row(&report, &report.programs[0], Check::MuxLead).is_none());
        assert!(program_av_lead(&report, &report.programs[0]).is_none());
    }

    /// The PAT row covers the multiplex and the PMT row the selected
    /// program's PMT; a PAT/PMT without enough evidence is unavailable
    /// analysis with the CLI's reason, a failed one a finding at its first
    /// problem.
    #[test]
    fn pat_and_pmt_health_rows() {
        let report = report(include_str!(
            "../../tests/fixtures/analysis_two_programs_188.json"
        ));
        let pmt = row(&report, &report.programs[1], Check::PmtHealth).unwrap();
        assert_eq!(
            (pmt.status.as_str(), pmt.value.as_str()),
            ("ok", "139.1 ms")
        );
        let fact = |row: &Row, name: &str| {
            row.facts
                .iter()
                .find(|(label, _)| *label == name)
                .map(|(_, value)| value.clone())
        };
        assert_eq!(fact(&pmt, "PID").as_deref(), Some("0x1001"));
        assert_eq!(fact(&pmt, "Program").as_deref(), Some("2"));
        assert_eq!(fact(&pmt, "Tables").as_deref(), Some("22"));
        assert_eq!(fact(&pmt, "Limit").as_deref(), Some("500 ms"));
        assert_eq!(
            fact(&pmt, "Time base").as_deref(),
            Some("PCR PID 0x0100 of program 1, 1 segment(s)")
        );
        assert_eq!(fact(&pmt, "Problems"), None);
        let pat = row(&report, &report.programs[1], Check::PatHealth).unwrap();
        assert_eq!(
            fact(&pat, "Applies to").as_deref(),
            Some("the whole multiplex")
        );

        let mut document: serde_json::Value = serde_json::from_str(include_str!(
            "../../tests/fixtures/analysis_two_programs_188.json"
        ))
        .unwrap();
        // A CLI that does not name the PCR_PID of a measured time base.
        document["psi_health"]["time_base"]
            .as_object_mut()
            .unwrap()
            .remove("pcr_pid");
        let tables = &mut document["psi_health"]["tables"];
        tables[0]["status"] = "insufficient_data".into();
        tables[0]["repetition"] = serde_json::json!({
            "status": "skipped", "reason": "no_pcr_time_base", "interval_count": 0, "late_interval_count": 0
        });
        tables[2]["status"] = "ng".into();
        tables[2]["reasons"] = serde_json::json!(["missing", "crc_error"]);
        tables[2]["instance_count"] = 0.into();
        tables[2]["crc_error_count"] = 2.into();
        tables[2]["first_problem_offset_bytes"] = 1316.into();
        tables[2]["repetition"] = serde_json::json!({
            "status": "late", "interval_count": 0, "max_interval_ms": 900.0,
            "max_interval_end_offset_bytes": 9400, "late_interval_count": 1, "first_late_end_offset_bytes": 9400
        });
        let report = super::tests::report(&document.to_string());
        let program = &report.programs[1];
        let pat = row(&report, program, Check::PatHealth).unwrap();
        assert_eq!(
            fact(&pat, "Time base").as_deref(),
            Some("PCR of program 1, 1 segment(s)")
        );
        assert_eq!(pat.value, "—");
        assert_eq!(
            pat.reason.as_deref(),
            Some("No PCR time base: no two PCRs in one segment")
        );
        let (findings, unavailable) = findings(&report, program);
        assert!(
            unavailable
                .iter()
                .any(|u| u.check == Check::PatHealth && u.status == "insufficient_data")
        );
        let pmt = findings
            .iter()
            .find(|f| f.check == Some(Check::PmtHealth))
            .unwrap();
        assert_eq!(
            (pmt.scope, pmt.offset, pmt.reason.as_str()),
            (
                Scope::Program(2),
                Some(1316),
                "PID 0x1001: no PMT found, longest 900.0 ms (limit 500 ms), 2 CRC errors"
            )
        );
        let row = row(&report, program, Check::PmtHealth).unwrap();
        assert_eq!(row.value, "900.0 ms");
        assert_eq!(fact(&row, "Problems").as_deref(), Some("2 CRC errors"));
        assert_eq!(fact(&row, "Reasons").as_deref(), Some("missing, crc_error"));
        assert_eq!(
            psi_problems(&crate::report::PsiTableHealth {
                table: "pmt".into(),
                program_number: Some(1),
                pid: "0x1000".into(),
                status: "ng".into(),
                reasons: vec![],
                instance_count: 0,
                version_change_count: 0,
                crc_error_count: 0,
                malformed_section_count: 1,
                incomplete_count: 2,
                wrong_table_id_count: 1,
                unusable_packet_count: 3,
                first_problem_offset_bytes: None,
                repetition: crate::report::PsiRepetition {
                    status: "skipped".into(),
                    reason: Some("other_reason".into()),
                    interval_count: 0,
                    max_interval_ms: None,
                    max_interval_end_offset_bytes: None,
                    late_interval_count: 0,
                    first_late_end_offset_bytes: None,
                },
            }),
            [
                "1 malformed section",
                "2 incomplete tables",
                "1 wrong table_id",
                "3 unusable packets"
            ]
        );
        assert_eq!(
            repetition_reason("no_complete_interval"),
            "No complete interval: the table was not seen twice within one PCR segment"
        );
        assert_eq!(repetition_reason("other_reason"), "other reason");
    }

    /// The PCR-discontinuities sample: one finding per undeclared kind at its
    /// first PCR; the legal kinds are listed in the row only.
    #[test]
    fn findings_of_the_pcr_discontinuities_sample() {
        let report = report(include_str!(
            "../../tests/fixtures/analysis_pcr_discontinuities_188.json"
        ));
        let program = &report.programs[0];
        let (findings, _) = findings(&report, program);
        let discontinuities: Vec<_> = findings
            .iter()
            .filter(|f| f.check == Some(Check::PcrDiscontinuities))
            .map(|f| (f.status.as_str(), f.reason.as_str(), f.offset))
            .collect();
        assert_eq!(
            discontinuities,
            [
                (
                    "ng",
                    "backward without discontinuity_indicator · 1× · PCR jumped -1960.000 ms",
                    Some(140 * 188)
                ),
                (
                    "ng",
                    "forward jump without discontinuity_indicator · 1× · PCR jumped +4040.000 ms",
                    Some(170 * 188)
                ),
            ]
        );
        let ng = row(&report, program, Check::PcrDiscontinuities).unwrap();
        assert_eq!((ng.status.as_str(), ng.value.as_str()), ("ng", "2"));
        let facts: Vec<(&str, &str)> = ng.facts.iter().map(|(l, v)| (*l, v.as_str())).collect();
        for fact in [
            ("Undeclared", "2"),
            ("PCRs", "127"),
            ("Wraparound", "1×, first at 0x000024B8 (+40.000 ms)"),
            ("Declared", "2×, first at 0x00003AC0 (+5040.000 ms)"),
            ("PCR PID change", "1×, first at 0x00009458 (+40.000 ms)"),
        ] {
            assert!(facts.contains(&fact), "{fact:?} in {facts:?}");
        }
        // An earlier 2.0 CLI without the check: no row.
        let mut document: serde_json::Value = serde_json::from_str(include_str!(
            "../../tests/fixtures/analysis_pcr_discontinuities_188.json"
        ))
        .unwrap();
        document["programs"][0]["compliance"]
            .as_object_mut()
            .unwrap()
            .remove("pcr_discontinuities");
        let older: AnalysisReport = serde_json::from_value(document).unwrap();
        assert!(row(&older, &older.programs[0], Check::PcrDiscontinuities).is_none());
    }

    /// Analyzer warnings, unknown statuses and skipped checks; a check without
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
        // Insufficient data is unavailable analysis with the CLI's reason.
        document["pts_repetition"]["status"] = "insufficient_data".into();
        document["pts_repetition"]["reason"] = "single_pts".into();
        let report = report(&document.to_string());
        let (findings, unavailable) = findings(&report, &report.programs[0]);
        let summary: Vec<_> = findings
            .iter()
            .map(|f| (f.status.as_str(), f.label.as_str(), f.scope))
            .collect();
        assert_eq!(
            summary,
            [
                ("indeterminate", "PCR jitter estimate", Scope::Program(1)),
                ("anomalies", "PTS/DTS anomalies", Scope::Program(1)),
                ("errors", "PES headers", Scope::Program(1)),
                ("errors", "Continuity counter", Scope::Multiplex),
                ("affected", "Transport errors", Scope::Multiplex),
                ("affected", "Scrambled packets", Scope::Multiplex),
                ("ng", "PAT health", Scope::Multiplex),
                ("ng", "PMT health", Scope::Program(1)),
                ("warning", "Analyzer warning", Scope::Multiplex),
            ]
        );
        assert!(
            findings[0]
                .reason
                .starts_with("max |estimated jitter| 100.0 µs;")
        );
        assert_eq!(findings[3].reason, "1 error");
        assert_eq!(
            unavailable,
            [
                Unavailable {
                    check: Check::PcrPtsDtsGap,
                    status: "skipped".to_owned(),
                    reason: "need a parsed PTS/DTS bracketed by PCR observations".to_owned()
                },
                Unavailable {
                    check: Check::PtsRepetition,
                    status: "insufficient_data".to_owned(),
                    reason: "single_pts".to_owned()
                }
            ]
        );
        assert!(is_unavailable(Check::PesHeaders, "insufficient_data"));
        assert!(!is_unavailable(Check::MaxPcrInterval, "insufficient_data"));
        assert!(!is_known_status(Check::PcrJitter, "indeterminate"));
        assert!(is_known_status(Check::PcrJitter, "not_assessable"));
        // Known for PCR jitter, but not a status Max PCR interval describes.
        assert!(is_known_status(Check::PcrJitter, "warning"));
        assert!(!is_known_status(Check::MaxPcrInterval, "warning"));
        assert!(!is_known_status(Check::MaxPcrInterval, "not_assessable"));
        // Unavailable only where the check describes the status; elsewhere
        // an unknown status stays a finding.
        assert!(is_unavailable(Check::PcrJitter, "not_assessable"));
        assert!(is_unavailable(Check::MaxPcrInterval, "skipped"));
        assert!(!is_unavailable(Check::MaxPcrInterval, "not_assessable"));
        assert!(is_finding(Check::MaxPcrInterval, "not_assessable"));
        assert!(!is_unavailable(Check::ContinuityCounter, "skipped"));
        assert!(is_finding(Check::ContinuityCounter, "skipped"));
    }

    /// A PCR jitter estimate the CLI did not grade is unavailable analysis
    /// with the CLI's reasons, not a finding; the row keeps the estimate.
    #[test]
    fn not_assessable_jitter_is_unavailable() {
        let report = report(include_str!(
            "../../tests/fixtures/analysis_two_programs_188.json"
        ));
        let program = &report.programs[0];
        let (findings, unavailable) = findings(&report, program);
        assert!(findings.iter().all(|f| f.check != Some(Check::PcrJitter)));
        assert_eq!(unavailable.len(), 1);
        assert_eq!(unavailable[0].check, Check::PcrJitter);
        assert_eq!(unavailable[0].status, "not_assessable");
        assert!(
            unavailable[0].reason.starts_with(
                "delivery rate is variable, so the byte-position estimate does not measure PCR accuracy. Delivery variable: the estimated jitter reaches 55.082ms"
            ),
            "{}",
            unavailable[0].reason
        );
        let row = row(&report, program, Check::PcrJitter).unwrap();
        assert_eq!(row.value, "55.08 ms");
        assert_eq!(status_label(&row.status), "NOT ASSESSABLE");
        let fact = |name: &str| {
            row.facts
                .iter()
                .find(|(label, _)| *label == name)
                .map(|(_, value)| value.as_str())
        };
        assert_eq!(fact("Delivery"), Some("variable"));
        assert_eq!(
            fact("Delivery evidence"),
            Some("1 × 1 s window at 767.9 kbit/s")
        );
        assert_eq!(fact("Worst offset"), Some("0x000223D8"));
        assert_eq!(fact("PCR PID"), Some("0x0100"));
        assert_eq!(fact("Observations"), Some("21"));
        assert!(
            fact("Reason")
                .unwrap()
                .starts_with("delivery rate is variable")
        );
        // No threshold: the CLI does not report one for PCR jitter.
        assert_eq!(fact("Limit"), None);
    }

    /// The current result lists only what the CLI reported.
    #[test]
    fn rows_list_only_reported_fields() {
        let mut document: serde_json::Value = serde_json::from_str(include_str!(
            "../../tests/fixtures/analysis_two_programs_188.json"
        ))
        .unwrap();
        document["programs"][0]["compliance"]["max_pcr_interval"] = serde_json::json!({
            "status": "skipped", "limit_ms": 100.0, "observations": 0,
            "reason": "need at least two PCRs"
        });
        document["programs"][0]["pcr_jitter"] = serde_json::Value::Null;
        let report = report(&document.to_string());
        let program = &report.programs[0];
        let rows = rows(&report, program);
        // A check the CLI did not report has no row.
        assert_eq!(
            rows.iter().map(|r| r.check).collect::<Vec<_>>(),
            [
                Check::MaxPcrInterval,
                Check::PcrPtsDtsGap,
                Check::TimestampAnomalies,
                Check::PesHeaders,
                Check::PtsRepetition,
                Check::ContinuityCounter,
                Check::TransportErrors,
                Check::ScrambledPackets,
                Check::Synchronization,
                Check::PatHealth,
                Check::PmtHealth,
                Check::ReferencedPids,
                Check::UnreferencedPids,
                Check::MuxLead,
            ]
        );
        // Mux lead of program 1 only: its A/V difference and lead range.
        let lead = rows.last().unwrap();
        assert_eq!(
            (lead.status.as_str(), lead.value.as_str()),
            ("info", "395.8 ms")
        );
        assert_eq!(
            lead.facts,
            [
                (
                    "A/V lead difference",
                    "395.8 ms: audio PID 0x0101 (lead 324.9 ms at 0x00024E70) against video PID 0x0100 (lead 720.7 ms at 0x0002A690)"
                        .to_owned()
                ),
                (
                    "Lead range",
                    "324.9 ms (PID 0x0101) to 721.5 ms (PID 0x0100)".to_owned()
                ),
                ("Unbracketed", "4 PES".to_owned()),
                ("Time basis", "PCR-interpolated file position".to_owned()),
                ("Verdict", "none: report only".to_owned()),
                ("Applies to", "the elementary streams of program 1".to_owned()),
            ]
        );
        assert!(matches!(
            status_verdict("info"),
            crate::theme::Verdict::Neutral
        ));
        // A healthy PAT: no problem, reason or first problem.
        let pat: Vec<&str> = rows[9].facts.iter().map(|(label, _)| *label).collect();
        assert_eq!(
            pat,
            [
                "Tables",
                "Longest interval",
                "Longest interval ends",
                "Limit",
                "Complete intervals",
                "Late intervals",
                "Version changes",
                "PID",
                "Applies to",
                "Time base"
            ]
        );
        let labels: Vec<&str> = rows[0].facts.iter().map(|(label, _)| *label).collect();
        assert_eq!(
            labels,
            ["Limit", "Program", "PCR PID", "Observations", "Reason"]
        );
        assert_eq!(rows[0].value, "—");
        // Healthy PES checks: no problems, PIDs or first offset.
        let headers: Vec<&str> = rows[3].facts.iter().map(|(label, _)| *label).collect();
        assert_eq!(headers, ["Errors", "Checked", "Applies to"]);
        assert_eq!(
            (rows[3].status.as_str(), rows[3].value.as_str()),
            ("ok", "0")
        );
        let repetition: Vec<&str> = rows[4].facts.iter().map(|(label, _)| *label).collect();
        assert_eq!(
            repetition,
            [
                "Value",
                "Limit",
                "Applies to",
                "Intervals",
                "PTS",
                "Exceeded",
                "Worst interval"
            ]
        );
        assert_eq!(rows[4].value, "414.3 ms");
        let continuity: Vec<&str> = rows[5].facts.iter().map(|(label, _)| *label).collect();
        assert_eq!(continuity, ["Errors", "Applies to"]);
        // A healthy transport check: no PID list and no first offset.
        let tei: Vec<&str> = rows[6].facts.iter().map(|(label, _)| *label).collect();
        assert_eq!(tei, ["Packets", "Excluded", "Applies to"]);
        assert_eq!(
            (rows[6].status.as_str(), rows[6].value.as_str()),
            ("ok", "0")
        );
        // Without a loss, only the counts and the method.
        let sync: Vec<&str> = rows[8].facts.iter().map(|(label, _)| *label).collect();
        assert_eq!(
            sync,
            [
                "Losses",
                "Resyncs",
                "Skipped bytes",
                "Realignment",
                "Applies to"
            ]
        );
        assert_eq!(rows[8].value, "0");
        // Healthy PID references: counts, limits and scope only.
        let referenced: Vec<&str> = rows[11].facts.iter().map(|(label, _)| *label).collect();
        assert_eq!(
            referenced,
            ["References", "Silence limit", "Time basis", "Applies to"]
        );
        assert_eq!(
            (rows[11].status.as_str(), rows[11].value.as_str()),
            ("ok", "0 of 8")
        );
        let unreferenced: Vec<&str> = rows[12].facts.iter().map(|(label, _)| *label).collect();
        assert_eq!(unreferenced, ["Grace period", "Applies to"]);
        assert_eq!(rows[12].value, "0");
    }

    /// The PID-reference sample: each reference that is not ok and each
    /// unreferenced PID is a finding with its own status.
    #[test]
    fn pid_reference_rows_and_findings() {
        let document = include_str!("../../tests/fixtures/analysis_pid_references_188.json");
        let report = report(document);
        let program = &report.programs[0];
        let referenced = row(&report, program, Check::ReferencedPids).unwrap();
        assert_eq!(
            (referenced.status.as_str(), referenced.value.as_str()),
            ("absent", "3 of 8")
        );
        assert_eq!(
            referenced.facts,
            [
                ("References", "8".to_owned()),
                (
                    "Absent",
                    "program 1 0x0103 (stream_type 0x04): no packet for 13175.0 ms".to_owned()
                ),
                (
                    "Silent",
                    "program 1 0x0101 (stream_type 0x03): 6100.0 ms without a packet".to_owned()
                ),
                (
                    "Insufficient data",
                    "program 1 0x0102 (stream_type 0x06): intermittent stream type".to_owned()
                ),
                ("Silence limit", "5000 ms".to_owned()),
                ("Time basis", "PCR-interpolated file position".to_owned()),
                ("Applies to", "the whole multiplex".to_owned()),
            ]
        );
        let unreferenced = row(&report, program, Check::UnreferencedPids).unwrap();
        assert_eq!(
            (unreferenced.status.as_str(), unreferenced.value.as_str()),
            ("unreferenced", "2")
        );
        assert_eq!(
            unreferenced.facts,
            [
                (
                    "Unreferenced PIDs",
                    "0x0300: 5 packets, 12750.0 ms".to_owned()
                ),
                ("Not judged", "0x0500: 2 packets, 350.0 ms".to_owned()),
                ("Grace period", "500 ms".to_owned()),
                ("Applies to", "the whole multiplex".to_owned()),
            ]
        );
        assert!(matches!(
            status_verdict("absent"),
            crate::theme::Verdict::Ng
        ));
        assert!(matches!(
            status_verdict("silent"),
            crate::theme::Verdict::Warn
        ));
        assert!(matches!(
            status_verdict("unreferenced"),
            crate::theme::Verdict::Warn
        ));

        let (all, unavailable) = findings(&report, program);
        // What the CLI could not judge is unavailable analysis, not a finding.
        assert_eq!(
            unavailable,
            [
                Unavailable {
                    check: Check::ReferencedPids,
                    status: "insufficient_data".to_owned(),
                    reason: "program 1 0x0102 (stream_type 0x06): intermittent stream type"
                        .to_owned()
                },
                Unavailable {
                    check: Check::UnreferencedPids,
                    status: "insufficient_data".to_owned(),
                    reason: "PID 0x0500: 2 packets without a reference for 350.0 ms".to_owned()
                },
            ]
        );
        let pid_findings: Vec<_> = all
            .iter()
            .filter(|f| {
                matches!(
                    f.check,
                    Some(Check::ReferencedPids | Check::UnreferencedPids)
                )
            })
            .map(|f| (f.status.as_str(), f.reason.as_str(), f.scope, f.offset))
            .collect();
        assert_eq!(
            pid_findings,
            [
                (
                    "silent",
                    "program 1 0x0101 (stream_type 0x03): 6100.0 ms without a packet",
                    Scope::Multiplex,
                    Some(0xAE0C)
                ),
                (
                    "absent",
                    "program 1 0x0103 (stream_type 0x04): no packet for 13175.0 ms",
                    Scope::Multiplex,
                    Some(0x1F94)
                ),
                (
                    "unreferenced",
                    "PID 0x0300: 5 packets without a reference for 12750.0 ms",
                    Scope::Multiplex,
                    Some(0x5C88)
                ),
            ]
        );

        // Without a time base: the reason, a finding without PIDs, and a
        // reference without silence; a skipped check is unavailable analysis.
        let mut edited: serde_json::Value = serde_json::from_str(document).unwrap();
        let references = &mut edited["pid_references"];
        references["unreferenced"] = serde_json::json!({"status": "insufficient_data", "reason": "no_pcr_time_base", "pids": []});
        references["referenced"]["pids"][0] = serde_json::json!({
            "program_number": 1, "role": "pmt", "pid": "0x1000", "status": "insufficient_data",
            "silence_judged": true, "packet_count": 0, "referenced_packet_count": 0,
            "referenced_from_offset_bytes": 8084, "referenced_until_offset_bytes": 120320
        });
        references["referenced"]["pids"][1]["status"] = "unknown_status".into();
        let report = self::report(&edited.to_string());
        let (all, unavailable) = findings(&report, &report.programs[0]);
        assert!(all.iter().all(|f| f.check != Some(Check::UnreferencedPids)
            && !f.reason.starts_with("program 1 0x1000")));
        assert!(
            all.iter()
                .any(|f| f.reason == "program 1 0x0100 (PCR): unknown status")
        );
        assert_eq!(
            unavailable,
            [
                Unavailable {
                    check: Check::ReferencedPids,
                    status: "insufficient_data".to_owned(),
                    reason: "program 1 0x1000 (PMT): insufficient data; program 1 0x0102 (stream_type 0x06): intermittent stream type".to_owned()
                },
                Unavailable {
                    check: Check::UnreferencedPids,
                    status: "insufficient_data".to_owned(),
                    reason: "no_pcr_time_base".to_owned()
                },
            ]
        );

        edited["pid_references"]["unreferenced"] = serde_json::json!({
            "status": "skipped", "reason": "program_layouts_unknown", "pids": []
        });
        let report = self::report(&edited.to_string());
        let (_, unavailable) = findings(&report, &report.programs[0]);
        assert!(unavailable.contains(&Unavailable {
            check: Check::UnreferencedPids,
            status: "skipped".to_owned(),
            reason: "program_layouts_unknown".to_owned()
        }));
        let reference = |role: &str, stream_type: Option<&str>| ReferencedPid {
            program_number: 2,
            role: role.to_owned(),
            pid: "0x0200".to_owned(),
            stream_type: stream_type.map(str::to_owned),
            status: "ok".to_owned(),
            reason: None,
            max_silence_ms: None,
            max_silence_start_offset_bytes: None,
            referenced_from_offset_bytes: 0,
        };
        assert_eq!(
            reference_text(&reference("elementary_stream", None)),
            "program 2 0x0200 (elementary_stream): ok"
        );
        assert_eq!(
            reference_text(&reference("elementary_stream", Some("0x1b"))),
            "program 2 0x0200 (stream_type 0x1b): ok"
        );
    }

    /// The sync-loss sample: a recovered result is a finding at the first
    /// loss; an unrecovered one carries the CLI's reason.
    #[test]
    fn sync_loss_row_and_finding() {
        let report = report(include_str!(
            "../../tests/fixtures/analysis_sync_loss_188.json"
        ));
        let program = &report.programs[0];
        let recovered = row(&report, program, Check::Synchronization).unwrap();
        assert_eq!(
            (recovered.status.as_str(), recovered.value.as_str()),
            ("recovered", "4")
        );
        assert_eq!(
            recovered.facts,
            [
                ("Losses", "4".to_owned()),
                ("Resyncs", "4".to_owned()),
                ("Skipped bytes", "1376".to_owned()),
                ("First loss", "0x00001D60".to_owned()),
                ("First resync", "0x00001E1C".to_owned()),
                ("Largest skip", "1000 bytes at 0x000058DC".to_owned()),
                (
                    "Partial packet at end",
                    "100 bytes at 0x000096C8".to_owned()
                ),
                (
                    "Realignment",
                    "5 sync bytes one packet apart, within 1048576 bytes".to_owned()
                ),
                ("Applies to", "the whole multiplex".to_owned()),
            ]
        );
        assert!(matches!(
            status_verdict("recovered"),
            crate::theme::Verdict::Warn
        ));
        // The skipped bytes also make delivery variable, so the PCR jitter
        // estimate is not assessable.
        let (all, unavailable) = findings(&report, program);
        assert_eq!(unavailable.len(), 1);
        assert_eq!(unavailable[0].check, Check::PcrJitter);
        let sync: Vec<_> = all
            .iter()
            .filter(|f| f.check == Some(Check::Synchronization))
            .collect();
        assert_eq!(sync.len(), 1);
        assert_eq!(
            (sync[0].status.as_str(), sync[0].scope, sync[0].offset),
            ("recovered", Scope::Multiplex, Some(7520))
        );
        assert_eq!(sync[0].reason, "4 losses, 1376 bytes skipped");

        let mut document: serde_json::Value = serde_json::from_str(include_str!(
            "../../tests/fixtures/analysis_sync_loss_188.json"
        ))
        .unwrap();
        document["status"] = "incomplete".into();
        document["reason"] = "sync_lost".into();
        document["sync"] = serde_json::json!({
            "status": "lost", "reason": "synchronization lost at 0x00009494 and not recovered",
            "required_sync_bytes": 5, "max_resync_scan_bytes": 1048576, "loss_count": 1,
            "resync_count": 0, "skipped_bytes": 0, "first_loss_offset_bytes": 38036,
            "unrecovered_offset_bytes": 38036, "trailing_partial_packet_bytes": 0
        });
        let report = self::report(&document.to_string());
        let row = row(&report, &report.programs[0], Check::Synchronization).unwrap();
        assert!(
            row.facts
                .contains(&("Analysis stopped at", "0x00009494".to_owned()))
        );
        assert!(matches!(status_verdict("lost"), crate::theme::Verdict::Ng));
        let (all, _) = findings(&report, &report.programs[0]);
        let lost = all
            .iter()
            .find(|f| f.check == Some(Check::Synchronization))
            .unwrap();
        assert_eq!(
            lost.reason,
            "1 loss, 0 bytes skipped; synchronization lost at 0x00009494 and not recovered"
        );
    }

    #[test]
    fn formats_jitter_durations() {
        assert_eq!(duration_ns(420.0), "420 ns");
        assert_eq!(duration_ns(100_000.0), "100.0 µs");
        assert_eq!(duration_ns(28_679_245.0), "28.68 ms");
    }
}
