use eframe::egui;

use crate::theme;

pub(super) const ROW_HEIGHT: f32 = 22.0;
/// Below this width the list scrolls horizontally instead of squeezing columns.
pub(super) const MIN_LIST_WIDTH: f32 = 1100.0;

pub(super) fn row_text_color(ui: &egui::Ui, selected: bool) -> egui::Color32 {
    if selected {
        ui.visuals().selection.stroke.color
    } else {
        ui.visuals().text_color()
    }
}

pub(super) fn paint_row_background(
    ui: &egui::Ui,
    rect: egui::Rect,
    response: &egui::Response,
    selected: bool,
    striped: bool,
) {
    let visuals = ui.visuals();
    let fill = if selected {
        Some(visuals.selection.bg_fill)
    } else if response.hovered() {
        Some(visuals.widgets.hovered.weak_bg_fill)
    } else if striped {
        Some(visuals.faint_bg_color)
    } else {
        None
    };
    if let Some(fill) = fill {
        ui.painter().rect_filled(rect, 0, fill);
    }
}

pub(super) fn paint_cells(
    ui: &egui::Ui,
    rect: egui::Rect,
    start_x: f32,
    cells: &[(String, f32)],
    color: egui::Color32,
    strong: bool,
) {
    let font = if strong {
        egui::FontId::proportional(12.5)
    } else {
        egui::TextStyle::Monospace.resolve(ui.style())
    };
    let mut x = rect.left() + start_x;
    for (text, width) in cells {
        ui.painter().text(
            egui::pos2(x, rect.center().y),
            egui::Align2::LEFT_CENTER,
            text,
            font.clone(),
            color,
        );
        x += width;
    }
}

pub(super) const TEXT_INSET: f32 = 8.0;

pub(super) fn header_row(ui: &mut egui::Ui, width: f32, columns: &[(&str, f32, &str)]) {
    let (rect, _) = ui.allocate_exact_size(egui::vec2(width, ROW_HEIGHT), egui::Sense::hover());
    let cells: Vec<(String, f32)> = columns
        .iter()
        .map(|(title, width, _)| ((*title).to_owned(), *width))
        .collect();
    paint_cells(
        ui,
        rect,
        TEXT_INSET,
        &cells,
        ui.visuals().strong_text_color(),
        true,
    );
    let mut x = rect.left() + TEXT_INSET;
    for &(title, width, description) in columns {
        let cell =
            egui::Rect::from_min_size(egui::pos2(x, rect.top()), egui::vec2(width, ROW_HEIGHT));
        ui.interact(
            cell,
            ui.scope_id().with(("column", title)),
            egui::Sense::hover(),
        )
        .on_hover_text(description);
        x += width;
    }
}

pub(super) type ByteSpan = std::ops::Range<usize>;

/// The field selected in the detail pane. Its name is part of its identity,
/// so fields that share a byte are selected one at a time.
#[derive(Clone, Debug, PartialEq)]
pub(super) struct FieldSelection {
    pub(super) key: String,
    pub(super) value: String,
    pub(super) bytes: ByteSpan,
    /// Packet-relative bit ranges in wire order (bit 0 is byte 0's most
    /// significant bit); empty when the field uses all bits of `bytes`.
    pub(super) bits: Vec<std::ops::Range<usize>>,
}

impl FieldSelection {
    /// A selection of whole bytes.
    #[cfg(test)]
    pub(super) fn of_bytes(key: impl Into<String>, bytes: ByteSpan) -> Self {
        Self {
            key: key.into(),
            value: String::new(),
            bytes,
            bits: Vec::new(),
        }
    }

    /// The selected bits, falling back to every bit of the byte span.
    pub(super) fn bit_ranges(&self) -> Vec<std::ops::Range<usize>> {
        if self.bits.is_empty() {
            std::iter::once(self.bytes.start * 8..self.bytes.end * 8).collect()
        } else {
            self.bits.clone()
        }
    }
}

/// A decoded field and the bytes of the packet it was read from.
pub(super) struct Field {
    key: String,
    value: String,
    bytes: Option<ByteSpan>,
    bits: Vec<std::ops::Range<usize>>,
}

impl Field {
    pub(super) fn new(key: impl Into<String>, value: impl Into<String>, bytes: ByteSpan) -> Self {
        Self {
            key: key.into(),
            value: value.into(),
            bytes: Some(bytes),
            bits: Vec::new(),
        }
    }

    /// The exact bits the field was read from, when it does not use whole bytes.
    pub(super) fn with_bits(mut self, bits: Vec<std::ops::Range<usize>>) -> Self {
        self.bits = bits;
        self
    }

    /// A derived value with no bytes of its own in this packet.
    pub(super) fn info(key: impl Into<String>, value: impl Into<String>) -> Self {
        Self {
            key: key.into(),
            value: value.into(),
            bytes: None,
            bits: Vec::new(),
        }
    }
}

