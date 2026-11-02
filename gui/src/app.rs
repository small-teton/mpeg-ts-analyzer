use std::path::PathBuf;

use eframe::egui;

use crate::cli::{self, AnalysisJob, JobUpdate};
use crate::icons;
use crate::range_bar::{self, AnalysisRange, Load};
use crate::report::{AnalysisReport, ProgressEvent};
use crate::settings::Settings;
use crate::views::{Screen, bitrate, overview, packets::PacketsView, timeline::TimelineView};

/// Range selected when a file is opened: small enough to analyze quickly.
const INITIAL_LIMIT: u64 = 64 * 1024 * 1024;

/// An analysis in progress.
struct Running {
    job: AnalysisJob,
    offset: u64,
    limit: u64,
    started: std::time::Instant,
    progress: Option<ProgressEvent>,
}

pub struct App {
    /// The CLI if it passed the capabilities check, or why it did not.
    cli: Result<cli::Cli, String>,
    input: Option<PathBuf>,
    file_size: u64,
    /// Why the opened file cannot be analyzed, if it cannot.
    file_problem: Option<String>,
    range: AnalysisRange,
    /// The range of the last started analysis.
    analyzed: Option<(u64, u64)>,
    /// Start was pressed for a range large enough to ask first.
    confirm_start: bool,
    running: Option<Running>,
    /// Result of the last completed analysis; the screens show sample data until then.
    analysis: Option<Box<AnalysisReport>>,
    /// Message for the error dialog.
    error: Option<String>,
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
            cli: cli::check(&cli::locate()),
            input: None,
            file_size: 0,
            file_problem: None,
            range: AnalysisRange {
                offset: 0,
                limit: INITIAL_LIMIT,
            },
            analyzed: None,
            confirm_start: false,
            running: None,
            analysis: None,
            error: None,
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
        (self.file_size, self.file_problem) = match std::fs::metadata(&path) {
            Ok(meta) if AnalysisRange::is_analyzable(meta.len()) => (meta.len(), None),
            Ok(meta) => (
                meta.len(),
                Some("The file is too small to hold a packet".to_owned()),
            ),
            Err(err) => (0, Some(format!("Cannot read the file: {err}"))),
        };
        self.range = AnalysisRange {
            offset: 0,
            limit: INITIAL_LIMIT,
        };
        self.analyzed = None;
        self.analysis = None;
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

    fn start(&mut self, ctx: &egui::Context) {
        let (Ok(cli), Some(input)) = (&self.cli, &self.input) else {
            return;
        };
        let repaint = {
            let ctx = ctx.clone();
            move || ctx.request_repaint()
        };
        match AnalysisJob::start(cli, input, self.range.offset, self.range.limit, repaint) {
            Ok(job) => {
                self.running = Some(Running {
                    job,
                    offset: self.range.offset,
                    limit: self.range.limit,
                    started: std::time::Instant::now(),
                    progress: None,
                });
            }
            Err(err) => self.error = Some(err),
        }
    }

    /// Takes in the analysis result once the CLI has finished.
    fn poll_analysis(&mut self) {
        let Some(running) = &mut self.running else {
            return;
        };
        match running.job.poll() {
            JobUpdate::Running(progress) => running.progress = progress.or(running.progress.take()),
            JobUpdate::Finished(Ok(report)) if report.status == "failed" => {
                self.error = Some(
                    report
                        .error
                        .or(report.reason)
                        .unwrap_or_else(|| "The analysis failed.".to_owned()),
                );
                self.running = None;
            }
            JobUpdate::Finished(Ok(report)) => {
                self.analyzed = Some((running.offset, running.limit));
                self.program = self.program.min(report.programs.len().saturating_sub(1));
                self.analysis = Some(report);
                self.running = None;
            }
            JobUpdate::Finished(Err(err)) => {
                self.error = Some(err);
                self.running = None;
            }
        }
    }

