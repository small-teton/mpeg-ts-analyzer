use eframe::egui;

use super::badge;
use crate::icons;
use crate::mock::{self, Verdict};
use crate::mock_packets::{self as data, ByteLayer, Kind, PAGE_SIZE, PAGES, Packet, Range, Unit};

pub(super) const ROW_HEIGHT: f32 = 22.0;
/// Below this width the list scrolls horizontally instead of squeezing columns.
pub(super) const MIN_LIST_WIDTH: f32 = 900.0;

enum Row {
    Packet(usize),
    LayoutChange(usize),
}

pub struct PacketsView {
    page: usize,
    selected: usize,
    /// PIDs unchecked in the PID filter.
    hidden_pids: std::collections::BTreeSet<u16>,
    /// Bytes highlighted in the hex dump: (packet index, byte range).
    highlight: Option<(usize, std::ops::Range<usize>)>,
}

impl Default for PacketsView {
    fn default() -> Self {
        // Open on the NG PCR, as when arriving from Timeline's "Open packet".
        let selected = data::range()
            .packets
            .iter()
            .find(|p| p.pcr_interval_ms.is_some_and(|ms| ms > 100.0))
            .map_or(0, |p| p.index);
        Self {
            page: Range::page_of(selected),
            selected,
            hidden_pids: Default::default(),
            highlight: None,
        }
    }
}

impl PacketsView {
    pub fn show(&mut self, ui: &mut egui::Ui) {
        egui::Panel::right("packet_detail")
            .resizable(true)
            .default_size(460.0)
            .show(ui, |ui| {
                egui::ScrollArea::vertical().show(ui, |ui| self.detail(ui));
            });
        egui::CentralPanel::default().show(ui, |ui| {
            crate::theme::compact(ui);
            self.pager(ui);
            ui.separator();
            self.packet_list(ui);
        });
    }

    fn select(&mut self, index: usize) {
        self.selected = index.min(data::RANGE_PACKETS - 1);
        self.page = Range::page_of(self.selected);
    }

    /// Checkbox per PID in the range, so any combination can be shown.
    fn pid_filter(&mut self, ui: &mut egui::Ui) {
        let pids = pids_in_range();
        let shown = pids.len() - self.hidden_pids.len();
        let label = if shown == pids.len() {
            "PIDs: all".to_owned()
        } else {
            format!("PIDs: {shown} of {}", pids.len())
        };
        ui.menu_button(label, |ui| {
            ui.horizontal(|ui| {
                if ui.button("All").clicked() {
                    self.hidden_pids.clear();
                }
                if ui.button("None").clicked() {
                    self.hidden_pids = pids.iter().map(|(pid, _)| *pid).collect();
                }
            });
            ui.separator();
            for (pid, kind) in &pids {
                let mut checked = !self.hidden_pids.contains(pid);
                let text = egui::RichText::new(format!("0x{pid:04X}  {}", kind.label()))
                    .color(kind.color());
                if ui.checkbox(&mut checked, text).changed() {
                    if checked {
                        self.hidden_pids.remove(pid);
                    } else {
                        self.hidden_pids.insert(*pid);
                    }
                }
            }
        });
    }

