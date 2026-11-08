//! Semantic UI tests: the window is driven through its accessibility tree with
//! egui_kittest, without a native window, a file dialog or a CLI process.

use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};

use eframe::egui::{
    self,
    accesskit::{Role, Toggled},
};
use egui_kittest::kittest::{NodeT, Queryable};
use egui_kittest::{Harness, Node};

use super::App;
use super::state::{AnalysisSession, CliState};
use crate::cli::Cli;
use crate::range_bar::AnalysisRange;
use crate::report::{AnalysisReport, PacketDetail, PacketsPage};
use crate::settings::Settings;
use crate::update::{Available, InstallKind, Updates};
use crate::views::Screen;
use crate::views::check_help::Check;
use crate::views::overview::OverviewPage;
use crate::views::packet_pages::PacketPages;

/// A CLI that passed the capabilities check. It is never run.
fn checked_cli() -> Cli {
    Cli {
        path: PathBuf::from("mpeg-ts-analyzer"),
        version: "2.3.0".to_owned(),
        packet_outputs: true,
    }
}

fn sample() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts")
}

fn app(cli: Result<Cli, String>) -> App {
    App {
        cli: cli.into(),
        ..App::without_cli_check(Settings::default())
    }
}

/// The first 40 packets of the two-program sample.
pub(super) fn packets_page() -> PacketsPage {
    serde_json::from_str(include_str!("../../tests/fixtures/packets_page_188.json"))
        .expect("packets fixture")
}

/// The decoded PAT packet at offset 188 of the two-program sample.
pub(super) fn pat_packet_detail() -> PacketDetail {
    serde_json::from_str(include_str!(
        "../../tests/fixtures/packet_detail_pat_188.json"
    ))
    .expect("packet fixture")
}

/// An app showing the two-program analysis fixture, with the Packets screen
/// already holding a page and the decoded PAT packet at offset 188. The input
/// is the relative path the fixture names, so nothing specific to the machine
/// running the tests is shown.
pub(super) fn analyzed_app() -> App {
    let report: AnalysisReport = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_two_programs_188.json"
    ))
    .expect("analysis fixture");
    app_with_report(report)
}

/// An app showing `report`, with the Packets screen loaded as in
/// [`analyzed_app`].
pub(super) fn app_with_report(report: AnalysisReport) -> App {
    let mut app = app(Ok(checked_cli()));
    app.input = Some(PathBuf::from(&report.input.path));
    app.file_size = report.input.file_size_bytes;
    app.range = AnalysisRange {
        offset: 0,
        limit: app.file_size,
    };
    app.generations = 1;
    app.session = Some(AnalysisSession::with_pages(
        Box::new(report),
        (0, app.file_size),
        PacketPages::loaded_for_test(packets_page(), 188, pat_packet_detail()),
        app.generations,
    ));
    app
}

fn harness(app: App) -> Harness<'static, App> {
    harness_sized(app, egui::vec2(1280.0, 800.0))
}

fn harness_sized(app: App, size: egui::Vec2) -> Harness<'static, App> {
    let mut harness = Harness::builder()
        .with_size(size)
        .with_context_setup(crate::theme::apply)
        .build_ui_state(|ui, app: &mut App| app.show(ui), app);
    harness.run();
    harness
}

fn tab<'a>(harness: &'a Harness<'_, App>, screen: Screen) -> Node<'a> {
    harness.get_by_role_and_label(Role::Tab, screen.title())
}

/// egui reports a selected tab or selectable label as toggled on.
fn is_selected(node: &Node<'_>) -> bool {
    node.accesskit_node().toggled() == Some(Toggled::True)
}

/// A field of the decoded packet, which is a selectable button.
fn field<'a>(harness: &'a Harness<'_, App>, name: &'a str) -> Node<'a> {
    harness.get_by_role_and_label(Role::Button, name)
}

/// Clicks a field of the decoded packet, scrolling the detail pane to it first.
fn click_field(harness: &mut Harness<'_, App>, name: &str) {
    field(harness, name).scroll_to_me();
    harness.run();
    field(harness, name).click();
    harness.run();
}

fn start_button<'a>(harness: &'a Harness<'_, App>) -> Node<'a> {
    harness.get_by(|node| {
        node.role() == Role::Button && node.label().is_some_and(|l| l.ends_with("Start"))
    })
}

#[test]
fn navigates_between_screens_before_an_analysis() {
    let mut harness = harness(app(Ok(checked_cli())));
    assert!(is_selected(&tab(&harness, Screen::Overview)));
    harness.get_by_label("Open a file, choose the scan range and press Start.");

    for screen in [
        Screen::Timeline,
        Screen::Bitrate,
        Screen::Packets,
        Screen::Overview,
    ] {
        tab(&harness, screen).click();
        harness.run();
        assert_eq!(harness.state().screen, screen);
        assert!(
            is_selected(&tab(&harness, screen)),
            "{screen:?} tab not selected"
        );
        for other in Screen::ANALYSIS.into_iter().filter(|s| *s != screen) {
            assert!(
                !is_selected(&tab(&harness, other)),
                "{other:?} tab still selected"
            );
        }
        // The empty state, and the status bar saying the same.
        assert_eq!(harness.query_all_by_label("Not analyzed yet").count(), 2);
        harness.get_by_label("Open a file, choose the scan range and press Start.");
    }
}

#[test]
fn empty_state_says_what_blocks_start() {
    // The CLI error is also in the status bar.
    let harness = self::harness(app(Err("Cannot run the CLI".to_owned())));
    assert_eq!(harness.query_all_by_label("Cannot run the CLI").count(), 2);

    let mut opened = app(Ok(checked_cli()));
    opened.open(sample());
    let harness = self::harness(opened);
    harness.get_by_label("Choose the scan range and press Start.");

    let mut unreadable = app(Ok(checked_cli()));
    unreadable.open(PathBuf::from("/no/such/file.ts"));
    let harness = self::harness(unreadable);
    harness.get_by_label("This file cannot be analyzed. Open another MPEG-TS file.");
}

#[test]
fn start_is_enabled_only_for_a_new_range_with_a_working_cli() {
    let mut ready = app(Ok(checked_cli()));
    ready.open(sample());
    let mut harness = harness(ready);
    assert!(!start_button(&harness).accesskit_node().is_disabled());
    for label in ["Offset", "Limit"] {
        harness.get_by(|node| {
            node.role() == Role::SpinButton && node.label().as_deref() == Some(label)
        });
    }

    let analyzed = (0, harness.state().range.limit);
    let session = analyzed_app()
        .session
        .take()
        .map(|session| AnalysisSession {
            range: analyzed,
            ..session
        });
    harness.state_mut().session = session;
    harness.run();
    assert!(
        start_button(&harness).accesskit_node().is_disabled(),
        "an analyzed range can be started again"
    );

    let mut broken = app(Err("Cannot run the CLI".to_owned()));
    broken.open(sample());
    let harness = self::harness(broken);
    assert!(
        start_button(&harness).accesskit_node().is_disabled(),
        "Start without a CLI"
    );
}

#[test]
fn opens_and_closes_settings_and_about() {
    let mut harness = harness(app(Ok(checked_cli())));
    harness
        .get_by_role_and_label(Role::Button, "Settings")
        .click();
    harness.run();
    assert!(harness.state().settings_open);
    harness.get_by_role_and_label(Role::Window, "Settings");
    // Language is not offered as a choice that would do nothing.
    harness.get_by_label("Other languages are not available yet.");
    assert!(harness.query_by_role(Role::ComboBox).is_none());
    harness
        .get_by_role_and_label(Role::Button, "Close window")
        .click();
    harness.run();
    assert!(!harness.state().settings_open);
    assert!(
        harness
            .query_by_role_and_label(Role::Window, "Settings")
            .is_none()
    );

    harness.get_by_role_and_label(Role::Button, "About").click();
    harness.run();
    assert!(harness.state().about_open);
    harness.get_by_label(&format!("GUI {}", env!("CARGO_PKG_VERSION")));
    harness
        .get_by_role_and_label(Role::Button, "Close window")
        .click();
    harness.run();
    assert!(!harness.state().about_open);
}

// The in-app update: the rail's button and the startup prompt. A fake
// checker stands in for GitHub; no test reaches the network.

/// An app whose update checks find `version` (none for `None`), counting
/// the checks in `checks`.
fn app_finding(version: Option<&str>, settings: Settings, checks: &Arc<AtomicUsize>) -> App {
    let version = version.map(str::to_owned);
    let checks = Arc::clone(checks);
    let mut app = App {
        cli: CliState::Ready(checked_cli()),
        ..App::without_cli_check(settings)
    };
    let kind = InstallKind::MacApp(PathBuf::from("/Applications/mpeg-ts-analyzer.app"));
    app.updates = Updates::new(kind).with_checker(move |_, _| {
        checks.fetch_add(1, Ordering::SeqCst);
        Ok(version
            .as_deref()
            .map(|version| Available::for_test(version, None)))
    });
    app
}

/// Runs frames until the update check has finished. The first frame handles
/// a queued click or key press, which is what starts a manual check.
fn finish_update_check(harness: &mut Harness<'_, App>) {
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(10);
    harness.run_steps(1);
    while harness.state().updates.checking() {
        assert!(
            std::time::Instant::now() < deadline,
            "the check did not finish"
        );
        std::thread::sleep(std::time::Duration::from_millis(5));
        harness.run_steps(1);
    }
    harness.run_steps(2);
}

/// Launches `app`: the first frame, then the startup check and its result.
fn launch(app: App) -> Harness<'static, App> {
    let mut harness = harness(app);
    let ctx = harness.ctx.clone();
    harness.state_mut().startup_update_check(&ctx);
    finish_update_check(&mut harness);
    harness
}

fn update_window_is_open(harness: &Harness<'_, App>) -> bool {
    harness
        .query_by_role_and_label(Role::Window, "Software update")
        .is_some()
}

#[test]
fn rail_update_button_checks_and_announces_an_update() {
    let checks = Arc::new(AtomicUsize::new(0));
    let mut harness = harness(app_finding(None, Settings::default(), &checks));
    assert!(!update_window_is_open(&harness));
    // Update, Settings, About from top to bottom of the rail.
    let top = |label: &str| {
        bounds(&harness.get_by_role_and_label(Role::Button, label))
            .min
            .y
    };
    assert!(top("Check for Updates") < top("Settings"));
    assert!(top("Settings") < top("About"));
    // As large as the navigation tabs above them.
    let height = |node: Node<'_>| bounds(&node).height();
    let tab = height(harness.get_by_role_and_label(Role::Tab, "Overview"));
    for label in ["Check for Updates", "Settings", "About"] {
        assert_eq!(
            height(harness.get_by_role_and_label(Role::Button, label)),
            tab
        );
    }
    assert!(
        harness
            .query_by_label_contains("Update available")
            .is_none()
    );

    // A manual check from the keyboard: the window shows its result.
    harness
        .get_by_role_and_label(Role::Button, "Check for Updates")
        .focus();
    harness.run();
    harness.key_press(egui::Key::Enter);
    finish_update_check(&mut harness);
    assert_eq!(checks.load(Ordering::SeqCst), 1);
    assert!(update_window_is_open(&harness));
    harness.get_by_label_contains("is the newest version");
    harness.get_by_label("OK").click();
    harness.run();
    assert!(!update_window_is_open(&harness));

    // Once a check has found an update, the button names it and opens the
    // window without checking again.
    let mut harness = launch(app_finding(Some("9.9.9"), Settings::default(), &checks));
    assert_eq!(checks.load(Ordering::SeqCst), 2);
    harness.get_by_label("Later").click();
    harness.run();
    assert!(harness.query_by_label("Check for Updates").is_none());
    let button = harness.get_by_role_and_label(Role::Button, "Update available: 9.9.9");
    button.hover();
    harness.run();
    // The name, and the same text in its tooltip.
    assert_eq!(
        harness
            .query_all_by_label("Update available: 9.9.9")
            .count(),
        2
    );
    harness
        .get_by_role_and_label(Role::Button, "Update available: 9.9.9")
        .click();
    harness.run();
    assert!(update_window_is_open(&harness));
    assert_eq!(checks.load(Ordering::SeqCst), 2);
}

