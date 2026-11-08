//! The GUI's memory profile (#154, docs/gui/memory-profile.md): peak and
//! retained heap of the app in the scenarios that decide how much an analysis
//! keeps, run against the real CLI and generated inputs.
//!
//! It reads the process-wide counters of the counting allocator
//! ([`super::heap`]), so it must run alone; `gui/scripts/memory-profile.sh`
//! generates the inputs and runs it with `--test-threads=1`. It prints the
//! measurements and asserts nothing about their size: absolute heap sizes
//! depend on the platform, the CLI and the inputs.

use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use eframe::egui;
use egui_kittest::Harness;

use super::heap;
use super::state::{AnalysisState, CliState};
use super::{App, INITIAL_LIMIT};
use crate::cli;
use crate::range_bar::AnalysisRange;
use crate::views::Screen;

const MIB: f64 = 1024.0 * 1024.0;

/// How long one scenario step may take before the profile gives up.
const TIMEOUT: Duration = Duration::from_secs(600);

/// Packets screens visited in the paging scenario.
const PAGE_VISITS: u64 = 40;

fn env_path(name: &str) -> PathBuf {
    std::env::var_os(name)
        .map(PathBuf::from)
        .unwrap_or_else(|| panic!("{name} is not set; run gui/scripts/memory-profile.sh"))
}

fn harness(app: App) -> Harness<'static, App> {
    Harness::builder()
        .with_size(egui::vec2(1280.0, 800.0))
        .with_context_setup(crate::theme::apply)
        .build_ui_state(|ui, app: &mut App| app.show(ui), app)
}

/// Runs frames until `done` holds.
fn step_until(harness: &mut Harness<'static, App>, what: &str, done: impl Fn(&App) -> bool) {
    let deadline = Instant::now() + TIMEOUT;
    loop {
        harness.step();
        if done(harness.state()) {
            return;
        }
        assert!(Instant::now() < deadline, "timed out: {what}");
        std::thread::sleep(Duration::from_millis(5));
    }
}

/// Shows each of `screens` for a few frames.
fn visit(harness: &mut Harness<'static, App>, screens: &[Screen]) {
    for &screen in screens {
        harness.state_mut().screen = screen;
        for _ in 0..3 {
            harness.step();
        }
    }
}

/// Opens `input` with the range `limit` (zero: the whole file).
fn open(harness: &mut Harness<'static, App>, input: &Path, limit: u64) {
    let app = harness.state_mut();
    app.open(input.to_path_buf());
    let limit = if limit == 0 { app.file_size } else { limit };
    app.range = AnalysisRange { offset: 0, limit };
}

fn start(harness: &mut Harness<'static, App>) {
    let ctx = harness.ctx.clone();
    harness.state_mut().start(&ctx);
    assert!(
        harness.state().analysis.is_running(),
        "the analysis did not start"
    );
}

/// Analyzes the opened range and waits for its session.
fn analyze(harness: &mut Harness<'static, App>) {
    let generation = harness.state().generations;
    start(harness);
    step_until(harness, "analysis", |app| !app.analysis.is_running());
    assert!(
        harness.state().error.is_none(),
        "{:?}",
        harness.state().error
    );
    assert_eq!(harness.state().generations, generation + 1, "no session");
}

/// One row of the profile: the heap above the idle app's.
struct Row {
    scenario: &'static str,
    /// The highest heap while the step ran.
    peak: usize,
    /// The heap after the step.
    retained: usize,
    /// What the step added (or, negative, freed).
    change: f64,
}

struct Profile {
    baseline: usize,
    rows: Vec<Row>,
}