    fn pager(&mut self, ui: &mut egui::Ui) {
        ui.horizontal(|ui| {
            let first = self.page > 0;
            let last = self.page + 1 < PAGES;
            if ui.add_enabled(first, egui::Button::new("First")).clicked() {
                self.page = 0;
            }
            if ui
                .add_enabled(first, egui::Button::new(icons::text(icons::CARET_LEFT)))
                .clicked()
            {
                self.page -= 1;
            }
            ui.monospace(format!("Page {} / {PAGES}", self.page + 1));
            if ui
                .add_enabled(last, egui::Button::new(icons::text(icons::CARET_RIGHT)))
                .clicked()
            {
                self.page += 1;
            }
            if ui.add_enabled(last, egui::Button::new("Last")).clicked() {
                self.page = PAGES - 1;
            }
            let start = self.page * PAGE_SIZE;
            let end = (start + PAGE_SIZE).min(data::RANGE_PACKETS);
            ui.weak(format!(
                "packets {}–{end} of {} in range",
                start + 1,
                data::RANGE_PACKETS,
            ));
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                self.pid_filter(ui);
            });
        });
    }

    fn packet_list(&mut self, ui: &mut egui::Ui) {
        let range = data::range();
        let start = self.page * PAGE_SIZE;
        let mut rows = Vec::new();
        for packet in range.page(self.page) {
            if let Some(layout) = range
                .layouts
                .iter()
                .position(|l| l.from == packet.index && l.from > 0)
            {
                rows.push(Row::LayoutChange(layout));
            }
            if !self.hidden_pids.contains(&packet.pid) {
                rows.push(Row::Packet(packet.index));
            }
        }
        egui::ScrollArea::both().auto_shrink(false).show_rows(
            ui,
            ROW_HEIGHT,
            rows.len() + 1,
            |ui, visible| {
                ui.spacing_mut().item_spacing.y = 0.0;
                let width = ui.available_width().max(MIN_LIST_WIDTH);
                for row_index in visible {
                    if row_index == 0 {
                        header_row(ui, width, &PACKET_COLUMNS);
                        continue;
                    }
                    match rows[row_index - 1] {
                        Row::LayoutChange(layout) => layout_row(ui, width, range, layout),
                        Row::Packet(index) => {
                            let packet = &range.packets[index];
                            let response = packet_row(
                                ui,
                                width,
                                packet,
                                index == self.selected,
                                (index - start) % 2 == 1,
                            );
                            if response.clicked() {
                                self.selected = index;
                            }
                        }
                    }
                }
            },
        );
    }

    fn detail(&mut self, ui: &mut egui::Ui) {
        crate::theme::compact(ui);
        let range = data::range();
        let packet = &range.packets[self.selected];
        let layout = range.layout_at(packet.index);
        // The highlight belongs to one packet; selecting another clears it.
        let mut highlight = self
            .highlight
            .clone()
            .filter(|(index, _)| *index == packet.index)
            .map(|(_, span)| span);

        ui.horizontal(|ui| {
            ui.heading(format!("Packet #{}", packet.index + 1));
            ui.weak(format!("0x{:08X}", packet.offset));
        });
        ui.horizontal(|ui| {
            ui.label(
                egui::RichText::new(packet.kind.label())
                    .color(packet.kind.color())
                    .strong(),
            );
            ui.monospace(format!("PID 0x{:04X}", packet.pid));
            if packet.cc_error {
                badge(ui, "CC ERROR", Verdict::Warn);
            }
            if packet.pcr_interval_ms.is_some_and(|ms| ms > 100.0) {
                badge(ui, "PCR NG", Verdict::Ng);
            }
        });
        ui.weak(format!(
            "Streams at this point (PMT version {}, from packet #{}): video 0x{:04X} · audio 0x{:04X}",
            layout.pmt_version,
            layout.from + 1,
            layout.video_pid,
            layout.audio_pid
        ));
        ui.add_space(6.0);

        if let Some(unit) = packet.unit.map(|id| &range.units[id]) {
            self.unit_card(ui, unit, packet);
            ui.add_space(6.0);
        }

        ui.horizontal(|ui| {
            ui.strong("This packet");
            ui.weak("· click a field to highlight its bytes");
        });
        field_tree(
            ui,
            "TS header (4 bytes)",
            layer_color(ByteLayer::TsHeader),
            &[
                Field::new("sync_byte", "0x47", 0..1),
                Field::new(
                    "payload_unit_start_indicator",
                    if packet.pusi { "1" } else { "0" },
                    1..2,
                ),
                Field::new("PID", format!("0x{:04X}", packet.pid), 1..3),
                Field::new(
                    "adaptation_field_control",
                    if packet.pcr.is_some() {
                        "11 (AF + payload)"
                    } else {
                        "01 (payload only)"
                    },
                    3..4,
                ),
                Field::new("continuity_counter", packet.cc.to_string(), 3..4),
            ],
            &mut highlight,
        );
        if let Some(pcr) = packet.pcr {
            egui::CollapsingHeader::new(
                egui::RichText::new("Adaptation field (8 bytes)")
                    .color(layer_color(ByteLayer::AdaptationField)),
            )
            .default_open(true)
            .show(ui, |ui| {
                key_values(
                    ui,
                    "adaptation_field",
                    &[
                        Field::new("adaptation_field_length", "7", 4..5),
                        Field::new("PCR_flag", "1", 5..6),
                        Field::new("program_clock_reference", format!("{pcr:.6} s"), 6..12),
                    ],
                    &mut highlight,
                );
                if let Some(ms) = packet.pcr_interval_ms {
                    ui.horizontal(|ui| {
                        ui.weak("interval from previous PCR");
                        ui.monospace(format!("{ms:.1} ms"));
                        if ms > 100.0 {
                            badge(ui, "NG", Verdict::Ng);
                        } else {
                            badge(ui, "OK", Verdict::Ok);
                        }
                    });
                }
            });
        }
        let payload = match (packet.kind, packet.pusi) {
            (Kind::Null, _) => "Stuffing (null packet)",
            (kind, true) if kind.is_section() => "Section start (pointer_field, table header)",
            (_, true) => "PES header starts here",
            (kind, false) if kind.is_section() => "Section continuation",
            (_, false) => "PES payload continuation",
        };
        ui.horizontal(|ui| {
            ui.weak("Payload");
            ui.label(payload);
        });
        let start = data::payload_start(packet);
        match packet.kind {
            Kind::Pat if packet.pusi => pat_section(ui, start, &mut highlight),
            Kind::Pmt if packet.pusi => pmt_section(ui, range, packet.index, start, &mut highlight),
            Kind::Video | Kind::Audio if packet.pusi => {
                if let Some(unit) = packet.unit.map(|id| &range.units[id]) {
                    pes_header(ui, unit, start, &mut highlight);
                }
            }
            _ => {}
        }

        ui.add_space(8.0);
        ui.separator();
        hex_dump(ui, packet, highlight.as_ref());
        self.highlight = highlight.map(|span| (packet.index, span));
    }

    /// The PES or section the selected packet belongs to, with navigation within it.
    fn unit_card(&mut self, ui: &mut egui::Ui, unit: &Unit, packet: &Packet) {
        let range = data::range();
        let position = unit.position_of(packet.index).unwrap_or(0);
        let mut target = None;
        egui::Frame::group(ui.style()).show(ui, |ui| {
            ui.set_width(ui.available_width());
            ui.horizontal(|ui| {
                ui.weak("Belongs to");
                ui.label(
                    egui::RichText::new(unit.name())
                        .color(unit.kind.color())
                        .strong(),
                );
            });
            let total = format!(
                "{} / {}{}",
                position + 1,
                unit.packets.len(),
                if unit.continues_after_range { "+" } else { "" }
            );
            let first = if unit.starts_in_range {
                format!(
                    "#{} · 0x{:08X}",
                    unit.first() + 1,
                    range.packets[unit.first()].offset
                )
            } else {
                "before this range (header not available)".to_owned()
            };
            let mut fields = vec![("Packet", total), ("First packet", first)];
            if let Some(dts) = unit.dts {
                fields.push(("DTS", format!("{dts:.6} s")));
            }
            if let Some(pts) = unit.pts {
                fields.push(("PTS", format!("{pts:.6} s")));
            }
            if unit.pts.is_some() {
                fields.push(("PES header", "in first packet".to_owned()));
            }
            let fields: Vec<Field> = fields
                .into_iter()
                .map(|(key, value)| Field::info(key, value))
                .collect();
            key_values(ui, "unit_card", &fields, &mut None);
            ui.add_space(4.0);
            ui.horizontal(|ui| {
                let previous = position.checked_sub(1).map(|p| unit.packets[p]);
                let next = unit.packets.get(position + 1).copied();
                if ui
                    .add_enabled(position > 0, egui::Button::new("First packet"))
                    .clicked()
                {
                    target = Some(unit.first());
                }
                if ui
                    .add_enabled(
                        previous.is_some(),
                        egui::Button::new((icons::text(icons::CARET_LEFT), "Previous")),
                    )
                    .clicked()
                {
                    target = previous;
                }
                if ui
                    .add_enabled(
                        next.is_some(),
                        egui::Button::new(("Next", icons::text(icons::CARET_RIGHT))),
                    )
                    .clicked()
                {
                    target = next;
                }
            });
            ui.weak("Moves within this unit; other PIDs' packets in between are skipped.");
        });
        if let Some(index) = target {
            self.select(index);
        }
    }
}

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

