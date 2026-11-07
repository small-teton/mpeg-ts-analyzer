//! Running the mpeg-ts-analyzer CLI as a subprocess.

mod bounded;
mod process;

use std::ffi::OsString;
use std::io::{self, BufReader, Read};
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::mpsc::{self, Receiver, TryRecvError};
use std::sync::{Arc, Mutex, PoisonError};
use std::thread;
use std::time::{Duration, Instant};

use crate::report::{self, AnalysisReport, Capabilities, PacketDetail, PacketsPage, ProgressEvent};
pub use bounded::Limits;
use bounded::{DiagnosticTail, Limited, OutputLimit};
use process::{Finished, Process};

/// Overrides the CLI location, mainly for development against a locally built CLI.
const CLI_ENV: &str = "MPEG_TS_ANALYZER_CLI";
const CLI_NAME: &str = "mpeg-ts-analyzer";
/// Major schema version of `--output analysis` this GUI reads. Schema 2.0
/// split PCR jitter into an estimate and a delivery assessment.
const ANALYSIS_SCHEMA_MAJOR: &str = "2";
/// Major schema version of `--output packets` and `--output packet`.
pub const PACKETS_SCHEMA_MAJOR: &str = "1";

/// Where the CLI in use was found.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Source {
    Environment,
    Settings,
    Bundled,
    Path,
}

impl Source {
    pub fn label(self) -> &'static str {
        match self {
            Source::Environment => "MPEG_TS_ANALYZER_CLI",
            Source::Settings => "Settings",
            Source::Bundled => "bundled with the GUI",
            Source::Path => "PATH",
        }
    }
}

/// Finds the CLI: the environment variable, then the path set in Settings,
/// then a CLI bundled with the GUI, then `mpeg-ts-analyzer` on `PATH`.
pub fn locate(setting: &str) -> (Source, PathBuf) {
    let exe = std::env::current_exe().ok();
    resolve(
        std::env::var_os(CLI_ENV),
        setting,
        &bundled_candidates(exe.as_deref(), std::env::var_os("APPDIR")),
        |path| path.is_file(),
    )
}

fn resolve(
    env: Option<std::ffi::OsString>,
    setting: &str,
    bundled: &[PathBuf],
    exists: impl Fn(&Path) -> bool,
) -> (Source, PathBuf) {
    if let Some(path) = env.filter(|value| !value.is_empty()) {
        return (Source::Environment, PathBuf::from(path));
    }
    if !setting.trim().is_empty() {
        return (Source::Settings, PathBuf::from(setting.trim()));
    }
    if let Some(path) = bundled.iter().find(|path| exists(path)) {
        return (Source::Bundled, path.clone());
    }
    (Source::Path, PathBuf::from(CLI_NAME))
}

/// The directory under `<prefix>/lib` where the Linux packages keep the CLI.
/// It is not put in `bin`, where the CLI's own `.deb` installs it.
const LINUX_LIB_DIR: &str = "mpeg-ts-analyzer-gui";

/// Where a packaged GUI keeps its CLI, relative to the GUI executable: in
/// `<prefix>/lib/mpeg-ts-analyzer-gui` for a GUI in `<prefix>/bin` (`.deb`,
/// AppImage), next to it (Windows install directory or portable zip), in the
/// macOS bundle's `Contents/Resources`, and under `$APPDIR/usr/lib/...` in an
/// AppImage. The lib directory comes first: next to `/usr/bin/...-gui` may be
/// the CLI's own `.deb`, which is not the CLI this GUI was built with.
fn bundled_candidates(exe: Option<&Path>, appdir: Option<std::ffi::OsString>) -> Vec<PathBuf> {
    let name = format!("{CLI_NAME}{}", std::env::consts::EXE_SUFFIX);
    let mut candidates = Vec::new();
    if let Some(dir) = exe.and_then(Path::parent) {
        if dir.ends_with("bin")
            && let Some(prefix) = dir.parent()
        {
            candidates.push(prefix.join("lib").join(LINUX_LIB_DIR).join(&name));
        }
        candidates.push(dir.join(&name));
        if dir.ends_with("Contents/MacOS")
            && let Some(contents) = dir.parent()
        {
            candidates.push(contents.join("Resources").join(&name));
        }
    }
    if let Some(appdir) = appdir {
        candidates.push(
            PathBuf::from(appdir)
                .join("usr/lib")
                .join(LINUX_LIB_DIR)
                .join(&name),
        );
    }
    candidates
}

/// `--check-cli`: finds the CLI as the GUI would, checks it, and reports the
/// result on stdout without opening a window, so a package can be verified
/// after installing it. Returns false when no usable CLI was found.
pub fn report_check(setting: &str) -> bool {
    let (source, path) = locate(setting);
    match check(&path) {
        Ok(cli) => {
            println!(
                "{} ({}): mpeg-ts-analyzer {}",
                path.display(),
                source.label(),
                cli.version
            );
            true
        }
        Err(err) => {
            println!("{} ({}): {err}", path.display(), source.label());
            false
        }
    }
}

