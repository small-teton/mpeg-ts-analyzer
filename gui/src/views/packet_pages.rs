//! Packets from the CLI: the analyzed range is read a page at a time with
//! `--output packets`, and the selected packet is decoded with
//! `--output packet`. Only the pages near the current one are kept.

use std::collections::{BTreeMap, BTreeSet, VecDeque};
use std::path::PathBuf;

use eframe::egui;

use super::packets::{
    ByteSpan, Field, MIN_LIST_WIDTH, ROW_HEIGHT, TEXT_INSET, field_tree, header_row,
    hex_dump_bytes, key_values, layer_color, paint_cells, paint_row_background, row_text_color,
    span_table,
};
use crate::cli::{self, Cli, Request};
use crate::icons;
use crate::mock;
use crate::mock_packets::ByteLayer;
use crate::range_bar;
use crate::report::{
    AnalysisReport, PacketDetail, PacketLayer, PacketSummary, PacketUnit, PacketsPage,
    stream_type_name,
};

pub const PAGE_SIZE: u64 = 100;
/// Pages kept in memory besides the one shown.
const CACHED_PAGES: usize = 4;

const NULL_PID: &str = "0x1fff";

/// (title, width, description shown on hover)
const COLUMNS: [(&str, f32, &str); 8] = [
    ("#", 64.0, "Packet number within the analysis range"),
    ("Offset", 100.0, "Byte offset of the packet in the file"),
    ("PID", 64.0, "Packet identifier"),
    (
        "Type",
        150.0,
        "What the PID carries, from the analysis of this range",
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
    ("Notes", 220.0, "Problems in this packet's header"),
];

/// Where the packets of the analyzed range are: known once the first page
/// has told where the first packet boundary is.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Geometry {
    pub first: u64,
    pub packet_size: u64,
    pub count: u64,
}

impl Geometry {
    /// `first` is the first packet boundary at or after the range start;
    /// `end` is where the range ends (at most the file size).
    pub fn new(first: u64, end: u64, packet_size: u64) -> Self {
        Self {
            first,
            packet_size,
            count: end.saturating_sub(first) / packet_size,
        }
    }

    pub fn pages(&self) -> u64 {
        self.count.div_ceil(PAGE_SIZE).max(1)
    }

    /// The `--offset` and `--count` that read a page.
    pub fn page_request(&self, page: u64) -> (u64, u64) {
        let start = page * PAGE_SIZE;
        (
            self.first + start * self.packet_size,
            PAGE_SIZE.min(self.count.saturating_sub(start)),
        )
    }

    /// The packet's 0-based index in the range, if the offset is one of its packets.
    pub fn index_of(&self, offset: u64) -> Option<u64> {
        let delta = offset.checked_sub(self.first)?;
        let index = delta / self.packet_size;
        (delta % self.packet_size == 0 && index < self.count).then_some(index)
    }
}

enum Fetch<T> {
    Loading(Request<T>),
    Ready(T),
    Failed(String),
}

impl<T: Send + 'static> Fetch<T> {
    fn poll(&mut self) {
        if let Fetch::Loading(request) = self
            && let Some(result) = request.poll()
        {
            *self = match result {
                Ok(value) => Fetch::Ready(value),
                Err(err) => Fetch::Failed(err),
            };
        }
    }
}

pub struct PacketPages {
    input: PathBuf,
    range_offset: u64,
    range_end: u64,
    packet_size: u64,
    geometry: Option<Geometry>,
    /// Reads the first page, which tells where the range's packets start.
    probe: Option<Fetch<PacketsPage>>,
    page: u64,
    pages: BTreeMap<u64, Fetch<PacketsPage>>,
    recent: VecDeque<u64>,
    selected: Option<u64>,
    detail: Option<(u64, Fetch<PacketDetail>)>,
    /// A packet to show once the geometry is known.
    pending: Option<u64>,
    /// What each PID carries, and the PIDs offered in the filter.
    labels: BTreeMap<String, String>,
    hidden_pids: BTreeSet<String>,
    highlight: Option<(u64, ByteSpan)>,
}

