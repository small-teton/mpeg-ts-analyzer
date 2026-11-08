//! Packets from the CLI: the analyzed range is read a page at a time with
//! `--output packets`, and the selected packet is decoded with
//! `--output packet`. Only the pages near the current one are kept.

use std::collections::{BTreeMap, BTreeSet, VecDeque};
use std::ffi::OsString;
use std::ops::RangeInclusive;
use std::path::PathBuf;

use eframe::egui;

use super::packets::{
    ByteLayer, ByteSpan, Field, FieldSelection, MIN_LIST_WIDTH, ROW_HEIGHT, TEXT_INSET,
    bit_inspector, byte_span_text, field_tree, header_row, hex_dump_bytes, key_values, layer_color,
    paint_cells, paint_row_background, row_text_color, span_table,
};
use super::wire_layout::{self, Model};
#[cfg(test)]
use crate::cli;
use crate::cli::{Cli, Reply, Request};
use crate::icons;
use crate::presentation::packet::{field_text, field_value, packet_kind};
use crate::psi::{NULL_PID, PsiLayout};
use crate::range_bar;
use crate::report::{
    AnalysisReport, DecodedPacket, PacketDetail, PacketLayer, PacketSummary, PacketUnit,
    PacketsPage,
};
use crate::theme;

/// How Packets reads a range: packets per page and how many pages stay in
/// memory. Comes from Settings and is fixed for the analysis it was created
/// with; a changed setting applies when the next analysis finishes.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Paging {
    page_size: u64,
    cached_pages: usize,
}

impl Paging {
    /// Packets per page that Settings offers.
    pub const PAGE_SIZES: RangeInclusive<u64> = 10..=10_000;
    /// Pages kept in memory, the one shown included, that Settings offers.
    pub const CACHED_PAGES: RangeInclusive<u32> = 1..=50;

    /// Values out of range (e.g. edited in the saved settings) are clamped.
    pub fn new(packets_per_page: u64, cached_pages: u32) -> Self {
        let (pages, cached) = (Self::PAGE_SIZES, Self::CACHED_PAGES);
        Self {
            page_size: packets_per_page.clamp(*pages.start(), *pages.end()),
            cached_pages: cached_pages.clamp(*cached.start(), *cached.end()) as usize,
        }
    }

    pub fn page_size(&self) -> u64 {
        self.page_size
    }

    pub fn cached_pages(&self) -> usize {
        self.cached_pages
    }
}

impl Default for Paging {
    fn default() -> Self {
        Self::new(100, 5)
    }
}

/// (title, width, description shown on hover)
const COLUMNS: [(&str, f32, &str); 9] = [
    ("#", 64.0, "Packet number within the analysis range"),
    ("Offset", 100.0, "Byte offset of the packet in the file"),
    ("PID", 64.0, "Packet identifier"),
    (
        "Type",
        150.0,
        "What the PID carries in the PAT/PMT layout in effect at this packet",
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
        "Sections",
        190.0,
        "PAT/PMT sections that start in this packet, and whether the analysis applied the copy, saw it repeated, did not apply it, or found it invalid",
    ),
    (
        "Notes",
        260.0,
        "PAT/PMT changes this packet completed, and problems in its header",
    ),
];

/// Where the packets of the analyzed range are: known once the first page
/// has told where the first packet boundary is.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Geometry {
    pub first: u64,
    pub packet_size: u64,
    pub count: u64,
    pub page_size: u64,
}

impl Geometry {
    /// `first` is the first packet boundary at or after the range start;
    /// `end` is where the range ends (at most the file size).
    pub fn new(first: u64, end: u64, packet_size: u64, paging: Paging) -> Self {
        Self {
            first,
            packet_size,
            count: end.saturating_sub(first) / packet_size,
            page_size: paging.page_size(),
        }
    }

    pub fn pages(&self) -> u64 {
        self.count.div_ceil(self.page_size).max(1)
    }

    /// The `--offset` and `--count` that read a page.
    pub fn page_request(&self, page: u64) -> (u64, u64) {
        let start = self.page_start(page);
        (
            self.first + start * self.packet_size,
            self.page_size.min(self.count.saturating_sub(start)),
        )
    }

    /// The 0-based index in the range of the page's first packet.
    pub fn page_start(&self, page: u64) -> u64 {
        page * self.page_size
    }

    /// The page that holds the packet with this 0-based index.
    pub fn page_of(&self, index: u64) -> u64 {
        index / self.page_size
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
    paging: Paging,
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
    /// What each PID carries where, and the PIDs offered in the filter.
    psi: PsiLayout,
    hidden_pids: BTreeSet<String>,
    highlight: Option<(u64, FieldSelection)>,
    /// The detailed inspection page, shown instead of the list while set.
    inspect: Option<Inspect>,
    /// The list's scroll offset, restored when the inspection page closes.
    list_offset: egui::Vec2,
    restore_list_offset: bool,
}

/// The packet the detailed inspection page shows and the page's selection.
/// The list's page, filter and selected row are left as they were, so going
/// back shows the list as it was left.
struct Inspect {
    offset: u64,
    state: wire_layout::State,
}

impl PacketPages {
    pub fn new(
        input: PathBuf,
        report: &AnalysisReport,
        offset: u64,
        limit: u64,
        paging: Paging,
    ) -> Self {
        let file_size = report.input.file_size_bytes;
        Self {
            input,
            range_offset: offset,
            range_end: (offset + limit).min(file_size),
            packet_size: u64::from(report.input.packet_size_bytes.unwrap_or(188)),
            paging,
            geometry: None,
            probe: None,
            page: 0,
            pages: BTreeMap::new(),
            recent: VecDeque::new(),
            selected: None,
            detail: None,
            pending: None,
            psi: PsiLayout::new(report),
            hidden_pids: BTreeSet::new(),
            highlight: None,
            inspect: None,
            list_offset: egui::Vec2::ZERO,
            restore_list_offset: false,
        }
    }

    #[cfg(test)]
    fn new_for_test(input: PathBuf) -> Self {
        Self {
            input,
            range_offset: 0,
            range_end: 0,
            packet_size: 188,
            paging: Paging::default(),
            geometry: None,
            probe: None,
            page: 0,
            pages: BTreeMap::new(),
            recent: VecDeque::new(),
            selected: None,
            detail: None,
            pending: None,
            psi: PsiLayout::default(),
            hidden_pids: BTreeSet::new(),
            highlight: None,
            inspect: None,
            list_offset: egui::Vec2::ZERO,
            restore_list_offset: false,
        }
    }

