//! The detailed inspection of one packet: a full-width page inside Packets
//! with the packet's complete wire layout. The left column lists every
//! structure, decoded field and encoded part (reserved, marker and prefix
//! bits, stuffing, undecoded bytes) in file order; the right column shows
//! every bit of the packet colored by its role, with the selection marked.
//!
//! The layout comes from the `bit_layout` of the packet detail. A CLI that
//! does not report it gets the same page with only the decoded fields'
//! bits classified, as the compact Bit inspector shows them.

use std::ops::Range;

use eframe::egui;

use super::packets::{ByteLayer, layer_color};
use crate::icons;
use crate::presentation::packet::{field_text, field_value};
use crate::report::{DecodedPacket, LayoutPart, PacketField, PacketLayer};

/// What a run of bits is on the wire (`kind` in the packet detail).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    Value,
    Flag,
    Reserved,
    Marker,
    Prefix,
    Stuffing,
    Payload,
    Unknown,
}

impl Kind {
    /// An unrecognized kind from a newer CLI is shown as unknown.
    pub fn parse(kind: &str) -> Self {
        match kind {
            "value" => Kind::Value,
            "flag" => Kind::Flag,
            "reserved" => Kind::Reserved,
            "marker" => Kind::Marker,
            "prefix" => Kind::Prefix,
            "stuffing" => Kind::Stuffing,
            "payload" => Kind::Payload,
            _ => Kind::Unknown,
        }
    }

    pub fn name(self) -> &'static str {
        match self {
            Kind::Value => "value",
            Kind::Flag => "flag",
            Kind::Reserved => "reserved",
            Kind::Marker => "marker",
            Kind::Prefix => "prefix",
            Kind::Stuffing => "stuffing",
            Kind::Payload => "payload",
            Kind::Unknown => "not decoded",
        }
    }
}

/// The colors of the roles, shared by the swatches, the legend and the grid:
/// one per role rather than per field, so the same kind of bits looks the
/// same everywhere. Bits whose fixed value is wrong are red whatever their role.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Role {
    Value,
    Reserved,
    Fixed,
    Stuffing,
    Payload,
    Unknown,
    Mismatch,
}

impl Role {
    pub const LEGEND: [(Role, &'static str); 7] = [
        (Role::Value, "value / flag"),
        (Role::Reserved, "reserved"),
        (Role::Fixed, "marker / prefix"),
        (Role::Stuffing, "stuffing"),
        (Role::Payload, "payload"),
        (Role::Unknown, "not decoded"),
        (Role::Mismatch, "fixed-bit mismatch"),
    ];

    pub fn of(kind: Kind, valid: Option<bool>) -> Self {
        if valid == Some(false) {
            return Role::Mismatch;
        }
        match kind {
            Kind::Value | Kind::Flag => Role::Value,
            Kind::Reserved => Role::Reserved,
            Kind::Marker | Kind::Prefix => Role::Fixed,
            Kind::Stuffing => Role::Stuffing,
            Kind::Payload => Role::Payload,
            Kind::Unknown => Role::Unknown,
        }
    }

    /// The fill of the role; "not decoded" has none and is striped instead.
    pub fn color(self, visuals: &egui::Visuals) -> egui::Color32 {
        match self {
            Role::Value => visuals.selection.bg_fill,
            Role::Reserved => egui::Color32::from_rgb(0x84, 0x88, 0x8e),
            Role::Fixed => egui::Color32::from_rgb(0x9b, 0x6b, 0xd6),
            Role::Stuffing => egui::Color32::from_rgb(0xb8, 0x9b, 0x3e),
            Role::Payload => egui::Color32::from_rgb(0x4f, 0x8a, 0x7a),
            Role::Unknown => egui::Color32::TRANSPARENT,
            Role::Mismatch => egui::Color32::from_rgb(0xd9, 0x3f, 0x3f),
        }
    }
}

/// One classified run of the packet.
#[derive(Clone, Debug)]
pub struct Part {
    pub id: String,
    pub label: String,
    pub kind: Kind,
    pub ranges: Vec<Range<usize>>,
    pub value: String,
    pub expected: Option<String>,
    pub valid: Option<bool>,
    pub rule: Option<String>,
    /// Where the part is: layer, table entry and field.
    pub context: String,
}

impl Part {
    pub fn role(&self) -> Role {
        Role::of(self.kind, self.valid)
    }

    fn from_layout(layer: &PacketLayer, part: &LayoutPart) -> Self {
        Part {
            id: part.id.clone(),
            label: part.label.clone(),
            kind: Kind::parse(&part.kind),
            ranges: part.bit_ranges(),
            value: layout_value(part),
            expected: part.expected.clone(),
            valid: part.valid,
            rule: part.rule.clone(),
            context: context(layer, part.row, part.field.as_deref()),
        }
    }

    /// A decoded field as a part, for a CLI that reports no layout.
    fn from_field(layer: &PacketLayer, row: Option<usize>, field: &PacketField) -> Self {
        let mut ranges = field.bit_ranges();
        if ranges.is_empty() {
            ranges.push(field.byte_offset * 8..(field.byte_offset + field.byte_length) * 8);
        }
        let row_id = row.map_or(String::new(), |r| format!("row[{r}]."));
        Part {
            id: format!("{}.{row_id}{}@{}", layer.name, field.name, ranges[0].start),
            label: field.name.clone(),
            kind: Kind::Value,
            ranges,
            value: field_text(field),
            expected: None,
            valid: None,
            rule: None,
            context: context(layer, row, None),
        }
    }
}

/// The value of a layout part for display (see [`field_value`]). Fixed bits
/// (reserved, marker, prefix, stuffing) stay as written, the notation their
/// expected value uses.
fn layout_value(part: &LayoutPart) -> String {
    match Kind::parse(&part.kind) {
        Kind::Value | Kind::Flag => {
            let bits = part.bits.iter().map(|b| b.bit_length).sum();
            field_value(&part.label, &part.value, bits)
        }
        _ => part.value.clone(),
    }
}

/// "pmt › stream 2 › elementary_PID".
fn context(layer: &PacketLayer, row: Option<usize>, field: Option<&str>) -> String {
    let mut path = vec![layer_title(&layer.name).to_owned()];
    if let Some(row) = row {
        path.push(format!("{} {}", row_noun(&layer.name), row + 1));
    }
    if let Some(field) = field {
        path.push(field.to_owned());
    }
    path.join(" › ")
}

fn row_noun(layer: &str) -> &'static str {
    match layer {
        "pat" => "program",
        "pmt" => "stream",
        _ => "entry",
    }
}

