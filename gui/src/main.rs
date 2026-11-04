// Hide the console window on Windows release builds.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod app;
mod cli;
mod icons;
mod plot_data;
mod psi;
mod range_bar;
mod report;
mod settings;
mod theme;
mod views;

use eframe::egui;

fn main() -> eframe::Result {
    if std::env::args().nth(1).as_deref() == Some("--check-cli") {
        // The CLI path from Settings is not read: packages are checked as
        // installed, before anyone has changed the settings.
        std::process::exit(if cli::report_check("") { 0 } else { 1 });
    }
    let mut viewport = egui::ViewportBuilder::default()
        .with_title("mpeg-ts-analyzer")
        // Matches the .desktop file of the Linux packages.
        .with_app_id("mpeg-ts-analyzer-gui")
        .with_inner_size([1280.0, 800.0])
        .with_drag_and_drop(true);
    if let Ok(icon) =
        eframe::icon_data::from_png_bytes(include_bytes!("../assets/icons/256x256.png"))
    {
        viewport = viewport.with_icon(icon);
    }
    let options = eframe::NativeOptions {
        viewport,
        ..Default::default()
    };
    eframe::run_native(
        "mpeg-ts-analyzer",
        options,
        Box::new(|cc| {
            theme::apply(&cc.egui_ctx);
            Ok(Box::new(app::App::new(cc)))
        }),
    )
}
