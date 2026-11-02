//! Running the mpeg-ts-analyzer CLI as a subprocess.

use std::io::{BufRead, BufReader, Read};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::mpsc::{self, Receiver, TryRecvError};
use std::sync::{Arc, Mutex};
use std::thread;

use crate::report::{self, AnalysisReport, Capabilities, ProgressEvent};

/// Overrides the CLI location, mainly for development against a locally built CLI.
const CLI_ENV: &str = "MPEG_TS_ANALYZER_CLI";
const CLI_NAME: &str = "mpeg-ts-analyzer";
/// Major schema version of `--output analysis` this GUI reads.
const ANALYSIS_SCHEMA_MAJOR: &str = "1";

pub fn locate() -> PathBuf {
    std::env::var_os(CLI_ENV).map_or_else(|| PathBuf::from(CLI_NAME), PathBuf::from)
}

/// A CLI that passed the capabilities check.
pub struct Cli {
    pub path: PathBuf,
    pub version: String,
}

fn command(cli: &Path) -> Command {
    let mut command = Command::new(cli);
    hide_console_window(&mut command);
    command
}

#[cfg(windows)]
fn hide_console_window(command: &mut Command) {
    use std::os::windows::process::CommandExt;
    const CREATE_NO_WINDOW: u32 = 0x0800_0000;
    command.creation_flags(CREATE_NO_WINDOW);
}

#[cfg(not(windows))]
fn hide_console_window(_command: &mut Command) {}

/// Runs `--capabilities` and checks that the CLI has the outputs this GUI needs.
pub fn check(path: &Path) -> Result<Cli, String> {
    let output = command(path)
        .arg("--capabilities")
        .output()
        .map_err(|err| {
            format!(
                "Cannot run {}: {err}. Set {CLI_ENV} or put {CLI_NAME} on PATH.",
                path.display()
            )
        })?;
    if !output.status.success() {
        return Err(format!(
            "{} does not support --capabilities; a newer CLI is needed.",
            path.display()
        ));
    }
    let caps: Capabilities = serde_json::from_slice(&output.stdout)
        .map_err(|err| format!("Unreadable --capabilities output: {err}"))?;
    // Start runs `--format json --output analysis --progress json`.
    if !caps.supports_json("analysis", ANALYSIS_SCHEMA_MAJOR) || !caps.supports_progress("json") {
        return Err(format!(
            "CLI {} lacks JSON --output analysis (schema {ANALYSIS_SCHEMA_MAJOR}.x) or --progress json; a newer CLI is needed.",
            caps.cli_version
        ));
    }
    Ok(Cli {
        path: path.to_path_buf(),
        version: caps.cli_version,
    })
}

/// How a running analysis has progressed.
pub enum JobUpdate {
    Running(Option<ProgressEvent>),
    Finished(Result<Box<AnalysisReport>, String>),
}

/// A range analysis running in a child process.
pub struct AnalysisJob {
    child: Arc<Mutex<Child>>,
    progress: Receiver<ProgressEvent>,
    result: Receiver<Result<Box<AnalysisReport>, String>>,
    latest: Option<ProgressEvent>,
}

impl AnalysisJob {
    /// Starts `--output analysis` over `[offset, offset + limit)` of input.
    /// `repaint` is called whenever there is something new to show.
    pub fn start(
        cli: &Cli,
        input: &Path,
        offset: u64,
        limit: u64,
        repaint: impl Fn() + Send + Clone + 'static,
    ) -> Result<Self, String> {
        let mut child = command(&cli.path)
            .arg(input)
            .args([
                "--format",
                "json",
                "--output",
                "analysis",
                "--progress",
                "json",
            ])
            .args([
                "--offset",
                &offset.to_string(),
                "--limit",
                &limit.to_string(),
            ])
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|err| format!("Cannot start {}: {err}", cli.path.display()))?;
        let stdout = child.stdout.take().expect("stdout is piped");
        let stderr = child.stderr.take().expect("stderr is piped");
        let child = Arc::new(Mutex::new(child));