impl PacketPages {
    pub fn new(input: PathBuf, report: &AnalysisReport, offset: u64, limit: u64) -> Self {
        let file_size = report.input.file_size_bytes;
        Self {
            input,
            range_offset: offset,
            range_end: (offset + limit).min(file_size),
            packet_size: u64::from(report.input.packet_size_bytes.unwrap_or(188)),
            geometry: None,
            probe: None,
            page: 0,
            pages: BTreeMap::new(),
            recent: VecDeque::new(),
            selected: None,
            detail: None,
            pending: None,
            labels: pid_labels(report),
            hidden_pids: BTreeSet::new(),
            highlight: None,
        }
    }

    #[cfg(test)]
    fn new_for_test(input: PathBuf) -> Self {
        Self {
            input,
            range_offset: 0,
            range_end: 0,
            packet_size: 188,
            geometry: None,
            probe: None,
            page: 0,
            pages: BTreeMap::new(),
            recent: VecDeque::new(),
            selected: None,
            detail: None,
            pending: None,
            labels: BTreeMap::new(),
            hidden_pids: BTreeSet::new(),
            highlight: None,
        }
    }

    /// Shows the packet at `offset` (e.g. from Timeline's "Open packet").
    pub fn go_to(&mut self, offset: u64) {
        match self.geometry {
            Some(geometry) => {
                if let Some(index) = geometry.index_of(offset) {
                    self.page = index / PAGE_SIZE;
                    self.select(offset);
                }
            }
            None => self.pending = Some(offset),
        }
    }

    fn select(&mut self, offset: u64) {
        if self.selected != Some(offset) {
            self.selected = Some(offset);
            self.detail = None;
        }
    }

    fn args(&self, output: &str, offset: u64) -> Vec<String> {
        vec![
            self.input.to_string_lossy().into_owned(),
            "--format".into(),
            "json".into(),
            "--output".into(),
            output.into(),
            "--offset".into(),
            offset.to_string(),
            "--packet-size".into(),
            self.packet_size.to_string(),
        ]
    }

    fn page_args(&self, offset: u64, count: u64) -> Vec<String> {
        let mut args = self.args("packets", offset);
        args.extend(["--count".into(), count.to_string()]);
        args
    }

    /// Starts the requests the screen needs and takes in finished ones.
    fn update(&mut self, cli: &Cli, ctx: &egui::Context) {
        let repaint = {
            let ctx = ctx.clone();
            move || ctx.request_repaint()
        };
        let Some(geometry) = self.geometry else {
            match &mut self.probe {
                None => {
                    let args = self.page_args(self.range_offset, PAGE_SIZE);
                    self.probe = Some(start(cli, args, parse_page, repaint));
                }
                Some(probe) => {
                    probe.poll();
                    let Fetch::Ready(page) = probe else {
                        return;
                    };
                    // An empty range has no first packet.
                    let first = page.first_offset_bytes.unwrap_or(self.range_end);
                    self.geometry = Some(Geometry::new(first, self.range_end, self.packet_size));
                    if let Some(page) = self.probe.take() {
                        self.pages.insert(0, page);
                        self.recent.push_back(0);
                    }
                    if let Some(offset) = self.pending.take() {
                        self.go_to(offset);
                    }
                }
            }
            return;
        };

        if !self.pages.contains_key(&self.page) {
            let (offset, count) = geometry.page_request(self.page);
            let args = self.page_args(offset, count);
            self.pages
                .insert(self.page, start(cli, args, parse_page, repaint.clone()));
        }
        self.recent.retain(|&p| p != self.page);
        self.recent.push_back(self.page);
        while self.recent.len() > CACHED_PAGES + 1 {
            // Dropping a page still loading kills its CLI run, so only the
            // last requested page keeps running when paging quickly.
            if let Some(old) = self.recent.pop_front() {
                self.pages.remove(&old);
            }
        }
        for page in self.pages.values_mut() {
            page.poll();
        }

        if let Some(offset) = self.selected {
            match &mut self.detail {
                Some((at, detail)) if *at == offset => detail.poll(),
                _ => {
                    let mut args = self.args("packet", offset);
                    args.extend([
                        "--range-offset".into(),
                        self.range_offset.to_string(),
                        "--range-limit".into(),
                        (self.range_end - self.range_offset).to_string(),
                    ]);
                    self.detail = Some((offset, start(cli, args, parse_detail, repaint)));
                }
            }
        }
    }

