use std::collections::BTreeSet;

use eframe::egui;
use egui_plot::{HLine, Legend, Line, LineStyle, MarkerShape, Plot, PlotPoint, Points, VLine};

use super::{Screen, badge, program_selector, program_selector_labels};
use crate::icons;
use crate::mock::{self, Verdict};
use crate::mock_programs::{Program, programs};
use crate::plot_data::{Point, Timeline, nearest};
use crate::range_bar;
use crate::report::{AnalysisReport, Program as ReportProgram};

const LINK_GROUP: &str = "timeline";
const SIDE_WIDTH: f32 = 260.0;
/// How close (in screen pixels) a click must be to a point to select it.
const PICK_DISTANCE: f32 = 12.0;

pub struct TimelineView {
    /// PIDs unchecked in the side panel (sample data).
    hidden_pids: BTreeSet<u16>,
    /// Gap series unchecked in the side panel, by PID.
    hidden_series: BTreeSet<String>,
    selection: Option<Selection>,
    /// The packet to show after "Open packet", until the app takes it.
    open_request: Option<u64>,
    /// The analysis generation and program the report state belongs to.
    report_context: Option<(u64, usize)>,
    show_pcr: bool,
    show_events: bool,
    point_selected: bool,
    reset_zoom: bool,
}

impl Default for TimelineView {
    fn default() -> Self {
        Self {
            hidden_pids: BTreeSet::new(),
            hidden_series: BTreeSet::new(),
            selection: None,
            open_request: None,
            report_context: None,
            show_pcr: true,
            show_events: true,
            point_selected: true,
            reset_zoom: false,
        }
    }
}

