use std::path::PathBuf;
use std::sync::Arc;

use eframe::egui::{self, Color32, FontFamily, FontId, TextStyle};

const HEADING_FAMILY: &str = "heading";

/// The verdict of a check, shown as a colored badge.
#[derive(Clone, Copy)]
pub enum Verdict {
    Ok,
    Ng,
    Warn,
}

impl Verdict {
    pub fn label(self) -> &'static str {
        match self {
            Verdict::Ok => "OK",
            Verdict::Ng => "NG",
            Verdict::Warn => "WARN",
        }
    }

    /// Foreground and background colors for a verdict badge: white text on a
    /// saturated fill, each at least 4.5:1 against white.
    pub fn colors(self) -> (Color32, Color32) {
        let fill = match self {
            Verdict::Ok => Color32::from_rgb(0x2e, 0x7d, 0x32),
            Verdict::Ng => Color32::from_rgb(0xc6, 0x28, 0x28),
            Verdict::Warn => Color32::from_rgb(0x9a, 0x67, 0x00),
        };
        (Color32::WHITE, fill)
    }
}

pub const PCR_COLOR: Color32 = Color32::from_rgb(0xf0, 0xa4, 0x3c);
pub const VIDEO_COLOR: Color32 = Color32::from_rgb(0x4e, 0xa8, 0xff);
pub const AUDIO_COLOR: Color32 = Color32::from_rgb(0x7f, 0xd6, 0x8a);
pub const PSI_COLOR: Color32 = Color32::from_rgb(0xc7, 0x92, 0xea);
pub const LIMIT_COLOR: Color32 = Color32::from_rgb(0xff, 0x7a, 0x7a);
/// Fill of a table's column-header row, clearly lighter than the panel.
pub const TABLE_HEADER_FILL: Color32 = Color32::from_gray(0x3a);
/// Fill of a category row inside a table, between the header and the rows.
pub const TABLE_CATEGORY_FILL: Color32 = Color32::from_gray(0x2e);
/// Text of table header and category rows.
pub const TABLE_HEADER_TEXT: Color32 = Color32::from_gray(0xf2);

/// Fill of the buttons that start an analysis, with white text.
pub const START_COLOR: Color32 = Color32::from_rgb(0x2e, 0x8b, 0x4f);

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

fn apply_style(ctx: &egui::Context) {
    ctx.all_styles_mut(|style| {
        style.text_styles = [
            (TextStyle::Small, FontId::proportional(12.0)),
            (TextStyle::Body, FontId::proportional(15.0)),
            (TextStyle::Button, FontId::proportional(15.0)),
            (
                TextStyle::Heading,
                FontId::new(22.0, FontFamily::Name(HEADING_FAMILY.into())),
            ),
            (TextStyle::Monospace, FontId::monospace(13.5)),
        ]
        .into();
        let spacing = &mut style.spacing;
        spacing.item_spacing = egui::vec2(10.0, 7.0);
        spacing.button_padding = egui::vec2(10.0, 5.0);
        spacing.interact_size.y = 26.0;
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
        .insert(TextStyle::Body, FontId::proportional(13.0));
    style
        .text_styles
        .insert(TextStyle::Monospace, FontId::monospace(12.5));
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
        for font in [FontId::proportional(15.0), FontId::monospace(13.5)] {
            assert!(ctx.fonts_mut(|fonts| fonts.has_glyphs(&font, "録画_日本語ファイル名.ts")));
        }
    }
}