    pub fn show(&mut self, ui: &mut egui::Ui, cli: &Cli) {
        self.update(cli, ui.ctx());
        egui::Panel::right("packet_detail_live")
            .resizable(true)
            .default_size(480.0)
            .show(ui, |ui| {
                egui::ScrollArea::vertical().show(ui, |ui| self.detail(ui));
            });
        egui::CentralPanel::default().show(ui, |ui| {
            crate::theme::compact(ui);
            let Some(geometry) = self.geometry else {
                match &self.probe {
                    Some(Fetch::Failed(err)) => {
                        ui.colored_label(ui.visuals().error_fg_color, err);
                    }
                    _ => {
                        ui.horizontal(|ui| {
                            ui.spinner();
                            ui.weak("Reading the first page…");
                        });
                    }
                }
                return;
            };
            self.pager(ui, geometry);
            ui.separator();
            self.packet_list(ui, geometry);
        });
    }

    fn pager(&mut self, ui: &mut egui::Ui, geometry: Geometry) {
        let pages = geometry.pages();
        ui.horizontal(|ui| {
            let first = self.page > 0;
            let last = self.page + 1 < pages;
            if ui.add_enabled(first, egui::Button::new("First")).clicked() {
                self.page = 0;
            }
            if ui
                .add_enabled(first, egui::Button::new(icons::text(icons::CARET_LEFT)))
                .clicked()
            {
                self.page -= 1;
            }
            ui.monospace(format!("Page {} / {pages}", self.page + 1));
            if ui
                .add_enabled(last, egui::Button::new(icons::text(icons::CARET_RIGHT)))
                .clicked()
            {
                self.page += 1;
            }
            if ui.add_enabled(last, egui::Button::new("Last")).clicked() {
                self.page = pages - 1;
            }
            let (_, count) = geometry.page_request(self.page);
            let start = self.page * PAGE_SIZE;
            ui.weak(format!(
                "packets {}–{} of {} in range · {}-byte packets",
                start + 1,
                start + count,
                geometry.count,
                geometry.packet_size
            ));
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                self.pid_filter(ui);
            });
        });
    }

    /// Checkbox per PID seen in the analysis, so any combination can be shown.
    fn pid_filter(&mut self, ui: &mut egui::Ui) {
        let total = self.labels.len();
        let shown = total - self.hidden_pids.len();
        let label = if shown == total {
            "PIDs: all".to_owned()
        } else {
            format!("PIDs: {shown} of {total}")
        };
        ui.menu_button(label, |ui| {
            ui.horizontal(|ui| {
                if ui.button("All").clicked() {
                    self.hidden_pids.clear();
                }
                if ui.button("None").clicked() {
                    self.hidden_pids = self.labels.keys().cloned().collect();
                }
            });
            ui.separator();
            for (pid, carries) in &self.labels {
                let mut checked = !self.hidden_pids.contains(pid);
                if ui
                    .checkbox(&mut checked, format!("{pid}  {carries}"))
                    .changed()
                {
                    if checked {
                        self.hidden_pids.remove(pid);
                    } else {
                        self.hidden_pids.insert(pid.clone());
                    }
                }
            }
        });
    }

    fn packet_list(&mut self, ui: &mut egui::Ui, geometry: Geometry) {
        let packets: Vec<PacketSummary> = match self.pages.get(&self.page) {
            Some(Fetch::Ready(page)) => page
                .packets
                .iter()
                .filter(|p| {
                    !p.pid
                        .as_ref()
                        .is_some_and(|pid| self.hidden_pids.contains(pid))
                })
                .cloned()
                .collect(),
            Some(Fetch::Failed(err)) => {
                ui.colored_label(ui.visuals().error_fg_color, err);
                return;
            }
            _ => {
                ui.horizontal(|ui| {
                    ui.spinner();
                    ui.weak("Reading packets…");
                });
                return;
            }
        };
        let mut clicked = None;
        egui::ScrollArea::both().auto_shrink(false).show_rows(
            ui,
            ROW_HEIGHT,
            packets.len() + 1,
            |ui, visible| {
                ui.spacing_mut().item_spacing.y = 0.0;
                let width = ui.available_width().max(MIN_LIST_WIDTH);
                for row in visible {
                    if row == 0 {
                        header_row(ui, width, &COLUMNS);
                        continue;
                    }
                    let packet = &packets[row - 1];
                    let index = geometry.index_of(packet.offset_bytes).unwrap_or(0);
                    let selected = self.selected == Some(packet.offset_bytes);
                    let carries = packet
                        .pid
                        .as_ref()
                        .and_then(|pid| self.labels.get(pid))
                        .map_or("", String::as_str);
                    if packet_row(ui, width, packet, index, carries, selected).clicked() {
                        clicked = Some(packet.offset_bytes);
                    }
                }
            },
        );
        if let Some(offset) = clicked {
            self.select(offset);
        }
    }

    fn detail(&mut self, ui: &mut egui::Ui) {
        crate::theme::compact(ui);
        let Some(offset) = self.selected else {
            ui.weak("Select a packet to decode it.");
            return;
        };
        let detail = match &self.detail {
            Some((at, Fetch::Ready(detail))) if *at == offset => detail,
            Some((at, Fetch::Failed(err))) if *at == offset => {
                ui.colored_label(ui.visuals().error_fg_color, err);
                return;
            }
            _ => {
                ui.horizontal(|ui| {
                    ui.spinner();
                    ui.weak("Decoding the packet…");
                });
                return;
            }
        };
        let Some(packet) = &detail.packet else {
            ui.weak("The CLI returned no packet.");
            return;
        };
        let mut highlight = self
            .highlight
            .clone()
            .filter(|(at, _)| *at == offset)
            .map(|(_, span)| span);
        let index = self.geometry.and_then(|g| g.index_of(offset));

        ui.horizontal(|ui| {
            ui.heading(match index {
                Some(index) => format!("Packet #{}", index + 1),
                None => "Packet".to_owned(),
            });
            ui.weak(range_bar::bytes(offset));
        });
        let mut target = None;
        if let Some(unit) = &detail.unit {
            target = unit_card(ui, unit);
            ui.add_space(6.0);
        }

        ui.horizontal(|ui| {
            ui.strong("This packet");
            ui.weak("· click a field to highlight its bytes");
        });
        for layer in &packet.layers {
            layer_fields(ui, layer, &mut highlight);
        }
        for warning in &detail.warnings {
            ui.colored_label(ui.visuals().warn_fg_color, &warning.message);
        }

        ui.add_space(8.0);
        ui.separator();
        let raw = packet.raw();
        let colors = byte_colors(raw.len(), &packet.layers);
        let bytes: Vec<(u8, egui::Color32)> = raw.into_iter().zip(colors).collect();
        hex_dump_bytes(ui, &bytes, highlight.as_ref());
        self.highlight = highlight.map(|span| (offset, span));

        if let Some(target) = target {
            self.go_to(target);
        }
    }
}

