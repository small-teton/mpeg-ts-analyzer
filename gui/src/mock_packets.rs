//! Dummy analysis range for the Packets screen: packets, the PES/sections they
//! form, and the PSI layout in effect at each position. Generated
//! deterministically so the mock stays consistent across pages.

use std::sync::OnceLock;

use eframe::egui::Color32;

use crate::mock;

pub const RANGE_START: u64 = 0x0088_0000;
pub const RANGE_PACKETS: usize = 1000;
pub const PAGE_SIZE: usize = 100;
pub const PAGES: usize = RANGE_PACKETS.div_ceil(PAGE_SIZE);
const PACKET_SIZE: u64 = 188;
/// Packet duration at 20 Mbit/s, used to give packets a time.
const PACKET_SECONDS: f64 = 188.0 * 8.0 / 20_000_000.0;
const START_SECONDS: f64 = 42.0;
const VIDEO_PES_PACKETS: usize = 48;
const AUDIO_PES_PACKETS: usize = 3;
const LAYOUT_CHANGE_AT: usize = 501;

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum Kind {
    Pat,
    Pmt,
    Video,
    Audio,
    Null,
}

impl Kind {
    pub fn label(self) -> &'static str {
        match self {
            Kind::Pat => "PAT",
            Kind::Pmt => "PMT",
            Kind::Video => "Video",
            Kind::Audio => "Audio",
            Kind::Null => "Null",
        }
    }

    pub fn color(self) -> Color32 {
        match self {
            Kind::Pat | Kind::Pmt => mock::PSI_COLOR,
            Kind::Video => mock::VIDEO_COLOR,
            Kind::Audio => mock::AUDIO_COLOR,
            Kind::Null => Color32::GRAY,
        }
    }

    /// Which open-unit slot packets of this kind continue in while generating.
    fn lane(self) -> Option<usize> {
        match self {
            Kind::Video => Some(0),
            Kind::Audio => Some(1),
            Kind::Pat | Kind::Pmt => Some(2),
            Kind::Null => None,
        }
    }

    pub fn is_section(self) -> bool {
        matches!(self, Kind::Pat | Kind::Pmt)
    }
}

pub struct Packet {
    /// Index within the analysis range.
    pub index: usize,
    pub offset: u64,
    pub pid: u16,
    pub kind: Kind,
    pub pusi: bool,
    pub cc: u8,
    pub pcr: Option<f64>,
    pub pcr_interval_ms: Option<f64>,
    pub cc_error: bool,
    pub unit: Option<usize>,
}

/// A PES packet or PSI section, made of one or more transport packets.
pub struct Unit {
    pub id: usize,
    pub kind: Kind,
    pub pid: u16,
    /// Range indices of its packets, in order.
    pub packets: Vec<usize>,
    /// False when the unit started before the analysis range, so its header is unknown.
    pub starts_in_range: bool,
    pub continues_after_range: bool,
    pub pts: Option<f64>,
    pub dts: Option<f64>,
}

impl Unit {
    pub fn name(&self) -> String {
        let what = if self.kind.is_section() {
            "section"
        } else {
            "PES"
        };
        format!("{} {what} #{}", self.kind.label(), self.id)
    }

    pub fn first(&self) -> usize {
        self.packets[0]
    }

    pub fn position_of(&self, index: usize) -> Option<usize> {
        self.packets.iter().position(|&i| i == index)
    }
}

/// The program layout announced by a PMT, valid from `from` onward.
pub struct Layout {
    pub from: usize,
    pub pmt_version: u8,
    pub video_pid: u16,
    pub audio_pid: u16,
}

pub struct Range {
    pub packets: Vec<Packet>,
    pub units: Vec<Unit>,
    pub layouts: Vec<Layout>,
}

impl Range {
    pub fn layout_at(&self, index: usize) -> &Layout {
        self.layouts
            .iter()
            .rev()
            .find(|layout| layout.from <= index)
            .unwrap_or(&self.layouts[0])
    }

    pub fn page_of(index: usize) -> usize {
        index / PAGE_SIZE
    }

    pub fn page(&self, page: usize) -> &[Packet] {
        let start = (page * PAGE_SIZE).min(self.packets.len());
        let end = (start + PAGE_SIZE).min(self.packets.len());
        &self.packets[start..end]
    }

    pub fn time_of(index: usize) -> f64 {
        START_SECONDS + index as f64 * PACKET_SECONDS
    }
}

pub fn range() -> &'static Range {
    static RANGE: OnceLock<Range> = OnceLock::new();
    RANGE.get_or_init(generate)
}

fn kind_at(index: usize) -> Kind {
    match index % 250 {
        0 => Kind::Pat,
        1 => Kind::Pmt,
        _ if index % 9 == 4 => Kind::Audio,
        _ if index % 23 == 11 => Kind::Null,
        _ => Kind::Video,
    }
}

