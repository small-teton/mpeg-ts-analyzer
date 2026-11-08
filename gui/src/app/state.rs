//! The lifecycle state of the app, apart from the egui composition in
//! `app.rs`: whether the CLI can be used, whether an analysis is waiting or
//! running, and the completed analysis the screens show.
//!
//! Each state is one enum or struct, so combinations that cannot happen are
//! not representable: a CLI that is being checked is not also a failed one,
//! a launch waiting for the CLI is not also running, and a completed analysis
//! always has its plots, its packet paging, its range and its generation.

use std::path::PathBuf;
use std::sync::mpsc::Receiver;
use std::time::Instant;

use crate::cli::{AnalysisJob, Cli};
use crate::plot_data;
use crate::report::{AnalysisReport, ProgressEvent};
use crate::views::packet_pages::{PacketPages, Paging};

/// What Start and the status bar say while the CLI is being checked.
pub(super) const CHECKING_CLI: &str = "Checking the CLI…";

/// The CLI as far as the app knows it.
pub(super) enum CliState {
    /// Not checked yet: only until the app starts its first check.
    NotChecked,
    /// The capabilities check is running off the UI thread, because any
    /// program can be chosen as the CLI; its result arrives on the receiver.
    Checking(Receiver<Result<Cli, String>>),
    /// The CLI passed the capabilities check.
    Ready(Cli),
    /// Why the CLI cannot be used.
    Unusable(String),
}

impl CliState {
    /// The CLI, if it passed the check.
    pub fn ready(&self) -> Option<&Cli> {
        match self {
            CliState::Ready(cli) => Some(cli),
            _ => None,
        }
    }

    pub fn is_checking(&self) -> bool {
        matches!(self, CliState::Checking(_))
    }

    /// The CLI, or why it cannot be used yet: still being checked, or failed.
    pub fn status(&self) -> Result<&Cli, &str> {
        match self {
            CliState::NotChecked | CliState::Checking(_) => Err(CHECKING_CLI),
            CliState::Ready(cli) => Ok(cli),
            CliState::Unusable(problem) => Err(problem),
        }
    }

    /// Takes in the result of the check once it has finished.
    pub fn poll(&mut self) {
        if let CliState::Checking(receiver) = self
            && let Ok(result) = receiver.try_recv()
        {
            *self = result.into();
        }
    }
}

impl From<Result<Cli, String>> for CliState {
    fn from(result: Result<Cli, String>) -> Self {
        match result {
            Ok(cli) => CliState::Ready(cli),
            Err(problem) => CliState::Unusable(problem),
        }
    }
}

/// Whether an analysis is on its way. A completed one is an
/// [`AnalysisSession`], kept while the next one runs and if it is cancelled.
pub(super) enum AnalysisState {
    Idle,
    /// A range from the command line starts as soon as the CLI check passes.
    AwaitingCli,
    Running(Box<Running>),
}

impl AnalysisState {
    pub fn running(&self) -> Option<&Running> {
        match self {
            AnalysisState::Running(running) => Some(running.as_ref()),
            _ => None,
        }
    }

    pub fn is_running(&self) -> bool {
        self.running().is_some()
    }
}

/// An analysis in progress. Dropping it cancels the CLI.
pub(super) struct Running {
    pub job: AnalysisJob,
    /// The file it analyzes, for its packet paging.
    pub input: PathBuf,
    pub range: (u64, u64),
    pub started: Instant,
    pub progress: Option<ProgressEvent>,
}

/// Plot series of every program of the analysis, derived once per analysis.
pub(super) struct Plots {
    pub timelines: Vec<plot_data::Timeline>,
    pub bitrates: Vec<plot_data::Bitrate>,
}

impl Plots {
    fn of(report: &AnalysisReport) -> Self {
        Plots {
            timelines: report
                .programs
                .iter()
                .map(|program| plot_data::timeline(report, program))
                .collect(),
            bitrates: report
                .programs
                .iter()
                .map(|program| plot_data::bitrate(report, program))
                .collect(),
        }
    }
}

/// A completed analysis: the CLI's report and everything the screens derive
/// from it, which lives and goes with it.
pub(super) struct AnalysisSession {
    pub report: Box<AnalysisReport>,
    /// Plot series of `report`, derived once rather than every frame.
    pub plots: Plots,
    /// Packets of the analyzed range, read from the CLI page by page.
    pub packet_pages: PacketPages,
    /// The analyzed range: offset and limit.
    pub range: (u64, u64),
    /// Tells this analysis apart from earlier ones, so views drop the state
    /// they kept for an older one.
    pub generation: u64,
}

impl AnalysisSession {
    /// The session of a finished run, with Packets paging `paging`.
    pub fn new(
        report: Box<AnalysisReport>,
        running: &Running,
        paging: Paging,
        generation: u64,
    ) -> Self {
        let (offset, limit) = running.range;
        let packet_pages = PacketPages::new(running.input.clone(), &report, offset, limit, paging);
        Self::with_pages(report, running.range, packet_pages, generation)
    }

    /// The session of `report` with the given packet paging.
    pub fn with_pages(
        report: Box<AnalysisReport>,
        range: (u64, u64),
        packet_pages: PacketPages,
        generation: u64,
    ) -> Self {
        AnalysisSession {
            plots: Plots::of(&report),
            report,
            packet_pages,
            range,
            generation,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn cli_state_says_why_the_cli_cannot_be_used() {
        let (sender, receiver) = std::sync::mpsc::channel();
        let mut state = CliState::Checking(receiver);
        assert!(state.is_checking() && state.ready().is_none());
        assert_eq!(state.status().err(), Some(CHECKING_CLI));
        state.poll();
        assert!(state.is_checking(), "no result yet");
        sender.send(Err("Cannot run the CLI".to_owned())).unwrap();
        state.poll();
        assert!(!state.is_checking());
        assert_eq!(state.status().err(), Some("Cannot run the CLI"));
        assert!(state.ready().is_none());
    }

    #[test]
    fn analysis_state_reports_only_a_running_analysis() {
        assert!(!AnalysisState::Idle.is_running());
        assert!(!AnalysisState::AwaitingCli.is_running());
        assert!(AnalysisState::AwaitingCli.running().is_none());
    }
}