pub(super) fn field_tree(
    ui: &mut egui::Ui,
    title: &str,
    color: egui::Color32,
    fields: &[Field],
    highlight: &mut Option<FieldSelection>,
) {
    egui::CollapsingHeader::new(egui::RichText::new(title).color(color))
        .default_open(true)
        .show(ui, |ui| key_values(ui, title, fields, highlight));
}

/// Cells of one clickable field or table row; clicking toggles the highlight of its bytes.
fn span_cells(
    ui: &mut egui::Ui,
    cells: &[egui::RichText],
    candidate: &FieldSelection,
    highlight: &mut Option<FieldSelection>,
) {
    let on = highlight.as_ref() == Some(candidate);
    let span = &candidate.bytes;
    let hover = if span.len() == 1 {
        format!("byte {}", span.start)
    } else {
        format!("bytes {}–{}", span.start, span.end - 1)
    };
    let mut clicked = false;
    for cell in cells {
        clicked |= ui
            .selectable_label(on, cell.clone())
            .on_hover_text(&hover)
            .clicked();
    }
    if clicked {
        *highlight = if on { None } else { Some(candidate.clone()) };
    }
}

pub(super) fn key_values(
    ui: &mut egui::Ui,
    id: &str,
    fields: &[Field],
    highlight: &mut Option<FieldSelection>,
) {
    egui::Grid::new(id)
        .num_columns(2)
        .spacing([16.0, 2.0])
        .show(ui, |ui| {
            for field in fields {
                match &field.bytes {
                    Some(span) => span_cells(
                        ui,
                        &[
                            egui::RichText::new(&field.key).color(ui.visuals().weak_text_color()),
                            egui::RichText::new(&field.value).monospace(),
                        ],
                        &FieldSelection {
                            key: field.key.clone(),
                            value: field.value.clone(),
                            bytes: span.clone(),
                            bits: field.bits.clone(),
                        },
                        highlight,
                    ),
                    None => {
                        ui.weak(&field.key);
                        ui.monospace(&field.value);
                    }
                }
                ui.end_row();
            }
        });
}

/// A table whose rows each map to a byte span, e.g. PAT programs or PMT streams.
pub(super) fn span_table(
    ui: &mut egui::Ui,
    id: &str,
    titles: &[&str],
    rows: &[(Vec<String>, FieldSelection)],
    highlight: &mut Option<FieldSelection>,
) {
    egui::Grid::new(id)
        .num_columns(titles.len())
        .striped(true)
        .spacing([16.0, 2.0])
        .show(ui, |ui| {
            for title in titles {
                ui.strong(*title);
            }
            ui.end_row();
            for (cells, selection) in rows {
                let cells: Vec<egui::RichText> = cells
                    .iter()
                    .map(|c| egui::RichText::new(c).monospace())
                    .collect();
                span_cells(ui, &cells, selection, highlight);
                ui.end_row();
            }
        });
}

/// Sixteen bytes per row, each in its layer's color; highlighted bytes are inverted.
pub(super) fn hex_dump_bytes(
    ui: &mut egui::Ui,
    bytes: &[(u8, egui::Color32)],
    highlight: Option<&ByteSpan>,
) {
    let (on_fill, on_text) = (
        ui.visuals().selection.bg_fill,
        ui.visuals().selection.stroke.color,
    );
    for (row_index, row) in bytes.chunks(16).enumerate() {
        ui.horizontal(|ui| {
            ui.spacing_mut().item_spacing.x = 6.0;
            ui.weak(egui::RichText::new(format!("{:04X}", row_index * 16)).monospace());
            for (column, &(byte, color)) in row.iter().enumerate() {
                let text = egui::RichText::new(format!("{byte:02X}")).monospace();
                let text =
                    if highlight.is_some_and(|span| span.contains(&(row_index * 16 + column))) {
                        text.background_color(on_fill).color(on_text)
                    } else {
                        text.color(color)
                    };
                ui.label(text);
            }
        });
    }
}

/// Bytes the Bit inspector shows at most; longer selections (payloads,
/// timestamps with their bytes) show the first ones and say how many are left.
const BIT_PREVIEW_BYTES: usize = 8;

/// The rows of the Bit inspector: the bytes the selected bits fall in, capped
/// at `BIT_PREVIEW_BYTES`, and how many further bytes are left out.
pub(super) fn bit_rows(
    ranges: &[std::ops::Range<usize>],
    len: usize,
) -> (std::ops::Range<usize>, usize) {
    let (Some(first), Some(last)) = (
        ranges.iter().map(|r| r.start).min(),
        ranges.iter().map(|r| r.end).max(),
    ) else {
        return (0..0, 0);
    };
    let start = (first / 8).min(len);
    let end = last.div_ceil(8).min(len);
    let shown = end.min(start + BIT_PREVIEW_BYTES);
    (start..shown, end - shown)
}

/// A summary of the selected bits, e.g. "13 bits · bits 11–23".
pub(super) fn bit_summary(ranges: &[std::ops::Range<usize>]) -> String {
    let count: usize = ranges.iter().map(|r| r.len()).sum();
    let spans: Vec<String> = ranges
        .iter()
        .map(|r| {
            if r.len() == 1 {
                format!("{}", r.start)
            } else {
                format!("{}–{}", r.start, r.end - 1)
            }
        })
        .collect();
    format!(
        "{count} bit{} · bit{} {}",
        if count == 1 { "" } else { "s" },
        if count == 1 { "" } else { "s" },
        spans.join(", ")
    )
}