/// (title, width, description shown on hover)
pub(super) const PACKET_COLUMNS: [(&str, f32, &str); 9] = [
    ("#", 56.0, "Packet number within the analysis range"),
    ("Offset", 100.0, "Byte offset of the packet in the file"),
    ("PID", 64.0, "Packet identifier"),
    (
        "Type",
        60.0,
        "What the PID carries, from the PAT/PMT in effect at this position",
    ),
    (
        "PUSI",
        46.0,
        "payload_unit_start_indicator: a PES packet or PSI section starts in this packet",
    ),
    (
        "AF",
        52.0,
        "Adaptation field; PCR when it carries a program clock reference",
    ),
    (
        "CC",
        34.0,
        "continuity_counter: 4-bit counter that increments with each packet of the same PID",
    ),
    (
        "Unit",
        190.0,
        "The PES packet or PSI section this packet belongs to, and its position in it",
    ),
    ("Notes", 200.0, "Problems detected at this packet"),
];
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

fn layout_row(ui: &mut egui::Ui, width: f32, range: &Range, layout: usize) {
    let (rect, _) = ui.allocate_exact_size(egui::vec2(width, ROW_HEIGHT), egui::Sense::hover());
    let previous = &range.layouts[layout - 1];
    let current = &range.layouts[layout];
    ui.painter().rect_filled(
        rect.shrink2(egui::vec2(0.0, 2.0)),
        3,
        mock::PSI_COLOR.gamma_multiply(0.18),
    );
    ui.painter().text(
        egui::pos2(rect.left() + TEXT_INSET, rect.center().y),
        egui::Align2::LEFT_CENTER,
        format!(
            "PMT updated (version {} → {}): audio PID 0x{:04X} → 0x{:04X}",
            previous.pmt_version, current.pmt_version, previous.audio_pid, current.audio_pid
        ),
        egui::FontId::proportional(12.5),
        mock::PSI_COLOR,
    );
}