#[test]
fn about_keeps_check_for_updates() {
    let checks = Arc::new(AtomicUsize::new(0));
    let mut harness = harness(app_finding(None, Settings::default(), &checks));
    harness.get_by_role_and_label(Role::Button, "About").click();
    harness.run();
    harness.get_by_label("Check for Updates…").click();
    finish_update_check(&mut harness);
    assert_eq!(checks.load(Ordering::SeqCst), 1);
    assert!(update_window_is_open(&harness));
}

#[test]
fn startup_check_prompts_once_for_a_new_version() {
    let checks = Arc::new(AtomicUsize::new(0));
    let mut harness = launch(app_finding(Some("9.9.9"), Settings::default(), &checks));
    assert!(update_window_is_open(&harness));
    harness.get_by_label("GUI 9.9.9 is available");
    harness.get_by_label("Install");
    harness.get_by_label("Skip this version");
    // Centered, clear of the rail.
    let window = bounds(&harness.get_by_role_and_label(Role::Window, "Software update"));
    assert!(window.min.x > 96.0, "{window:?} covers the rail");
    assert!(
        (window.center().x - 640.0).abs() < 2.0,
        "{window:?} is not centered"
    );
    // A window, not a modal: the rail still works behind it.
    tab(&harness, Screen::Timeline).click();
    harness.run();
    assert_eq!(harness.state().screen, Screen::Timeline);
    assert!(update_window_is_open(&harness));

    // Later closes it for this launch only.
    let layer = harness
        .ctx
        .layer_id_at(window.center())
        .expect("window layer");
    let saved = egui::AreaState::load(&harness.ctx, layer.id);
    harness.get_by_label("Later").click();
    harness.run();
    assert!(!update_window_is_open(&harness));
    assert_eq!(harness.state().settings.skipped_update_version, None);

    // A position saved by an earlier run, over the rail, does not win when
    // the window opens again.
    let mut moved = saved.expect("window state");
    moved.pivot_pos = Some(egui::pos2(20.0, 120.0));
    harness
        .ctx
        .memory_mut(|memory| memory.areas_mut().set_state(layer, moved));
    harness
        .get_by_role_and_label(Role::Button, "Update available: 9.9.9")
        .click();
    harness.run();
    let window = bounds(&harness.get_by_role_and_label(Role::Window, "Software update"));
    assert!(window.min.x > 96.0, "{window:?} reopened over the rail");
    harness.get_by_label("Later").click();
    harness.run();
    let ctx = harness.ctx.clone();
    harness.state_mut().startup_update_check(&ctx);
    finish_update_check(&mut harness);
    assert!(
        !update_window_is_open(&harness),
        "prompted twice in one launch"
    );

    // The next launch prompts again.
    let harness = launch(app_finding(Some("9.9.9"), Settings::default(), &checks));
    assert!(update_window_is_open(&harness));
}

#[test]
fn a_skipped_version_does_not_prompt_at_startup() {
    let checks = Arc::new(AtomicUsize::new(0));
    let mut harness = launch(app_finding(Some("9.9.9"), Settings::default(), &checks));
    harness.get_by_label("Skip this version").click();
    harness.run();
    assert!(!update_window_is_open(&harness));
    let settings = harness.state().settings.clone();
    assert_eq!(settings.skipped_update_version.as_deref(), Some("9.9.9"));
    harness.get_by_role_and_label(Role::Button, "Update available: 9.9.9");

    // After a restart (the skip is saved with the settings), the startup
    // check stays quiet about 9.9.9 but the rail still announces it.
    let mut harness = launch(app_finding(Some("9.9.9"), settings.clone(), &checks));
    assert!(!update_window_is_open(&harness));
    harness
        .get_by_role_and_label(Role::Button, "Update available: 9.9.9")
        .click();
    harness.run();
    assert!(update_window_is_open(&harness));

    // A manual check ignores the skip.
    let mut harness = self::harness(app_finding(Some("9.9.9"), settings.clone(), &checks));
    harness
        .get_by_role_and_label(Role::Button, "Check for Updates")
        .click();
    finish_update_check(&mut harness);
    assert!(update_window_is_open(&harness));
    harness.get_by_label("GUI 9.9.9 is available");

    // A newer release prompts again.
    let harness = launch(app_finding(Some("9.9.10"), settings, &checks));
    assert!(update_window_is_open(&harness));
    harness.get_by_label("GUI 9.9.10 is available");
}

#[test]
fn startup_check_turned_off_requests_and_shows_nothing() {
    let checks = Arc::new(AtomicUsize::new(0));
    let settings = Settings {
        check_updates_at_startup: false,
        ..Settings::default()
    };
    let harness = launch(app_finding(Some("9.9.9"), settings, &checks));
    assert_eq!(checks.load(Ordering::SeqCst), 0);
    assert!(!update_window_is_open(&harness));
    harness.get_by_role_and_label(Role::Button, "Check for Updates");
}

#[test]
fn analysis_screens_pass_the_accessibility_check() {
    let mut harness = harness(analyzed_app());
    for screen in Screen::ANALYSIS {
        tab(&harness, screen).click();
        harness.run();
        assert_eq!(harness.state().screen, screen);
        assert_eq!(harness.query_all_by_label("Not analyzed yet").count(), 0);
    }
}

#[test]
fn selecting_a_field_shows_its_bytes_and_bits() {
    let mut app = analyzed_app();
    app.screen = Screen::Packets;
    let mut harness = harness(app);
    harness.get_by_label("Click a field to see its bits.");

    // PID: 13 bits over bytes 1 and 2.
    click_field(&mut harness, "PID");
    assert!(is_selected(&field(&harness, "PID")));
    harness.get_by_label("bytes 1–2 selected");
    harness.get_by_label("13 bits · bits 11–23");
    for byte in ["001", "002"] {
        harness.get_by_label(byte);
    }
    assert!(harness.query_by_label("003").is_none());

    // version_number and current_next_indicator share byte 10 of the packet.
    click_field(&mut harness, "version_number");
    harness.get_by_label("5 bits · bits 82–86");
    click_field(&mut harness, "current_next_indicator");
    harness.get_by_label("1 bit · bit 87");
    assert!(is_selected(&field(&harness, "current_next_indicator")));
    assert!(!is_selected(&field(&harness, "version_number")));
    harness.get_by_label("byte 10 selected");

    // Clicking the selected field again clears the selection.
    click_field(&mut harness, "current_next_indicator");
    harness.get_by_label("Click a field to see its bits.");
}

/// A 204-byte packet keeps its 16-byte trailer out of the decoded TS packet:
/// the trailer is listed as a layer of its own, and its bytes can be selected
/// like any field. The page and the video packet at offset 2448 (slot 12) are
/// what the CLI writes for `sample_data/diagnostics/sample_204byte_all_findings.ts`.
#[test]
fn packet_detail_shows_the_trailer_of_a_204_byte_packet() {
    let page: PacketsPage =
        serde_json::from_str(include_str!("../../tests/fixtures/packets_page_204.json")).unwrap();
    let detail: PacketDetail = serde_json::from_str(include_str!(
        "../../tests/fixtures/packet_detail_video_204.json"
    ))
    .unwrap();
    let mut app = analyzed_app();
    app.screen = Screen::Packets;
    app.session_mut().packet_pages = PacketPages::loaded_for_test(page, 2448, detail);
    let mut harness = harness(app);
    harness.get_by_label("Packet #13");
    harness.get_by_label("Trailer (after the TS packet; not decoded)");
    click_field(&mut harness, "trailer_bytes");
    harness.get_by_label("bytes 188–203 selected");
    harness.get_by_label("128 bits · bits 1504–1631");
    // The decoded TS fields stay within the first 188 bytes.
    click_field(&mut harness, "PID");
    harness.get_by_label("bytes 1–2 selected");
}

/// The packet fixtures with a wire layout are what the CLI writes for their
/// sample. (`packet_detail_pat_188.json` is kept as an older CLI wrote it,
/// without `bit_layout`.)
/// Runs only against a CLI named by MPEG_TS_ANALYZER_CLI (e.g. via `make gui-test`).
#[test]
fn packet_fixtures_match_the_cli() {
    const TWO_PROGRAMS_188: &str = "sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts";
    const TWO_PROGRAMS_192: &str = "sample_192byte_two_programs_mpeg2_160x120_25fps_mp2.ts";
    const ALL_FINDINGS_204: &str = "diagnostics/sample_204byte_all_findings.ts";
    for (sample, args, fixture) in [
        (
            ALL_FINDINGS_204,
            ["--output", "packets", "--count", "40"],
            include_str!("../../tests/fixtures/packets_page_204.json"),
        ),
        (
            ALL_FINDINGS_204,
            ["--output", "packet", "--offset", "2448"],
            include_str!("../../tests/fixtures/packet_detail_video_204.json"),
        ),
        (
            TWO_PROGRAMS_188,
            ["--output", "packet", "--offset", "564"],
            include_str!("../../tests/fixtures/packet_detail_pmt_1001_188.json"),
        ),
        (
            TWO_PROGRAMS_192,
            ["--output", "packet", "--offset", "30528"],
            include_str!("../../tests/fixtures/packet_detail_pes_pcr_192.json"),
        ),
    ] {
        let mut all = vec!["--format", "json"];
        all.extend(args);
        let Some(json) = crate::cli::run_on_sample(sample, &all) else {
            return;
        };
        let fixture: serde_json::Value = serde_json::from_str(fixture).unwrap();
        let mut output: serde_json::Value = serde_json::from_slice(&json).unwrap();
        output["input"]["path"] = fixture["input"]["path"].clone();
        assert_eq!(output, fixture, "regenerate the fixture with the CLI");
    }
}

/// The decoded PMT packet of program 2 (PID 0x1001) at offset 564, whose PID
/// has set bits in both bytes it spans.
pub(super) fn pmt_packet_detail() -> PacketDetail {
    serde_json::from_str(include_str!(
        "../../tests/fixtures/packet_detail_pmt_1001_188.json"
    ))
    .expect("packet fixture")
}

/// The left edge of the packet detail pane, where its "This packet" label starts.
fn detail_pane_left(offset: u64, detail: PacketDetail) -> f64 {
    let mut app = analyzed_app();
    app.screen = Screen::Packets;
    app.session_mut().packet_pages = PacketPages::loaded_for_test(packets_page(), offset, detail);
    let harness = harness(app);
    harness
        .get_by_label("This packet")
        .accesskit_node()
        .bounding_box()
        .expect("label bounds")
        .x0
}

/// The PMT packet decodes into rows wider than the pane's default width. They
/// scroll inside the pane instead of widening it over the packet list.
#[test]
fn packet_detail_pane_keeps_its_width_for_every_packet() {
    let pat = detail_pane_left(188, pat_packet_detail());
    let pmt = detail_pane_left(564, pmt_packet_detail());
    assert_eq!(pat, pmt);
}