/// The bits of the selected field, one row per byte in packet order, each byte
/// from bit 7 (most significant) to bit 0, with only the field's bits marked.
pub(super) fn bit_inspector(ui: &mut egui::Ui, raw: &[u8], selection: Option<&FieldSelection>) {
    ui.horizontal(|ui| {
        ui.strong("Bit inspector");
        ui.weak("file order ↓ · each byte MSB → LSB");
    });
    let Some(selection) = selection else {
        ui.weak("Click a field to see its bits.");
        return;
    };
    let ranges = selection.bit_ranges();
    ui.horizontal_wrapped(|ui| {
        ui.label(egui::RichText::new(&selection.key).strong());
        if !selection.value.is_empty() {
            ui.monospace(&selection.value);
        }
        ui.weak(bit_summary(&ranges));
    });
    let (rows, omitted) = bit_rows(&ranges, raw.len());
    let (on_fill, on_text) = (
        ui.visuals().selection.bg_fill,
        ui.visuals().selection.stroke.color,
    );
    egui::Grid::new("selected_field_bits")
        .num_columns(10)
        .spacing([6.0, 2.0])
        .show(ui, |ui| {
            ui.weak("byte");
            for bit in (0..8).rev() {
                ui.weak(bit.to_string());
            }
            ui.weak("hex");
            ui.end_row();
            for index in rows {
                let byte = raw[index];
                ui.monospace(format!("{index:03}"));
                for wire in 0..8 {
                    let value = (byte >> (7 - wire)) & 1;
                    let text = egui::RichText::new(value.to_string()).monospace();
                    if ranges.iter().any(|r| r.contains(&(index * 8 + wire))) {
                        ui.label(text.background_color(on_fill).color(on_text));
                    } else {
                        ui.weak(text);
                    }
                }
                ui.monospace(format!("{byte:02X}"));
                ui.end_row();
            }
        });
    if omitted > 0 {
        ui.weak(format!(
            "{omitted} more byte{} not shown",
            if omitted == 1 { "" } else { "s" }
        ));
    }
}

#[derive(Clone, Copy)]
pub(super) enum ByteLayer {
    TsHeader,
    AdaptationField,
    PesHeader,
    Section,
    Payload,
}

pub(super) fn layer_color(layer: ByteLayer) -> egui::Color32 {
    match layer {
        ByteLayer::TsHeader => egui::Color32::from_rgb(0x7c, 0xc4, 0xff),
        ByteLayer::AdaptationField => theme::PCR_COLOR,
        ByteLayer::PesHeader => egui::Color32::from_rgb(0x8f, 0xe3, 0x9f),
        ByteLayer::Section => theme::PSI_COLOR,
        ByteLayer::Payload => egui::Color32::GRAY,
    }
}

#[cfg(test)]
// Bit ranges are lists of ranges; a list of one is the common case here.
#[allow(clippy::single_range_in_vec_init)]
mod tests {
    use super::*;

    #[test]
    fn falls_back_to_every_bit_of_the_bytes() {
        let selection = FieldSelection::of_bytes("stream_type", 17..18);
        assert_eq!(selection.bit_ranges(), [136..144]);
        let pid = FieldSelection {
            bits: vec![11..24],
            ..FieldSelection::of_bytes("PID", 1..3)
        };
        assert_eq!(pid.bit_ranges(), [11..24]);
    }

    #[test]
    fn shows_the_bytes_of_the_selected_bits() {
        // PID: the low 5 bits of byte 1 and all of byte 2.
        assert_eq!(bit_rows(&[11..24], 188), (1..3, 0));
        // PTS value bits around marker bits stay within their 5 bytes.
        assert_eq!(bit_rows(&[172..175, 176..191, 192..207], 188), (21..26, 0));
        // A payload is cut to a preview.
        assert_eq!(bit_rows(&[96..1504], 188), (12..20, 168));
        assert_eq!(bit_rows(&[], 188), (0..0, 0));
    }

    #[test]
    fn summarizes_the_selected_bits() {
        assert_eq!(bit_summary(&[11..24]), "13 bits · bits 11–23");
        assert_eq!(bit_summary(&[9..10]), "1 bit · bit 9");
        assert_eq!(
            bit_summary(&[48..81, 87..96]),
            "42 bits · bits 48–80, 87–95"
        );
    }

    /// Fields that share a byte are different selections.
    #[test]
    fn fields_sharing_a_byte_are_selected_separately() {
        let version = FieldSelection {
            bits: vec![82..87],
            ..FieldSelection::of_bytes("version_number", 10..11)
        };
        let current = FieldSelection {
            bits: vec![87..88],
            ..FieldSelection::of_bytes("current_next_indicator", 10..11)
        };
        assert_ne!(version, current);
        assert_eq!(version.bytes, current.bytes);
    }
}