    /// Packets whose first page and one decoded packet are already read, so a
    /// test can show the screen without running the CLI.
    #[cfg(test)]
    pub(crate) fn loaded_for_test(page: PacketsPage, offset: u64, detail: PacketDetail) -> Self {
        let mut pages = Self::new_for_test(PathBuf::from("fixture.ts"));
        pages.packet_size = u64::from(page.input.packet_size_bytes.unwrap_or(188));
        pages.range_end = page.next_offset_bytes.unwrap_or_default();
        pages.accept_first_page(page);
        pages.select(offset);
        pages.detail = Some((offset, Fetch::Ready(detail)));
        pages
    }

    /// The offset of the selected packet.
    #[cfg(test)]
    pub(crate) fn selected_for_test(&self) -> Option<u64> {
        self.selected
    }

    /// The packet the detailed inspection page shows, if it is open.
    #[cfg(test)]
    pub(crate) fn inspected_for_test(&self) -> Option<u64> {
        self.inspect.as_ref().map(|i| i.offset)
    }

    /// The list's scroll offset, and sets it as a scroll would.
    #[cfg(test)]
    pub(crate) fn list_offset_for_test(&mut self, set: Option<egui::Vec2>) -> egui::Vec2 {
        if let Some(offset) = set {
            self.list_offset = offset;
            self.restore_list_offset = true;
        }
        self.list_offset
    }

    /// Opens the detailed inspection of the selected packet with a line of
    /// its field list selected, as clicks do.
    #[cfg(test)]
    pub(crate) fn inspect_for_test(&mut self, selection: Option<wire_layout::Selection>) {
        self.open_inspection();
        if let Some(inspect) = &mut self.inspect {
            inspect.state = wire_layout::State::with(selection);
        }
    }

    /// Selects a field of the decoded packet, as a click on it does.
    #[cfg(all(test, feature = "screenshots"))]
    pub(crate) fn select_field_for_test(&mut self, name: &str) {
        let Some((offset, Fetch::Ready(detail))) = &self.detail else {
            panic!("no decoded packet");
        };
        let selection = detail
            .packet
            .as_ref()
            .and_then(|packet| field_selection(packet, name))
            .unwrap_or_else(|| panic!("no field {name}"));
        self.highlight = Some((*offset, selection));
    }

    /// Learns where the range's packets start from the first page and keeps
    /// that page. The first page is read with a full page count, so packets
    /// past the end of a short range are cut off.
    fn accept_first_page(&mut self, mut page: PacketsPage) {
        // An empty range has no first packet.
        let first = page.first_offset_bytes.unwrap_or(self.range_end);
        let geometry = Geometry::new(first, self.range_end, self.packet_size, self.paging);
        page.packets
            .truncate(geometry.count.min(self.paging.page_size()) as usize);
        self.geometry = Some(geometry);
        self.pages.insert(0, Fetch::Ready(page));
        self.recent.push_back(0);
        if let Some(offset) = self.pending.take() {
            self.go_to(offset);
        }
    }

    /// Shows the packet at `offset` (e.g. from Timeline's "Open packet") in
    /// the list.
    pub fn go_to(&mut self, offset: u64) {
        self.close_inspection();
        match self.geometry {
            Some(geometry) => {
                if let Some(index) = geometry.index_of(offset) {
                    self.page = geometry.page_of(index);
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

    /// Opens the detailed inspection page for the selected packet.
    fn open_inspection(&mut self) {
        if let Some(offset) = self.selected {
            self.inspect = Some(Inspect {
                offset,
                state: wire_layout::State::default(),
            });
        }
    }

    /// Back to the list, with the page, filter, selected row and scroll
    /// position it had. The decoded packet is read again when the page had
    /// moved to another one.
    fn close_inspection(&mut self) {
        if self.inspect.take().is_some() {
            self.restore_list_offset = true;
            if !matches!(&self.detail, Some((at, _)) if Some(*at) == self.selected) {
                self.detail = None;
            }
        }
    }

    /// The packet whose decoding is needed: the inspected one, else the
    /// selected one.
    fn focus(&self) -> Option<u64> {
        self.inspect.as_ref().map(|i| i.offset).or(self.selected)
    }

    /// The CLI arguments; the input path is passed as is, never through a
    /// lossy UTF-8 conversion.
    fn args(&self, output: &str, offset: u64) -> Vec<OsString> {
        vec![
            self.input.clone().into_os_string(),
            "--format".into(),
            "json".into(),
            "--output".into(),
            output.into(),
            "--offset".into(),
            offset.to_string().into(),
            "--packet-size".into(),
            self.packet_size.to_string().into(),
        ]
    }

    fn page_args(&self, offset: u64, count: u64) -> Vec<OsString> {
        let mut args = self.args("packets", offset);
        args.extend(["--count".into(), count.to_string().into()]);
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
                    let count = self.paging.page_size();
                    let args = self.page_args(self.range_offset, count);
                    let reply = Reply::packets_page(count);
                    self.probe = Some(start(cli, args, reply, repaint));
                }
                Some(probe) => {
                    probe.poll();
                    if !matches!(probe, Fetch::Ready(_)) {
                        return;
                    }
                    if let Some(Fetch::Ready(page)) = self.probe.take() {
                        self.accept_first_page(page);
                    }
                }
            }
            return;
        };

        if !self.pages.contains_key(&self.page) {
            let (offset, count) = geometry.page_request(self.page);
            let args = self.page_args(offset, count);
            let reply = Reply::packets_page(count);
            self.pages
                .insert(self.page, start(cli, args, reply, repaint.clone()));
        }
        self.keep_recent_pages();
        for page in self.pages.values_mut() {
            page.poll();
        }

        if let Some(offset) = self.focus() {
            match &mut self.detail {
                Some((at, detail)) if *at == offset => detail.poll(),
                _ => {
                    let mut args = self.args("packet", offset);
                    args.extend([
                        "--range-offset".into(),
                        self.range_offset.to_string().into(),
                        "--range-limit".into(),
                        (self.range_end - self.range_offset).to_string().into(),
                    ]);
                    let reply = Reply::packet_detail();
                    self.detail = Some((offset, start(cli, args, reply, repaint)));
                }
            }
        }
    }

    /// Keeps the current page and the most recently viewed ones, as many as
    /// the paging allows in all. Dropping a
    /// page still loading cancels its CLI run without waiting for it, so only
    /// the last requested page keeps running when paging quickly.
    fn keep_recent_pages(&mut self) {
        self.recent.retain(|&p| p != self.page);
        self.recent.push_back(self.page);
        while self.recent.len() > self.paging.cached_pages() {
            if let Some(old) = self.recent.pop_front() {
                self.pages.remove(&old);
            }
        }
    }

    pub fn show(&mut self, ui: &mut egui::Ui, cli: &Cli) {
        self.update(cli, ui.ctx());
        if self.inspect.is_some() {
            self.inspection_page(ui);
            return;
        }
        egui::Panel::right("packet_detail_live")
            .resizable(true)
            .default_size(480.0)
            .show(ui, |ui| {
                // Scrolls both ways: a packet wider than the pane scrolls
                // inside it instead of widening the pane over the list, so the
                // pane keeps the width it was dragged to.
                egui::ScrollArea::both()
                    .auto_shrink(false)
                    .show(ui, |ui| self.detail(ui));
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
            let start = geometry.page_start(self.page);
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
        let total = self.psi.pids().len();
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
                    self.hidden_pids = self.psi.pids().iter().map(|(pid, _)| pid.clone()).collect();
                }
            });
            ui.separator();
            for (pid, carries) in self.psi.pids() {
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
        let mut area = egui::ScrollArea::both()
            .id_salt("packet_list")
            .auto_shrink(false);
        if std::mem::take(&mut self.restore_list_offset) {
            area = area.scroll_offset(self.list_offset);
        }
        let output = area.show_rows(ui, ROW_HEIGHT, packets.len() + 1, |ui, visible| {
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
                let pid = packet.pid.as_deref().unwrap_or_default();
                let row = Row {
                    carries: if packet.pid.is_some() {
                        self.psi.label(pid, packet.offset_bytes)
                    } else {
                        String::new()
                    },
                    sections: self
                        .psi
                        .sections_text(pid, packet.offset_bytes, &packet.sections),
                    change: self.psi.change_at(packet.offset_bytes),
                };
                if packet_row(ui, width, packet, index, &row, selected).clicked() {
                    clicked = Some(packet.offset_bytes);
                }
            }
        });
        self.list_offset = output.state.clamped_offset();
        if let Some(offset) = clicked {
            self.select(offset);
        }
    }