/// The CLI's analysis of `sample_data/diagnostics/sample_188byte_all_findings.ts`, in
/// which every transport-health check reports a problem.
fn all_findings_report() -> AnalysisReport {
    serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_all_findings_188.json"
    ))
    .expect("all-findings fixture")
}

/// The all-findings report with `edit` applied to its JSON.
fn edited_report(edit: impl FnOnce(&mut serde_json::Value)) -> AnalysisReport {
    let mut document: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_all_findings_188.json"
    ))
    .unwrap();
    edit(&mut document);
    serde_json::from_value(document).unwrap()
}

/// The window sizes the Overview tests use: wide, and narrow enough that
/// check names wrap.
const WIDE: egui::Vec2 = egui::vec2(1280.0, 800.0);
const NARROW: egui::Vec2 = egui::vec2(480.0, 900.0);

fn info_button<'a>(harness: &'a Harness<'_, App>, check: Check) -> Node<'a> {
    let name = check.about();
    harness.get_by(move |node| node.role() == Role::Button && node.label() == Some(name.clone()))
}

/// Opens the detail page of `check` from its information button, with the
/// keyboard when `keyboard` is set.
fn open_detail(harness: &mut Harness<'_, App>, check: Check, keyboard: bool) {
    info_button(harness, check).scroll_to_me();
    harness.run();
    if keyboard {
        info_button(harness, check).focus();
        harness.run();
        harness.key_press(egui::Key::Enter);
    } else {
        info_button(harness, check).click();
    }
    harness.run();
    assert_eq!(
        harness.state().overview.page(),
        OverviewPage::CheckDetail(check)
    );
}

fn back_button<'a>(harness: &'a Harness<'_, App>) -> Node<'a> {
    harness.get_by_role_and_label(Role::Button, "Back to Overview")
}

/// The row of a check in the Transport health table.
fn table_row<'a>(harness: &'a Harness<'_, App>, check: Check) -> Node<'a> {
    let prefix = format!("{}: ", check.title());
    harness.get_by(move |node| {
        node.role() == Role::Row && node.label().is_some_and(|l| l.starts_with(&prefix))
    })
}

fn bounds(node: &Node<'_>) -> egui::Rect {
    let b = node.accesskit_node().bounding_box().expect("bounds");
    egui::Rect::from_min_max(
        egui::pos2(b.x0 as f32, b.y0 as f32),
        egui::pos2(b.x1 as f32, b.y1 as f32),
    )
}

/// The cell widths of a table row, left to right.
fn cell_widths(row: &Node<'_>) -> Vec<f32> {
    row.children()
        .filter(|cell| {
            matches!(
                cell.accesskit_node().role(),
                Role::Cell | Role::ColumnHeader
            )
        })
        .map(|cell| bounds(&cell).width())
        .collect()
}

/// Whether `node` is inside a table, row or cell.
fn in_table(node: &Node<'_>) -> bool {
    std::iter::successors(node.parent(), |n| n.parent()).any(|n| {
        matches!(
            n.accesskit_node().role(),
            Role::Table | Role::Row | Role::Cell | Role::Grid | Role::ColumnHeader
        )
    })
}

/// The content area right of the navigation rail.
fn content_area(size: egui::Vec2) -> egui::Rect {
    egui::Rect::from_min_size(egui::pos2(96.0, 0.0), size - egui::vec2(96.0, 0.0))
}

/// Every check once, in the documented order under its category, at every
/// width; no scope column, badge or accordion.
#[test]
fn transport_health_table_lists_every_check_once_in_order() {
    for size in [WIDE, NARROW] {
        let mut harness = harness_sized(app_with_report(all_findings_report()), size);
        harness.get_by_role_and_label(Role::Heading, "Transport health");
        let table = harness.get_by_role_and_label(Role::Table, "Transport health");
        let headers: Vec<String> = table
            .query_all_by_role(Role::ColumnHeader)
            .map(|cell| {
                cell.query_by_role(Role::Label)
                    .unwrap()
                    .accesskit_node()
                    .value()
                    .unwrap()
            })
            .collect();
        assert_eq!(headers, ["Check", "Value", "Status", "Help"], "{size:?}");
        assert!(harness.query_by_label("Scope").is_none());

        let mut tops = Vec::new();
        for check in Check::ALL {
            assert_eq!(
                harness
                    .query_all_by(|n| n.role() == Role::Row
                        && n.label()
                            .is_some_and(|l| l.starts_with(&format!("{}: ", check.title()))))
                    .count(),
                1,
                "{size:?} {check:?}"
            );
            assert_eq!(
                harness
                    .query_all_by_role_and_label(Role::Button, &check.about())
                    .count(),
                1
            );
            tops.push((
                check.category().title(),
                bounds(&table_row(&harness, check)).top(),
            ));
        }
        // Categories are headings above their rows, in table order.
        let mut previous = f32::MIN;
        for (category, top) in &tops {
            let heading = bounds(&harness.get_by_role_and_label(Role::Heading, category));
            assert!(heading.top() < *top, "{size:?} {category} below its row");
            assert!(*top > previous, "{size:?}: rows out of order");
            previous = *top;
        }
        let categories: Vec<f32> = ["Timing", "PES / Timestamps", "Integrity"]
            .iter()
            .map(|c| bounds(&harness.get_by_role_and_label(Role::Heading, c)).top())
            .collect();
        assert!(categories.is_sorted(), "{size:?}");

        // Every row is reachable and inside the window; the table has no
        // collapsing headers.
        let area = content_area(size);
        for check in Check::ALL {
            info_button(&harness, check).scroll_to_me();
            harness.run();
            let row = bounds(&table_row(&harness, check));
            assert!(area.contains_rect(row), "{size:?} {check:?} row {row:?}");
            assert!(
                area.contains_rect(bounds(&info_button(&harness, check))),
                "{size:?} {check:?} button"
            );
        }
        assert!(
            harness
                .get_by_role_and_label(Role::Table, "Transport health")
                .query_all_by_role(Role::Button)
                .all(|b| b
                    .accesskit_node()
                    .label()
                    .is_some_and(|l| l.starts_with("About ")))
        );
    }
}

/// Value, Status and Help take their content's width, the same at every
/// window width, and Check the rest; check names wrap in a narrow window.
#[test]
fn table_columns_are_compact_and_check_names_wrap() {
    let widths = |size: egui::Vec2| {
        let harness = harness_sized(app_with_report(all_findings_report()), size);
        let table = harness.get_by_role_and_label(Role::Table, "Transport health");
        let rows: Vec<Vec<f32>> = table
            .query_all_by_role(Role::Row)
            .map(|row| cell_widths(&row))
            .collect();
        assert_eq!(rows.len(), 1 + Check::ALL.len());
        for row in &rows {
            assert_eq!(row.len(), 4, "{size:?}");
            // Columns line up.
            for (cell, first) in row.iter().zip(&rows[0]) {
                assert!((cell - first).abs() < 0.5, "{size:?} {row:?}");
            }
        }
        let title_heights: Vec<f32> = Check::ALL
            .iter()
            .map(|c| bounds(&harness.get_by_role_and_label(Role::Label, c.title())).height())
            .collect();
        let badge = bounds(&harness.get_all_by_label("NG").next().unwrap());
        (rows[0].clone(), title_heights, badge)
    };
    let (wide, wide_titles, wide_badge) = widths(WIDE);
    let (narrow, narrow_titles, narrow_badge) = widths(NARROW);
    for compact in 1..4 {
        assert!(wide[compact] < wide[0], "{wide:?}");
        // Not stretched: the same width at both window sizes.
        assert!((wide[compact] - narrow[compact]).abs() < 0.5);
    }
    assert!(wide[0] > 2.0 * (wide[1] + wide[2] + wide[3]), "{wide:?}");
    // Narrow: Check gives way (its names wrap) but keeps a readable width.
    assert!(narrow[0] < wide[0]);
    assert!(narrow[0] >= 80.0, "{narrow:?}");
    // A badge is its text plus padding, at least the minimum width.
    assert!(wide_badge.width() >= crate::theme::BADGE_MIN_WIDTH);
    assert!(wide_badge.width() < 64.0 && wide_badge.height() < 28.0);
    assert_eq!(wide_badge.size(), narrow_badge.size());
    // One line each when wide; some check name wraps when narrow.
    let line = wide_titles[0];
    assert!(wide_titles.iter().all(|h| (h - line).abs() < 0.5));
    assert!(
        narrow_titles.iter().any(|h| *h > 1.5 * line),
        "{narrow_titles:?}"
    );
}

/// The information button opens a page inside Overview, not a window; Back
/// returns to the summary with the focus on the same button, and nothing is
/// analyzed or derived again.
#[test]
fn detail_page_opens_and_closes_with_the_keyboard() {
    for size in [WIDE, NARROW] {
        let mut harness = harness_sized(app_with_report(all_findings_report()), size);
        let session = |harness: &Harness<'_, App>| {
            harness.state().session.as_ref().map(|s| {
                (
                    std::ptr::from_ref(&*s.report),
                    s.plots.timelines.as_ptr(),
                    s.generation,
                )
            })
        };
        let before = session(&harness);

        open_detail(&mut harness, Check::PcrJitter, true);
        assert!(harness.query_by_role(Role::Window).is_none());
        assert!(is_selected(&tab(&harness, Screen::Overview)));
        assert!(back_button(&harness).is_focused());
        harness.get_by_role_and_label(Role::Heading, "PCR jitter estimate");
        assert!(
            harness
                .query_by_role_and_label(Role::Table, "Transport health")
                .is_none()
        );

        // Tab moves on to the next control, Shift+Tab back to Back.
        harness.key_press(egui::Key::Tab);
        harness.run();
        assert!(!back_button(&harness).is_focused());
        harness.key_press_modifiers(egui::Modifiers::SHIFT, egui::Key::Tab);
        harness.run();
        assert!(back_button(&harness).is_focused());

        harness.key_press(egui::Key::Enter);
        harness.run();
        assert_eq!(harness.state().overview.page(), OverviewPage::Summary);
        assert!(info_button(&harness, Check::PcrJitter).is_focused());
        assert_eq!(harness.state().screen, Screen::Overview);
        assert!(before.is_some());
        assert_eq!(session(&harness), before);
    }
}

/// Back restores the program and the summary's scroll position, whatever the
/// detail page was scrolled to; the next detail page starts at its top. The
/// Overview tile also returns to the summary, and a new analysis shows it.
#[test]
fn back_restores_the_program_and_scroll_position() {
    let mut app = analyzed_app();
    app.program = 1;
    let mut harness = harness_sized(app, egui::vec2(480.0, 600.0));
    harness
        .get_by_role_and_label(Role::Link, "PCR jitter estimate")
        .scroll_to_me();
    harness.run();
    let offset = harness.state().overview.summary_offset();
    assert!(offset > 0.0, "the summary did not scroll");
    let reference = bounds(&harness.get_by_label("Elementary streams"));

    // The unavailable-analysis link opens the same page as the table.
    harness
        .get_by_role_and_label(Role::Link, "PCR jitter estimate")
        .click();
    harness.run();
    assert_eq!(
        harness.state().overview.page(),
        OverviewPage::CheckDetail(Check::PcrJitter)
    );
    let title = bounds(&harness.get_by_role_and_label(Role::Heading, "PCR jitter estimate"));
    assert!(content_area(egui::vec2(480.0, 600.0)).contains_rect(title));
    harness
        .get_by_role_and_label(Role::Heading, "Limitations")
        .scroll_to_me();
    harness.run();
    assert_eq!(harness.state().program, 1);

    back_button(&harness).focus();
    harness.run();
    harness.key_press(egui::Key::Enter);
    harness.run();
    assert_eq!(harness.state().overview.page(), OverviewPage::Summary);
    assert_eq!(harness.state().program, 1);
    assert_eq!(harness.state().overview.summary_offset(), offset);
    assert_eq!(
        bounds(&harness.get_by_label("Elementary streams")),
        reference
    );

    // Another page starts at its top, not where the last one was left.
    open_detail(&mut harness, Check::ContinuityCounter, false);
    // Scrolled up to the table to open it.
    let offset = harness.state().overview.summary_offset();
    let title = bounds(&harness.get_by_role_and_label(Role::Heading, "Continuity counter"));
    assert!(content_area(egui::vec2(480.0, 600.0)).contains_rect(title));

    // The Overview tile, selected all along, also goes back.
    tab(&harness, Screen::Overview).click();
    harness.run();
    assert_eq!(harness.state().overview.page(), OverviewPage::Summary);
    assert_eq!(harness.state().overview.summary_offset(), offset);
    assert!(info_button(&harness, Check::ContinuityCounter).is_focused());

    // A new analysis shows its summary from the top.
    open_detail(&mut harness, Check::ContinuityCounter, false);
    harness.state_mut().session_mut().generation += 1;
    harness.run();
    assert_eq!(harness.state().overview.page(), OverviewPage::Summary);
}

