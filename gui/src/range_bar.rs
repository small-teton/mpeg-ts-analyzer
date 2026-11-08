//! The analysis range: a bar over the whole file to drag a byte range, plus
//! exact Offset / Limit fields. Both edit the same values (the CLI's
//! `--offset` / `--limit`).

use eframe::egui;

/// The range is kept in bytes. It is not snapped to packets: the packet size
/// (188, 192 or 204) is unknown until the CLI has read the file, and the CLI
/// synchronizes on packet boundaries itself. The minimum stays 192 bytes so
/// that short 188-byte and 192-byte inputs remain analyzable; a 204-byte range
/// that short holds no complete packet, which the CLI reports.
const MIN_LIMIT: u64 = 192;
const BAR_HEIGHT: f32 = 22.0;
/// How far from a handle (in pixels) a press still grabs it.
const EDGE_GRAB: f32 = 10.0;
const HANDLE_WIDTH: f32 = 8.0;
/// A small range is still drawn this wide so both handles stay reachable.
const MIN_VISUAL_WIDTH: f32 = 2.0 * EDGE_GRAB + 4.0;

pub struct AnalysisRange {
    pub offset: u64,
    pub limit: u64,
}

#[derive(Clone, Copy)]
enum Grab {
    Start,
    End,
    Body,
}

#[derive(Clone, Copy)]
struct Drag {
    grab: Grab,
    pointer_x: f32,
    offset: u64,
    limit: u64,
}

impl AnalysisRange {
    /// Keeps the range inside the file. A file shorter than one packet gets
    /// the whole file, which [`Self::is_analyzable`] then rejects.
    pub fn clamp(&mut self, file_size: u64) {
        if file_size < MIN_LIMIT {
            self.offset = 0;
            self.limit = file_size;
            return;
        }
        self.limit = self.limit.clamp(MIN_LIMIT, file_size);
        self.offset = self.offset.min(file_size - self.limit);
    }

    /// Whether the file can hold a packet at all.
    pub fn is_analyzable(file_size: u64) -> bool {
        file_size >= MIN_LIMIT
    }