/// A CLI that passed the capabilities check.
pub struct Cli {
    pub path: PathBuf,
    pub version: String,
    /// Whether the CLI pages and decodes packets (`--output packets` / `packet`).
    pub packet_outputs: bool,
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
    check_with_timeout(path, CHECK_TIMEOUT)
}

/// How long `--capabilities` may take. Any file can be chosen as the CLI, and
/// a program that is not one may never exit.
const CHECK_TIMEOUT: Duration = Duration::from_secs(5);

/// Like [`check`], but the program is killed if it has not answered within
/// `timeout`.
pub fn check_with_timeout(path: &Path, timeout: Duration) -> Result<Cli, String> {
    let mut command = command(path);
    command.arg("--capabilities");
    check_command(command, path, timeout, &Limits::DEFAULT)
}

/// What became of a JSON document read from a pipe.
enum Document<T> {
    Parsed(T),
    /// It did not parse; the error to show.
    Unreadable(String),
    /// Nothing but whitespace was written.
    Empty,
    /// More than its limit was written; the error to show.
    Rejected(String),
}

/// Parses one JSON document with `parse` as it is read from `pipe`, through a
/// reader that rejects the first byte past `limit`. The output is read to its
/// end (within the limit) even when the parse stops early, so the process is
/// neither left blocked on a full pipe nor killed by a closed one; `unreadable` makes the error for it from the
/// parse error and the first `prefix` bytes of the output.
fn read_document<T>(
    pipe: impl Read,
    limit: &OutputLimit,
    prefix: usize,
    parse: impl FnOnce(&mut dyn Read) -> Result<T, String>,
    unreadable: impl FnOnce(String, &[u8]) -> String,
) -> Document<T> {
    let mut limited = Limited::new(pipe, limit.bytes).keeping_prefix(prefix);
    let parsed = parse(&mut BufReader::new(&mut limited));
    if !limited.exceeded() {
        let _ = io::copy(&mut limited, &mut io::sink());
    }
    if limited.exceeded() {
        return Document::Rejected(limit.rejected());
    }
    match parsed {
        Ok(value) => Document::Parsed(value),
        Err(_) if !limited.had_content() => Document::Empty,
        Err(err) => Document::Unreadable(unreadable(err, limited.prefix())),
    }
}

fn check_command(
    mut command: Command,
    path: &Path,
    timeout: Duration,
    limits: &Limits,
) -> Result<Cli, String> {
    let mut child = command
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|err| {
            format!(
                "Cannot run {}: {err}. Set {CLI_ENV} or put {CLI_NAME} on PATH.",
                path.display()
            )
        })?;
    let stdout = child.stdout.take().expect("stdout is piped");
    // The output is read on a thread and waited for with the same deadline: a
    // program can exit while a process it started keeps stdout open.
    let limit = limits.capabilities();
    let (output_tx, output) = mpsc::channel();
    {
        let limit = limit.clone();
        thread::spawn(move || {
            let document = read_document(
                stdout,
                &limit,
                0,
                |json| serde_json::from_reader::<_, Capabilities>(json).map_err(|e| e.to_string()),
                |err, _| err,
            );
            let _ = output_tx.send(document);
        });
    }
    let deadline = Instant::now() + timeout;
    let mut document = None;
    let status = loop {
        if document.is_none() {
            document = output.try_recv().ok();
        }
        if let Some(Document::Rejected(err)) = &document {
            // Waiting for a program that keeps writing is pointless.
            let _ = child.kill();
            let _ = child.wait();
            return Err(format!(
                "{}: {err} Is it the mpeg-ts-analyzer CLI?",
                path.display()
            ));
        }
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if Instant::now() < deadline => thread::sleep(Duration::from_millis(10)),
            Ok(None) => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(format!(
                    "{} did not answer --capabilities within {} s; is it the mpeg-ts-analyzer CLI?",
                    path.display(),
                    timeout.as_secs()
                ));
            }
            Err(err) => return Err(format!("Cannot wait for {}: {err}", path.display())),
        }
    };
    let document = match document {
        Some(document) => document,
        None => output
            .recv_timeout(deadline.saturating_duration_since(Instant::now()))
            .map_err(|_| {
                format!(
                    "{} did not finish its --capabilities output within {} s; is it the mpeg-ts-analyzer CLI?",
                    path.display(),
                    timeout.as_secs()
                )
            })?,
    };
    if let Document::Rejected(err) = document {
        return Err(format!(
            "{}: {err} Is it the mpeg-ts-analyzer CLI?",
            path.display()
        ));
    }
    if !status.success() {
        return Err(format!(
            "{} does not support --capabilities; a newer CLI is needed.",
            path.display()
        ));
    }
    let caps = match document {
        Document::Parsed(caps) => caps,
        Document::Empty => return Err("Unreadable --capabilities output: it was empty".to_owned()),
        Document::Unreadable(err) | Document::Rejected(err) => {
            return Err(format!("Unreadable --capabilities output: {err}"));
        }
    };
    // Start runs `--format json --output analysis --progress json`.
    if !caps.supports_json("analysis", ANALYSIS_SCHEMA_MAJOR) || !caps.supports_progress("json") {
        let analysis = caps
            .outputs
            .iter()
            .find(|o| o.name == "analysis" && o.format == "json")
            .map_or("no".to_owned(), |o| format!("schema {}", o.schema_version));
        return Err(format!(
            "CLI {} writes {analysis} JSON --output analysis{}; this GUI needs analysis schema {ANALYSIS_SCHEMA_MAJOR}.x and --progress json. Update the mpeg-ts-analyzer CLI.",
            caps.cli_version,
            if caps.supports_progress("json") {
                ""
            } else {
                " and no --progress json"
            }
        ));
    }
    Ok(Cli {
        path: path.to_path_buf(),
        packet_outputs: caps.supports_json("packets", PACKETS_SCHEMA_MAJOR)
            && caps.supports_json("packet", PACKETS_SCHEMA_MAJOR),
        version: caps.cli_version,
    })
}