/// One packet row; the whole row is clickable.
fn packet_row(
    ui: &mut egui::Ui,
    width: f32,
    packet: &Packet,
    selected: bool,
    striped: bool,
) -> egui::Response {
    let (rect, response) =
        ui.allocate_exact_size(egui::vec2(width, ROW_HEIGHT), egui::Sense::click());
    if !ui.is_rect_visible(rect) {
        return response;
    }
    paint_row_background(ui, rect, &response, selected, striped);
    let painter = ui.painter();

    let unit = packet.unit.map(|id| &data::range().units[id]);
    let unit_text = unit.map_or(String::new(), |unit| {
        let position = unit.position_of(packet.index).unwrap_or(0) + 1;
        let total = format!(
            "{}{}",
            unit.packets.len(),
            if unit.continues_after_range { "+" } else { "" }
        );
        format!("{} · {position}/{total}", unit.name())
    });
    let mut notes = Vec::new();
    if packet.cc_error {
        notes.push("CC error".to_owned());
    }
    if let Some(ms) = packet.pcr_interval_ms.filter(|&ms| ms > 100.0) {
        notes.push(format!("PCR interval {ms:.1} ms (NG)"));
    }
    let cells = [
        ((packet.index + 1).to_string(), 56.0),
        (format!("0x{:08X}", packet.offset), 100.0),
        (format!("0x{:04X}", packet.pid), 64.0),
        (String::new(), 60.0),
        (
            if packet.pusi {
                "●".to_owned()
            } else {
                String::new()
            },
            46.0,
        ),
        (
            if packet.pcr.is_some() {
                "PCR".to_owned()
            } else {
                String::new()
            },
            52.0,
        ),
        (
            if packet.kind == Kind::Null {
                "–".to_owned()
            } else {
                packet.cc.to_string()
            },
            34.0,
        ),
        (unit_text, 190.0),
    ];
    let text_color = row_text_color(ui, selected);
    paint_cells(ui, rect, TEXT_INSET, &cells, text_color, false);

    // Colored cells drawn over the plain ones.
    let type_x = rect.left() + TEXT_INSET + 56.0 + 100.0 + 64.0;
    let font = egui::TextStyle::Monospace.resolve(ui.style());
    painter.text(
        egui::pos2(type_x, rect.center().y),
        egui::Align2::LEFT_CENTER,
        packet.kind.label(),
        font.clone(),
        packet.kind.color(),
    );
    let notes_x =
        rect.left() + TEXT_INSET + PACKET_COLUMNS[..8].iter().map(|(_, w, _)| w).sum::<f32>();
    painter.text(
        egui::pos2(notes_x, rect.center().y),
        egui::Align2::LEFT_CENTER,
        notes.join(" · "),
        font,
        mock::LIMIT_COLOR,
    );
    response
}

pub(super) type ByteSpan = std::ops::Range<usize>;

/// A decoded field and the bytes of the packet it was read from.
pub(super) struct Field {
    key: String,
    value: String,
    bytes: Option<ByteSpan>,
}

