use std::collections::BTreeSet;

use eframe::egui;
use egui_plot::{HLine, Legend, Line, LineStyle, MarkerShape, Plot, PlotPoint, Points, VLine};

use super::{Screen, badge, constrain_time_axis, program_selector_labels};
use crate::icons;
use crate::plot_data::{Marker, Point, Timeline, nearest};
use crate::range_bar;
use crate::report::{AnalysisReport, Program as ReportProgram};
use crate::theme::{self, Verdict};

const LINK_GROUP: &str = "timeline";
/// Series name of the PAT/PMT change markers.
const MARKER_SERIES: &str = "PAT/PMT change";
const SIDE_WIDTH: f32 = 260.0;
/// How close (in screen pixels) a click must be to a point to select it.
const PICK_DISTANCE: f32 = 12.0;

pub struct TimelineView {
    /// Gap series unchecked in the side panel, by PID.
    hidden_series: BTreeSet<String>,
    selection: Option<Selection>,
    /// The packet to show after "Open packet", until the app takes it.
    open_request: Option<u64>,
    /// The analysis generation and program the report state belongs to.
    report_context: Option<(u64, usize)>,
    show_pcr: bool,
    show_events: bool,
    show_markers: bool,
    reset_zoom: bool,
}

impl Default for TimelineView {
    fn default() -> Self {
        Self {
            hidden_series: BTreeSet::new(),
            selection: None,
            open_request: None,
            report_context: None,
            show_pcr: true,
            show_events: true,
            show_markers: true,
            reset_zoom: false,
        }
    }
}

/// A point picked on a real-data plot.
#[derive(Clone)]
struct Selection {
    series: String,
    pid: String,
    point: Point,
    /// The value with its unit, if the point has one (events do not).
    value: Option<String>,
    verdict: Option<Verdict>,
}

/// Plot colors for elementary-stream series, cycled by index.
const SERIES_COLORS: [egui::Color32; 4] = [
    theme::VIDEO_COLOR,
    theme::AUDIO_COLOR,
    egui::Color32::from_rgb(0x6c, 0xd4, 0xd4),
    egui::Color32::from_rgb(0xe8, 0x8a, 0xc8),
];

pub fn series_color(index: usize) -> egui::Color32 {
    SERIES_COLORS[index % SERIES_COLORS.len()]
}

/// Formats elapsed program time as h:mm:ss.mmm.
pub fn clock(seconds: f64) -> String {
    let millis = (seconds * 1000.0).round() as u64;
    format!(
        "{}:{:02}:{:02}.{:03}",
        millis / 3_600_000,
        millis / 60_000 % 60,
        millis / 1000 % 60,
        millis % 1000
    )
}

