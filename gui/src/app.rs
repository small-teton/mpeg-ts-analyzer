use std::path::PathBuf;

use eframe::egui;

use crate::cli;
use crate::icons;
use crate::mock;
use crate::range_bar::{self, AnalysisRange, Load};
use crate::settings::Settings;
use crate::views::{Screen, bitrate, overview, packets::PacketsView, timeline::TimelineView};

/// Range selected when a file is opened: small enough to analyze quickly.
const INITIAL_LIMIT: u64 = 64 * 1024 * 1024;

/// How long the mock analysis takes, to show the progress dialog.
const MOCK_ANALYSIS_SECONDS: f32 = 3.0;

/// An analysis in progress.
struct Running {
    offset: u64,
    limit: u64,
    started: std::time::Instant,
}

pub struct App {
    cli_path: PathBuf,
    input: Option<PathBuf>,
    file_size: u64,
    range: AnalysisRange,
    /// The range of the last started analysis.
    analyzed: Option<(u64, u64)>,
    /// Start was pressed for a range large enough to ask first.
    confirm_start: bool,
    running: Option<Running>,
    screen: Screen,
    /// Program shown by Overview, Timeline and Bitrate.
    program: usize,
    timeline: TimelineView,
    packets: PacketsView,
    settings: Settings,
    settings_open: bool,
    about_open: bool,
}

impl Default for App {
    fn default() -> Self {
        Self {
            cli_path: cli::locate(),
            input: None,
            file_size: mock::FILE_SIZE,
            range: AnalysisRange {
                offset: 0,
                limit: INITIAL_LIMIT,
            },
            analyzed: None,
            confirm_start: false,
            running: None,
            screen: Screen::Overview,
            program: 0,
            timeline: TimelineView::default(),
            packets: PacketsView::default(),
            settings: Settings::default(),
            settings_open: false,
            about_open: false,
        }
    }
}

impl App {
    fn open(&mut self, path: PathBuf) {
        self.file_size = std::fs::metadata(&path).map_or(0, |m| m.len());
        self.range = AnalysisRange {
            offset: 0,
            limit: INITIAL_LIMIT,
        };
        self.analyzed = None;
        self.input = Some(path);
    }

    fn pick_file(&mut self) {
        if let Some(path) = rfd::FileDialog::new()
            .add_filter("MPEG-TS", &["ts", "m2ts", "mts", "m2t", "trp"])
            .add_filter("All files", &["*"])
            .pick_file()
        {
            self.open(path);
        }
    }

    fn load(&self) -> Load {
        Load::of(
            self.range.limit,
            self.settings.confirm_range_mib * 1024 * 1024,
        )
    }

    fn start(&mut self) {
        self.running = Some(Running {
            offset: self.range.offset,
            limit: self.range.limit,
            started: std::time::Instant::now(),
        });
    }

    /// Progress dialog over a grayed-out window while the range is analyzed.
    /// The mock advances on a timer; the real one follows the CLI's progress events.
    fn analysis_dialog(&mut self, ctx: &egui::Context) {
        let Some(running) = &self.running else {
            return;
        };
        let elapsed = running.started.elapsed();
        let progress = (elapsed.as_secs_f32() / MOCK_ANALYSIS_SECONDS).min(1.0);
        if progress >= 1.0 {
            self.analyzed = Some((running.offset, running.limit));
            self.running = None;
            return;
        }
        let (offset, limit) = (running.offset, running.limit);
        let mut cancel = false;
        // Clicking the backdrop or pressing Escape does not dismiss it; only Cancel does.
        egui::Modal::new(egui::Id::unique("analysis_progress")).show(ctx, |ui| {
            ui.set_width(420.0);
            ui.heading("Analyzing");
            ui.add_space(2.0);
            ui.monospace(format!(
                "{} – {}",
                range_bar::bytes(offset),
                range_bar::bytes(offset + limit)
            ));
            ui.add_space(8.0);
            ui.add(
                egui::ProgressBar::new(progress)
                    .show_percentage()
                    .animate(true),
            );
            ui.add_space(4.0);
            ui.weak(format!(
                "{} of {} · {:02}:{:02}",
                range_bar::human_size((limit as f32 * progress) as u64),
                range_bar::human_size(limit),
                elapsed.as_secs() / 60,
                elapsed.as_secs() % 60
            ));
            ui.add_space(10.0);
            ui.vertical_centered(|ui| {
                if ui.button("Cancel").clicked() {
                    cancel = true;
                }
            });
        });
        if cancel {
            // The previous result, if any, stays as it was.
            self.running = None;
        }
        ctx.request_repaint();
    }