pub fn layer_title(name: &str) -> &str {
    match name {
        "m2ts_header" => "M2TS header",
        "ts_header" => "TS header",
        "adaptation_field" => "Adaptation field",
        "payload" => "Payload",
        "pes_header" => "PES header",
        "pat" => "PAT",
        "pmt" => "PMT",
        "psi_section" => "PSI section",
        "trailer" => "Trailer",
        other => other,
    }
}

/// Parts in wire order with the field each belongs to.
type Members = Vec<(Option<String>, usize)>;

/// A line of the field list: a part, a decoded field made of several parts
/// (a PTS, a PCR), or a table entry.
#[derive(Clone, Debug, PartialEq)]
pub enum Node {
    Part(usize),
    Group {
        key: String,
        label: String,
        value: String,
        children: Vec<Node>,
    },
}

pub struct LayerTree {
    pub name: String,
    pub nodes: Vec<Node>,
    /// The layer whose bytes this one decodes: the payload, for a PES header
    /// or a PSI section. The field list draws it beneath that layer.
    pub parent: Option<usize>,
}

/// The layers that decode another layer's bytes, by position: a layer after
/// the payload (a PES header or a PSI section such as a PAT or PMT) decodes
/// that payload. The framing layers (M2TS header, TS header, adaptation
/// field, payload and the trailer of a 204-byte packet) partition the packet
/// and have no parent.
pub fn layer_parents<'a>(names: impl IntoIterator<Item = &'a str>) -> Vec<Option<usize>> {
    let mut payload = None;
    names
        .into_iter()
        .enumerate()
        .map(|(index, name)| match name {
            "payload" => {
                payload = Some(index);
                None
            }
            "m2ts_header" | "ts_header" | "adaptation_field" | "trailer" => {
                payload = None;
                None
            }
            _ => payload,
        })
        .collect()
}

/// The selected line: one part, or a field or entry with all its parts.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Selection {
    Part(String),
    Group(String),
}

/// The classified packet.
pub struct Model {
    pub parts: Vec<Part>,
    pub layers: Vec<LayerTree>,
    /// The part of each packet bit; PES headers and sections over the
    /// payload they sit in.
    pub owners: Vec<Option<usize>>,
    /// Whether the CLI reported the layout, rather than only field bits.
    pub complete: bool,
}

impl Model {
    pub fn new(packet: &DecodedPacket, bits: usize) -> Self {
        let complete = packet.layers.iter().any(|l| !l.bit_layout.is_empty());
        let mut model = Model {
            parts: Vec::new(),
            layers: Vec::new(),
            owners: vec![None; bits],
            complete,
        };
        let parents = layer_parents(packet.layers.iter().map(|l| l.name.as_str()));
        for (layer, parent) in packet.layers.iter().zip(parents) {
            let first = model.parts.len();
            let nodes = if complete {
                model.layout_nodes(layer)
            } else {
                model.field_nodes(layer)
            };
            // "Payload › PMT › …": where the part is, as the list nests it.
            if let Some(parent) = parent {
                let title = layer_title(&packet.layers[parent].name);
                for part in &mut model.parts[first..] {
                    part.context = format!("{title} › {}", part.context);
                }
            }
            model.layers.push(LayerTree {
                name: layer.name.clone(),
                nodes,
                parent,
            });
        }
        for (index, part) in model.parts.iter().enumerate() {
            for bit in part.ranges.iter().flat_map(|r| r.clone()) {
                if let Some(owner) = model.owners.get_mut(bit) {
                    *owner = Some(index);
                }
            }
        }
        model
    }

    fn push(&mut self, part: Part) -> usize {
        self.parts.push(part);
        self.parts.len() - 1
    }

    /// The layout of a layer, its parts grouped by table entry and then by
    /// the field they belong to.
    fn layout_nodes(&mut self, layer: &PacketLayer) -> Vec<Node> {
        let mut nodes = Vec::new();
        let mut entry: Option<(usize, Members)> = None;
        let mut loose: Members = Vec::new();
        for part in &layer.bit_layout {
            let index = self.push(Part::from_layout(layer, part));
            match (part.row, &mut entry) {
                (Some(row), Some((current, members))) if *current == row => {
                    members.push((part.field.clone(), index));
                }
                (Some(row), _) => {
                    nodes.extend(self.by_field(layer, None, std::mem::take(&mut loose)));
                    if let Some((current, members)) = entry.take() {
                        nodes.push(self.entry_node(layer, current, members));
                    }
                    entry = Some((row, vec![(part.field.clone(), index)]));
                }
                (None, _) => {
                    if let Some((current, members)) = entry.take() {
                        nodes.push(self.entry_node(layer, current, members));
                    }
                    loose.push((part.field.clone(), index));
                }
            }
        }
        nodes.extend(self.by_field(layer, None, loose));
        if let Some((current, members)) = entry {
            nodes.push(self.entry_node(layer, current, members));
        }
        nodes
    }

    fn entry_node(&self, layer: &PacketLayer, row: usize, members: Members) -> Node {
        let values: Vec<String> = layer
            .rows
            .get(row)
            .map(|fields| fields.iter().map(field_text).collect())
            .unwrap_or_default();
        Node::Group {
            key: format!("{}/{row}", layer.name),
            label: format!("{} {}", row_noun(&layer.name), row + 1),
            value: values.join(" · "),
            children: self.by_field(layer, Some(row), members),
        }
    }