impl TimelineView {
    /// The Timeline of one program of an analysis. Returns the screen to switch
    /// to when the selected point's packet is opened.
    pub fn show_report(
        &mut self,
        ui: &mut egui::Ui,
        report: &AnalysisReport,
        timelines: &[Timeline],
        generation: u64,
        program: &mut usize,
    ) -> Option<Screen> {
        let labels: Vec<String> = report.programs.iter().map(ReportProgram::label).collect();
        // Another screen may have changed the program, or a new analysis may
        // have replaced the report.
        self.sync_report_context(generation, *program);
        let mut navigate = None;
        let height = ui.available_height();
        ui.horizontal_top(|ui| {
            let plots_width =
                (ui.available_width() - SIDE_WIDTH - 3.0 * ui.spacing().item_spacing.x).max(240.0);
            ui.allocate_ui_with_layout(
                egui::vec2(plots_width, height),
                egui::Layout::top_down(egui::Align::Min),
                |ui| {
                    ui.set_width(plots_width);
                    ui.horizontal(|ui| {
                        if program_selector_labels(ui, &labels, program) {
                            self.sync_report_context(generation, *program);
                        }
                        ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                            if ui
                                .button((icons::text(icons::ARROW_COUNTER_CLOCKWISE), "Reset zoom"))
                                .clicked()
                            {
                                self.reset_zoom = true;
                            }
                        });
                    });
                    ui.separator();
                    let (Some(selected), Some(data)) =
                        (report.programs.get(*program), timelines.get(*program))
                    else {
                        ui.weak("No program was found in this range.");
                        return;
                    };
                    egui::ScrollArea::vertical()
                        .id_salt("timeline_plots")
                        .show(ui, |ui| {
                            if self.report_plots(ui, selected, data) {
                                navigate = Some(Screen::Packets);
                            }
                        });
                },
            );
            ui.separator();
            ui.allocate_ui_with_layout(
                egui::vec2(SIDE_WIDTH, height),
                egui::Layout::top_down(egui::Align::Min),
                |ui| {
                    ui.set_width(SIDE_WIDTH);
                    if let (Some(selected), Some(data)) =
                        (report.programs.get(*program), timelines.get(*program))
                        && self.report_side_panel(ui, selected, data)
                    {
                        navigate = Some(Screen::Packets);
                    }
                },
            );
        });
        if navigate.is_some() {
            self.open_request = self.selection.as_ref().map(|s| s.point.offset_bytes);
        }
        navigate
    }

    /// Selects the event at `offset` of the given program's Timeline, as if
    /// it had been clicked. Returns false when the Timeline has no event
    /// there.
    pub fn show_event(
        &mut self,
        generation: u64,
        program: usize,
        data: &Timeline,
        offset: u64,
    ) -> bool {
        self.sync_report_context(generation, program);
        let Some(event) = data.events.iter().find(|e| e.offset_bytes == offset) else {
            return false;
        };
        self.show_events = true;
        self.selection = Some(Selection {
            series: "Event".to_owned(),
            pid: event.pid.clone(),
            point: Point {
                seconds: event.seconds,
                value: 0.0,
                offset_bytes: event.offset_bytes,
            },
            value: Some(event.label.clone()),
            verdict: None,
        });
        true
    }

    /// The selected point's series and offset.
    #[cfg(test)]
    pub fn selected_for_test(&self) -> Option<(&str, u64)> {
        self.selection
            .as_ref()
            .map(|s| (s.series.as_str(), s.point.offset_bytes))
    }

    /// The packet "Open packet" asked for, once.
    pub fn take_open_request(&mut self) -> Option<u64> {
        self.open_request.take()
    }

    /// Clears what belongs to another program or analysis.
    fn sync_report_context(&mut self, generation: u64, program: usize) {
        let context = Some((generation, program));
        if self.report_context != context {
            self.report_context = context;
            self.selection = None;
            self.hidden_series.clear();
            self.reset_zoom = true;
        }
    }

    /// Returns true when "Open packet" is chosen from a plot's context menu.
    fn report_plots(
        &mut self,
        ui: &mut egui::Ui,
        program: &ReportProgram,
        data: &Timeline,
    ) -> bool {
        let linked = egui::Vec2b::new(true, false);
        let reset_zoom = std::mem::take(&mut self.reset_zoom);
        let pcr_label = program.pcr_pid.clone().unwrap_or_else(|| "none".to_owned());
        let limits = program.compliance.as_ref().map_or((100.0, 1000.0), |c| {
            (c.max_pcr_interval.limit_ms, c.max_pcr_pts_dts_gap.limit_ms)
        });
        let mut clicked: Option<Selection> = None;
        let mut any_click = false;

        ui.horizontal(|ui| {
            ui.strong("PCR interval");
            ui.weak(format!(
                "ms · PCR PID {pcr_label} · limit {:.0} ms",
                limits.0
            ));
        });
        let selection = self.selection.clone();
        let show_pcr = self.show_pcr;
        let markers: &[Marker] = if self.show_markers {
            &data.markers
        } else {
            &[]
        };
        let pcr_plot = timeline_plot(
            "pcr_interval",
            220.0,
            linked,
            limits.0 * 1.2,
            data.time_range,
            reset_zoom,
        )
        .show(ui, |plot| {
            // This first linked plot alone owns the X limit. Having every
            // linked plot write it back makes their one-frame updates fight.
            constrain_time_axis(plot, data.time_range);
            plot.hline(
                HLine::new("limit", limits.0)
                    .color(theme::LIMIT_COLOR)
                    .style(LineStyle::Dashed { length: 8.0 }),
            );
            psi_markers(plot, markers);
            if show_pcr {
                plot.line(
                    Line::new(format!("PCR {pcr_label}"), data.pcr_drawn.as_slice())
                        .color(theme::PCR_COLOR),
                );
                if !data.pcr_over_limit.is_empty() {
                    plot.points(
                        Points::new("over limit", data.pcr_over_limit.as_slice())
                            .color(theme::LIMIT_COLOR)
                            .radius(5.0)
                            .filled(true),
                    );
                }
            }
            highlight(plot, selection.as_ref(), "PCR interval");
        });
        if let Some(pick) = pick(
            &pcr_plot,
            show_pcr
                .then_some(data.pcr_interval.iter().copied())
                .into_iter()
                .flatten(),
        ) {
            clicked = Some(Selection {
                series: "PCR interval".to_owned(),
                pid: pcr_label.clone(),
                point: pick,
                value: Some(format!("{:.3} ms", pick.value)),
                verdict: Some(verdict_against(pick.value, limits.0)),
            });
        }
        any_click |= pcr_plot.response.clicked() || pcr_plot.response.secondary_clicked();
        let mut open_packet = self.context_menu(&pcr_plot.response);

        ui.add_space(8.0);
        ui.horizontal(|ui| {
            ui.strong("PCR → PTS/DTS gap");
            ui.weak(format!(
                "ms per PID · limit {:.0} ms · DTS used when present",
                limits.1
            ));
        });
        let hidden = &self.hidden_series;
        let gap_plot = timeline_plot(
            "pts_gap",
            200.0,
            linked,
            limits.1 * 1.2,
            data.time_range,
            false,
        )
        .show(ui, |plot| {
            plot.hline(
                HLine::new("limit", limits.1)
                    .color(theme::LIMIT_COLOR)
                    .style(LineStyle::Dashed { length: 8.0 }),
            );
            psi_markers(plot, markers);
            for (index, gap) in data.gaps.iter().enumerate() {
                if hidden.contains(&gap.pid) {
                    continue;
                }
                // Stretches share the name, so the legend shows the PID once.
                for stretch in &gap.drawn {
                    plot.line(
                        Line::new(gap.label.clone(), stretch.as_slice()).color(series_color(index)),
                    );
                }
            }
            highlight(plot, selection.as_ref(), "PCR → PTS/DTS gap");
        });
        let gap_clicked = gap_plot.response.clicked() || gap_plot.response.secondary_clicked();
        // Only a click is worth a scan over every point.
        if gap_clicked
            && let Some((pid, pick)) = data
                .gaps
                .iter()
                .filter(|g| !hidden.contains(&g.pid))
                .flat_map(|g| g.points.iter().map(move |p| (g.pid.as_str(), p)))
                .map(|(pid, p)| (pid, p, distance(&gap_plot, p)))
                .filter(|(_, _, d)| *d <= PICK_DISTANCE)
                .min_by(|a, b| a.2.total_cmp(&b.2))
                .map(|(pid, p, _)| (pid.to_owned(), *p))
        {
            clicked = Some(Selection {
                series: "PCR → PTS/DTS gap".to_owned(),
                pid,
                point: pick,
                value: Some(format!("{:.3} ms", pick.value)),
                verdict: Some(verdict_against(pick.value, limits.1)),
            });
        }
        any_click |= gap_clicked;
        open_packet |= self.context_menu(&gap_plot.response);

        if self.show_events {
            ui.add_space(8.0);
            ui.horizontal(|ui| {
                ui.strong("Events");
                ui.weak(format!("{} in this program", data.events.len()));
            });
            let events_plot = Plot::new("events")
                .height(60.0)
                .default_x_bounds(data.time_range.0, data.time_range.1)
                .link_axis(egui::Id::unique(LINK_GROUP), linked)
                .link_cursor(egui::Id::unique(LINK_GROUP), linked)
                .show_axes([true, false])
                .show_grid([true, false])
                .include_y(-1.0)
                .include_y(1.0)
                .allow_zoom(false)
                .allow_drag(false);
            let events_plot = events_plot.show(ui, |plot| {
                psi_markers(plot, markers);
                plot.points(
                    Points::new("event", data.event_drawn.as_slice())
                        .shape(MarkerShape::Diamond)
                        .color(crate::theme::EVENT_COLOR)
                        .radius(6.0)
                        .filled(true),
                );
                highlight(plot, selection.as_ref(), "Event");
            });
            let event_points = data.events.iter().map(|e| Point {
                seconds: e.seconds,
                value: 0.0,
                offset_bytes: e.offset_bytes,
            });
            if let Some(pick) = pick(&events_plot, event_points)
                && let Some(event) = data
                    .events
                    .iter()
                    .find(|e| e.offset_bytes == pick.offset_bytes)
            {
                clicked = Some(Selection {
                    series: "Event".to_owned(),
                    pid: event.pid.clone(),
                    point: pick,
                    value: Some(event.label.clone()),
                    verdict: None,
                });
            }
            any_click |= events_plot.response.clicked() || events_plot.response.secondary_clicked();
            open_packet |= self.context_menu(&events_plot.response);
        }
        if any_click {
            self.selection = clicked;
        }
        ui.weak("Click a point to select · Right-click for actions · Scroll: zoom · Drag: pan");
        open_packet
    }

    /// Context menu of a plot; returns true when "Open packet" is chosen.
    fn context_menu(&mut self, response: &egui::Response) -> bool {
        let mut open_packet = false;
        let selected = self.selection.is_some();
        response.context_menu(|ui| {
            if ui
                .add_enabled(
                    selected,
                    egui::Button::new((icons::text(icons::ARROW_SQUARE_OUT), "Open packet")),
                )
                .clicked()
            {
                open_packet = true;
                ui.close();
            }
            if ui
                .button((icons::text(icons::ARROW_COUNTER_CLOCKWISE), "Reset zoom"))
                .clicked()
            {
                self.reset_zoom = true;
                ui.close();
            }
        });
        open_packet
    }

    /// Returns true when "Open packet" is clicked.
    fn report_side_panel(
        &mut self,
        ui: &mut egui::Ui,
        program: &ReportProgram,
        data: &Timeline,
    ) -> bool {
        ui.add_space(4.0);
        ui.strong("Series");
        ui.checkbox(
            &mut self.show_pcr,
            format!(
                "PCR interval ({})",
                program.pcr_pid.as_deref().unwrap_or("none")
            ),
        );
        for gap in &data.gaps {
            let mut shown = !self.hidden_series.contains(&gap.pid);
            let span = match (gap.points.first(), gap.points.last()) {
                (Some(first), Some(last)) => {
                    format!("  {:.1}–{:.1} s", first.seconds, last.seconds)
                }
                _ => String::new(),
            };
            if ui
                .checkbox(&mut shown, format!("{} gap{span}", gap.label))
                .changed()
            {
                if shown {
                    self.hidden_series.remove(&gap.pid);
                } else {
                    self.hidden_series.insert(gap.pid.clone());
                }
            }
        }
        ui.checkbox(&mut self.show_events, "Event markers");
        ui.checkbox(
            &mut self.show_markers,
            format!("PAT/PMT changes ({})", data.markers.len()),
        );
        if !data.markers.is_empty() {
            ui.add_space(8.0);
            ui.strong("PAT/PMT changes");
            for marker in &data.markers {
                let selected = self.selection.as_ref().is_some_and(|s| {
                    s.series == MARKER_SERIES && s.point.offset_bytes == marker.offset_bytes
                });
                let text = format!("{}  {}", clock(marker.seconds), marker.label);
                if ui.selectable_label(selected, text).clicked() {
                    self.selection = Some(Selection {
                        series: MARKER_SERIES.to_owned(),
                        pid: marker.pid.clone(),
                        point: Point {
                            seconds: marker.seconds,
                            value: 0.0,
                            offset_bytes: marker.offset_bytes,
                        },
                        value: Some(marker.label.clone()),
                        verdict: None,
                    });
                }
            }
        }

        ui.add_space(12.0);
        ui.strong("Selected point");
        let mut open_packet = false;
        egui::Frame::group(ui.style()).show(ui, |ui| {
            ui.set_width(ui.available_width());
            let Some(selection) = &self.selection else {
                ui.weak("Click a point on a plot to select it.");
                return;
            };
            egui::Grid::new("timeline_selection")
                .num_columns(2)
                .spacing([12.0, 4.0])
                .show(ui, |ui| {
                    ui.weak("Series");
                    ui.label(&selection.series);
                    ui.end_row();
                    ui.weak("Time");
                    ui.monospace(clock(selection.point.seconds));
                    ui.end_row();
                    ui.weak("Offset");
                    ui.monospace(range_bar::bytes(selection.point.offset_bytes));
                    ui.end_row();
                    ui.weak("PID");
                    ui.monospace(&selection.pid);
                    ui.end_row();
                    if let Some(value) = &selection.value {
                        ui.weak("Value");
                        ui.label(value);
                        ui.end_row();
                    }
                    if let Some(verdict) = selection.verdict {
                        ui.weak("Verdict");
                        badge(ui, verdict.label(), verdict);
                        ui.end_row();
                    }
                });
            ui.add_space(6.0);
            let open = egui::Button::new((icons::text(icons::ARROW_SQUARE_OUT), "Open packet"));
            if ui.add_sized([ui.available_width(), 30.0], open).clicked() {
                open_packet = true;
            }
        });
        open_packet
    }
}

