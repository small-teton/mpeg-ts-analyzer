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
use crate::range_bar::AnalysisRange;
use crate::report::{AnalysisReport, PacketDetail, PacketsPage};
use crate::settings::Settings;
use crate::views::Screen;
use crate::views::metric_help::Metric;
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
fn app_with_report(report: AnalysisReport) -> App {
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
/// which every compliance card reports a problem.
fn all_findings_report() -> AnalysisReport {
    serde_json::from_str(include_str!(
        "../../tests/fixtures/analysis_all_findings_188.json"
    ))
    .expect("all-findings fixture")
}

fn info_button<'a>(harness: &'a Harness<'_, App>, metric: Metric) -> Node<'a> {
    let name = metric.about();
    harness.get_by(move |node| node.role() == Role::Button && node.label() == Some(name.clone()))
}

/// The title label of a compliance card.
fn card_title<'a>(harness: &'a Harness<'_, App>, metric: Metric) -> Node<'a> {
    harness.get_by_role_and_label(Role::Label, metric.title())
}

fn bounds(node: &Node<'_>) -> egui::Rect {
    let b = node.accesskit_node().bounding_box().expect("bounds");
    egui::Rect::from_min_max(
        egui::pos2(b.x0 as f32, b.y0 as f32),
        egui::pos2(b.x1 as f32, b.y1 as f32),
    )
}

#[test]
fn every_card_has_an_information_button_with_an_explanation() {
    let mut harness = harness(app_with_report(all_findings_report()));
    for metric in Metric::ALL {
        info_button(&harness, metric).click();
        harness.run();
        assert_eq!(harness.state().overview.help, Some(metric));
        harness.get_by_role_and_label(Role::Window, metric.title());
        harness.get_by_label(metric.help().measures);
        harness.get_by_label(metric.help().limitations);
        harness.key_press(egui::Key::Escape);
        harness.run();
        assert_eq!(harness.state().overview.help, None);
    }
}

#[test]
fn help_opens_and_closes_without_changing_the_analysis() {
    let mut app = app_with_report(all_findings_report());
    app.program = 0;
    let mut harness = harness(app);
    let generation = harness.state().analysis_generation;
    let unchanged = |harness: &Harness<'_, App>| {
        let state = harness.state();
        assert_eq!(state.screen, Screen::Overview);
        assert_eq!(state.program, 0);
        assert_eq!(state.analysis_generation, generation);
        assert!(state.analysis.is_some());
    };

    // Mouse, then the window's close button.
    info_button(&harness, Metric::PcrJitter).click();
    harness.run();
    assert_eq!(harness.state().overview.help, Some(Metric::PcrJitter));
    harness.get_by_role_and_label(Role::Window, "PCR jitter");
    unchanged(&harness);
    harness
        .get_by_role_and_label(Role::Button, "Close window")
        .click();
    harness.run();
    assert_eq!(harness.state().overview.help, None);
    assert!(
        harness
            .query_by_role_and_label(Role::Window, "PCR jitter")
            .is_none()
    );

    // Keyboard, then Escape.
    info_button(&harness, Metric::MaxPcrInterval).focus();
    harness.run();
    harness.key_press(egui::Key::Enter);
    harness.run();
    assert_eq!(harness.state().overview.help, Some(Metric::MaxPcrInterval));
    harness.key_press(egui::Key::Escape);
    harness.run();
    assert_eq!(harness.state().overview.help, None);
    unchanged(&harness);

    // A click outside the window.
    info_button(&harness, Metric::ContinuityCounter).click();
    harness.run();
    assert_eq!(
        harness.state().overview.help,
        Some(Metric::ContinuityCounter)
    );
    harness.get_by_label("Elementary streams").click();
    harness.run();
    assert_eq!(harness.state().overview.help, None);
    unchanged(&harness);

    // Its link opens the detailed view.
    info_button(&harness, Metric::PcrJitter).click();
    harness.run();
    harness
        .get_by_role_and_label(Role::Button, "Open Bitrate")
        .scroll_to_me();
    harness.run();
    harness
        .get_by_role_and_label(Role::Button, "Open Bitrate")
        .click();
    harness.run();
    assert_eq!(harness.state().screen, Screen::Bitrate);
    assert_eq!(harness.state().overview.help, None);
}

