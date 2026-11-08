//! The full-page explanation of one transport-health check, shown inside
//! Overview: the current result as the CLI reported it, the findings of the
//! check, a table of verdicts, then the explanation as headings and prose.

use eframe::egui;

use super::check_help::Check;
use super::overview::{
    self, CELL_PADDING, Finding, Focus, OverviewAction, OverviewState, accessible_scope,
    badge_width, status_badge, table_cell, text_width,
};
use crate::icons;
use crate::plot_data::Timeline;
use crate::presentation::CheckStatus;
use crate::report::{AnalysisReport, AvLeadProgram, MuxLeadPid, PacketGapPid, Program};

/// What the page shows its check for.
pub struct Context<'a> {
    pub report: &'a AnalysisReport,
    pub program: &'a Program,
    /// The Timeline series of the program, to tell which findings it shows.
    pub timeline: Option<&'a Timeline>,
}

/// Width from which the current result's labels get their wide column.
const WIDE: f32 = 480.0;

/// The detail page of `check`. `to_top` scrolls the page to its top, as when
/// it has just been opened.
pub fn show(
    ui: &mut egui::Ui,
    context: &Context<'_>,
    check: Check,
    state: &mut OverviewState,
    to_top: bool,
) -> Option<OverviewAction> {
    // Above the scrolling page, so it stays in reach.
    let back = ui.button((icons::text(icons::ARROW_LEFT), "Back to Overview"));
    back.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Button, true, "Back to Overview"));
    if state.take_focus(Focus::Back) {
        back.request_focus();
    }
    if back.clicked() {
        state.show_summary();
        ui.ctx().request_repaint();
        return None;
    }
    ui.add_space(4.0);
    let mut area = egui::ScrollArea::vertical()
        .id_salt("overview_check_detail")
        .auto_shrink([false, false]);
    if to_top {
        area = area.vertical_scroll_offset(0.0);
    }
    area.show(ui, |ui| page(ui, context, check)).inner
}

fn page(ui: &mut egui::Ui, context: &Context<'_>, check: Check) -> Option<OverviewAction> {
    let mut action = None;
    let help = check.help();
    let row = overview::row(context.report, context.program, check);

    ui.horizontal_wrapped(|ui| {
        let title = ui.label(crate::theme::page_title(help.title));
        title.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Heading, true, help.title));
        if let Some(row) = &row {
            let status = status_badge(ui, &row.status, Some(check));
            let name = format!("Status: {}", row.status.label());
            status.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Label, true, &name));
        }
    });
    ui.add(egui::Label::new(egui::RichText::new(help.summary).weak()).wrap());
    ui.add_space(10.0);

    section_heading(ui, "Current result");
    match &row {
        Some(row) => {
            facts(ui, &row.facts);
            if !row.status.is_known_for(check) {
                ui.add_space(4.0);
                let note = if row.status.is_finding_for(check) {
                    "It is shown as reported and listed as a finding."
                } else {
                    "It is shown as reported."
                };
                ui.add(
                    egui::Label::new(format!(
                        "The CLI reported the status \"{}\", which this GUI does not describe. {note}",
                        row.status
                    ))
                    .wrap(),
                );
            }
        }
        None => {
            ui.weak("The CLI did not report this check for the selected program.");
        }
    }
    ui.add_space(12.0);

    if check == Check::PacketGaps
        && let Some(pids) = overview::program_packet_gaps(context.report, context.program)
        && !pids.is_empty()
    {
        section_heading(ui, "Per PID");
        if let Some(chosen) = packet_gap_table(ui, &pids, context.program.program_number) {
            action = Some(chosen);
        }
        ui.add_space(12.0);
    }

    if check == Check::MuxLead {
        if let Some(pids) = overview::program_mux_leads(context.report, context.program)
            && !pids.is_empty()
        {
            section_heading(ui, "Per PID");
            if let Some(chosen) = mux_lead_table(ui, &pids) {
                action = Some(chosen);
            }
            ui.add_space(12.0);
        }
        if let Some(av) = overview::program_av_lead(context.report, context.program) {
            section_heading(ui, "A/V lead difference per audio stream");
            if let Some(chosen) = av_lead_table(ui, av) {
                action = Some(chosen);
            }
            ui.add_space(12.0);
        }
    }

    let (findings, _) = overview::findings(context.report, context.program);
    let findings: Vec<&Finding> = findings.iter().filter(|f| f.check == Some(check)).collect();
    if !findings.is_empty() {
        section_heading(ui, &format!("Findings ({})", findings.len()));
        for finding in findings {
            if let Some(chosen) = finding_row(ui, finding, context.timeline) {
                action = Some(chosen);
            }
        }
        ui.add_space(12.0);
    }

    section_heading(ui, "Verdicts");
    verdict_table(ui, check, row.as_ref().map(|r| &r.status));
    ui.add_space(12.0);

    for (heading, text) in [
        ("What it measures", help.measures),
        ("Applies to", help.applies_to),
        ("What the value means", help.value),
        ("When it is skipped", help.skipped),
        ("Limitations", help.limitations),
    ] {
        prose(ui, heading, text);
    }
    prose(ui, "CI exit status", &check.exit_status());
    if let Some((screen, text)) = help.navigation {
        prose(ui, "Details", text);
        let name = format!("Open {}", screen.title());
        let button = ui.button((icons::text(icons::ARROW_SQUARE_OUT), name.as_str()));
        button.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Button, true, &name));
        if button.clicked() {
            action = Some(OverviewAction::Open(screen));
        }
    }
    ui.add_space(12.0);
    action
}

