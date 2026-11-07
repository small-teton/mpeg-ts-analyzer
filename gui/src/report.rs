//! Types for the CLI's JSON documents: `--output analysis`
//! (docs/json-schema-analysis-v2.json) and `--capabilities`. Fields the
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

/// The fields that identify a document, picked out of output that did not
/// parse as a whole, so that a document of another schema version is reported
/// as such rather than as malformed. Only these two values are kept; the rest
/// is skipped, and a document cut short (such as the start of a streamed one)
/// still yields what came before the cut.
#[derive(Default)]
struct EnvelopeProbe {
    output: Option<String>,
    schema_version: Option<String>,
}

impl<'de> serde::de::Visitor<'de> for &mut EnvelopeProbe {
    type Value = ();

    fn expecting(&self, formatter: &mut std::fmt::Formatter) -> std::fmt::Result {
        formatter.write_str("a JSON object")
    }

    fn visit_map<A: serde::de::MapAccess<'de>>(self, mut map: A) -> Result<(), A::Error> {
        while let Some(key) = map.next_key::<String>()? {
            match key.as_str() {
                "output" => self.output = Some(map.next_value()?),
                "schema_version" => self.schema_version = Some(map.next_value()?),
                _ => {
                    map.next_value::<serde::de::IgnoredAny>()?;
                }
            }
            if self.output.is_some() && self.schema_version.is_some() {
                // Nothing more is needed; stop reading.
                return Err(serde::de::Error::custom("envelope found"));
            }
        }
        Ok(())
    }
}

fn check_analysis_envelope(output: &str, schema_version: &str, major: &str) -> Result<(), String> {
    if output != "analysis" || schema_major(schema_version) != Some(major) {
        return Err(format!(
            "Unsupported analysis document (output {output:?}, schema {schema_version:?}); this GUI reads analysis schema {major}.x. Update the mpeg-ts-analyzer CLI to a version that writes it."
        ));
    }
    Ok(())
}

/// For analysis output that did not parse: the error for a document of
/// another output or schema version, when the start of the output (`json`,
/// possibly cut short) says it is one.
pub fn unsupported_analysis(json: &[u8], major: &str) -> Option<String> {
    use serde::Deserializer as _;
    let mut probe = EnvelopeProbe::default();
    let _ = serde_json::Deserializer::from_slice(json).deserialize_map(&mut probe);
    check_analysis_envelope(&probe.output?, &probe.schema_version?, major).err()
}

/// Parses an `--output analysis` document as it is read, without holding the
/// raw document, and checks that it is one this GUI reads: the analysis
/// output with a schema of the given major version.
pub fn read_analysis(json: impl std::io::Read, major: &str) -> Result<AnalysisReport, String> {
    let report: AnalysisReport = serde_json::from_reader(json)
        .map_err(|err| format!("Unreadable analysis output: {err}"))?;
    check_analysis_envelope(&report.output, &report.schema_version, major)?;
    Ok(report)
}

/// [`read_analysis`] of a document in memory; a document of another schema
/// version is reported as such even when it does not parse as this one.
pub fn parse_analysis(json: &[u8], major: &str) -> Result<AnalysisReport, String> {
    read_analysis(json, major).map_err(|err| unsupported_analysis(json, major).unwrap_or(err))
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
    /// TS synchronization of the scan; absent before the CLI reported it and
    /// when no program was analyzed.
    #[serde(default)]
    pub sync: Option<Sync>,
    pub continuity_counter: Option<Continuity>,
    pub continuity_events: Vec<ContinuityEvent>,
    /// Packets with transport_error_indicator set; absent from a CLI without
    /// the check and when no program was analyzed.
    #[serde(default)]
    pub transport_errors: Option<AffectedPackets>,
    /// Packets with a non-zero transport_scrambling_control; absent as
    /// `transport_errors` is.
    #[serde(default)]
    pub scrambled_packets: Option<AffectedPackets>,
    /// PAT and PMT availability and repetition; absent as `transport_errors`
    /// is.
    #[serde(default)]
    pub psi_health: Option<PsiHealth>,
    /// PES header checks of every elementary stream; absent as
    /// `transport_errors` is.
    #[serde(default)]
    pub pes_headers: Option<PesHeaders>,
    /// Intervals between PES packets carrying a PTS; absent as
    /// `transport_errors` is.
    #[serde(default)]
    pub pts_repetition: Option<PtsRepetition>,
    /// Referenced, missing and unreferenced PIDs; absent as
    /// `transport_errors` is.
    #[serde(default)]
    pub pid_references: Option<PidReferences>,
    /// Report-only multiplexing diagnostics; absent as `transport_errors` is.
    #[serde(default)]
    pub multiplexing: Option<Multiplexing>,
    pub programs: Vec<Program>,
    pub warnings: Vec<Warning>,
    /// Absent before CLI schema 1.x gained it, and when no program was analyzed.
    #[serde(default)]
    pub psi_layout: Option<PsiLayout>,
    /// Absent when no PAT or PMT change was applied.
    #[serde(default)]
    pub psi_transitions: Vec<PsiTransition>,
}

