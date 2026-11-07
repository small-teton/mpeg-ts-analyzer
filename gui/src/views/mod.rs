pub mod bitrate;
pub mod check_detail;
pub mod check_help;
pub mod overview;
pub mod packet_pages;
pub mod packets;
pub mod timeline;

use eframe::egui;
use egui_plot::PlotUi;

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

/// A colored status badge, exactly as large as its text, so it never
/// stretches with the row or cell around it. Announced as a label with its
/// text.
pub fn badge(ui: &mut egui::Ui, text: &str, verdict: Verdict) -> egui::Response {
    let (foreground, background) = verdict.colors();
    let galley = egui::WidgetText::from(egui::RichText::new(text).strong().small()).into_galley(
        ui,
        Some(egui::TextWrapMode::Extend),
        f32::INFINITY,
        egui::TextStyle::Small,
    );
    let margin = egui::vec2(6.0, 1.0);
    let (rect, response) =
        ui.allocate_exact_size(galley.size() + 2.0 * margin, egui::Sense::hover());
    if ui.is_rect_visible(rect) {
        let painter = ui.painter();
        painter.rect_filled(rect, 2, background);
        painter.galley(rect.min + margin, galley, foreground);
    }
    response.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Label, true, text));
    response
}

/// Program choice over the given labels. Returns true when it changed.
pub fn program_selector_labels(ui: &mut egui::Ui, labels: &[String], selected: &mut usize) -> bool {
    let before = *selected;
    *selected = (*selected).min(labels.len().saturating_sub(1));
    let title = ui.label("Program");
    egui::ComboBox::from_id_salt(ui.scope_id().with("program"))
        .truncate()
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

/// Keeps a time plot inside the analyzed interval while preserving its zoom.
///
/// egui_plot does not currently expose pan limits. Bounds set here are applied
/// on the next paint, so an out-of-range drag snaps back immediately instead
/// of leaving the user in empty space.
pub(crate) fn constrain_time_axis(plot: &mut PlotUi<'_>, allowed: (f64, f64)) {
    let bounds = plot.plot_bounds();
    let current = (bounds.min()[0], bounds.max()[0]);
    let constrained = constrain_range(current, allowed);
    if constrained != current {
        plot.set_plot_bounds_x(constrained.0..=constrained.1);
    }
}

fn constrain_range(current: (f64, f64), allowed: (f64, f64)) -> (f64, f64) {
    let width = current.1 - current.0;
    let allowed_width = allowed.1 - allowed.0;
    if !width.is_finite() || width <= 0.0 || width >= allowed_width {
        return allowed;
    }
    if current.0 < allowed.0 {
        return (allowed.0, allowed.0 + width);
    }
    if current.1 > allowed.1 {
        return (allowed.1 - width, allowed.1);
    }
    current
}

#[cfg(test)]
mod tests {
    use super::constrain_range;

    #[test]
    fn plot_range_stays_inside_the_analyzed_interval() {
        assert_eq!(constrain_range((-3.0, 2.0), (0.0, 10.0)), (0.0, 5.0));
        assert_eq!(constrain_range((8.0, 13.0), (0.0, 10.0)), (5.0, 10.0));
        assert_eq!(constrain_range((2.0, 7.0), (0.0, 10.0)), (2.0, 7.0));
        assert_eq!(constrain_range((-5.0, 15.0), (0.0, 10.0)), (0.0, 10.0));
    }
}