    /// The full-width detailed inspection of one packet, with its complete
    /// wire layout; see `wire_layout`.
    fn inspection_page(&mut self, ui: &mut egui::Ui) {
        let Some(mut inspect) = self.inspect.take() else {
            return;
        };
        let offset = inspect.offset;
        let geometry = self.geometry;
        let index = geometry.and_then(|g| g.index_of(offset));
        let neighbor = |delta: i64| -> Option<u64> {
            let (g, i) = (geometry?, index? as i64 + delta);
            (i >= 0 && (i as u64) < g.count).then(|| g.first + i as u64 * g.packet_size)
        };
        let detail = match &self.detail {
            Some((at, Fetch::Ready(detail))) if *at == offset => Ok(detail),
            Some((at, Fetch::Failed(err))) if *at == offset => Err(Some(err.clone())),
            _ => Err(None),
        };
        let packet = detail.as_ref().ok().and_then(|d| d.packet.as_ref());
        let raw = packet.map(DecodedPacket::raw).unwrap_or_default();
        let model = packet.map(|p| Model::new(p, raw.len() * 8));
        let pid = packet.and_then(|p| {
            p.layers
                .iter()
                .find(|l| l.name == "ts_header")?
                .fields
                .iter()
                .find(|f| f.name == "PID")
                .map(|f| f.value.clone())
        });
        let carries = pid.as_ref().map(|pid| self.psi.label(pid, offset));
        let mut context = vec![format!("offset {}", range_bar::bytes(offset))];
        if let Some(pid) = &pid {
            context.push(format!("PID {}", field_value("PID", pid, 13)));
            if let Some(carries) = carries.as_ref().filter(|c| !c.is_empty()) {
                context.push(carries.clone());
            }
        }
        if !raw.is_empty() {
            context.push(format!("{}-byte packet", raw.len()));
        }
        if let Some(model) = &model {
            context.push(format!(
                "{} of {} bits classified",
                model.classified(),
                raw.len() * 8
            ));
        }
        let header = wire_layout::Header {
            title: match (index, geometry) {
                (Some(i), Some(g)) => format!("Packet #{} of {}", i + 1, g.count),
                _ => "Packet".to_owned(),
            },
            kind: packet.map(|p| {
                let unit = detail.as_ref().ok().and_then(|d| d.unit.as_ref());
                packet_kind(p, unit, carries.as_deref().unwrap_or_default())
            }),
            context: context.join(" · "),
            previous: neighbor(-1),
            next: neighbor(1),
        };

        let mut action = None;
        egui::Panel::top("wire_layout_header").show(ui, |ui| {
            crate::theme::compact(ui);
            ui.add_space(4.0);
            action = wire_layout::header(ui, &header);
            wire_layout::legend(ui);
            ui.add_space(2.0);
        });
        // ← and → step through the packets as Previous and Next do, unless a
        // text field has the keyboard.
        if action.is_none() && !ui.ctx().egui_wants_keyboard_input() {
            let (left, right) = ui.input(|i| {
                (
                    i.modifiers.is_none() && i.key_pressed(egui::Key::ArrowLeft),
                    i.modifiers.is_none() && i.key_pressed(egui::Key::ArrowRight),
                )
            });
            let to = if left {
                header.previous
            } else if right {
                header.next
            } else {
                None
            };
            action = to.map(wire_layout::Action::Go);
        }
        match (&detail, &model) {
            (Ok(_), Some(model)) => {
                let width = ui.available_width();
                egui::Panel::left("wire_layout_fields")
                    .resizable(true)
                    .default_size((width * 0.38).clamp(260.0, 520.0))
                    .size_range(220.0..=(width - 240.0).max(240.0))
                    .show(ui, |ui| {
                        crate::theme::compact(ui);
                        ui.horizontal(|ui| {
                            ui.strong("Fields");
                            ui.weak("decoded values and their encoded parts");
                        });
                        egui::ScrollArea::both()
                            .id_salt("wire_layout_fields")
                            .auto_shrink(false)
                            .show(ui, |ui| {
                                wire_layout::field_list(ui, model, &mut inspect.state)
                            });
                    });
                egui::CentralPanel::default().show(ui, |ui| {
                    crate::theme::compact(ui);
                    ui.horizontal(|ui| {
                        ui.strong("Bit inspector");
                        ui.weak("file order ↓ · each byte MSB → LSB");
                    });
                    if !model.complete {
                        ui.colored_label(
                            ui.visuals().warn_fg_color,
                            "This CLI does not report the wire layout (bit_layout); only the bits of decoded fields are classified.",
                        );
                    }
                    wire_layout::selection_card(ui, model, &inspect.state);
                    ui.add_space(4.0);
                    let per_row = wire_layout::bytes_per_row(ui.available_width() - 16.0);
                    wire_layout::grid_header(ui, per_row);
                    egui::ScrollArea::vertical()
                        .id_salt(("wire_layout_grid", offset))
                        .auto_shrink(false)
                        .show(ui, |ui| {
                            wire_layout::bit_grid(ui, &raw, per_row, model, &mut inspect.state)
                        });
                });
            }
            (Err(Some(err)), _) => {
                egui::CentralPanel::default().show(ui, |ui| {
                    ui.colored_label(ui.visuals().error_fg_color, err);
                });
            }
            (Ok(_), None) => {
                egui::CentralPanel::default().show(ui, |ui| {
                    ui.weak("The CLI returned no packet.");
                });
            }
            (Err(None), _) => {
                egui::CentralPanel::default().show(ui, |ui| {
                    ui.horizontal(|ui| {
                        ui.spinner();
                        ui.weak("Decoding the packet…");
                    });
                });
            }
        }
        self.inspect = Some(inspect);
        match action {
            Some(wire_layout::Action::Back) => self.close_inspection(),
            Some(wire_layout::Action::Go(to)) => {
                self.inspect = Some(Inspect {
                    offset: to,
                    state: wire_layout::State::default(),
                });
            }
            None => {}
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
            .map(|(_, selection)| selection);
        let index = self.geometry.and_then(|g| g.index_of(offset));

        ui.horizontal(|ui| {
            ui.heading(match index {
                Some(index) => format!("Packet #{}", index + 1),
                None => "Packet".to_owned(),
            });
            ui.weak(range_bar::bytes(offset));
        });
        let mut inspect = detailed_inspection_button(ui);
        let mut target = None;
        if let Some(unit) = &detail.unit {
            target = unit_card(ui, unit);
            ui.add_space(6.0);
        }

        ui.horizontal(|ui| {
            ui.strong("This packet");
            ui.weak("· click a field to inspect its bytes and bits");
        });
        for layer in &packet.layers {
            layer_fields(ui, layer, &mut highlight);
        }
        for warning in &detail.warnings {
            ui.colored_label(ui.visuals().warn_fg_color, &warning.message);
        }

        ui.add_space(8.0);
        ui.separator();
        inspectors(ui, packet, highlight.as_ref());
        // Again after the Bit inspector, where wanting more detail begins.
        ui.add_space(6.0);
        ui.separator();
        inspect |= detailed_inspection_button(ui);
        self.highlight = highlight.map(|selection| (offset, selection));

        if let Some(target) = target {
            self.go_to(target);
        } else if inspect {
            self.open_inspection();
        }
    }
}

/// Opens the detailed inspection page; shown above the decoded fields and
/// again below the Bit inspector. Text only, at the app's regular button size
/// (the pane around it is compact), with room above and below.
fn detailed_inspection_button(ui: &mut egui::Ui) -> bool {
    ui.add_space(6.0);
    let clicked = ui
        .scope(|ui| {
            ui.spacing_mut().button_padding = theme::BUTTON_PADDING;
            ui.spacing_mut().interact_size.y = theme::BUTTON_HEIGHT;
            ui.button(
                egui::RichText::new("Detailed inspection").text_style(egui::TextStyle::Button),
            )
            .on_hover_text("Open the complete wire layout of this packet on a full-width page")
            .clicked()
        })
        .inner;
    ui.add_space(6.0);
    clicked
}

/// The Byte and Bit inspectors of a decoded packet, with the selected field's
/// bytes and bits marked.
fn inspectors(ui: &mut egui::Ui, packet: &DecodedPacket, highlight: Option<&FieldSelection>) {
    byte_inspector(ui, packet, highlight);
    ui.add_space(8.0);
    ui.separator();
    bit_inspector(ui, &packet.raw(), highlight);
}

fn byte_inspector(ui: &mut egui::Ui, packet: &DecodedPacket, highlight: Option<&FieldSelection>) {
    let raw = packet.raw();
    let colors = byte_colors(raw.len(), &packet.layers);
    let bytes: Vec<(u8, egui::Color32)> = raw.iter().copied().zip(colors).collect();
    ui.horizontal(|ui| {
        ui.strong("Byte inspector");
        if let Some(selection) = highlight {
            ui.weak(format!("{} selected", byte_span_text(&selection.bytes)));
        }
    });
    hex_dump_bytes(ui, &bytes, highlight.map(|s| &s.bytes));
}

/// Which of the production inspectors a screenshot shows.
#[cfg(all(test, feature = "screenshots"))]
#[derive(Clone, Copy)]
pub(crate) enum Inspector {
    Byte,
    Bit,
}

/// One production inspector with the named field of a decoded packet
/// selected, for screenshots.
#[cfg(all(test, feature = "screenshots"))]
pub(crate) fn show_inspector_for_test(
    ui: &mut egui::Ui,
    detail: &PacketDetail,
    name: &str,
    inspector: Inspector,
) {
    let packet = detail.packet.as_ref().expect("a decoded packet");
    let selection = field_selection(packet, name).unwrap_or_else(|| panic!("no field {name}"));
    match inspector {
        Inspector::Byte => byte_inspector(ui, packet, Some(&selection)),
        Inspector::Bit => bit_inspector(ui, &packet.raw(), Some(&selection)),
    }
}

/// The selection a click on a field of the decoded packet makes.
#[cfg(all(test, feature = "screenshots"))]
fn field_selection(packet: &DecodedPacket, name: &str) -> Option<FieldSelection> {
    let field = packet
        .layers
        .iter()
        .flat_map(|layer| &layer.fields)
        .find(|field| field.name == name)?;
    Some(FieldSelection {
        key: field.name.clone(),
        value: field_text(field),
        bytes: span(field),
        bits: field.bit_ranges(),
    })
}

fn start<T: Send + 'static>(
    cli: &Cli,
    args: Vec<OsString>,
    reply: Reply<T>,
    repaint: impl Fn() + Send + 'static,
) -> Fetch<T> {
    match Request::start(cli, args, reply, repaint) {
        Ok(request) => Fetch::Loading(request),
        Err(err) => Fetch::Failed(err),
    }
}

/// What the PSI layout says about a packet.
struct Row<'a> {
    carries: String,
    sections: String,
    /// The PAT/PMT change the packet completed.
    change: Option<&'a str>,
}

