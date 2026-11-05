//! Semantic UI tests: the window is driven through its accessibility tree with
//! egui_kittest, without a native window, a file dialog or a CLI process.

use std::path::{Path, PathBuf};

use eframe::egui::{
    self,
    accesskit::{Role, Toggled},
};
use egui_kittest::kittest::{NodeT, Queryable};
use egui_kittest::{Harness, Node};

use super::App;
use crate::cli::Cli;
use crate::report::{AnalysisReport, PacketDetail, PacketsPage};
use crate::settings::Settings;
use crate::views::Screen;
use crate::views::packet_pages::PacketPages;

/// A CLI that passed the capabilities check. It is never run.
fn checked_cli() -> Cli {
    Cli {
        path: PathBuf::from("/fixture/mpeg-ts-analyzer"),
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

/// An app showing the two-program analysis fixture, with the Packets screen
/// already holding a page and the decoded PAT packet at offset 188.
fn analyzed_app() -> App {
    let report: AnalysisReport = serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_two_programs_188.json"
    ))
    .expect("analysis fixture");
    let page: PacketsPage =
        serde_json::from_str(include_str!("../../tests/fixtures/packets_page_188.json"))
            .expect("packets fixture");
    let detail: PacketDetail = serde_json::from_str(include_str!(
        "../../tests/fixtures/packet_detail_pat_188.json"
    ))
    .expect("packet fixture");
    let mut app = app(Ok(checked_cli()));
    app.open(sample());
    app.analyzed = Some((app.range.offset, app.range.limit));
    app.analysis = Some(Box::new(report));
    app.packet_pages = Some(PacketPages::loaded_for_test(page, 188, detail));
    app.analysis_generation = 1;
    app
}

fn harness(app: App) -> Harness<'static, App> {
    let mut harness = Harness::builder()
        .with_size(egui::vec2(1280.0, 800.0))
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
