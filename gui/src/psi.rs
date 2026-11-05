//! The PSI layout of the analyzed range (`psi_layout` and `psi_transitions` of
//! the range analysis): what a PID carries at an offset, where the tables
//! changed, and what the analysis did with each PAT/PMT section copy.
//!
//! A CLI without `psi_layout` gives no layouts; labels then come from the
//! tables the analysis started with.

use std::collections::{BTreeMap, BTreeSet};

use crate::report::{
    AnalysisReport, Layout, PacketSection, Program, PsiSection, PsiTransition, stream_type_name,
};

pub const PAT_PID: &str = "0x0000";
pub const NULL_PID: &str = "0x1fff";

/// The layouts, section runs and changes of one analysis, for labeling packets.
#[derive(Debug, Default)]
pub struct PsiLayout {
    layouts: Vec<Layout>,
    sections: Vec<PsiSection>,
    /// Description of each applied change, by the offset of the packet that
    /// completed the new table.
    changes: BTreeMap<u64, String>,
    /// Labels when the CLI reports no layouts: the continuity check's and the
    /// tables the analysis started with.
    fallback: BTreeMap<String, String>,
    /// Every PID of the range with what it carries, in filter order.
    pids: Vec<(String, String)>,
}

impl PsiLayout {
    pub fn new(report: &AnalysisReport) -> Self {
        let mut changes: BTreeMap<u64, String> = BTreeMap::new();
        for transition in &report.psi_transitions {
            let text = describe(transition);
            changes
                .entry(transition.offset_bytes)
                .and_modify(|existing| {
                    existing.push_str(" · ");
                    existing.push_str(&text);
                })
                .or_insert(text);
        }
        let Some(layout) = &report.psi_layout else {
            let mut fallback = BTreeMap::new();
            if let Some(continuity) = &report.continuity_counter {
                for pid in &continuity.pids {
                    fallback.insert(pid.pid.clone(), pid.label.clone());
                }
            }
            fallback.insert(PAT_PID.to_owned(), "PAT".to_owned());
            for program in &report.programs {
                fallback.insert(
                    program.pmt_pid.clone(),
                    format!("PMT (program {})", program.program_number),
                );
                for stream in &program.elementary_streams {
                    fallback.insert(stream.pid.clone(), stream_type_name(&stream.stream_type));
                }
            }
            fallback.insert(NULL_PID.to_owned(), "null".to_owned());
            let mut pids: Vec<(String, String)> = fallback
                .iter()
                .map(|(pid, label)| (pid.clone(), label.clone()))
                .collect();
            pids.sort_by_key(|(pid, label)| (group(label), pid.clone()));
            return Self {
                changes,
                fallback,
                pids,
                ..Self::default()
            };
        };
        let mut pids: Vec<(String, String, u8)> = layout
            .pids
            .iter()
            .map(|pid| {
                let mut names: Vec<String> = Vec::new();
                let mut rank = u8::MAX;
                for role in &pid.roles {
                    let (name, role_rank) = match role.kind.as_str() {
                        "pat" => ("PAT".to_owned(), 0),
                        "pmt" => (
                            match role.program_number {
                                Some(number) => format!("PMT (program {number})"),
                                None => "PMT".to_owned(),
                            },
                            1,
                        ),
                        "elementary_stream" => (
                            role.stream_type
                                .as_deref()
                                .map_or("stream".to_owned(), stream_type_name),
                            2,
                        ),
                        "pcr" => ("PCR".to_owned(), 2),
                        "null" => ("null".to_owned(), 3),
                        _ => ("unannounced".to_owned(), 4),
                    };
                    rank = rank.min(role_rank);
                    if !names.contains(&name) {
                        names.push(name);
                    }
                }
                // A PID that carries a stream and the PCR is named by the stream.
                if names.len() > 1 {
                    names.retain(|name| name != "PCR");
                }
                (pid.pid.clone(), names.join(" / "), rank)
            })
            .collect();
        pids.sort_by(|a, b| (a.2, &a.0).cmp(&(b.2, &b.0)));
        Self {
            layouts: layout.layouts.clone(),
            sections: layout.sections.clone(),
            changes,
            fallback: BTreeMap::new(),
            pids: pids
                .into_iter()
                .map(|(pid, label, _)| (pid, label))
                .collect(),
        }
    }

    /// Every PID of the range with what it carries: PAT, PMTs, streams, null,
    /// then others, in PID order within each group.
    pub fn pids(&self) -> &[(String, String)] {
        &self.pids
    }