fn start<T: Send + 'static>(
    cli: &Cli,
    args: Vec<String>,
    parse: fn(&[u8]) -> Result<T, String>,
    repaint: impl Fn() + Send + 'static,
) -> Fetch<T> {
    match Request::start(cli, args, parse, repaint) {
        Ok(request) => Fetch::Loading(request),
        Err(err) => Fetch::Failed(err),
    }
}

fn parse_page(json: &[u8]) -> Result<PacketsPage, String> {
    crate::report::parse_packets_page(json, cli::PACKETS_SCHEMA_MAJOR)
}

fn parse_detail(json: &[u8]) -> Result<PacketDetail, String> {
    crate::report::parse_packet_detail(json, cli::PACKETS_SCHEMA_MAJOR)
}

/// What each PID of the range carries: PSI and elementary streams from the
/// programs, other PIDs with the label the continuity check gave them.
pub fn pid_labels(report: &AnalysisReport) -> BTreeMap<String, String> {
    let mut labels = BTreeMap::new();
    if let Some(continuity) = &report.continuity_counter {
        for pid in &continuity.pids {
            labels.insert(pid.pid.clone(), pid.label.clone());
        }
    }
    labels.insert("0x0000".to_owned(), "PAT".to_owned());
    for program in &report.programs {
        labels.insert(
            program.pmt_pid.clone(),
            format!("PMT (program {})", program.program_number),
        );
        for stream in &program.elementary_streams {
            labels.insert(stream.pid.clone(), stream_type_name(&stream.stream_type));
        }
    }
    labels.insert(NULL_PID.to_owned(), "null".to_owned());
    labels
}