    /// Consecutive parts of one field become one line with the parts beneath.
    fn by_field(&self, layer: &PacketLayer, row: Option<usize>, parts: Members) -> Vec<Node> {
        let mut nodes: Vec<Node> = Vec::new();
        let mut run: Vec<usize> = Vec::new();
        let mut run_field: Option<String> = None;
        let flush = |nodes: &mut Vec<Node>, field: &Option<String>, run: &mut Vec<usize>| {
            match (field, run.len()) {
                (_, 0) => {}
                (Some(name), n) if n > 1 => {
                    let fields = match row {
                        Some(r) => layer.rows.get(r).map(Vec::as_slice).unwrap_or_default(),
                        None => layer.fields.as_slice(),
                    };
                    let value = fields
                        .iter()
                        .find(|f| &f.name == name)
                        .map_or(String::new(), field_text);
                    let row_key = row.map_or(String::new(), |r| format!("/{r}"));
                    nodes.push(Node::Group {
                        key: format!("{}{row_key}/{name}", layer.name),
                        label: name.clone(),
                        value,
                        children: run.iter().map(|&i| Node::Part(i)).collect(),
                    });
                }
                _ => nodes.extend(run.iter().map(|&i| Node::Part(i))),
            }
            run.clear();
        };
        for (field, index) in parts {
            if field.is_none() || field != run_field {
                flush(&mut nodes, &run_field, &mut run);
                run_field = field;
            }
            run.push(index);
        }
        flush(&mut nodes, &run_field, &mut run);
        nodes
    }

    /// The decoded fields of a layer, for a CLI without a layout.
    fn field_nodes(&mut self, layer: &PacketLayer) -> Vec<Node> {
        let mut nodes: Vec<Node> = layer
            .fields
            .iter()
            .map(|f| Node::Part(self.push(Part::from_field(layer, None, f))))
            .collect();
        for (row, fields) in layer.rows.iter().enumerate() {
            let children = fields
                .iter()
                .map(|f| Node::Part(self.push(Part::from_field(layer, Some(row), f))))
                .collect();
            nodes.push(Node::Group {
                key: format!("{}/{row}", layer.name),
                label: format!("{} {}", row_noun(&layer.name), row + 1),
                value: fields
                    .iter()
                    .map(field_text)
                    .collect::<Vec<_>>()
                    .join(" · "),
                children,
            });
        }
        nodes
    }

    /// The parts a selection covers.
    pub fn selected_parts(&self, selection: &Selection) -> Vec<usize> {
        match selection {
            Selection::Part(id) => self
                .parts
                .iter()
                .position(|p| &p.id == id)
                .into_iter()
                .collect(),
            Selection::Group(key) => self
                .layers
                .iter()
                .flat_map(|l| &l.nodes)
                .find_map(|node| find_group(node, key))
                .map(leaves)
                .unwrap_or_default(),
        }
    }

    /// The parts of a layer and of the layers beneath it.
    fn layer_parts(&self, index: usize) -> Vec<usize> {
        let mut parts: Vec<usize> = self.layers[index].nodes.iter().flat_map(leaves).collect();
        for (child, layer) in self.layers.iter().enumerate() {
            if layer.parent == Some(index) {
                parts.extend(self.layer_parts(child));
            }
        }
        parts
    }

    /// Bits that some part classifies.
    pub fn classified(&self) -> usize {
        self.owners.iter().filter(|o| o.is_some()).count()
    }
}

fn find_group<'a>(node: &'a Node, key: &str) -> Option<&'a Node> {
    match node {
        Node::Group {
            key: k, children, ..
        } => {
            if k == key {
                Some(node)
            } else {
                children.iter().find_map(|c| find_group(c, key))
            }
        }
        Node::Part(_) => None,
    }
}

fn leaves(node: &Node) -> Vec<usize> {
    match node {
        Node::Part(i) => vec![*i],
        Node::Group { children, .. } => children.iter().flat_map(leaves).collect(),
    }
}

/// "bits 107–119" or "bit 9".
pub fn bits_text(ranges: &[Range<usize>]) -> String {
    let spans: Vec<String> = ranges
        .iter()
        .map(|r| {
            if r.len() == 1 {
                r.start.to_string()
            } else {
                format!("{}–{}", r.start, r.end - 1)
            }
        })
        .collect();
    let count: usize = ranges.iter().map(Range::len).sum();
    format!(
        "bit{} {}",
        if count == 1 { "" } else { "s" },
        spans.join(", ")
    )
}

/// "byte 13, bit 4" for packet bit 108: bits are numbered 7 (MSB) to 0.
pub fn coordinates(bit: usize) -> String {
    format!("byte {}, bit {}", bit / 8, 7 - bit % 8)
}

/// Where a part is: its packet bits and the byte/bit coordinates they span.
pub fn position(ranges: &[Range<usize>]) -> String {
    let (Some(first), Some(last)) = (ranges.first(), ranges.last()) else {
        return String::new();
    };
    let count: usize = ranges.iter().map(Range::len).sum();
    let span = if count == 1 {
        coordinates(first.start)
    } else {
        format!(
            "{} – {}",
            coordinates(first.start),
            coordinates(last.end - 1)
        )
    };
    format!(
        "packet {} ({count} bit{}) · {span}",
        bits_text(ranges),
        if count == 1 { "" } else { "s" }
    )
}

/// The facts of a part: label and kind, position, value, expected value and
/// rule, and where it is.
pub fn describe(part: &Part) -> Vec<(String, String)> {
    let mut rows = vec![
        ("Kind".to_owned(), part.kind.name().to_owned()),
        ("Position".to_owned(), position(&part.ranges)),
        ("Value".to_owned(), part.value.clone()),
    ];
    if let Some(expected) = &part.expected {
        let verdict = match part.valid {
            Some(false) => "mismatch",
            _ => "valid",
        };
        rows.push(("Expected".to_owned(), format!("{expected} · {verdict}")));
    }
    if let Some(rule) = &part.rule {
        rows.push(("Rule".to_owned(), rule.clone()));
    }
    if part.kind == Kind::Unknown {
        rows.push((
            "Note".to_owned(),
            "Present in the packet but not interpreted by this decoder".to_owned(),
        ));
    }
    rows.push(("In".to_owned(), part.context.clone()));
    rows
}

/// Bytes per grid row that fit `width`: 4 (a 32-bit word, as the
/// specification draws its syntax), 2 or 1.
pub fn bytes_per_row(width: f32) -> usize {
    [4, 2]
        .into_iter()
        .find(|&n| grid_width(n) <= width)
        .unwrap_or(1)
}