    /// The layout in effect at `offset`.
    pub fn layout_at(&self, offset: u64) -> Option<&Layout> {
        let index = self.layouts.partition_point(|l| l.offset_bytes <= offset);
        self.layouts
            .get(index.checked_sub(1)?)
            .filter(|l| offset < l.end_offset_bytes)
    }

    /// What `pid` carries in the layout in effect at `offset`.
    pub fn label(&self, pid: &str, offset: u64) -> String {
        if let Some(layout) = self.layout_at(offset)
            && let Some(label) = layout_label(layout, pid)
        {
            return label;
        }
        if pid == PAT_PID {
            return "PAT".to_owned();
        }
        if pid == NULL_PID {
            return "null".to_owned();
        }
        if self.layouts.is_empty() {
            return self.fallback.get(pid).cloned().unwrap_or_default();
        }
        "unannounced".to_owned()
    }

    /// The change completed by the packet at `offset`, if any.
    pub fn change_at(&self, offset: u64) -> Option<&str> {
        self.changes.get(&offset).map(String::as_str)
    }

    /// The run of section copies that `section`, starting in the packet of
    /// `pid` at `offset`, belongs to. None when the CLI did not log it.
    pub fn section_run(
        &self,
        pid: &str,
        offset: u64,
        section: &PacketSection,
    ) -> Option<&PsiSection> {
        self.sections
            .iter()
            .find(|run| holds(run, pid, offset, section))
    }

    /// What the sections starting in a packet are and what the analysis did
    /// with them, for example `PMT v1 · applied`: `applied` for the copy that
    /// made the table take effect, `repeat` for later identical copies,
    /// `not applied` and `invalid (reason)` as the CLI reports them.
    pub fn sections_text(&self, pid: &str, offset: u64, sections: &[PacketSection]) -> String {
        sections
            .iter()
            .map(|section| {
                let table = match section.table_id.as_str() {
                    "0x00" => "PAT".to_owned(),
                    "0x02" => "PMT".to_owned(),
                    other => format!("table {other}"),
                };
                let mut text = format!("{table} v{}", section.version_number);
                if section.last_section_number > 0 {
                    text.push_str(&format!(
                        " #{}/{}",
                        section.section_number, section.last_section_number
                    ));
                }
                if section.current_next_indicator == 0 {
                    text.push_str(" next");
                }
                if let Some(run) = self.section_run(pid, offset, section) {
                    // A run's status covers all its copies; only the first copy
                    // of an applied run is the one the analysis applied.
                    let status = match (run.status.as_str(), &run.invalid_reason) {
                        ("applied", _) if offset == run.offset_bytes => "applied".to_owned(),
                        ("applied", _) => "repeat".to_owned(),
                        ("retransmission", _) => "repeat".to_owned(),
                        ("not_applied", _) => "not applied".to_owned(),
                        ("invalid", Some(reason)) => {
                            format!("invalid ({})", reason.replace('_', " "))
                        }
                        (other, _) => other.replace('_', " "),
                    };
                    text.push_str(" · ");
                    text.push_str(&status);
                }
                text
            })
            .collect::<Vec<_>>()
            .join(", ")
    }
}

/// Whether the copy of `section` in the packet of `pid` at `offset` is one of
/// `run` (docs/reference/json-output.md, "PSI layout of the range", "Per packet").
fn holds(run: &PsiSection, pid: &str, offset: u64, section: &PacketSection) -> bool {
    let extension = match (section.table_id.as_str(), run.table.as_str()) {
        ("0x00", "pat") => run.transport_stream_id,
        ("0x02", "pmt") => run.program_number,
        _ => return false,
    };
    run.pid == pid
        && extension == Some(section.table_id_extension)
        && run.version_number == Some(section.version_number)
        && run.current_next_indicator == Some(section.current_next_indicator)
        && run.section_number == Some(section.section_number)
        && run.last_section_number == Some(section.last_section_number)
        && (run.offset_bytes..=run.last_offset_bytes).contains(&offset)
}

fn group(label: &str) -> u8 {
    match label {
        "PAT" => 0,
        _ if label.starts_with("PMT") => 1,
        "null" => 3,
        _ => 2,
    }
}