/// Every check's page: the current result, verdicts as a table with the
/// current status selected, the explanation as headings and prose outside
/// any table, and navigation.
#[test]
fn every_check_has_a_detail_page() {
    for size in [WIDE, NARROW] {
        let mut harness = harness_sized(app_with_report(all_findings_report()), size);
        for check in Check::ALL {
            open_detail(&mut harness, check, false);
            let help = check.help();
            let row = crate::views::overview::row(
                &harness.state().session.as_ref().unwrap().report,
                &harness.state().session.as_ref().unwrap().report.programs[0],
                check,
            )
            .unwrap();
            harness.get_by_role_and_label(Role::Heading, help.title);
            harness.get_by_label(&format!("Status: {}", row.status.label()));
            for heading in [
                "Current result",
                "Verdicts",
                "What it measures",
                "Applies to",
                "What the value means",
                "When it is skipped",
                "Limitations",
                "Details",
            ] {
                let node = harness.get_by_role_and_label(Role::Heading, heading);
                assert!(!in_table(&node), "{check:?} {heading}");
            }
            for (label, value) in &row.facts {
                // The top bar also has a "Limit" label.
                assert!(
                    harness
                        .query_all_by_role_and_label(Role::Label, label)
                        .count()
                        >= 1,
                    "{check:?} {label}"
                );
                assert!(
                    harness.query_all_by_label(value).count() >= 1,
                    "{check:?} {label}"
                );
            }
            for text in [
                help.measures,
                help.applies_to,
                help.value,
                help.skipped,
                help.limitations,
            ] {
                let node = harness.get_by_label(text);
                assert!(!in_table(&node), "{check:?}: prose inside a table");
            }
            let verdicts = harness.get_by_role_and_label(Role::Table, "Verdicts");
            let rows: Vec<_> = verdicts
                .query_all_by_role(Role::Row)
                .filter(|r| r.accesskit_node().label().is_some())
                .collect();
            assert_eq!(rows.len(), help.statuses.len(), "{check:?}");
            for (node, (status, meaning)) in rows.iter().zip(help.statuses) {
                let label = format!(
                    "{}: {meaning}",
                    crate::presentation::CheckStatus::parse(status).label()
                );
                assert_eq!(
                    node.accesskit_node().label().as_deref(),
                    Some(label.as_str())
                );
                assert_eq!(
                    node.accesskit_node().is_selected(),
                    Some(*status == row.status.as_str()),
                    "{check:?} {status}"
                );
                assert_eq!(cell_widths(node).len(), 2);
            }
            let (screen, _) = help.navigation.unwrap();
            let open = format!("Open {}", screen.title());
            harness
                .get_by_role_and_label(Role::Button, &open)
                .scroll_to_me();
            harness.run();
            harness.get_by_role_and_label(Role::Button, &open).click();
            harness.run();
            assert_eq!(harness.state().screen, screen);
            // Overview comes back on the same page.
            tab(&harness, Screen::Overview).click();
            harness.run();
            assert_eq!(
                harness.state().overview.page(),
                OverviewPage::CheckDetail(check)
            );
            back_button(&harness).click();
            harness.run();
            assert_eq!(harness.state().overview.page(), OverviewPage::Summary);
        }
    }
}

/// Findings link to the same page, which lists the check's findings with the
/// packet and Timeline actions their data supports, and none it does not.
#[test]
fn findings_open_the_detail_page_with_their_actions() {
    let mut app = app_with_report(all_findings_report());
    // A CLI that cannot page packets: Packets shows its empty state instead
    // of running a CLI.
    app.cli = CliState::Ready(Cli {
        packet_outputs: false,
        ..checked_cli()
    });
    let mut harness = harness(app);
    harness.get_by_label("Findings (11)");
    let link = |harness: &mut Harness<'_, App>, name: &str| {
        harness
            .get_by_role_and_label(Role::Link, name)
            .scroll_to_me();
        harness.run();
        harness.get_by_role_and_label(Role::Link, name).click();
        harness.run();
    };

    link(&mut harness, "Continuity counter");
    assert_eq!(
        harness.state().overview.page(),
        OverviewPage::CheckDetail(Check::ContinuityCounter)
    );
    harness.get_by_role_and_label(Role::Heading, "Findings (1)");
    harness.get_by_label("PID 0x0101: continuity_counter 6 (expected 5)");
    harness
        .get_by_role_and_label(Role::Button, "Show 0x0000125C on timeline")
        .click();
    harness.run();
    assert_eq!(harness.state().screen, Screen::Timeline);
    assert_eq!(
        harness.state().timeline.selected_for_test(),
        Some(("Event", 4700))
    );
    // Overview keeps its page while another screen is shown.
    tab(&harness, Screen::Overview).click();
    harness.run();
    assert_eq!(
        harness.state().overview.page(),
        OverviewPage::CheckDetail(Check::ContinuityCounter)
    );
    harness
        .get_by_role_and_label(Role::Button, "Open packet at 0x0000125C")
        .click();
    harness.run();
    assert_eq!(harness.state().screen, Screen::Packets);
    assert_eq!(
        harness
            .state()
            .session
            .as_ref()
            .unwrap()
            .packet_pages
            .selected_for_test(),
        Some(4700)
    );

    // PCR jitter: the packet of the worst estimate, but no Timeline event.
    tab(&harness, Screen::Overview).click();
    harness.run();
    tab(&harness, Screen::Overview).click();
    harness.run();
    link(&mut harness, "PCR jitter estimate");
    harness.get_by_role_and_label(Role::Button, "Open packet at 0x00002A98");
    assert!(harness.query_by_label_contains("on timeline").is_none());

    // Transport errors and scrambled packets: the first affected packet of
    // each PID, which Timeline does not mark.
    for (check, offset) in [
        (Check::TransportErrors, "0x00000524"),
        (Check::ScrambledPackets, "0x00000814"),
    ] {
        back_button(&harness).click();
        harness.run();
        link(&mut harness, check.title());
        harness.get_by_role_and_label(Role::Heading, "Findings (1)");
        harness.get_by_label("PID 0x1fff (null): 1 packet, 0 excluded from analysis");
        harness.get_by_role_and_label(Role::Button, &format!("Open packet at {offset}"));
        assert!(harness.query_by_label_contains("on timeline").is_none());
    }

    // PAT and PMT health: the end of the late interval, which Timeline does
    // not mark.
    for (check, offset, reason) in [
        (
            Check::PatHealth,
            "0x00003A04",
            "PID 0x0000: 1 late interval(s), longest 800.0 ms (limit 500 ms)",
        ),
        (
            Check::PmtHealth,
            "0x00003CF4",
            "PID 0x1000: 1 late interval(s), longest 800.0 ms (limit 500 ms)",
        ),
    ] {
        back_button(&harness).click();
        harness.run();
        link(&mut harness, check.title());
        harness.get_by_role_and_label(Role::Heading, "Findings (1)");
        harness.get_by_label(reason);
        harness.get_by_role_and_label(Role::Button, &format!("Open packet at {offset}"));
        assert!(harness.query_by_label_contains("on timeline").is_none());
    }

    // PES headers and PTS repetition: the first malformed header and the end
    // of the first interval over the limit, in program 1.
    for (check, offset, reason) in [
        (
            Check::PesHeaders,
            "0x00004DD8",
            "PID 0x0100 (video): 1 of 58 headers (bad_marker_bits 1)",
        ),
        (
            Check::PtsRepetition,
            "0x00004A2C",
            "PID 0x0101 (audio): 1 interval over 700 ms, longest 800.0 ms",
        ),
    ] {
        back_button(&harness).click();
        harness.run();
        link(&mut harness, check.title());
        harness.get_by_role_and_label(Role::Heading, "Findings (1)");
        harness.get_by_label(reason);
        harness.get_by_role_and_label(Role::Button, &format!("Open packet at {offset}"));
        assert!(harness.query_by_label_contains("on timeline").is_none());
    }

    // Summary-only findings have no offset to open.
    for check in [
        Check::MaxPcrInterval,
        Check::PcrPtsDtsGap,
        Check::TimestampAnomalies,
    ] {
        back_button(&harness).click();
        harness.run();
        link(&mut harness, check.title());
        harness.get_by_role_and_label(Role::Heading, "Findings (1)");
        assert!(
            harness.query_by_label_contains("Open packet").is_none(),
            "{check:?}"
        );
        assert!(harness.query_by_label_contains("on timeline").is_none());
    }
}

#[test]
fn findings_account_for_every_check_that_is_not_ok() {
    // Every check of the all-findings sample reports a problem; its packets
    // are aligned and its PCR clock is continuous, so the sync result of the
    // sync-loss sample and the PCR discontinuities of the
    // PCR-discontinuities sample are added.
    let sync_loss: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_sync_loss_188.json"
    ))
    .unwrap();
    let pcr_discontinuities: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_pcr_discontinuities_188.json"
    ))
    .unwrap();
    let pid_references: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_pid_references_188.json"
    ))
    .unwrap();
    // One finding each: the silent reference and the unreferenced PID of the
    // PID-reference sample.
    let mut references = pid_references["pid_references"].clone();
    references["referenced"]["pids"]
        .as_array_mut()
        .unwrap()
        .retain(|p| p["status"] == "ok" || p["status"] == "silent");
    references["referenced"]["status"] = "silent".into();
    references["unreferenced"]["pids"]
        .as_array_mut()
        .unwrap()
        .retain(|p| p["status"] == "unreferenced");
    let report = edited_report(|document| {
        document["sync"] = sync_loss["sync"].clone();
        document["programs"][0]["compliance"]["pcr_discontinuities"] =
            pcr_discontinuities["programs"][0]["compliance"]["pcr_discontinuities"].clone();
        document["pid_references"] = references;
    });
    let harness = harness(app_with_report(report));
    // Two findings for PCR discontinuities (a backward step and a forward
    // jump), the two PID-reference findings, and two for PAT and PMT health.
    harness.get_by_label("Findings (16)");
    assert!(harness.query_by_label("None.").is_none());
    for check in Check::ALL {
        // Packet gaps and Mux lead are reports without a verdict: never a
        // finding.
        let links = match check {
            Check::PcrDiscontinuities => 2,
            Check::PacketGaps | Check::MuxLead => 0,
            _ => 1,
        };
        assert_eq!(
            harness
                .query_all_by_role_and_label(Role::Link, check.title())
                .count(),
            links,
            "{check:?}"
        );
    }

    // The two-program sample is VBR: PCR jitter is not assessable, which is
    // unavailable analysis with the CLI's reason, not a finding.
    let harness = self::harness(analyzed_app());
    harness.get_by_label("Findings (0)");
    harness.get_by_label("Unavailable analysis (1)");
    // The badge of the table row and of the unavailable analysis.
    assert_eq!(harness.query_all_by_label("NOT ASSESSABLE").count(), 2);
    harness.get_by_role_and_label(Role::Link, "PCR jitter estimate");
    harness.get_by_label_contains("Delivery variable: the estimated jitter reaches 55.082ms");

    // Every check OK: no finding.
    let mut clean: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_two_programs_188.json"
    ))
    .unwrap();
    clean["programs"][0]["pcr_jitter"]["status"] = "ok".into();
    let harness = self::harness(app_with_report(
        serde_json::from_value(clean.clone()).unwrap(),
    ));
    harness.get_by_label("Findings (0)");
    harness.get_by_label("None.");
    assert!(
        harness
            .query_by_label_contains("Unavailable analysis")
            .is_none()
    );

    // A skipped check is unavailable analysis, not a finding.
    clean["programs"][0]["pcr_jitter"]["status"] = "skipped".into();
    clean["programs"][0]["pcr_jitter"]["reason"] = "need at least 3 PCR samples".into();
    let harness = self::harness(app_with_report(serde_json::from_value(clean).unwrap()));
    harness.get_by_label("Findings (0)");
    harness.get_by_label("Unavailable analysis (1)");
    harness.get_by_role_and_label(Role::Link, "PCR jitter estimate");
}

