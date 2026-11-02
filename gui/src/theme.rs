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
/// `assets/fonts` with their licenses). egui's default fonts stay as fallbacks.
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

    // Icons get their own family: as a fallback, Phosphor's Private Use Area codepoints can be
    // claimed first by other fonts that put their own glyphs there.
    fonts.families.insert(
        FontFamily::Name(crate::icons::FAMILY.into()),
        vec!["Phosphor".to_owned()],
    );

    ctx.set_fonts(fonts);
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