/// What `pid` carries in `layout`, if the layout lists it.
fn layout_label(layout: &Layout, pid: &str) -> Option<String> {
    if pid == PAT_PID {
        return Some("PAT".to_owned());
    }
    let pmt_of: Vec<String> = layout
        .programs
        .iter()
        .filter(|p| p.pmt_pid == pid)
        .map(|p| p.program_number.to_string())
        .collect();
    if !pmt_of.is_empty() {
        let plural = if pmt_of.len() > 1 { "s" } else { "" };
        return Some(format!("PMT (program{plural} {})", pmt_of.join(", ")));
    }
    let mut names: Vec<String> = Vec::new();
    for program in &layout.programs {
        for stream in program.elementary_streams.iter().filter(|s| s.pid == pid) {
            let name = stream_type_name(&stream.stream_type);
            if !names.contains(&name) {
                names.push(name);
            }
        }
    }
    if !names.is_empty() {
        return Some(names.join(" / "));
    }
    layout
        .programs
        .iter()
        .any(|p| p.pcr_pid.as_deref() == Some(pid))
        .then(|| "PCR".to_owned())
}

/// One line describing an applied change, for example
/// `PMT 1 v0 → v1: +0x0200 −0x0100, 0x0101 0x03 → 0x04, PCR 0x0100 → 0x0200`.
pub fn describe(transition: &PsiTransition) -> String {
    let mut parts: Vec<String> = Vec::new();
    for stream in &transition.elementary_streams_added {
        parts.push(format!("+{}", stream.pid));
    }
    for stream in &transition.elementary_streams_removed {
        parts.push(format!("−{}", stream.pid));
    }
    for change in &transition.stream_type_changes {
        parts.push(format!(
            "{} {} → {}",
            change.pid, change.previous_stream_type, change.stream_type
        ));
    }
    if let (Some(previous), Some(pcr)) = (&transition.previous_pcr_pid, &transition.pcr_pid)
        && previous != pcr
    {
        parts.push(format!("PCR {previous} → {pcr}"));
    }
    for program in &transition.programs_added {
        parts.push(format!(
            "+program {} (PMT {})",
            program.program_number, program.pmt_pid
        ));
    }
    for program in &transition.programs_removed {
        parts.push(format!("−program {}", program.program_number));
    }
    for change in &transition.pmt_pid_changes {
        parts.push(format!(
            "program {} PMT {} → {}",
            change.program_number, change.previous_pmt_pid, change.pmt_pid
        ));
    }
    let table = match (transition.table.as_str(), transition.program_number) {
        ("pmt", Some(number)) => format!("PMT {number}"),
        ("pat", _) => "PAT".to_owned(),
        (other, _) => other.to_uppercase(),
    };
    let head = format!(
        "{table} v{} → v{}",
        transition.previous_version, transition.version
    );
    if parts.is_empty() {
        head
    } else {
        format!("{head}: {}", parts.join(", "))
    }
}

/// Whether a change concerns `program`: every PAT change, and the PMT changes
/// of that program.
pub fn concerns(transition: &PsiTransition, program: &Program) -> bool {
    transition.table == "pat" || transition.program_number == Some(program.program_number)
}

/// What the PIDs of one program carry over the whole range.
#[derive(Debug, Default, PartialEq)]
pub struct ProgramPids {
    /// PAT and every PMT PID the program used.
    psi: BTreeSet<String>,
    /// Codec names per elementary-stream PID, in the order they applied.
    streams: BTreeMap<String, Vec<String>>,
    /// For each elementary-stream PID, the byte intervals of the layouts that
    /// list it, merged where they touch.
    present: BTreeMap<String, Vec<(u64, u64)>>,
    /// PMT, PCR and elementary-stream PIDs of the program per layout interval.
    members: Vec<(u64, u64, BTreeSet<String>)>,
}