/// Wide, medium and narrow windows, and one narrower than a card's
/// preferred width: the cards wrap to more rows in their order, and every
/// title and information button is inside the window without overlapping.
#[test]
fn cards_reflow_in_order_and_stay_visible() {
    for (size, columns) in [
        (egui::vec2(1280.0, 800.0), 5),
        (egui::vec2(860.0, 800.0), 3),
        (egui::vec2(480.0, 900.0), 1),
        (egui::vec2(300.0, 900.0), 1),
    ] {
        let mut harness = harness_sized(app_with_report(all_findings_report()), size);
        let titles: Vec<egui::Rect> = Metric::ALL
            .iter()
            .map(|m| bounds(&card_title(&harness, *m)))
            .collect();
        let first_row = titles
            .iter()
            .filter(|r| (r.top() - titles[0].top()).abs() < 1.0)
            .count();
        assert_eq!(first_row, columns, "{size:?}");
        for pair in titles.windows(2) {
            let same_row = (pair[1].top() - pair[0].top()).abs() < 1.0;
            assert!(
                pair[1].top() > pair[0].bottom() || (same_row && pair[1].left() > pair[0].right()),
                "{size:?}: cards out of order"
            );
        }
        let window = egui::Rect::from_min_size(egui::pos2(96.0, 0.0), size - egui::vec2(96.0, 0.0));
        for metric in Metric::ALL {
            card_title(&harness, metric).scroll_to_me();
            harness.run();
            let title = bounds(&card_title(&harness, metric));
            let button = bounds(&info_button(&harness, metric));
            assert!(
                window.contains_rect(title),
                "{size:?} {metric:?} title {title:?}"
            );
            assert!(
                window.contains_rect(button),
                "{size:?} {metric:?} button {button:?}"
            );
            assert!(!title.intersects(button), "{size:?} {metric:?} overlaps");
        }
    }
}

#[test]
fn findings_account_for_every_card_that_is_not_ok() {
    // Every card of the all-findings sample reports a problem.
    let mut harness = harness(app_with_report(all_findings_report()));
    harness.get_by_label("Findings (5)");
    assert!(harness.query_by_label("None.").is_none());
    assert_eq!(
        harness
            .query_all_by_role_and_label(Role::Link, "Continuity counter")
            .count(),
        1
    );
    let link = |harness: &Harness<'_, App>| {
        harness
            .get_by_role_and_label(Role::Link, "PCR jitter (estimate)")
            .scroll_to_me();
    };
    link(&harness);
    harness.run();
    harness
        .get_by_role_and_label(Role::Link, "PCR jitter (estimate)")
        .click();
    harness.run();
    assert_eq!(harness.state().overview.help, Some(Metric::PcrJitter));

    // The two-program sample is VBR: PCR jitter is not assessable, which is
    // unavailable analysis with the CLI's reason, not a finding.
    let harness = self::harness(analyzed_app());
    harness.get_by_label("Findings (0)");
    harness.get_by_label("Unavailable analysis (1)");
    // The badge of the card and of the unavailable analysis.
    assert_eq!(harness.query_all_by_label("NOT ASSESSABLE").count(), 2);
    harness.get_by_role_and_label(Role::Link, "PCR jitter");
    harness.get_by_label_contains("Delivery variable: the estimated jitter reaches 55.082ms");
    assert!(
        harness
            .query_by_role_and_label(Role::Link, "PCR jitter (estimate)")
            .is_none()
    );

    // Every card OK: no finding.
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

    // A skipped card is unavailable analysis, not a finding.
    clean["programs"][0]["pcr_jitter"]["status"] = "skipped".into();
    clean["programs"][0]["pcr_jitter"]["reason"] = "need at least 3 PCR samples".into();
    let harness = self::harness(app_with_report(serde_json::from_value(clean).unwrap()));
    harness.get_by_label("Findings (0)");
    harness.get_by_label("Unavailable analysis (1)");
    harness.get_by_role_and_label(Role::Link, "PCR jitter");
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
