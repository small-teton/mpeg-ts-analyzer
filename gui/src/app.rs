use std::path::PathBuf;

use eframe::egui;

use crate::cli::{self, AnalysisJob, JobUpdate};
use crate::icons;
use crate::plot_data;
use crate::range_bar::{self, AnalysisRange, Load};
use crate::report::{AnalysisReport, ProgressEvent};
use crate::settings::Settings;
use crate::update::{InstallKind, Updates};
use crate::views::packet_pages::PacketPages;
use crate::views::{Screen, bitrate, overview, timeline::TimelineView};

/// Range selected when a file is opened: small enough to analyze quickly.
const INITIAL_LIMIT: u64 = 64 * 1024 * 1024;

/// A file range supplied on the GUI command line and analyzed after startup.
#[derive(Clone, Debug, PartialEq)]
pub(crate) struct LaunchRequest {
    pub input: PathBuf,
    pub offset: u64,
    /// `None` uses [`INITIAL_LIMIT`]; zero means through the end of the file.
    pub limit: Option<u64>,
}

/// An analysis in progress.
struct Running {
    job: AnalysisJob,
    offset: u64,
    limit: u64,
    started: std::time::Instant,
    progress: Option<ProgressEvent>,
}

/// Plot series of every program of the analysis, derived once per analysis.
struct Plots {
    timelines: Vec<plot_data::Timeline>,
    bitrates: Vec<plot_data::Bitrate>,
}

pub struct App {
    /// The CLI if it passed the capabilities check, or why it did not.
    cli: Result<cli::Cli, String>,
    /// Where the CLI was looked up, and through which setting.
    cli_location: (cli::Source, PathBuf),
    /// The capabilities check in progress; it runs off the UI thread because
    /// any program can be chosen as the CLI.
    cli_check: Option<std::sync::mpsc::Receiver<Result<cli::Cli, String>>>,
    /// Start the command-line range as soon as the asynchronous CLI check passes.
    pending_start: bool,
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
    /// Result of the last completed analysis; the screens show an empty state until then.
    analysis: Option<Box<AnalysisReport>>,
    /// Plot series of `analysis`, derived once rather than every frame.
    plots: Option<Plots>,
    /// Packets of the analyzed range, read from the CLI page by page.
    packet_pages: Option<PacketPages>,
    /// Counts accepted analyses, so views can drop state of an older one.
    analysis_generation: u64,
    /// Message for the error dialog.
    error: Option<String>,
    screen: Screen,
    /// Program shown by Overview, Timeline and Bitrate.
    program: usize,
    overview: overview::OverviewState,
    timeline: TimelineView,
    settings: Settings,
    settings_open: bool,
    about_open: bool,
    /// The in-app update: its check, window and pending install.
    updates: Updates,
}

impl Default for App {
    fn default() -> Self {
        Self::with_settings(Settings::default(), None)
    }
}

impl App {
    /// The app with the settings saved by the previous run, if any.
    pub fn new(cc: &eframe::CreationContext, launch: Option<LaunchRequest>) -> Self {
        let settings = cc
            .storage
            .and_then(|storage| eframe::get_value(storage, crate::settings::STORAGE_KEY))
            .unwrap_or_default();
        let mut app = Self::with_launch(settings, Some(cc.egui_ctx.clone()), launch);
        // Only here, not in the constructors tests use: they stay offline.
        if app.settings.check_updates_at_startup {
            app.updates.check(&cc.egui_ctx, false);
        }
        app
    }

    fn with_settings(settings: Settings, ctx: Option<egui::Context>) -> Self {
        Self::with_launch(settings, ctx, None)
    }

    fn with_launch(
        settings: Settings,
        ctx: Option<egui::Context>,
        launch: Option<LaunchRequest>,
    ) -> Self {
        let mut app = Self::without_cli_check(settings);
        if let Some(launch) = launch {
            app.apply_launch(launch);
        }
        app.begin_cli_check(ctx);
        app
    }

    /// The app before the CLI is checked; Start stays disabled.
    fn without_cli_check(settings: Settings) -> Self {
        let cli_location = cli::locate(&settings.cli_path);
        Self {
            cli: Err(CHECKING_CLI.to_owned()),
            cli_location,
            cli_check: None,
            pending_start: false,
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
            plots: None,
            packet_pages: None,
            analysis_generation: 0,
            error: None,
            screen: Screen::Overview,
            program: 0,
            overview: overview::OverviewState::default(),
            timeline: TimelineView::default(),
            settings,
            settings_open: false,
            about_open: false,
            updates: Updates::new(InstallKind::detect()),
        }
    }
}

