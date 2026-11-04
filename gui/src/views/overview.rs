use eframe::egui;

use super::{Screen, badge, program_selector_labels, status_verdict};
use crate::psi;
use crate::range_bar;
use crate::report::{AnalysisReport, Check, Program, stream_type_name};
use crate::theme::Verdict;

pub fn show_report(
    ui: &mut egui::Ui,
    report: &AnalysisReport,
    program: &mut usize,
) -> Option<Screen> {
    let labels: Vec<String> = report.programs.iter().map(Program::label).collect();
    ui.horizontal(|ui| {
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

        ui.horizontal(|ui| {
            ui.heading("Compliance");
            ui.weak("ISO/IEC 13818-1 timing checks");
        });
        ui.add_space(6.0);
        ui.horizontal_wrapped(|ui| {
            if let Some(compliance) = &selected.compliance {
                check_card(ui, "Max PCR interval", &compliance.max_pcr_interval);
                check_card(ui, "PCR → PTS/DTS gap", &compliance.max_pcr_pts_dts_gap);
            }
            if let Some(anomalies) = &selected.timestamp_anomalies {
                let count: u64 = anomalies.items.iter().map(|a| a.count).sum();
                card(
                    ui,
                    "PTS/DTS anomalies",
                    &anomalies.status,
                    &count.to_string(),
                    "per program",
                );
            }
            if let Some(continuity) = &report.continuity_counter {
                card(
                    ui,
                    "Continuity counter",
                    &continuity.status,
                    &continuity.total_error_count.to_string(),
                    "errors in the whole multiplex",
                );
            }
            if let Some(jitter) = &selected.pcr_jitter {
                let value = jitter
                    .max_jitter_ns
                    .zip(jitter.min_jitter_ns)
                    .map_or("—".to_owned(), |(max, min)| {
                        format!("{:.0} ns", max.abs().max(min.abs()))
                    });
                let note = jitter
                    .reason
                    .clone()
                    .unwrap_or_else(|| "largest deviation".to_owned());
                card(ui, "PCR jitter", &jitter.status, &value, &note);
            }
        });
        ui.add_space(10.0);

        ui.strong("Elementary streams");
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
        ui.add_space(10.0);

        let anomalies = selected
            .timestamp_anomalies
            .as_ref()
            .map_or(&[][..], |a| &a.items[..]);
        let count = report.warnings.len() + anomalies.len() + report.continuity_events.len();
        egui::CollapsingHeader::new(format!("Warnings ({count})"))
            .default_open(true)
            .show(ui, |ui| {
                if count == 0 {
                    ui.weak("None.");
                }
                for warning in &report.warnings {
                    ui.horizontal(|ui| {
                        badge(ui, "WARN", Verdict::Warn);
                        ui.monospace(format!("0x{:08X}", warning.offset_bytes));
                        ui.label(&warning.message);
                    });
                }
                for event in &report.continuity_events {
                    ui.horizontal(|ui| {
                        badge(ui, "CC", Verdict::Warn);
                        ui.monospace(format!("0x{:08X}", event.offset_bytes));
                        ui.label(format!(
                            "PID {}: continuity_counter {} (expected {})",
                            event.pid, event.actual_cc, event.expected_cc
                        ));
                    });
                }
                for anomaly in anomalies {
                    ui.horizontal(|ui| {
                        badge(ui, "PTS", Verdict::Warn);
                        ui.label(format!(
                            "PID {} · {}× · {}",
                            anomaly.pid, anomaly.count, anomaly.message
                        ));
                    });
                }
            });
    });
    None
}

fn report_stream(ui: &mut egui::Ui, report: &AnalysisReport, program: &Program) {
    ui.heading("Stream");
    ui.add_space(4.0);
    ui.horizontal_top(|ui| {
        ui.group(|ui| {
            ui.set_min_width(320.0);
            ui.vertical(|ui| {
                egui::CollapsingHeader::new(format!("Program {}", program.label()))
                    .id_salt("overview_report_program")
                    .default_open(true)
                    .show(ui, |ui| {
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
    });
}

fn mbps(bps: f64) -> String {
    format!("{:.3} Mbps", bps / 1e6)
}

fn check_card(ui: &mut egui::Ui, title: &str, check: &Check) {
    let value = check
        .value_ms
        .map_or("—".to_owned(), |ms| format!("{ms:.1} ms"));
    let note = match &check.reason {
        Some(reason) => reason.clone(),
        None => format!(
            "limit {:.0} ms · {} observations",
            check.limit_ms, check.observations
        ),
    };
    card(ui, title, &check.status, &value, &note);
}

fn card(ui: &mut egui::Ui, title: &str, status: &str, value: &str, note: &str) {
    ui.group(|ui| {
        ui.set_width(220.0);
        ui.vertical(|ui| {
            ui.horizontal(|ui| {
                ui.label(title);
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    badge(ui, &status.to_uppercase(), status_verdict(status));
                });
            });
            ui.label(egui::RichText::new(value).size(24.0).strong());
            ui.weak(egui::RichText::new(note).small());
        });
    });
}