const CELL: egui::Vec2 = egui::vec2(15.0, 20.0);
const BYTE_GAP: f32 = 8.0;
const OFFSET_WIDTH: f32 = 40.0;
const HEX_WIDTH: f32 = 22.0;

fn grid_width(bytes: usize) -> f32 {
    OFFSET_WIDTH + bytes as f32 * (8.0 * CELL.x + BYTE_GAP + HEX_WIDTH)
}

/// The page's state: the selected line, and whether the grid should scroll
/// to it.
#[derive(Default)]
pub struct State {
    pub selection: Option<Selection>,
    scroll_to_selection: bool,
    /// The selectable lines of the field list as last drawn, top to bottom:
    /// the lines of collapsed groups and layers are not in it.
    lines: Vec<Selection>,
    /// Scroll the field list to the selected line on the next frame.
    scroll_list: bool,
    /// Open the layers holding the selection on the next frame, which may
    /// have been made in the grid while they were collapsed.
    reveal: bool,
}

impl State {
    /// A page opened with `selection`, at the top of the packet.
    #[cfg(test)]
    pub fn with(selection: Option<Selection>) -> Self {
        State {
            reveal: selection.is_some(),
            selection,
            ..State::default()
        }
    }

    pub fn select(&mut self, selection: Option<Selection>) {
        self.scroll_to_selection = selection.is_some();
        self.reveal = selection.is_some();
        self.selection = selection;
    }

    /// Moves the selection `delta` lines up (negative) or down the field list
    /// as drawn, stopping at its ends; nothing happens without a selection.
    /// The list and the grid scroll to the new line.
    pub fn step(&mut self, delta: isize) {
        let Some(current) = &self.selection else {
            return;
        };
        let Some(at) = self.lines.iter().position(|l| l == current) else {
            return;
        };
        let to = at.saturating_add_signed(delta).min(self.lines.len() - 1);
        if to != at {
            self.select(Some(self.lines[to].clone()));
            self.scroll_list = true;
        }
    }
}

/// The swatch of a role: a filled square, or an outlined striped one for
/// bits that are not decoded.
fn swatch(ui: &mut egui::Ui, role: Role) {
    let (rect, _) = ui.allocate_exact_size(egui::vec2(10.0, 10.0), egui::Sense::hover());
    paint_cell(ui.painter(), rect, role, ui.visuals(), 1.0);
}

fn paint_cell(
    painter: &egui::Painter,
    rect: egui::Rect,
    role: Role,
    visuals: &egui::Visuals,
    alpha: f32,
) {
    let color = role.color(visuals);
    if role == Role::Unknown {
        let stroke = egui::Stroke::new(
            1.0,
            visuals.weak_text_color().gamma_multiply(alpha.max(0.6)),
        );
        let clipped = painter.with_clip_rect(rect);
        let mut x = rect.left() - rect.height();
        while x < rect.right() {
            clipped.line_segment(
                [
                    egui::pos2(x, rect.bottom()),
                    egui::pos2(x + rect.height(), rect.top()),
                ],
                stroke,
            );
            x += 5.0;
        }
        painter.rect_stroke(rect, 1.0, stroke, egui::StrokeKind::Inside);
    } else {
        painter.rect_filled(rect, 1.0, color.gamma_multiply(alpha));
    }
    if role == Role::Mismatch {
        painter.rect_stroke(
            rect,
            1.0,
            egui::Stroke::new(1.5, color),
            egui::StrokeKind::Inside,
        );
    }
}

/// The legend of the role colors.
pub fn legend(ui: &mut egui::Ui) {
    ui.horizontal_wrapped(|ui| {
        for (role, label) in Role::LEGEND {
            // Each swatch stays with its label when the legend wraps.
            ui.horizontal(|ui| {
                swatch(ui, role);
                ui.weak(label);
            });
            ui.add_space(6.0);
        }
    });
}

/// The facts of the selection, above the grid.
pub fn selection_card(ui: &mut egui::Ui, model: &Model, state: &State) {
    egui::Frame::group(ui.style()).show(ui, |ui| {
        ui.set_width(ui.available_width());
        let Some(selection) = &state.selection else {
            ui.weak("Select a field or part on the left, or click a bit, to see its wire layout.");
            return;
        };
        let parts = model.selected_parts(selection);
        match (selection, parts.as_slice()) {
            (Selection::Part(_), [index]) => {
                let part = &model.parts[*index];
                ui.horizontal(|ui| {
                    swatch(ui, part.role());
                    ui.label(crate::theme::emphasis(&part.label));
                });
                facts(ui, "selected_part", &describe(part));
            }
            (_, indices) if !indices.is_empty() => {
                let mut ranges: Vec<Range<usize>> = indices
                    .iter()
                    .flat_map(|&i| model.parts[i].ranges.clone())
                    .collect();
                ranges.sort_by_key(|r| r.start);
                let mismatches = indices
                    .iter()
                    .filter(|&&i| model.parts[i].valid == Some(false))
                    .count();
                let (label, value) = group_label(model, selection);
                ui.label(crate::theme::emphasis(label));
                let mut rows = vec![
                    ("Value".to_owned(), value),
                    ("Position".to_owned(), position(&merge(ranges))),
                    ("Parts".to_owned(), indices.len().to_string()),
                ];
                if mismatches > 0 {
                    rows.push((
                        "Fixed bits".to_owned(),
                        format!("{mismatches} mismatch(es)"),
                    ));
                }
                facts(ui, "selected_group", &rows);
            }
            _ => {
                ui.weak("The selection is not in this packet.");
            }
        }
    });
}

fn group_label(model: &Model, selection: &Selection) -> (String, String) {
    if let Selection::Group(key) = selection {
        for node in model.layers.iter().flat_map(|l| &l.nodes) {
            if let Some(Node::Group { label, value, .. }) = find_group(node, key) {
                return (label.clone(), value.clone());
            }
        }
    }
    (String::new(), String::new())
}

