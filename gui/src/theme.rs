use std::path::PathBuf;
use std::sync::Arc;

use eframe::egui::{self, Color32, FontFamily, FontId, TextStyle};

const HEADING_FAMILY: &str = "heading";

/// The verdict of a check, shown as a colored badge.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Verdict {
    Ok,
    Ng,
    Warn,
    /// Not a verdict: skipped, not assessable, or a status this GUI does not know.
    Neutral,
}

impl Verdict {
    pub fn label(self) -> &'static str {
        match self {
            Verdict::Ok => "OK",
            Verdict::Ng => "NG",
            Verdict::Warn => "WARN",
            Verdict::Neutral => "N/A",
        }
    }

    /// Foreground and background colors for a verdict badge: white text on a
    /// saturated fill, each at least 4.5:1 against white.
    pub fn colors(self) -> (Color32, Color32) {
        let fill = match self {
            Verdict::Ok => Color32::from_rgb(0x2e, 0x7d, 0x32),
            Verdict::Ng => Color32::from_rgb(0xc6, 0x28, 0x28),
            Verdict::Warn => Color32::from_rgb(0x9a, 0x67, 0x00),
            Verdict::Neutral => Color32::from_rgb(0x59, 0x63, 0x6e),
        };
        (ON_FILL_TEXT, fill)
    }
}

// Type scale and text roles. Every text of the GUI uses one of these; see
// docs/gui/design.md, "Look and feel". Sizes are in points.

/// Title of a full-page view (a check's detail page): 26 pt semibold.
pub const PAGE_TITLE_SIZE: f32 = 26.0;
/// Section headings (Overview sections, the Packets pane's packet, dialogs,
/// empty states): 22 pt semibold, egui's Heading style (`ui.heading`).
pub const SECTION_HEADING_SIZE: f32 = 22.0;
/// Headings inside a page (a check's detail page): 18 pt semibold.
pub const SUBHEADING_SIZE: f32 = 18.0;
/// Body text and buttons: 15 pt; 13 pt in compact data areas (see
/// [`compact`]).
pub const BODY_SIZE: f32 = 15.0;
pub const COMPACT_BODY_SIZE: f32 = 13.0;
/// Values, offsets, PIDs and hex: JetBrains Mono 13.5 pt; 12.5 pt in
/// compact data areas.
pub const MONO_SIZE: f32 = 13.5;
pub const COMPACT_MONO_SIZE: f32 = 12.5;
/// Small text: labels painted under the rail's icons and inside the range
/// bar, table category labels; egui's Small style.
pub const CAPTION_SIZE: f32 = 12.5;

/// Text on a saturated fill (status and packet-kind badges, the Start
/// button): white, and every such fill keeps at least 4.5:1 against it.
pub const ON_FILL_TEXT: Color32 = Color32::WHITE;

/// A page title (see [`PAGE_TITLE_SIZE`]).
pub fn page_title(text: &str) -> egui::RichText {
    semibold(text, PAGE_TITLE_SIZE)
}

/// A heading inside a page (see [`SUBHEADING_SIZE`]).
pub fn subheading(text: &str) -> egui::RichText {
    semibold(text, SUBHEADING_SIZE)
}

/// Emphasized text: the titles of panels and groups (Fields, Bit inspector,
/// Scan range …), layer titles, the names of selected fields and the
/// breadcrumb's packet. Semibold at the size of the surrounding body text
/// (15 pt, 13 pt in compact areas) in the strong text color; an explicit
/// `.color()` (layer titles) replaces the color, not the weight.
pub fn emphasis(text: impl Into<String>) -> egui::RichText {
    egui::RichText::new(text)
        .family(FontFamily::Name(HEADING_FAMILY.into()))
        .strong()
}

/// The font of captions painted directly (see [`CAPTION_SIZE`]).
pub fn caption_font() -> FontId {
    FontId::proportional(CAPTION_SIZE)
}

/// The semibold face at `size`, for text painted directly (the packet
/// list's column headers).
pub fn semibold_font(size: f32) -> FontId {
    FontId::new(size, FontFamily::Name(HEADING_FAMILY.into()))
}

