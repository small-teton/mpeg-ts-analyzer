pub mod bitrate;
pub mod overview;
pub mod packet_pages;
pub mod packets;
pub mod timeline;

use eframe::egui;

use crate::icons;
use crate::theme::Verdict;

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
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

/// Program choice over the given labels. Returns true when it changed.
pub fn program_selector_labels(ui: &mut egui::Ui, labels: &[String], selected: &mut usize) -> bool {
    let before = *selected;
    *selected = (*selected).min(labels.len().saturating_sub(1));
    let title = ui.label("Program");
    egui::ComboBox::from_id_salt(ui.scope_id().with("program"))
        .selected_text(labels.get(*selected).map_or("none", String::as_str))
        .show_ui(ui, |ui| {
            for (index, label) in labels.iter().enumerate() {
                ui.selectable_value(selected, index, label);
            }
        })
        .response
        .labelled_by(title.id);
    ui.weak(format!("{} programs in this range", labels.len()));
    *selected != before
}

/// Badge colors for a status string from the CLI.
pub fn status_verdict(status: &str) -> Verdict {
    match status {
        "ok" | "complete" => Verdict::Ok,
        "ng" | "errors" | "failed" => Verdict::Ng,
        _ => Verdict::Warn,
    }
}
