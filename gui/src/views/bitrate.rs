use eframe::egui;
use egui_plot::{Bar, BarChart, HLine, Legend, Line, LineStyle, Plot};

use super::program_selector;
use crate::mock;
use crate::mock_programs::programs;

pub fn show(ui: &mut egui::Ui, selected: &mut usize) {
    ui.horizontal(|ui| {
        program_selector(ui, selected);
    });
    ui.separator();
    let programs = programs();
    let program = &programs[*selected];
    egui::ScrollArea::vertical().show(ui, |ui| {
        ui.horizontal(|ui| {
            ui.strong("Bitrate per PID");
            ui.weak("Mbps · PCR time base · 1 s window");
        });
        Plot::new("bitrate")
            .height(240.0)
            .legend(Legend::default())
            .include_y(0.0)
            .include_y(2.0)
            .show(ui, |plot| {
                plot.line(
                    Line::new("Total", mock::constant_mbps(program.average_mbps))
                        .color(egui::Color32::LIGHT_GRAY)
                        .style(LineStyle::Dashed { length: 6.0 }),
                );
                for (index, stream) in program.streams.iter().enumerate() {
                    let line = Line::new(stream.label(), stream.bitrate_mbps(program))
                        .color(stream.color(index));
                    plot.line(if index == 0 { line.fill(0.0).fill_alpha(0.12) } else { line });
                }
                plot.line(Line::new("PAT + PMT", mock::constant_mbps(0.030)).color(mock::PSI_COLOR));
            });
        ui.add_space(10.0);

        ui.columns(2, |columns| {
            columns[0].horizontal(|ui| {
                ui.strong("PCR jitter");
                ui.weak(format!("PCR PID 0x{:04X} · deviation from expected PCR, ms", program.pcr_pid));
            });
            Plot::new("pcr_jitter")
                .height(160.0)
                .include_y(-0.02)
                .include_y(0.02)
                .show(&mut columns[0], |plot| {
                    plot.hline(HLine::new("expected", 0.0).color(egui::Color32::DARK_GRAY));
                    plot.line(Line::new("jitter", mock::pcr_jitter_ms()).color(mock::PCR_COLOR));
                });
            columns[0].horizontal(|ui| {
                ui.weak("max |dev|");
                ui.monospace("0.009812");
                ui.weak("σ");
                ui.monospace("0.003602");
            });

            columns[1].horizontal(|ui| {
                ui.strong("Jitter histogram");
                ui.weak("1,502 intervals");
            });
            let bars = mock::JITTER_HISTOGRAM
                .iter()
                .map(|&(center, count)| Bar::new(center, count).width(0.0018))
                .collect();
            Plot::new("jitter_histogram")
                .height(160.0)
                .allow_zoom(false)
                .allow_drag(false)
                .show(&mut columns[1], |plot| {
                    plot.bar_chart(BarChart::new("intervals", bars).color(mock::PCR_COLOR));
                });
        });
        ui.add_space(10.0);

        egui::Grid::new("bitrate_table")
            .striped(true)
            .num_columns(5)
            .spacing([24.0, 4.0])
            .show(ui, |ui| {
                for header in ["PID", "Type", "Average", "Peak", "Share"] {
                    ui.strong(header);
                }
                ui.end_row();
                for stream in &mock::STREAMS {
                    ui.monospace(stream.pid);
                    ui.label(stream.kind);
                    ui.monospace(stream.average);
                    ui.monospace(stream.peak);
                    ui.monospace(stream.share);
                    ui.end_row();
                }
            });
        ui.weak("File-based estimate assuming a near-constant delivery rate. Not a TR 101 290 measurement.");
    });
}
