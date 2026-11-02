use eframe::egui;
use serde::{Deserialize, Serialize};

use crate::cli::{Cli, Source};

#[derive(Clone, Copy, PartialEq, Eq, Debug, Serialize, Deserialize)]
pub enum Language {
    System,
    English,
    Japanese,
}

impl Language {
    const ALL: [Language; 3] = [Language::System, Language::English, Language::Japanese];

    fn label(self) -> &'static str {
        match self {
            Language::System => "System default",
            Language::English => "English",
            // Shown in English until a Japanese font is bundled.
            Language::Japanese => "Japanese",
        }
    }
}

/// Key of the settings in eframe's storage.
pub const STORAGE_KEY: &str = "settings";

/// User preferences, saved between runs. Missing fields (from an older
/// version) take their defaults. Only the CLI path and the confirmation size
/// are applied so far; the window lays out the other options.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Settings {
    pub language: Language,
    /// Ranges (Limit) larger than this, in MiB, ask for confirmation before Start.
    pub confirm_range_mib: u64,
    pub packets_per_page: u64,
    pub cached_pages: u32,
    pub cli_path: String,
}

impl Default for Settings {
    fn default() -> Self {
        Self {
            language: Language::System,
            confirm_range_mib: 1024,
            packets_per_page: 100,
            cached_pages: 5,
            cli_path: String::new(),
        }
    }
}

/// The CLI in use, for the Settings window.
pub struct CliStatus<'a> {
    pub source: Source,
    pub path: &'a std::path::Path,
    pub check: &'a Result<Cli, String>,
}

impl Settings {
    /// Returns true when the CLI should be looked up again (its path changed).
    pub fn show_window(&mut self, ctx: &egui::Context, open: &mut bool, cli: &CliStatus) -> bool {
        let mut recheck = false;
        egui::Window::new("Settings")
            .open(open)
            .collapsible(false)
            .resizable(false)
            .default_width(420.0)
            .show(ctx, |ui| {
                section(ui, "General");
                egui::Grid::new("settings_general")
                    .num_columns(2)
                    .spacing([16.0, 8.0])
                    .show(ui, |ui| {
                        ui.label("Language");
                        egui::ComboBox::from_id_salt("settings_language")
                            .selected_text(self.language.label())
                            .show_ui(ui, |ui| {
                                for language in Language::ALL {
                                    ui.selectable_value(
                                        &mut self.language,
                                        language,
                                        language.label(),
                                    );
                                }
                            });
                        ui.end_row();
                    });

                section(ui, "Analysis range");
                egui::Grid::new("settings_range")
                    .num_columns(2)
                    .spacing([16.0, 8.0])
                    .show(ui, |ui| {
                        ui.label("Confirm ranges above");
                        ui.add(
                            egui::DragValue::new(&mut self.confirm_range_mib)
                                .range(1..=65_536)
                                .suffix(" MiB"),
                        );
                        ui.end_row();
                    });

                section(ui, "Packets");
                egui::Grid::new("settings_packets")
                    .num_columns(2)
                    .spacing([16.0, 8.0])
                    .show(ui, |ui| {
                        ui.label("Packets per page");
                        ui.add(egui::DragValue::new(&mut self.packets_per_page).range(10..=10_000));
                        ui.end_row();

                        ui.label("Cached pages");
                        ui.add(egui::DragValue::new(&mut self.cached_pages).range(1..=50));
                        ui.end_row();
                    });

                section(ui, "Command-line tool");
                egui::Grid::new("settings_cli")
                    .num_columns(2)
                    .spacing([16.0, 8.0])
                    .show(ui, |ui| {
                        ui.label("CLI path");
                        ui.horizontal(|ui| {
                            let edit = ui.add(
                                egui::TextEdit::singleline(&mut self.cli_path)
                                    .hint_text("bundled, then PATH")
                                    .desired_width(220.0),
                            );
                            recheck |= edit.lost_focus();
                            if ui.button("Browse…").clicked()
                                && let Some(path) = rfd::FileDialog::new().pick_file()
                            {
                                self.cli_path = path.display().to_string();
                                recheck = true;
                            }
                        });
                        ui.end_row();

                        ui.label("In use");
                        ui.vertical(|ui| {
                            ui.monospace(cli.path.display().to_string());
                            ui.weak(format!("found through {}", cli.source.label()));
                            match cli.check {
                                Ok(found) => ui.weak(format!("version {}", found.version)),
                                Err(err) => ui.colored_label(ui.visuals().warn_fg_color, err),
                            };
                        });
                        ui.end_row();
                    });
                ui.weak(
                    "Order: MPEG_TS_ANALYZER_CLI, this path, the CLI bundled with the GUI, PATH.",
                );

                ui.add_space(8.0);
                ui.horizontal(|ui| {
                    if ui.button("Restore defaults").clicked() {
                        recheck |= !self.cli_path.is_empty();
                        *self = Settings::default();
                    }
                });
            });
        recheck
    }
}

fn section(ui: &mut egui::Ui, title: &str) {
    ui.add_space(6.0);
    ui.strong(title);
    ui.separator();
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    #[derive(Default)]
    struct MemoryStorage(HashMap<String, String>);

    impl eframe::Storage for MemoryStorage {
        fn get_string(&self, key: &str) -> Option<String> {
            self.0.get(key).cloned()
        }
        fn set_string(&mut self, key: &str, value: String) {
            self.0.insert(key.to_owned(), value);
        }
        fn remove_string(&mut self, key: &str) {
            self.0.remove(key);
        }
        fn flush(&mut self) {}
    }

    #[test]
    fn settings_survive_a_restart() {
        let saved = Settings {
            language: Language::Japanese,
            confirm_range_mib: 256,
            packets_per_page: 50,
            cached_pages: 3,
            cli_path: "/opt/mpeg-ts-analyzer/bin/mpeg-ts-analyzer".to_owned(),
        };
        let mut storage = MemoryStorage::default();
        eframe::set_value(&mut storage, STORAGE_KEY, &saved);
        let loaded: Option<Settings> = eframe::get_value(&storage, STORAGE_KEY);
        assert_eq!(loaded, Some(saved));
    }

    #[test]
    fn older_settings_take_defaults() {
        let mut storage = MemoryStorage::default();
        storage
            .0
            .insert(STORAGE_KEY.to_owned(), "(cli_path: \"/x\")".to_owned());
        let loaded: Settings = eframe::get_value(&storage, STORAGE_KEY).unwrap();
        assert_eq!(loaded.cli_path, "/x");
        assert_eq!(
            loaded.confirm_range_mib,
            Settings::default().confirm_range_mib
        );
    }
}
