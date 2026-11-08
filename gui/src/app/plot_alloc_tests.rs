//! Heap allocation per frame of the plot screens (#154): long, multi-program
//! series must be drawn from the derived plot data without copying every
//! point vector on each frame.
//!
//! Allocations are counted per thread by the counting global allocator
//! ([`super::heap`]), so tests running in parallel do not disturb each other:
//! the egui_kittest harness builds frames on the calling thread.

use std::time::Instant;

use eframe::egui;
use egui_kittest::Harness;

use super::App;
use super::heap::{WATCHED, counted, watching};
use super::ui_tests::app_with_report;
use crate::plot_data;
use crate::report::{
    AnalysisReport, BitrateWindowPid, BitrateWindows, ContinuityEvent, JitterPoint, PcrPoint,
    PesPoint, TimestampEvent,
};
use crate::views::Screen;

/// PCRs (and jitter samples) per program: 40 ms apart, 20 minutes.
const PCRS: usize = 30_000;
/// PES packets per elementary stream.
const PES_PER_PID: usize = 30_000;
/// 1 s bitrate windows.
const WINDOWS: usize = 1_200;
/// Timestamp events per program, and as many continuity events.
const EVENTS: usize = 1_037;

/// The two-program fixture with each program's series replaced by long ones,
/// so every drawn series is decimated to `MAX_PLOT_POINTS`.
fn long_report() -> AnalysisReport {
    let mut report: AnalysisReport = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_two_programs_188.json"
    ))
    .expect("analysis fixture");
    let mut continuity = Vec::new();
    for program in &mut report.programs {
        let series = program.series.as_mut().expect("fixture series");
        let pids: Vec<String> = program
            .elementary_streams
            .iter()
            .map(|es| es.pid.clone())
            .collect();
        // Offsets stay inside the fixture's file, a few bytes apart.
        let offset = |i: usize| (i * 6) as u64;
        series.pcr = (0..PCRS)
            .map(|i| PcrPoint {
                offset_bytes: offset(i),
                elapsed_ms: i as f64 * 40.0,
                pcr_ms: i as f64 * 40.0,
                interval_ms: (i > 0).then_some(40.0 + (i % 997) as f64 * 0.1),
                discontinuity: false,
            })
            .collect();
        series.pes = (0..PES_PER_PID)
            .flat_map(|i| {
                pids.iter().map(move |pid| PesPoint {
                    pid: pid.clone(),
                    offset_bytes: offset(i),
                    elapsed_ms: Some(i as f64 * 40.0),
                    pts_ms: Some(i as f64 * 40.0 + 600.0),
                    dts_ms: None,
                    pcr_gap_ms: Some(600.0 + (i % 101) as f64),
                })
            })
            .collect();
        series.pcr_jitter = (1..PCRS)
            .map(|i| JitterPoint {
                offset_bytes: offset(i),
                jitter_ns: ((i % 211) as f64 - 105.0) * 1_000.0,
            })
            .collect();
        series.timestamp_events = (0..EVENTS)
            .map(|i| TimestampEvent {
                pid: pids[0].clone(),
                offset_bytes: offset(i * PCRS / EVENTS),
                kind: "pts_jump".to_owned(),
                message: "PTS jump".to_owned(),
            })
            .collect();
        let windows = &series.bitrate;
        let labels: Vec<(String, String)> = windows
            .pids
            .iter()
            .map(|p| (p.pid.clone(), p.label.clone()))
            .collect();
        series.bitrate = BitrateWindows {
            window_seconds: 1,
            duration_seconds: vec![1.0; WINDOWS],
            pids: labels
                .into_iter()
                .enumerate()
                .map(|(n, (pid, label))| BitrateWindowPid {
                    pid,
                    label,
                    bytes: (0..WINDOWS)
                        .map(|w| 10_000 + (w * 7 + n) as u64 % 500)
                        .collect(),
                    bps: (0..WINDOWS)
                        .map(|w| 80_000.0 + ((w * 7 + n) % 500) as f64 * 8.0)
                        .collect(),
                })
                .collect(),
        };
        continuity.extend((0..EVENTS).map(|i| ContinuityEvent {
            offset_bytes: offset(i * PCRS / EVENTS + 1),
            pid: pids[1].clone(),
            expected_cc: 1,
            actual_cc: 3,
        }));
    }
    report.continuity_events = continuity;
    report
}