/// `psi_layout`: what each PID carries at any offset of the range
/// (docs/reference/json-output.md, "PSI layout of the range").
#[derive(Clone, Debug, Deserialize)]
pub struct PsiLayout {
    pub sections: Vec<PsiSection>,
    pub layouts: Vec<Layout>,
    pub pids: Vec<LayoutPid>,
}

/// A run of identical PAT or PMT section copies.
#[derive(Clone, Debug, Deserialize)]
pub struct PsiSection {
    pub offset_bytes: u64,
    pub last_offset_bytes: u64,
    pub count: u64,
    pub pid: String,
    pub table: String,
    pub status: String,
    pub invalid_reason: Option<String>,
    pub transport_stream_id: Option<u16>,
    pub program_number: Option<u16>,
    pub version_number: Option<u8>,
    pub section_number: Option<u8>,
    pub last_section_number: Option<u8>,
    pub current_next_indicator: Option<u8>,
}

/// The tables in effect for the packets in `[offset_bytes, end_offset_bytes)`.
#[derive(Clone, Debug, Deserialize)]
pub struct Layout {
    pub offset_bytes: u64,
    pub end_offset_bytes: u64,
    pub transition_indices: Vec<usize>,
    pub programs: Vec<LayoutProgram>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct LayoutProgram {
    pub program_number: u16,
    pub pmt_pid: String,
    pub pcr_pid: Option<String>,
    pub elementary_streams: Vec<ElementaryStream>,
}

/// A PID seen in the range.
#[derive(Clone, Debug, Deserialize)]
pub struct LayoutPid {
    pub pid: String,
    pub roles: Vec<PidRole>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct PidRole {
    pub kind: String,
    pub program_number: Option<u16>,
    pub stream_type: Option<String>,
}

/// One applied PAT or PMT change; the new layout applies after the packet at
/// `offset_bytes`, which completed the new table.
#[derive(Clone, Debug, Deserialize)]
pub struct PsiTransition {
    pub offset_bytes: u64,
    pub table: String,
    pub pid: String,
    pub previous_version: u8,
    pub version: u8,
    pub program_number: Option<u16>,
    pub previous_pcr_pid: Option<String>,
    pub pcr_pid: Option<String>,
    #[serde(default)]
    pub elementary_streams_added: Vec<ElementaryStream>,
    #[serde(default)]
    pub elementary_streams_removed: Vec<ElementaryStream>,
    #[serde(default)]
    pub stream_type_changes: Vec<StreamTypeChange>,
    #[serde(default)]
    pub programs_added: Vec<PatProgram>,
    #[serde(default)]
    pub programs_removed: Vec<PatProgram>,
    #[serde(default)]
    pub pmt_pid_changes: Vec<PmtPidChange>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct StreamTypeChange {
    pub pid: String,
    pub previous_stream_type: String,
    pub stream_type: String,
}

#[derive(Clone, Debug, Deserialize)]
pub struct PatProgram {
    pub program_number: u16,
    pub pmt_pid: String,
}

#[derive(Clone, Debug, Deserialize)]
pub struct PmtPidChange {
    pub program_number: u16,
    pub previous_pmt_pid: String,
    pub pmt_pid: String,
}

#[derive(Debug, Deserialize)]
pub struct Input {
    pub path: String,
    pub file_size_bytes: u64,
    pub packet_size_bytes: Option<u32>,
    pub offset_bytes: u64,
    pub limit_bytes: Option<u64>,
}

/// `sync`: packet boundaries without a sync byte and the realignment after
/// each (docs/reference/json-output.md, "TS synchronization").
#[derive(Debug, Deserialize)]
pub struct Sync {
    pub status: String,
    pub reason: Option<String>,
    pub required_sync_bytes: u64,
    pub max_resync_scan_bytes: u64,
    pub loss_count: u64,
    pub resync_count: u64,
    pub skipped_bytes: u64,
    pub first_loss_offset_bytes: Option<u64>,
    pub first_resync_offset_bytes: Option<u64>,
    pub max_skipped_bytes: Option<u64>,
    pub max_skipped_offset_bytes: Option<u64>,
    pub unrecovered_offset_bytes: Option<u64>,
    pub trailing_partial_packet_bytes: u64,
    pub trailing_partial_packet_offset_bytes: Option<u64>,
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

/// The CLI's `transport_errors` or `scrambled_packets` result.
#[derive(Debug, Deserialize)]
pub struct AffectedPackets {
    pub status: String,
    pub packet_count: u64,
    pub excluded_packet_count: u64,
    pub first_offset_bytes: Option<u64>,
    pub pids: Vec<AffectedPid>,
}

#[derive(Debug, Deserialize)]
pub struct AffectedPid {
    pub pid: String,
    pub label: String,
    pub packet_count: u64,
    pub excluded_packet_count: u64,
    pub first_offset_bytes: u64,
}

/// The CLI's `pes_headers` result: the totals and every elementary PID.
#[derive(Debug, Deserialize)]
pub struct PesHeaders {
    #[serde(flatten)]
    pub counts: PesHeaderCounts,
    pub pids: Vec<PesHeaderPid>,
}

/// The header counts of one PID or of all PIDs.
#[derive(Debug, Deserialize)]
pub struct PesHeaderCounts {
    pub status: String,
    pub reason: Option<String>,
    pub checked_count: u64,
    pub error_count: u64,
    pub unchecked_count: u64,
    pub first_error_offset_bytes: Option<u64>,
    pub errors: Vec<PesHeaderError>,
}

#[derive(Debug, Deserialize)]
pub struct PesHeaderError {
    pub kind: String,
    pub count: u64,
    pub first_offset_bytes: u64,
}

#[derive(Debug, Deserialize)]
pub struct PesHeaderPid {
    #[serde(flatten)]
    pub stream: PesStream,
    #[serde(flatten)]
    pub counts: PesHeaderCounts,
}

/// An elementary stream of a program in the PES results.
#[derive(Debug, Deserialize)]
pub struct PesStream {
    pub program_number: u16,
    pub pid: String,
    pub stream_type: String,
    pub label: String,
}

impl PesStream {
    /// "Program 1 PID 0x0100 (video)".
    pub fn describe(&self) -> String {
        format!(
            "Program {} PID {} ({})",
            self.program_number, self.pid, self.label
        )
    }
}

/// The CLI's `pts_repetition` result: the limit, the totals and every
/// elementary PID.
#[derive(Debug, Deserialize)]
pub struct PtsRepetition {
    pub limit_ms: f64,
    #[serde(flatten)]
    pub counts: PtsRepetitionCounts,
    pub pids: Vec<PtsRepetitionPid>,
}

/// The PTS repetition values of one PID or of all PIDs.
#[derive(Debug, Deserialize)]
pub struct PtsRepetitionCounts {
    pub status: String,
    pub reason: Option<String>,
    pub pts_count: u64,
    pub interval_count: u64,
    pub max_interval_ms: Option<f64>,
    pub max_interval_start_offset_bytes: Option<u64>,
    pub max_interval_end_offset_bytes: Option<u64>,
    pub exceeded_count: u64,
    pub first_exceeded_offset_bytes: Option<u64>,
}

#[derive(Debug, Deserialize)]
pub struct PtsRepetitionPid {
    #[serde(flatten)]
    pub stream: PesStream,
    #[serde(flatten)]
    pub counts: PtsRepetitionCounts,
}

/// The CLI's `psi_health` result.
#[derive(Debug, Deserialize)]
pub struct PsiHealth {
    pub status: String,
    pub repetition_limit_ms: f64,
    pub time_base: PsiTimeBase,
    pub tables: Vec<PsiTableHealth>,
}

impl PsiHealth {
    /// The PAT, or the PMT of `program_number`.
    pub fn table(&self, program_number: Option<u16>) -> Option<&PsiTableHealth> {
        self.tables
            .iter()
            .find(|t| t.program_number == program_number)
    }
}

#[derive(Debug, Deserialize)]
pub struct PsiTimeBase {
    pub status: String,
    pub program_number: u16,
    pub pcr_pid: Option<String>,
    pub segment_count: u64,
}

/// The health of the PAT (no `program_number`) or of one program's PMT.
#[derive(Debug, Deserialize)]
pub struct PsiTableHealth {
    pub table: String,
    pub program_number: Option<u16>,
    pub pid: String,
    pub status: String,
    pub reasons: Vec<String>,
    pub instance_count: u64,
    pub version_change_count: u64,
    pub crc_error_count: u64,
    pub malformed_section_count: u64,
    pub incomplete_count: u64,
    pub wrong_table_id_count: u64,
    pub unusable_packet_count: u64,
    pub first_problem_offset_bytes: Option<u64>,
    pub repetition: PsiRepetition,
}

#[derive(Debug, Deserialize)]
pub struct PsiRepetition {
    pub status: String,
    pub reason: Option<String>,
    pub interval_count: u64,
    pub max_interval_ms: Option<f64>,
    pub max_interval_end_offset_bytes: Option<u64>,
    pub late_interval_count: u64,
    pub first_late_end_offset_bytes: Option<u64>,
}

/// The CLI's `pid_references` result.
#[derive(Debug, Deserialize)]
pub struct PidReferences {
    pub silence_limit_ms: f64,
    pub unreferenced_grace_ms: f64,
    pub referenced: ReferencedPids,
    pub unreferenced: UnreferencedPids,
}

#[derive(Debug, Deserialize)]
pub struct ReferencedPids {
    pub status: String,
    pub pids: Vec<ReferencedPid>,
}

/// One program, role and PID of the PSI layouts.
#[derive(Debug, Deserialize)]
pub struct ReferencedPid {
    pub program_number: u16,
    pub role: String,
    pub pid: String,
    pub stream_type: Option<String>,
    pub status: String,
    pub reason: Option<String>,
    pub max_silence_ms: Option<f64>,
    pub max_silence_start_offset_bytes: Option<u64>,
    pub referenced_from_offset_bytes: u64,
}

#[derive(Debug, Deserialize)]
pub struct UnreferencedPids {
    pub status: String,
    pub reason: Option<String>,
    pub pids: Vec<UnreferencedPid>,
}

/// A PID that carried packets no layout referenced.
#[derive(Debug, Deserialize)]
pub struct UnreferencedPid {
    pub pid: String,
    pub status: String,
    pub packet_count: u64,
    pub first_offset_bytes: u64,
    pub longest_run_ms: f64,
}

/// The CLI's `multiplexing` result: report-only diagnostics without a verdict.
#[derive(Debug, Deserialize)]
pub struct Multiplexing {
    pub packet_gaps: PacketGaps,
}

/// The largest packet gap of every non-NULL PID of the range.
#[derive(Debug, Deserialize)]
pub struct PacketGaps {
    pub pids: Vec<PacketGapPid>,
}

/// The largest gap between two consecutive packets of one PID.
#[derive(Debug, Deserialize)]
pub struct PacketGapPid {
    pub pid: String,
    /// The programs and roles that reference the PID; empty for PSI/SI PIDs
    /// and PIDs no layout references.
    pub references: Vec<PidReference>,
    pub packet_count: u64,
    pub status: String,
    pub reason: Option<String>,
    pub max_gap_packets: Option<u64>,
    pub max_gap_start_offset_bytes: Option<u64>,
    pub max_gap_end_offset_bytes: Option<u64>,
    pub max_gap_ms: Option<f64>,
    pub max_gap_exact: Option<bool>,
}

/// A program and role under which a layout referenced a PID.
#[derive(Debug, Deserialize)]
pub struct PidReference {
    pub program_number: u16,
    pub role: String,
    pub stream_type: Option<String>,
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
    pub service: Option<Service>,
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

/// The program's service from the SDT. A name the CLI cannot decode comes as
/// hex in `name_hex` instead of `name`.
#[derive(Debug, Default, Deserialize)]
pub struct Service {
    pub service_type_description: Option<String>,
    pub provider_name: Option<String>,
    pub provider_name_hex: Option<String>,
    pub name: Option<String>,
    pub name_hex: Option<String>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct ElementaryStream {
    pub pid: String,
    pub stream_type: String,
    pub stream_type_description: String,
}

#[derive(Debug, Deserialize)]
pub struct Compliance {
    pub max_pcr_interval: Check,
    /// Added within schema 2.0; absent from an earlier 2.0 CLI.
    #[serde(default)]
    pub pcr_discontinuities: Option<PcrDiscontinuities>,
    pub max_pcr_pts_dts_gap: Check,
}

/// The PCR discontinuity check: how each PCR of the program relates to the
/// previous one. `ng` when a step has no discontinuity_indicator (`backward`
/// or `forward_jump`), `skipped` with fewer than two PCRs.
#[derive(Debug, Deserialize)]
pub struct PcrDiscontinuities {
    pub status: String,
    pub reason: Option<String>,
    pub pcr_count: u64,
    pub undeclared_count: u64,
    pub jump_limit_ms: f64,
    pub items: Vec<PcrStep>,
}

/// How often one kind of PCR step occurred, and the first such PCR packet.
#[derive(Debug, Deserialize)]
pub struct PcrStep {
    pub kind: String,
    pub count: u64,
    pub first_offset_bytes: u64,
    pub first_jump_ms: f64,
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

/// PCR jitter: an estimate, the delivery assessment that decides whether the
/// CLI grades it, and the CLI's status (`ok`, `warning`, `ng`,
/// `not_assessable` or `skipped`).
#[derive(Debug, Deserialize)]
pub struct PcrJitter {
    pub status: String,
    pub reason: Option<String>,
    pub estimate: JitterEstimate,
    pub delivery: Delivery,
}

/// The jitter estimated with one model (`byte_position`). The values are
/// absent when no estimate could be made.
#[derive(Debug, Deserialize)]
pub struct JitterEstimate {
    pub model: String,
    pub pcr_pids: Vec<String>,
    pub sample_count: u64,
    pub segment_count: u64,
    pub observations: u64,
    pub max_abs_jitter_ns: Option<f64>,
    pub max_abs_jitter_offset_bytes: Option<u64>,
    pub max_jitter_ns: Option<f64>,
    pub max_jitter_offset_bytes: Option<u64>,
    pub min_jitter_ns: Option<f64>,
    pub min_jitter_offset_bytes: Option<u64>,
    pub average_absolute_jitter_ns: Option<f64>,
    pub within_500ns_percent: Option<f64>,
    pub discontinuities: Vec<PcrDiscontinuity>,
}

/// Whether the stream shows the constant delivery the byte-position model
/// needs (`constant`, `variable` or `unknown`), with the multiplex rates of
/// its 1-second windows as evidence.
#[derive(Debug, Deserialize)]
pub struct Delivery {
    pub assessment: String,
    pub reason: String,
    pub window_seconds: u32,
    pub window_count: u64,
    pub average_rate_bps: Option<f64>,
    pub min_window_rate_bps: Option<f64>,
    pub max_window_rate_bps: Option<f64>,
    pub max_deviation_percent: Option<f64>,
    pub tolerance_percent: f64,
    pub plausible_jitter_limit_ns: f64,
}

impl Delivery {
    /// The assessment and its window evidence in one line, such as
    /// `delivery constant: 2 × 1 s windows, max deviation 0.00 % of 75.2 kbit/s`.
    pub fn summary(&self) -> String {
        format!("delivery {}: {}", self.assessment, self.evidence())
    }