const CHECKING_CLI: &str = "Checking the CLI…";

impl App {
    /// Looks the CLI up and checks its capabilities on a thread; Start stays
    /// disabled until the result is in.
    fn begin_cli_check(&mut self, ctx: Option<egui::Context>) {
        self.cli_location = cli::locate(&self.settings.cli_path);
        self.check_cli_at(self.cli_location.1.clone(), ctx);
    }

    fn check_cli_at(&mut self, path: PathBuf, ctx: Option<egui::Context>) {
        self.cli = Err(CHECKING_CLI.to_owned());
        let (sender, receiver) = std::sync::mpsc::channel();
        std::thread::spawn(move || {
            let _ = sender.send(cli::check(&path));
            if let Some(ctx) = ctx {
                ctx.request_repaint();
            }
        });
        self.cli_check = Some(receiver);
    }

    /// Takes in the result of the capabilities check once it has finished.
    fn poll_cli_check(&mut self) {
        if let Some(receiver) = &self.cli_check
            && let Ok(result) = receiver.try_recv()
        {
            self.cli = result;
            self.cli_check = None;
        }
    }

    /// Replaces the input. An analysis still running for the previous file is
    /// stopped, so its result cannot be shown for the new one.
    fn open(&mut self, path: PathBuf) {
        self.running = None;
        self.pending_start = false;
        self.confirm_start = false;
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
        self.plots = None;
        self.packet_pages = None;
        self.input = Some(path);
    }