impl Field {
    pub(super) fn new(key: impl Into<String>, value: impl Into<String>, bytes: ByteSpan) -> Self {
        Self {
            key: key.into(),
            value: value.into(),
            bytes: Some(bytes),
        }
    }

    /// A derived value with no bytes of its own in this packet.
    pub(super) fn info(key: impl Into<String>, value: impl Into<String>) -> Self {
        Self {
            key: key.into(),
            value: value.into(),
            bytes: None,
        }
    }
}

pub(super) fn field_tree(
    ui: &mut egui::Ui,
    title: &str,
    color: egui::Color32,
    fields: &[Field],
    highlight: &mut Option<ByteSpan>,
) {
    egui::CollapsingHeader::new(egui::RichText::new(title).color(color))
        .default_open(true)
        .show(ui, |ui| key_values(ui, title, fields, highlight));
}

/// Cells of one clickable field or table row; clicking toggles the highlight of its bytes.
fn span_cells(
    ui: &mut egui::Ui,
    cells: &[egui::RichText],
    span: &ByteSpan,
    highlight: &mut Option<ByteSpan>,
) {
    let on = highlight.as_ref() == Some(span);
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
        *highlight = if on { None } else { Some(span.clone()) };
    }
}

pub(super) fn key_values(
    ui: &mut egui::Ui,
    id: &str,
    fields: &[Field],
    highlight: &mut Option<ByteSpan>,
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
                        span,
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
    rows: &[(Vec<String>, ByteSpan)],
    highlight: &mut Option<ByteSpan>,
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
            for (cells, span) in rows {
                let cells: Vec<egui::RichText> = cells
                    .iter()
                    .map(|c| egui::RichText::new(c).monospace())
                    .collect();
                span_cells(ui, &cells, span, highlight);
                ui.end_row();
            }
        });
}