/// Adjacent ranges joined, so a field's complete span reads as one.
fn merge(ranges: Vec<Range<usize>>) -> Vec<Range<usize>> {
    let mut merged: Vec<Range<usize>> = Vec::new();
    for r in ranges {
        match merged.last_mut() {
            Some(last) if last.end == r.start => last.end = r.end,
            _ => merged.push(r),
        }
    }
    merged
}

fn facts(ui: &mut egui::Ui, id: &str, rows: &[(String, String)]) {
    egui::Grid::new(id)
        .num_columns(2)
        .spacing([12.0, 2.0])
        .show(ui, |ui| {
            for (key, value) in rows {
                ui.weak(key);
                ui.add(egui::Label::new(egui::RichText::new(value).monospace()).wrap());
                ui.end_row();
            }
        });
}

/// The field list: one collapsible section per layer, in file order, with
/// the layer that decodes the payload (PES header or PSI section) inside the
/// payload's section. Lines are dense (small button padding and spacing) so
/// a long table fits.
pub fn field_list(ui: &mut egui::Ui, model: &Model, state: &mut State) {
    state.lines.clear();
    let reveal = std::mem::take(&mut state.reveal);
    let spacing = ui.spacing_mut();
    spacing.button_padding = egui::vec2(8.0, 3.0);
    spacing.interact_size.y = 19.0;
    spacing.item_spacing.y = 2.0;
    for (index, layer) in model.layers.iter().enumerate() {
        if layer.parent.is_none() {
            layer_section(ui, model, index, state, reveal);
        }
    }
}

