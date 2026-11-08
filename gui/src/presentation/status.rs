//! The statuses the CLI reports for its checks, as the GUI interprets them.
//!
//! [`CheckStatus`] has one variant per status value the current schema
//! (docs/json-schema-v2.json, "Current values" of each check's `status`)
//! documents, and keeps any other value in [`CheckStatus::Unknown`]: the
//! schema permits additive lowercase status values within schema version 2,
//! so a newer CLI can report one this GUI has never seen. Such a status is
//! shown verbatim with a neutral badge, never as a warning or an error.

use std::fmt;

use crate::theme::Verdict;
use crate::views::check_help::Check;

/// A status of a check, of one of its rows, or of an analyzer warning.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub enum CheckStatus {
    /// `ok`: the check passed.
    Ok,
    /// `warning`: the PCR jitter estimate is beyond the warning limit; also
    /// the GUI's own status of an analyzer warning.
    Warning,
    /// `ng`: the check failed.
    Ng,
    /// `errors`: continuity or PES-header errors were found.
    Errors,
    /// `anomalies`: timestamp anomalies were found.
    Anomalies,
    /// `recovered`: synchronization was lost and recovered.
    Recovered,
    /// `lost`: synchronization was lost and not recovered.
    Lost,
    /// `absent`: a referenced PID carried no packet.
    Absent,
    /// `silent`: a referenced PID went silent for longer than the limit.
    Silent,
    /// `unreferenced`: a PID carried packets without a reference.
    Unreferenced,
    /// `late`: a PSI repetition interval exceeded its limit.
    Late,
    /// `affected`: packets with transport errors or scrambling, none carrying
    /// data the analysis reads.
    Affected,
    /// `data_excluded`: data of at least one affected packet was left out of
    /// the analysis.
    DataExcluded,
    /// `info`: a report without a verdict (packet gaps, mux lead).
    Info,
    /// `skipped`: the check was not evaluated.
    Skipped,
    /// `not_assessable`: the PCR jitter estimate exists but was not graded.
    NotAssessable,
    /// `insufficient_data`: the range did not hold enough evidence to judge.
    InsufficientData,
    /// A status this GUI does not know, as the CLI wrote it. It is never one
    /// of the values above: [`CheckStatus::parse`] maps those to their
    /// variants.
    Unknown(String),
}

/// Every known status, with its wire value, in one table so parsing and
/// formatting cannot disagree.
const KNOWN: [(CheckStatus, &str); 17] = [
    (CheckStatus::Ok, "ok"),
    (CheckStatus::Warning, "warning"),
    (CheckStatus::Ng, "ng"),
    (CheckStatus::Errors, "errors"),
    (CheckStatus::Anomalies, "anomalies"),
    (CheckStatus::Recovered, "recovered"),
    (CheckStatus::Lost, "lost"),
    (CheckStatus::Absent, "absent"),
    (CheckStatus::Silent, "silent"),
    (CheckStatus::Unreferenced, "unreferenced"),
    (CheckStatus::Late, "late"),
    (CheckStatus::Affected, "affected"),
    (CheckStatus::DataExcluded, "data_excluded"),
    (CheckStatus::Info, "info"),
    (CheckStatus::Skipped, "skipped"),
    (CheckStatus::NotAssessable, "not_assessable"),
    (CheckStatus::InsufficientData, "insufficient_data"),
];

impl CheckStatus {
    /// The status the CLI wrote; a value this GUI does not know is kept.
    pub fn parse(value: &str) -> Self {
        KNOWN.iter().find(|(_, wire)| *wire == value).map_or_else(
            || CheckStatus::Unknown(value.to_owned()),
            |(s, _)| s.clone(),
        )
    }

    /// The status as the CLI writes it.
    pub fn as_str(&self) -> &str {
        match self {
            CheckStatus::Unknown(value) => value,
            known => {
                KNOWN
                    .iter()
                    .find(|(status, _)| status == known)
                    .expect("every known status has a wire value")
                    .1
            }
        }
    }

    /// The badge text: upper case, with spaces for underscores.
    pub fn label(&self) -> String {
        self.as_str().to_uppercase().replace('_', " ")
    }