    /// File, then the byte range to analyze, then Start.
    fn top_bar(&mut self, ui: &mut egui::Ui) {
        self.range.clamp(self.file_size);
        let load = self.load();
        ui.horizontal(|ui| {
            if ui
                .button((icons::text(icons::FOLDER_OPEN), "Open…"))
                .clicked()
            {
                self.pick_file();
            }
            match &self.input {
                Some(path) => ui.monospace(path.display().to_string()),
                None => ui.monospace(mock::FILE_PATH),
            };
            ui.weak(range_bar::bytes(self.file_size));
        });
        ui.horizontal(|ui| {
            ui.strong("Scan range");
            let controls_width = 470.0;
            let bar_width = (ui.available_width() - controls_width).max(160.0);
            self.range.bar(ui, bar_width, self.file_size, load);
            self.range.fields(ui, self.file_size);
            let changed = self.analyzed != Some((self.range.offset, self.range.limit));
            let start = egui::Button::new((icons::text(icons::PLAY), "Start"));
            if ui
                .add_enabled(changed || self.analyzed.is_none(), start)
                .on_disabled_hover_text("This range is already analyzed")
                .clicked()
            {
                if load == Load::NeedsConfirmation {
                    self.confirm_start = true;
                } else {
                    self.start();
                }
            }
        });
        ui.add_space(6.0);
    }

    fn navigation(&mut self, ui: &mut egui::Ui) {
        ui.add_space(6.0);
        ui.spacing_mut().item_spacing.y = 6.0;
        for screen in Screen::ANALYSIS {
            if navigation_tile(ui, screen, self.screen == screen).clicked() {
                self.screen = screen;
            }
        }
        ui.with_layout(egui::Layout::bottom_up(egui::Align::Center), |ui| {
            ui.add_space(6.0);
            let about = egui::Button::new(icons::text(icons::INFO).size(20.0)).frame(false);
            if ui.add(about).on_hover_text("About").clicked() {
                self.about_open = true;
            }
            let settings = egui::Button::new(icons::text(icons::GEAR).size(20.0)).frame(false);
            if ui.add(settings).on_hover_text("Settings").clicked() {
                self.settings_open = true;
            }
        });
    }

    fn confirm_start_dialog(&mut self, ctx: &egui::Context) {
        if !self.confirm_start {
            return;
        }
        let modal = egui::Modal::new(egui::Id::unique("confirm_start")).show(ctx, |ui| {
            ui.set_width(380.0);
            ui.heading("Analyze a large range?");
            ui.add_space(4.0);
            ui.label(format!(
                "The range is {}. Analyzing it can take a while and use a lot of memory.",
                range_bar::bytes(self.range.limit)
            ));
            ui.weak(format!(
                "Ranges above {} MiB ask first (Settings).",
                self.settings.confirm_range_mib
            ));
            ui.add_space(8.0);
            ui.horizontal(|ui| {
                if ui.button((icons::text(icons::PLAY), "Analyze")).clicked() {
                    self.start();
                    self.confirm_start = false;
                }
                if ui.button("Cancel").clicked() {
                    self.confirm_start = false;
                }
            });
        });
        if modal.should_close() {
            self.confirm_start = false;
        }
    }