fn semibold(text: &str, size: f32) -> egui::RichText {
    egui::RichText::new(text)
        .size(size)
        .family(FontFamily::Name(HEADING_FAMILY.into()))
}

/// Fill of the Timeline's event markers (anomalies, continuity errors):
/// the warning tone the badges used before they turned white-on-color.
pub const EVENT_COLOR: Color32 = Color32::from_rgb(0xff, 0xd5, 0x83);
pub const PCR_COLOR: Color32 = Color32::from_rgb(0xf0, 0xa4, 0x3c);
pub const VIDEO_COLOR: Color32 = Color32::from_rgb(0x4e, 0xa8, 0xff);
pub const AUDIO_COLOR: Color32 = Color32::from_rgb(0x7f, 0xd6, 0x8a);
pub const PSI_COLOR: Color32 = Color32::from_rgb(0xc7, 0x92, 0xea);
pub const LIMIT_COLOR: Color32 = Color32::from_rgb(0xff, 0x7a, 0x7a);

/// Colors of a packet's layers in the Packets pane's Byte inspector and the
/// layer titles of both field lists; the adaptation field and PSI sections
/// use [`PCR_COLOR`] and [`PSI_COLOR`].
pub const LAYER_TS_HEADER: Color32 = Color32::from_rgb(0x7c, 0xc4, 0xff);
pub const LAYER_PES_HEADER: Color32 = Color32::from_rgb(0x8f, 0xe3, 0x9f);
pub const LAYER_PAYLOAD: Color32 = Color32::GRAY;
pub const LAYER_TRAILER: Color32 = Color32::from_rgb(0xe0, 0x8a, 0xb4);

/// Colors of the scan-range bar by load: blue for light ranges, shifting
/// through teal, yellow and orange to red for ranges that need confirmation.
pub const LOAD_LIGHT: Color32 = Color32::from_rgb(0x4e, 0xa8, 0xff);
pub const LOAD_MODERATE: Color32 = Color32::from_rgb(0x4f, 0xc3, 0xb5);
pub const LOAD_HEAVY: Color32 = Color32::from_rgb(0xe6, 0xc4, 0x4c);
pub const LOAD_VERY_HEAVY: Color32 = Color32::from_rgb(0xf0, 0x8c, 0x3c);
pub const LOAD_NEEDS_CONFIRMATION: Color32 = Color32::from_rgb(0xff, 0x6b, 0x6b);
// Table colors (Codex proposal): Material 3 tonal surfaces with Carbon/
// Primer-like values, fixed per light and dark theme.
fn by_theme(visuals: &egui::Visuals, light: u32, dark: u32) -> Color32 {
    let rgb = if visuals.dark_mode { dark } else { light };
    Color32::from_rgb((rgb >> 16) as u8, (rgb >> 8) as u8, rgb as u8)
}

/// Color of a table's column headers.
pub fn table_header_color(visuals: &egui::Visuals) -> Color32 {
    by_theme(visuals, 0x3C4043, 0xE0E0E0)
}

/// Text of a table's column headers, in every table and grid: semibold at the
/// size of the surrounding body text, in the header color.
pub fn table_header_text(text: &str, visuals: &egui::Visuals) -> egui::RichText {
    emphasis(text).color(table_header_color(visuals))
}

/// Fill of a table's header row.
pub fn table_header_fill(visuals: &egui::Visuals) -> Color32 {
    by_theme(visuals, 0xE5E7EA, 0x292B2E)
}

/// Fill of a category subheader row.
pub fn table_category_fill(visuals: &egui::Visuals) -> Color32 {
    by_theme(visuals, 0xF1F3F5, 0x222427)
}

/// The rule under a table's header row.
pub fn table_header_rule(visuals: &egui::Visuals) -> egui::Stroke {
    egui::Stroke::new(1.0, by_theme(visuals, 0xAEB4BC, 0x565A61))
}