/// A status this GUI does not know is shown verbatim in the table, as a
/// finding and on the detail page, which says so; no verdict is selected.
#[test]
fn unknown_status_stays_visible() {
    for size in [WIDE, NARROW] {
        let report = edited_report(|d| {
            d["programs"][0]["pcr_jitter"]["status"] = "indeterminate".into();
        });
        let mut harness = harness_sized(app_with_report(report), size);
        table_row(&harness, Check::PcrJitter)
            .query_by_label("INDETERMINATE")
            .expect("the table shows the status");
        harness.get_by_label("Findings (11)");
        open_detail(&mut harness, Check::PcrJitter, false);
        harness.get_by_label("Status: INDETERMINATE");
        harness.get_by_label_contains(
            "The CLI reported the status \"indeterminate\", which this GUI does not describe.",
        );
        let verdicts = harness.get_by_role_and_label(Role::Table, "Verdicts");
        assert!(
            verdicts
                .query_all_by_role(Role::Row)
                .all(|row| row.accesskit_node().is_selected() != Some(true))
        );
    }
}

/// A status that another check describes is still unknown for a check
/// without it: `warning` exists for PCR jitter, not for Max PCR interval.
#[test]
fn status_knowledge_is_per_check() {
    let report = edited_report(|d| {
        d["programs"][0]["compliance"]["max_pcr_interval"]["status"] = "warning".into();
    });
    let mut harness = harness_sized(app_with_report(report), WIDE);
    open_detail(&mut harness, Check::MaxPcrInterval, false);
    harness.get_by_label("Status: WARNING");
    harness.get_by_label_contains(
        "The CLI reported the status \"warning\", which this GUI does not describe.",
    );
    let verdicts = harness.get_by_role_and_label(Role::Table, "Verdicts");
    assert!(
        verdicts
            .query_all_by_role(Role::Row)
            .all(|row| row.accesskit_node().is_selected() != Some(true))
    );

    // `not_assessable` means unavailable only for PCR jitter: on Max PCR
    // interval it is an unknown status, listed as a finding.
    let report = edited_report(|d| {
        d["programs"][0]["compliance"]["max_pcr_interval"]["status"] = "not_assessable".into();
    });
    let mut harness = harness_sized(app_with_report(report), WIDE);
    harness.run();
    // Still eleven findings (the fixture's), and nothing unavailable.
    harness.get_by_label("Findings (11)");
    assert!(
        harness
            .query_by_label_contains("Unavailable analysis")
            .is_none()
    );
    open_detail(&mut harness, Check::MaxPcrInterval, false);
    harness.get_by_label_contains("It is shown as reported and listed as a finding.");
}

/// PCR jitter that the CLI did not grade: its status in the table, the
/// delivery evidence and reason on its page, and no finding.
#[test]
fn not_assessable_jitter_has_its_evidence_on_the_detail_page() {
    for size in [WIDE, NARROW] {
        let mut harness = harness_sized(analyzed_app(), size);
        table_row(&harness, Check::PcrJitter)
            .query_by_label("NOT ASSESSABLE")
            .expect("the table shows the status");
        open_detail(&mut harness, Check::PcrJitter, true);
        harness.get_by_label("Status: NOT ASSESSABLE");
        assert!(harness.query_by_label_contains("Findings (").is_none());
        for (label, value) in [
            ("Value", "55.08 ms"),
            ("Worst offset", "0x000223D8"),
            ("Delivery", "variable"),
            ("Delivery evidence", "1 × 1 s window at 767.9 kbit/s"),
        ] {
            harness.get_by_role_and_label(Role::Label, label);
            harness.get_by_role_and_label(Role::Label, value);
        }
        harness.get_by_label_contains("the estimated jitter reaches 55.082ms");
        harness.get_by_label(
            "delivery rate is variable, so the byte-position estimate does not measure PCR accuracy",
        );
        let selected: Vec<_> = harness
            .get_by_role_and_label(Role::Table, "Verdicts")
            .query_all_by_role(Role::Row)
            .filter(|row| row.accesskit_node().is_selected() == Some(true))
            .map(|row| row.accesskit_node().label().unwrap())
            .collect();
        assert_eq!(selected.len(), 1);
        assert!(selected[0].starts_with("NOT ASSESSABLE: "));
    }
}

/// The PCR-discontinuities fixture is what the CLI writes for its sample.
/// Runs only against a CLI named by MPEG_TS_ANALYZER_CLI (e.g. via `make gui-test`).
#[test]
fn pcr_discontinuities_fixture_matches_the_cli() {
    let args = ["--format", "json", "--output", "analysis"];
    let Some(json) =
        crate::cli::run_on_sample("diagnostics/sample_188byte_pcr_discontinuities.ts", &args)
    else {
        return;
    };
    let fixture: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_pcr_discontinuities_188.json"
    ))
    .unwrap();
    let mut output: serde_json::Value = serde_json::from_slice(&json).unwrap();
    output["input"]["path"] = fixture["input"]["path"].clone();
    assert_eq!(output, fixture, "regenerate the fixture with the CLI");
}

/// The fixture is what the CLI writes for the sample, so the Overview tests
/// above see a real report; both packet sizes give the same findings.
/// Runs only against a CLI named by MPEG_TS_ANALYZER_CLI (e.g. via `make gui-test`).
#[test]
fn all_findings_fixture_matches_the_cli() {
    let args = ["--format", "json", "--output", "analysis"];
    let Some(json) = crate::cli::run_on_sample("diagnostics/sample_188byte_all_findings.ts", &args)
    else {
        return;
    };
    let fixture: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_all_findings_188.json"
    ))
    .unwrap();
    let mut output: serde_json::Value = serde_json::from_slice(&json).unwrap();
    output["input"]["path"] = fixture["input"]["path"].clone();
    assert_eq!(output, fixture, "regenerate the fixture with the CLI");

    let m2ts =
        crate::cli::run_on_sample("diagnostics/sample_192byte_all_findings.ts", &args).unwrap();
    let m2ts = crate::report::parse_analysis(&m2ts, "2").unwrap();
    let report = all_findings_report();
    let summary = |report: &AnalysisReport| -> Vec<(String, String)> {
        crate::views::overview::findings(report, &report.programs[0])
            .0
            .into_iter()
            .map(|f| (f.status.to_string(), f.label))
            .collect()
    };
    assert_eq!(summary(&m2ts), summary(&report));
    assert_eq!(m2ts.input.packet_size_bytes, Some(192));
}

/// The sync-loss fixture is what the CLI writes for the sample.
/// Runs only against a CLI named by MPEG_TS_ANALYZER_CLI (e.g. via `make gui-test`).
#[test]
fn pid_references_fixture_matches_the_cli() {
    let args = ["--format", "json", "--output", "analysis"];
    let Some(json) =
        crate::cli::run_on_sample("diagnostics/sample_188byte_pid_references.ts", &args)
    else {
        return;
    };
    let fixture: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_pid_references_188.json"
    ))
    .unwrap();
    let mut output: serde_json::Value = serde_json::from_slice(&json).unwrap();
    output["input"]["path"] = fixture["input"]["path"].clone();
    assert_eq!(output, fixture, "regenerate the fixture with the CLI");
}

/// The Referenced PIDs page of the PID-reference sample lists its findings,
/// each with the packet before its silence.
#[test]
fn referenced_pids_page_opens_the_packet_before_a_silence() {
    let report: AnalysisReport = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_pid_references_188.json"
    ))
    .unwrap();
    let mut harness = harness_sized(app_with_report(report), WIDE);
    harness.run();
    table_row(&harness, Check::ReferencedPids)
        .query_by_label("ABSENT")
        .expect("the table shows the status");
    open_detail(&mut harness, Check::ReferencedPids, false);
    harness.get_by_role_and_label(Role::Heading, "Findings (2)");
    harness.get_by_label("Status: ABSENT");
    harness.get_by_role_and_label(Role::Button, "Open packet at 0x0000AE0C");
}

/// The packet-gap fixture is what the CLI writes for its sample.
/// Runs only against a CLI named by MPEG_TS_ANALYZER_CLI (e.g. via `make gui-test`).
#[test]
fn packet_gaps_fixture_matches_the_cli() {
    let args = ["--format", "json", "--output", "analysis"];
    let Some(json) = crate::cli::run_on_sample("diagnostics/sample_188byte_packet_gaps.ts", &args)
    else {
        return;
    };
    let fixture: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_packet_gaps_188.json"
    ))
    .unwrap();
    let mut output: serde_json::Value = serde_json::from_slice(&json).unwrap();
    output["input"]["path"] = fixture["input"]["path"].clone();
    assert_eq!(output, fixture, "regenerate the fixture with the CLI");
}

/// The Packet gaps page of the bursty remux: a neutral report without
/// findings, and a per-PID table whose offsets open the packets around the
/// largest gap.
#[test]
fn packet_gaps_page_lists_every_pid_of_the_program() {
    let report: AnalysisReport = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_packet_gaps_188.json"
    ))
    .unwrap();
    let mut harness = harness_sized(app_with_report(report), WIDE);
    harness.run();
    table_row(&harness, Check::PacketGaps)
        .query_by_label("INFO")
        .expect("the table shows the status");
    open_detail(&mut harness, Check::PacketGaps, false);
    harness.get_by_label("Status: INFO");
    harness.get_by_role_and_label(Role::Heading, "Per PID");
    // No finding section: the report has no verdict.
    assert!(harness.query_by_label_contains("Findings (").is_none());
    for offset in ["0x00011F24", "0x00017BAC", "0x000040A0"] {
        harness.get_by_role_and_label(Role::Button, &format!("Open packet at {offset}"));
    }
}