/// Applied PAT and PMT changes as vertical dashed lines.
fn psi_markers(plot: &mut egui_plot::PlotUi, markers: &[Marker]) {
    for marker in markers {
        plot.vline(
            VLine::new(MARKER_SERIES, marker.seconds)
                .color(theme::PSI_COLOR)
                .style(LineStyle::Dashed { length: 6.0 }),
        );
    }
}

/// A time plot linked to the other Timeline plots: zoom and pan act on time only.
fn timeline_plot<'a>(
    id: &str,
    height: f32,
    linked: egui::Vec2b,
    top: f64,
    time_range: (f64, f64),
    reset: bool,
) -> Plot<'a> {
    let plot = Plot::new(id.to_owned())
        .height(height)
        .default_x_bounds(time_range.0, time_range.1)
        .legend(Legend::default())
        .link_axis(egui::Id::unique(LINK_GROUP), linked)
        .link_cursor(egui::Id::unique(LINK_GROUP), linked)
        .include_y(0.0)
        .include_y(top)
        .allow_zoom([true, false])
        .allow_drag([true, false])
        .allow_scroll([true, false])
        .allow_axis_zoom_drag([true, false])
        .allow_boxed_zoom(false);
    if reset { plot.reset() } else { plot }
}

/// Rings the selected point when it belongs to this plot.
fn highlight(plot: &mut egui_plot::PlotUi, selection: Option<&Selection>, series: &str) {
    if let Some(selection) = selection.filter(|s| s.series == series) {
        let y = if series == "Event" {
            0.0
        } else {
            selection.point.value
        };
        plot.points(
            Points::new("selected", vec![[selection.point.seconds, y]])
                .color(egui::Color32::WHITE)
                .radius(9.0)
                .filled(false),
        );
    }
}