/// One packet row; the whole row is clickable.
fn packet_row(
    ui: &mut egui::Ui,
    width: f32,
    packet: &PacketSummary,
    index: u64,
    row: &Row,
    selected: bool,
) -> egui::Response {
    let (rect, response) =
        ui.allocate_exact_size(egui::vec2(width, ROW_HEIGHT), egui::Sense::click());
    if !ui.is_rect_visible(rect) {
        return response;
    }
    paint_row_background(ui, rect, &response, selected, index % 2 == 1);
    if row.change.is_some() && !selected {
        ui.painter()
            .rect_filled(rect, 0, theme::PSI_COLOR.gamma_multiply(0.2));
    }
    let null = packet.pid.as_deref() == Some(NULL_PID);
    let cells = [
        ((index + 1).to_string(), COLUMNS[0].1),
        (format!("0x{:08X}", packet.offset_bytes), COLUMNS[1].1),
        (packet.pid.clone().unwrap_or_default(), COLUMNS[2].1),
        (row.carries.clone(), COLUMNS[3].1),
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
        (row.sections.clone(), COLUMNS[7].1),
    ];
    paint_cells(
        ui,
        rect,
        TEXT_INSET,
        &cells,
        row_text_color(ui, selected),
        false,
    );
    let font = egui::TextStyle::Monospace.resolve(ui.style());
    let mut notes = egui::text::LayoutJob::default();
    if let Some(change) = row.change {
        notes.append(
            change,
            0.0,
            egui::TextFormat::simple(font.clone(), theme::PSI_COLOR),
        );
    }
    let problems = packet_notes(packet).join(" · ");
    if !problems.is_empty() {
        let gap = if notes.is_empty() { 0.0 } else { 12.0 };
        notes.append(
            &problems,
            gap,
            egui::TextFormat::simple(font, theme::LIMIT_COLOR),
        );
    }
    let notes_x = rect.left() + TEXT_INSET + COLUMNS[..8].iter().map(|(_, w, _)| w).sum::<f32>();
    let galley = ui.fonts_mut(|fonts| fonts.layout_job(notes));
    ui.painter().galley(
        egui::pos2(notes_x, rect.center().y - galley.size().y / 2.0),
        galley,
        theme::LIMIT_COLOR,
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
        "trailer" => ByteLayer::Trailer,
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
        // The 16 bytes after the TS packet of a 204-byte packet; often
        // Reed-Solomon parity, but nothing here checks that.
        "trailer" => "Trailer (after the TS packet; not decoded)",
        other => other,
    };
    if layer.truncated {
        format!("{title} (continues in the next packet)")
    } else {
        title.to_owned()
    }
}

