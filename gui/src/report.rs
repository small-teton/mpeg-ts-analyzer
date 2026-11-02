//! Types for the CLI's JSON documents: `--output analysis`
//! (docs/json-schema-analysis-v1.json) and `--capabilities`. Optional and
//! unknown fields are tolerated, matching the schemas' additive-change policy.

use serde::Deserialize;

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct Capabilities {
    pub cli_version: String,
    pub outputs: Vec<CapabilityOutput>,
    pub progress_formats: Vec<String>,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct CapabilityOutput {
    pub name: String,
    pub schema_version: String,
}

impl Capabilities {
    /// Whether the CLI supports an output with a schema of the given major version.
    pub fn supports(&self, name: &str, major: &str) -> bool {
        self.outputs
            .iter()
            .any(|o| o.name == name && o.schema_version.split('.').next() == Some(major))
    }
}

/// One progress event from `--progress json` on stderr.
#[derive(Clone, Debug, Default, Deserialize, PartialEq)]
#[serde(default)]
pub struct ProgressEvent {
    #[serde(rename = "type")]
    pub kind: String,
    pub processed_bytes: u64,
    pub total_bytes: u64,
    pub done: bool,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct AnalysisReport {
    pub schema_version: String,
    pub output: String,
    pub input: Input,
    pub status: String,
    pub reason: Option<String>,
    pub error: Option<String>,
    pub analysis_start_offset_bytes: Option<u64>,
    pub analysis_end_offset_bytes: Option<u64>,
    pub continuity_counter: Option<Continuity>,
    pub continuity_events: Vec<ContinuityEvent>,
    pub programs: Vec<Program>,
    pub warnings: Vec<Warning>,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct Input {
    pub path: String,
    pub file_size_bytes: u64,
    pub packet_size_bytes: Option<u32>,
    pub offset_bytes: u64,
    pub limit_bytes: Option<u64>,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct Continuity {
    pub status: String,
    pub total_error_count: u64,
    pub pids: Vec<ContinuityPid>,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct ContinuityPid {
    pub pid: String,
    pub label: String,
    pub error_count: u64,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct ContinuityEvent {
    pub offset_bytes: u64,
    pub pid: String,
    pub expected_cc: u8,
    pub actual_cc: u8,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct Warning {
    pub offset_bytes: u64,
    pub message: String,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct Program {
    pub program_number: u16,
    pub pmt_pid: String,
    pub pcr_pid: Option<String>,
    pub elementary_streams: Vec<ElementaryStream>,
    pub status: String,
    pub reason: Option<String>,
    pub error: Option<String>,
    pub analysis_start_offset_bytes: Option<u64>,
    pub average_bitrate_bps: Option<f64>,
    pub compliance: Option<Compliance>,
    pub timestamp_anomalies: Option<TimestampAnomalies>,
    pub pcr_jitter: Option<PcrJitter>,
    pub bitrate: Option<Bitrate>,
    pub series: Option<Series>,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct ElementaryStream {
    pub pid: String,
    pub stream_type: String,
    pub stream_type_description: String,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct Compliance {
    pub max_pcr_interval: Check,
    pub max_pcr_pts_dts_gap: Check,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct Check {
    pub status: String,
    pub value_ms: Option<f64>,
    pub limit_ms: f64,
    pub observations: u64,
    pub reason: Option<String>,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct TimestampAnomalies {
    pub status: String,
    pub items: Vec<TimestampAnomaly>,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct TimestampAnomaly {
    pub pid: String,
    pub kind: String,
    pub count: u64,
    pub message: String,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct PcrJitter {
    pub status: String,
    pub reason: Option<String>,
    pub max_jitter_ns: Option<f64>,
    pub min_jitter_ns: Option<f64>,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct Bitrate {
    pub status: String,
    pub reason: Option<String>,
    pub duration_seconds: f64,
    pub pids: Vec<BitratePid>,
    pub null_pid: Option<BitratePid>,
    pub program_average_bps: f64,
    pub program_peak_1s_bps: Option<f64>,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct BitratePid {
    pub pid: String,
    pub label: String,
    pub average_bps: f64,
    pub peak_1s_bps: Option<f64>,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct Series {
    pub pcr: Vec<PcrPoint>,
    pub pes: Vec<PesPoint>,
    pub timestamp_events: Vec<TimestampEvent>,
    pub pcr_jitter: Vec<JitterPoint>,
    pub bitrate: BitrateWindows,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct PcrPoint {
    pub offset_bytes: u64,
    pub elapsed_ms: f64,
    pub pcr_ms: f64,
    pub interval_ms: Option<f64>,
    pub discontinuity: bool,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct PesPoint {
    pub pid: String,
    pub offset_bytes: u64,
    pub elapsed_ms: Option<f64>,
    pub pts_ms: Option<f64>,
    pub dts_ms: Option<f64>,
    pub pcr_gap_ms: Option<f64>,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct TimestampEvent {
    pub pid: String,
    pub offset_bytes: u64,
    pub kind: String,
    pub message: String,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct JitterPoint {
    pub offset_bytes: u64,
    pub jitter_ns: f64,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct BitrateWindows {
    pub window_seconds: u32,
    /// Length of each window; the last one is shorter when the measured
    /// duration is not a whole number of seconds.
    pub duration_seconds: Vec<f64>,
    pub pids: Vec<BitrateWindowPid>,
}

#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct BitrateWindowPid {
    pub pid: String,
    pub label: String,
    pub bytes: Vec<u64>,
    pub bps: Vec<f64>,
}

/// Short codec name for a PMT stream_type ("0x1b").
pub fn stream_type_name(stream_type: &str) -> String {
    let value = u8::from_str_radix(stream_type.trim_start_matches("0x"), 16).ok();
    match value {
        Some(0x01) => "MPEG-1 video",
        Some(0x02) => "MPEG-2 video",
        Some(0x03) => "MPEG-1 audio",
        Some(0x04) => "MPEG-2 audio",
        Some(0x06) => "private data",
        Some(0x0F) => "AAC audio",
        Some(0x11) => "AAC (LATM) audio",
        Some(0x1B) => "H.264 video",
        Some(0x24) => "HEVC video",
        Some(0x81) => "AC-3 audio",
        Some(0x87) => "E-AC-3 audio",
        _ => return format!("stream_type {stream_type}"),
    }
    .to_owned()
}

impl Program {
    /// What the program carries, from its PMT stream types.
    pub fn description(&self) -> String {
        let mut parts: Vec<String> = Vec::new();
        for stream in &self.elementary_streams {
            let name = stream_type_name(&stream.stream_type);
            if !parts.contains(&name) {
                parts.push(name);
            }
        }
        if parts.is_empty() {
            "no streams".to_owned()
        } else {
            parts.join(" + ")
        }
    }

    /// Label used by the program selector.
    pub fn label(&self) -> String {
        match self.average_bitrate_bps {
            Some(bps) => format!(
                "{} · {} · {:.2} Mbps (PMT {})",
                self.program_number,
                self.description(),
                bps / 1e6,
                self.pmt_pid
            ),
            None => format!(
                "{} · {} (PMT {})",
                self.program_number,
                self.description(),
                self.pmt_pid
            ),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_a_two_program_analysis() {
        let report: AnalysisReport = serde_json::from_str(include_str!(
            "../tests/fixtures/analysis_two_programs_188.json"
        ))
        .expect("fixture parses");
        assert_eq!(report.status, "complete");
        assert_eq!(report.input.packet_size_bytes, Some(188));
        assert_eq!(report.programs.len(), 2);
        let program = &report.programs[0];
        assert_eq!(program.description(), "MPEG-2 video + MPEG-1 audio");
        assert!(
            program
                .label()
                .starts_with("1 · MPEG-2 video + MPEG-1 audio · 0.32 Mbps")
        );
        let series = program.series.as_ref().expect("series");
        assert!(!series.pcr.is_empty() && !series.pes.is_empty());
        assert_eq!(series.bitrate.window_seconds, 1);
        assert_eq!(series.bitrate.duration_seconds, [1.0, 0.76]);
        let video = &series.bitrate.pids[1];
        assert_eq!(video.bytes.len(), 2);
        assert_eq!(video.bps.len(), 2);
        assert!(
            program
                .compliance
                .as_ref()
                .unwrap()
                .max_pcr_interval
                .value_ms
                .is_some()
        );
    }

    #[test]
    fn parses_a_failed_analysis() {
        let report: AnalysisReport =
            serde_json::from_str(include_str!("../tests/fixtures/analysis_failed.json"))
                .expect("fixture parses");
        assert_eq!(report.status, "failed");
        assert!(report.error.is_some() && report.programs.is_empty());
    }

    #[test]
    fn checks_capabilities() {
        let caps: Capabilities =
            serde_json::from_str(include_str!("../tests/fixtures/capabilities.json"))
                .expect("fixture parses");
        assert!(caps.supports("analysis", "1"));
        assert!(!caps.supports("analysis", "2"));
        assert!(!caps.supports("timeline", "1"));
        assert_eq!(caps.progress_formats, ["json"]);
    }

    #[test]
    fn parses_progress_events() {
        let event: ProgressEvent =
            serde_json::from_str(r#"{"type":"progress","processed_bytes":10,"total_bytes":20}"#)
                .unwrap();
        assert_eq!(event.kind, "progress");
        assert_eq!(
            (event.processed_bytes, event.total_bytes, event.done),
            (10, 20, false)
        );
    }

    #[test]
    fn names_stream_types() {
        assert_eq!(stream_type_name("0x1b"), "H.264 video");
        assert_eq!(stream_type_name("0x99"), "stream_type 0x99");
    }
}