/// A layer's section with the layers beneath it; opened when `reveal` asks
/// for the selection to be shown and it is inside.
fn layer_section(ui: &mut egui::Ui, model: &Model, index: usize, state: &mut State, reveal: bool) {
    let layer = &model.layers[index];
    let color = layer_color(byte_layer(&layer.name));
    let mut header =
        egui::CollapsingHeader::new(crate::theme::emphasis(layer_title(&layer.name)).color(color))
            .id_salt(("wire_layer", index))
            .default_open(true);
    if reveal && let Some(selection) = &state.selection {
        let parts = model.layer_parts(index);
        if model
            .selected_parts(selection)
            .iter()
            .any(|i| parts.contains(i))
        {
            header = header.open(Some(true));
        }
    }
    header.show(ui, |ui| {
        for node in &layer.nodes {
            node_line(ui, model, node, state);
        }
        for (child, layer) in model.layers.iter().enumerate() {
            if layer.parent == Some(index) {
                layer_section(ui, model, child, state, reveal);
            }
        }
    });
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

/// The text of a part's line: label, value, bits and the validity of fixed
/// bits, as (label, value, the rest).
pub fn part_line(part: &Part) -> (String, String, String) {
    let mark = match (part.valid, &part.expected) {
        (Some(true), _) => " ✓".to_owned(),
        (Some(false), Some(expected)) => format!(" ✗ expected {expected}"),
        _ => String::new(),
    };
    (
        part.label.clone(),
        part.value.clone(),
        format!("{mark}  · {}", bits_text(&part.ranges)),
    )
}

/// A line of the field list: the label, the value in the monospace data
/// face (as in the Packets pane and the facts), then the rest. Its text, and
/// so its accessible name, is `label  value rest`.
fn line_text(ui: &egui::Ui, label: &str, value: &str, rest: &str) -> egui::text::LayoutJob {
    let mut job = egui::text::LayoutJob::default();
    let style = ui.style();
    let font = egui::FontSelection::Default;
    let valign = egui::Align::Center;
    egui::RichText::new(format!("{label}  ")).append_to(&mut job, style, font.clone(), valign);
    egui::RichText::new(value)
        .monospace()
        .append_to(&mut job, style, font.clone(), valign);
    egui::RichText::new(rest).append_to(&mut job, style, font, valign);
    job
}

/// Brings the selected line into view after ↑/↓ moved the selection to it.
fn scroll_if_stepped(state: &mut State, on: bool, response: &egui::Response) {
    if on && state.scroll_list {
        state.scroll_list = false;
        response.scroll_to_me(None);
    }
}

fn node_line(ui: &mut egui::Ui, model: &Model, node: &Node, state: &mut State) {
    match node {
        Node::Part(index) => {
            let part = &model.parts[*index];
            let selection = Selection::Part(part.id.clone());
            ui.horizontal(|ui| {
                swatch(ui, part.role());
                let on = state.selection.as_ref() == Some(&selection);
                let (label, value, rest) = part_line(part);
                let text = line_text(ui, &label, &value, &rest);
                let response = ui
                    .selectable_label(on, text)
                    .on_hover_ui(|ui| facts(ui, &format!("hover_{}", part.id), &describe(part)));
                scroll_if_stepped(state, on, &response);
                state.lines.push(selection.clone());
                if response.clicked() {
                    state.select(if on { None } else { Some(selection) });
                }
            });
        }
        Node::Group {
            key,
            label,
            value,
            children,
        } => {
            let selection = Selection::Group(key.clone());
            let id = ui.make_persistent_id(("wire_group", key));
            let inside = state.selection.as_ref().is_some_and(|s| match s {
                Selection::Part(_) => model
                    .selected_parts(s)
                    .iter()
                    .any(|i| leaves(node).contains(i)),
                Selection::Group(k) => k
                    .strip_prefix(key.as_str())
                    .is_some_and(|rest| rest.starts_with('/')),
            });
            // Open when asked to, or while one of its parts is selected.
            let mut open = ui.data(|d| d.get_temp::<bool>(id)).unwrap_or(false) || inside;
            ui.horizontal(|ui| {
                let toggle = ui
                    .add(
                        egui::Button::new(icons::text(if open {
                            icons::CARET_DOWN
                        } else {
                            icons::CARET_RIGHT
                        }))
                        .frame(false),
                    )
                    .on_hover_text(if open {
                        "Hide the parts"
                    } else {
                        "Show the parts"
                    });
                toggle.widget_info(|| {
                    egui::WidgetInfo::labeled(
                        egui::Role::Button,
                        true,
                        format!("{} parts of {label}", if open { "Hide" } else { "Show" }),
                    )
                });
                if toggle.clicked() {
                    open = !open;
                }
                let on = state.selection.as_ref() == Some(&selection);
                let parts = leaves(node);
                let mut ranges: Vec<Range<usize>> = parts
                    .iter()
                    .flat_map(|&i| model.parts[i].ranges.clone())
                    .collect();
                ranges.sort_by_key(|r| r.start);
                let bad = parts.iter().any(|&i| model.parts[i].valid == Some(false));
                let rest = format!(
                    "{}  · {}",
                    if bad { " ✗" } else { "" },
                    bits_text(&merge(ranges))
                );
                let text = line_text(ui, label, value, &rest);
                let response = ui.selectable_label(on, text);
                scroll_if_stepped(state, on, &response);
                state.lines.push(selection.clone());
                if response.clicked() {
                    state.select(if on { None } else { Some(selection.clone()) });
                }
            });
            ui.data_mut(|d| d.insert_temp(id, open));
            if open {
                ui.indent(id, |ui| {
                    for child in children {
                        node_line(ui, model, child, state);
                    }
                });
            }
        }
    }
}

/// Every bit of the packet in file order, `bytes_per_row` bytes a row, each
/// byte from bit 7 to bit 0, colored by role with the selection marked.
/// Hovering a bit describes its part; clicking selects it.
/// `per_row` comes from [`bytes_per_row`]; [`grid_header`] heads the columns.
pub fn bit_grid(ui: &mut egui::Ui, raw: &[u8], per_row: usize, model: &Model, state: &mut State) {
    let rows = raw.len().div_ceil(per_row);
    let size = egui::vec2(grid_width(per_row), CELL.y * rows as f32);
    let (rect, response) = ui.allocate_exact_size(size, egui::Sense::click());
    let total = raw.len() * 8;
    response.widget_info(|| {
        egui::WidgetInfo::labeled(
            egui::Role::Image,
            true,
            format!(
                "Bit grid: {} of {total} bits classified",
                model.classified()
            ),
        )
    });
    let selected: Vec<usize> = state
        .selection
        .as_ref()
        .map(|s| model.selected_parts(s))
        .unwrap_or_default();
    let byte_x = |column: usize| {
        rect.left() + OFFSET_WIDTH + column as f32 * (8.0 * CELL.x + BYTE_GAP + HEX_WIDTH)
    };
    let cell_rect = |bit: usize| {
        let byte = bit / 8;
        let (row, column) = (byte / per_row, byte % per_row);
        egui::Rect::from_min_size(
            egui::pos2(
                byte_x(column) + (bit % 8) as f32 * CELL.x,
                rect.top() + row as f32 * CELL.y,
            ),
            CELL,
        )
    };
    let bit_at = |pos: egui::Pos2| (0..total).find(|&bit| cell_rect(bit).contains(pos));

    if ui.is_rect_visible(rect) {
        let painter = ui.painter();
        let visuals = ui.visuals();
        let mono = egui::TextStyle::Monospace.resolve(ui.style());
        let weak = visuals.weak_text_color();
        for (index, &byte) in raw.iter().enumerate() {
            let (row, column) = (index / per_row, index % per_row);
            let y = rect.top() + (row as f32 + 0.5) * CELL.y;
            if column == 0 {
                painter.text(
                    egui::pos2(rect.left(), y),
                    egui::Align2::LEFT_CENTER,
                    format!("{index:03}"),
                    mono.clone(),
                    weak,
                );
            }
            painter.text(
                egui::pos2(byte_x(column) + 8.0 * CELL.x + 4.0, y),
                egui::Align2::LEFT_CENTER,
                format!("{byte:02X}"),
                mono.clone(),
                weak,
            );
        }
        for bit in 0..total {
            let cell = cell_rect(bit).shrink2(egui::vec2(0.5, 1.0));
            let owner = model.owners[bit];
            let on = owner.is_some_and(|o| selected.contains(&o));
            let text_color = match owner {
                Some(o) => {
                    let role = model.parts[o].role();
                    paint_cell(painter, cell, role, visuals, if on { 1.0 } else { 0.4 });
                    if on {
                        visuals.strong_text_color()
                    } else {
                        visuals.text_color()
                    }
                }
                None => weak,
            };
            let value = (raw[bit / 8] >> (7 - bit % 8)) & 1;
            painter.text(
                cell.center(),
                egui::Align2::CENTER_CENTER,
                value.to_string(),
                mono.clone(),
                text_color,
            );
        }
        // The selection outlined, one box per run within a row.
        let stroke = egui::Stroke::new(2.0, visuals.strong_text_color());
        let mut bit = 0;
        while bit < total {
            let on = model.owners[bit].is_some_and(|o| selected.contains(&o));
            if !on {
                bit += 1;
                continue;
            }
            let start = bit;
            while bit + 1 < total
                && model.owners[bit + 1].is_some_and(|o| selected.contains(&o))
                && (bit + 1) % 8 != 0
            {
                bit += 1;
            }
            let outline = cell_rect(start).union(cell_rect(bit));
            painter.rect_stroke(outline, 2.0, stroke, egui::StrokeKind::Inside);
            bit += 1;
        }
    }

    if state.scroll_to_selection {
        state.scroll_to_selection = false;
        if let Some(first) =
            (0..total).find(|&b| model.owners[b].is_some_and(|o| selected.contains(&o)))
        {
            // Only as far as needed to bring it into view.
            let cell = cell_rect(first);
            if !ui.clip_rect().contains_rect(cell) {
                ui.scroll_to_rect(cell, None);
            }
        }
    }

    let hovered = response.hover_pos().and_then(bit_at);
    let clicked = response.clicked();
    let response = match hovered {
        Some(bit) => response.on_hover_ui_at_pointer(|ui| {
            let value = (raw[bit / 8] >> (7 - bit % 8)) & 1;
            ui.monospace(format!("packet bit {bit} · {} = {value}", coordinates(bit)));
            match model.owners[bit] {
                Some(owner) => {
                    let part = &model.parts[owner];
                    ui.label(crate::theme::emphasis(&part.label));
                    facts(ui, "hover_bit", &describe(part));
                }
                None => {
                    ui.weak("Not classified: this CLI does not report the wire layout.");
                }
            }
        }),
        None => response,
    };
    if clicked
        && let Some(bit) = response.interact_pointer_pos().and_then(bit_at)
        && let Some(owner) = model.owners[bit]
    {
        let selection = Selection::Part(model.parts[owner].id.clone());
        let on = state.selection.as_ref() == Some(&selection);
        state.select(if on { None } else { Some(selection) });
    }
}

/// The column heads of the bit grid, kept above its scrolling rows: the bit
/// numbers 7 to 0 of each byte.
pub fn grid_header(ui: &mut egui::Ui, per_row: usize) {
    let (rect, _) = ui.allocate_exact_size(
        egui::vec2(grid_width(per_row), CELL.y),
        egui::Sense::hover(),
    );
    let mono = egui::TextStyle::Monospace.resolve(ui.style());
    let weak = ui.visuals().weak_text_color();
    let painter = ui.painter();
    painter.text(
        rect.left_center(),
        egui::Align2::LEFT_CENTER,
        "byte",
        mono.clone(),
        weak,
    );
    for column in 0..per_row {
        let x = rect.left() + OFFSET_WIDTH + column as f32 * (8.0 * CELL.x + BYTE_GAP + HEX_WIDTH);
        for bit in 0..8 {
            painter.text(
                egui::pos2(x + (bit as f32 + 0.5) * CELL.x, rect.center().y),
                egui::Align2::CENTER_CENTER,
                (7 - bit).to_string(),
                mono.clone(),
                weak,
            );
        }
        painter.text(
            egui::pos2(x + 8.0 * CELL.x + 4.0, rect.center().y),
            egui::Align2::LEFT_CENTER,
            "hex",
            mono.clone(),
            weak,
        );
    }
}

/// What the page asks its owner to do.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Action {
    Back,
    Go(u64),
}