/// One packet row; the whole row is clickable.
fn packet_row(
    ui: &mut egui::Ui,
    width: f32,
    packet: &PacketSummary,
    index: u64,
    carries: &str,
    selected: bool,
) -> egui::Response {
    let (rect, response) =
        ui.allocate_exact_size(egui::vec2(width, ROW_HEIGHT), egui::Sense::click());
    if !ui.is_rect_visible(rect) {
        return response;
    }
    paint_row_background(ui, rect, &response, selected, index % 2 == 1);
    let null = packet.pid.as_deref() == Some(NULL_PID);
    let cells = [
        ((index + 1).to_string(), COLUMNS[0].1),
        (format!("0x{:08X}", packet.offset_bytes), COLUMNS[1].1),
        (packet.pid.clone().unwrap_or_default(), COLUMNS[2].1),
        (carries.to_owned(), COLUMNS[3].1),
        (if packet.pusi { "●" } else { "" }.to_owned(), COLUMNS[4].1),
        (
            match (packet.pcr_ms, packet.adaptation_field_control) {
                (Some(_), _) => "PCR",
                (None, 2 | 3) => "AF",
                _ => "",
            }
            .to_owned(),
            COLUMNS[5].1,
        ),
        (
            if null || !packet.valid {
                "–".to_owned()
            } else {
                packet.continuity_counter.to_string()
            },
            COLUMNS[6].1,
        ),
    ];
    paint_cells(
        ui,
        rect,
        TEXT_INSET,
        &cells,
        row_text_color(ui, selected),
        false,
    );
    let notes = packet_notes(packet).join(" · ");
    let notes_x = rect.left() + TEXT_INSET + COLUMNS[..7].iter().map(|(_, w, _)| w).sum::<f32>();
    ui.painter().text(
        egui::pos2(notes_x, rect.center().y),
        egui::Align2::LEFT_CENTER,
        notes,
        egui::TextStyle::Monospace.resolve(ui.style()),
        mock::LIMIT_COLOR,
    );
    response
}

/// Header problems worth flagging in the list.
pub fn packet_notes(packet: &PacketSummary) -> Vec<&'static str> {
    let mut notes = Vec::new();
    if !packet.valid {
        notes.push("no sync byte");
    }
    if packet.transport_error {
        notes.push("transport error");
    }
    if packet.scrambling_control != 0 {
        notes.push("scrambled");
    }
    notes
}