/// The line between table rows.
pub fn table_divider(visuals: &egui::Visuals) -> egui::Stroke {
    egui::Stroke::new(1.0, by_theme(visuals, 0xD5D8DC, 0x383B40))
}

/// The outer border of a table, a little stronger than the dividers.
pub fn table_border(visuals: &egui::Visuals) -> egui::Stroke {
    egui::Stroke::new(1.0, by_theme(visuals, 0xB8BDC4, 0x4B4F56))
}

/// Text of a category subheader: caption-sized semibold capitals in blue.
pub fn table_category_text(text: &str, visuals: &egui::Visuals) -> egui::RichText {
    semibold(&text.to_uppercase(), CAPTION_SIZE).color(by_theme(visuals, 0x0969DA, 0x58A6FF))
}

/// Size of the text in every badge (statuses and the packet kind): close to
/// body text, so a verdict reads at a glance, and small enough that a badge
/// sits in a table row or next to body text without growing it.
pub const BADGE_TEXT_SIZE: f32 = 14.0;
/// Space around the text of a status badge.
pub const BADGE_MARGIN: egui::Vec2 = egui::vec2(10.0, 3.0);
/// Narrowest status badge, so short verdicts (OK, NG) read as solid blocks.
pub const BADGE_MIN_WIDTH: f32 = 48.0;
/// Corner radius of a status badge.
pub const BADGE_RADIUS: u8 = 4;

/// The text of a badge: semibold (the heading face), near body size. The
/// caller colors it [`ON_FILL_TEXT`].
pub fn badge_text(text: &str) -> egui::RichText {
    semibold(text, BADGE_TEXT_SIZE)
}

/// Space around the text of a button.
pub const BUTTON_PADDING: egui::Vec2 = egui::vec2(10.0, 5.0);
/// Height of buttons and other controls (compact areas use less).
pub const BUTTON_HEIGHT: f32 = 26.0;

/// Fills of the packet-kind badge of the detailed inspection page, all with
/// white text at least 5:1 and the same in light and dark: PSI/SI tables,
/// video, audio, PCR, other PES, and packets without a payload of their own.
pub const KIND_PSI: Color32 = Color32::from_rgb(0x7b, 0x3f, 0xb0);
pub const KIND_VIDEO: Color32 = Color32::from_rgb(0x1f, 0x63, 0xb5);
pub const KIND_AUDIO: Color32 = Color32::from_rgb(0x2e, 0x7d, 0x32);
pub const KIND_PCR: Color32 = Color32::from_rgb(0x9a, 0x53, 0x00);
pub const KIND_PES: Color32 = Color32::from_rgb(0x3d, 0x6a, 0x93);
pub const KIND_OTHER: Color32 = Color32::from_rgb(0x59, 0x63, 0x6e);

/// Fill of the buttons that start an analysis, with [`ON_FILL_TEXT`].
pub const START_COLOR: Color32 = Color32::from_rgb(0x2e, 0x8b, 0x4f);

/// Fill of the star reminder's **Star on GitHub** button, with
/// [`ON_FILL_TEXT`] (5.8:1): a muted amber, the same in light and dark.
pub const STAR_FILL: Color32 = Color32::from_rgb(0x8f, 0x5a, 0x00);

/// The star reminder's warm accent: its star icon and the rule above the row.
/// At least 3:1 on the panel (light `#B36B00`, dark `#F2B544`).
pub fn star_accent(visuals: &egui::Visuals) -> Color32 {
    by_theme(visuals, 0xB36B00, 0xF2B544)
}

/// Larger than egui's defaults so menus, dialogs and controls are easy to read and hit.
pub fn apply(ctx: &egui::Context) {
    install_fonts(ctx, true);
    apply_style(ctx);
}

/// The theme with the bundled fonts only, for screenshots that must not depend
/// on the fonts of the machine that renders them.
#[cfg(all(test, feature = "screenshots"))]
pub fn apply_with_bundled_fonts(ctx: &egui::Context) {
    install_fonts(ctx, false);
    apply_style(ctx);
}

