//! The README screenshots, rendered from fixtures with egui_kittest's wgpu
//! renderer: `docs/images/gui-tabs.png` (Overview, Timeline, Bitrate and
//! Packets in a 2×2 grid), `docs/images/gui-byte-inspector.png` and
//! `docs/images/gui-bit-inspector.png` (the Byte and Bit inspectors with the
//! 13-bit PID selected) and `docs/images/gui-wire-layout.png` (the detailed
//! inspection of a PMT packet with its PID selected).
//!
//! `make check-gui-screenshots` renders them and compares them with the
//! committed images; `make gui-screenshots` (`UPDATE_SNAPSHOTS=1`) writes them.
//! Comparison output (`.new.png`, `.diff.png`) goes to `gui/target/screenshots`.
//! The canonical renderer is macOS (the `gui-screenshots` CI job).

use std::path::{Path, PathBuf};

use eframe::egui;
use egui_kittest::{Harness, SnapshotOptions};
use image::{Rgba, RgbaImage, imageops};

use super::App;
use super::ui_tests::{analyzed_app, packets_page, pmt_packet_detail};
use crate::views::Screen;
use crate::views::packet_pages::PacketPages;
use crate::views::packet_pages::{Inspector, show_inspector_for_test};
use crate::views::wire_layout::Selection;

/// Logical size of each tab. After cropping each tab's margin, the grid is
/// 2588×1628 pixels.
const TAB_SIZE: egui::Vec2 = egui::vec2(1280.0, 800.0);
/// Space between and around the tabs of the grid.
const GUTTER: u32 = 20;
/// Logical size of each inspector close-up, rendered at two pixels per point.
const BYTE_INSPECTOR_SIZE: egui::Vec2 = egui::vec2(540.0, 330.0);
const BIT_INSPECTOR_SIZE: egui::Vec2 = egui::vec2(540.0, 150.0);

fn render_tab(screen: Screen) -> RgbaImage {
    let mut app = analyzed_app();
    app.screen = screen;
    // Packets shows program 2's PMT packet with its PID selected.
    let mut pages = PacketPages::loaded_for_test(packets_page(), 564, pmt_packet_detail());
    pages.select_field_for_test("PID");
    app.session_mut().packet_pages = pages;
    let mut harness = Harness::builder()
        .with_size(TAB_SIZE)
        .with_pixels_per_point(1.0)
        .with_theme(egui::Theme::Dark)
        .with_context_setup(crate::theme::apply_with_bundled_fonts)
        .wgpu()
        .build_ui_state(|ui, app: &mut App| app.show(ui), app);
    harness.run();
    without_margin(harness.render().expect("render the tab"), 1.0)
}

/// The image without the transparent margin of eight points the harness
/// leaves around the ui.
fn without_margin(image: RgbaImage, pixels_per_point: f32) -> RgbaImage {
    let margin = (8.0 * pixels_per_point) as u32;
    let (width, height) = image.dimensions();
    imageops::crop_imm(
        &image,
        margin,
        margin,
        width - 2 * margin,
        height - 2 * margin,
    )
    .to_image()
}

/// Overview and Timeline on the first row, Bitrate and Packets on the second.
fn tabs_grid() -> RgbaImage {
    let tiles = Screen::ANALYSIS.map(render_tab);
    let (width, height) = tiles[0].dimensions();
    let background = Rgba([0x10, 0x10, 0x12, 0xff]);
    let mut grid =
        RgbaImage::from_pixel(2 * width + 3 * GUTTER, 2 * height + 3 * GUTTER, background);
    for (index, tile) in tiles.iter().enumerate() {
        assert_eq!(tile.dimensions(), (width, height), "tabs differ in size");
        let (column, row) = (index as u32 % 2, index as u32 / 2);
        let x = GUTTER + column * (width + GUTTER);
        let y = GUTTER + row * (height + GUTTER);
        imageops::overlay(&mut grid, tile, i64::from(x), i64::from(y));
    }
    grid
}

