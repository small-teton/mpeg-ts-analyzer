use eframe::egui;
use serde::{Deserialize, Serialize};

use crate::cli::{Cli, Source};
use crate::views::packet_pages::Paging;

/// Key of the settings in eframe's storage.
pub const STORAGE_KEY: &str = "settings";

/// User preferences, saved between runs. Missing fields (from an older
/// version) take their defaults, and fields no longer used (`language`,
/// saved by versions that showed an inactive Language choice) are ignored.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Settings {
    /// Ranges (Limit) larger than this, in MiB, ask for confirmation before Start.
    pub confirm_range_mib: u64,
    /// Packets per page in Packets; see [`Settings::paging`].
    pub packets_per_page: u64,
    /// Packet pages kept in memory, the one shown included; see [`Settings::paging`].
    pub cached_pages: u32,
    pub cli_path: String,
    /// Look for a newer GUI release once at startup (docs/gui/design.md, "Updates").
    pub check_updates_at_startup: bool,
    /// A GUI version the user chose to skip: the startup check does not open
    /// the update window for it (docs/gui/design.md, "Updates").
    pub skipped_update_version: Option<String>,
}

impl Default for Settings {
    fn default() -> Self {
        Self {
            confirm_range_mib: 1024,
            packets_per_page: 100,
            cached_pages: 5,
            cli_path: String::new(),
            check_updates_at_startup: true,
            skipped_update_version: None,
        }
    }
}

/// The CLI in use, for the Settings window.
pub struct CliStatus<'a> {
    pub source: Source,
    pub path: &'a std::path::Path,
    pub check: Result<&'a Cli, &'a str>,
}

impl Settings {
    /// The packet paging for the next analysis, validated: an analysis keeps
    /// the paging it started Packets with.
    pub fn paging(&self) -> Paging {
        Paging::new(self.packets_per_page, self.cached_pages)
    }

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
                        // Not a choice: the UI is English only (docs/gui/design.md, "Settings").
                        ui.label("Language");
                        ui.vertical(|ui| {
                            ui.label("English");
                            ui.weak("Other languages are not available yet.");
                        });
                        ui.end_row();

                        ui.label("Updates");
                        ui.checkbox(
                            &mut self.check_updates_at_startup,
                            "Check for updates at startup",
                        );
                        ui.end_row();
                    });

                section(ui, "Analysis range");
                egui::Grid::new("settings_range")
                    .num_columns(2)
                    .spacing([16.0, 8.0])
                    .show(ui, |ui| {
                        let label = ui.label("Confirm ranges above");
                        ui.add(
                            egui::DragValue::new(&mut self.confirm_range_mib)
                                .range(1..=65_536)
                                .suffix(" MiB"),
                        )
                        .labelled_by(label.id);
                        ui.end_row();
                    });

                section(ui, "Packets");
                egui::Grid::new("settings_packets")
                    .num_columns(2)
                    .spacing([16.0, 8.0])
                    .show(ui, |ui| {
                        let label = ui.label("Packets per page");
                        ui.add(
                            egui::DragValue::new(&mut self.packets_per_page)
                                .range(Paging::PAGE_SIZES),
                        )
                        .labelled_by(label.id);
                        ui.end_row();

                        let label = ui.label("Cached pages");
                        ui.add(
                            egui::DragValue::new(&mut self.cached_pages)
                                .range(Paging::CACHED_PAGES),
                        )
                        .on_hover_text("Packet pages kept in memory, the one shown included")
                        .labelled_by(label.id);
                        ui.end_row();
                    });
                ui.weak("Applied to the next analysis; the current Packets view keeps its paging.");

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
    ui.label(crate::theme::emphasis(title));
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
            confirm_range_mib: 256,
            packets_per_page: 50,
            cached_pages: 3,
            cli_path: "/opt/mpeg-ts-analyzer/bin/mpeg-ts-analyzer".to_owned(),
            check_updates_at_startup: false,
            skipped_update_version: Some("9.9.9".to_owned()),
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
        assert_eq!(loaded.skipped_update_version, None);
    }

    /// Saves from versions with the Language choice still load.
    #[test]
    fn a_saved_language_is_ignored() {
        let mut storage = MemoryStorage::default();
        storage.0.insert(
            STORAGE_KEY.to_owned(),
            "(language: Japanese, packets_per_page: 50, cli_path: \"/x\")".to_owned(),
        );
        let loaded: Settings = eframe::get_value(&storage, STORAGE_KEY).unwrap();
        assert_eq!(loaded.cli_path, "/x");
        assert_eq!(loaded.packets_per_page, 50);
    }

    #[test]
    fn paging_comes_from_the_settings_and_is_validated() {
        let settings = Settings {
            packets_per_page: 250,
            cached_pages: 2,
            ..Settings::default()
        };
        assert_eq!(settings.paging(), Paging::new(250, 2));
        assert_eq!(Settings::default().paging(), Paging::default());
        // Out of range in a hand-edited save: clamped, not used as is.
        let settings = Settings {
            packets_per_page: 0,
            cached_pages: 1_000,
            ..Settings::default()
        };
        let paging = settings.paging();
        assert_eq!((paging.page_size(), paging.cached_pages()), (10, 50));
    }
}