/// A heading of the page.
fn section_heading(ui: &mut egui::Ui, text: &str) {
    let response = ui.label(crate::theme::subheading(text));
    response.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Heading, true, text));
    ui.add_space(2.0);
}

/// A heading followed by a wrapped paragraph: no grid, border or card.
fn prose(ui: &mut egui::Ui, heading: &str, text: &str) {
    section_heading(ui, heading);
    ui.add(egui::Label::new(text).wrap());
    ui.add_space(10.0);
}

/// The current result: a label column and a wrapping value column.
fn facts(ui: &mut egui::Ui, facts: &[(&'static str, String)]) {
    let width = ui.available_width();
    let key_width = if width >= WIDE { 160.0 } else { 116.0 };
    for (key, value) in facts {
        ui.horizontal_top(|ui| {
            ui.allocate_ui_with_layout(
                egui::vec2(key_width, 0.0),
                egui::Layout::top_down(egui::Align::Min),
                |ui| {
                    ui.set_width(key_width);
                    ui.add(egui::Label::new(egui::RichText::new(*key).weak()).wrap());
                },
            );
            let rest = (width - key_width - ui.spacing().item_spacing.x).max(0.0);
            ui.allocate_ui_with_layout(
                egui::vec2(rest, 0.0),
                egui::Layout::top_down(egui::Align::Min),
                |ui| {
                    ui.set_width(rest);
                    ui.add(egui::Label::new(value.as_str()).wrap());
                },
            );
        });
    }
}

/// A finding of this check with the navigation its data supports: the
/// packet when the finding has an offset, and the Timeline when the
/// program's Timeline shows an event at that offset.
fn finding_row(
    ui: &mut egui::Ui,
    finding: &Finding,
    timeline: Option<&Timeline>,
) -> Option<OverviewAction> {
    let mut action = None;
    ui.horizontal_wrapped(|ui| {
        status_badge(ui, &finding.status, finding.check);
        ui.weak(finding.scope.text());
        if let Some(offset) = finding.offset {
            ui.monospace(format!("0x{offset:08X}"));
        }
        ui.add(egui::Label::new(&finding.reason).wrap());
        if let Some(offset) = finding.offset {
            let name = format!("Open packet at 0x{offset:08X}");
            if icon_button(ui, icons::ARROW_SQUARE_OUT, "Open packet", &name).clicked() {
                action = Some(OverviewAction::OpenPacket(offset));
            }
            let on_timeline =
                timeline.is_some_and(|t| t.events.iter().any(|e| e.offset_bytes == offset));
            let name = format!("Show 0x{offset:08X} on timeline");
            if on_timeline && icon_button(ui, icons::TIMER, "Show on timeline", &name).clicked() {
                action = Some(OverviewAction::ShowOnTimeline(offset));
            }
        }
    });
    action
}

/// The largest gap of every PID of the program, with buttons that open the
/// packets before and after it.
fn packet_gap_table(
    ui: &mut egui::Ui,
    pids: &[&PacketGapPid],
    program: u16,
) -> Option<OverviewAction> {
    let mut action = None;
    egui::ScrollArea::horizontal()
        .id_salt("packet_gaps_scroll")
        .show(ui, |ui| {
            egui::Grid::new("packet_gaps")
                .striped(true)
                .num_columns(7)
                .spacing([16.0, 4.0])
                .show(ui, |ui| {
                    for header in ["PID", "Role", "Packets", "Gap", "Time", "From", "To"] {
                        ui.label(crate::theme::table_header_text(header, ui.visuals()));
                    }
                    ui.end_row();
                    for pid in pids {
                        ui.monospace(&pid.pid);
                        ui.label(overview::gap_roles(pid, program));
                        ui.monospace(pid.packet_count.to_string());
                        match pid.max_gap_packets {
                            Some(gap) => ui.monospace(gap.to_string()),
                            None => ui.weak(CheckStatus::parse(&pid.status).words()),
                        };
                        ui.monospace(overview::gap_ms(pid).unwrap_or_else(|| "—".to_owned()));
                        for offset in [pid.max_gap_start_offset_bytes, pid.max_gap_end_offset_bytes]
                        {
                            match offset {
                                Some(offset) => {
                                    let text = format!("0x{offset:08X}");
                                    let name = format!("Open packet at {text}");
                                    let button =
                                        ui.small_button(egui::RichText::new(&text).monospace());
                                    button.widget_info(|| {
                                        egui::WidgetInfo::labeled(egui::Role::Button, true, &name)
                                    });
                                    if button.on_hover_text(&name).clicked() {
                                        action = Some(OverviewAction::OpenPacket(offset));
                                    }
                                }
                                None => {
                                    ui.weak("—");
                                }
                            }
                        }
                        ui.end_row();
                    }
                });
        });
    action
}

/// A monospace button that opens the packet at `offset`, or a dash.
fn offset_button(ui: &mut egui::Ui, offset: Option<u64>) -> Option<OverviewAction> {
    let Some(offset) = offset else {
        ui.weak("—");
        return None;
    };
    let text = format!("0x{offset:08X}");
    let name = format!("Open packet at {text}");
    let button = ui.small_button(egui::RichText::new(&text).monospace());
    button.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Button, true, &name));
    button
        .on_hover_text(&name)
        .clicked()
        .then_some(OverviewAction::OpenPacket(offset))
}