    /// Draws the bar. Dragging a handle resizes the range, dragging the middle
    /// moves it, clicking elsewhere centers the range there, and the mouse
    /// wheel over the bar grows or shrinks the Limit.
    pub fn bar(&mut self, ui: &mut egui::Ui, width: f32, file_size: u64, load: Load) {
        let (rect, response) =
            ui.allocate_exact_size(egui::vec2(width, BAR_HEIGHT), egui::Sense::click_and_drag());
        let size = file_size.max(1) as f32;
        let bytes_per_px = size / rect.width();
        let x0 = rect.left() + self.offset as f32 / bytes_per_px;
        let x1 = (rect.left() + (self.offset + self.limit) as f32 / bytes_per_px)
            .max(x0 + MIN_VISUAL_WIDTH)
            .min(rect.right());
        let x0 = x0.min(x1 - MIN_VISUAL_WIDTH).max(rect.left());

        // The nearer handle wins, so a narrow range can still be resized from either end.
        let grab_at = |x: f32| {
            let (to_start, to_end) = ((x - x0).abs(), (x - x1).abs());
            if to_start.min(to_end) <= EDGE_GRAB {
                Some(if to_end <= to_start {
                    Grab::End
                } else {
                    Grab::Start
                })
            } else if (x0..=x1).contains(&x) {
                Some(Grab::Body)
            } else {
                None
            }
        };

        let id = response.id;
        if response.drag_started()
            && let Some(origin) = ui.input(|i| i.pointer.press_origin())
            && let Some(grab) = grab_at(origin.x)
        {
            let drag = Drag {
                grab,
                pointer_x: origin.x,
                offset: self.offset,
                limit: self.limit,
            };
            ui.data_mut(|data| data.insert_temp(id, drag));
        }
        if response.drag_stopped() {
            ui.data_mut(|data| data.remove::<Drag>(id));
        }
        if response.dragged()
            && let (Some(drag), Some(pointer)) = (
                ui.data(|data| data.get_temp::<Drag>(id)),
                response.interact_pointer_pos(),
            )
        {
            let delta = ((pointer.x - drag.pointer_x) * bytes_per_px) as i64;
            let shift = |value: u64| value.saturating_add_signed(delta);
            match drag.grab {
                Grab::Body => self.offset = shift(drag.offset),
                Grab::End => self.limit = shift(drag.limit),
                Grab::Start => {
                    let end = drag.offset + drag.limit;
                    self.offset = shift(drag.offset).min(end.saturating_sub(MIN_LIMIT));
                    self.limit = end - self.offset;
                }
            }
            self.clamp(file_size);
        } else if response.clicked()
            && let Some(pointer) = response.interact_pointer_pos()
            && grab_at(pointer.x).is_none()
        {
            let center = ((pointer.x - rect.left()) * bytes_per_px) as u64;
            self.offset = center.saturating_sub(self.limit / 2);
            self.clamp(file_size);
        }
        if response.hovered() {
            let scroll = ui.input(|i| i.smooth_scroll_delta.y);
            if scroll != 0.0 {
                let factor = (1.0 + f64::from(scroll) * 0.004).max(0.1);
                self.limit = (self.limit as f64 * factor) as u64;
                self.clamp(file_size);
            }
        }

        let visuals = ui.visuals();
        let painter = ui.painter();
        painter.rect_filled(rect, 4, visuals.extreme_bg_color);
        let selected = egui::Rect::from_x_y_ranges(x0..=x1, rect.y_range());
        painter.rect_filled(selected, 4, load.color().gamma_multiply(0.5));
        let hover_grab = ui
            .input(|i| i.pointer.hover_pos())
            .filter(|p| rect.expand(4.0).contains(*p))
            .and_then(|p| grab_at(p.x));
        let active_grab = ui.data(|data| data.get_temp::<Drag>(id)).map(|d| d.grab);
        for (x, grab) in [(x0, Grab::Start), (x1, Grab::End)] {
            let hot = matches!(
                (active_grab.or(hover_grab), grab),
                (Some(Grab::Start), Grab::Start) | (Some(Grab::End), Grab::End)
            );
            let handle = egui::Rect::from_center_size(
                egui::pos2(x, rect.center().y),
                egui::vec2(HANDLE_WIDTH + if hot { 2.0 } else { 0.0 }, BAR_HEIGHT + 6.0),
            );
            painter.rect_filled(handle, 3, load.color());
            painter.rect_stroke(
                handle,
                3,
                egui::Stroke::new(1.0, visuals.strong_text_color()),
                egui::StrokeKind::Inside,
            );
        }
        // Size of the range in the middle of the bar, drawn after the fill so it stays readable.
        painter.text(
            rect.center(),
            egui::Align2::CENTER_CENTER,
            format!("{} of {}", human_size(self.limit), human_size(file_size)),
            crate::theme::caption_font(),
            visuals.strong_text_color(),
        );
        match hover_grab {
            Some(Grab::Start | Grab::End) => {
                ui.ctx().set_cursor_icon(egui::CursorIcon::ResizeHorizontal)
            }
            Some(Grab::Body) => ui.ctx().set_cursor_icon(egui::CursorIcon::Grab),
            None if response.hovered() => ui.ctx().set_cursor_icon(egui::CursorIcon::PointingHand),
            None => {}
        }
        response.on_hover_text(format!(
            "{} – {} of {} · {}\nDrag a handle to resize, the middle to move; click elsewhere to jump. \
             Scroll to grow or shrink the Limit.",
            bytes(self.offset),
            bytes(self.offset + self.limit),
            bytes(file_size),
            load.label()
        ));
    }

    /// Exact byte fields. Values accept decimal (commas allowed) or 0x-prefixed hex.
    pub fn fields(&mut self, ui: &mut egui::Ui, file_size: u64) {
        let before = (self.offset, self.limit);
        let label = ui.label("Offset");
        ui.add(byte_value(&mut self.offset, file_size))
            .labelled_by(label.id);
        let label = ui.label("Limit");
        ui.add(byte_value(&mut self.limit, file_size))
            .labelled_by(label.id);
        if (self.offset, self.limit) != before {
            self.clamp(file_size);
        }
    }
}

/// How heavy analyzing a range of a given size is, relative to the size above
/// which Start asks for confirmation.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Load {
    Light,
    Moderate,
    Heavy,
    VeryHeavy,
    NeedsConfirmation,
}

impl Load {
    pub fn of(limit: u64, confirm_above: u64) -> Self {
        let ratio = limit as f64 / confirm_above.max(1) as f64;
        match ratio {
            r if r > 1.0 => Load::NeedsConfirmation,
            r if r > 0.5 => Load::VeryHeavy,
            r if r > 0.25 => Load::Heavy,
            r if r > 0.125 => Load::Moderate,
            _ => Load::Light,
        }
    }