impl TimelineView {
    /// Returns the screen to switch to when the selected point's packet is opened.
    pub fn show(&mut self, ui: &mut egui::Ui, program: &mut usize) -> Option<Screen> {
        let programs = programs();
        let mut navigate = None;
        let height = ui.available_height();
        // Explicit column widths so the plots can never extend under the side column.
        ui.horizontal_top(|ui| {
            let plots_width =
                (ui.available_width() - SIDE_WIDTH - 3.0 * ui.spacing().item_spacing.x).max(240.0);
            ui.allocate_ui_with_layout(
                egui::vec2(plots_width, height),
                egui::Layout::top_down(egui::Align::Min),
                |ui| {
                    ui.set_width(plots_width);
                    self.toolbar(ui, program);
                    ui.separator();
                    egui::ScrollArea::vertical()
                        .id_salt("timeline_plots")
                        .show(ui, |ui| {
                            if self.plots(ui, &programs[*program]) {
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
                    if self.side_panel(ui, &programs[*program]) {
                        navigate = Some(Screen::Packets);
                    }
                },
            );
        });
        navigate
    }

    /// Controls that act on the plots as a whole: which program, and the zoom.
    fn toolbar(&mut self, ui: &mut egui::Ui, program: &mut usize) {
        ui.horizontal(|ui| {
            if program_selector(ui, program) {
                self.point_selected = false;
                self.reset_zoom = true;
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
    }

    /// Returns true when "Open packet" is chosen from the plot's context menu.
    fn plots(&mut self, ui: &mut egui::Ui, program: &Program) -> bool {
        let linked = egui::Vec2b::new(true, false);
        let reset_zoom = std::mem::take(&mut self.reset_zoom);
        let mut open_packet = false;
        // Only program 1's PCR has the NG spike in the mock data.
        let has_spike = program.number == 1;
        let pmt_updates = |plot: &mut egui_plot::PlotUi| {
            for (seconds, label) in &program.pmt_updates {
                plot.vline(
                    VLine::new(*label, *seconds)
                        .color(mock::PSI_COLOR)
                        .style(LineStyle::Dashed { length: 6.0 }),
                );
            }
        };

        ui.horizontal(|ui| {
            ui.strong("PCR interval");
            ui.weak(format!(
                "ms · PCR PID 0x{:04X} · limit 100 ms",
                program.pcr_pid
            ));
        });
        let show_pcr = self.show_pcr;
        let point_selected = self.point_selected && has_spike;
        let pcr_plot = Plot::new("pcr_interval")
            .height(220.0)
            .legend(Legend::default())
            .link_axis(egui::Id::unique(LINK_GROUP), linked)
            .link_cursor(egui::Id::unique(LINK_GROUP), linked)
            .include_y(0.0)
            .include_y(120.0)
            .allow_zoom([true, false])
            .allow_drag([true, false])
            .allow_scroll([true, false])
            .allow_axis_zoom_drag([true, false])
            .allow_boxed_zoom(false)
            .show(ui, |plot| {
                if reset_zoom {
                    plot.set_auto_bounds(true);
                }
                plot.hline(
                    HLine::new("limit", 100.0)
                        .color(mock::LIMIT_COLOR)
                        .style(LineStyle::Dashed { length: 8.0 }),
                );
                pmt_updates(plot);
                if show_pcr {
                    let series = if has_spike {
                        mock::pcr_interval_ms()
                    } else {
                        mock::pcr_interval_ms()
                            .into_iter()
                            .map(|[t, ms]| [t, if ms > 100.0 { 41.0 } else { ms - 6.0 }])
                            .collect()
                    };
                    plot.line(
                        Line::new(format!("PCR 0x{:04X}", program.pcr_pid), series)
                            .color(mock::PCR_COLOR),
                    );
                    if has_spike {
                        plot.points(
                            Points::new("NG", vec![mock::PCR_SPIKE])
                                .color(mock::LIMIT_COLOR)
                                .radius(5.0)
                                .filled(true),
                        );
                    }
                    if point_selected {
                        plot.points(
                            Points::new("selected", vec![mock::PCR_SPIKE])
                                .color(egui::Color32::WHITE)
                                .radius(9.0)
                                .filled(false),
                        );
                    }
                }
                plot.screen_from_plot(PlotPoint::new(mock::PCR_SPIKE[0], mock::PCR_SPIKE[1]))
            });

        let spike_on_screen = pcr_plot.inner;
        let response = &pcr_plot.response;
        if response.clicked() || response.secondary_clicked() {
            self.point_selected = show_pcr
                && has_spike
                && response
                    .interact_pointer_pos()
                    .is_some_and(|pos| pos.distance(spike_on_screen) <= PICK_DISTANCE);
        }
        let point_selected = self.point_selected && has_spike;
        response.context_menu(|ui| {
            if ui
                .add_enabled(
                    point_selected,
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

        ui.add_space(8.0);
        ui.horizontal(|ui| {
            ui.strong("PCR → PTS/DTS gap");
            ui.weak("ms per PID · limit 1000 ms · DTS used when present");
        });
        let hidden = &self.hidden_pids;
        Plot::new("pts_gap")
            .height(200.0)
            .legend(Legend::default())
            .link_axis(egui::Id::unique(LINK_GROUP), linked)
            .link_cursor(egui::Id::unique(LINK_GROUP), linked)
            .include_y(0.0)
            .include_y(1200.0)
            .allow_zoom([true, false])
            .allow_drag([true, false])
            .allow_scroll([true, false])
            .allow_axis_zoom_drag([true, false])
            .allow_boxed_zoom(false)
            .show(ui, |plot| {
                if reset_zoom {
                    plot.set_auto_bounds(true);
                }
                plot.hline(
                    HLine::new("limit", 1000.0)
                        .color(mock::LIMIT_COLOR)
                        .style(LineStyle::Dashed { length: 8.0 }),
                );
                pmt_updates(plot);
                for (index, stream) in program.streams.iter().enumerate() {
                    if hidden.contains(&stream.pid) {
                        continue;
                    }
                    plot.line(
                        Line::new(stream.label(), stream.gap_ms()).color(stream.color(index)),
                    );
                }
            });

        if self.show_events {
            ui.add_space(8.0);
            ui.strong("Events");
            Plot::new("events")
                .height(60.0)
                .link_axis(egui::Id::unique(LINK_GROUP), linked)
                .link_cursor(egui::Id::unique(LINK_GROUP), linked)
                .show_axes([true, false])
                .show_grid([true, false])
                .include_y(-1.0)
                .include_y(1.0)
                .allow_zoom(false)
                .allow_drag(false)
                .show(ui, |plot| {
                    if reset_zoom {
                        plot.set_auto_bounds(true);
                    }
                    pmt_updates(plot);
                    let cc_events: Vec<[f64; 2]> =
                        mock::CC_EVENT_SECONDS.iter().map(|&s| [s, 0.0]).collect();
                    plot.points(
                        Points::new("CC error", cc_events)
                            .shape(MarkerShape::Diamond)
                            .color(Verdict::Warn.colors().0)
                            .radius(6.0)
                            .filled(true),
                    );
                    if has_spike {
                        plot.points(
                            Points::new("PCR NG", vec![[mock::PCR_SPIKE[0], 0.0]])
                                .color(mock::LIMIT_COLOR)
                                .radius(6.0)
                                .filled(true),
                        );
                    }
                });
        }
        ui.weak("Click a point to select · Right-click for actions · Scroll: zoom · Drag: pan");
        open_packet
    }

    /// Returns true when "Open packet" is clicked.
    fn side_panel(&mut self, ui: &mut egui::Ui, program: &Program) -> bool {
        ui.add_space(4.0);
        ui.strong("Series");
        ui.checkbox(
            &mut self.show_pcr,
            format!("PCR interval (0x{:04X})", program.pcr_pid),
        );
        for stream in &program.streams {
            let mut shown = !self.hidden_pids.contains(&stream.pid);
            let present = if stream.from_seconds > 0.0 {
                format!("  from {:.0} s", stream.from_seconds)
            } else if stream.to_seconds < 60.0 {
                format!("  until {:.0} s", stream.to_seconds)
            } else {
                String::new()
            };
            if ui
                .checkbox(&mut shown, format!("{} gap{present}", stream.label()))
                .changed()
            {
                if shown {
                    self.hidden_pids.remove(&stream.pid);
                } else {
                    self.hidden_pids.insert(stream.pid);
                }
            }
        }
        ui.checkbox(&mut self.show_events, "Event markers");

        ui.add_space(12.0);
        ui.strong("Selected point");
        let mut open_packet = false;
        egui::Frame::group(ui.style()).show(ui, |ui| {
            ui.set_width(ui.available_width());
            if !(self.point_selected && program.number == 1) {
                ui.weak("Click a point on a plot to select it.");
                return;
            }
            egui::Grid::new("timeline_selection")
                .num_columns(2)
                .spacing([12.0, 4.0])
                .show(ui, |ui| {
                    for (key, value) in [
                        ("Time", "00:00:42.120"),
                        ("Offset", "0x008892DC"),
                        ("Packet", "#47,609"),
                        ("PID", "0x0100"),
                        ("Interval", "112.4 ms"),
                    ] {
                        ui.weak(key);
                        ui.monospace(value);
                        ui.end_row();
                    }
                    ui.weak("Verdict");
                    badge(ui, "NG", Verdict::Ng);
                    ui.end_row();
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
    mock::VIDEO_COLOR,
    mock::AUDIO_COLOR,
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
        let pcr_plot =
            timeline_plot("pcr_interval", 220.0, linked, limits.0 * 1.2).show(ui, |plot| {
                if reset_zoom {
                    plot.set_auto_bounds(true);
                }
                plot.hline(
                    HLine::new("limit", limits.0)
                        .color(mock::LIMIT_COLOR)
                        .style(LineStyle::Dashed { length: 8.0 }),
                );
                if show_pcr {
                    plot.line(
                        Line::new(format!("PCR {pcr_label}"), data.pcr_drawn.clone())
                            .color(mock::PCR_COLOR),
                    );
                    if !data.pcr_over_limit.is_empty() {
                        plot.points(
                            Points::new("over limit", data.pcr_over_limit.clone())
                                .color(mock::LIMIT_COLOR)
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
                .then_some(data.pcr_interval.iter())
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
        let gap_plot = timeline_plot("pts_gap", 200.0, linked, limits.1 * 1.2).show(ui, |plot| {
            if reset_zoom {
                plot.set_auto_bounds(true);
            }
            plot.hline(
                HLine::new("limit", limits.1)
                    .color(mock::LIMIT_COLOR)
                    .style(LineStyle::Dashed { length: 8.0 }),
            );
            for (index, gap) in data.gaps.iter().enumerate() {
                if hidden.contains(&gap.pid) {
                    continue;
                }
                plot.line(
                    Line::new(gap.label.clone(), gap.drawn.clone()).color(series_color(index)),
                );
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
            let event_points: Vec<Point> = data
                .events
                .iter()
                .map(|e| Point {
                    seconds: e.seconds,
                    value: 0.0,
                    offset_bytes: e.offset_bytes,
                })
                .collect();
            let events_plot = Plot::new("events")
                .height(60.0)
                .link_axis(egui::Id::unique(LINK_GROUP), linked)
                .link_cursor(egui::Id::unique(LINK_GROUP), linked)
                .show_axes([true, false])
                .show_grid([true, false])
                .include_y(-1.0)
                .include_y(1.0)
                .allow_zoom(false)
                .allow_drag(false)
                .show(ui, |plot| {
                    if reset_zoom {
                        plot.set_auto_bounds(true);
                    }
                    plot.points(
                        Points::new(
                            "event",
                            event_points
                                .iter()
                                .map(|p| [p.seconds, 0.0])
                                .collect::<Vec<_>>(),
                        )
                        .shape(MarkerShape::Diamond)
                        .color(Verdict::Warn.colors().0)
                        .radius(6.0)
                        .filled(true),
                    );
                    highlight(plot, selection.as_ref(), "Event");
                });
            if let Some(pick) = pick(&events_plot, event_points.iter())
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

/// A time plot linked to the other Timeline plots: zoom and pan act on time only.
fn timeline_plot(id: &str, height: f32, linked: egui::Vec2b, top: f64) -> Plot<'static> {
    Plot::new(id.to_owned())
        .height(height)
        .legend(Legend::default())
        .link_axis(egui::Id::unique(LINK_GROUP), linked)
        .link_cursor(egui::Id::unique(LINK_GROUP), linked)
        .include_y(0.0)
        .include_y(top)
        .allow_zoom([true, false])
        .allow_drag([true, false])
        .allow_scroll([true, false])
        .allow_axis_zoom_drag([true, false])
        .allow_boxed_zoom(false)
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
fn pick<'a, R>(
    plot: &egui_plot::PlotResponse<R>,
    points: impl Iterator<Item = &'a Point>,
) -> Option<Point> {
    if !(plot.response.clicked() || plot.response.secondary_clicked()) {
        return None;
    }
    nearest(points, |p| distance(plot, p))
        .filter(|(_, d)| *d <= PICK_DISTANCE)
        .map(|(p, _)| *p)
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