/// The mux-lead fixture is what the CLI writes for its sample.
/// Runs only against a CLI named by MPEG_TS_ANALYZER_CLI (e.g. via `make gui-test`).
#[test]
fn mux_lead_fixture_matches_the_cli() {
    let args = ["--format", "json", "--output", "analysis"];
    let Some(json) = crate::cli::run_on_sample("diagnostics/sample_188byte_mux_lead.ts", &args)
    else {
        return;
    };
    let fixture: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_mux_lead_188.json"
    ))
    .unwrap();
    let mut output: serde_json::Value = serde_json::from_slice(&json).unwrap();
    output["input"]["path"] = fixture["input"]["path"].clone();
    assert_eq!(output, fixture, "regenerate the fixture with the CLI");
}

/// The Mux lead page of the audio-preload sample: a neutral report without
/// findings, a per-PID table and a per-audio table whose offsets open the
/// PES packets.
#[test]
fn mux_lead_page_lists_every_pid_and_audio_stream() {
    let report: AnalysisReport = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_mux_lead_188.json"
    ))
    .unwrap();
    let mut harness = harness_sized(app_with_report(report), WIDE);
    harness.run();
    table_row(&harness, Check::MuxLead)
        .query_by_label("INFO")
        .expect("the table shows the status");
    open_detail(&mut harness, Check::MuxLead, false);
    harness.get_by_label("Status: INFO");
    harness.get_by_role_and_label(Role::Heading, "Per PID");
    harness.get_by_role_and_label(Role::Heading, "A/V lead difference per audio stream");
    // No finding section: the report has no verdict.
    assert!(harness.query_by_label_contains("Findings (").is_none());
    // The smallest AAC lead (packet 51) and the two PES packets of the
    // largest A/V difference (packets 9 and 10).
    for offset in ["0x00002574", "0x0000069C", "0x00000758"] {
        assert!(
            harness
                .query_all_by_role_and_label(Role::Button, &format!("Open packet at {offset}"))
                .count()
                >= 1,
            "{offset}"
        );
    }
    harness
        .query_all_by_role_and_label(Role::Button, "Open packet at 0x00002574")
        .next()
        .unwrap()
        .click();
    harness.run();
    assert_eq!(harness.state().screen, Screen::Packets);
}

#[test]
fn sync_loss_fixture_matches_the_cli() {
    let args = ["--format", "json", "--output", "analysis"];
    let Some(json) = crate::cli::run_on_sample("diagnostics/sample_188byte_sync_loss.ts", &args)
    else {
        return;
    };
    let fixture: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_sync_loss_188.json"
    ))
    .unwrap();
    let mut output: serde_json::Value = serde_json::from_slice(&json).unwrap();
    output["input"]["path"] = fixture["input"]["path"].clone();
    assert_eq!(output, fixture, "regenerate the fixture with the CLI");
}

/// The PES packet with a PCR, a PTS and a DTS at offset 30528 of the 192-byte
/// two-program sample, as the CLI decodes it, with its wire layout.
pub(super) fn pes_pcr_192_detail() -> PacketDetail {
    serde_json::from_str(include_str!(
        "../../tests/fixtures/packet_detail_pes_pcr_192.json"
    ))
    .expect("packet fixture")
}

/// The Packets screen with the PMT packet at offset 564 selected.
fn packets_screen(size: egui::Vec2) -> Harness<'static, App> {
    let mut app = analyzed_app();
    app.screen = Screen::Packets;
    app.session_mut().packet_pages =
        PacketPages::loaded_for_test(packets_page(), 564, pmt_packet_detail());
    harness_sized(app, size)
}

fn pages_mut<'a>(harness: &'a mut Harness<'_, App>) -> &'a mut PacketPages {
    &mut harness.state_mut().session_mut().packet_pages
}

/// A line of the wire-layout field list, by the start of its text.
fn layout_line<'a>(harness: &'a Harness<'_, App>, prefix: &'a str) -> Node<'a> {
    harness.get_by(move |node| {
        node.role() == Role::Button && node.label().is_some_and(|l| l.starts_with(prefix))
    })
}

/// Clicks a line of the field list, scrolling it into view first.
fn click_line(harness: &mut Harness<'_, App>, prefix: &str) {
    layout_line(harness, prefix).scroll_to_me();
    harness.run();
    layout_line(harness, prefix).click();
    harness.run();
}

/// "Detailed inspection" replaces the whole Packets screen, list and pane,
/// with the packet's wire layout; Back shows the list as it was left.
#[test]
fn detailed_inspection_is_a_full_page_and_back_restores_the_list() {
    for size in [WIDE, egui::vec2(820.0, 640.0)] {
        let mut harness = packets_screen(size);
        pages_mut(&mut harness).list_offset_for_test(Some(egui::vec2(0.0, 44.0)));
        harness.run();
        let scrolled = pages_mut(&mut harness).list_offset_for_test(None);
        assert!(scrolled.y > 0.0);
        detailed_inspection_buttons(&harness)[0].click();
        harness.run();
        assert_eq!(pages_mut(&mut harness).inspected_for_test(), Some(564));
        // A page of its own: neither the list nor the compact pane is shown.
        assert!(harness.query_by_label("This packet").is_none());
        assert!(harness.query_by_label("Page 1 / 1").is_none());
        harness.get_by_label("Packet #4 of 40");
        harness.get_by_label("Packet type: PMT");
        harness.get_by_label_contains("· PID 4097 (0x1001) ·");
        harness.get_by_label("Bit grid: 1504 of 1504 bits classified");

        // A reserved run is selectable although several parts are labeled
        // reserved; its expected value and rule are shown.
        click_line(&mut harness, "reserved  111 ✓  · bits 104–106");
        harness.get_by_label("111 · valid");
        harness.get_by_label_contains("ISO/IEC 13818-1 2.1 (reserved)");

        // Next stays in the range and keeps the page open.
        button_containing(&harness, "Next").click();
        // The next packet is read with the CLI, which this test does not have.
        harness.run_steps(10);
        assert_eq!(pages_mut(&mut harness).inspected_for_test(), Some(752));
        harness.get_by_label("Packet #5 of 40");

        back_to_packets(&harness).click();
        // The selected packet is read again with the CLI, which this test does not have.
        harness.run_steps(10);
        let pages = pages_mut(&mut harness);
        assert_eq!(pages.inspected_for_test(), None);
        assert_eq!(pages.selected_for_test(), Some(564));
        assert_eq!(pages.list_offset_for_test(None), scrolled);
        harness.get_by_label("Page 1 / 1");
    }
}

/// The "Detailed inspection" buttons of the packet detail pane, top first.
fn detailed_inspection_buttons<'a>(harness: &'a Harness<'_, App>) -> Vec<Node<'a>> {
    let mut buttons: Vec<Node<'a>> = harness
        .query_all_by_role_and_label(Role::Button, "Detailed inspection")
        .collect();
    buttons.sort_by(|a, b| bounds(a).min.y.total_cmp(&bounds(b).min.y));
    buttons
}

/// The pane offers "Detailed inspection" above the decoded fields and again
/// at its very end, after the Bit inspector; either opens the page.
#[test]
fn detailed_inspection_is_offered_again_after_the_bit_inspector() {
    let mut harness = packets_screen(WIDE);
    let buttons = detailed_inspection_buttons(&harness);
    assert_eq!(buttons.len(), 2);
    let top = |node: &Node<'_>| bounds(node).min.y;
    assert!(top(&buttons[0]) < top(&harness.get_by_label("This packet")));
    assert!(top(&buttons[1]) > top(&harness.get_by_label("Bit inspector")));
    let last = detailed_inspection_buttons(&harness).pop().unwrap();
    last.scroll_to_me();
    harness.run();
    detailed_inspection_buttons(&harness).pop().unwrap().click();
    harness.run();
    assert_eq!(pages_mut(&mut harness).inspected_for_test(), Some(564));
    harness.get_by_label("Packet #4 of 40");
}

/// The Packets field list shows numbers decimal first with the hexadecimal
/// padded to the field's width; flags, bit patterns and checksums stay as written.
#[test]
fn packet_fields_are_decimal_with_hex() {
    let harness = packets_screen(WIDE);
    harness.get_by_label("4097 (0x1001)");
    harness.get_by_label("2 (0x02)");
    harness.get_by_label("23 (0x017)");
    harness.get_by_label("0x1058355a");
    assert!(harness.query_by_label("0x1001").is_none());
}

/// ← and → step through the packets like Previous and Next, within the range.
#[test]
fn arrow_keys_step_through_the_packets() {
    let mut harness = packets_screen(WIDE);
    detailed_inspection_buttons(&harness)[0].click();
    harness.run();
    harness.key_press(egui::Key::ArrowRight);
    harness.run_steps(10);
    assert_eq!(pages_mut(&mut harness).inspected_for_test(), Some(752));
    harness.key_press(egui::Key::ArrowLeft);
    harness.key_press(egui::Key::ArrowLeft);
    harness.run_steps(10);
    assert_eq!(pages_mut(&mut harness).inspected_for_test(), Some(376));

    // Not past the first packet of the range.
    let mut app = analyzed_app();
    app.screen = Screen::Packets;
    let mut pages = PacketPages::loaded_for_test(packets_page(), 0, pat_packet_detail());
    pages.inspect_for_test(None);
    app.session_mut().packet_pages = pages;
    let mut harness = self::harness(app);
    harness.key_press(egui::Key::ArrowLeft);
    harness.run_steps(4);
    assert_eq!(pages_mut(&mut harness).inspected_for_test(), Some(0));
}

/// ↑ and ↓ move the selection through the lines of the field list as drawn,
/// updating the facts above the grid; they stop at the ends of the list.
#[test]
fn up_and_down_step_through_the_field_list() {
    let mut app = analyzed_app();
    app.screen = Screen::Packets;
    let mut pages = PacketPages::loaded_for_test(packets_page(), 564, pmt_packet_detail());
    pages.inspect_for_test(Some(crate::views::wire_layout::Selection::Part(
        "ts_header.pid@11".to_owned(),
    )));
    app.session_mut().packet_pages = pages;
    let mut harness = harness(app);
    harness.get_by_label_contains("packet bits 11–23 (13 bits)");
    harness.key_press(egui::Key::ArrowDown);
    harness.run();
    harness.get_by_label_contains("packet bits 24–25 (2 bits)");
    harness.key_press(egui::Key::ArrowUp);
    harness.key_press(egui::Key::ArrowUp);
    harness.run();
    harness.get_by_label_contains("packet bit 10 (1 bit)");
    // Not past the first line; the packet stays the same.
    for _ in 0..6 {
        harness.key_press(egui::Key::ArrowUp);
    }
    harness.run();
    harness.get_by_label_contains("packet bits 0–7 (8 bits)");
    assert_eq!(pages_mut(&mut harness).inspected_for_test(), Some(564));

    // The lines are 25 points apart.
    let top = |prefix: &str| bounds(&layout_line(&harness, prefix)).min.y;
    let pitch = top("transport_error_indicator") - top("sync_byte");
    assert!((pitch - 25.0).abs() < 0.5, "row pitch {pitch}");
}

/// The Detailed inspection page of `detail`, with `selection` on its list.
fn inspected(
    offset: u64,
    detail: PacketDetail,
    selection: Option<crate::views::wire_layout::Selection>,
) -> Harness<'static, App> {
    let mut app = analyzed_app();
    app.screen = Screen::Packets;
    let mut pages = PacketPages::loaded_for_test(packets_page(), offset, detail);
    pages.inspect_for_test(selection);
    app.session_mut().packet_pages = pages;
    harness(app)
}

/// A section header of the field list (a layer), by its title.
fn section<'a>(harness: &'a Harness<'_, App>, title: &'a str) -> Node<'a> {
    harness.get_by(move |node| node.label().as_deref() == Some(title) && node.role() != Role::Label)
}

