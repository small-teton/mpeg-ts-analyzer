//! Types for the CLI's JSON documents: `--output analysis`
//! (docs/json-schema-analysis-v1.json) and `--capabilities`. Fields the
//! schemas require are required here too, so a malformed document is rejected;
//! unknown fields are ignored, matching the schemas' additive-change policy.

// The types mirror the schemas in full; fields the Overview does not show yet
// are read by the Timeline, Bitrate and Packets screens as they move to real data.
#![allow(dead_code)]

use serde::Deserialize;

#[derive(Debug, Deserialize)]
pub struct Capabilities {
    pub cli_version: String,
    pub outputs: Vec<CapabilityOutput>,
    pub progress_formats: Vec<String>,
}

#[derive(Debug, Deserialize)]
pub struct CapabilityOutput {
    pub name: String,
    pub format: String,
    pub schema_version: String,
}

impl Capabilities {
    /// Whether the CLI writes the named output as JSON with a schema of the
    /// given major version.
    pub fn supports_json(&self, name: &str, major: &str) -> bool {
        self.outputs.iter().any(|o| {
            o.name == name && o.format == "json" && schema_major(&o.schema_version) == Some(major)
        })
    }

    pub fn supports_progress(&self, format: &str) -> bool {
        self.progress_formats.iter().any(|f| f == format)
    }
}

fn schema_major(version: &str) -> Option<&str> {
    version.split('.').next()
}

/// Parses an `--output analysis` document and checks that it is one this GUI
/// reads: the analysis output with a schema of the given major version.
pub fn parse_analysis(json: &[u8], major: &str) -> Result<AnalysisReport, String> {
    let report: AnalysisReport =
        serde_json::from_slice(json).map_err(|err| format!("Unreadable analysis output: {err}"))?;
    if report.output != "analysis" || schema_major(&report.schema_version) != Some(major) {
        return Err(format!(
            "Unsupported analysis document (output {:?}, schema {:?}); a CLI with analysis schema {major}.x is needed.",
            report.output, report.schema_version
        ));
    }
    Ok(report)
}

/// One progress event from `--progress json` on stderr.
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct ProgressEvent {
    #[serde(rename = "type")]
    pub kind: String,
    pub processed_bytes: u64,
    pub total_bytes: u64,
    #[serde(default)]
    pub done: bool,
}

#[derive(Debug, Deserialize)]
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

#[derive(Debug, Deserialize)]
pub struct Input {
    pub path: String,
    pub file_size_bytes: u64,
    pub packet_size_bytes: Option<u32>,
    pub offset_bytes: u64,
    pub limit_bytes: Option<u64>,
}

#[derive(Debug, Deserialize)]
pub struct Continuity {
    pub status: String,
    pub total_error_count: u64,
    pub pids: Vec<ContinuityPid>,
}

#[derive(Debug, Deserialize)]
pub struct ContinuityPid {
    pub pid: String,
    pub label: String,
    pub error_count: u64,
}

#[derive(Debug, Deserialize)]
pub struct ContinuityEvent {
    pub offset_bytes: u64,
    pub pid: String,
    pub expected_cc: u8,
    pub actual_cc: u8,
}

#[derive(Debug, Deserialize)]
pub struct Warning {
    pub offset_bytes: u64,
    pub message: String,
}

#[derive(Debug, Deserialize)]
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

#[derive(Debug, Deserialize)]
pub struct ElementaryStream {
    pub pid: String,
    pub stream_type: String,
    pub stream_type_description: String,
}

#[derive(Debug, Deserialize)]
pub struct Compliance {
    pub max_pcr_interval: Check,
    pub max_pcr_pts_dts_gap: Check,
}

