//! Placeholder data for laying out the screens before the CLI exposes the
//! corresponding output. Values are made up but kept mutually consistent
//! (one 60 s program: MPEG-2 video 0x0100 carrying the PCR, MP2 audio 0x0101).

use eframe::egui::Color32;

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

    /// Foreground and background colors for a verdict badge.
    pub fn colors(self) -> (Color32, Color32) {
        match self {
            Verdict::Ok => (
                Color32::from_rgb(0x9b, 0xe8, 0xa9),
                Color32::from_rgb(0x1d, 0x4a, 0x29),
            ),
            Verdict::Ng => (
                Color32::from_rgb(0xff, 0xaa, 0xaa),
                Color32::from_rgb(0x5c, 0x1f, 0x1f),
            ),
            Verdict::Warn => (
                Color32::from_rgb(0xff, 0xd5, 0x83),
                Color32::from_rgb(0x56, 0x42, 0x0e),
            ),
        }
    }
}

pub const PCR_COLOR: Color32 = Color32::from_rgb(0xf0, 0xa4, 0x3c);
pub const VIDEO_COLOR: Color32 = Color32::from_rgb(0x4e, 0xa8, 0xff);
pub const AUDIO_COLOR: Color32 = Color32::from_rgb(0x7f, 0xd6, 0x8a);
pub const PSI_COLOR: Color32 = Color32::from_rgb(0xc7, 0x92, 0xea);
pub const LIMIT_COLOR: Color32 = Color32::from_rgb(0xff, 0x7a, 0x7a);

pub const FILE_PATH: &str =
    "~/streams/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts";
pub const FILE_SIZE: u64 = 4_187_402_240;

pub struct Check {
    pub label: &'static str,
    pub value: &'static str,
    pub note: &'static str,
    pub verdict: Verdict,
}

pub const CHECKS: [Check; 4] = [
    Check {
        label: "Max PCR interval",
        value: "112.4 ms",
        note: "limit 100 ms · at 00:00:42.120",
        verdict: Verdict::Ng,
    },
    Check {
        label: "PCR → PTS/DTS max gap",
        value: "724.0 ms",
        note: "limit 1000 ms · PID 0x0100 (DTS)",
        verdict: Verdict::Ok,
    },
    Check {
        label: "PTS/DTS anomalies",
        value: "None",
        note: "backward / wrap / jump / DTS > PTS",
        verdict: Verdict::Ok,
    },
    Check {
        label: "Continuity counter",
        value: "2 events",
        note: "PID 0x0101",
        verdict: Verdict::Warn,
    },
];

pub struct ElementaryStream {
    pub pid: &'static str,
    pub kind: &'static str,
    pub packets: &'static str,
    pub cc_events: &'static str,
    pub average: &'static str,
    pub peak: &'static str,
    pub share: &'static str,
}

pub const STREAMS: [ElementaryStream; 5] = [
    ElementaryStream {
        pid: "0x0100",
        kind: "MPEG-2 Video",
        packets: "56,649",
        cc_events: "0",
        average: "1.42 Mbps",
        peak: "1.47 Mbps",
        share: "83.5%",
    },
    ElementaryStream {
        pid: "0x0101",
        kind: "MPEG-1 Audio L2",
        packets: "7,660",
        cc_events: "2",
        average: "192 kbps",
        peak: "192 kbps",
        share: "11.3%",
    },
    ElementaryStream {
        pid: "0x0000",
        kind: "PAT",
        packets: "600",
        cc_events: "0",
        average: "15 kbps",
        peak: "15 kbps",
        share: "0.9%",
    },
    ElementaryStream {
        pid: "0x1000",
        kind: "PMT",
        packets: "600",
        cc_events: "0",
        average: "15 kbps",
        peak: "15 kbps",
        share: "0.9%",
    },
    ElementaryStream {
        pid: "0x1FFF",
        kind: "Null",
        packets: "2,310",
        cc_events: "—",
        average: "58 kbps",
        peak: "—",
        share: "3.4%",
    },
];

pub struct Warning {
    pub kind: &'static str,
    pub verdict: Verdict,
    pub text: &'static str,
}

pub const WARNINGS: [Warning; 3] = [
    Warning {
        kind: "PCR",
        verdict: Verdict::Ng,
        text: "interval 112.4 ms at 00:00:42.120 (offset 0x008892DC) exceeds 100 ms",
    },
    Warning {
        kind: "CC",
        verdict: Verdict::Warn,
        text: "PID 0x0101 at 0x003A5CDC: expected 4, got 6",
    },
    Warning {
        kind: "CC",
        verdict: Verdict::Warn,
        text: "PID 0x0101 at 0x00A55DC8: expected 11, got 13",
    },
];

/// Seconds at which the PCR interval exceeds the limit, and its value in ms.
pub const PCR_SPIKE: [f64; 2] = [42.0, 112.4];
pub const CC_EVENT_SECONDS: [f64; 2] = [18.0, 51.0];

fn sampled(step_seconds: f64, value_at: impl Fn(usize, f64) -> f64) -> Vec<[f64; 2]> {
    let samples = (60.0 / step_seconds).round() as usize;
    (0..=samples)
        .map(|index| {
            let seconds = index as f64 * step_seconds;
            [seconds, value_at(index, seconds)]
        })
        .collect()
}

pub fn pcr_interval_ms() -> Vec<[f64; 2]> {
    sampled(0.4, |index, seconds| {
        if seconds == PCR_SPIKE[0] {
            PCR_SPIKE[1]
        } else {
            let i = index as f64;
            40.0 + 3.0 * (i * 0.7).sin() + 2.0 * (i * 1.9).sin()
        }
    })
}

pub fn video_gap_ms() -> Vec<[f64; 2]> {
    sampled(0.4, |index, _| {
        let i = index as f64;
        680.0 + 34.0 * (i * 0.23).sin() + 10.0 * (i * 1.3).sin()
    })
}

pub fn audio_gap_ms() -> Vec<[f64; 2]> {
    sampled(0.4, |index, _| 330.0 + 15.0 * (index as f64 * 0.41).sin())
}

pub fn video_bitrate_mbps() -> Vec<[f64; 2]> {
    sampled(1.0, |index, _| {
        let i = index as f64;
        1.42 + 0.04 * (i * 0.9).sin() + 0.02 * (i * 2.3).sin()
    })
}

pub fn constant_mbps(value: f64) -> Vec<[f64; 2]> {
    sampled(1.0, |_, _| value)
}

pub fn pcr_jitter_ms() -> Vec<[f64; 2]> {
    sampled(0.2, |index, _| {
        let i = index as f64;
        0.006 * (i * 1.7).sin() + 0.003 * (i * 0.31).sin() + 0.0008 * (i * 7.1).sin()
    })
}

/// Jitter histogram: (bin center in ms, interval count).
pub const JITTER_HISTOGRAM: [(f64, f64); 9] = [
    (-0.008, 6.0),
    (-0.006, 22.0),
    (-0.004, 61.0),
    (-0.002, 118.0),
    (0.0, 152.0),
    (0.002, 121.0),
    (0.004, 58.0),
    (0.006, 20.0),
    (0.008, 5.0),
];