/// The layer that decodes the payload (a PSI section, a PES header) is a
/// section inside the payload's, after the payload line; the framing layers
/// and a 204-byte trailer stay at the top level, and a payload that nothing
/// decodes keeps its line alone.
#[test]
fn decoded_layers_are_inside_the_payload() {
    let left = |harness: &Harness<'_, App>, title: &str| bounds(&section(harness, title)).min.x;
    let top = |harness: &Harness<'_, App>, title: &str| bounds(&section(harness, title)).min.y;

    let harness = inspected(564, pmt_packet_detail(), None);
    assert!(left(&harness, "PMT") > left(&harness, "Payload") + 4.0);
    assert_eq!(left(&harness, "TS header"), left(&harness, "Payload"));
    let payload_line = bounds(&layout_line(&harness, "payload  184 bytes")).min.y;
    assert!(top(&harness, "Payload") < payload_line && payload_line < top(&harness, "PMT"));

    // A PES header likewise; the adaptation field stays a sibling.
    let harness = inspected(30528, pes_pcr_192_detail(), None);
    assert!(left(&harness, "PES header") > left(&harness, "Payload") + 4.0);
    assert_eq!(
        left(&harness, "Adaptation field"),
        left(&harness, "Payload")
    );

    // A 204-byte trailer follows the payload (and its PES header) at the top.
    let detail: PacketDetail = serde_json::from_str(include_str!(
        "../../tests/fixtures/packet_detail_video_204.json"
    ))
    .unwrap();
    let harness = inspected(2448, detail, None);
    assert_eq!(left(&harness, "Trailer"), left(&harness, "Payload"));
    assert!(left(&harness, "PES header") > left(&harness, "Payload") + 4.0);

    // A payload nothing decodes (a continuation, scrambled): its line alone.
    let mut document: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/packet_detail_pmt_1001_188.json"
    ))
    .unwrap();
    document["packet"]["layers"]
        .as_array_mut()
        .unwrap()
        .truncate(2);
    let harness = inspected(564, serde_json::from_value(document).unwrap(), None);
    section(&harness, "Payload");
    layout_line(&harness, "payload  184 bytes");
    assert!(harness.query_by_label("PMT").is_none());

    // The Packets pane nests the PMT in the payload's section too.
    let harness = packets_screen(WIDE);
    assert!(left(&harness, "PMT") > left(&harness, "Payload") + 4.0);
    assert_eq!(left(&harness, "TS header"), left(&harness, "Payload"));
    assert!(top(&harness, "Payload") < top(&harness, "PMT"));
}

/// The facts of a part in a decoded layer say it is in the payload, and
/// ↑ / ↓ walk from the payload line into the PMT beneath it.
#[test]
fn up_and_down_step_into_the_decoded_layer() {
    let mut harness = inspected(
        564,
        pmt_packet_detail(),
        Some(crate::views::wire_layout::Selection::Part(
            "ts_header.continuity_counter@28".to_owned(),
        )),
    );
    harness.get_by_label_contains("packet bits 28–31 (4 bits)");
    harness.key_press(egui::Key::ArrowDown);
    harness.run();
    harness.get_by_label_contains("packet bits 32–1503 (1472 bits)");
    harness.key_press(egui::Key::ArrowDown);
    harness.run();
    // pointer_field, the first line of the PMT inside the payload.
    harness.get_by_label_contains("packet bits 32–39 (8 bits)");
    harness.get_by_label("Payload › PMT › pointer_field");
    harness.key_press(egui::Key::ArrowUp);
    harness.run();
    harness.get_by_label_contains("packet bits 32–1503 (1472 bits)");
}

/// Selecting a part of the PMT in the bit grid while the payload's section
/// is collapsed opens it, so the selected line is shown.
#[test]
fn selecting_a_decoded_part_opens_the_payload() {
    let mut harness = inspected(564, pmt_packet_detail(), None);
    section(&harness, "Payload").click();
    harness.run();
    assert!(harness.query_by_label("PMT").is_none());
    assert!(harness.query_by_label_contains("table_id  ").is_none());

    // table_id: packet bits 40–47, byte 5.
    let grid = bounds(&harness.get_by_label("Bit grid: 1504 of 1504 bits classified"));
    let per_row = ((grid.width() - 40.0) / 150.0).round() as usize;
    let (row, column) = (5 / per_row, 5 % per_row);
    let at = egui::pos2(
        grid.left() + 40.0 + column as f32 * 150.0 + 7.0,
        grid.top() + row as f32 * 20.0 + 10.0,
    );
    harness.hover_at(at);
    for pressed in [true, false] {
        harness.event(egui::Event::PointerButton {
            pos: at,
            button: egui::PointerButton::Primary,
            pressed,
            modifiers: egui::Modifiers::default(),
        });
    }
    harness.run();
    harness.get_by_label("Payload › PMT › table_id");
    section(&harness, "PMT");
    assert!(is_selected(&layout_line(&harness, "table_id  ")));
    // Collapsing it again works while the part stays selected.
    section(&harness, "Payload").click();
    harness.run();
    assert!(harness.query_by_label("PMT").is_none());
    harness.get_by_label("Payload › PMT › table_id");
}

/// The header names what the packet is with a badge.
#[test]
fn the_header_names_the_kind_of_packet() {
    let inspect = |offset: u64, detail: PacketDetail| {
        let mut app = analyzed_app();
        app.screen = Screen::Packets;
        let mut pages = PacketPages::loaded_for_test(packets_page(), offset, detail);
        pages.inspect_for_test(None);
        app.session_mut().packet_pages = pages;
        harness(app)
    };
    inspect(188, pat_packet_detail()).get_by_label("Packet type: PAT");
    inspect(564, pmt_packet_detail()).get_by_label("Packet type: PMT");
    inspect(30528, pes_pcr_192_detail()).get_by_label_contains("Packet type: PES · ");
}

fn button_containing<'a>(harness: &'a Harness<'_, App>, text: &'a str) -> Node<'a> {
    harness.get_by(move |node| {
        node.role() == Role::Button && node.label().is_some_and(|l| l.contains(text))
    })
}

fn back_to_packets<'a>(harness: &'a Harness<'_, App>) -> Node<'a> {
    harness.get_by_role_and_label(Role::Button, "Back to Packets")
}

/// Previous is disabled on the first packet of the range.
#[test]
fn previous_and_next_stay_within_the_range() {
    let mut app = analyzed_app();
    app.screen = Screen::Packets;
    let mut pages = PacketPages::loaded_for_test(packets_page(), 0, pat_packet_detail());
    pages.inspect_for_test(None);
    app.session_mut().packet_pages = pages;
    let mut harness = harness(app);
    harness.get_by_label("Packet #1 of 40");
    assert!(
        !button_containing(&harness, "Next")
            .accesskit_node()
            .is_disabled()
    );
    assert!(
        button_containing(&harness, "Previous")
            .accesskit_node()
            .is_disabled()
    );
    assert_eq!(pages_mut(&mut harness).inspected_for_test(), Some(0));
}

/// A PTS is one line with its complete encoded span; its prefix, value runs
/// and marker bits are lines beneath it, opened when one of them is selected.
/// A PCR likewise spans its base, reserved bits and extension.
#[test]
fn timestamps_and_clock_references_have_their_parts_beneath_them() {
    let inspect = |selection: Option<crate::views::wire_layout::Selection>| {
        let mut app = analyzed_app();
        app.screen = Screen::Packets;
        let mut pages = PacketPages::loaded_for_test(packets_page(), 30528, pes_pcr_192_detail());
        pages.inspect_for_test(selection);
        app.session_mut().packet_pages = pages;
        harness(app)
    };
    let mut harness = inspect(None);
    click_line(
        &mut harness,
        "PTS  144000 (0x000023280, 1.600000 s)  · bits 200–239",
    );
    harness.get_by_label("packet bits 200–239 (40 bits) · byte 25, bit 7 – byte 29, bit 0");
    harness.get_by_label("7");
    assert!(
        harness
            .query_by_label_contains("marker_bit  1 ✓  · bit 207")
            .is_none()
    );
    click_line(
        &mut harness,
        "program_clock_reference  23220000 (0.860000 s)",
    );
    harness.get_by_label_contains("packet bits 80–127 (48 bits)");

    // Selecting a part opens its field and shows the part on its own.
    let harness = inspect(Some(crate::views::wire_layout::Selection::Part(
        "pes_header.pts.marker_bit@207".to_owned(),
    )));
    layout_line(&harness, "prefix  0011 ✓  · bits 200–203");
    layout_line(&harness, "PTS[32..30]  000  · bits 204–206");
    layout_line(&harness, "marker_bit  1 ✓  · bit 207");
    harness.get_by_label("1 · valid");
    harness.get_by_label_contains("marker_bit is '1'");
}

/// A fixed bit that differs from the specification is flagged.
#[test]
fn a_fixed_bit_mismatch_is_flagged() {
    let mut document: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/packet_detail_pmt_1001_188.json"
    ))
    .unwrap();
    for part in document["packet"]["layers"][2]["bit_layout"]
        .as_array_mut()
        .unwrap()
    {
        if part["id"] == "pmt.pcr_pid.reserved@104" {
            part["value"] = "011".into();
            part["valid"] = false.into();
        }
    }
    let detail: PacketDetail = serde_json::from_value(document).unwrap();
    let mut app = analyzed_app();
    app.screen = Screen::Packets;
    let mut pages = PacketPages::loaded_for_test(packets_page(), 564, detail);
    pages.inspect_for_test(Some(crate::views::wire_layout::Selection::Part(
        "pmt.pcr_pid.reserved@104".to_owned(),
    )));
    app.session_mut().packet_pages = pages;
    let harness = harness(app);
    harness.get_by_label("111 · mismatch");
    layout_line(&harness, "reserved  011 ✗ expected 111");
}

/// A CLI without `bit_layout`: the page still opens, says so, and classifies
/// the decoded fields' bits.
#[test]
fn detailed_inspection_falls_back_to_field_bits() {
    let mut app = analyzed_app();
    app.screen = Screen::Packets;
    let mut pages = PacketPages::loaded_for_test(packets_page(), 188, pat_packet_detail());
    pages.inspect_for_test(None);
    app.session_mut().packet_pages = pages;
    let mut harness = harness(app);
    harness.get_by_label_contains("does not report the wire layout");
    click_line(&mut harness, "PID  0 (0x0000)");
    harness.get_by_label_contains("packet bits 11–23 (13 bits)");
}

/// The session keeps the plot data in place of the CLI's raw series (#154):
/// no program of its report retains `series`, while the plots of every
/// program hold what Timeline and Bitrate draw and pick from.
#[test]
fn session_keeps_plots_instead_of_the_raw_series() {
    let report: AnalysisReport = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_two_programs_188.json"
    ))
    .expect("analysis fixture");
    let raw: Vec<(usize, usize)> = report
        .programs
        .iter()
        .map(|program| {
            let series = program.series.as_ref().expect("fixture series");
            let intervals = series.pcr.iter().filter(|p| p.interval_ms.is_some());
            (intervals.count(), series.pcr_jitter.len())
        })
        .collect();
    let app = app_with_report(report);
    let session = app.session.as_ref().expect("a session");
    assert!(session.report.programs.iter().all(|p| p.series.is_none()));
    assert_eq!(session.plots.timelines.len(), raw.len());
    assert_eq!(session.plots.bitrates.len(), raw.len());
    let plots = session.plots.timelines.iter().zip(&session.plots.bitrates);
    for ((timeline, bitrate), (intervals, jitter)) in plots.zip(raw) {
        // Timeline picks from every PCR interval and PCR → PTS/DTS gap.
        assert_eq!(timeline.pcr_interval.len(), intervals);
        assert!(timeline.gaps.iter().all(|gap| !gap.points.is_empty()));
        assert!(!timeline.gaps.is_empty() && !bitrate.pids.is_empty());
        assert_eq!(bitrate.jitter_samples, jitter);
    }
}