/// The packet the page shows and its neighbors in the analyzed range.
pub struct Header {
    pub title: String,
    /// What the packet is (see `presentation::packet::packet_kind`), once decoded.
    pub kind: Option<String>,
    pub context: String,
    pub previous: Option<u64>,
    pub next: Option<u64>,
}

/// The page header: back to the list, where the packet is with a badge
/// saying what it is, and Previous/Next.
pub fn header(ui: &mut egui::Ui, header: &Header) -> Option<Action> {
    let mut action = None;
    ui.horizontal(|ui| {
        let back = ui.button((icons::text(icons::ARROW_LEFT), "Back to Packets"));
        back.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Button, true, "Back to Packets"));
        if back.clicked() {
            action = Some(Action::Back);
        }
        ui.weak("Packets ›");
        let title = ui.label(crate::theme::emphasis(&header.title));
        title.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Heading, true, &header.title));
        if let Some(kind) = &header.kind {
            kind_badge(ui, kind);
        }
        ui.weak("› Detailed inspection");
        ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
            if ui
                .add_enabled(
                    header.next.is_some(),
                    egui::Button::new(("Next", icons::text(icons::CARET_RIGHT))),
                )
                .on_hover_text("The next packet of the range (→)")
                .clicked()
            {
                action = header.next.map(Action::Go);
            }
            if ui
                .add_enabled(
                    header.previous.is_some(),
                    egui::Button::new((icons::text(icons::CARET_LEFT), "Previous")),
                )
                .on_hover_text("The previous packet of the range (←)")
                .clicked()
            {
                action = header.previous.map(Action::Go);
            }
        });
    });
    ui.weak(&header.context);
    action
}

/// The badge of the packet kind: a status badge's text on a fill by kind
/// (PSI, video, audio, PCR, other).
fn kind_badge(ui: &mut egui::Ui, kind: &str) {
    let response = egui::Frame::new()
        .fill(kind_color(kind))
        .corner_radius(crate::theme::BADGE_RADIUS)
        .inner_margin(crate::theme::BADGE_MARGIN)
        .show(ui, |ui| {
            ui.label(crate::theme::badge_text(kind).color(crate::theme::ON_FILL_TEXT))
        })
        .inner;
    response.widget_info(|| {
        egui::WidgetInfo::labeled(egui::Role::Label, true, format!("Packet type: {kind}"))
    });
}

fn kind_color(kind: &str) -> egui::Color32 {
    use crate::theme::{KIND_AUDIO, KIND_OTHER, KIND_PCR, KIND_PES, KIND_PSI, KIND_VIDEO};
    if kind.contains("audio") {
        KIND_AUDIO
    } else if kind.contains("video") {
        KIND_VIDEO
    } else if kind == "PCR only" || kind == "PCR" {
        KIND_PCR
    } else if kind.starts_with("PES") {
        KIND_PES
    } else if matches!(kind, "Null" | "Adaptation field only" | "TS packet") {
        KIND_OTHER
    } else {
        KIND_PSI
    }
}

#[cfg(test)]
// Bit ranges are lists of ranges; a list of one is the common case here.
#[allow(clippy::single_range_in_vec_init)]
mod tests {
    use super::*;
    use crate::report::PacketDetail;

    fn detail(json: &str) -> PacketDetail {
        serde_json::from_str(json).unwrap()
    }

    fn video_204() -> PacketDetail {
        detail(include_str!(
            "../../tests/fixtures/packet_detail_video_204.json"
        ))
    }

    #[test]
    fn classifies_every_bit_of_a_204_byte_packet() {
        let detail = video_204();
        let packet = detail.packet.unwrap();
        let model = Model::new(&packet, packet.raw().len() * 8);
        assert!(model.complete);
        assert_eq!(model.classified(), 204 * 8);
        // The trailer is not decoded; the AF stuffing and PCR reserved bits
        // have their own roles.
        let trailer = model.owners[1504].unwrap();
        assert_eq!(model.parts[trailer].role(), Role::Unknown);
        assert_eq!(model.parts[model.owners[100].unwrap()].kind, Kind::Stuffing);
        assert_eq!(
            model.parts[model.owners[81].unwrap()].role(),
            Role::Reserved
        );
        // The PES header covers the payload bits it sits in.
        assert_eq!(
            model.parts[model.owners[1264].unwrap()].label,
            "packet_start_code_prefix"
        );
    }