    /// The status in running text: lower case, with spaces for underscores.
    pub fn words(&self) -> String {
        self.as_str().replace('_', " ")
    }

    /// The severity of the badge. Only statuses that report a problem are
    /// colored as one; `info`, skipped or ungraded analysis and every status
    /// this GUI does not know are neutral.
    ///
    /// `late`, `affected` and `data_excluded` are neutral as they were before
    /// this type existed: changing their color is a visible change of its own.
    pub fn severity(&self) -> Verdict {
        match self {
            CheckStatus::Ok => Verdict::Ok,
            CheckStatus::Ng | CheckStatus::Errors | CheckStatus::Lost | CheckStatus::Absent => {
                Verdict::Ng
            }
            CheckStatus::Warning
            | CheckStatus::Anomalies
            | CheckStatus::Recovered
            | CheckStatus::Silent
            | CheckStatus::Unreferenced => Verdict::Warn,
            CheckStatus::Late
            | CheckStatus::Affected
            | CheckStatus::DataExcluded
            | CheckStatus::Info
            | CheckStatus::Skipped
            | CheckStatus::NotAssessable
            | CheckStatus::InsufficientData
            | CheckStatus::Unknown(_) => Verdict::Neutral,
        }
    }

    /// Whether this GUI describes the status for `check`. A status another
    /// check knows (such as `warning`) is still unknown for a check without
    /// it.
    pub fn is_known_for(&self, check: Check) -> bool {
        check.verdict_meaning(self.as_str()).is_some()
    }

    /// Whether the status means the analysis of `check` is unavailable rather
    /// than a result: `skipped`, a PCR jitter estimate the CLI did not grade
    /// (`not_assessable`, because delivery is not constant), a PAT/PMT whose
    /// repetition the range does not show, or PES checks and PID references
    /// the CLI could not judge (`insufficient_data`). Only a status this
    /// check describes counts: an unknown `not_assessable` on another check
    /// stays a finding, shown as the CLI reported it.
    pub fn is_unavailable_for(&self, check: Check) -> bool {
        matches!(
            self,
            CheckStatus::Skipped | CheckStatus::NotAssessable | CheckStatus::InsufficientData
        ) && self.is_known_for(check)
    }

    /// Whether the status is a report without a verdict: the `info` of a
    /// report-only check (Packet gaps, Mux lead), which is neither a finding
    /// nor unavailable analysis.
    pub fn is_report_only_for(&self, check: Check) -> bool {
        *self == CheckStatus::Info && self.is_known_for(check)
    }

    /// Whether the status is a finding of `check`. Unavailable analysis and a
    /// report without a verdict are not findings; any other status than `ok`,
    /// including one this GUI does not know, is shown as the CLI reported it.
    pub fn is_finding_for(&self, check: Check) -> bool {
        *self != CheckStatus::Ok
            && !self.is_unavailable_for(check)
            && !self.is_report_only_for(check)
    }
}

impl From<&str> for CheckStatus {
    fn from(value: &str) -> Self {
        CheckStatus::parse(value)
    }
}

impl From<&String> for CheckStatus {
    fn from(value: &String) -> Self {
        CheckStatus::parse(value)
    }
}