/// The PES or section the packet belongs to. Returns a packet to move to.
fn unit_card(ui: &mut egui::Ui, unit: &PacketUnit) -> Option<u64> {
    let mut target = None;
    egui::Frame::group(ui.style()).show(ui, |ui| {
        ui.set_width(ui.available_width());
        let name = match unit.kind.as_str() {
            "pes" => format!(
                "PES{}",
                unit.stream_id
                    .as_ref()
                    .map_or(String::new(), |id| format!(" (stream_id {id})"))
            ),
            "psi" => format!(
                "PSI section{}",
                unit.table_id
                    .as_ref()
                    .map_or(String::new(), |id| format!(" (table_id {id})"))
            ),
            "none" => "No unit (no payload, or a null packet)".to_owned(),
            _ => "A unit that started before the range".to_owned(),
        };
        ui.horizontal(|ui| {
            ui.weak("Belongs to");
            ui.strong(name);
        });
        if unit.kind == "none" {
            return;
        }
        let mut fields = vec![Field::info(
            "Packet",
            format!("{} / {} in range", unit.position, unit.packets_in_range),
        )];
        fields.push(Field::info(
            "First packet",
            match unit.first_offset_bytes {
                Some(offset) => range_bar::bytes(offset),
                None => "before this range (header not available)".to_owned(),
            },
        ));
        if let Some(dts) = unit.dts_ms {
            fields.push(Field::info("DTS", format!("{:.6} s", dts / 1000.0)));
        }
        if let Some(pts) = unit.pts_ms {
            fields.push(Field::info("PTS", format!("{:.6} s", pts / 1000.0)));
        }
        key_values(ui, "unit_card", &fields, &mut None);
        ui.add_space(4.0);
        ui.horizontal(|ui| {
            if ui
                .add_enabled(
                    unit.position > 1 && unit.first_offset_bytes.is_some(),
                    egui::Button::new("First packet"),
                )
                .clicked()
            {
                target = unit.first_offset_bytes;
            }
            if ui
                .add_enabled(
                    unit.previous_offset_bytes.is_some(),
                    egui::Button::new((icons::text(icons::CARET_LEFT), "Previous")),
                )
                .clicked()
            {
                target = unit.previous_offset_bytes;
            }
            if ui
                .add_enabled(
                    unit.next_offset_bytes.is_some(),
                    egui::Button::new(("Next", icons::text(icons::CARET_RIGHT))),
                )
                .clicked()
            {
                target = unit.next_offset_bytes;
            }
        });
        ui.weak("Moves within this unit across pages; other PIDs' packets in between are skipped.");
    });
    target
}

fn span(field: &crate::report::PacketField) -> ByteSpan {
    field.byte_offset..field.byte_offset + field.byte_length
}

fn byte_layer(name: &str) -> ByteLayer {
    match name {
        "m2ts_header" | "ts_header" => ByteLayer::TsHeader,
        "adaptation_field" => ByteLayer::AdaptationField,
        "pes_header" => ByteLayer::PesHeader,
        "payload" => ByteLayer::Payload,
        _ => ByteLayer::Section,
    }
}

fn layer_title(layer: &PacketLayer) -> String {
    let title = match layer.name.as_str() {
        "m2ts_header" => "M2TS header",
        "ts_header" => "TS header",
        "adaptation_field" => "Adaptation field",
        "payload" => "Payload",
        "pes_header" => "PES header",
        "pat" => "PAT",
        "pmt" => "PMT",
        "psi_section" => "PSI section",
        other => other,
    };
    if layer.truncated {
        format!("{title} (continues in the next packet)")
    } else {
        title.to_owned()
    }
}