impl ProgramPids {
    pub fn new(report: &AnalysisReport, program: &Program) -> Self {
        let mut pids = Self::default();
        pids.psi.insert(PAT_PID.to_owned());
        pids.psi.insert(program.pmt_pid.clone());
        fn add_stream(streams: &mut BTreeMap<String, Vec<String>>, pid: &str, stream_type: &str) {
            let names = streams.entry(pid.to_owned()).or_default();
            let name = stream_type_name(stream_type);
            if !names.contains(&name) {
                names.push(name);
            }
        }
        let layouts = report
            .psi_layout
            .as_ref()
            .map_or(&[][..], |l| &l.layouts[..]);
        if layouts.is_empty() {
            for stream in &program.elementary_streams {
                add_stream(&mut pids.streams, &stream.pid, &stream.stream_type);
                pids.present.insert(stream.pid.clone(), vec![(0, u64::MAX)]);
            }
            let members = program
                .elementary_streams
                .iter()
                .map(|s| s.pid.clone())
                .chain(program.pcr_pid.clone())
                .chain([program.pmt_pid.clone()])
                .collect();
            pids.members.push((0, u64::MAX, members));
            return pids;
        }
        for layout in layouts {
            for entry in layout
                .programs
                .iter()
                .filter(|p| p.program_number == program.program_number)
            {
                pids.psi.insert(entry.pmt_pid.clone());
                let members = entry
                    .elementary_streams
                    .iter()
                    .map(|s| s.pid.clone())
                    .chain(entry.pcr_pid.clone())
                    .chain([entry.pmt_pid.clone()])
                    .collect();
                pids.members
                    .push((layout.offset_bytes, layout.end_offset_bytes, members));
                for stream in &entry.elementary_streams {
                    add_stream(&mut pids.streams, &stream.pid, &stream.stream_type);
                    let intervals = pids.present.entry(stream.pid.clone()).or_default();
                    match intervals.last_mut() {
                        Some(last) if last.1 == layout.offset_bytes => {
                            last.1 = layout.end_offset_bytes
                        }
                        _ => intervals.push((layout.offset_bytes, layout.end_offset_bytes)),
                    }
                }
            }
        }
        pids
    }

    /// PAT and the program's PMTs: a few kbit/s next to megabits of audio and
    /// video, so the Bitrate screen leaves them out of its lines and table.
    pub fn is_psi(&self, pid: &str) -> bool {
        self.psi.contains(pid)
    }

    /// What a PID carries: the codecs for elementary streams (`MPEG-1 audio →
    /// MPEG-2 audio` when its stream type changed), else `fallback`.
    pub fn carried_by(&self, pid: &str, fallback: &str) -> String {
        match self.streams.get(pid) {
            Some(names) => names.join(" → "),
            None => fallback.to_owned(),
        }
    }

    /// Whether `pid` is the PMT, PCR or an elementary-stream PID of the program
    /// in the layout in effect at `offset`.
    pub fn belongs(&self, pid: &str, offset: u64) -> bool {
        self.members
            .iter()
            .any(|(start, end, pids)| (*start..*end).contains(&offset) && pids.contains(pid))
    }