        // stderr carries progress events and, on failure, diagnostics.
        let (progress_tx, progress) = mpsc::channel();
        let diagnostics = Arc::new(Mutex::new(String::new()));
        let stderr_lines = Arc::clone(&diagnostics);
        let stderr_repaint = repaint.clone();
        let stderr_thread = thread::spawn(move || {
            for line in BufReader::new(stderr).lines().map_while(Result::ok) {
                match serde_json::from_str::<ProgressEvent>(&line) {
                    Ok(event) if event.kind == "progress" => {
                        let _ = progress_tx.send(event);
                        stderr_repaint();
                    }
                    _ => {
                        let mut text = stderr_lines.lock().expect("diagnostics lock");
                        text.push_str(&line);
                        text.push('\n');
                    }
                }
            }
        });

        // stdout carries the JSON document, written once the analysis ends. The
        // child is locked only after stdout closes, so cancel can always kill it.
        let (result_tx, result) = mpsc::channel();
        let waiter_child = Arc::clone(&child);
        thread::spawn(move || {
            let mut json = Vec::new();
            let read = BufReader::new(stdout).read_to_end(&mut json);
            let status = waiter_child.lock().expect("child lock").wait();
            let _ = stderr_thread.join();
            let diagnostics = diagnostics.lock().expect("diagnostics lock").clone();
            let outcome = match read {
                Ok(_) if json.iter().any(|b| !b.is_ascii_whitespace()) => {
                    report::parse_analysis(&json, ANALYSIS_SCHEMA_MAJOR).map(Box::new)
                }
                _ => Err(match status {
                    Ok(status) if !diagnostics.trim().is_empty() => {
                        format!("The CLI failed ({status}): {}", diagnostics.trim())
                    }
                    Ok(status) => format!("The CLI ended without a result ({status})."),
                    Err(err) => format!("The CLI could not be waited for: {err}"),
                }),
            };
            let _ = result_tx.send(outcome);
            repaint();
        });

        Ok(Self {
            child,
            progress,
            result,
            latest: None,
        })
    }

    /// Kills the process; its result, if any, is ignored by dropping the job.
    pub fn cancel(&self) {
        if let Ok(mut child) = self.child.lock() {
            let _ = child.kill();
        }
    }

    #[cfg(all(test, unix))]
    pub(crate) fn child(&self) -> Arc<Mutex<Child>> {
        Arc::clone(&self.child)
    }

    /// Collects what happened since the last call.
    pub fn poll(&mut self) -> JobUpdate {
        while let Ok(event) = self.progress.try_recv() {
            self.latest = Some(event);
        }
        match self.result.try_recv() {
            Ok(result) => JobUpdate::Finished(result),
            Err(TryRecvError::Empty) => JobUpdate::Running(self.latest.clone()),
            Err(TryRecvError::Disconnected) => {
                JobUpdate::Finished(Err("The analysis stopped unexpectedly.".to_owned()))
            }
        }
    }
}

/// A job owns its process: dropping it (closing the app, opening another
/// file) stops the analysis, since dropping a `Child` does not.
impl Drop for AnalysisJob {
    fn drop(&mut self) {
        self.cancel();
    }
}

/// A stand-in CLI: a shell script that ignores its arguments.
#[cfg(all(test, unix))]
pub(crate) fn fake_cli(dir: &Path, script: &str) -> Cli {
    use std::os::unix::fs::PermissionsExt;
    let path = dir.join("fake-cli");
    std::fs::write(&path, format!("#!/bin/sh\n{script}\n")).expect("write the fake CLI");
    std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o755)).expect("chmod");
    Cli {
        path,
        version: "fake".to_owned(),
    }
}