    fn apply_launch(&mut self, launch: LaunchRequest) {
        self.open(launch.input);
        let offset = launch.offset.min(self.file_size);
        let remaining = self.file_size.saturating_sub(offset);
        let requested = match launch.limit {
            Some(0) => remaining,
            Some(limit) => limit.min(remaining),
            None => INITIAL_LIMIT.min(remaining),
        };
        self.range = AnalysisRange {
            offset,
            limit: requested,
        };
        self.range.clamp(self.file_size);
        self.pending_start = self.file_problem.is_none();
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

    /// Derives the plot series of every program once an analysis arrives.
    fn update_plots(&mut self) {
        let Some(report) = &self.analysis else {
            self.plots = None;
            return;
        };
        if self.plots.is_none() {
            self.plots = Some(Plots {
                timelines: report
                    .programs
                    .iter()
                    .map(|program| plot_data::timeline(report, program))
                    .collect(),
                bitrates: report
                    .programs
                    .iter()
                    .map(|program| plot_data::bitrate(report, program))
                    .collect(),
            });
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
                self.packet_pages = self
                    .input
                    .clone()
                    .map(|input| PacketPages::new(input, &report, running.offset, running.limit));
                self.analysis = Some(report);
                self.plots = None;
                self.analysis_generation += 1;
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
        // Both rows wrap, so a narrow window does not widen the screens below.
        ui.horizontal_wrapped(|ui| {
            if ui
                .button((icons::text(icons::FOLDER_OPEN), "Open…"))
                .clicked()
            {
                self.pick_file();
            }
            match &self.input {
                Some(path) => {
                    let path = path.display().to_string();
                    ui.add(egui::Label::new(egui::RichText::new(&path).monospace()).truncate())
                        .on_hover_text(path);
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
        ui.horizontal_wrapped(|ui| {
            ui.strong("Scan range");
            let controls_width = 470.0;
            let bar_width = (ui.available_width() - controls_width).max(160.0);
            self.range.bar(ui, bar_width, self.file_size, load);
            self.range.fields(ui, self.file_size);
            let changed = self.analyzed != Some((self.range.offset, self.range.limit));
            let why_not = match (&self.cli, &self.file_problem) {
                (Err(err), _) => Some(err.as_str()),
                (Ok(_), Some(problem)) => Some(problem.as_str()),
                (Ok(_), None) if !changed => Some("This range is already analyzed"),
                (Ok(_), None) => None,
            };
            let start = if why_not.is_none() {
                primary_button("Start")
            } else {
                egui::Button::new((icons::text(icons::PLAY), "Start"))
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
            if icon_button(ui, icons::INFO, "About").clicked() {
                self.about_open = true;
            }
            if icon_button(ui, icons::GEAR, "Settings").clicked() {
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
                if ui.add(primary_button("Analyze")).clicked() {
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
        let mut check_updates = false;
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
                ui.add_space(6.0);
                check_updates = ui.button("Check for Updates…").clicked();
            });
        if check_updates {
            self.updates.check(ctx, true);
        }
    }
}

impl eframe::App for App {
    fn save(&mut self, storage: &mut dyn eframe::Storage) {
        eframe::set_value(storage, crate::settings::STORAGE_KEY, &self.settings);
    }

    fn ui(&mut self, ui: &mut egui::Ui, _frame: &mut eframe::Frame) {
        self.show(ui);
    }

    /// Runs a downloaded Windows update installer; eframe has saved the
    /// settings by now.
    fn on_exit(&mut self) {
        self.updates.on_exit();
    }
}

impl App {
    /// Draws the whole window; separate from `eframe::App::ui` so tests can
    /// drive it without a native window.
    fn show(&mut self, ui: &mut egui::Ui) {
        if let Some(path) =
            ui.input(|i| i.raw.dropped_files.first().map(|f| f.path().to_path_buf()))
        {
            self.open(path);
        }

        self.poll_cli_check();
        self.updates.poll(ui.ctx());
        if self.pending_start && self.cli_check.is_none() {
            self.pending_start = false;
            if self.cli.is_ok() && self.file_problem.is_none() {
                self.start(ui.ctx());
            }
        }
        self.poll_analysis();
        self.update_plots();
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
                    _ => ui.weak("Not analyzed yet"),
                };
                ui.separator();
                match &self.cli {
                    Ok(cli) => ui.weak(format!("CLI {} · {}", cli.version, cli.path.display())),
                    Err(err) => ui.colored_label(ui.visuals().warn_fg_color, err),
                };
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    self.updates.status_button(ui);
                });
            });
        });
        egui::Panel::left("navigation")
            .resizable(false)
            .exact_size(96.0)
            .show(ui, |ui| self.navigation(ui));
        egui::CentralPanel::default().show(ui, |ui| {
            let navigate = match (&self.analysis, &self.plots) {
                (Some(report), Some(plots)) => match self.screen {
                    Screen::Overview => {
                        overview::show_report(ui, report, &mut self.program, &mut self.overview)
                    }
                    Screen::Timeline => self.timeline.show_report(
                        ui,
                        report,
                        &plots.timelines,
                        self.analysis_generation,
                        &mut self.program,
                    ),
                    Screen::Bitrate => {
                        bitrate::show_report(
                            ui,
                            report,
                            &plots.bitrates,
                            self.analysis_generation,
                            &mut self.program,
                        );
                        None
                    }
                    Screen::Packets => {
                        match (&self.cli, &mut self.packet_pages) {
                            (Ok(cli), Some(pages)) if cli.packet_outputs => pages.show(ui, cli),
                            (Ok(_), Some(_)) => empty_state(
                                ui,
                                "Packets are not available",
                                "This CLI cannot page packets (--output packets). Choose a newer CLI in Settings.",
                            ),
                            (Err(err), _) => empty_state(ui, "Packets are not available", err),
                            (Ok(_), None) => empty_state(
                                ui,
                                "Packets are not available",
                                "Packet paging was not set up for this analysis. Press Start again.",
                            ),
                        }
                        None
                    }
                },
                _ => {
                    // Say what actually blocks Start: the CLI, the file, or nothing.
                    let hint = match (&self.cli, &self.input, &self.file_problem) {
                        (Err(err), _, _) => err.as_str(),
                        (Ok(_), None, _) => "Open a file, choose the scan range and press Start.",
                        (Ok(_), Some(_), Some(_)) => "This file cannot be analyzed. Open another MPEG-TS file.",
                        (Ok(_), Some(_), None) => "Choose the scan range and press Start.",
                    };
                    empty_state(ui, "Not analyzed yet", hint);
                    None
                }
            };
            if let Some(screen) = navigate {
                self.screen = screen;
            }
            if let (Some(offset), Some(pages)) =
                (self.timeline.take_open_request(), &mut self.packet_pages)
            {
                pages.go_to(offset);
            }
        });

        let ctx = ui.ctx().clone();
        if self.screen == Screen::Overview && self.plots.is_some() {
            if let Some(screen) = self.overview.help_window(&ctx) {
                self.screen = screen;
            }
        } else {
            self.overview.help = None;
        }
        let status = crate::settings::CliStatus {
            source: self.cli_location.0,
            path: &self.cli_location.1,
            check: &self.cli,
        };
        if self
            .settings
            .show_window(&ctx, &mut self.settings_open, &status)
        {
            self.begin_cli_check(Some(ctx.clone()));
        }
        self.about_window(&ctx);
        self.updates.window(&ctx, self.running.is_some());
        self.confirm_start_dialog(&ctx);
        self.analysis_dialog(&ctx);
        self.error_dialog(&ctx);
    }
}

/// What a screen shows when it has nothing to show: a title and what to do.
fn empty_state(ui: &mut egui::Ui, title: &str, hint: &str) {
    ui.vertical_centered(|ui| {
        ui.add_space((ui.available_height() / 3.0).max(24.0));
        ui.label(icons::text(icons::INFO).size(40.0).weak());
        ui.add_space(8.0);
        ui.heading(title);
        ui.weak(hint);
    });
}

/// The button that starts an analysis, filled green so it stands out from the
/// other controls and from the blue scan-range bar next to it. Shown only
/// while it can be pressed: a plain button says there is nothing to start.
fn primary_button(label: &str) -> egui::Button<'static> {
    let color = egui::Color32::WHITE;
    egui::Button::new((
        icons::text(icons::PLAY).color(color),
        egui::RichText::new(label).color(color).strong(),
    ))
    .fill(crate::theme::START_COLOR)
}

/// A frameless icon button; `name` is its tooltip and its accessible name,
/// since the icon glyph alone names nothing.
fn icon_button(ui: &mut egui::Ui, glyph: &str, name: &str) -> egui::Response {
    let button = egui::Button::new(icons::text(glyph).size(20.0)).frame(false);
    let response = ui.add(button).on_hover_text(name);
    response.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Button, true, name));
    response
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