    /// Blue for light ranges, shifting through teal, yellow and orange to red.
    pub fn color(self) -> egui::Color32 {
        match self {
            Load::Light => crate::theme::LOAD_LIGHT,
            Load::Moderate => crate::theme::LOAD_MODERATE,
            Load::Heavy => crate::theme::LOAD_HEAVY,
            Load::VeryHeavy => crate::theme::LOAD_VERY_HEAVY,
            Load::NeedsConfirmation => crate::theme::LOAD_NEEDS_CONFIRMATION,
        }
    }

    pub fn label(self) -> &'static str {
        match self {
            Load::Light => "light",
            Load::Moderate => "moderate",
            Load::Heavy => "heavy",
            Load::VeryHeavy => "very heavy",
            Load::NeedsConfirmation => "large: Start asks to confirm",
        }
    }
}

fn byte_value(value: &mut u64, max: u64) -> egui::DragValue<'_> {
    egui::DragValue::new(value)
        .range(0..=max)
        .speed(4096.0)
        .custom_formatter(|v, _| bytes(v as u64))
        .custom_parser(|text| {
            let text = text.trim().trim_end_matches('B').trim().replace(',', "");
            match text.strip_prefix("0x").or_else(|| text.strip_prefix("0X")) {
                Some(hex) => u64::from_str_radix(hex, 16).ok().map(|v| v as f64),
                None => text.parse::<u64>().ok().map(|v| v as f64),
            }
        })
}

/// Approximate size in binary units, e.g. `64.0 MiB`.
pub fn human_size(value: u64) -> String {
    const UNITS: [&str; 4] = ["KiB", "MiB", "GiB", "TiB"];
    if value < 1024 {
        return format!("{value} B");
    }
    let mut size = value as f64 / 1024.0;
    let mut unit = 0;
    while size >= 1024.0 && unit + 1 < UNITS.len() {
        size /= 1024.0;
        unit += 1;
    }
    format!("{size:.1} {}", UNITS[unit])
}

/// Byte count with thousands separators, e.g. `12,345,678 B`.
pub fn bytes(value: u64) -> String {
    let digits = value.to_string();
    let mut out = String::new();
    for (i, c) in digits.chars().enumerate() {
        if i > 0 && (digits.len() - i).is_multiple_of(3) {
            out.push(',');
        }
        out.push(c);
    }
    out + " B"
}

#[cfg(test)]
mod tests {
    use super::*;

    const INITIAL_TEST_LIMIT: u64 = 64 * 1024 * 1024;

    #[test]
    fn clamp_keeps_range_inside_file() {
        let mut range = AnalysisRange {
            offset: 1_000,
            limit: 10_000_000,
        };
        range.clamp(5_000_000);
        assert_eq!(range.limit, 5_000_000);
        assert_eq!(range.offset, 0);

        let mut range = AnalysisRange {
            offset: 4_999_000,
            limit: 1_000_000,
        };
        range.clamp(5_000_000);
        assert!(range.offset + range.limit <= 5_000_000);
        assert_eq!(range.limit, 1_000_000);
    }

    #[test]
    fn clamp_keeps_tiny_files_whole() {
        for (file_size, analyzable) in [
            (0, false),
            (1, false),
            (187, false),
            (188, false),
            (191, false),
            (192, true),
        ] {
            let mut range = AnalysisRange {
                offset: 50,
                limit: INITIAL_TEST_LIMIT,
            };
            range.clamp(file_size);
            assert_eq!(
                (range.offset, range.limit),
                (0, file_size),
                "size {file_size}"
            );
            assert_eq!(AnalysisRange::is_analyzable(file_size), analyzable);
        }
    }

    #[test]
    fn load_steps_up_to_confirmation() {
        let gib = 1 << 30;
        assert_eq!(Load::of(gib / 16, gib), Load::Light);
        assert_eq!(Load::of(gib / 5, gib), Load::Moderate);
        assert_eq!(Load::of(gib / 3, gib), Load::Heavy);
        assert_eq!(Load::of(gib, gib), Load::VeryHeavy);
        assert_eq!(Load::of(gib + 1, gib), Load::NeedsConfirmation);
    }

    #[test]
    fn formats_bytes_with_separators() {
        assert_eq!(bytes(0), "0 B");
        assert_eq!(bytes(283_761_748), "283,761,748 B");
        assert_eq!(human_size(64 * 1024 * 1024), "64.0 MiB");
        assert_eq!(human_size(4_187_402_240), "3.9 GiB");
    }
}