/// Waits until a killed or finished child has been reaped.
#[cfg(all(test, unix))]
pub(crate) fn wait_until_exited(child: &Mutex<Child>) -> bool {
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(10);
    while std::time::Instant::now() < deadline {
        if matches!(child.lock().expect("child lock").try_wait(), Ok(Some(_))) {
            return true;
        }
        thread::sleep(std::time::Duration::from_millis(20));
    }
    false
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{Duration, Instant};

    /// Runs only against a CLI named by MPEG_TS_ANALYZER_CLI (e.g. via `make gui-test`).
    fn test_cli() -> Option<Cli> {
        std::env::var_os(CLI_ENV).map(|path| check(Path::new(&path)).expect("capabilities check"))
    }

    fn wait(job: &mut AnalysisJob) -> (Result<Box<AnalysisReport>, String>, Option<ProgressEvent>) {
        let deadline = Instant::now() + Duration::from_secs(60);
        loop {
            match job.poll() {
                JobUpdate::Finished(result) => return (result, job.latest.clone()),
                JobUpdate::Running(_) if Instant::now() > deadline => panic!("analysis timed out"),
                JobUpdate::Running(_) => thread::sleep(Duration::from_millis(20)),
            }
        }
    }

    #[test]
    fn analyzes_a_sample_with_progress() {
        let Some(cli) = test_cli() else { return };
        let sample = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts");
        let mut job = AnalysisJob::start(&cli, &sample, 0, 200_000, || {}).expect("start");
        let (result, progress) = wait(&mut job);
        let report = result.expect("report");
        assert_eq!(report.status, "complete");
        assert_eq!(report.programs.len(), 1);
        let progress = progress.expect("progress events");
        assert!(progress.done && progress.processed_bytes == progress.total_bytes);
    }

    #[test]
    fn reports_a_failed_analysis_as_a_document() {
        let Some(cli) = test_cli() else { return };
        let mut job =
            AnalysisJob::start(&cli, Path::new("/no/such/file.ts"), 0, 1000, || {}).expect("start");
        let report = wait(&mut job).0.expect("a failure document");
        assert_eq!(report.status, "failed");
        assert_eq!(report.reason.as_deref(), Some("input_open_failed"));
    }

    #[cfg(unix)]
    #[test]
    fn dropping_a_job_stops_the_process() {
        let dir = tempfile_dir("drop");
        let cli = fake_cli(&dir, "exec sleep 30");
        let job = AnalysisJob::start(&cli, Path::new("input.ts"), 0, 1000, || {}).expect("start");
        let child = job.child();
        drop(job);
        assert!(
            wait_until_exited(&child),
            "the CLI kept running after the job was dropped"
        );
    }

    #[cfg(unix)]
    #[test]
    fn rejects_documents_that_are_not_analysis_schema_1() {
        let dir = tempfile_dir("documents");
        for (document, expected) in [
            ("{}", "Unreadable analysis output"),
            (
                r#"{"schema_version":"2.0","output":"analysis","input":{"path":"a","file_size_bytes":0,"offset_bytes":0},"status":"complete","continuity_events":[],"programs":[],"warnings":[]}"#,
                "Unsupported analysis document",
            ),
        ] {
            std::fs::write(dir.join("doc.json"), document).unwrap();
            let cli = fake_cli(&dir, &format!("cat '{}'", dir.join("doc.json").display()));
            let mut job =
                AnalysisJob::start(&cli, Path::new("input.ts"), 0, 1000, || {}).expect("start");
            let err = wait(&mut job).0.expect_err("an error");
            assert!(err.contains(expected), "{err}");
        }
    }

    #[cfg(unix)]
    #[test]
    fn rejects_a_cli_without_json_progress() {
        let dir = tempfile_dir("caps");
        let caps = r#"{"capabilities_version":"1.0","cli_version":"0.9","outputs":[{"name":"analysis","format":"json","schema_version":"1.0"}],"progress_formats":[]}"#;
        std::fs::write(dir.join("caps.json"), caps).unwrap();
        let cli = fake_cli(&dir, &format!("cat '{}'", dir.join("caps.json").display()));
        let Err(err) = check(&cli.path) else {
            panic!("accepted")
        };
        assert!(err.contains("--progress json"), "{err}");
    }

    fn tempfile_dir(name: &str) -> std::path::PathBuf {
        let dir = std::env::temp_dir().join(format!("mtsa-gui-test-{name}-{}", std::process::id()));
        std::fs::create_dir_all(&dir).expect("temp dir");
        dir
    }

    #[test]
    fn rejects_a_missing_cli() {
        let err = check(Path::new("/no/such/mpeg-ts-analyzer"))
            .err()
            .expect("error");
        assert!(err.contains("Cannot run"));
    }
}
