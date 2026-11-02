pub mod bitrate;
pub mod overview;
pub mod packets;
pub mod timeline;

use eframe::egui;

use crate::icons;
use crate::mock::Verdict;

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Screen {
    Overview,
    Timeline,
    Bitrate,
    Packets,
}

impl Screen {
    pub const ANALYSIS: [Screen; 4] = [
        Screen::Overview,
        Screen::Timeline,
        Screen::Bitrate,
        Screen::Packets,
    ];

    /// Phosphor glyphs; see `crate::icons`.
    pub fn icon(self) -> &'static str {
        match self {
            Screen::Overview => icons::SQUARES_FOUR,
            Screen::Timeline => icons::TIMER,
            Screen::Bitrate => icons::GAUGE,
            Screen::Packets => icons::LIST_MAGNIFYING_GLASS,
        }
    }

    pub fn title(self) -> &'static str {
        match self {
            Screen::Overview => "Overview",
            Screen::Timeline => "Timeline",
            Screen::Bitrate => "Bitrate",
            Screen::Packets => "Packets",
        }
    }
}

pub fn badge(ui: &mut egui::Ui, text: &str, verdict: Verdict) {
    let (foreground, background) = verdict.colors();
    egui::Frame::new()
        .fill(background)
        .corner_radius(2)
        .inner_margin(egui::Margin::symmetric(6, 1))
        .show(ui, |ui| {
            ui.label(egui::RichText::new(text).color(foreground).strong().small());
        });
}

/// Program choice shared by Overview, Timeline and Bitrate. Returns true when it changed.
pub fn program_selector(ui: &mut egui::Ui, selected: &mut usize) -> bool {
    let programs = crate::mock_programs::programs();
    let before = *selected;
    ui.label("Program");
    egui::ComboBox::from_id_salt(ui.scope_id().with("program"))
        .selected_text(programs[*selected].label())
        .show_ui(ui, |ui| {
            for (index, program) in programs.iter().enumerate() {
                ui.selectable_value(selected, index, program.label());
            }
        });
    ui.weak(format!("{} programs in this range", programs.len()));
    *selected != before
}