fn generate() -> Range {
    let layouts = vec![
        Layout {
            from: 0,
            pmt_version: 3,
            video_pid: 0x0100,
            audio_pid: 0x0101,
        },
        Layout {
            from: LAYOUT_CHANGE_AT,
            pmt_version: 4,
            video_pid: 0x0100,
            audio_pid: 0x0102,
        },
    ];
    let mut packets = Vec::with_capacity(RANGE_PACKETS);
    let mut units: Vec<Unit> = Vec::new();
    let mut open_unit: [Option<usize>; 3] = [None; 3];
    let mut cc = std::collections::HashMap::<u16, u8>::new();
    let (mut video_count, mut audio_count) = (5usize, 1usize);

    let mut pcr_count = 0;

    for index in 0..RANGE_PACKETS {
        let kind = kind_at(index);
        let layout = layouts.iter().rev().find(|l| l.from <= index).unwrap();
        let pid = match kind {
            Kind::Pat => 0x0000,
            Kind::Pmt => 0x1000,
            Kind::Video => layout.video_pid,
            Kind::Audio => layout.audio_pid,
            Kind::Null => 0x1FFF,
        };
        let pusi = match kind {
            Kind::Pat | Kind::Pmt => true,
            Kind::Video => {
                video_count += 1;
                video_count % VIDEO_PES_PACKETS == 0
            }
            Kind::Audio => {
                audio_count += 1;
                audio_count % AUDIO_PES_PACKETS == 0
            }
            Kind::Null => false,
        };

        let counter = cc.entry(pid).or_insert((index % 16) as u8);
        // One audio packet is lost on purpose so the list has a CC error to show.
        let cc_error = kind == Kind::Audio && index == 436;
        *counter = (*counter + if cc_error { 3 } else { 1 }) & 0x0F;
        let packet_cc = if kind == Kind::Null { 0 } else { *counter };

        let time = Range::time_of(index);
        let (pcr, pcr_interval_ms) = if kind == Kind::Video && video_count % 20 == 0 {
            pcr_count += 1;
            // One interval exceeds the 100 ms limit so the list has an NG PCR to show.
            let interval = match pcr_count {
                1 => None,
                18 => Some(112.4),
                n => Some(38.0 + (n % 5) as f64 * 1.5),
            };
            (Some(time), interval)
        } else {
            (None, None)
        };

        let unit = kind.lane().map(|lane| {
            let continuing =
                open_unit[lane].filter(|&id| units[id].pid == pid || kind.is_section());
            match continuing {
                Some(id) if !pusi => {
                    units[id].packets.push(index);
                    id
                }
                _ => {
                    let id = units.len();
                    let decode = time + if kind == Kind::Video { 0.12 } else { 0.08 };
                    units.push(Unit {
                        id,
                        kind,
                        pid,
                        packets: vec![index],
                        starts_in_range: pusi,
                        continues_after_range: false,
                        pts: (pusi && !kind.is_section())
                            .then(|| decode + if kind == Kind::Video { 0.04 } else { 0.0 }),
                        dts: (pusi && kind == Kind::Video).then_some(decode),
                    });
                    open_unit[lane] = Some(id);
                    id
                }
            }
        });

        packets.push(Packet {
            index,
            offset: RANGE_START + index as u64 * PACKET_SIZE,
            pid,
            kind,
            pusi,
            cc: packet_cc,
            pcr,
            pcr_interval_ms,
            cc_error,
            unit,
        });
    }
    for id in open_unit.into_iter().flatten() {
        if !units[id].kind.is_section() {
            units[id].continues_after_range = true;
        }
    }
    Range {
        packets,
        units,
        layouts,
    }
}

/// Which structure a byte belongs to, for coloring the hex dump.
#[derive(Clone, Copy)]
pub enum ByteLayer {
    TsHeader,
    AdaptationField,
    PesHeader,
    Section,
    Payload,
}

/// Offset of the payload within the packet (after the TS header and adaptation field).
pub fn payload_start(packet: &Packet) -> usize {
    if packet.pcr.is_some() { 12 } else { 4 }
}

/// Encodes a 33-bit timestamp in the 5-byte PES form with the given 4-bit prefix.
fn timestamp_bytes(prefix: u8, seconds: f64) -> [u8; 5] {
    let ts = (seconds * 90_000.0) as u64 & 0x1_FFFF_FFFF;
    [
        (prefix << 4) | ((ts >> 29) as u8 & 0x0E) | 1,
        (ts >> 22) as u8,
        ((ts >> 14) as u8 & 0xFE) | 1,
        (ts >> 7) as u8,
        ((ts << 1) as u8 & 0xFE) | 1,
    ]
}