fn layer_fields(ui: &mut egui::Ui, layer: &PacketLayer, highlight: &mut Option<FieldSelection>) {
    let fields: Vec<Field> = layer
        .fields
        .iter()
        .map(|f| Field::new(f.name.clone(), field_text(f), span(f)).with_bits(f.bit_ranges()))
        .collect();
    let color = layer_color(byte_layer(&layer.name));
    field_tree(ui, &layer_title(layer), color, &fields, highlight);
    if let Some(first) = layer.rows.first() {
        let titles: Vec<&str> = first.iter().map(|f| f.name.as_str()).collect();
        let rows: Vec<(Vec<String>, FieldSelection)> = layer
            .rows
            .iter()
            .enumerate()
            .filter_map(|(index, row)| {
                let selection = row_selection(&layer.name, index, row)?;
                Some((row.iter().map(field_text).collect(), selection))
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

/// A table row (e.g. one PMT stream) as one selection: the bytes from its
/// first to its last field, and the bits of every field in the row.
fn row_selection(
    layer: &str,
    index: usize,
    row: &[crate::report::PacketField],
) -> Option<FieldSelection> {
    let start = row.iter().map(|f| f.byte_offset).min()?;
    let end = row.iter().map(|f| f.byte_offset + f.byte_length).max()?;
    let mut bits: Vec<std::ops::Range<usize>> = row
        .iter()
        .flat_map(|f| {
            let ranges = f.bit_ranges();
            if ranges.is_empty() {
                std::iter::once(f.byte_offset * 8..(f.byte_offset + f.byte_length) * 8).collect()
            } else {
                ranges
            }
        })
        .collect();
    bits.sort_by_key(|r| r.start);
    Some(FieldSelection {
        key: format!("{layer} row {index}"),
        value: row.iter().map(field_text).collect::<Vec<_>>().join(" · "),
        bytes: start..end,
        bits,
    })
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
// Bit ranges are lists of ranges; a list of one is the common case here.
#[allow(clippy::single_range_in_vec_init)]
mod tests {
    use super::*;

    #[test]
    fn pages_cover_every_packet_of_the_range_once() {
        // A range of 1,079 packets starting at a packet boundary 4 bytes in.
        let geometry = Geometry::new(4, 4 + 1079 * 192 + 100, 192, Paging::default());
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
        assert_eq!(Geometry::new(0, 100, 188, Paging::default()).pages(), 1);
    }

    /// The same range with a non-default page size from Settings.
    #[test]
    fn page_geometry_follows_the_page_size() {
        let geometry = Geometry::new(4, 4 + 1079 * 192 + 100, 192, Paging::new(250, 5));
        assert_eq!(geometry.pages(), 5);
        let mut next = geometry.first;
        let mut seen = 0;
        for page in 0..geometry.pages() {
            let (offset, count) = geometry.page_request(page);
            assert_eq!(offset, next);
            assert_eq!(geometry.page_start(page), seen);
            next += count * 192;
            seen += count;
        }
        assert_eq!(seen, geometry.count);
        assert_eq!(geometry.page_request(4), (4 + 1000 * 192, 79));
        assert_eq!(geometry.page_of(249), 0);
        assert_eq!(geometry.page_of(250), 1);
        assert_eq!(geometry.page_of(1078), 4);
        // Exactly one page's worth of packets is one page.
        assert_eq!(
            Geometry::new(0, 10 * 188, 188, Paging::new(10, 1)).pages(),
            1
        );
        assert_eq!(
            Geometry::new(0, 11 * 188, 188, Paging::new(10, 1)).pages(),
            2
        );
    }

    /// Settings limits are applied once, when the paging is made.
    #[test]
    fn paging_is_clamped_to_the_settings_ranges() {
        assert_eq!(Paging::default(), Paging::new(100, 5));
        let low = Paging::new(0, 0);
        assert_eq!((low.page_size(), low.cached_pages()), (10, 1));
        let high = Paging::new(u64::MAX, u32::MAX);
        assert_eq!((high.page_size(), high.cached_pages()), (10_000, 50));
        let set = Paging::new(37, 3);
        assert_eq!((set.page_size(), set.cached_pages()), (37, 3));
    }

    /// With 2 cached pages, the shown page and the last one viewed stay.
    #[test]
    fn cache_keeps_as_many_pages_as_the_paging_allows() {
        let mut pages = PacketPages {
            paging: Paging::new(100, 2),
            ..PacketPages::new_for_test(PathBuf::from("x"))
        };
        for page in [0, 1, 2, 1, 3] {
            pages.page = page;
            pages.pages.insert(page, Fetch::Failed(String::new()));
            pages.keep_recent_pages();
        }
        assert_eq!(pages.recent, [1, 3]);
        assert_eq!(pages.pages.keys().copied().collect::<Vec<_>>(), [1, 3]);

        // One cached page keeps only the page shown.
        pages.paging = Paging::new(100, 1);
        pages.page = 4;
        pages.pages.insert(4, Fetch::Failed(String::new()));
        pages.keep_recent_pages();
        assert_eq!(pages.recent, [4]);
        assert_eq!(pages.pages.keys().copied().collect::<Vec<_>>(), [4]);
    }

    /// A packet chosen elsewhere (Timeline) opens on its page for the page size in use.
    #[test]
    fn go_to_uses_the_page_size() {
        let mut pages = PacketPages {
            paging: Paging::new(25, 5),
            range_end: 188 * 100,
            ..PacketPages::new_for_test(PathBuf::from("x"))
        };
        pages.geometry = Some(Geometry::new(0, 188 * 100, 188, pages.paging));
        pages.go_to(188 * 60);
        assert_eq!((pages.page, pages.selected), (2, Some(188 * 60)));
    }

    #[test]
    fn labels_pids_from_the_analysis() {
        let report = crate::report::parse_analysis(
            include_bytes!("../../tests/fixtures/analysis_two_programs_188.json"),
            "2",
        )
        .unwrap();
        // Without psi_layout, labels come from the tables the analysis started with.
        assert!(report.psi_layout.is_none());
        let psi = PsiLayout::new(&report);
        assert_eq!(psi.label("0x0000", 0), "PAT");
        assert_eq!(psi.label("0x1000", 0), "PMT (program 1)");
        assert_eq!(psi.label("0x0100", 0), "MPEG-2 video");
        assert_eq!(psi.label("0x0101", 0), "MPEG-1 audio");
        assert_eq!(psi.label(NULL_PID, 0), "null");
        let order: Vec<&str> = psi.pids().iter().map(|(_, label)| label.as_str()).collect();
        assert_eq!(order.first(), Some(&"PAT"));
        assert_eq!(order.last(), Some(&"null"));
    }

    #[test]
    fn colors_bytes_by_layer() {
        let field = |offset, length| crate::report::PacketField {
            name: "f".into(),
            value: "v".into(),
            byte_offset: offset,
            byte_length: length,
            bits: Vec::new(),
        };
        let layer = |name: &str, fields| PacketLayer {
            name: name.into(),
            fields,
            rows: Vec::new(),
            truncated: false,
            bit_layout: Vec::new(),
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

        // A 204-byte packet: the trailer after the TS packet has its own color.
        let layers = [
            layer("ts_header", vec![field(0, 4)]),
            layer("payload", vec![field(4, 184)]),
            layer("trailer", vec![field(188, 16)]),
        ];
        let colors = byte_colors(204, &layers);
        assert_eq!(colors[187], layer_color(ByteLayer::Payload));
        assert!((188..204).all(|i| colors[i] == layer_color(ByteLayer::Trailer)));
        assert_ne!(
            layer_color(ByteLayer::Trailer),
            layer_color(ByteLayer::Payload)
        );
        assert_eq!(
            layer_title(&layers[2]),
            "Trailer (after the TS packet; not decoded)"
        );
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
            ("diagnostics/sample_204byte_all_findings.ts", 204),
        ] {
            let input = samples.join(name);
            let size = std::fs::metadata(&input).unwrap().len();
            let mut pages = PacketPages {
                packet_size,
                range_end: size,
                ..PacketPages::new_for_test(input.clone())
            };
            // Every packet once, at the default and at a non-default page size.
            let mut first = None;
            for paging in [Paging::default(), Paging::new(37, 3)] {
                let page = wait(
                    Request::start(
                        &cli,
                        pages.page_args(0, paging.page_size()),
                        Reply::packets_page(paging.page_size()),
                        || {},
                    )
                    .unwrap(),
                )
                .unwrap();
                let geometry =
                    Geometry::new(page.first_offset_bytes.unwrap(), size, packet_size, paging);
                assert_eq!(geometry.count, page.total_packets, "{name}");
                let mut expected = geometry.first;
                for page in 0..geometry.pages() {
                    let (offset, count) = geometry.page_request(page);
                    let result = wait(
                        Request::start(
                            &cli,
                            pages.page_args(offset, count),
                            Reply::packets_page(count),
                            || {},
                        )
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
                first.get_or_insert((page, geometry));
            }
            let (first, geometry) = first.unwrap();

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
            let detail =
                wait(Request::start(&cli, args, Reply::packet_detail(), || {}).unwrap()).unwrap();
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
            // Only the 188 TS packet bytes are decoded; a 204-byte packet's
            // trailer follows them as a layer of its own.
            let ts = if packet_size == 192 { 4 } else { 0 };
            for layer in packet.layers.iter().filter(|l| l.name != "trailer") {
                for field in layer.fields.iter().chain(layer.rows.iter().flatten()) {
                    assert!(
                        field.byte_offset + field.byte_length <= ts + 188,
                        "{field:?}"
                    );
                }
            }
            let trailer = packet.layers.iter().find(|l| l.name == "trailer");
            if packet_size == 204 {
                let field = &trailer.expect("a trailer layer").fields[0];
                assert_eq!((field.byte_offset, field.byte_length), (188, 16));
            } else {
                assert!(trailer.is_none(), "{name}");
            }
            let header = packet
                .layers
                .iter()
                .find(|l| l.name == "ts_header")
                .unwrap();
            let pid = header.fields.iter().find(|f| f.name == "PID").unwrap();
            let raw = packet.raw();
            let decoded = format!(
                "0x{:04x}",
                (u16::from(raw[ts + 1] & 0x1f) << 8) | u16::from(raw[ts + 2])
            );
            assert_eq!(pid.value.to_lowercase(), decoded);
        }
    }

    /// A Unix file name need not be UTF-8: the packet page and detail
    /// requests give the CLI the same name the analysis opened.
    #[cfg(unix)]
    #[test]
    fn page_and_detail_requests_keep_a_non_utf8_input_path() {
        use std::os::unix::ffi::{OsStrExt, OsStringExt};
        let dir =
            std::env::temp_dir().join(format!("mtsa-gui-test-non-utf8-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let input = dir.join(std::ffi::OsString::from_vec(b"caf\xe9.ts".to_vec()));
        assert!(input.to_str().is_none());
        let pages = PacketPages::new_for_test(input.clone());
        for (output, fixture) in [
            ("packets", "packets_page_188.json"),
            ("packet", "packet_detail_pat_188.json"),
        ] {
            let document = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
                .join("tests/fixtures")
                .join(fixture);
            let recorded = dir.join(format!("{output}-arg"));
            let cli = cli::fake_cli(
                &dir,
                &format!(
                    "printf '%s' \"$1\" > '{}'\ncat '{}'",
                    recorded.display(),
                    document.display()
                ),
            );
            if output == "packets" {
                let reply = Reply::packets_page(Paging::default().page_size());
                let request = Request::start(
                    &cli,
                    pages.page_args(0, Paging::default().page_size()),
                    reply,
                    || {},
                );
                wait(request.unwrap()).expect("a page");
            } else {
                let reply = Reply::packet_detail();
                let request = Request::start(&cli, pages.args("packet", 0), reply, || {});
                wait(request.unwrap()).expect("a detail");
            }
            assert_eq!(
                std::fs::read(&recorded).unwrap(),
                input.as_os_str().as_bytes(),
                "{output}"
            );
        }
    }

    /// Against the real CLI (MPEG_TS_ANALYZER_CLI, e.g. via `make gui-test`):
    /// a sample under a non-UTF-8 name is analyzed, paged and decoded. Skipped
    /// where the file system refuses such names (macOS APFS does).
    #[cfg(unix)]
    #[test]
    fn analyzes_and_pages_a_file_with_a_non_utf8_name() {
        use std::os::unix::ffi::OsStringExt;
        let Some(path) = std::env::var_os("MPEG_TS_ANALYZER_CLI") else {
            return;
        };
        let cli = cli::check(std::path::Path::new(&path)).expect("capabilities");
        let dir =
            std::env::temp_dir().join(format!("mtsa-gui-test-non-utf8-cli-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let input = dir.join(std::ffi::OsString::from_vec(b"caf\xe9.ts".to_vec()));
        let sample = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../sample_data/sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts");
        if std::fs::copy(&sample, &input).is_err() {
            return;
        }
        let size = std::fs::metadata(&input).unwrap().len();
        let mut job = cli::AnalysisJob::start(&cli, &input, 0, size, || {}).expect("start");
        let deadline = std::time::Instant::now() + std::time::Duration::from_secs(60);
        let report = loop {
            match job.poll() {
                cli::JobUpdate::Finished(result) => break result.expect("report"),
                cli::JobUpdate::Running(_) => {
                    assert!(std::time::Instant::now() < deadline, "analysis timed out");
                    std::thread::sleep(std::time::Duration::from_millis(20));
                }
            }
        };
        assert_eq!(report.status, "complete");
        assert_eq!(report.programs.len(), 2);
        let pages = PacketPages {
            range_end: size,
            ..PacketPages::new_for_test(input.clone())
        };
        let page = wait(
            Request::start(
                &cli,
                pages.page_args(0, Paging::default().page_size()),
                Reply::packets_page(Paging::default().page_size()),
                || {},
            )
            .unwrap(),
        )
        .expect("a page");
        let offset = page.packets[0].offset_bytes;
        let detail = wait(
            Request::start(
                &cli,
                pages.args("packet", offset),
                Reply::packet_detail(),
                || {},
            )
            .unwrap(),
        )
        .expect("a detail");
        assert!(detail.packet.is_some());
    }

    /// A page that drops out of the cache while its CLI run is still going
    /// is cancelled without waiting, even when the CLI closed its output.
    #[test]
    fn evicting_a_loading_page_stops_its_cli() {
        let marker = std::env::temp_dir()
            .join(format!("mtsa-gui-test-evict-{}", std::process::id()))
            .join("closed");
        std::fs::create_dir_all(marker.parent().unwrap()).unwrap();
        let request = Request::spawn(
            cli::closes_output_and_hangs(&marker),
            Reply::packets_page(Paging::default().page_size()),
            || {},
        )
        .unwrap();
        let child = request.child();
        cli::wait_for_closed_output(&marker);
        // A non-default cache of 3 pages, full: the loading page 0 is the oldest.
        let mut pages = PacketPages {
            paging: Paging::new(100, 3),
            ..PacketPages::new_for_test(PathBuf::from("x"))
        };
        pages.pages.insert(0, Fetch::Loading(request));
        for page in 1..3 {
            pages.pages.insert(page, Fetch::Failed(String::new()));
        }
        pages.recent = (0..3).collect();
        pages.page = 3;
        let pages = std::sync::Arc::new(std::sync::Mutex::new(pages));
        let paging = std::sync::Arc::clone(&pages);
        assert!(
            cli::returns_promptly(move || paging.lock().unwrap().keep_recent_pages()),
            "evicting a loading page waited for its CLI"
        );
        let pages = pages.lock().unwrap();
        assert!(!pages.pages.contains_key(&0), "page 0 is still cached");
        assert_eq!(pages.recent, [1, 2, 3]);
        assert!(
            cli::wait_until_exited(&child),
            "the evicted page's CLI kept running"
        );
    }

    /// A range of three packets, read as a full page: only those three stay,
    /// and a packet requested before the page arrived is selected.
    #[test]
    fn trims_the_first_page_to_a_short_range() {
        let summary = |offset_bytes| PacketSummary {
            offset_bytes,
            valid: true,
            pid: Some("0x0100".into()),
            pusi: false,
            transport_error: false,
            scrambling_control: 0,
            adaptation_field_control: 1,
            continuity_counter: 0,
            pcr_ms: None,
            sections: Vec::new(),
        };
        let page: PacketsPage = serde_json::from_str(
            r#"{"schema_version":"1.0","output":"packets","input":{"path":"x","file_size_bytes":100000,"packet_size_bytes":188,"offset_bytes":100},"status":"complete","first_offset_bytes":188,"end_of_file":false,"total_packets":531,"packets":[],"warnings":[]}"#,
        )
        .unwrap();
        let page = PacketsPage {
            packets: (0..100).map(|i| summary(188 + i * 188)).collect(),
            ..page
        };
        let mut pages = PacketPages {
            range_offset: 100,
            range_end: 100 + 3 * 188 + 150,
            ..PacketPages::new_for_test(PathBuf::from("x"))
        };
        pages.go_to(376);
        pages.accept_first_page(page);
        let geometry = pages.geometry.unwrap();
        assert_eq!((geometry.first, geometry.count), (188, 3));
        let Some(Fetch::Ready(first)) = pages.pages.get(&0) else {
            panic!("no first page");
        };
        assert_eq!(
            first
                .packets
                .iter()
                .map(|p| p.offset_bytes)
                .collect::<Vec<_>>(),
            [188, 376, 564]
        );
        assert_eq!(pages.selected, Some(376));
    }

    /// Runs only against a CLI named by MPEG_TS_ANALYZER_CLI (e.g. via `make gui-test`).
    #[test]
    fn bit_ranges_match_the_raw_bytes() {
        let Some(path) = std::env::var_os("MPEG_TS_ANALYZER_CLI") else {
            return;
        };
        let cli = cli::check(std::path::Path::new(&path)).expect("capabilities");
        let samples = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../sample_data");
        for (name, packet_size) in [
            (
                "sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts",
                188u64,
            ),
            (
                "sample_192byte_two_programs_mpeg2_160x120_25fps_mp2.ts",
                192,
            ),
            ("diagnostics/sample_204byte_all_findings.ts", 204),
        ] {
            let pages = PacketPages {
                packet_size,
                ..PacketPages::new_for_test(samples.join(name))
            };
            let decode = |offset: u64| {
                wait(
                    Request::start(
                        &cli,
                        pages.args("packet", offset),
                        Reply::packet_detail(),
                        || {},
                    )
                    .unwrap(),
                )
                .unwrap()
                .packet
                .unwrap()
            };
            let find = |packet: &crate::report::DecodedPacket, name: &str| {
                packet
                    .layers
                    .iter()
                    .flat_map(|l| l.fields.iter())
                    .find(|f| f.name == name)
                    .cloned()
                    .unwrap_or_else(|| panic!("no {name}"))
            };
            let value_of = |raw: &[u8], ranges: &[std::ops::Range<usize>]| {
                ranges
                    .iter()
                    .flat_map(|r| r.clone())
                    .fold(0u64, |acc, bit| {
                        acc << 1 | u64::from(raw[bit / 8] >> (7 - bit % 8) & 1)
                    })
            };
            let shift = if packet_size == 192 { 4 * 8 } else { 0 };

            // The PAT is the second packet of the sample.
            let pat = decode(packet_size);
            let raw = pat.raw();
            let pid = find(&pat, "PID");
            let selection = FieldSelection {
                bits: pid.bit_ranges(),
                ..FieldSelection::of_bytes("PID", span(&pid))
            };
            let ranges = selection.bit_ranges();
            assert_eq!(ranges, [11 + shift..24 + shift], "{name}");
            assert_eq!(
                super::super::packets::bit_rows(&ranges, raw.len()).0.len(),
                2
            );
            assert_eq!(value_of(&raw, &ranges), 0);
            let pusi = find(&pat, "payload_unit_start_indicator").bit_ranges();
            assert_eq!(pusi.iter().map(|r| r.len()).sum::<usize>(), 1);
            assert_eq!(value_of(&raw, &pusi), 1);

            // version_number and current_next_indicator share a byte but not bits.
            let version = find(&pat, "version_number");
            let current = find(&pat, "current_next_indicator");
            assert_eq!(span(&version), span(&current));
            let (version_bits, current_bits) = (version.bit_ranges(), current.bit_ranges());
            assert!(version_bits.iter().all(|v| {
                current_bits
                    .iter()
                    .all(|c| v.end <= c.start || c.end <= v.start)
            }));
            assert_eq!(value_of(&raw, &version_bits).to_string(), version.value);
            assert_eq!(value_of(&raw, &current_bits).to_string(), current.value);

            // The wire layout classifies every bit of the packet, and the
            // binary values of fixed and reserved runs are the raw bits.
            for packet in [&pat, &decode(packet_size * 12)] {
                let raw = packet.raw();
                let model = Model::new(packet, raw.len() * 8);
                assert!(model.complete, "{name}");
                assert_eq!(model.classified(), raw.len() * 8, "{name}");
                for part in packet.layers.iter().flat_map(|l| &l.bit_layout) {
                    if matches!(part.kind.as_str(), "reserved" | "marker") {
                        let bits: String = part
                            .bit_ranges()
                            .iter()
                            .flat_map(|r| r.clone())
                            .map(|bit| {
                                if raw[bit / 8] >> (7 - bit % 8) & 1 == 1 {
                                    '1'
                                } else {
                                    '0'
                                }
                            })
                            .collect();
                        assert_eq!(bits, part.value, "{name} {}", part.id);
                    }
                }
            }
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
            sections: Vec::new(),
        };
        assert_eq!(packet_notes(&packet), ["transport error", "scrambled"]);
    }
}