/// The mux lead of every PID of the program, with buttons that open the PES
/// packets with the smallest and largest lead.
fn mux_lead_table(ui: &mut egui::Ui, pids: &[&MuxLeadPid]) -> Option<OverviewAction> {
    let mut action = None;
    egui::ScrollArea::horizontal()
        .id_salt("mux_lead_scroll")
        .show(ui, |ui| {
            egui::Grid::new("mux_lead")
                .striped(true)
                .num_columns(9)
                .spacing([16.0, 4.0])
                .show(ui, |ui| {
                    for header in [
                        "PID",
                        "Stream",
                        "PES",
                        "Min",
                        "At",
                        "Max",
                        "At",
                        "Spread",
                        "Unbracketed",
                    ] {
                        ui.label(crate::theme::table_header_text(header, ui.visuals()));
                    }
                    ui.end_row();
                    for pid in pids {
                        ui.monospace(&pid.stream.pid);
                        ui.label(&pid.stream.label);
                        match pid.sample_count {
                            0 => ui.weak(
                                pid.reason
                                    .as_deref()
                                    .unwrap_or(&pid.status)
                                    .replace('_', " "),
                            ),
                            count if pid.negative_count > 0 => {
                                ui.monospace(format!("{count} ({} negative)", pid.negative_count))
                            }
                            count => ui.monospace(count.to_string()),
                        };
                        ui.monospace(overview::lead_ms(pid.min_lead_ms));
                        action = offset_button(ui, pid.min_lead_offset_bytes).or(action);
                        ui.monospace(overview::lead_ms(pid.max_lead_ms));
                        action = offset_button(ui, pid.max_lead_offset_bytes).or(action);
                        ui.monospace(overview::lead_ms(pid.spread_ms));
                        ui.monospace(pid.unbracketed_count.to_string());
                        ui.end_row();
                    }
                });
        });
    action
}

