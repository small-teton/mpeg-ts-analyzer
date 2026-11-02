//! Programs found in the analyzed range, shared by Overview, Timeline and
//! Bitrate. Program 1 switches its audio PID at 30 s; program 2 is a second
//! service in the same multiplex.

use eframe::egui;

use crate::mock;

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum StreamKind {
    Video,
    Audio,
}

/// An elementary stream of a program, present in part of the range when the
/// PMT changes mid-range.
pub struct Stream {
    pub pid: u16,
    pub kind: StreamKind,
    pub from_seconds: f64,
    pub to_seconds: f64,
}

pub struct Program {
    pub number: u16,
    pub pmt_pid: u16,
    pub pcr_pid: u16,
    pub streams: Vec<Stream>,
    /// (seconds, label) where the PMT changed.
    pub pmt_updates: Vec<(f64, &'static str)>,
    /// Average bitrate of the program over the range, from the analysis.
    pub average_mbps: f64,
}

pub fn programs() -> Vec<Program> {
    vec![
        Program {
            number: 1,
            pmt_pid: 0x1000,
            pcr_pid: 0x0100,
            streams: vec![
                Stream {
                    pid: 0x0100,
                    kind: StreamKind::Video,
                    from_seconds: 0.0,
                    to_seconds: 60.0,
                },
                Stream {
                    pid: 0x0101,
                    kind: StreamKind::Audio,
                    from_seconds: 0.0,
                    to_seconds: 30.0,
                },
                Stream {
                    pid: 0x0102,
                    kind: StreamKind::Audio,
                    from_seconds: 30.0,
                    to_seconds: 60.0,
                },
            ],
            pmt_updates: vec![(30.0, "PMT v4: audio 0x0101 → 0x0102")],
            average_mbps: 1.70,
        },
        Program {
            number: 2,
            pmt_pid: 0x1100,
            pcr_pid: 0x0200,
            streams: vec![
                Stream {
                    pid: 0x0200,
                    kind: StreamKind::Video,
                    from_seconds: 0.0,
                    to_seconds: 60.0,
                },
                Stream {
                    pid: 0x0201,
                    kind: StreamKind::Audio,
                    from_seconds: 0.0,
                    to_seconds: 60.0,
                },
            ],
            pmt_updates: Vec::new(),
            average_mbps: 0.45,
        },
    ]
}

impl Stream {
    pub fn label(&self) -> String {
        let kind = match self.kind {
            StreamKind::Video => "Video",
            StreamKind::Audio => "Audio",
        };
        format!("0x{:04X} {kind}", self.pid)
    }

    pub fn color(&self, index: usize) -> egui::Color32 {
        match (self.kind, index % 2) {
            (StreamKind::Video, _) => mock::VIDEO_COLOR,
            (StreamKind::Audio, 0) => mock::AUDIO_COLOR,
            (StreamKind::Audio, _) => egui::Color32::from_rgb(0x5f, 0xc8, 0xc8),
        }
    }

    /// PCR→PTS/DTS gap samples, only where this PID is present.
    pub fn gap_ms(&self) -> Vec<[f64; 2]> {
        let samples = match self.kind {
            StreamKind::Video => mock::video_gap_ms(),
            StreamKind::Audio => mock::audio_gap_ms(),
        };
        // Vary the level per PID so different streams are distinguishable.
        let shift = f64::from(self.pid & 0x0F) * 18.0;
        samples
            .into_iter()
            .filter(|[t, _]| *t >= self.from_seconds && *t < self.to_seconds)
            .map(|[t, ms]| [t, ms + shift])
            .collect()
    }

    /// Bitrate samples in Mbps, only where this PID is present.
    pub fn bitrate_mbps(&self, program: &Program) -> Vec<[f64; 2]> {
        // Program 2 stands for a low-rate second service.
        let scale = if program.number == 1 { 1.0 } else { 0.25 };
        let samples = match self.kind {
            StreamKind::Video => mock::video_bitrate_mbps(),
            StreamKind::Audio => mock::constant_mbps(0.192),
        };
        samples
            .into_iter()
            .filter(|[t, _]| *t >= self.from_seconds && *t < self.to_seconds)
            .map(|[t, mbps]| [t, mbps * scale])
            .collect()
    }
}

impl Program {
    /// What the program carries, built from its PMT stream types since the
    /// stream may not name its services.
    pub fn description(&self) -> String {
        let mut parts: Vec<&str> = Vec::new();
        for stream in &self.streams {
            let part = match stream.kind {
                StreamKind::Video => "H.264 video",
                StreamKind::Audio => "AAC audio",
            };
            if !parts.contains(&part) {
                parts.push(part);
            }
        }
        parts.join(" + ")
    }

    pub fn label(&self) -> String {
        format!(
            "{} · {} · {:.2} Mbps (PMT 0x{:04X})",
            self.number,
            self.description(),
            self.average_mbps,
            self.pmt_pid
        )
    }
}
