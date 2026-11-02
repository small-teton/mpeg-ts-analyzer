use eframe::egui;

#[derive(Clone, Copy, PartialEq, Eq)]
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

/// User preferences. Not persisted or applied yet; the window only lays out the options.
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

impl Settings {
    pub fn show_window(&mut self, ctx: &egui::Context, open: &mut bool) {
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
                        ui.add(
                            egui::TextEdit::singleline(&mut self.cli_path)
                                .hint_text("bundled, then PATH")
                                .desired_width(260.0),
                        );
                        ui.end_row();
                    });

                ui.add_space(8.0);
                ui.horizontal(|ui| {
                    if ui.button("Restore defaults").clicked() {
                        *self = Settings::default();
                    }
                });
            });
    }
}

fn section(ui: &mut egui::Ui, title: &str) {
    ui.add_space(6.0);
    ui.strong(title);
    ui.separator();
}
