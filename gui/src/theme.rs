use std::path::PathBuf;
use std::sync::Arc;

use eframe::egui::{self, FontFamily, FontId, TextStyle};

const HEADING_FAMILY: &str = "heading";

/// Larger than egui's defaults so menus, dialogs and controls are easy to read and hit.
pub fn apply(ctx: &egui::Context) {
    install_fonts(ctx);
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
fn install_fonts(ctx: &egui::Context) {
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

    if let Some(bytes) = cjk_candidates()
        .into_iter()
        .chain(std::iter::once_with(fontconfig_cjk).flatten())
        .find_map(|path| std::fs::read(path).ok())
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