pub fn packet_bytes(packet: &Packet) -> Vec<(u8, ByteLayer)> {
    let range = range();
    let mut bytes = vec![
        (0x47, ByteLayer::TsHeader),
        (
            (u8::from(packet.pusi) << 6) | (packet.pid >> 8) as u8,
            ByteLayer::TsHeader,
        ),
        (packet.pid as u8, ByteLayer::TsHeader),
        (
            if packet.pcr.is_some() { 0x30 } else { 0x10 } | packet.cc,
            ByteLayer::TsHeader,
        ),
    ];
    if let Some(pcr) = packet.pcr {
        let base = (pcr * 90_000.0) as u64;
        bytes.extend(
            [
                0x07,
                0x10,
                (base >> 25) as u8,
                (base >> 17) as u8,
                (base >> 9) as u8,
                (base >> 1) as u8,
                ((base & 1) << 7) as u8 | 0x7E,
                0x00,
            ]
            .map(|b| (b, ByteLayer::AdaptationField)),
        );
    }
    let layout = range.layout_at(packet.index);
    let header: Vec<u8> = match packet.kind {
        Kind::Pat if packet.pusi => vec![
            0x00, 0x00, 0xB0, 0x0D, 0x7F, 0xE1, 0xC1, 0x00, 0x00, // header
            0x00, 0x00, 0xE0, 0x10, // program 0 -> NIT 0x0010
            0x00, 0x01, 0xF0, 0x00, // program 1 -> PMT 0x1000
            0x2D, 0xBC, 0xB0, 0x53, // CRC_32
        ],
        Kind::Pmt if packet.pusi => {
            let mut section = vec![
                0x00,
                0x02,
                0xB0,
                0x17,
                0x00,
                0x01,
                0xC1 | (layout.pmt_version << 1),
                0x00,
                0x00,
                0xE0 | (layout.video_pid >> 8) as u8,
                layout.video_pid as u8,
                0xF0,
                0x00,
            ];
            for (stream_type, pid) in [(0x1B, layout.video_pid), (0x0F, layout.audio_pid)] {
                section.extend([stream_type, 0xE0 | (pid >> 8) as u8, pid as u8, 0xF0, 0x00]);
            }
            section.extend([0x8A, 0x3F, 0x21, 0xC4]);
            section
        }
        Kind::Video | Kind::Audio if packet.pusi => {
            let unit = packet.unit.map(|id| &range.units[id]);
            let pts = unit.and_then(|u| u.pts).unwrap_or(0.0);
            let mut pes = if packet.kind == Kind::Video {
                vec![0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0xC0, 0x0A]
            } else {
                vec![0x00, 0x00, 0x01, 0xC0, 0x02, 0x28, 0x80, 0x80, 0x05]
            };
            match unit.and_then(|u| u.dts) {
                Some(dts) => {
                    pes.extend(timestamp_bytes(0x3, pts));
                    pes.extend(timestamp_bytes(0x1, dts));
                }
                None => pes.extend(timestamp_bytes(0x2, pts)),
            }
            pes
        }
        _ => Vec::new(),
    };
    let layer = if packet.kind.is_section() {
        ByteLayer::Section
    } else {
        ByteLayer::PesHeader
    };
    bytes.extend(header.into_iter().map(|b| (b, layer)));
    let fill = if packet.kind == Kind::Null || packet.kind.is_section() {
        Some(0xFF)
    } else {
        None
    };
    while bytes.len() < PACKET_SIZE as usize {
        let i = bytes.len();
        let byte = fill.unwrap_or(((i * 73 + packet.index * 31 + 29) & 0xFF) as u8);
        bytes.push((byte, ByteLayer::Payload));
    }
    bytes
}

#[cfg(test)]
mod tests {
    use super::*;

    fn decode_timestamp(b: &[u8]) -> u64 {
        (u64::from(b[0] & 0x0E) << 29)
            | (u64::from(b[1]) << 22)
            | (u64::from(b[2] & 0xFE) << 14)
            | (u64::from(b[3]) << 7)
            | (u64::from(b[4]) >> 1)
    }

    #[test]
    fn encoded_fields_sit_where_the_detail_pane_points() {
        let range = range();
        for packet in &range.packets {
            let bytes: Vec<u8> = packet_bytes(packet).into_iter().map(|(b, _)| b).collect();
            assert_eq!(bytes.len(), 188);
            assert_eq!(
                ((u16::from(bytes[1]) & 0x1F) << 8) | u16::from(bytes[2]),
                packet.pid
            );
            assert_eq!(bytes[3] & 0x0F, packet.cc);
            let p = payload_start(packet);
            match packet.kind {
                Kind::Pat if packet.pusi => {
                    assert_eq!(&bytes[p + 13..p + 17], &[0x00, 0x01, 0xF0, 0x00])
                }
                Kind::Pmt if packet.pusi => {
                    let layout = range.layout_at(packet.index);
                    assert_eq!((bytes[p + 6] >> 1) & 0x1F, layout.pmt_version);
                    let audio = ((u16::from(bytes[p + 19]) & 0x1F) << 8) | u16::from(bytes[p + 20]);
                    assert_eq!(audio, layout.audio_pid);
                }
                Kind::Video | Kind::Audio if packet.pusi => {
                    let unit = &range.units[packet.unit.unwrap()];
                    let pts = (unit.pts.unwrap() * 90_000.0) as u64;
                    assert_eq!(decode_timestamp(&bytes[p + 9..p + 14]), pts);
                    if let Some(dts) = unit.dts {
                        assert_eq!(
                            decode_timestamp(&bytes[p + 14..p + 19]),
                            (dts * 90_000.0) as u64
                        );
                    }
                }
                _ => {}
            }
        }
    }
}