fn hex_dump(ui: &mut egui::Ui, packet: &Packet, highlight: Option<&ByteSpan>) {
    let bytes: Vec<(u8, egui::Color32)> = data::packet_bytes(packet)
        .into_iter()
        .map(|(byte, layer)| (byte, layer_color(layer)))
        .collect();
    hex_dump_bytes(ui, &bytes, highlight);
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

pub(super) fn layer_color(layer: ByteLayer) -> egui::Color32 {
    match layer {
        ByteLayer::TsHeader => egui::Color32::from_rgb(0x7c, 0xc4, 0xff),
        ByteLayer::AdaptationField => mock::PCR_COLOR,
        ByteLayer::PesHeader => egui::Color32::from_rgb(0x8f, 0xe3, 0x9f),
        ByteLayer::Section => mock::PSI_COLOR,
        ByteLayer::Payload => egui::Color32::GRAY,
    }
}

fn section_header(title: &str) -> egui::CollapsingHeader {
    egui::CollapsingHeader::new(egui::RichText::new(title).color(layer_color(ByteLayer::Section)))
        .default_open(true)
}

/// `p` is the payload start: the pointer_field, then the section.
fn pat_section(ui: &mut egui::Ui, p: usize, highlight: &mut Option<ByteSpan>) {
    section_header("PAT section (16 bytes)").show(ui, |ui| {
        key_values(
            ui,
            "pat_fields",
            &[
                Field::new("pointer_field", "0", p..p + 1),
                Field::new(
                    "table_id",
                    "0x00 (program_association_section)",
                    p + 1..p + 2,
                ),
                Field::new("section_length", "13", p + 2..p + 4),
                Field::new("transport_stream_id", "0x7FE1", p + 4..p + 6),
                Field::new("version_number", "0", p + 6..p + 7),
                Field::new("current_next_indicator", "1", p + 6..p + 7),
                Field::new("section_number / last", "0 / 0", p + 7..p + 9),
                Field::new("CRC_32", "0x2DBCB053 (OK)", p + 17..p + 21),
            ],
            highlight,
        );
        ui.add_space(4.0);
        ui.weak("Programs");
        span_table(
            ui,
            "pat_programs",
            &["program_number", "PID"],
            &[
                (
                    vec!["0 (network)".into(), "0x0010 (NIT)".into()],
                    p + 9..p + 13,
                ),
                (vec!["1".into(), "0x1000 (PMT)".into()], p + 13..p + 17),
            ],
            highlight,
        );
    });
}

fn pmt_section(
    ui: &mut egui::Ui,
    range: &Range,
    index: usize,
    p: usize,
    highlight: &mut Option<ByteSpan>,
) {
    let layout = range.layout_at(index);
    section_header("PMT section (28 bytes)").show(ui, |ui| {
        key_values(
            ui,
            "pmt_fields",
            &[
                Field::new("pointer_field", "0", p..p + 1),
                Field::new("table_id", "0x02 (TS_program_map_section)", p + 1..p + 2),
                Field::new("section_length", "23", p + 2..p + 4),
                Field::new("program_number", "1", p + 4..p + 6),
                Field::new(
                    "version_number",
                    layout.pmt_version.to_string(),
                    p + 6..p + 7,
                ),
                Field::new("current_next_indicator", "1", p + 6..p + 7),
                Field::new("section_number / last", "0 / 0", p + 7..p + 9),
                Field::new(
                    "PCR_PID",
                    format!("0x{:04X}", layout.video_pid),
                    p + 9..p + 11,
                ),
                Field::new("program_info_length", "0", p + 11..p + 13),
                Field::new("CRC_32", "0x8A3F21C4 (OK)", p + 23..p + 27),
            ],
            highlight,
        );
        if layout.from == index && layout.from > 0 {
            let previous = range.layout_at(index - 1);
            ui.colored_label(
                mock::PSI_COLOR,
                format!(
                    "Version changed from v{}: audio PID 0x{:04X} → 0x{:04X}",
                    previous.pmt_version, previous.audio_pid, layout.audio_pid
                ),
            );
        }
        ui.add_space(4.0);
        ui.weak("Elementary streams");
        let rows: Vec<(Vec<String>, ByteSpan)> = [
            ("0x1B", layout.video_pid, "H.264 video"),
            ("0x0F", layout.audio_pid, "AAC audio (ADTS)"),
        ]
        .iter()
        .enumerate()
        .map(|(i, (stream_type, pid, description))| {
            let start = p + 13 + i * 5;
            (
                vec![
                    (*stream_type).into(),
                    format!("0x{pid:04X}"),
                    "0".into(),
                    (*description).into(),
                ],
                start..start + 5,
            )
        })
        .collect();
        span_table(
            ui,
            "pmt_streams",
            &["stream_type", "PID", "ES_info_length", "Description"],
            &rows,
            highlight,
        );
    });
}

fn pes_header(ui: &mut egui::Ui, unit: &Unit, p: usize, highlight: &mut Option<ByteSpan>) {
    let video = unit.kind == Kind::Video;
    let mut fields = vec![
        Field::new("packet_start_code_prefix", "0x000001", p..p + 3),
        Field::new(
            "stream_id",
            if video {
                "0xE0 (video)"
            } else {
                "0xC0 (audio)"
            },
            p + 3..p + 4,
        ),
        Field::new(
            "PES_packet_length",
            if video { "0 (unbounded)" } else { "552" },
            p + 4..p + 6,
        ),
        Field::new(
            "PTS_DTS_flags",
            if unit.dts.is_some() { "11" } else { "10" },
            p + 7..p + 8,
        ),
        Field::new(
            "PES_header_data_length",
            if unit.dts.is_some() { "10" } else { "5" },
            p + 8..p + 9,
        ),
    ];
    if let Some(pts) = unit.pts {
        fields.push(Field::new("PTS", format!("{pts:.6} s"), p + 9..p + 14));
    }
    if let Some(dts) = unit.dts {
        fields.push(Field::new("DTS", format!("{dts:.6} s"), p + 14..p + 19));
    }
    egui::CollapsingHeader::new(
        egui::RichText::new("PES header").color(layer_color(ByteLayer::PesHeader)),
    )
    .default_open(true)
    .show(ui, |ui| key_values(ui, "pes_header", &fields, highlight));
}

/// Distinct PIDs in the range: PSI first (PAT, then PMT), then elementary
/// streams, then null packets; PID order within each group.
fn pids_in_range() -> Vec<(u16, Kind)> {
    let mut pids: Vec<(u16, Kind)> = Vec::new();
    for packet in &data::range().packets {
        if !pids.iter().any(|(pid, _)| *pid == packet.pid) {
            pids.push((packet.pid, packet.kind));
        }
    }
    let group = |kind: Kind| match kind {
        Kind::Pat => 0,
        Kind::Pmt => 1,
        Kind::Video | Kind::Audio => 2,
        Kind::Null => 3,
    };
    pids.sort_by_key(|(pid, kind)| (group(*kind), *pid));
    pids
}
