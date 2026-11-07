//! Phosphor icon glyphs (regular weight). Codepoints come from the
//! `@phosphor-icons/web` 2.1.2 stylesheet that ships with `assets/fonts/Phosphor.ttf`.

use eframe::egui::{FontFamily, FontId, RichText};

/// Font family holding only the Phosphor font; registered in `crate::theme`.
pub const FAMILY: &str = "icons";

pub const SQUARES_FOUR: &str = "\u{e464}";
pub const TIMER: &str = "\u{e492}";
pub const GAUGE: &str = "\u{e628}";
pub const LIST_MAGNIFYING_GLASS: &str = "\u{ebe0}";
pub const GEAR: &str = "\u{e270}";
pub const FOLDER_OPEN: &str = "\u{e256}";
pub const INFO: &str = "\u{e2ce}";
pub const ARROW_SQUARE_OUT: &str = "\u{e5de}";
pub const ARROW_CIRCLE_UP: &str = "\u{e030}";
pub const ARROW_COUNTER_CLOCKWISE: &str = "\u{e038}";
pub const ARROW_LEFT: &str = "\u{e058}";
pub const CARET_LEFT: &str = "\u{e138}";
pub const CARET_RIGHT: &str = "\u{e13a}";
pub const PLAY: &str = "\u{e3d0}";

pub fn font(size: f32) -> FontId {
    FontId::new(size, FontFamily::Name(FAMILY.into()))
}

/// An icon glyph to combine with a label, e.g. `ui.button((icons::text(GEAR), "Settings…"))`.
pub fn text(glyph: &str) -> RichText {
    RichText::new(glyph).family(FontFamily::Name(FAMILY.into()))
}