    /// Which presence interval of `pid` holds `offset`: points in different
    /// intervals are not joined. None where no layout of the program lists it.
    pub fn presence(&self, pid: &str, offset: u64) -> Option<usize> {
        self.present
            .get(pid)?
            .iter()
            .position(|(start, end)| (*start..*end).contains(&offset))
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::cli::run_on_sample;
    use crate::report::{PacketsPage, parse_analysis};

    pub(crate) const VERSION_CHANGE: [&str; 2] = [
        "sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts",
        "sample_192byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts",
    ];

    pub(crate) fn analyze(sample: &str) -> Option<AnalysisReport> {
        let json = run_on_sample(sample, &["--format", "json", "--output", "analysis"])?;
        Some(parse_analysis(&json, "1").expect("analysis parses"))
    }

    fn every_packet(sample: &str) -> PacketsPage {
        let json = run_on_sample(
            sample,
            &[
                "--format", "json", "--output", "packets", "--count", "10000",
            ],
        )
        .expect("checked by the caller");
        let page: PacketsPage = serde_json::from_slice(&json).expect("page parses");
        assert!(page.end_of_file, "{sample} fits in one page");
        page
    }

    /// The sample changes the PMT (video 0x0100 → 0x0200, audio stream type
    /// 0x03 → 0x04), then the PAT (PMT PID 0x1000 → 0x1100).
    #[test]
    fn labels_follow_the_layout_in_effect() {
        for sample in VERSION_CHANGE {
            let Some(mut report) = analyze(sample) else {
                return;
            };
            // A continuity error does not name a PID the layout no longer lists.
            report
                .continuity_counter
                .as_mut()
                .unwrap()
                .pids
                .push(crate::report::ContinuityPid {
                    pid: "0x0100".to_owned(),
                    label: "video".to_owned(),
                    error_count: 1,
                });
            let psi = PsiLayout::new(&report);
            let [pmt_change, pat_change] = &report.psi_transitions[..] else {
                panic!("{sample}: two changes expected");
            };
            let before = pmt_change.offset_bytes;
            let between = report.psi_layout.as_ref().unwrap().layouts[1].offset_bytes;
            let after = pat_change.offset_bytes + 1000;
            assert_eq!(psi.label("0x0100", before), "MPEG-2 video");
            assert_eq!(psi.label("0x0100", between), "unannounced");
            assert_eq!(psi.label("0x0200", before), "unannounced");
            assert_eq!(psi.label("0x0200", between), "MPEG-2 video");
            assert_eq!(psi.label("0x0101", before), "MPEG-1 audio");
            assert_eq!(psi.label("0x0101", between), "MPEG-2 audio");
            assert_eq!(psi.label("0x1000", between), "PMT (program 1)");
            assert_eq!(psi.label("0x1100", between), "unannounced");
            assert_eq!(psi.label("0x1100", after), "PMT (program 1)");
            assert_eq!(psi.label(PAT_PID, after), "PAT");
            assert_eq!(psi.label("0x0011", before), "unannounced");

            let filter: Vec<(&str, &str)> = psi
                .pids()
                .iter()
                .map(|(pid, label)| (pid.as_str(), label.as_str()))
                .collect();
            assert_eq!(
                &filter[..3],
                [
                    ("0x0000", "PAT"),
                    ("0x1000", "PMT (program 1)"),
                    ("0x1100", "PMT (program 1)")
                ]
            );
            assert!(filter.contains(&("0x0101", "MPEG-1 audio / MPEG-2 audio")));
            assert!(filter.contains(&("0x0100", "MPEG-2 video")));
            assert_eq!(filter.last(), Some(&("0x0011", "unannounced")));

            assert_eq!(
                psi.change_at(pmt_change.offset_bytes),
                Some("PMT 1 v0 → v1: +0x0200, −0x0100, 0x0101 0x03 → 0x04, PCR 0x0100 → 0x0200")
            );
            assert_eq!(
                psi.change_at(pat_change.offset_bytes),
                Some("PAT v0 → v1: program 1 PMT 0x1000 → 0x1100")
            );
            assert_eq!(psi.change_at(pmt_change.offset_bytes + 1), None);
        }
    }

    /// Every PAT/PMT copy on an announced PID belongs to exactly one run, and
    /// each run counts exactly its copies.
    #[test]
    fn sections_match_their_runs() {
        for sample in VERSION_CHANGE {
            let Some(report) = analyze(sample) else {
                return;
            };
            let psi = PsiLayout::new(&report);
            let page = every_packet(sample);
            let mut counts: BTreeMap<u64, u64> = BTreeMap::new();
            for packet in &page.packets {
                let pid = packet.pid.as_deref().unwrap_or_default();
                // Other tables (SDT on 0x0011) are not logged.
                for section in packet
                    .sections
                    .iter()
                    .filter(|s| ["0x00", "0x02"].contains(&s.table_id.as_str()))
                {
                    let runs: Vec<&PsiSection> = psi
                        .sections
                        .iter()
                        .filter(|run| holds(run, pid, packet.offset_bytes, section))
                        .collect();
                    let [run] = runs[..] else {
                        panic!(
                            "{sample}: {} runs for {pid} at {}",
                            runs.len(),
                            packet.offset_bytes
                        )
                    };
                    *counts.entry(run.offset_bytes).or_default() += 1;
                }
            }
            let expected: BTreeMap<u64, u64> = psi
                .sections
                .iter()
                .map(|run| (run.offset_bytes, run.count))
                .collect();
            assert_eq!(counts, expected, "{sample}");

            let change = report.psi_transitions[0].offset_bytes;
            let packet = page
                .packets
                .iter()
                .find(|p| p.offset_bytes == change)
                .unwrap();
            assert_eq!(
                psi.sections_text("0x1000", change, &packet.sections),
                "PMT v1 · applied"
            );
            let repeat = page
                .packets
                .iter()
                .filter(|p| p.pid.as_deref() == Some("0x1000") && !p.sections.is_empty())
                .nth(1)
                .unwrap();
            assert_eq!(
                psi.sections_text("0x1000", repeat.offset_bytes, &repeat.sections),
                "PMT v0 · repeat"
            );
        }
    }

    #[test]
    fn labels_sections_without_a_run() {
        let psi = PsiLayout::default();
        let section = |table_id: &str, section_number, last_section_number| PacketSection {
            table_id: table_id.to_owned(),
            table_id_extension: 1,
            version_number: 3,
            current_next_indicator: 0,
            section_number,
            last_section_number,
        };
        assert_eq!(
            psi.sections_text("0x0000", 0, &[section("0x00", 1, 2)]),
            "PAT v3 #1/2 next"
        );
        assert_eq!(
            psi.sections_text("0x0011", 0, &[section("0x42", 0, 0), section("0x42", 0, 0)]),
            "table 0x42 v3 next, table 0x42 v3 next"
        );
    }
}