/// The A/V lead difference of every audio stream of the program, with
/// buttons that open the two PES packets of the largest difference.
fn av_lead_table(ui: &mut egui::Ui, av: &AvLeadProgram) -> Option<OverviewAction> {
    let mut action = None;
    egui::ScrollArea::horizontal()
        .id_salt("av_lead_scroll")
        .show(ui, |ui| {
            egui::Grid::new("av_lead")
                .striped(true)
                .num_columns(8)
                .spacing([16.0, 4.0])
                .show(ui, |ui| {
                    for header in [
                        "Audio PID",
                        "Difference",
                        "Audio lead",
                        "At",
                        "Video PID",
                        "Video lead",
                        "At",
                        "Comparisons",
                    ] {
                        ui.label(crate::theme::table_header_text(header, ui.visuals()));
                    }
                    ui.end_row();
                    for audio in &av.audio {
                        let d = &audio.difference;
                        ui.monospace(&audio.pid);
                        match d.max_av_lead_difference_ms {
                            Some(ms) => ui.monospace(format!("{ms:.1} ms")),
                            None => ui.weak(
                                audio
                                    .reason
                                    .as_deref()
                                    .unwrap_or(&audio.status)
                                    .replace('_', " "),
                            ),
                        };
                        ui.monospace(overview::lead_ms(d.audio_lead_ms));
                        action = offset_button(ui, d.audio_offset_bytes).or(action);
                        ui.monospace(d.video_pid.as_deref().unwrap_or("—"));
                        ui.monospace(overview::lead_ms(d.video_lead_ms));
                        action = offset_button(ui, d.video_offset_bytes).or(action);
                        ui.monospace(audio.comparison_count.to_string());
                        ui.end_row();
                    }
                });
        });
    action
}

/// A small button with an icon and `text`, named `name` for assistive
/// technology.
fn icon_button(ui: &mut egui::Ui, glyph: &str, text: &str, name: &str) -> egui::Response {
    let button = ui.small_button((icons::text(glyph), text));
    button.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Button, true, name));
    button
}