fn harness(app: App) -> Harness<'static, App> {
    let mut harness = Harness::builder()
        .with_size(egui::vec2(1280.0, 800.0))
        .with_context_setup(crate::theme::apply)
        .build_ui_state(|ui, app: &mut App| app.show(ui), app);
    harness.run();
    harness
}

/// Frames measured per screen, after a few to settle layout and plot memory.
const FRAMES: u64 = 10;

/// What one frame of `screen` costs: allocations, bytes, and allocations
/// the size of a copy of one of the series it draws.
struct Frame {
    allocations: u64,
    bytes: u64,
    series_copies: u64,
}

fn per_frame(report: AnalysisReport, screen: Screen) -> Frame {
    // Byte sizes a per-frame copy of a drawn series would allocate: its
    // coordinates as two `f64`s, or the events as picking points. Short
    // series are left out: their sizes are too common to tell apart.
    let program = &report.programs[0];
    let timeline = plot_data::timeline(&report, program);
    let bitrate = plot_data::bitrate(&report, program);
    let lengths = [
        timeline.pcr_drawn.len(),
        timeline.pcr_over_limit.len(),
        bitrate.program_drawn.len(),
        bitrate.jitter_drawn.len(),
    ]
    .into_iter()
    .chain(
        timeline
            .gaps
            .iter()
            .flat_map(|g| g.drawn.iter().map(Vec::len)),
    )
    .chain(bitrate.pids.iter().map(|p| p.drawn.len()));
    let events = timeline.events.len();
    let mut sizes: Vec<usize> = lengths
        .chain([events])
        .filter(|&n| n >= 1_000)
        .map(|n| n * size_of::<[f64; 2]>())
        .collect();
    if events >= 1_000 {
        sizes.push(events * size_of::<plot_data::Point>());
    }
    sizes.sort_unstable();
    sizes.dedup();
    assert!(sizes.len() <= WATCHED);

    let mut app = app_with_report(report);
    app.screen = screen;
    let mut harness = harness(app);
    for _ in 0..3 {
        harness.step();
    }
    let mut counts = (0, 0);
    let series_copies = watching(&sizes, || {
        counts = counted(|| {
            for _ in 0..FRAMES {
                harness.step();
            }
        });
    });
    Frame {
        allocations: counts.0 / FRAMES,
        bytes: counts.1 / FRAMES,
        series_copies,
    }
}

fn report_frames(report: impl Fn() -> AnalysisReport) -> Vec<(Screen, Frame)> {
    let report_once = report();
    let started = Instant::now();
    let (_, derive_bytes) = counted(|| {
        for program in &report_once.programs {
            drop(plot_data::timeline(&report_once, program));
            drop(plot_data::bitrate(&report_once, program));
        }
    });
    eprintln!(
        "plot derivation for {} programs: {:?}, {} KiB allocated",
        report_once.programs.len(),
        started.elapsed(),
        derive_bytes / 1024
    );
    [Screen::Timeline, Screen::Bitrate]
        .into_iter()
        .map(|screen| {
            let frame = per_frame(report(), screen);
            eprintln!(
                "{screen:?}: {} allocations, {} KiB, {} series-sized allocations per frame",
                frame.allocations,
                frame.bytes / 1024,
                frame.series_copies as f64 / FRAMES as f64
            );
            (screen, frame)
        })
        .collect()
}

/// Run with `-- --nocapture` for the numbers.
#[test]
fn plot_screens_do_not_copy_series_per_frame() {
    let report = long_report();
    let timeline = plot_data::timeline(&report, &report.programs[0]);
    // Long enough for the events and every drawn line to be watched.
    assert!(timeline.events.len() >= 1_000 && timeline.pcr_drawn.len() >= 1_000);
    for (screen, frame) in report_frames(long_report) {
        assert_eq!(frame.series_copies, 0, "{screen:?} copies drawn series");
    }
}

/// The same measurement for a real analysis document:
/// `MTSA_PLOT_REPORT=analysis.json cargo test plot_screens -- --ignored --nocapture`.
#[test]
#[ignore = "needs MTSA_PLOT_REPORT"]
fn plot_screens_of_a_real_analysis() {
    let path = std::env::var_os("MTSA_PLOT_REPORT").expect("MTSA_PLOT_REPORT");
    let bytes = std::fs::read(path).expect("analysis document");
    report_frames(|| crate::report::parse_analysis(&bytes, "2").expect("analysis document"));
}