fn distance<R>(plot: &egui_plot::PlotResponse<R>, point: &Point) -> f32 {
    let Some(pointer) = plot.response.interact_pointer_pos() else {
        return f32::INFINITY;
    };
    plot.transform
        .position_from_point(&PlotPoint::new(point.seconds, point.value))
        .distance(pointer)
}

/// The point clicked on a plot, if a click landed close enough to one.
fn pick<R>(
    plot: &egui_plot::PlotResponse<R>,
    points: impl Iterator<Item = Point>,
) -> Option<Point> {
    if !(plot.response.clicked() || plot.response.secondary_clicked()) {
        return None;
    }
    nearest(points, |p| distance(plot, p))
        .filter(|(_, d)| *d <= PICK_DISTANCE)
        .map(|(p, _)| p)
}

fn verdict_against(value: f64, limit: f64) -> Verdict {
    if value > limit {
        Verdict::Ng
    } else {
        Verdict::Ok
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn selected_view() -> TimelineView {
        let mut view = TimelineView::default();
        view.sync_report_context(1, 0);
        view.selection = Some(Selection {
            series: "PCR interval".to_owned(),
            pid: "0x0100".to_owned(),
            point: Point {
                seconds: 1.0,
                value: 40.0,
                offset_bytes: 188,
            },
            value: None,
            verdict: None,
        });
        view.hidden_series.insert("0x0101".to_owned());
        view.reset_zoom = false;
        view
    }

    #[test]
    fn keeps_the_selection_in_the_same_context() {
        let mut view = selected_view();
        view.sync_report_context(1, 0);
        assert!(view.selection.is_some() && !view.reset_zoom);
    }

    /// Another screen switched the program.
    #[test]
    fn clears_the_selection_when_the_program_changes() {
        let mut view = selected_view();
        view.sync_report_context(1, 1);
        assert!(view.selection.is_none() && view.hidden_series.is_empty() && view.reset_zoom);
    }

    /// A new analysis replaced the report; the program index is the same.
    #[test]
    fn clears_the_selection_for_a_new_analysis() {
        let mut view = selected_view();
        view.sync_report_context(2, 0);
        assert!(view.selection.is_none() && view.hidden_series.is_empty());
    }
}