impl fmt::Display for CheckStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The status fields of docs/json-schema-v2.json whose values the GUI
    /// shows as check statuses, by schema location (`properties` and
    /// `items` left out).
    const CHECK_STATUS_FIELDS: [&str; 23] = [
        "$defs/check",
        "$defs/pcrDiscontinuityCheck",
        "$defs/sync",
        "$defs/continuity",
        "$defs/pidReferences/referenced",
        "$defs/pidReferences/unreferenced",
        "$defs/referencedPID",
        "$defs/unreferencedPID",
        "$defs/packetGapPID",
        "$defs/affectedPackets",
        "$defs/pesHeaderPID",
        "$defs/pesHeaders",
        "$defs/ptsRepetitionPID",
        "$defs/ptsRepetition",
        "$defs/muxLeadPID",
        "$defs/avLeadAudio",
        "$defs/avLeadProgram",
        "$defs/psiHealth",
        "$defs/psiTableHealth",
        "$defs/psiRepetition",
        "$defs/timestampAnomalies",
        "$defs/pcrJitter",
        "$defs/bitrate",
    ];

    /// Status fields that are not check statuses: document and program
    /// lifecycle, acquisition evidence, the PSI time base, the failure policy
    /// and PSI section states. The GUI reads these where it needs them; they
    /// are never shown as a check badge.
    const OTHER_STATUS_FIELDS: [&str; 9] = [
        "json-schema-v2.json:",
        "$defs/program",
        "$defs/acquisition/alignment",
        "$defs/acquisition/alignment/layouts",
        "$defs/acquisition/pat",
        "$defs/psiTimeBase",
        "$defs/failurePolicy",
        "json-schema-analysis-v2.json:",
        "$defs/psiSection",
    ];

    /// Every `status` property of a schema with its location and the values
    /// its description lists after "Current values:".
    fn schema_statuses(file: &str) -> Vec<(String, Vec<String>)> {
        let path = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../docs")
            .join(file);
        let text = std::fs::read_to_string(&path).expect("read the schema");
        let schema: serde_json::Value = serde_json::from_str(&text).expect("parse the schema");
        let mut found = Vec::new();
        walk(&schema, &mut Vec::new(), &mut found);
        found
            .into_iter()
            .map(|(location, description)| {
                let location = if location.is_empty() {
                    format!("{file}:")
                } else {
                    location
                };
                (location, current_values(&description))
            })
            .collect()
    }

    fn walk(value: &serde_json::Value, path: &mut Vec<String>, found: &mut Vec<(String, String)>) {
        let serde_json::Value::Object(object) = value else {
            return;
        };
        if let Some(serde_json::Value::Object(properties)) = object.get("properties")
            && let Some(status) = properties.get("status")
        {
            let description = status
                .get("description")
                .and_then(serde_json::Value::as_str)
                .unwrap_or_default();
            found.push((path.join("/"), description.to_owned()));
        }
        for (key, child) in object {
            let skip = matches!(key.as_str(), "properties" | "items");
            if !skip {
                path.push(key.clone());
            }
            walk(child, path, found);
            if !skip {
                path.pop();
            }
        }
    }

    /// The values after "Current values:", without their parenthesized
    /// explanations, up to the end of that sentence.
    fn current_values(description: &str) -> Vec<String> {
        let Some((_, rest)) = description.split_once("Current values:") else {
            return Vec::new();
        };
        let mut plain = String::new();
        let mut depth = 0;
        for c in rest.chars() {
            match c {
                '(' => depth += 1,
                ')' => depth -= 1,
                _ if depth == 0 => plain.push(c),
                _ => {}
            }
        }
        let sentence = plain.split('.').next().unwrap_or_default();
        sentence
            .split(',')
            .map(|v| v.trim().to_owned())
            .filter(|v| !v.is_empty())
            .collect()
    }

    /// Pins the mapping to the schema: every status value a check can report
    /// today has its own variant, and every status field of the schemas is
    /// classified, so a new one has to be looked at.
    #[test]
    fn every_documented_check_status_has_a_variant() {
        let mut seen = Vec::new();
        for file in ["json-schema-v2.json", "json-schema-analysis-v2.json"] {
            for (location, values) in schema_statuses(file) {
                let is_check = CHECK_STATUS_FIELDS.contains(&location.as_str());
                assert!(
                    is_check || OTHER_STATUS_FIELDS.contains(&location.as_str()),
                    "{file}: classify the status of {location}"
                );
                assert!(!values.is_empty(), "{location}: no current values");
                if !is_check {
                    continue;
                }
                seen.push(location.clone());
                for value in values {
                    let status = CheckStatus::parse(&value);
                    assert!(
                        !matches!(status, CheckStatus::Unknown(_)),
                        "{location}: {value} has no variant"
                    );
                    assert_eq!(status.as_str(), value);
                }
            }
        }
        for location in CHECK_STATUS_FIELDS {
            assert!(
                seen.iter().any(|s| s == location),
                "{location} not in the schema"
            );
        }
    }

    /// Every status the check explanations describe is a known variant.
    #[test]
    fn every_described_status_has_a_variant() {
        for check in Check::ALL {
            for (value, _) in check.help().statuses {
                let status = CheckStatus::parse(value);
                assert!(
                    !matches!(status, CheckStatus::Unknown(_)),
                    "{check:?}: {value}"
                );
                assert!(status.is_known_for(check), "{check:?}: {value}");
            }
        }
    }

    #[test]
    fn statuses_round_trip_through_their_wire_value() {
        for (status, wire) in KNOWN {
            assert_eq!(CheckStatus::parse(wire), status);
            assert_eq!(status.as_str(), wire);
            assert_eq!(status.to_string(), wire);
        }
        let unknown = CheckStatus::parse("indeterminate");
        assert_eq!(unknown, CheckStatus::Unknown("indeterminate".to_owned()));
        assert_eq!(unknown.as_str(), "indeterminate");
        assert_eq!(CheckStatus::from("ok"), CheckStatus::Ok);
        assert_eq!(CheckStatus::from(&"ng".to_owned()), CheckStatus::Ng);
    }

    #[test]
    fn labels_and_words_keep_the_cli_value() {
        assert_eq!(CheckStatus::InsufficientData.label(), "INSUFFICIENT DATA");
        assert_eq!(CheckStatus::NotAssessable.words(), "not assessable");
        let unknown = CheckStatus::parse("some_future_state");
        assert_eq!(unknown.label(), "SOME FUTURE STATE");
        assert_eq!(unknown.words(), "some future state");
    }

    /// The badge colors the GUI used before statuses were typed, and a
    /// neutral badge for a status from a newer CLI (never a warning).
    #[test]
    fn severity_is_neutral_unless_the_status_reports_a_problem() {
        let expected = [
            ("ok", Verdict::Ok),
            ("ng", Verdict::Ng),
            ("errors", Verdict::Ng),
            ("lost", Verdict::Ng),
            ("absent", Verdict::Ng),
            ("warning", Verdict::Warn),
            ("anomalies", Verdict::Warn),
            ("recovered", Verdict::Warn),
            ("silent", Verdict::Warn),
            ("unreferenced", Verdict::Warn),
            ("late", Verdict::Neutral),
            ("affected", Verdict::Neutral),
            ("data_excluded", Verdict::Neutral),
            ("info", Verdict::Neutral),
            ("skipped", Verdict::Neutral),
            ("not_assessable", Verdict::Neutral),
            ("insufficient_data", Verdict::Neutral),
            ("indeterminate", Verdict::Neutral),
            ("warn", Verdict::Neutral),
            ("some_future_state", Verdict::Neutral),
        ];
        for (value, verdict) in expected {
            assert_eq!(CheckStatus::parse(value).severity(), verdict, "{value}");
        }
        for (status, _) in KNOWN {
            // Every known status is in the table above.
            assert!(
                expected.iter().any(|(v, _)| *v == status.as_str()),
                "{status}"
            );
        }
    }

    #[test]
    fn classification_depends_on_the_check() {
        let insufficient = CheckStatus::InsufficientData;
        assert!(insufficient.is_unavailable_for(Check::PesHeaders));
        assert!(!insufficient.is_unavailable_for(Check::MaxPcrInterval));
        assert!(CheckStatus::Skipped.is_unavailable_for(Check::MaxPcrInterval));
        // Continuity does not describe skipped: it stays a finding.
        assert!(!CheckStatus::Skipped.is_unavailable_for(Check::ContinuityCounter));
        assert!(CheckStatus::Skipped.is_finding_for(Check::ContinuityCounter));
        assert!(CheckStatus::Info.is_report_only_for(Check::PacketGaps));
        assert!(!CheckStatus::Info.is_finding_for(Check::MuxLead));
        assert!(CheckStatus::Info.is_finding_for(Check::MaxPcrInterval));
        assert!(!CheckStatus::Ok.is_finding_for(Check::PcrJitter));
        let future = CheckStatus::parse("indeterminate");
        assert!(!future.is_known_for(Check::PcrJitter));
        assert!(future.is_finding_for(Check::PcrJitter));
        assert!(CheckStatus::Warning.is_known_for(Check::PcrJitter));
        assert!(!CheckStatus::Warning.is_known_for(Check::MaxPcrInterval));
    }
}