/// Every status of the check with its condition. The current status is
/// highlighted and marked selected for assistive technology; its meaning is
/// the same as in any other row.
fn verdict_table(ui: &mut egui::Ui, check: Check, current: Option<&CheckStatus>) {
    let statuses: Vec<(CheckStatus, &str)> = check
        .help()
        .statuses
        .iter()
        .map(|(status, meaning)| (CheckStatus::parse(status), *meaning))
        .collect();
    let width = ui.available_width();
    let stroke = crate::theme::table_border(ui.visuals());
    let divider = crate::theme::table_divider(ui.visuals());
    let frame = egui::Frame::new()
        .stroke(stroke)
        .corner_radius(4)
        .inner_margin(egui::Margin::same(stroke.width.ceil() as i8));
    let inner = (width - frame.total_margin().sum().x).max(0.0);
    let status_width = statuses
        .iter()
        .map(|(status, _)| badge_width(ui, status))
        .fold(
            text_width(ui, crate::theme::table_header_text("Status", ui.visuals())),
            f32::max,
        )
        + 2.0 * CELL_PADDING;
    let condition_width = (inner - status_width).max(0.0);
    let highlight = ui.visuals().selection.bg_fill.gamma_multiply(0.45);
    let accent = ui.visuals().selection.stroke.color;
    let builder = egui::UiBuilder::new()
        .accessibility_role(egui::Role::Table)
        .accessibility_label("Verdicts");
    frame.show(ui, |ui| {
        ui.set_width(inner);
        accessible_scope(ui, builder, |ui| {
            ui.spacing_mut().item_spacing.y = 0.0;
            verdict_row(
                ui,
                [status_width, condition_width],
                None,
                |ui, column| {
                    ui.label(crate::theme::table_header_text(
                        ["Status", "Condition"][column],
                        ui.visuals(),
                    ));
                },
                Some(crate::theme::table_header_fill(ui.visuals())),
                None,
            );
            overview::rule(ui, inner, crate::theme::table_header_rule(ui.visuals()));
            for (index, (status, meaning)) in statuses.iter().enumerate() {
                if index > 0 {
                    let rect = ui.allocate_exact_size(egui::vec2(inner, 1.0), egui::Sense::hover());
                    ui.painter()
                        .hline(rect.0.x_range(), rect.0.center().y, divider);
                }
                let selected = current == Some(status);
                let label = format!("{}: {meaning}", status.label());
                verdict_row(
                    ui,
                    [status_width, condition_width],
                    Some((&label, selected)),
                    |ui, column| {
                        if column == 0 {
                            // The check's own statuses: always described.
                            super::badge(ui, &status.label(), status.severity_for(Some(check)));
                        } else {
                            ui.add(egui::Label::new(*meaning).wrap());
                        }
                    },
                    selected.then_some(highlight),
                    selected.then_some(accent),
                );
            }
        });
    });
}

/// One row of the verdict table: two top-aligned cells. The header row has
/// no `row`; any other row has its name and whether it is the current status.
fn verdict_row(
    ui: &mut egui::Ui,
    widths: [f32; 2],
    row: Option<(&str, bool)>,
    mut add: impl FnMut(&mut egui::Ui, usize),
    fill: Option<egui::Color32>,
    accent: Option<egui::Color32>,
) {
    let background = ui.painter().add(egui::Shape::Noop);
    let mut builder = egui::UiBuilder::new()
        .layout(egui::Layout::left_to_right(egui::Align::Min))
        .accessibility_role(egui::Role::Row);
    if let Some((label, _)) = row {
        builder = builder.accessibility_label(label);
    }
    let role = if row.is_some() {
        egui::Role::Cell
    } else {
        egui::Role::ColumnHeader
    };
    let response = accessible_scope(ui, builder, |ui| {
        if let Some((_, selected)) = row {
            let id = ui.unique_id();
            ui.ctx()
                .accesskit_node_builder(id, |node| node.set_selected(selected));
        }
        ui.spacing_mut().item_spacing = egui::Vec2::ZERO;
        for (column, width) in widths.into_iter().enumerate() {
            table_cell(
                ui,
                role,
                egui::vec2(width, 0.0),
                egui::Layout::top_down(egui::Align::Min),
                |ui| {
                    egui::Frame::new()
                        .inner_margin(egui::Margin::symmetric(CELL_PADDING as i8, 5))
                        .show(ui, |ui| {
                            ui.set_width(width - 2.0 * CELL_PADDING);
                            add(ui, column);
                        });
                },
            );
        }
    });
    let rect = response.response.rect;
    let mut shapes = Vec::new();
    if let Some(fill) = fill {
        shapes.push(egui::Shape::rect_filled(rect, 0, fill));
    }
    if let Some(accent) = accent {
        shapes.push(egui::Shape::rect_filled(
            egui::Rect::from_min_size(rect.min, egui::vec2(3.0, rect.height())),
            0,
            accent,
        ));
    }
    ui.painter().set(background, egui::Shape::Vec(shapes));
}