#[cfg(all(test, feature = "screenshots"))]
mod screenshots;
#[cfg(test)]
mod ui_tests;

#[cfg(all(test, unix))]
mod tests {
    use super::*;

    #[test]
    fn opening_another_file_stops_the_running_analysis() {
        let dir = std::env::temp_dir().join(format!("mtsa-gui-test-open-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let cli = cli::fake_cli(&dir, "exec sleep 30");
        let job = AnalysisJob::start(&cli, &dir.join("a.ts"), 0, 1000, || {}).expect("start");
        let child = job.child();
        let mut app = App {
            cli: Ok(cli),
            ..App::default()
        };
        app.input = Some(dir.join("a.ts"));
        app.running = Some(Running {
            job,
            offset: 0,
            limit: 1000,
            started: std::time::Instant::now(),
            progress: None,
        });
        app.open(dir.join("b.ts"));
        assert!(app.running.is_none() && app.analysis.is_none() && app.analyzed.is_none());
        assert!(
            cli::wait_until_exited(&child),
            "the old analysis kept running"
        );
        app.poll_analysis();
        assert!(app.analysis.is_none());
    }

    #[test]
    fn checks_the_cli_off_the_ui_thread() {
        let mut app = App::default();
        app.check_cli_at(PathBuf::from("/no/such/mpeg-ts-analyzer"), None);
        assert_eq!(
            app.cli.as_ref().err().map(String::as_str),
            Some(CHECKING_CLI)
        );
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(10);
        while app.cli_check.is_some() && std::time::Instant::now() < deadline {
            std::thread::sleep(std::time::Duration::from_millis(10));
            app.poll_cli_check();
        }
        let Err(err) = &app.cli else {
            panic!("a missing CLI was accepted")
        };
        assert!(err.contains("Cannot run"), "{err}");
    }

    #[test]
    fn launch_range_is_clamped_and_waits_for_the_cli() {
        let sample = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts");
        let size = std::fs::metadata(&sample).unwrap().len();
        let mut app = App::without_cli_check(Settings::default());
        app.apply_launch(LaunchRequest {
            input: sample,
            offset: 188,
            limit: Some(0),
        });
        assert_eq!(app.range.offset, 188);
        assert_eq!(app.range.limit, size - 188);
        assert!(app.pending_start && app.running.is_none());
    }
}
