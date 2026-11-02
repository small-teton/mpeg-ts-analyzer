use eframe::egui;

use super::{Screen, badge, program_selector};
use crate::mock;
use crate::mock_programs::programs;

/// Returns the screen to switch to when a warning link is clicked.
pub fn show(ui: &mut egui::Ui, program: &mut usize) -> Option<Screen> {
    let mut navigate = None;
    ui.horizontal(|ui| {
        program_selector(ui, program);
    });
    ui.separator();
    egui::ScrollArea::vertical().show(ui, |ui| {
        stream(ui, *program);
        ui.add_space(12.0);

        ui.horizontal(|ui| {
            ui.heading("Compliance");
            ui.weak("ISO/IEC 13818-1 timing checks");
        });
        ui.add_space(6.0);

        ui.horizontal_wrapped(|ui| {
            for check in &mock::CHECKS {
                ui.group(|ui| {
                    ui.set_width(220.0);
                    ui.vertical(|ui| {
                        ui.horizontal(|ui| {
                            ui.label(check.label);
                            ui.with_layout(
                                egui::Layout::right_to_left(egui::Align::Center),
                                |ui| {
                                    badge(ui, check.verdict.label(), check.verdict);
                                },
                            );
                        });
                        ui.label(egui::RichText::new(check.value).size(24.0).strong());
                        ui.weak(egui::RichText::new(check.note).small());
                    });
                });
            }
        });
        ui.add_space(10.0);

        ui.strong("Elementary streams");
        egui::Grid::new("overview_streams")
            .striped(true)
            .num_columns(6)
            .spacing([24.0, 4.0])
            .show(ui, |ui| {
                for header in [
                    "PID",
                    "Type",
                    "Packets",
                    "CC events",
                    "Avg bitrate",
                    "Peak bitrate",
                ] {
                    ui.strong(header);
                }
                ui.end_row();
                for stream in &mock::STREAMS {
                    ui.monospace(stream.pid);
                    ui.label(stream.kind);
                    ui.monospace(stream.packets);
                    ui.monospace(stream.cc_events);
                    ui.monospace(stream.average);
                    ui.monospace(stream.peak);
                    ui.end_row();
                }
            });
        ui.add_space(10.0);

        egui::CollapsingHeader::new(format!("Warnings ({})", mock::WARNINGS.len()))
            .default_open(true)
            .show(ui, |ui| {
                for warning in &mock::WARNINGS {
                    ui.horizontal(|ui| {
                        badge(ui, warning.kind, warning.verdict);
                        ui.monospace(warning.text);
                        if ui.link("Show packet").clicked() {
                            navigate = Some(Screen::Packets);
                        }
                    });
                }
            });
        ui.add_space(10.0);

        ui.horizontal(|ui| {
            let _ = ui.button("Export JSON…");
            let _ = ui.button("Copy summary");
        });
    });
    navigate
}

fn stream(ui: &mut egui::Ui, selected: usize) {
    let programs = programs();
    let program = &programs[selected];
    ui.heading("Stream");
    ui.add_space(4.0);
    ui.horizontal_top(|ui| {
        ui.group(|ui| {
            ui.set_min_width(320.0);
            ui.vertical(|ui| {
                egui::CollapsingHeader::new(format!("Program {}", program.label()))
                    .id_salt("overview_program")
                    .default_open(true)
                    .show(ui, |ui| {
                        for stream in &program.streams {
                            let pcr = if stream.pid == program.pcr_pid {
                                " (PCR)"
                            } else {
                                ""
                            };
                            let present = if stream.from_seconds > 0.0 {
                                format!("  from {:.0} s", stream.from_seconds)
                            } else if stream.to_seconds < 60.0 {
                                format!("  until {:.0} s", stream.to_seconds)
                            } else {
                                String::new()
                            };
                            ui.monospace(format!("{}{pcr}{present}", stream.label()));
                        }
                        for (_, update) in &program.pmt_updates {
                            ui.colored_label(mock::PSI_COLOR, *update);
                        }
                    });
                egui::CollapsingHeader::new("Other programs")
                    .default_open(false)
                    .show(ui, |ui| {
                        for other in programs.iter().filter(|p| p.number != program.number) {
                            ui.monospace(format!("Program {}", other.label()));
                        }
                    });
                egui::CollapsingHeader::new("Other PIDs")
                    .default_open(true)
                    .show(ui, |ui| {
                        ui.monospace("0x0000  PAT");
                        ui.monospace("0x1FFF  Null (CC excluded)");
                    });
            });
        });
        ui.group(|ui| {
            egui::Grid::new("overview_file_info")
                .num_columns(2)
                .spacing([16.0, 4.0])
                .show(ui, |ui| {
                    for (key, value) in [
                        ("Packet", "188 byte TS (auto)"),
                        ("Size", "12.7 MB"),
                        ("Packets", "67,819"),
                        ("Duration", "00:01:00.000"),
                    ] {
                        ui.weak(key);
                        ui.label(value);
                        ui.end_row();
                    }
                });
        });
    });
}