fn layer_fields(ui: &mut egui::Ui, layer: &PacketLayer, highlight: &mut Option<ByteSpan>) {
    let fields: Vec<Field> = layer
        .fields
        .iter()
        .map(|f| Field::new(f.name.clone(), f.value.clone(), span(f)))
        .collect();
    let color = layer_color(byte_layer(&layer.name));
    field_tree(ui, &layer_title(layer), color, &fields, highlight);
    if let Some(first) = layer.rows.first() {
        let titles: Vec<&str> = first.iter().map(|f| f.name.as_str()).collect();
        let rows: Vec<(Vec<String>, ByteSpan)> = layer
            .rows
            .iter()
            .filter_map(|row| {
                let start = row.iter().map(|f| f.byte_offset).min()?;
                let end = row.iter().map(|f| f.byte_offset + f.byte_length).max()?;
                Some((row.iter().map(|f| f.value.clone()).collect(), start..end))
            })
            .collect();
        span_table(
            ui,
            &format!("{}_rows", layer.name),
            &titles,
            &rows,
            highlight,
        );
    }
}

/// The color of each packet byte: the layer whose fields cover it, later
/// layers (PES header, sections) over the payload they sit in.
pub fn byte_colors(len: usize, layers: &[PacketLayer]) -> Vec<egui::Color32> {
    let mut colors = vec![layer_color(ByteLayer::Payload); len];
    for layer in layers {
        let color = layer_color(byte_layer(&layer.name));
        for field in layer.fields.iter().chain(layer.rows.iter().flatten()) {
            for byte in span(field) {
                if let Some(c) = colors.get_mut(byte) {
                    *c = color;
                }
            }
        }
    }
    colors
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn pages_cover_every_packet_of_the_range_once() {
        // A range of 1,079 packets starting at a packet boundary 4 bytes in.
        let geometry = Geometry::new(4, 4 + 1079 * 192 + 100, 192);
        assert_eq!(geometry.count, 1079);
        assert_eq!(geometry.pages(), 11);
        let mut next = geometry.first;
        let mut seen = 0;
        for page in 0..geometry.pages() {
            let (offset, count) = geometry.page_request(page);
            assert_eq!(offset, next);
            next += count * 192;
            seen += count;
        }
        assert_eq!(seen, geometry.count);
        assert_eq!(geometry.page_request(10), (4 + 1000 * 192, 79));
        assert_eq!(geometry.index_of(4 + 192 * 150), Some(150));
        assert_eq!(geometry.index_of(5), None);
        assert_eq!(geometry.index_of(0), None);
        assert_eq!(geometry.index_of(4 + 192 * 1079), None);
        assert_eq!(Geometry::new(0, 100, 188).pages(), 1);
    }

    #[test]
    fn labels_pids_from_the_analysis() {
        let report = crate::report::parse_analysis(
            include_bytes!("../../tests/fixtures/analysis_two_programs_188.json"),
            "1",
        )
        .unwrap();
        let labels = pid_labels(&report);
        assert_eq!(labels["0x0000"], "PAT");
        assert_eq!(labels["0x1000"], "PMT (program 1)");
        assert_eq!(labels["0x0100"], "MPEG-2 video");
        assert_eq!(labels["0x0101"], "MPEG-1 audio");
        assert_eq!(labels[NULL_PID], "null");
    }

    #[test]
    fn colors_bytes_by_layer() {
        let field = |offset, length| crate::report::PacketField {
            name: "f".into(),
            value: "v".into(),
            byte_offset: offset,
            byte_length: length,
        };
        let layer = |name: &str, fields| PacketLayer {
            name: name.into(),
            fields,
            rows: Vec::new(),
            truncated: false,
        };
        let layers = [
            layer("ts_header", vec![field(0, 4)]),
            layer("payload", vec![field(4, 184)]),
            layer("pes_header", vec![field(4, 9)]),
        ];
        let colors = byte_colors(188, &layers);
        assert_eq!(colors[0], layer_color(ByteLayer::TsHeader));
        assert_eq!(colors[5], layer_color(ByteLayer::PesHeader));
        assert_eq!(colors[100], layer_color(ByteLayer::Payload));
    }

    fn wait<T: Send + 'static>(request: Request<T>) -> Result<T, String> {
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(60);
        loop {
            if let Some(result) = request.poll() {
                return result;
            }
            assert!(std::time::Instant::now() < deadline, "the CLI timed out");
            std::thread::sleep(std::time::Duration::from_millis(10));
        }
    }

    /// Runs only against a CLI named by MPEG_TS_ANALYZER_CLI (e.g. via `make gui-test`).
    #[test]
    fn pages_visit_every_packet_and_details_match() {
        let Some(path) = std::env::var_os("MPEG_TS_ANALYZER_CLI") else {
            return;
        };
        let cli = cli::check(std::path::Path::new(&path)).expect("capabilities");
        assert!(cli.packet_outputs);
        let samples = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../sample_data");
        for (name, packet_size) in [
            (
                "sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts",
                188,
            ),
            (
                "sample_192byte_two_programs_mpeg2_160x120_25fps_mp2.ts",
                192,
            ),
        ] {
            let input = samples.join(name);
            let size = std::fs::metadata(&input).unwrap().len();
            let mut pages = PacketPages {
                packet_size,
                range_end: size,
                ..PacketPages::new_for_test(input.clone())
            };
            let first = wait(
                Request::start(&cli, pages.page_args(0, PAGE_SIZE), parse_page, || {}).unwrap(),
            )
            .unwrap();
            let geometry = Geometry::new(first.first_offset_bytes.unwrap(), size, packet_size);
            assert_eq!(geometry.count, first.total_packets, "{name}");
            let mut expected = geometry.first;
            for page in 0..geometry.pages() {
                let (offset, count) = geometry.page_request(page);
                let result = wait(
                    Request::start(&cli, pages.page_args(offset, count), parse_page, || {})
                        .unwrap(),
                )
                .unwrap();
                assert_eq!(result.packets.len() as u64, count, "{name} page {page}");
                for packet in &result.packets {
                    assert_eq!(packet.offset_bytes, expected, "{name}");
                    expected += packet_size;
                }
            }
            assert_eq!(expected, geometry.first + geometry.count * packet_size);

            // A video PES start: the decoded PID and spans match the page summary.
            let offset = first
                .packets
                .iter()
                .find(|p| p.pid.as_deref() == Some("0x0100") && p.pusi);
            let offset = offset
                .map(|p| p.offset_bytes)
                .unwrap_or(geometry.first + 160 * packet_size);
            pages.range_offset = 0;
            let mut args = pages.args("packet", offset);
            args.extend(["--range-offset".into(), "0".into()]);
            let detail = wait(Request::start(&cli, args, parse_detail, || {}).unwrap()).unwrap();
            let packet = detail.packet.unwrap();
            assert_eq!(packet.raw().len() as u64, packet_size);
            for field in packet
                .layers
                .iter()
                .flat_map(|l| l.fields.iter().chain(l.rows.iter().flatten()))
            {
                assert!(
                    field.byte_offset + field.byte_length <= packet_size as usize,
                    "{field:?}"
                );
            }
            let header = packet
                .layers
                .iter()
                .find(|l| l.name == "ts_header")
                .unwrap();
            let pid = header.fields.iter().find(|f| f.name == "PID").unwrap();
            let raw = packet.raw();
            let ts = (packet_size - 188) as usize;
            let decoded = format!(
                "0x{:04x}",
                (u16::from(raw[ts + 1] & 0x1f) << 8) | u16::from(raw[ts + 2])
            );
            assert_eq!(pid.value.to_lowercase(), decoded);
        }
    }

    #[test]
    fn flags_header_problems() {
        let packet = PacketSummary {
            offset_bytes: 0,
            valid: true,
            pid: Some("0x0100".into()),
            pusi: false,
            transport_error: true,
            scrambling_control: 2,
            adaptation_field_control: 1,
            continuity_counter: 3,
            pcr_ms: None,
        };
        assert_eq!(packet_notes(&packet), ["transport error", "scrambled"]);
    }
}