/// Body text of non-interactive widgets in the dark theme: egui's gray 140
/// (5.1:1 on the panel) is raised so weak text can stay below it and still
/// read comfortably.
pub const DARK_TEXT: Color32 = Color32::from_gray(190);
/// Weak (secondary) text: hints, context lines, legends, the status bar.
/// At least 4.5:1 on the panel (gray 27 dark: 5.8:1; gray 248 light: 4.8:1),
/// instead of egui's 60 % of the text color (about 2.7:1 and 2.9:1).
pub const DARK_WEAK_TEXT: Color32 = Color32::from_gray(150);
pub const LIGHT_WEAK_TEXT: Color32 = Color32::from_gray(110);

fn apply_style(ctx: &egui::Context) {
    ctx.all_styles_mut(|style| {
        let visuals = &mut style.visuals;
        if visuals.dark_mode {
            visuals.widgets.noninteractive.fg_stroke.color = DARK_TEXT;
            visuals.weak_text_color = Some(DARK_WEAK_TEXT);
        } else {
            visuals.weak_text_color = Some(LIGHT_WEAK_TEXT);
        }
        style.text_styles = [
            (TextStyle::Small, FontId::proportional(CAPTION_SIZE)),
            (TextStyle::Body, FontId::proportional(BODY_SIZE)),
            (TextStyle::Button, FontId::proportional(BODY_SIZE)),
            (
                TextStyle::Heading,
                FontId::new(
                    SECTION_HEADING_SIZE,
                    FontFamily::Name(HEADING_FAMILY.into()),
                ),
            ),
            (TextStyle::Monospace, FontId::monospace(MONO_SIZE)),
        ]
        .into();
        let spacing = &mut style.spacing;
        spacing.item_spacing = egui::vec2(10.0, 7.0);
        spacing.button_padding = BUTTON_PADDING;
        spacing.interact_size.y = BUTTON_HEIGHT;
        spacing.icon_width = 18.0;
        spacing.icon_width_inner = 10.0;
        spacing.menu_margin = egui::Margin::same(8);
        spacing.menu_width = 220.0;
        spacing.window_margin = egui::Margin::same(16);
    });
}

/// Inter for UI text, JetBrains Mono for data, Phosphor for icons (all bundled under
/// `assets/fonts` with their licenses). egui's default fonts stay as fallbacks, and
/// a Japanese font the OS ships comes last so Japanese file names can be shown.
fn install_fonts(ctx: &egui::Context, with_os_japanese: bool) {
    let mut fonts = egui::FontDefinitions::default();
    for (name, bytes) in [
        (
            "Inter",
            &include_bytes!("../assets/fonts/Inter-Regular.ttf")[..],
        ),
        (
            "Inter SemiBold",
            &include_bytes!("../assets/fonts/Inter-SemiBold.ttf")[..],
        ),
        (
            "JetBrains Mono",
            &include_bytes!("../assets/fonts/JetBrainsMono-Regular.ttf")[..],
        ),
        (
            "Phosphor",
            &include_bytes!("../assets/fonts/Phosphor.ttf")[..],
        ),
    ] {
        fonts.font_data.insert(
            name.to_owned(),
            Arc::new(egui::FontData::from_static(bytes)),
        );
    }

    let proportional = fonts.families.entry(FontFamily::Proportional).or_default();
    proportional.insert(0, "Inter".to_owned());
    let mut heading = proportional.clone();
    heading.insert(0, "Inter SemiBold".to_owned());
    fonts
        .families
        .insert(FontFamily::Name(HEADING_FAMILY.into()), heading);
    fonts
        .families
        .entry(FontFamily::Monospace)
        .or_default()
        .insert(0, "JetBrains Mono".to_owned());

    if let Some(bytes) = with_os_japanese
        .then(|| {
            cjk_candidates()
                .into_iter()
                .chain(std::iter::once_with(fontconfig_cjk).flatten())
                .find_map(|path| std::fs::read(path).ok())
        })
        .flatten()
    {
        fonts.font_data.insert(
            CJK_FONT.to_owned(),
            Arc::new(egui::FontData::from_owned(bytes)),
        );
        for family in fonts.families.values_mut() {
            family.push(CJK_FONT.to_owned());
        }
    }

    // Icons get their own family: as a fallback, Phosphor's Private Use Area codepoints can be
    // claimed first by other fonts that put their own glyphs there.
    fonts.families.insert(
        FontFamily::Name(crate::icons::FAMILY.into()),
        vec!["Phosphor".to_owned()],
    );

    ctx.set_fonts(fonts);
}