impl Profile {
    /// Measures `step`: the highest heap while it runs, and what stays after.
    fn measure(
        &mut self,
        harness: &mut Harness<'static, App>,
        scenario: &'static str,
        step: impl FnOnce(&mut Harness<'static, App>),
    ) {
        let before = heap::live();
        heap::reset_peak();
        step(harness);
        let after = heap::live();
        let row = Row {
            scenario,
            peak: heap::peak().saturating_sub(self.baseline),
            retained: after.saturating_sub(self.baseline),
            change: (after as f64 - before as f64) / MIB,
        };
        eprintln!(
            "{:<44} peak {:>8.1} MiB  retained {:>8.1} MiB  change {:>+8.1} MiB",
            row.scenario,
            row.peak as f64 / MIB,
            row.retained as f64 / MIB,
            row.change
        );
        self.rows.push(row);
    }
}

/// `gui/scripts/memory-profile.sh` runs this with the CLI and inputs it
/// names, alone in the test process.
#[test]
#[ignore = "needs the CLI and generated inputs; run gui/scripts/memory-profile.sh"]
fn memory_profile() {
    let cli = cli::check(&env_path("MPEG_TS_ANALYZER_CLI")).expect("a usable CLI");
    let single = env_path("MTSA_PROFILE_SINGLE");
    let multi = env_path("MTSA_PROFILE_MULTI");
    let mut harness = harness(App {
        cli: CliState::Ready(cli),
        ..App::default()
    });
    // The idle app with every screen drawn once: what egui keeps anyway.
    visit(&mut harness, &Screen::ANALYSIS);
    visit(&mut harness, &[Screen::Overview]);
    let mut profile = Profile {
        baseline: heap::live(),
        rows: Vec::new(),
    };
    eprintln!(
        "idle app: {:.1} MiB of heap; rows are relative to it",
        profile.baseline as f64 / MIB
    );
    let plots = [Screen::Overview, Screen::Timeline, Screen::Bitrate];

    // 1. One program, the default 64 MiB range.
    open(&mut harness, &single, INITIAL_LIMIT);
    profile.measure(&mut harness, "default 64 MiB range, 1 program", analyze);
    profile.measure(&mut harness, "  + Overview, Timeline, Bitrate drawn", |h| {
        visit(h, &plots)
    });

    // 2. Packets: many pages across the range, each with a decoded packet.
    profile.measure(&mut harness, "  + Packets, 40 pages visited", |h| {
        visit(h, &[Screen::Packets]);
        step_until(h, "first page", |app| {
            app.session
                .as_ref()
                .is_some_and(|s| s.packet_pages.settled_for_test())
        });
        let packets = INITIAL_LIMIT / 188;
        for visit in 0..PAGE_VISITS {
            let offset = packets * visit / PAGE_VISITS * 188;
            h.state_mut()
                .session
                .as_mut()
                .expect("a session")
                .packet_pages
                .go_to(offset);
            step_until(h, "page", |app| {
                app.session
                    .as_ref()
                    .is_some_and(|s| s.packet_pages.settled_for_test())
            });
        }
        visit(h, &[Screen::Overview]);
    });

    // 3. Cancel an analysis of the whole file half-way; the last session stays.
    profile.measure(
        &mut harness,
        "  + whole file started, cancelled at 50%",
        |h| {
            h.state_mut().range = AnalysisRange {
                offset: 0,
                limit: h.state().file_size,
            };
            start(h);
            step_until(h, "half of the file", |app| match &app.analysis {
                AnalysisState::Running(running) => running
                    .progress
                    .as_ref()
                    .is_some_and(|p| p.processed_bytes * 2 >= p.total_bytes),
                _ => true,
            });
            assert!(
                h.state().analysis.is_running(),
                "finished before the cancel"
            );
            h.state_mut().cancel_analysis();
            // The readers stop at their next read once the CLI is killed.
            let until = Instant::now() + Duration::from_millis(500);
            while Instant::now() < until {
                h.step();
                std::thread::sleep(Duration::from_millis(10));
            }
            assert_eq!(h.state().generations, 1, "the cancelled analysis was kept");
        },
    );

    // 4. A replacement file with two programs, the default range.
    profile.measure(
        &mut harness,
        "open replacement file (session dropped)",
        |h| {
            open(h, &multi, INITIAL_LIMIT);
            h.step();
        },
    );
    profile.measure(&mut harness, "default 64 MiB range, 2 programs", analyze);
    profile.measure(&mut harness, "  + Overview, Timeline, Bitrate drawn", |h| {
        for program in 0..2 {
            h.state_mut().program = program;
            visit(h, &plots);
        }
    });

    // 5. The whole 1.1 GB file, one program.
    profile.measure(&mut harness, "open the large file (session dropped)", |h| {
        open(h, &single, 0);
        h.step();
    });
    profile.measure(&mut harness, "large range (whole file), 1 program", analyze);
    profile.measure(&mut harness, "  + Overview, Timeline, Bitrate drawn", |h| {
        visit(h, &plots)
    });
    profile.measure(&mut harness, "open another file (session dropped)", |h| {
        open(h, &multi, INITIAL_LIMIT);
        h.step();
    });

    eprintln!("\n| Scenario | Peak (MiB) | Retained (MiB) | Change (MiB) |\n|---|---:|---:|---:|");
    for row in &profile.rows {
        eprintln!(
            "| {} | {:.1} | {:.1} | {:+.1} |",
            row.scenario.trim_start(),
            row.peak as f64 / MIB,
            row.retained as f64 / MIB,
            row.change
        );
    }
}