#[derive(Debug, Deserialize)]
pub struct Check {
    pub status: String,
    pub value_ms: Option<f64>,
    pub limit_ms: f64,
    pub observations: u64,
    pub reason: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct TimestampAnomalies {
    pub status: String,
    pub items: Vec<TimestampAnomaly>,
}

#[derive(Debug, Deserialize)]
pub struct TimestampAnomaly {
    pub pid: String,
    pub kind: String,
    pub count: u64,
    pub message: String,
}

#[derive(Debug, Deserialize)]
pub struct PcrJitter {
    pub status: String,
    pub reason: Option<String>,
    pub max_jitter_ns: Option<f64>,
    pub min_jitter_ns: Option<f64>,
}

#[derive(Debug, Deserialize)]
pub struct Bitrate {
    pub status: String,
    pub reason: Option<String>,
    pub duration_seconds: f64,
    pub pids: Vec<BitratePid>,
    pub null_pid: Option<BitratePid>,
    pub program_average_bps: f64,
    pub program_peak_1s_bps: Option<f64>,
}

#[derive(Debug, Deserialize)]
pub struct BitratePid {
    pub pid: String,
    pub label: String,
    pub average_bps: f64,
    pub peak_1s_bps: Option<f64>,
}

#[derive(Debug, Deserialize)]
pub struct Series {
    pub pcr: Vec<PcrPoint>,
    pub pes: Vec<PesPoint>,
    pub timestamp_events: Vec<TimestampEvent>,
    pub pcr_jitter: Vec<JitterPoint>,
    pub bitrate: BitrateWindows,
}

#[derive(Debug, Deserialize)]
pub struct PcrPoint {
    pub offset_bytes: u64,
    pub elapsed_ms: f64,
    pub pcr_ms: f64,
    pub interval_ms: Option<f64>,
    #[serde(default)]
    pub discontinuity: bool,
}

#[derive(Debug, Deserialize)]
pub struct PesPoint {
    pub pid: String,
    pub offset_bytes: u64,
    pub elapsed_ms: Option<f64>,
    pub pts_ms: Option<f64>,
    pub dts_ms: Option<f64>,
    pub pcr_gap_ms: Option<f64>,
}

#[derive(Debug, Deserialize)]
pub struct TimestampEvent {
    pub pid: String,
    pub offset_bytes: u64,
    pub kind: String,
    pub message: String,
}

#[derive(Debug, Deserialize)]
pub struct JitterPoint {
    pub offset_bytes: u64,
    pub jitter_ns: f64,
}

#[derive(Debug, Deserialize)]
pub struct BitrateWindows {
    pub window_seconds: u32,
    /// Length of each window; the last one is shorter when the measured
    /// duration is not a whole number of seconds.
    pub duration_seconds: Vec<f64>,
    pub pids: Vec<BitrateWindowPid>,
}

#[derive(Debug, Deserialize)]
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
    fn rejects_malformed_or_unsupported_analysis_documents() {
        let valid = include_str!("../tests/fixtures/analysis_two_programs_188.json");
        assert!(parse_analysis(valid.as_bytes(), "1").is_ok());
        let mut missing: serde_json::Value = serde_json::from_str(valid).unwrap();
        missing.as_object_mut().unwrap().remove("programs");
        let mut other_output: serde_json::Value = serde_json::from_str(valid).unwrap();
        other_output["output"] = "summary".into();
        let mut schema_2: serde_json::Value = serde_json::from_str(valid).unwrap();
        schema_2["schema_version"] = "2.0".into();
        let mut input_missing: serde_json::Value = serde_json::from_str(valid).unwrap();
        input_missing["input"]
            .as_object_mut()
            .unwrap()
            .remove("file_size_bytes");
        for (name, document, expected) in [
            ("empty object", "{}".to_owned(), "Unreadable"),
            ("missing programs", missing.to_string(), "Unreadable"),
            (
                "missing input field",
                input_missing.to_string(),
                "Unreadable",
            ),
            ("summary output", other_output.to_string(), "Unsupported"),
            ("schema 2.0", schema_2.to_string(), "Unsupported"),
        ] {
            let err = parse_analysis(document.as_bytes(), "1")
                .err()
                .unwrap_or_else(|| panic!("{name} was accepted"));
            assert!(err.contains(expected), "{name}: {err}");
        }
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
        assert!(caps.supports_json("analysis", "1"));
        assert!(!caps.supports_json("analysis", "2"));
        assert!(!caps.supports_json("timeline", "1"));
        assert!(caps.supports_progress("json"));
        assert!(!caps.supports_progress("xml"));
        let text_only: Capabilities = serde_json::from_str(
            r#"{"capabilities_version":"1.0","cli_version":"x","outputs":[{"name":"analysis","format":"text","schema_version":"1.0"}],"progress_formats":[]}"#,
        )
        .unwrap();
        assert!(!text_only.supports_json("analysis", "1"));
        assert!(!text_only.supports_progress("json"));
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