// The star reminder: a row of the root shell above the status bar.

use crate::star_reminder::{self, ReminderState};

/// `app` with the star reminder shown, as after three successful analyses.
fn with_star_reminder(mut app: App) -> App {
    app.star_reminder.successful_analyses = star_reminder::FIRST_REMINDER_AFTER;
    app.star_reminder.state = ReminderState::Eligible;
    app
}

fn shell(harness: &Harness<'_, App>) -> super::ShellLayout {
    harness.state().shell.expect("a frame was drawn")
}

fn star_button<'a>(harness: &'a Harness<'_, App>) -> Node<'a> {
    harness.get_by_role_and_label(Role::Button, star_reminder::STAR_ACTION)
}

fn dont_ask_button<'a>(harness: &'a Harness<'_, App>) -> Node<'a> {
    harness.get_by_role_and_label(Role::Button, star_reminder::DONT_ASK_ACTION)
}

fn close_reminder_button<'a>(harness: &'a Harness<'_, App>) -> Node<'a> {
    harness.get_by_role_and_label(Role::Button, star_reminder::CLOSE_ACTION)
}

/// The row is reserved between the content and the status bar, right of the
/// rail, and the content area ends above it: nothing is drawn over anything.
fn assert_reminder_reserves_its_row(harness: &Harness<'_, App>, size: egui::Vec2) {
    let shell = shell(harness);
    let row = shell.star_reminder.expect("the reminder is shown");
    assert!(
        row.bottom() <= shell.status.top() + 0.5,
        "{row:?} {shell:?}"
    );
    assert!(
        shell.central.bottom() <= row.top() + 0.5,
        "{row:?} {shell:?}"
    );
    assert!(
        row.left() >= shell.navigation.right() - 0.5,
        "{row:?} {shell:?}"
    );
    assert!(row.right() <= size.x + 0.5, "{row:?}");
    for button in [
        star_button(harness),
        dont_ask_button(harness),
        close_reminder_button(harness),
    ] {
        let b = bounds(&button);
        assert!(row.expand(0.5).contains_rect(b), "{b:?} outside {row:?}");
    }
    harness.get_by_label(star_reminder::HEADING);
    harness.get_by_label(star_reminder::BODY);
    // The status bar is unchanged below the row.
    // The status bar's text (Packets' header also names the packet size).
    let status = harness
        .query_all_by_label_contains("-byte packets")
        .map(|node| bounds(&node))
        .max_by(|a, b| a.top().total_cmp(&b.top()))
        .expect("the status bar");
    assert!(status.top() >= row.bottom() - 0.5, "{status:?} {row:?}");
}

/// Shown from every screen and from the Detailed inspection page, always in
/// the same reserved row, never over the screen's content.
#[test]
fn star_reminder_is_shown_on_every_screen() {
    for screen in Screen::ANALYSIS {
        let mut app = with_star_reminder(analyzed_app());
        app.screen = screen;
        let harness = harness(app);
        assert_reminder_reserves_its_row(&harness, WIDE);
    }
    let mut harness = packets_screen(WIDE);
    harness.state_mut().star_reminder = with_star_reminder(App::default()).star_reminder;
    harness.run();
    detailed_inspection_buttons(&harness)[0].click();
    harness.run();
    assert_eq!(pages_mut(&mut harness).inspected_for_test(), Some(564));
    harness.get_by_label("Packet #4 of 40");
    assert_reminder_reserves_its_row(&harness, WIDE);
}

/// Wide windows keep the row to one line with the actions on the right;
/// narrow ones wrap it inside the reserved row, and the content area shrinks
/// by the row instead of being covered.
#[test]
fn star_reminder_wraps_in_narrow_windows() {
    for size in [WIDE, egui::vec2(820.0, 640.0), NARROW] {
        let mut app = analyzed_app();
        app.screen = Screen::Packets;
        let hidden = shell(&harness_sized(app, size));
        assert!(hidden.star_reminder.is_none());

        let mut app = with_star_reminder(analyzed_app());
        app.screen = Screen::Packets;
        let harness = harness_sized(app, size);
        assert_reminder_reserves_its_row(&harness, size);
        let shown = shell(&harness);
        let row = shown.star_reminder.unwrap();
        assert!((shown.central.bottom() - (hidden.central.bottom() - row.height())).abs() < 1.0);
        assert_eq!(shown.status, hidden.status);
        let heading = bounds(&harness.get_by_label(star_reminder::HEADING));
        let star = bounds(&star_button(&harness));
        if size == WIDE {
            assert!(
                star.top() < heading.bottom(),
                "one row: {heading:?} {star:?}"
            );
            assert!(star.left() > bounds(&harness.get_by_label(star_reminder::BODY)).right());
        } else {
            assert!(
                star.top() >= heading.bottom() - 0.5,
                "wrapped: {heading:?} {star:?}"
            );
        }
    }
}

/// Appearing does not take the keyboard focus; the actions follow each
/// other in Tab order and work with the keyboard.
#[test]
fn star_reminder_keeps_the_focus_and_works_with_the_keyboard() {
    let mut harness = packets_screen(WIDE);
    harness.state_mut().star_reminder.successful_analyses = 2;
    harness.run();
    assert!(harness.query_by_label(star_reminder::HEADING).is_none());
    tab(&harness, Screen::Bitrate).focus();
    harness.run();
    assert!(tab(&harness, Screen::Bitrate).is_focused());

    harness
        .state_mut()
        .star_reminder
        .record_successful_analysis();
    harness.run();
    harness.get_by_label(star_reminder::HEADING);
    assert!(tab(&harness, Screen::Bitrate).is_focused());
    assert!(!star_button(&harness).is_focused());

    star_button(&harness).focus();
    harness.run();
    harness.key_press(egui::Key::Tab);
    harness.run();
    assert!(dont_ask_button(&harness).is_focused());
    harness.key_press(egui::Key::Tab);
    harness.run();
    assert!(close_reminder_button(&harness).is_focused());
    harness.key_press(egui::Key::Enter);
    harness.run();
    assert!(harness.query_by_label(star_reminder::HEADING).is_none());
    assert_eq!(
        harness.state().star_reminder.state,
        ReminderState::Deferred {
            until: 3 + star_reminder::REMIND_AGAIN_AFTER
        }
    );
}

/// Records the URLs the app opens, answering with `result`.
fn recording_opener(
    app: &mut App,
    result: Result<(), String>,
) -> Arc<std::sync::Mutex<Vec<String>>> {
    let opened = Arc::new(std::sync::Mutex::new(Vec::new()));
    let record = Arc::clone(&opened);
    app.open_url = Box::new(move |url| {
        record.lock().unwrap().push(url.to_owned());
        result.clone()
    });
    opened
}

#[test]
fn star_on_github_opens_the_repository_and_completes() {
    let mut app = with_star_reminder(analyzed_app());
    let opened = recording_opener(&mut app, Ok(()));
    let mut harness = harness(app);
    star_button(&harness).click();
    harness.run();
    assert_eq!(*opened.lock().unwrap(), [star_reminder::REPOSITORY_URL]);
    assert_eq!(
        harness.state().star_reminder.state,
        ReminderState::Completed
    );
    assert!(harness.query_by_label(star_reminder::HEADING).is_none());
    assert!(shell(&harness).star_reminder.is_none());
}

/// A browser that does not open: the row stays with the reason and the
/// address, and a later attempt that works completes it.
#[test]
fn star_on_github_stays_when_the_browser_does_not_open() {
    let mut app = with_star_reminder(analyzed_app());
    let opened = recording_opener(&mut app, Err("no browser".to_owned()));
    let mut harness = harness(app);
    star_button(&harness).click();
    harness.run();
    assert_eq!(opened.lock().unwrap().len(), 1);
    assert_eq!(harness.state().star_reminder.state, ReminderState::Eligible);
    harness.get_by_label_contains("The browser did not open (no browser)");
    harness.get_by_label(star_reminder::REPOSITORY_URL);
    assert_reminder_reserves_its_row(&harness, WIDE);

    let opened = recording_opener(harness.state_mut(), Ok(()));
    star_button(&harness).click();
    harness.run();
    assert_eq!(opened.lock().unwrap().len(), 1);
    assert_eq!(
        harness.state().star_reminder.state,
        ReminderState::Completed
    );
    assert!(
        harness
            .query_by_label_contains("The browser did not open")
            .is_none()
    );
}

#[test]
fn dont_ask_again_completes_without_opening_anything() {
    let mut app = with_star_reminder(analyzed_app());
    let opened = recording_opener(&mut app, Ok(()));
    let mut harness = harness(app);
    dont_ask_button(&harness).click();
    harness.run();
    assert!(opened.lock().unwrap().is_empty());
    assert_eq!(
        harness.state().star_reminder.state,
        ReminderState::Completed
    );
    for _ in 0..50 {
        harness
            .state_mut()
            .star_reminder
            .record_successful_analysis();
    }
    harness.run();
    assert!(harness.query_by_label(star_reminder::HEADING).is_none());
}

/// Close hides the row until five more analyses, every time.
#[test]
fn close_brings_the_reminder_back_after_five_analyses() {
    let mut app = with_star_reminder(analyzed_app());
    let opened = recording_opener(&mut app, Ok(()));
    let mut harness = harness(app);
    for _ in 0..3 {
        close_reminder_button(&harness).click();
        harness.run();
        assert!(harness.query_by_label(star_reminder::HEADING).is_none());
        for _ in 0..star_reminder::REMIND_AGAIN_AFTER - 1 {
            harness
                .state_mut()
                .star_reminder
                .record_successful_analysis();
            harness.run();
            assert!(harness.query_by_label(star_reminder::HEADING).is_none());
        }
        harness
            .state_mut()
            .star_reminder
            .record_successful_analysis();
        harness.run();
        harness.get_by_label(star_reminder::HEADING);
    }
    assert!(opened.lock().unwrap().is_empty());
}

/// The breathing highlight repaints only while the row is shown; hidden, the
/// app is idle. (Tests draw it without the highlight unless asked.)
#[test]
fn star_reminder_repaints_only_while_shown() {
    let repaint_delay = |harness: &Harness<'_, App>| {
        harness.output().viewport_output[&egui::ViewportId::ROOT].repaint_delay
    };
    // Animations off (egui_kittest's default, egui's reduced motion): a
    // static highlight, and no repaints.
    let mut app = with_star_reminder(analyzed_app());
    app.star_pulse = true;
    let mut harness = harness(app);
    harness.step();
    assert_eq!(repaint_delay(&harness), std::time::Duration::MAX);

    let mut app = analyzed_app();
    app.star_pulse = true;
    let mut harness = self::harness(app);
    // Animations on: the highlight breathes.
    harness
        .ctx
        .all_styles_mut(|style| style.animation_time = 1.0 / 12.0);
    harness.run();
    assert_eq!(repaint_delay(&harness), std::time::Duration::MAX);

    harness.state_mut().star_reminder = with_star_reminder(App::default()).star_reminder;
    harness.run();
    harness.step();
    let delay = repaint_delay(&harness);
    assert!(delay <= star_reminder::PULSE_FRAME, "{delay:?}");

    dont_ask_button(&harness).click();
    harness.run();
    harness.step();
    assert_eq!(repaint_delay(&harness), std::time::Duration::MAX);
}