    #[test]
    fn groups_the_parts_of_a_field_under_it() {
        let detail = video_204();
        let packet = detail.packet.unwrap();
        let model = Model::new(&packet, 204 * 8);
        let pts = Selection::Group("pes_header/PTS".to_owned());
        let parts = model.selected_parts(&pts);
        let labels: Vec<&str> = parts
            .iter()
            .map(|&i| model.parts[i].label.as_str())
            .collect();
        assert_eq!(
            labels,
            [
                "prefix",
                "PTS[32..30]",
                "marker_bit",
                "PTS[29..15]",
                "marker_bit",
                "PTS[14..0]",
                "marker_bit"
            ]
        );
        // The complete encoded span of the PTS: 5 bytes.
        let ranges = merge(
            parts
                .iter()
                .flat_map(|&i| model.parts[i].ranges.clone())
                .collect(),
        );
        assert_eq!(ranges, [1336..1376]);
        let pcr = model.selected_parts(&Selection::Group(
            "adaptation_field/program_clock_reference".to_owned(),
        ));
        assert_eq!(pcr.len(), 3);
        // A part on its own is not a group.
        let pid = model.selected_parts(&Selection::Part("ts_header.pid@11".to_owned()));
        assert_eq!(model.parts[pid[0]].ranges, [11..24]);
    }

    #[test]
    fn groups_table_entries() {
        let detail = detail(include_str!(
            "../../tests/fixtures/packet_detail_pmt_1001_188.json"
        ));
        let packet = detail.packet.unwrap();
        let model = Model::new(&packet, 188 * 8);
        let pmt = model.layers.iter().find(|l| l.name == "pmt").unwrap();
        let entries: Vec<&str> = pmt
            .nodes
            .iter()
            .filter_map(|n| match n {
                Node::Group { label, .. } => Some(label.as_str()),
                Node::Part(_) => None,
            })
            .collect();
        assert_eq!(entries, ["stream 1", "stream 2"]);
        let stream = model.selected_parts(&Selection::Group("pmt/0".to_owned()));
        let labels: Vec<&str> = stream
            .iter()
            .map(|&i| model.parts[i].label.as_str())
            .collect();
        assert_eq!(
            labels,
            [
                "stream_type",
                "reserved",
                "elementary_PID",
                "reserved",
                "ES_info_length"
            ]
        );
        assert_eq!(model.parts[stream[1]].context, "Payload › PMT › stream 1");
    }

    /// A PES header or PSI section decodes the payload it follows; the
    /// framing layers, the trailer among them, have no parent.
    #[test]
    fn decoded_layers_are_beneath_the_payload() {
        assert_eq!(
            layer_parents(["ts_header", "payload", "pmt"]),
            [None, None, Some(1)]
        );
        assert_eq!(
            layer_parents([
                "ts_header",
                "adaptation_field",
                "payload",
                "pes_header",
                "trailer"
            ]),
            [None, None, None, Some(2), None]
        );
        // A continuation or scrambled payload: nothing decodes it.
        assert_eq!(layer_parents(["ts_header", "payload"]), [None, None]);
        assert_eq!(
            layer_parents(["ts_header", "payload", "trailer"]),
            [None, None, None]
        );
        // Nothing to decode before a payload.
        assert_eq!(layer_parents(["ts_header", "pmt"]), [None, None]);

        let packet = video_204().packet.unwrap();
        let model = Model::new(&packet, 204 * 8);
        let parents: Vec<_> = model.layers.iter().map(|l| l.parent).collect();
        assert_eq!(parents, [None, None, None, Some(2), None]);
        // The payload's parts include those of the PES header beneath it.
        let pes = model.parts.iter().position(|p| p.label == "PTS[14..0]");
        assert!(model.layer_parts(2).contains(&pes.unwrap()));
        assert!(!model.layer_parts(4).contains(&pes.unwrap()));
        let prefix = model.owners[1264].unwrap();
        assert_eq!(
            model.parts[prefix].context,
            "Payload › PES header › packet_start_code_prefix"
        );
        let pid = model.owners[11].unwrap();
        assert_eq!(model.parts[pid].context, "TS header › PID");
    }

    #[test]
    fn falls_back_to_field_bits_without_a_layout() {
        let detail = detail(include_str!(
            "../../tests/fixtures/packet_detail_pat_188.json"
        ));
        let packet = detail.packet.unwrap();
        assert!(packet.layers.iter().all(|l| l.bit_layout.is_empty()));
        let model = Model::new(&packet, 188 * 8);
        assert!(!model.complete);
        // Field bits only: the PID keeps its 13 bits, and the 3 reserved
        // bits before program_map_PID are only part of the payload.
        let pid = model.parts.iter().position(|p| p.label == "PID").unwrap();
        assert_eq!(model.parts[pid].ranges, [11..24]);
        assert_eq!(model.parts[model.owners[120].unwrap()].label, "payload");
        assert_eq!(
            model.parts[model.owners[123].unwrap()].label,
            "program_map_PID"
        );
    }

    #[test]
    fn mismatches_override_the_role() {
        assert_eq!(Role::of(Kind::Reserved, Some(false)), Role::Mismatch);
        assert_eq!(Role::of(Kind::Reserved, Some(true)), Role::Reserved);
        assert_eq!(Role::of(Kind::Prefix, None), Role::Fixed);
        assert_eq!(Kind::parse("something new"), Kind::Unknown);
        let visuals = egui::Visuals::dark();
        let colors: std::collections::HashSet<_> = Role::LEGEND
            .iter()
            .map(|(role, _)| role.color(&visuals))
            .collect();
        assert_eq!(colors.len(), Role::LEGEND.len());
    }

    #[test]
    fn describes_positions() {
        assert_eq!(coordinates(108), "byte 13, bit 3");
        assert_eq!(
            position(&[107..120]),
            "packet bits 107–119 (13 bits) · byte 13, bit 4 – byte 14, bit 0"
        );
        assert_eq!(position(&[9..10]), "packet bit 9 (1 bit) · byte 1, bit 6");
        assert_eq!(bytes_per_row(2000.0), 4);
        assert_eq!(bytes_per_row(500.0), 2);
        assert_eq!(bytes_per_row(200.0), 1);
    }
}