fn inspector(inspector: Inspector, size: egui::Vec2) -> RgbaImage {
    let detail = pmt_packet_detail();
    let mut harness = Harness::builder()
        .with_size(size)
        .with_pixels_per_point(2.0)
        .with_theme(egui::Theme::Dark)
        .with_context_setup(crate::theme::apply_with_bundled_fonts)
        .wgpu()
        .build_ui(|ui| {
            egui::CentralPanel::default().show(ui, |ui| {
                crate::theme::compact(ui);
                show_inspector_for_test(ui, &detail, "PID", inspector);
            });
        });
    harness.run();
    without_margin(harness.render().expect("render the inspectors"), 2.0)
}

/// Packets showing the detailed inspection of program 2's PMT packet at
/// offset 564, with its 13-bit PID (across bytes 1 and 2) selected.
pub(super) fn wire_layout_app() -> App {
    let mut app = analyzed_app();
    app.screen = Screen::Packets;
    let mut pages = PacketPages::loaded_for_test(packets_page(), 564, pmt_packet_detail());
    pages.inspect_for_test(Some(Selection::Part("ts_header.pid@11".to_owned())));
    app.session_mut().packet_pages = pages;
    app
}

fn wire_layout(size: egui::Vec2) -> RgbaImage {
    let mut harness = Harness::builder()
        .with_size(size)
        .with_pixels_per_point(1.0)
        .with_theme(egui::Theme::Dark)
        .with_context_setup(crate::theme::apply_with_bundled_fonts)
        .wgpu()
        .build_ui_state(|ui, app: &mut App| app.show(ui), wire_layout_app());
    harness.run();
    without_margin(harness.render().expect("render the wire layout"), 1.0)
}

fn manifest_dir() -> &'static Path {
    Path::new(env!("CARGO_MANIFEST_DIR"))
}

/// Compares an image with `docs/images/<name>.png`, or replaces that file when
/// `UPDATE_SNAPSHOTS` is set. egui_kittest does the comparison in
/// `gui/target/screenshots`, so its `.new.png` and `.diff.png` files stay out
/// of `docs/images`.
fn check(image: &RgbaImage, name: &str) -> Result<(), String> {
    let committed = manifest_dir().join(format!("../docs/images/{name}.png"));
    let work: PathBuf = manifest_dir().join("target/screenshots");
    std::fs::create_dir_all(&work).expect("create target/screenshots");
    let baseline = work.join(format!("{name}.png"));
    if committed.exists() {
        std::fs::copy(&committed, &baseline).expect("copy the committed image");
    } else if baseline.exists() {
        std::fs::remove_file(&baseline).expect("remove a stale baseline");
    }
    let result = egui_kittest::try_image_snapshot_options(
        image,
        name,
        &SnapshotOptions::default().output_path(&work),
    );
    if std::env::var_os("UPDATE_SNAPSHOTS").is_some() {
        std::fs::copy(&baseline, &committed).expect("write the image to docs/images");
    }
    result.map_err(|err| {
        format!(
            "docs/images/{name}.png is out of date ({err}). The new rendering is \
             gui/target/screenshots/{name}.new.png."
        )
    })
}

#[test]
fn readme_screenshots_are_current() {
    let errors: Vec<String> = [
        (tabs_grid(), "gui-tabs"),
        (
            inspector(Inspector::Byte, BYTE_INSPECTOR_SIZE),
            "gui-byte-inspector",
        ),
        (
            inspector(Inspector::Bit, BIT_INSPECTOR_SIZE),
            "gui-bit-inspector",
        ),
        (wire_layout(TAB_SIZE), "gui-wire-layout"),
    ]
    .iter()
    .filter_map(|(image, name)| check(image, name).err())
    .collect();
    assert!(
        errors.is_empty(),
        "{}\nRun `make gui-screenshots` on macOS and commit the images.",
        errors.join("\n")
    );
}
