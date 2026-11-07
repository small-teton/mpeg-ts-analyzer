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
        cli,
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
    app.analyzed = Some((0, app.file_size));
    app.analysis = Some(Box::new(report));
    app.packet_pages = Some(PacketPages::loaded_for_test(
        packets_page(),
        188,
        pat_packet_detail(),
    ));
    app.analysis_generation = 1;
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

    harness.state_mut().analyzed = Some((0, harness.state().range.limit));
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
        cli: Ok(checked_cli()),
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
    app.packet_pages = Some(PacketPages::loaded_for_test(packets_page(), offset, detail));
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

/// The CLI's analysis of `sample_data/sample_188byte_all_findings.ts`, in
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
        let report = harness.state().analysis.as_deref().map(std::ptr::from_ref);
        let plots = harness.state().plots.as_ref().map(|p| p.timelines.as_ptr());
        let generation = harness.state().analysis_generation;

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
        let state = harness.state();
        assert_eq!(state.screen, Screen::Overview);
        assert_eq!(state.analysis_generation, generation);
        assert_eq!(state.analysis.as_deref().map(std::ptr::from_ref), report);
        assert_eq!(state.plots.as_ref().map(|p| p.timelines.as_ptr()), plots);
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
    harness.state_mut().analysis_generation += 1;
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
                harness.state().analysis.as_ref().unwrap(),
                &harness.state().analysis.as_ref().unwrap().programs[0],
                check,
            )
            .unwrap();
            harness.get_by_role_and_label(Role::Heading, help.title);
            harness.get_by_label(&format!(
                "Status: {}",
                crate::views::overview::status_label(&row.status)
            ));
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
                    crate::views::overview::status_label(status)
                );
                assert_eq!(
                    node.accesskit_node().label().as_deref(),
                    Some(label.as_str())
                );
                assert_eq!(
                    node.accesskit_node().is_selected(),
                    Some(*status == row.status),
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
    app.cli = Ok(Cli {
        packet_outputs: false,
        ..checked_cli()
    });
    let mut harness = harness(app);
    harness.get_by_label("Findings (7)");
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
            .packet_pages
            .as_ref()
            .unwrap()
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
    // jump), and the two PID-reference findings.
    harness.get_by_label("Findings (12)");
    assert!(harness.query_by_label("None.").is_none());
    for check in Check::ALL {
        let links = if check == Check::PcrDiscontinuities {
            2
        } else {
            1
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
        harness.get_by_label("Findings (7)");
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
    // Still seven findings (the fixture's), and nothing unavailable.
    harness.get_by_label("Findings (7)");
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
    let Some(json) = crate::cli::run_on_sample("sample_188byte_pcr_discontinuities.ts", &args)
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
    let Some(json) = crate::cli::run_on_sample("sample_188byte_all_findings.ts", &args) else {
        return;
    };
    let fixture: serde_json::Value = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_all_findings_188.json"
    ))
    .unwrap();
    let mut output: serde_json::Value = serde_json::from_slice(&json).unwrap();
    output["input"]["path"] = fixture["input"]["path"].clone();
    assert_eq!(output, fixture, "regenerate the fixture with the CLI");

    let m2ts = crate::cli::run_on_sample("sample_192byte_all_findings.ts", &args).unwrap();
    let m2ts = crate::report::parse_analysis(&m2ts, "2").unwrap();
    let report = all_findings_report();
    let summary = |report: &AnalysisReport| -> Vec<(String, String)> {
        crate::views::overview::findings(report, &report.programs[0])
            .0
            .into_iter()
            .map(|f| (f.status, f.label))
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
    let Some(json) = crate::cli::run_on_sample("sample_188byte_pid_references.ts", &args) else {
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

#[test]
fn sync_loss_fixture_matches_the_cli() {
    let args = ["--format", "json", "--output", "analysis"];
    let Some(json) = crate::cli::run_on_sample("sample_188byte_sync_loss.ts", &args) else {
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