/// How a running analysis has progressed.
pub enum JobUpdate {
    Running(Option<ProgressEvent>),
    Finished(Result<Box<AnalysisReport>, String>),
}

/// The latest progress event not yet taken by the UI. Only the newest one
/// matters, so a CLI writing progress as fast as it can holds one event here,
/// not a queue of them. The lock is held only to swap the value.
#[derive(Clone, Default)]
struct LatestProgress(Arc<Mutex<Option<ProgressEvent>>>);

impl LatestProgress {
    /// Stores `event`; true when the slot was empty, i.e. the UI has taken
    /// the previous one and needs a repaint to take this one.
    fn put(&self, event: ProgressEvent) -> bool {
        self.lock().replace(event).is_none()
    }

    fn take(&self) -> Option<ProgressEvent> {
        self.lock().take()
    }

    fn lock(&self) -> std::sync::MutexGuard<'_, Option<ProgressEvent>> {
        self.0.lock().unwrap_or_else(PoisonError::into_inner)
    }
}

/// Reads an analysis's stderr: progress events go to `progress`, and a
/// bounded tail of the other lines is kept as diagnostics.
fn read_analysis_stderr(
    limits: Limits,
    progress: LatestProgress,
    repaint: impl Fn() + Send + 'static,
) -> process::StderrReader {
    Box::new(move |stderr| {
        let mut reader = BufReader::new(stderr);
        let mut diagnostics = DiagnosticTail::new(limits.diagnostics);
        let mut line = Vec::new();
        while let Ok(Some(dropped)) =
            bounded::read_line(&mut reader, limits.diagnostic_line, &mut line)
        {
            match serde_json::from_slice::<ProgressEvent>(&line) {
                Ok(event) if dropped == 0 && event.kind == "progress" => {
                    if progress.put(event) {
                        repaint();
                    }
                }
                _ => diagnostics.push(&line, dropped),
            }
        }
        diagnostics.finish()
    })
}

