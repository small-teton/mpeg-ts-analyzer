use eframe::egui;
use egui_plot::{Bar, BarChart, HLine, Legend, Line, LineStyle, Plot};

use super::program_selector_labels;
use super::timeline::series_color;
use crate::plot_data;
use crate::report::{AnalysisReport, Program as ReportProgram};
use crate::theme;

/// The Bitrate screen for one program of an analysis.
pub fn show_report(
    ui: &mut egui::Ui,
    report: &AnalysisReport,
    bitrates: &[plot_data::Bitrate],
    generation: u64,
    selected: &mut usize,
) {
    let labels: Vec<String> = report.programs.iter().map(ReportProgram::label).collect();
    ui.horizontal(|ui| {
        program_selector_labels(ui, &labels, selected);
    });
    ui.separator();
    let (Some(program), Some(data)) = (report.programs.get(*selected), bitrates.get(*selected))
    else {
        ui.weak("No program was found in this range.");
        return;
    };
    // Plot state (zoom, pan) belongs to one program of one analysis: a new id
    // starts again from the data's own Y range.
    let context = (generation, *selected);
    egui::ScrollArea::vertical().show(ui, |ui| {
        ui.horizontal(|ui| {
            ui.strong("Bitrate per PID");
            ui.weak("Mbps · PCR time base · 1 s windows (the last one may be shorter)");
        });
        time_plot(("bitrate", context), 240.0, data.mbps_range)
            .legend(Legend::default())
            .show(ui, |plot| {
                plot.line(
                    Line::new("Program (incl. PSI)", data.program_drawn.clone())
                        .color(egui::Color32::LIGHT_GRAY)
                        .style(LineStyle::Dashed { length: 6.0 }),
                );
                for (index, pid) in data.pids.iter().enumerate() {
                    plot.line(
                        Line::new(pid.label.clone(), pid.drawn.clone())
                            .color(series_color(index)),
                    );
                }
            });
        ui.add_space(10.0);

        let jitter = program.pcr_jitter.as_ref();
        ui.columns(2, |columns| {
            columns[0].horizontal(|ui| {
                ui.strong("PCR jitter");
                ui.weak(format!(
                    "PCR PID {} · deviation from the expected PCR, ms",
                    program.pcr_pid.as_deref().unwrap_or("none")
                ));
            });
            time_plot(("pcr_jitter", context), 160.0, data.jitter_range)
                .show(&mut columns[0], |plot| {
                    plot.hline(HLine::new("expected", 0.0).color(egui::Color32::DARK_GRAY));
                    plot.line(
                        Line::new("jitter", data.jitter_drawn.clone())
                            .color(theme::PCR_COLOR),
                    );
                });
            columns[0].horizontal_wrapped(|ui| match jitter {
                Some(j) if j.max_jitter_ns.is_some() => {
                    let max_abs = j
                        .max_jitter_ns
                        .unwrap_or(0.0)
                        .abs()
                        .max(j.min_jitter_ns.unwrap_or(0.0).abs());
                    ui.weak("max |dev|");
                    ui.monospace(format!("{:.6} ms", max_abs / 1e6));
                    if let Some(mean) = j.average_absolute_jitter_ns {
                        ui.weak("mean |dev|");
                        ui.monospace(format!("{:.6} ms", mean / 1e6));
                    }
                    if let Some(within) = j.within_500ns_percent {
                        ui.weak("within ±500 ns");
                        ui.monospace(format!("{within:.1} %"));
                    }
                }
                Some(j) => {
                    ui.weak(j.reason.as_deref().unwrap_or("Not enough PCR samples."));
                }
                None => {
                    ui.weak("No PCR jitter in this program.");
                }
            });

            columns[1].horizontal(|ui| {
                ui.strong("Jitter histogram");
                ui.weak(format!("{} samples, ms", data.jitter.len()));
            });
            let bars = data
                .jitter_histogram
                .iter()
                .map(|&(center, count, width)| Bar::new(center, count).width(width * 0.9))
                .collect();
            Plot::new("jitter_histogram")
                .height(160.0)
                .allow_zoom(false)
                .allow_drag(false)
                .show(&mut columns[1], |plot| {
                    plot.bar_chart(BarChart::new("samples", bars).color(theme::PCR_COLOR));
                });
        });
        ui.add_space(10.0);

        if let Some(bitrate) = &program.bitrate {
            let rows = bitrate
                .pids
                .iter()
                .filter(|row| !data.program_pids.is_psi(&row.pid))
                .chain(bitrate.null_pid.as_ref());
            egui::Grid::new("bitrate_table")
                .striped(true)
                .num_columns(5)
                .spacing([24.0, 4.0])
                .show(ui, |ui| {
                    for header in ["PID", "Carries", "Average", "Peak (1 s)", "Share"] {
                        ui.strong(header);
                    }
                    ui.end_row();
                    for row in rows {
                        ui.monospace(&row.pid);
                        ui.label(data.program_pids.carried_by(&row.pid, &row.label));
                        ui.monospace(format!("{:.3} Mbps", row.average_bps / 1e6));
                        ui.monospace(
                            row.peak_1s_bps
                                .map_or("—".to_owned(), |bps| format!("{:.3} Mbps", bps / 1e6)),
                        );
                        let share = if bitrate.program_average_bps > 0.0 {
                            format!("{:.1} %", 100.0 * row.average_bps / bitrate.program_average_bps)
                        } else {
                            "—".to_owned()
                        };
                        ui.monospace(share);
                        ui.end_row();
                    }
                    ui.strong("Program").on_hover_text("All PIDs of the program, including PAT and PMT");
                    ui.label("");
                    ui.monospace(format!("{:.3} Mbps", bitrate.program_average_bps / 1e6));
                    ui.monospace(
                        bitrate
                            .program_peak_1s_bps
                            .map_or("—".to_owned(), |bps| format!("{:.3} Mbps", bps / 1e6)),
                    );
                    ui.label("");
                    ui.end_row();
                });
            ui.weak(format!(
                "Measured over {:.2} s of PCR time. File-based estimate assuming a near-constant delivery rate. Not a TR 101 290 measurement.",
                bitrate.duration_seconds
            ));
        }
    });
}

/// A plot over time like Timeline's: zoom, scroll and drag move the time axis
/// only, and the Y axis stays at `y_range` (a reset returns to it).
fn time_plot(id: impl egui::AsIdSalt, height: f32, y_range: (f64, f64)) -> Plot<'static> {
    Plot::new(id)
        .height(height)
        .default_y_bounds(y_range.0, y_range.1)
        .allow_zoom([true, false])
        .allow_drag([true, false])
        .allow_scroll([true, false])
        .allow_axis_zoom_drag([true, false])
        .allow_boxed_zoom(false)
}
