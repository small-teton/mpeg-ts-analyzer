use std::collections::BTreeSet;

use eframe::egui;
use egui_plot::{HLine, Legend, Line, LineStyle, MarkerShape, Plot, PlotPoint, Points, VLine};

use super::{Screen, badge, program_selector};
use crate::icons;
use crate::mock::{self, Verdict};
use crate::mock_programs::{Program, programs};

const LINK_GROUP: &str = "timeline";
const SIDE_WIDTH: f32 = 260.0;
/// How close (in screen pixels) a click must be to a point to select it.
const PICK_DISTANCE: f32 = 12.0;

pub struct TimelineView {
    /// PIDs unchecked in the side panel.
    hidden_pids: BTreeSet<u16>,
    show_pcr: bool,
    show_events: bool,
    point_selected: bool,
    reset_zoom: bool,
}

impl Default for TimelineView {
    fn default() -> Self {
        Self {
            hidden_pids: BTreeSet::new(),
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