/// A range analysis running in a child process.
pub struct AnalysisJob {
    /// Dropping the job (closing the app, opening another file) kills the
    /// process, since dropping a `Child` does not.
    process: Process<Document<AnalysisReport>>,
    progress: LatestProgress,
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
        let mut command = command(&cli.path);
        command
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
            ]);
        Self::spawn(command, Limits::DEFAULT, limit, repaint)
            .map_err(|err| format!("Cannot start {}: {err}", cli.path.display()))
    }

    /// Runs `command` as an analysis of `range_bytes` of input, reading its
    /// output within `limits`.
    fn spawn(
        command: Command,
        limits: Limits,
        range_bytes: u64,
        repaint: impl Fn() + Send + Clone + 'static,
    ) -> io::Result<Self> {
        // stderr carries progress events and, on failure, diagnostics.
        let progress = LatestProgress::default();
        let read_stderr = read_analysis_stderr(limits, progress.clone(), repaint.clone());

        // stdout carries the JSON document, written once the analysis ends.
        // It is parsed as it is read, so the raw document is never held.
        let limit = limits.analysis(range_bytes);
        let read_stdout: process::StdoutReader<Document<AnalysisReport>> = Box::new(move |pipe| {
            let document = read_document(
                pipe,
                &limit,
                bounded::PREFIX_BYTES,
                |json| report::read_analysis(json, ANALYSIS_SCHEMA_MAJOR),
                |err, prefix| {
                    report::unsupported_analysis(prefix, ANALYSIS_SCHEMA_MAJOR).unwrap_or(err)
                },
            );
            let refused = matches!(document, Document::Rejected(_));
            process::Output {
                value: document,
                refused,
            }
        });

        let (result_tx, result) = mpsc::channel();
        let process = Process::spawn(
            command,
            read_stdout,
            Some(read_stderr),
            move |Finished {
                      status,
                      stdout,
                      diagnostics,
                  }| {
                let failed = |status: io::Result<std::process::ExitStatus>| match status {
                    Ok(status) if !diagnostics.trim().is_empty() => {
                        format!("The CLI failed ({status}): {}", diagnostics.trim())
                    }
                    Ok(status) => format!("The CLI ended without a result ({status})."),
                    Err(err) => format!("The CLI could not be waited for: {err}"),
                };
                let outcome = match stdout {
                    Document::Parsed(report) => Ok(Box::new(report)),
                    Document::Rejected(err) => Err(err),
                    Document::Empty => Err(failed(status)),
                    // A CLI that failed says why on stderr; that is more use
                    // than the parse error of whatever it wrote.
                    Document::Unreadable(err) => match &status {
                        Ok(code) if !code.success() && !diagnostics.trim().is_empty() => {
                            Err(failed(status))
                        }
                        _ => Err(err),
                    },
                };
                let _ = result_tx.send(outcome);
                repaint();
            },
        )?;

        Ok(Self {
            process,
            progress,
            result,
            latest: None,
        })
    }

    /// Kills the process; its result, if any, is ignored by dropping the job.
    /// Returns at once: the process is killed and reaped by its supervisor.
    pub fn cancel(&self) {
        self.process.cancel();
    }

    /// Whether the process has exited and been reaped.
    #[cfg(test)]
    pub(crate) fn child(&self) -> process::ExitProbe {
        self.process.exited()
    }

    /// Collects what happened since the last call.
    pub fn poll(&mut self) -> JobUpdate {
        if let Some(event) = self.progress.take() {
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

/// The reply a [`Request`] expects: how to parse it and the most of it to read.
pub struct Reply<T> {
    limit: OutputLimit,
    parse: fn(&mut dyn Read) -> Result<T, String>,
}

impl Reply<PacketsPage> {
    /// A page of `count` packets from `--output packets`.
    pub fn packets_page(count: u64) -> Self {
        Self {
            limit: Limits::DEFAULT.packets_page(count),
            parse: |json| report::parse_packets_page(json, PACKETS_SCHEMA_MAJOR),
        }
    }
}

impl Reply<PacketDetail> {
    /// One decoded packet from `--output packet`.
    pub fn packet_detail() -> Self {
        Self {
            limit: Limits::DEFAULT.packet_detail(),
            parse: |json| report::parse_packet_detail(json, PACKETS_SCHEMA_MAJOR),
        }
    }
}

impl<T> Reply<T> {
    /// The same reply read within another limit.
    #[cfg(test)]
    pub(crate) fn with_limit(self, limit: OutputLimit) -> Self {
        Self { limit, ..self }
    }

    #[cfg(test)]
    pub(crate) fn new(limit: OutputLimit, parse: fn(&mut dyn Read) -> Result<T, String>) -> Self {
        Self { limit, parse }
    }
}

/// A short CLI run that writes one JSON document, such as a page of packets.
/// Dropping the request kills the process.
pub struct Request<T> {
    process: Process<Result<T, String>>,
    result: Receiver<Result<T, String>>,
}

impl<T: Send + 'static> Request<T> {
    /// Runs the CLI with `args` and parses its stdout as `reply`. The
    /// arguments are native strings, so a path that is not UTF-8 reaches the
    /// CLI unchanged.
    pub fn start(
        cli: &Cli,
        args: Vec<OsString>,
        reply: Reply<T>,
        repaint: impl Fn() + Send + 'static,
    ) -> Result<Self, String> {
        let mut command = command(&cli.path);
        command.args(&args);
        Self::spawn(command, reply, repaint)
            .map_err(|err| format!("Cannot start {}: {err}", cli.path.display()))
    }

    pub(crate) fn spawn(
        command: Command,
        reply: Reply<T>,
        repaint: impl Fn() + Send + 'static,
    ) -> io::Result<Self> {
        let (result_tx, result) = mpsc::channel();
        let Reply { limit, parse } = reply;
        let read_stdout: process::StdoutReader<Result<T, String>> = Box::new(move |pipe| {
            let (value, refused) = match read_document(pipe, &limit, 0, parse, |err, _| err) {
                Document::Parsed(value) => (Ok(value), false),
                Document::Unreadable(err) => (Err(err), false),
                Document::Empty => (Err("The CLI ended without a result.".to_owned()), false),
                Document::Rejected(err) => (Err(err), true),
            };
            process::Output { value, refused }
        });
        let process = Process::spawn(command, read_stdout, None, move |finished| {
            let _ = result_tx.send(finished.stdout);
            repaint();
        })?;
        Ok(Self { process, result })
    }

    /// The result once the CLI has finished.
    pub fn poll(&self) -> Option<Result<T, String>> {
        match self.result.try_recv() {
            Ok(result) => Some(result),
            Err(TryRecvError::Empty) => None,
            Err(TryRecvError::Disconnected) => {
                Some(Err("The CLI request stopped unexpectedly.".to_owned()))
            }
        }
    }

    /// Whether the process has exited and been reaped.
    #[cfg(test)]
    pub(crate) fn child(&self) -> process::ExitProbe {
        self.process.exited()
    }
}

impl<T> Drop for Request<T> {
    /// Returns at once: the process is killed and reaped by its supervisor.
    fn drop(&mut self) {
        self.process.cancel();
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
        packet_outputs: true,
    }
}

#[cfg(test)]
pub(crate) use process::{
    closes_output_and_hangs, returns_promptly, wait_for_closed_output, wait_until_exited,
    wait_until_released,
};

/// Runs the CLI named by MPEG_TS_ANALYZER_CLI (e.g. via `make gui-test`) on a
/// file of `sample_data` and returns its stdout; None without that CLI.
#[cfg(test)]
pub fn run_on_sample(sample: &str, args: &[&str]) -> Option<Vec<u8>> {
    let cli = std::env::var_os(CLI_ENV)?;
    let path = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../sample_data")
        .join(sample);
    let output = Command::new(cli)
        .arg(path)
        .args(args)
        .output()
        .expect("the CLI runs");
    assert!(output.status.success(), "{sample} {args:?}");
    Some(output.stdout)
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

    /// A CLI that closes its output and keeps running must not hold up
    /// Cancel: the UI thread never waits for the process.
    #[test]
    fn cancels_an_analysis_whose_cli_closed_its_output_and_hangs() {
        let marker = tempfile_dir("job-closed").join("closed");
        let job = AnalysisJob::spawn(
            closes_output_and_hangs(&marker),
            Limits::DEFAULT,
            1000,
            || {},
        )
        .expect("start");
        let child = job.child();
        wait_for_closed_output(&marker);
        assert!(!child.is_set(), "the fake CLI ended by itself");
        let job = std::sync::Arc::new(std::sync::Mutex::new(Some(job)));
        let cancelled = std::sync::Arc::clone(&job);
        assert!(
            returns_promptly(move || cancelled.lock().unwrap().as_ref().unwrap().cancel()),
            "Cancel waited for the process"
        );
        assert!(wait_until_exited(&child), "Cancel did not stop the CLI");
        assert!(
            returns_promptly(move || drop(job.lock().unwrap().take())),
            "dropping the job waited for the process"
        );
    }

    #[test]
    fn dropping_a_job_whose_cli_closed_its_output_stops_it() {
        let marker = tempfile_dir("job-drop-closed").join("closed");
        let job = AnalysisJob::spawn(
            closes_output_and_hangs(&marker),
            Limits::DEFAULT,
            1000,
            || {},
        )
        .expect("start");
        let child = job.child();
        wait_for_closed_output(&marker);
        assert!(
            returns_promptly(move || drop(job)),
            "dropping the job waited for the process"
        );
        assert!(wait_until_exited(&child), "the CLI kept running");
    }

    #[test]
    fn dropping_a_request_whose_cli_closed_its_output_stops_it() {
        let marker = tempfile_dir("request-closed").join("closed");
        let request = Request::spawn(
            closes_output_and_hangs(&marker),
            Reply::new(Limits::DEFAULT.packet_detail(), |_| Ok::<(), String>(())),
            || {},
        )
        .expect("start");
        let child = request.child();
        wait_for_closed_output(&marker);
        assert!(request.poll().is_none(), "the hanging CLI gave a result");
        assert!(
            returns_promptly(move || drop(request)),
            "dropping the request waited for the process"
        );
        assert!(wait_until_exited(&child), "the CLI kept running");
    }

    #[cfg(unix)]
    #[test]
    fn rejects_documents_that_are_not_analysis_schema_2() {
        let dir = tempfile_dir("documents");
        for (document, expected) in [
            ("{}", "Unreadable analysis output"),
            (
                r#"{"schema_version":"1.0","output":"analysis","input":{"path":"a","file_size_bytes":0,"offset_bytes":0},"status":"complete","continuity_events":[],"programs":[],"warnings":[]}"#,
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
        let caps = r#"{"capabilities_version":"1.0","cli_version":"0.9","outputs":[{"name":"analysis","format":"json","schema_version":"2.0"}],"progress_formats":[]}"#;
        std::fs::write(dir.join("caps.json"), caps).unwrap();
        let cli = fake_cli(&dir, &format!("cat '{}'", dir.join("caps.json").display()));
        let Err(err) = check(&cli.path) else {
            panic!("accepted")
        };
        assert!(err.contains("and no --progress json"), "{err}");
    }

    /// A CLI from before schema 2.0 (such as 2.4.0) is rejected with a
    /// request to update it; a CLI without the analysis output as well.
    #[cfg(unix)]
    #[test]
    fn rejects_a_cli_with_analysis_schema_1() {
        let dir = tempfile_dir("caps-1");
        for (outputs, expected) in [
            (
                r#"[{"name":"analysis","format":"json","schema_version":"1.0"}]"#,
                "CLI 2.4.0 writes schema 1.0 JSON --output analysis; this GUI needs analysis schema 2.x",
            ),
            (
                "[]",
                "CLI 2.4.0 writes no JSON --output analysis; this GUI needs analysis schema 2.x",
            ),
        ] {
            let caps = format!(
                r#"{{"capabilities_version":"1.0","cli_version":"2.4.0","outputs":{outputs},"progress_formats":["json"]}}"#
            );
            std::fs::write(dir.join("caps.json"), caps).unwrap();
            let cli = fake_cli(&dir, &format!("cat '{}'", dir.join("caps.json").display()));
            let Err(err) = check(&cli.path) else {
                panic!("accepted")
            };
            assert!(err.contains(expected), "{err}");
            assert!(err.contains("Update the mpeg-ts-analyzer CLI"), "{err}");
        }
    }

    fn tempfile_dir(name: &str) -> std::path::PathBuf {
        let dir = std::env::temp_dir().join(format!("mtsa-gui-test-{name}-{}", std::process::id()));
        std::fs::create_dir_all(&dir).expect("temp dir");
        dir
    }

    #[test]
    fn finds_the_cli_in_order() {
        let bundled = [PathBuf::from("/opt/app/mpeg-ts-analyzer")];
        let exists = |path: &Path| path == bundled[0];
        let env = Some(std::ffi::OsString::from("/dev/cli"));
        assert_eq!(
            resolve(env, "/set/cli", &bundled, exists),
            (Source::Environment, PathBuf::from("/dev/cli"))
        );
        assert_eq!(
            resolve(Some("".into()), " /set/cli ", &bundled, exists),
            (Source::Settings, PathBuf::from("/set/cli"))
        );
        assert_eq!(
            resolve(None, "", &bundled, exists),
            (Source::Bundled, bundled[0].clone())
        );
        assert_eq!(
            resolve(None, "", &bundled, |_| false),
            (Source::Path, PathBuf::from(CLI_NAME))
        );
    }

    #[test]
    fn looks_for_a_bundled_cli_next_to_the_gui() {
        let name = format!("{CLI_NAME}{}", std::env::consts::EXE_SUFFIX);
        let app = Path::new("/Applications/Analyzer.app/Contents/MacOS/mpeg-ts-analyzer-gui");
        assert_eq!(
            bundled_candidates(Some(app), None),
            [
                Path::new("/Applications/Analyzer.app/Contents/MacOS").join(&name),
                Path::new("/Applications/Analyzer.app/Contents/Resources").join(&name),
            ]
        );
        let installed = Path::new("/usr/bin/mpeg-ts-analyzer-gui");
        assert_eq!(
            bundled_candidates(Some(installed), Some("/tmp/.mount_x".into())),
            [
                Path::new("/usr/lib/mpeg-ts-analyzer-gui").join(&name),
                Path::new("/usr/bin").join(&name),
                Path::new("/tmp/.mount_x/usr/lib/mpeg-ts-analyzer-gui").join(&name),
            ]
        );
        // With the CLI's own .deb installed too, the GUI's copy still wins.
        let candidates = bundled_candidates(Some(installed), None);
        assert_eq!(
            resolve(None, "", &candidates, |_| true),
            (
                Source::Bundled,
                Path::new("/usr/lib/mpeg-ts-analyzer-gui").join(&name)
            )
        );
        let portable = Path::new("C:/tools/mpeg-ts-analyzer/mpeg-ts-analyzer-gui.exe");
        assert_eq!(
            bundled_candidates(Some(portable), None),
            [Path::new("C:/tools/mpeg-ts-analyzer").join(&name)]
        );
        assert!(bundled_candidates(None, None).is_empty());
    }

    /// The program exits, but a process it started keeps stdout open.
    #[cfg(unix)]
    #[test]
    fn gives_up_when_the_output_stays_open() {
        let dir = tempfile_dir("open-stdout");
        // The shell exits at once; the timeout leaves it ample time to do so
        // even on a loaded machine, so the open output is what runs it out.
        let cli = fake_cli(&dir, "sleep 10 &\nexit 0");
        let started = Instant::now();
        let Err(err) = check_with_timeout(&cli.path, Duration::from_secs(2)) else {
            panic!("accepted")
        };
        assert!(err.contains("did not finish"), "{err}");
        assert!(started.elapsed() < Duration::from_secs(8));
    }

    #[cfg(unix)]
    #[test]
    fn gives_up_on_a_program_that_does_not_answer() {
        let dir = tempfile_dir("hang");
        let cli = fake_cli(&dir, "exec sleep 30");
        let started = Instant::now();
        let Err(err) = check_with_timeout(&cli.path, Duration::from_millis(200)) else {
            panic!("accepted")
        };
        assert!(err.contains("did not answer"), "{err}");
        assert!(started.elapsed() < Duration::from_secs(5));
    }

    /// Limits small enough that the tests never write anything near the
    /// production sizes.
    fn small_limits(bytes: u64) -> Limits {
        Limits {
            capabilities: bytes,
            packets_base: bytes,
            packets_per_packet: 0,
            packet_detail: bytes,
            analysis_base: bytes,
            analysis_per_input_byte: 0,
            diagnostics: 1024,
            diagnostic_line: 256,
        }
    }

    /// Writes `document` padded with spaces to exactly `len` bytes.
    fn padded(dir: &Path, name: &str, document: &[u8], len: usize) -> PathBuf {
        assert!(document.len() <= len, "{name} is longer than {len}");
        let mut content = document.to_vec();
        content.resize(len, b' ');
        let path = dir.join(name);
        std::fs::write(&path, content).expect("write the document");
        path
    }

    fn wait_request<T: Send + 'static>(request: &Request<T>) -> Result<T, String> {
        let deadline = Instant::now() + Duration::from_secs(60);
        loop {
            if let Some(result) = request.poll() {
                return result;
            }
            assert!(Instant::now() < deadline, "the request timed out");
            thread::sleep(Duration::from_millis(10));
        }
    }

    const CAPABILITIES: &[u8] = include_bytes!("../tests/fixtures/capabilities.json");
    const PACKETS_PAGE: &[u8] = include_bytes!("../tests/fixtures/packets_page_188.json");
    const PACKET_DETAIL: &[u8] = include_bytes!("../tests/fixtures/packet_detail_pat_188.json");
    const ANALYSIS: &[u8] = include_bytes!("../tests/fixtures/analysis_two_programs_188.json");

    #[test]
    fn capabilities_are_read_up_to_their_limit() {
        let dir = tempfile_dir("caps-limit");
        let limit = 4096;
        let fits = padded(&dir, "fits.json", CAPABILITIES, limit);
        let cli = check_command(
            process::prints_file(&fits),
            &fits,
            Duration::from_secs(30),
            &small_limits(limit as u64),
        )
        .expect("a reply of exactly the limit is read");
        assert!(cli.packet_outputs);

        let over = padded(&dir, "over.json", CAPABILITIES, limit + 1);
        let Err(err) = check_command(
            process::prints_file(&over),
            &over,
            Duration::from_secs(30),
            &small_limits(limit as u64),
        ) else {
            panic!("accepted")
        };
        assert!(
            err.contains(
                "wrote more than 4 KiB of --capabilities JSON (the limit for a capabilities reply)"
            ),
            "{err}"
        );
    }

    #[test]
    fn packet_replies_are_read_up_to_their_limit() {
        let dir = tempfile_dir("packets-limit");
        let limit = 64 * 1024;
        let limits = small_limits(limit as u64);
        for (fits, expected) in [(true, None), (false, Some("--output packets JSON"))] {
            let len = if fits { limit } else { limit + 1 };
            let file = padded(&dir, "page.json", PACKETS_PAGE, len);
            let reply = Reply::packets_page(100).with_limit(limits.packets_page(100));
            let request = Request::spawn(process::prints_file(&file), reply, || {}).unwrap();
            let result = wait_request(&request);
            match expected {
                None => assert!(!result.expect("page").packets.is_empty()),
                Some(text) => {
                    let err = result.expect_err("rejected");
                    assert!(err.contains(text), "{err}");
                    assert!(err.contains("the limit for a page of 100 packets"), "{err}");
                }
            }
        }
        for (fits, expected) in [(true, None), (false, Some("--output packet JSON"))] {
            let len = if fits { limit } else { limit + 1 };
            let file = padded(&dir, "detail.json", PACKET_DETAIL, len);
            let reply = Reply::packet_detail().with_limit(limits.packet_detail());
            let request = Request::spawn(process::prints_file(&file), reply, || {}).unwrap();
            match (wait_request(&request), expected) {
                (Ok(detail), None) => assert!(detail.packet.is_some()),
                (Err(err), Some(text)) => {
                    assert!(err.contains(text), "{err}");
                    assert!(err.contains("the limit for one decoded packet"), "{err}");
                }
                (result, _) => panic!("{:?}", result.err()),
            }
        }
    }

    #[test]
    fn analysis_output_is_read_up_to_its_limit() {
        let dir = tempfile_dir("analysis-limit");
        let limit = ANALYSIS.len() + 100;
        let limits = small_limits(limit as u64);
        let fits = padded(&dir, "fits.json", ANALYSIS, limit);
        let mut job =
            AnalysisJob::spawn(process::prints_file(&fits), limits, 1000, || {}).expect("start");
        let report = wait(&mut job).0.expect("a document of exactly the limit");
        assert_eq!(report.programs.len(), 2);

        let over = padded(&dir, "over.json", ANALYSIS, limit + 1);
        let mut job =
            AnalysisJob::spawn(process::prints_file(&over), limits, 1000, || {}).expect("start");
        let err = wait(&mut job).0.expect_err("rejected");
        assert!(
            err.contains("of --output analysis JSON (the limit for a 1000 bytes range)"),
            "{err}"
        );
        assert!(err.contains("Analyze a smaller range"), "{err}");
    }

    /// The limit grows with the analyzed range.
    #[test]
    fn the_analysis_limit_scales_with_the_range() {
        let limits = Limits {
            analysis_per_input_byte: 2,
            ..small_limits(100)
        };
        assert_eq!(limits.analysis(1000).bytes, 2100);
    }

    /// A CLI that writes without end has its output rejected at the limit
    /// and is stopped, without the GUI holding more than the limit.
    #[test]
    fn endless_analysis_output_is_rejected_and_stopped() {
        let mut job = AnalysisJob::spawn(
            process::fake_process("stdout-forever"),
            small_limits(256 * 1024),
            1000,
            || {},
        )
        .expect("start");
        let child = job.child();
        let err = wait(&mut job).0.expect_err("rejected");
        assert!(
            err.contains("more than 256 KiB of --output analysis JSON"),
            "{err}"
        );
        assert!(wait_until_released(&child), "the CLI kept running");
    }

    /// Only a marked tail of a flood of diagnostics is kept, and an endless
    /// line is cut.
    #[test]
    fn keeps_a_bounded_tail_of_diagnostic_spam() {
        let mut job = AnalysisJob::spawn(
            process::fake_process("diagnostics:5000"),
            small_limits(1024 * 1024),
            1000,
            || {},
        )
        .expect("start");
        let err = wait(&mut job).0.expect_err("the fake CLI fails");
        assert!(err.starts_with("The CLI failed ("), "{err}");
        assert!(err.contains(" earlier diagnostic lines ("), "{err}");
        assert!(err.contains("omitted …]"), "{err}");
        assert!(err.contains("diagnostic line 4999"), "{err}");
        assert!(
            err.contains("[… 4744 more bytes of this line omitted]"),
            "{err}"
        );
        // The 1 KiB tail, its marker, and the start of the message.
        assert!(err.len() < 1500, "{} bytes kept", err.len());
    }

    /// A flood of progress events is coalesced to the latest one; the UI is
    /// asked to repaint once per event it takes, not once per event written.
    #[test]
    fn coalesces_progress_spam() {
        let events = 20_000;
        let repaints = Arc::new(std::sync::atomic::AtomicUsize::new(0));
        let counter = Arc::clone(&repaints);
        let mut job = AnalysisJob::spawn(
            process::fake_process(&format!("progress:{events}")),
            Limits::DEFAULT,
            1000,
            move || {
                counter.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
            },
        )
        .expect("start");
        let deadline = Instant::now() + Duration::from_secs(60);
        let mut polls = 0;
        loop {
            polls += 1;
            if let JobUpdate::Running(Some(progress)) = job.poll()
                && progress.processed_bytes == events
            {
                assert!(progress.done);
                break;
            }
            assert!(Instant::now() < deadline, "the progress did not arrive");
            thread::sleep(Duration::from_millis(20));
        }
        let repaints = repaints.load(std::sync::atomic::Ordering::SeqCst);
        assert!(
            repaints <= polls + 1,
            "{repaints} repaints for {polls} polls"
        );
        let child = job.child();
        drop(job);
        assert!(wait_until_released(&child), "the CLI kept running");
    }

    /// A Unix file name need not be UTF-8; the CLI is given the name as is.
    #[cfg(unix)]
    #[test]
    fn passes_a_non_utf8_input_path_to_the_analysis() {
        use std::os::unix::ffi::{OsStrExt, OsStringExt};
        let dir = tempfile_dir("non-utf8-analysis");
        let document = dir.join("doc.json");
        std::fs::write(&document, ANALYSIS).unwrap();
        let recorded = dir.join("arg");
        let cli = fake_cli(
            &dir,
            &format!(
                "printf '%s' \"$1\" > '{}'\ncat '{}'",
                recorded.display(),
                document.display()
            ),
        );
        let input = dir.join(std::ffi::OsString::from_vec(b"caf\xe9.ts".to_vec()));
        assert!(input.to_str().is_none());
        let mut job = AnalysisJob::start(&cli, &input, 0, 1000, || {}).expect("start");
        wait(&mut job).0.expect("report");
        assert_eq!(
            std::fs::read(&recorded).unwrap(),
            input.as_os_str().as_bytes()
        );
    }

    #[test]
    fn rejects_a_missing_cli() {
        let err = check(Path::new("/no/such/mpeg-ts-analyzer"))
            .err()
            .expect("error");
        assert!(err.contains("Cannot run"));
    }
}
