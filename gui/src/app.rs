use std::path::PathBuf;

use eframe::egui;

use crate::cli::{self, AnalysisJob, JobUpdate};
use crate::icons;
use crate::range_bar::{self, AnalysisRange, Load};
use crate::settings::Settings;
use crate::update::{InstallKind, Updates};
use crate::views::overview::{self, OverviewAction};
use crate::views::{Screen, bitrate, timeline::TimelineView};

mod state;

#[cfg(test)]
use state::CHECKING_CLI;
use state::{AnalysisSession, AnalysisState, CliState, Running};

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

pub struct App {
    /// The CLI: being checked, usable, or why it is not.
    cli: CliState,
    /// Where the CLI was looked up, and through which setting.
    cli_location: (cli::Source, PathBuf),
    input: Option<PathBuf>,
    file_size: u64,
    /// Why the opened file cannot be analyzed, if it cannot.
    file_problem: Option<String>,
    range: AnalysisRange,
    /// Start was pressed for a range large enough to ask first.
    confirm_start: bool,
    /// An analysis waiting for the CLI or running.
    analysis: AnalysisState,
    /// The last completed analysis; the screens show an empty state until then.
    session: Option<AnalysisSession>,
    /// Counts accepted analyses, so each session has its own generation.
    generations: u64,
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
        app.startup_update_check(&cc.egui_ctx);
        app
    }

    /// The startup check, when the setting is on: it opens the update window
    /// once for a newer version that was not skipped.
    fn startup_update_check(&mut self, ctx: &egui::Context) {
        if self.settings.check_updates_at_startup {
            self.updates.check(ctx, false);
        }
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
            cli: CliState::NotChecked,
            cli_location,
            input: None,
            file_size: 0,
            file_problem: None,
            range: AnalysisRange {
                offset: 0,
                limit: INITIAL_LIMIT,
            },
            confirm_start: false,
            analysis: AnalysisState::Idle,
            session: None,
            generations: 0,
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

impl App {
    /// Looks the CLI up and checks its capabilities on a thread; Start stays
    /// disabled until the result is in.
    fn begin_cli_check(&mut self, ctx: Option<egui::Context>) {
        self.cli_location = cli::locate(&self.settings.cli_path);
        self.check_cli_at(self.cli_location.1.clone(), ctx);
    }

    fn check_cli_at(&mut self, path: PathBuf, ctx: Option<egui::Context>) {
        let (sender, receiver) = std::sync::mpsc::channel();
        std::thread::spawn(move || {
            let _ = sender.send(cli::check(&path));
            if let Some(ctx) = ctx {
                ctx.request_repaint();
            }
        });
        self.cli = CliState::Checking(receiver);
    }

    /// Replaces the input. An analysis still running for the previous file is
    /// stopped, so its result cannot be shown for the new one.
    fn open(&mut self, path: PathBuf) {
        self.analysis = AnalysisState::Idle;
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
        self.session = None;
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
        if self.file_problem.is_none() {
            self.analysis = AnalysisState::AwaitingCli;
        }
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
        let (Some(cli), Some(input)) = (self.cli.ready(), &self.input) else {
            return;
        };
        let repaint = {
            let ctx = ctx.clone();
            move || ctx.request_repaint()
        };
        match AnalysisJob::start(cli, input, self.range.offset, self.range.limit, repaint) {
            Ok(job) => {
                self.analysis = AnalysisState::Running(Box::new(Running {
                    job,
                    input: input.clone(),
                    range: (self.range.offset, self.range.limit),
                    started: std::time::Instant::now(),
                    progress: None,
                }));
            }
            Err(err) => self.error = Some(err),
        }
    }

    /// Starts the command-line range once the CLI check has finished; a CLI
    /// that failed the check starts nothing.
    fn start_awaited(&mut self, ctx: &egui::Context) {
        if matches!(self.analysis, AnalysisState::AwaitingCli) && !self.cli.is_checking() {
            self.analysis = AnalysisState::Idle;
            if self.file_problem.is_none() {
                self.start(ctx);
            }
        }
    }

    /// Stops the running analysis. The previous result, if any, stays as it
    /// was.
    fn cancel_analysis(&mut self) {
        if let AnalysisState::Running(running) = &self.analysis {
            running.job.cancel();
        }
        self.analysis = AnalysisState::Idle;
    }

    /// Takes in the analysis result once the CLI has finished.
    fn poll_analysis(&mut self) {
        let AnalysisState::Running(running) = &mut self.analysis else {
            return;
        };
        match running.job.poll() {
            JobUpdate::Running(progress) => running.progress = progress.or(running.progress.take()),
            JobUpdate::Finished(Ok(report)) if report.status == "failed" => {
                self.error = Some(crate::report::failure_message(&report));
                self.analysis = AnalysisState::Idle;
            }
            JobUpdate::Finished(Ok(report)) => {
                self.program = self.program.min(report.programs.len().saturating_sub(1));
                self.generations += 1;
                // Packets takes the paging in Settings now and keeps it for this analysis.
                let paging = self.settings.paging();
                self.session = Some(AnalysisSession::new(
                    report,
                    running,
                    paging,
                    self.generations,
                ));
                self.analysis = AnalysisState::Idle;
            }
            JobUpdate::Finished(Err(err)) => {
                self.error = Some(err);
                self.analysis = AnalysisState::Idle;
            }
        }
    }

    /// Progress dialog over a grayed-out window while the range is analyzed.
    fn analysis_dialog(&mut self, ctx: &egui::Context) {
        let Some(running) = self.analysis.running() else {
            return;
        };
        let elapsed = running.started.elapsed();
        let (offset, limit) = running.range;
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
            self.cancel_analysis();
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
            let changed = self.session.as_ref().map(|s| s.range)
                != Some((self.range.offset, self.range.limit));
            let why_not = match (self.cli.status(), &self.file_problem) {
                (Err(err), _) => Some(err),
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
                // Overview again while it is shown: back to its summary.
                if screen == Screen::Overview && self.screen == Screen::Overview {
                    self.overview.show_summary();
                }
                self.screen = screen;
            }
        }
        ui.with_layout(egui::Layout::bottom_up(egui::Align::Center), |ui| {
            ui.add_space(6.0);
            if rail_button(ui, icons::INFO, "About", "About").clicked() {
                self.about_open = true;
            }
            if rail_button(ui, icons::GEAR, "Settings", "Settings").clicked() {
                self.settings_open = true;
            }
            if update_button(ui, self.updates.available_version()).clicked() {
                self.updates.open(ui.ctx());
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
            self.updates.open(ctx);
        }
    }
}

#[cfg(test)]
impl App {
    /// The completed analysis, for tests that change what it holds.
    fn session_mut(&mut self) -> &mut AnalysisSession {
        self.session.as_mut().expect("an analysis")
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

        self.cli.poll();
        self.updates
            .poll(ui.ctx(), self.settings.skipped_update_version.as_deref());
        self.start_awaited(ui.ctx());
        self.poll_analysis();
        egui::Panel::top("top_bar").show(ui, |ui| self.top_bar(ui));
        egui::Panel::bottom("status").show(ui, |ui| {
            ui.horizontal(|ui| {
                match &self.session {
                    Some(session) => ui.weak(format!(
                        "Analyzed {} – {} · {}-byte packets",
                        range_bar::bytes(session.range.0),
                        range_bar::bytes(session.range.0 + session.range.1),
                        session.report.input.packet_size_bytes.unwrap_or(188)
                    )),
                    None => ui.weak("Not analyzed yet"),
                };
                ui.separator();
                match self.cli.status() {
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
            let mut open_packet = None;
            let navigate = match &mut self.session {
                Some(session) => {
                    let (report, plots, generation) =
                        (&session.report, &session.plots, session.generation);
                    match self.screen {
                        Screen::Overview => match overview::show_report(
                            ui,
                            report,
                            &mut self.program,
                            &mut self.overview,
                            &plots.timelines,
                            generation,
                        ) {
                            Some(OverviewAction::Open(screen)) => Some(screen),
                            Some(OverviewAction::OpenPacket(offset)) => {
                                open_packet = Some(offset);
                                Some(Screen::Packets)
                            }
                            Some(OverviewAction::ShowOnTimeline(offset)) => {
                                let shown =
                                    plots.timelines.get(self.program).is_some_and(|data| {
                                        self.timeline.show_event(
                                            generation,
                                            self.program,
                                            data,
                                            offset,
                                        )
                                    });
                                shown.then_some(Screen::Timeline)
                            }
                            None => None,
                        },
                        Screen::Timeline => self.timeline.show_report(
                            ui,
                            report,
                            &plots.timelines,
                            generation,
                            &mut self.program,
                        ),
                        Screen::Bitrate => {
                            bitrate::show_report(
                                ui,
                                report,
                                &plots.bitrates,
                                generation,
                                &mut self.program,
                            );
                            None
                        }
                        Screen::Packets => {
                            match self.cli.status() {
                                Ok(cli) if cli.packet_outputs => {
                                    session.packet_pages.show(ui, cli)
                                }
                                Ok(_) => empty_state(
                                    ui,
                                    "Packets are not available",
                                    "This CLI cannot page packets (--output packets). Choose a newer CLI in Settings.",
                                ),
                                Err(err) => empty_state(ui, "Packets are not available", err),
                            }
                            None
                        }
                    }
                }
                None => {
                    // Say what actually blocks Start: the CLI, the file, or nothing.
                    let hint = match (self.cli.status(), &self.input, &self.file_problem) {
                        (Err(err), _, _) => err,
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
            if let (Some(offset), Some(session)) = (
                open_packet.or(self.timeline.take_open_request()),
                &mut self.session,
            ) {
                session.packet_pages.go_to(offset);
            }
        });

        let ctx = ui.ctx().clone();
        let status = crate::settings::CliStatus {
            source: self.cli_location.0,
            path: &self.cli_location.1,
            check: self.cli.status(),
        };
        if self
            .settings
            .show_window(&ctx, &mut self.settings_open, &status)
        {
            self.begin_cli_check(Some(ctx.clone()));
        }
        self.about_window(&ctx);
        self.updates.window(
            &ctx,
            self.analysis.is_running(),
            &mut self.settings.skipped_update_version,
        );
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

/// A button of the rail's bottom group, drawn like the navigation tiles: a
/// large icon over a short `title`. `name` is its accessible name and tooltip.
fn rail_button(ui: &mut egui::Ui, glyph: &str, title: &str, name: &str) -> egui::Response {
    let response = rail_tile(ui, glyph, title, false);
    response.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Button, true, name));
    response.on_hover_text(name)
}

/// The rail's update button: "Check for Updates", or, once a check has found
/// an update, its version with a badge dot on the icon.
fn update_button(
    ui: &mut egui::Ui,
    available: Option<&cargo_packager_updater::semver::Version>,
) -> egui::Response {
    let name = match available {
        Some(version) => format!("Update available: {version}"),
        None => "Check for Updates".to_owned(),
    };
    let title = if available.is_some() {
        "Update"
    } else {
        "Updates"
    };
    let response = rail_button(ui, icons::ARROWS_CLOCKWISE, title, &name);
    if available.is_some() && ui.is_rect_visible(response.rect) {
        // On the icon's upper right, as on an app icon.
        let center = response.rect.center_top() + egui::vec2(14.0, 10.0);
        let painter = ui.painter();
        painter.circle(
            center,
            4.5,
            crate::theme::START_COLOR,
            egui::Stroke::new(1.5, ui.visuals().panel_fill),
        );
    }
    response
}

/// A navigation entry drawn as a raised card: a large icon over a short label.
fn navigation_tile(ui: &mut egui::Ui, screen: Screen, selected: bool) -> egui::Response {
    let response = rail_tile(ui, screen.icon(), screen.title(), selected);
    response.widget_info(|| {
        egui::WidgetInfo::selected(egui::Role::Tab, true, selected, screen.title())
    });
    response
}

/// A raised card of the rail: a large icon over a short label.
fn rail_tile(ui: &mut egui::Ui, glyph: &str, title: &str, selected: bool) -> egui::Response {
    let size = egui::vec2(ui.available_width(), 64.0);
    let (rect, response) = ui.allocate_exact_size(size, egui::Sense::click());

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
        if response.has_focus() {
            painter.rect_stroke(card, 8, visuals.selection.stroke, egui::StrokeKind::Outside);
        }
        painter.text(
            card.center_top() + egui::vec2(0.0, 23.0),
            egui::Align2::CENTER_CENTER,
            glyph,
            icons::font(28.0),
            text_color,
        );
        painter.text(
            card.center_bottom() - egui::vec2(0.0, 12.0),
            egui::Align2::CENTER_CENTER,
            title,
            egui::FontId::proportional(12.5),
            text_color,
        );
    }
    response
}

#[cfg(test)]
mod plot_alloc_tests;
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
            cli: CliState::Ready(cli),
            ..App::default()
        };
        app.input = Some(dir.join("a.ts"));
        app.analysis = AnalysisState::Running(Box::new(Running {
            job,
            input: dir.join("a.ts"),
            range: (0, 1000),
            started: std::time::Instant::now(),
            progress: None,
        }));
        app.open(dir.join("b.ts"));
        assert!(matches!(app.analysis, AnalysisState::Idle) && app.session.is_none());
        assert!(
            cli::wait_until_exited(&child),
            "the old analysis kept running"
        );
        app.poll_analysis();
        assert!(app.session.is_none());
    }

    #[test]
    fn checks_the_cli_off_the_ui_thread() {
        let mut app = App::default();
        app.check_cli_at(PathBuf::from("/no/such/mpeg-ts-analyzer"), None);
        assert!(app.cli.is_checking());
        assert_eq!(app.cli.status().err(), Some(CHECKING_CLI));
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(10);
        while app.cli.is_checking() && std::time::Instant::now() < deadline {
            std::thread::sleep(std::time::Duration::from_millis(10));
            app.cli.poll();
        }
        let CliState::Unusable(err) = &app.cli else {
            panic!("a missing CLI was accepted")
        };
        assert!(err.contains("Cannot run"), "{err}");
    }

    /// Polls until the running analysis has finished, or ten seconds pass.
    fn finish(app: &mut App) {
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(10);
        while app.analysis.is_running() && std::time::Instant::now() < deadline {
            std::thread::sleep(std::time::Duration::from_millis(10));
            app.poll_analysis();
        }
        assert!(!app.analysis.is_running(), "the analysis did not finish");
    }

    /// A fake CLI that prints the two-program analysis fixture.
    fn fixture_cli(dir: &std::path::Path) -> cli::Cli {
        let document = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("tests/fixtures/analysis_two_programs_188.json");
        cli::fake_cli(dir, &format!("cat '{}'", document.display()))
    }

    fn temp_dir(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("mtsa-gui-test-{name}-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    /// Each accepted analysis replaces the session as a whole, with its own
    /// range and a new generation; a failed or cancelled one keeps it.
    #[test]
    fn analyses_replace_the_session_and_failures_keep_it() {
        let dir = temp_dir("session");
        let ctx = egui::Context::default();
        let mut app = App {
            cli: CliState::Ready(fixture_cli(&dir)),
            ..App::default()
        };
        app.input = Some(dir.join("a.ts"));
        app.range = AnalysisRange {
            offset: 0,
            limit: 1000,
        };
        app.start(&ctx);
        assert!(app.analysis.is_running() && app.session.is_none());
        finish(&mut app);
        let session = app.session.as_ref().expect("a session");
        assert_eq!((session.range, session.generation), ((0, 1000), 1));
        assert_eq!(session.plots.timelines.len(), session.report.programs.len());
        assert_eq!(session.plots.bitrates.len(), session.report.programs.len());

        app.range = AnalysisRange {
            offset: 188,
            limit: 2000,
        };
        app.start(&ctx);
        finish(&mut app);
        let session = app.session.as_ref().expect("a session");
        assert_eq!((session.range, session.generation), ((188, 2000), 2));

        // A CLI that fails: an error, and the last analysis stays.
        app.cli = CliState::Ready(cli::fake_cli(&dir, "echo broken >&2\nexit 1"));
        app.start(&ctx);
        finish(&mut app);
        assert!(app.error.take().is_some_and(|e| e.contains("broken")));
        assert_eq!(app.session.as_ref().map(|s| s.generation), Some(2));

        // Cancel: the CLI is stopped, and the last analysis stays.
        app.cli = CliState::Ready(cli::fake_cli(&dir, "exec sleep 30"));
        app.start(&ctx);
        let AnalysisState::Running(running) = &app.analysis else {
            panic!("not running")
        };
        let child = running.job.child();
        app.cancel_analysis();
        assert!(matches!(app.analysis, AnalysisState::Idle));
        assert!(cli::wait_until_exited(&child), "the analysis kept running");
        assert_eq!(app.session.as_ref().map(|s| s.generation), Some(2));
        assert!(app.error.is_none());
    }

    /// A command-line range starts once the CLI check has passed, and not at
    /// all when it failed.
    #[test]
    fn automatic_launch_starts_after_the_cli_check() {
        let sample = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts");
        let dir = temp_dir("launch");
        let ctx = egui::Context::default();
        for passes in [false, true] {
            let mut app = App::without_cli_check(Settings::default());
            app.apply_launch(LaunchRequest {
                input: sample.clone(),
                offset: 0,
                limit: None,
            });
            let (sender, receiver) = std::sync::mpsc::channel();
            app.cli = CliState::Checking(receiver);
            app.start_awaited(&ctx);
            assert!(matches!(app.analysis, AnalysisState::AwaitingCli));
            sender
                .send(if passes {
                    Ok(fixture_cli(&dir))
                } else {
                    Err("Cannot run the CLI".to_owned())
                })
                .unwrap();
            app.cli.poll();
            app.start_awaited(&ctx);
            assert_eq!(app.analysis.is_running(), passes);
            if passes {
                finish(&mut app);
                assert!(app.session.is_some());
            } else {
                assert!(matches!(app.analysis, AnalysisState::Idle));
            }
        }
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
        assert!(matches!(app.analysis, AnalysisState::AwaitingCli));
    }
}