    /// Progress dialog over a grayed-out window while the range is analyzed.
    fn analysis_dialog(&mut self, ctx: &egui::Context) {
        let Some(running) = &self.running else {
            return;
        };
        let elapsed = running.started.elapsed();
        let (offset, limit) = (running.offset, running.limit);
        let progress = running
            .progress
            .as_ref()
            .filter(|p| p.total_bytes > 0)
            .map(|p| (p.processed_bytes, p.total_bytes));
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
            let clock = format!(
                "{:02}:{:02}",
                elapsed.as_secs() / 60,
                elapsed.as_secs() % 60
            );
            match progress {
                Some((processed, total)) => {
                    let fraction = processed as f32 / total as f32;
                    ui.add(egui::ProgressBar::new(fraction).show_percentage());
                    ui.add_space(4.0);
                    ui.weak(format!(
                        "{} of {} · {clock}",
                        range_bar::human_size(processed),
                        range_bar::human_size(total)
                    ));
                }
                None => {
                    ui.horizontal(|ui| {
                        ui.spinner();
                        ui.weak(format!("Starting… {clock}"));
                    });
                }
            }
            ui.add_space(10.0);
            ui.vertical_centered(|ui| {
                if ui.button("Cancel").clicked() {
                    cancel = true;
                }
            });
        });
        if cancel {
            running.job.cancel();
            // The previous result, if any, stays as it was.
            self.running = None;
        }
        // Keeps the elapsed time moving between progress events.
        ctx.request_repaint_after(std::time::Duration::from_millis(250));
    }

    fn error_dialog(&mut self, ctx: &egui::Context) {
        let Some(message) = &self.error else {
            return;
        };
        let mut close = false;
        let modal = egui::Modal::new(egui::Id::unique("error")).show(ctx, |ui| {
            ui.set_width(420.0);
            ui.heading("Analysis failed");
            ui.add_space(4.0);
            ui.label(message);
            ui.add_space(8.0);
            if ui.button("OK").clicked() {
                close = true;
            }
        });
        if close || modal.should_close() {
            self.error = None;
        }
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
                Some(path) => {
                    ui.monospace(path.display().to_string());
                    ui.weak(range_bar::bytes(self.file_size));
                    if let Some(problem) = &self.file_problem {
                        ui.colored_label(ui.visuals().error_fg_color, problem);
                    }
                }
                None => {
                    ui.weak("Open a file or drop it here.");
                }
            }
        });
        if self.input.is_none() {
            ui.add_space(6.0);
            return;
        }
        ui.horizontal(|ui| {
            ui.strong("Scan range");
            let controls_width = 470.0;
            let bar_width = (ui.available_width() - controls_width).max(160.0);
            self.range.bar(ui, bar_width, self.file_size, load);
            self.range.fields(ui, self.file_size);
            let changed = self.analyzed != Some((self.range.offset, self.range.limit));
            let start = egui::Button::new((icons::text(icons::PLAY), "Start"));
            let why_not = match (&self.cli, &self.file_problem) {
                (Err(err), _) => Some(err.as_str()),
                (Ok(_), Some(problem)) => Some(problem.as_str()),
                (Ok(_), None) if !changed => Some("This range is already analyzed"),
                (Ok(_), None) => None,
            };
            if ui
                .add_enabled(why_not.is_none(), start)
                .on_disabled_hover_text(why_not.unwrap_or_default())
                .clicked()
            {
                if load == Load::NeedsConfirmation {
                    self.confirm_start = true;
                } else {
                    self.start(ui.ctx());
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
                    self.start(ctx);
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

        self.poll_analysis();
        egui::Panel::top("top_bar").show(ui, |ui| self.top_bar(ui));
        egui::Panel::bottom("status").show(ui, |ui| {
            ui.horizontal(|ui| {
                match (&self.analysis, self.analyzed) {
                    (Some(report), Some((offset, limit))) => ui.weak(format!(
                        "Analyzed {} – {} · {}-byte packets",
                        range_bar::bytes(offset),
                        range_bar::bytes(offset + limit),
                        report.input.packet_size_bytes.unwrap_or(188)
                    )),
                    _ => ui.weak("Screens show sample data until a range is analyzed."),
                };
                ui.separator();
                match &self.cli {
                    Ok(cli) => ui.weak(format!("CLI {} · {}", cli.version, cli.path.display())),
                    Err(err) => ui.colored_label(ui.visuals().warn_fg_color, err),
                };
            });
        });
        egui::Panel::left("navigation")
            .resizable(false)
            .exact_size(96.0)
            .show(ui, |ui| self.navigation(ui));
        egui::CentralPanel::default().show(ui, |ui| {
            let navigate = match self.screen {
                Screen::Overview => match &self.analysis {
                    Some(report) => overview::show_report(ui, report, &mut self.program),
                    None => overview::show(ui, &mut self.program),
                },
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
        self.error_dialog(&ctx);
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
