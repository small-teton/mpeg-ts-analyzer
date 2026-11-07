//! Running the mpeg-ts-analyzer CLI as a subprocess.

mod process;

use std::io::{BufRead, BufReader, Read};
use std::path::{Path, PathBuf};
use std::process::{ChildStderr, Command, Stdio};
use std::sync::mpsc::{self, Receiver, TryRecvError};
use std::thread;
use std::time::{Duration, Instant};

use crate::report::{self, AnalysisReport, Capabilities, ProgressEvent};
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
    let mut child = command(path)
        .arg("--capabilities")
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
    let mut stdout = child.stdout.take().expect("stdout is piped");
    // The output is read on a thread and waited for with the same deadline: a
    // program can exit while a process it started keeps stdout open.
    let (output_tx, output) = mpsc::channel();
    thread::spawn(move || {
        let mut out = Vec::new();
        let _ = output_tx.send(stdout.read_to_end(&mut out).map(|_| out));
    });
    let deadline = Instant::now() + timeout;
    let status = loop {
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
    let stdout = output
        .recv_timeout(deadline.saturating_duration_since(Instant::now()))
        .map_err(|_| {
            format!(
                "{} did not finish its --capabilities output within {} s; is it the mpeg-ts-analyzer CLI?",
                path.display(),
                timeout.as_secs()
            )
        })?
        .map_err(|err| format!("Cannot read the --capabilities output: {err}"))?;
    if !status.success() {
        return Err(format!(
            "{} does not support --capabilities; a newer CLI is needed.",
            path.display()
        ));
    }
    let caps: Capabilities = serde_json::from_slice(&stdout)
        .map_err(|err| format!("Unreadable --capabilities output: {err}"))?;
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

/// A range analysis running in a child process.
pub struct AnalysisJob {
    /// Dropping the job (closing the app, opening another file) kills the
    /// process, since dropping a `Child` does not.
    process: Process,
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
        Self::spawn(command, repaint)
            .map_err(|err| format!("Cannot start {}: {err}", cli.path.display()))
    }

    fn spawn(
        command: Command,
        repaint: impl Fn() + Send + Clone + 'static,
    ) -> std::io::Result<Self> {
        // stderr carries progress events and, on failure, diagnostics.
        let (progress_tx, progress) = mpsc::channel();
        let stderr_repaint = repaint.clone();
        let read_stderr = move |stderr: ChildStderr| {
            let mut diagnostics = String::new();
            for line in BufReader::new(stderr).lines().map_while(Result::ok) {
                match serde_json::from_str::<ProgressEvent>(&line) {
                    Ok(event) if event.kind == "progress" => {
                        let _ = progress_tx.send(event);
                        stderr_repaint();
                    }
                    _ => {
                        diagnostics.push_str(&line);
                        diagnostics.push('\n');
                    }
                }
            }
            diagnostics
        };

        // stdout carries the JSON document, written once the analysis ends.
        let (result_tx, result) = mpsc::channel();
        let process = Process::spawn(
            command,
            Some(Box::new(read_stderr)),
            move |Finished {
                      status,
                      stdout,
                      diagnostics,
                  }| {
                let outcome = match stdout {
                    Ok(json) if json.iter().any(|b| !b.is_ascii_whitespace()) => {
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

/// A short CLI run that writes one JSON document, such as a page of packets.
/// Dropping the request kills the process.
pub struct Request<T> {
    process: Process,
    result: Receiver<Result<T, String>>,
}

impl<T: Send + 'static> Request<T> {
    /// Runs the CLI with `args` and parses its stdout with `parse`.
    pub fn start(
        cli: &Cli,
        args: Vec<String>,
        parse: fn(&[u8]) -> Result<T, String>,
        repaint: impl Fn() + Send + 'static,
    ) -> Result<Self, String> {
        let mut command = command(&cli.path);
        command.args(&args);
        Self::spawn(command, parse, repaint)
            .map_err(|err| format!("Cannot start {}: {err}", cli.path.display()))
    }

    pub(crate) fn spawn(
        command: Command,
        parse: fn(&[u8]) -> Result<T, String>,
        repaint: impl Fn() + Send + 'static,
    ) -> std::io::Result<Self> {
        let (result_tx, result) = mpsc::channel();
        let process = Process::spawn(command, None, move |finished| {
            let outcome = match finished.stdout {
                Ok(json) if json.iter().any(|b| !b.is_ascii_whitespace()) => parse(&json),
                Ok(_) => Err("The CLI ended without a result.".to_owned()),
                Err(err) => Err(format!("Cannot read the CLI output: {err}")),
            };
            let _ = result_tx.send(outcome);
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
        let job = AnalysisJob::spawn(closes_output_and_hangs(&marker), || {}).expect("start");
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
        let job = AnalysisJob::spawn(closes_output_and_hangs(&marker), || {}).expect("start");
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
            |_| Ok::<(), String>(()),
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

    #[test]
    fn rejects_a_missing_cli() {
        let err = check(Path::new("/no/such/mpeg-ts-analyzer"))
            .err()
            .expect("error");
        assert!(err.contains("Cannot run"));
    }
}