const CJK_FONT: &str = "CJK fallback";

/// Fonts with Japanese glyphs that the OS usually ships, in order of
/// preference. Only the first one that can be read is used, so Chinese or
/// Korean text shows only as far as that font covers it. Bundling a font would
/// add 10 MB or more to the GUI. Collections (`.ttc`) use their first face.
fn cjk_candidates() -> Vec<PathBuf> {
    if cfg!(target_os = "macos") {
        [
            "/System/Library/Fonts/ヒラギノ角ゴシック W3.ttc",
            "/System/Library/Fonts/Hiragino Sans GB.ttc",
        ]
        .iter()
        .map(PathBuf::from)
        .collect()
    } else if cfg!(windows) {
        let fonts = std::env::var_os("WINDIR")
            .map_or_else(|| PathBuf::from(r"C:\Windows"), PathBuf::from)
            .join("Fonts");
        ["YuGothM.ttc", "meiryo.ttc", "msgothic.ttc"]
            .iter()
            .map(|name| fonts.join(name))
            .collect()
    } else {
        [
            "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
            "/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
            "/usr/share/fonts/google-noto-cjk/NotoSansCJK-Regular.ttc",
            "/usr/share/fonts/opentype/noto/NotoSansCJKjp-Regular.otf",
            "/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
        ]
        .iter()
        .map(PathBuf::from)
        .collect()
    }
}

/// On Linux, asks fontconfig for a Japanese sans-serif font. Called only when
/// none of the usual paths can be read.
fn fontconfig_cjk() -> Option<PathBuf> {
    if cfg!(any(target_os = "macos", windows)) {
        return None;
    }
    let output = std::process::Command::new("fc-match")
        .args(["--format=%{file}", "sans-serif:lang=ja"])
        .output()
        .ok()?;
    let path = PathBuf::from(String::from_utf8(output.stdout).ok()?.trim());
    (output.status.success() && path.is_file()).then_some(path)
}

/// Denser text and spacing for data-heavy areas such as packet lists and hex dumps.
pub fn compact(ui: &mut egui::Ui) {
    let style = ui.style_mut();
    style
        .text_styles
        .insert(TextStyle::Body, FontId::proportional(COMPACT_BODY_SIZE));
    style
        .text_styles
        .insert(TextStyle::Monospace, FontId::monospace(COMPACT_MONO_SIZE));
    style.spacing.item_spacing = egui::vec2(8.0, 3.0);
    style.spacing.interact_size.y = 20.0;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn lists_cjk_fonts_for_this_os() {
        let candidates = cjk_candidates();
        assert!(!candidates.is_empty());
        if cfg!(target_os = "macos") {
            // Hiragino ships with every macOS version the GUI supports.
            assert!(candidates.iter().any(|path| path.is_file()));
        }
    }

    /// The fallback is in effect: a Japanese file name has glyphs. Checked on
    /// macOS and Windows, which always ship a CJK font; a minimal Linux may not.
    #[test]
    fn shows_japanese_text() {
        if !cfg!(any(target_os = "macos", windows)) {
            return;
        }
        let ctx = egui::Context::default();
        apply(&ctx);
        // The first frame loads the fonts; its texture updates are not drawn here.
        ctx.run_ui(Default::default(), |_| {})
            .textures_delta
            .clear();
        for font in [
            FontId::proportional(BODY_SIZE),
            FontId::monospace(MONO_SIZE),
        ] {
            assert!(ctx.fonts_mut(|fonts| fonts.has_glyphs(&font, "録画_日本語ファイル名.ts")));
        }
    }
}