    /// The window evidence alone, such as
    /// `2 × 1 s windows, max deviation 0.00 % of 75.2 kbit/s`.
    pub fn evidence(&self) -> String {
        let windows = match self.window_count {
            1 => format!("1 × {} s window", self.window_seconds),
            n => format!("{n} × {} s windows", self.window_seconds),
        };
        match (self.max_deviation_percent, self.average_rate_bps) {
            (Some(deviation), Some(rate)) => {
                format!(
                    "{windows}, max deviation {deviation:.2} % of {}",
                    rate_text(rate)
                )
            }
            (None, Some(rate)) => format!("{windows} at {}", rate_text(rate)),
            _ => format!("no {} s window", self.window_seconds),
        }
    }
}

/// A bit rate in the unit that keeps it short.
pub fn rate_text(bps: f64) -> String {
    if bps < 1e6 {
        format!("{:.1} kbit/s", bps / 1e3)
    } else {
        format!("{:.2} Mbit/s", bps / 1e6)
    }
}

#[derive(Debug, Deserialize)]
pub struct PcrDiscontinuity {
    pub offset_bytes: u64,
    /// `declared`, `pcr_pid_change`, `backward` or `forward_jump`; absent
    /// from an earlier 2.0 CLI.
    #[serde(default)]
    pub kind: Option<String>,
    pub jump_ms: f64,
    pub from_segment: u64,
    pub to_segment: u64,
}

#[derive(Debug, Deserialize)]
pub struct Bitrate {
    pub status: String,
    pub reason: Option<String>,
    pub duration_seconds: f64,
    pub segment_count: u64,
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

/// `--output packets`: one page of packet summaries
/// (docs/json-schema-packets-v1.json).
#[derive(Debug, Deserialize)]
pub struct PacketsPage {
    pub schema_version: String,
    pub output: String,
    pub input: PacketsInput,
    pub status: String,
    pub error: Option<String>,
    pub first_offset_bytes: Option<u64>,
    pub next_offset_bytes: Option<u64>,
    pub end_of_file: bool,
    pub total_packets: u64,
    pub packets: Vec<PacketSummary>,
    pub warnings: Vec<Warning>,
}

#[derive(Debug, Deserialize)]
pub struct PacketsInput {
    pub path: String,
    pub file_size_bytes: u64,
    pub packet_size_bytes: Option<u32>,
    pub offset_bytes: i64,
}

#[derive(Clone, Debug, Deserialize)]
pub struct PacketSummary {
    pub offset_bytes: u64,
    pub valid: bool,
    pub pid: Option<String>,
    pub pusi: bool,
    pub transport_error: bool,
    pub scrambling_control: u8,
    pub adaptation_field_control: u8,
    pub continuity_counter: u8,
    pub pcr_ms: Option<f64>,
    /// Headers of the PSI sections that start in this packet.
    #[serde(default)]
    pub sections: Vec<PacketSection>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct PacketSection {
    pub table_id: String,
    pub table_id_extension: u16,
    pub version_number: u8,
    pub current_next_indicator: u8,
    pub section_number: u8,
    pub last_section_number: u8,
}

/// `--output packet`: one packet decoded field by field.
#[derive(Debug, Deserialize)]
pub struct PacketDetail {
    pub schema_version: String,
    pub output: String,
    pub input: PacketsInput,
    pub status: String,
    pub error: Option<String>,
    pub packet: Option<DecodedPacket>,
    pub unit: Option<PacketUnit>,
    pub warnings: Vec<Warning>,
}

#[derive(Debug, Deserialize)]
pub struct DecodedPacket {
    pub offset_bytes: u64,
    /// The packet bytes in hex.
    pub bytes: String,
    pub layers: Vec<PacketLayer>,
}

#[derive(Debug, Deserialize)]
pub struct PacketLayer {
    pub name: String,
    pub fields: Vec<PacketField>,
    #[serde(default)]
    pub rows: Vec<Vec<PacketField>>,
    #[serde(default)]
    pub truncated: bool,
}

#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct PacketField {
    pub name: String,
    pub value: String,
    pub byte_offset: usize,
    pub byte_length: usize,
    /// The exact bits, when the field does not use all bits of its bytes.
    #[serde(default)]
    pub bits: Vec<BitRange>,
}

/// Packet-relative bits in wire order: bit 0 is the most significant bit of
/// packet byte 0.
#[derive(Clone, Debug, Deserialize, PartialEq)]
pub struct BitRange {
    pub bit_offset: usize,
    pub bit_length: usize,
}

impl PacketField {
    pub fn bit_ranges(&self) -> Vec<std::ops::Range<usize>> {
        self.bits
            .iter()
            .map(|b| b.bit_offset..b.bit_offset + b.bit_length)
            .collect()
    }
}

#[derive(Debug, Deserialize)]
pub struct PacketUnit {
    pub kind: String,
    pub first_offset_bytes: Option<u64>,
    #[serde(default)]
    pub started_before_range: bool,
    pub position: u64,
    pub packets_in_range: u64,
    pub previous_offset_bytes: Option<u64>,
    pub next_offset_bytes: Option<u64>,
    pub stream_id: Option<String>,
    pub pts_ms: Option<f64>,
    pub dts_ms: Option<f64>,
    pub table_id: Option<String>,
}

impl DecodedPacket {
    /// The packet bytes, or nothing when the hex is malformed.
    pub fn raw(&self) -> Vec<u8> {
        (0..self.bytes.len() / 2)
            .map_while(|i| u8::from_str_radix(self.bytes.get(2 * i..2 * i + 2)?, 16).ok())
            .collect()
    }
}

/// Parses a packet document of the given output and returns it only when it
/// completed; a failed document becomes its error message.
fn parse_packets_document<T: for<'de> Deserialize<'de>>(
    json: impl std::io::Read,
    output: &str,
    major: &str,
    envelope: impl Fn(&T) -> (&str, &str, &str, Option<&String>),
) -> Result<T, String> {
    let document: T = serde_json::from_reader(json)
        .map_err(|err| format!("Unreadable {output} output: {err}"))?;
    let (doc_output, schema, status, error) = envelope(&document);
    if doc_output != output || schema_major(schema) != Some(major) {
        return Err(format!(
            "Unsupported {output} document (output {doc_output:?}, schema {schema:?})."
        ));
    }
    if status != "complete" {
        return Err(error
            .cloned()
            .unwrap_or_else(|| format!("The CLI reported {status}.")));
    }
    Ok(document)
}

/// Parses an `--output packets` document as it is read.
pub fn parse_packets_page(json: impl std::io::Read, major: &str) -> Result<PacketsPage, String> {
    parse_packets_document(json, "packets", major, |d: &PacketsPage| {
        (&d.output, &d.schema_version, &d.status, d.error.as_ref())
    })
}

/// Parses an `--output packet` document as it is read.
pub fn parse_packet_detail(json: impl std::io::Read, major: &str) -> Result<PacketDetail, String> {
    parse_packets_document(json, "packet", major, |d: &PacketDetail| {
        (&d.output, &d.schema_version, &d.status, d.error.as_ref())
    })
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

impl Service {
    /// One line for the Overview: the name, then the provider and service type.
    pub fn summary(&self) -> String {
        let name = match (&self.name, &self.name_hex) {
            (Some(name), _) => format!("\"{}\"", name.replace('\n', " ")),
            (None, Some(hex)) => format!("name not decodable (0x{hex})"),
            (None, None) => "without a name".to_owned(),
        };
        let provider = match (&self.provider_name, &self.provider_name_hex) {
            (Some(provider), _) => Some(provider.replace('\n', " ")),
            (None, Some(hex)) => Some(format!("provider not decodable (0x{hex})")),
            (None, None) => None,
        };
        let details: Vec<String> = provider
            .into_iter()
            .chain(self.service_type_description.iter().cloned())
            .collect();
        if details.is_empty() {
            format!("Service {name}")
        } else {
            format!("Service {name} ({})", details.join(", "))
        }
    }
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

    /// The service name from the SDT, if the CLI could decode one.
    pub fn service_name(&self) -> Option<&str> {
        let name = self.service.as_ref()?.name.as_deref()?;
        (!name.trim().is_empty()).then_some(name)
    }

    /// Label used by the program selector.
    pub fn label(&self) -> String {
        let mut parts = vec![self.program_number.to_string()];
        parts.extend(self.service_name().map(|name| name.replace('\n', " ")));
        parts.push(self.description());
        parts.extend(
            self.average_bitrate_bps
                .map(|bps| format!("{:.2} Mbps", bps / 1e6)),
        );
        format!("{} (PMT {})", parts.join(" · "), self.pmt_pid)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn program_with(service: &str) -> Program {
        serde_json::from_str(&format!(
            r#"{{"program_number": 3, "pmt_pid": "0x0100", "elementary_streams": [{{"pid": "0x0101", "stream_type": "0x1b", "stream_type_description": "AVC video"}}], {service} "status": "complete"}}"#
        ))
        .expect("program parses")
    }

    #[test]
    fn labels_programs_by_service_name() {
        assert_eq!(program_with("").label(), "3 · H.264 video (PMT 0x0100)");
        let undecodable = program_with(r#""service": {"name_hex": "110041"},"#);
        assert_eq!(undecodable.label(), "3 · H.264 video (PMT 0x0100)");
        assert_eq!(
            undecodable
                .service
                .as_ref()
                .map(Service::summary)
                .as_deref(),
            Some("Service name not decodable (0x110041)")
        );
        let blank = program_with(r#""service": {"name": " "},"#);
        assert_eq!(blank.service_name(), None);
        let hex_provider = program_with(
            r#""service": {"name": "Radio", "provider_name_hex": "1f02", "service_type_description": "digital radio sound service"},"#,
        );
        assert_eq!(
            hex_provider
                .service
                .as_ref()
                .map(Service::summary)
                .as_deref(),
            Some(
                "Service \"Radio\" (provider not decodable (0x1f02), digital radio sound service)"
            )
        );
        let unnamed = program_with(r#""service": {},"#);
        assert_eq!(
            unnamed.service.as_ref().map(Service::summary).as_deref(),
            Some("Service without a name")
        );
        let two_lines =
            program_with(r#""service": {"name": "News\nOne", "provider_name": "Broad\ncaster"},"#);
        assert_eq!(two_lines.label(), "3 · News One · H.264 video (PMT 0x0100)");
        assert_eq!(
            two_lines.service.as_ref().map(Service::summary).as_deref(),
            Some("Service \"News One\" (Broad caster)")
        );
    }

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
        assert_eq!(
            program.label(),
            "1 · Main · MPEG-2 video + MPEG-1 audio · 0.32 Mbps (PMT 0x1000)"
        );
        assert_eq!(
            program.service.as_ref().map(Service::summary).as_deref(),
            Some("Service \"Main\" (FFmpeg, digital television service)")
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
        assert!(parse_analysis(valid.as_bytes(), "2").is_ok());
        let mut missing: serde_json::Value = serde_json::from_str(valid).unwrap();
        missing.as_object_mut().unwrap().remove("programs");
        let mut other_output: serde_json::Value = serde_json::from_str(valid).unwrap();
        other_output["output"] = "summary".into();
        let mut input_missing: serde_json::Value = serde_json::from_str(valid).unwrap();
        input_missing["input"]
            .as_object_mut()
            .unwrap()
            .remove("file_size_bytes");
        let mut jitter_missing: serde_json::Value = serde_json::from_str(valid).unwrap();
        jitter_missing["programs"][0]["pcr_jitter"]["estimate"]
            .as_object_mut()
            .unwrap()
            .remove("sample_count");
        let mut delivery_missing: serde_json::Value = serde_json::from_str(valid).unwrap();
        delivery_missing["programs"][0]["pcr_jitter"]
            .as_object_mut()
            .unwrap()
            .remove("delivery");
        // A schema 1.0 document has the estimate fields at the top level of
        // pcr_jitter and no delivery; it is rejected for its version.
        let mut schema_1: serde_json::Value = serde_json::from_str(valid).unwrap();
        schema_1["schema_version"] = "1.0".into();
        schema_1["programs"][0]["pcr_jitter"] = serde_json::json!({
            "status": "ng", "sample_count": 3, "segment_count": 1,
            "measured_sample_count": 1, "discontinuities": []
        });
        let mut schema_3: serde_json::Value = serde_json::from_str(valid).unwrap();
        schema_3["schema_version"] = "3.0".into();
        let mut bitrate_missing: serde_json::Value = serde_json::from_str(valid).unwrap();
        bitrate_missing["programs"][0]["bitrate"]
            .as_object_mut()
            .unwrap()
            .remove("segment_count");
        for (name, document, expected) in [
            ("empty object", "{}".to_owned(), "Unreadable"),
            ("missing programs", missing.to_string(), "Unreadable"),
            (
                "missing input field",
                input_missing.to_string(),
                "Unreadable",
            ),
            (
                "missing jitter field",
                jitter_missing.to_string(),
                "Unreadable",
            ),
            (
                "missing delivery",
                delivery_missing.to_string(),
                "Unreadable",
            ),
            (
                "missing bitrate field",
                bitrate_missing.to_string(),
                "Unreadable",
            ),
            ("summary output", other_output.to_string(), "Unsupported"),
            (
                "schema 1.0",
                schema_1.to_string(),
                "Update the mpeg-ts-analyzer CLI",
            ),
            ("schema 3.0", schema_3.to_string(), "Unsupported"),
        ] {
            let err = parse_analysis(document.as_bytes(), "2")
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
        assert!(caps.supports_json("analysis", "2"));
        assert!(!caps.supports_json("analysis", "1"));
        assert!(caps.supports_json("packets", "1"));
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