    fn about_window(&mut self, ctx: &egui::Context) {
        egui::Window::new("About")
            .open(&mut self.about_open)
            .collapsible(false)
            .resizable(false)
            .show(ctx, |ui| {
                ui.heading("mpeg-ts-analyzer");
                ui.label(format!("GUI {}", env!("CARGO_PKG_VERSION")));
                ui.label("MPEG-2 Transport Stream analyzer (ISO/IEC 13818-1)");
                ui.hyperlink("https://github.com/small-teton/mpeg-ts-analyzer");
                ui.weak("Apache License 2.0");
            });
    }
}

impl eframe::App for App {
    fn ui(&mut self, ui: &mut egui::Ui, _frame: &mut eframe::Frame) {
        if let Some(path) =
            ui.input(|i| i.raw.dropped_files.first().map(|f| f.path().to_path_buf()))
        {
            self.open(path);
        }

        egui::Panel::top("top_bar").show(ui, |ui| self.top_bar(ui));
        egui::Panel::bottom("status").show(ui, |ui| {
            ui.horizontal(|ui| {
                match self.analyzed {
                    Some((offset, limit)) => ui.weak(format!(
                        "Analyzed {} – {} (mock data)",
                        range_bar::bytes(offset),
                        range_bar::bytes(offset + limit)
                    )),
                    None => {
                        ui.weak("Choose a range and press Start. Analysis screens show mock data.")
                    }
                };
                ui.separator();
                ui.weak(format!("CLI: {}", self.cli_path.display()));
            });
        });
        egui::Panel::left("navigation")
            .resizable(false)
            .exact_size(96.0)
            .show(ui, |ui| self.navigation(ui));
        egui::CentralPanel::default().show(ui, |ui| {
            let navigate = match self.screen {
                Screen::Overview => overview::show(ui, &mut self.program),
                Screen::Timeline => self.timeline.show(ui, &mut self.program),
                Screen::Bitrate => {
                    bitrate::show(ui, &mut self.program);
                    None
                }
                Screen::Packets => {
                    self.packets.show(ui);
                    None
                }
            };
            if let Some(screen) = navigate {
                self.screen = screen;
            }
        });

        let ctx = ui.ctx().clone();
        self.settings.show_window(&ctx, &mut self.settings_open);
        self.about_window(&ctx);
        self.confirm_start_dialog(&ctx);
        self.analysis_dialog(&ctx);
    }
}

/// A navigation entry drawn as a raised card: a large icon over a short label.
fn navigation_tile(ui: &mut egui::Ui, screen: Screen, selected: bool) -> egui::Response {
    let size = egui::vec2(ui.available_width(), 64.0);
    let (rect, response) = ui.allocate_exact_size(size, egui::Sense::click());
    response.widget_info(|| {
        egui::WidgetInfo::selected(egui::Role::Tab, true, selected, screen.title())
    });

    if ui.is_rect_visible(rect) {
        let card = rect.shrink2(egui::vec2(6.0, 1.0));
        let visuals = ui.visuals();
        let (fill, text_color) = if selected {
            (visuals.selection.bg_fill, visuals.selection.stroke.color)
        } else if response.hovered() {
            (
                visuals.widgets.hovered.weak_bg_fill,
                visuals.strong_text_color(),
            )
        } else {
            (visuals.widgets.inactive.weak_bg_fill, visuals.text_color())
        };
        let shadow = egui::Shadow {
            offset: [0, 2],
            blur: 8,
            spread: 0,
            color: egui::Color32::from_black_alpha(110),
        };
        let painter = ui.painter();
        painter.add(shadow.as_shape(card, 8));
        painter.rect_filled(card, 8, fill);
        painter.text(
            card.center_top() + egui::vec2(0.0, 23.0),
            egui::Align2::CENTER_CENTER,
            screen.icon(),
            icons::font(28.0),
            text_color,
        );
        painter.text(
            card.center_bottom() - egui::vec2(0.0, 12.0),
            egui::Align2::CENTER_CENTER,
            screen.title(),
            egui::FontId::proportional(12.5),
            text_color,
        );
    }
    response
}
