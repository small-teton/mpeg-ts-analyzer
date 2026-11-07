//! One CLI run in a child process, owned by a supervisor thread.
//!
//! The supervisor thread is the only owner of the [`Child`]: it polls the
//! process, reaps it, and kills it when asked. The UI thread only sends a
//! message, so cancelling never waits for the process or for a lock that a
//! waiting thread holds, even when the CLI has closed its output and keeps
//! running. stdout and stderr are drained on threads of their own, so neither
//! a full pipe nor one closed early holds up the other or the supervisor.
//!
//! The readers stop at their next read once the run is cancelled, and the
//! kill ends a producer that never finishes, so a cancelled or superseded run
//! releases its partly read output promptly. What a reader keeps is bounded
//! by the reader itself (see [`super::bounded`]); when it refuses the output
//! (more than its limit), the process is killed rather than waited for.

use std::io::{self, Read};
use std::process::{ChildStderr, ChildStdout, Command, ExitStatus, Stdio};
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{self, RecvTimeoutError, Sender};
use std::thread;
use std::time::Duration;

/// How often the supervisor checks whether the process has exited. A cancel
/// request is acted on as soon as it arrives.
const POLL_INTERVAL: Duration = Duration::from_millis(20);

/// One of the process's output pipes. Reading fails once the run has been
/// cancelled, so a reader that is still going gives up and frees what it holds.
pub struct Pipe<R> {
    inner: R,
    stop: Arc<AtomicBool>,
}

impl<R: Read> Read for Pipe<R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        if self.stop.load(Ordering::SeqCst) {
            return Err(io::Error::other("the CLI run was cancelled"));
        }
        self.inner.read(buf)
    }
}

/// What the stdout reader made of the output.
pub struct Output<T> {
    pub value: T,
    /// The output was refused (it went past its limit): the process is killed
    /// instead of being waited for, and its diagnostics are not waited for.
    pub refused: bool,
}

/// Reads a process's stdout to its end.
pub type StdoutReader<T> = Box<dyn FnOnce(Pipe<ChildStdout>) -> Output<T> + Send>;
/// Reads a process's stderr to its end and returns the diagnostics to keep.
pub type StderrReader = Box<dyn FnOnce(Pipe<ChildStderr>) -> String + Send>;

/// What a run that ended by itself left behind.
pub struct Finished<T> {
    pub status: io::Result<ExitStatus>,
    pub stdout: T,
    /// What the stderr reader returned; empty when stderr was not read or the
    /// output was refused.
    pub diagnostics: String,
}

enum Event<T> {
    Cancel,
    Stdout(Output<T>),
    Stderr(String),
}

/// A running CLI process. Dropping it cancels the run: the process is killed
/// and reaped, and the result is not delivered.
pub struct Process<T> {
    control: Sender<Event<T>>,
    stop: Arc<AtomicBool>,
    #[cfg(test)]
    exited: ExitProbe,
}

impl<T: Send + 'static> Process<T> {
    /// Starts `command` with stdout piped to `stdout`, and stderr piped to
    /// `stderr` when given (otherwise discarded). Once the process has exited
    /// and both outputs are read to their end, `finish` runs on the supervisor
    /// thread; it does not run for a cancelled process.
    pub fn spawn(
        mut command: Command,
        stdout: StdoutReader<T>,
        stderr: Option<StderrReader>,
        finish: impl FnOnce(Finished<T>) + Send + 'static,
    ) -> io::Result<Self> {
        let mut child = command
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(if stderr.is_some() {
                Stdio::piped()
            } else {
                Stdio::null()
            })
            .spawn()?;
        let (control, events) = mpsc::channel();
        let stop = Arc::new(AtomicBool::new(false));
        #[cfg(test)]
        let exited = ExitProbe::default();

        let pipe = Pipe {
            inner: child.stdout.take().expect("stdout is piped"),
            stop: Arc::clone(&stop),
        };
        let stdout_events = control.clone();
        #[cfg(test)]
        let reader = exited.reader();
        thread::spawn(move || {
            let _ = stdout_events.send(Event::Stdout(stdout(pipe)));
            #[cfg(test)]
            drop(reader);
        });
        let mut diagnostics = match (stderr, child.stderr.take()) {
            (Some(read), Some(inner)) => {
                let pipe = Pipe {
                    inner,
                    stop: Arc::clone(&stop),
                };
                let stderr_events = control.clone();
                #[cfg(test)]
                let reader = exited.reader();
                thread::spawn(move || {
                    let _ = stderr_events.send(Event::Stderr(read(pipe)));
                    #[cfg(test)]
                    drop(reader);
                });
                None
            }
            _ => Some(String::new()),
        };

        #[cfg(test)]
        let reaped = exited.clone();
        let stopped = Arc::clone(&stop);
        thread::spawn(move || {
            let mut status: Option<io::Result<ExitStatus>> = None;
            let mut stdout = None;
            loop {
                match events.recv_timeout(POLL_INTERVAL) {
                    // The handle is gone, or asked for the run to stop.
                    Ok(Event::Cancel) | Err(RecvTimeoutError::Disconnected) => {
                        stopped.store(true, Ordering::SeqCst);
                        if status.is_none() {
                            let _ = child.kill();
                            let _ = child.wait();
                        }
                        #[cfg(test)]
                        reaped.set();
                        return;
                    }
                    Ok(Event::Stdout(output)) => {
                        if output.refused {
                            // Nothing more is wanted from this run.
                            stopped.store(true, Ordering::SeqCst);
                            if status.is_none() {
                                let _ = child.kill();
                                status = Some(child.wait());
                            }
                            diagnostics.get_or_insert_with(String::new);
                        }
                        stdout = Some(output.value);
                    }
                    Ok(Event::Stderr(text)) => diagnostics = Some(text),
                    Err(RecvTimeoutError::Timeout) => {}
                }
                if status.is_none() {
                    status = child.try_wait().transpose();
                }
                if status.is_some() && stdout.is_some() && diagnostics.is_some() {
                    break;
                }
            }
            #[cfg(test)]
            reaped.set();
            finish(Finished {
                status: status.expect("the process has exited"),
                stdout: stdout.expect("stdout has been read"),
                diagnostics: diagnostics.unwrap_or_default(),
            });
        });

        Ok(Self {
            control,
            stop,
            #[cfg(test)]
            exited,
        })
    }
}

impl<T> Process<T> {
    /// Asks the supervisor to kill the process. Returns at once.
    pub fn cancel(&self) {
        // The readers give up at their next read.
        self.stop.store(true, Ordering::SeqCst);
        // The supervisor is gone once the run has ended; nothing to stop then.
        let _ = self.control.send(Event::Cancel);
    }

    /// Whether the process has exited and been reaped, for tests.
    #[cfg(test)]
    pub fn exited(&self) -> ExitProbe {
        self.exited.clone()
    }
}

impl<T> Drop for Process<T> {
    fn drop(&mut self) {
        self.cancel();
    }
}

/// Set by the supervisor once the process has exited and been reaped; also
/// counts the output readers still running.
#[cfg(test)]
#[derive(Clone, Default)]
pub struct ExitProbe(Arc<ProbeState>);

#[cfg(test)]
#[derive(Default)]
pub struct ProbeState {
    reaped: AtomicBool,
    readers: std::sync::atomic::AtomicUsize,
}

/// Held by a running output reader.
#[cfg(test)]
struct ReaderToken(ExitProbe);

#[cfg(test)]
impl Drop for ReaderToken {
    fn drop(&mut self) {
        self.0.0.readers.fetch_sub(1, Ordering::SeqCst);
    }
}

#[cfg(test)]
impl ExitProbe {
    fn set(&self) {
        self.0.reaped.store(true, Ordering::SeqCst);
    }

    fn reader(&self) -> ReaderToken {
        self.0.readers.fetch_add(1, Ordering::SeqCst);
        ReaderToken(self.clone())
    }

    pub fn is_set(&self) -> bool {
        self.0.reaped.load(Ordering::SeqCst)
    }

    /// Whether the process was reaped and its output readers have ended,
    /// releasing what they had read.
    pub fn is_released(&self) -> bool {
        self.is_set() && self.0.readers.load(Ordering::SeqCst) == 0
    }
}

/// Waits until a killed or finished process has been reaped.
#[cfg(test)]
pub fn wait_until_exited(probe: &ExitProbe) -> bool {
    wait_for(|| probe.is_set())
}

/// Waits until a process has been reaped and its output readers have ended.
#[cfg(test)]
pub fn wait_until_released(probe: &ExitProbe) -> bool {
    wait_for(|| probe.is_released())
}

#[cfg(test)]
fn wait_for(done: impl Fn() -> bool) -> bool {
    let deadline = std::time::Instant::now() + Duration::from_secs(10);
    while std::time::Instant::now() < deadline {
        if done() {
            return true;
        }
        thread::sleep(Duration::from_millis(20));
    }
    false
}

/// Environment variable that turns this test executable into a fake CLI.
#[cfg(test)]
const FAKE_ENV: &str = "MTSA_GUI_FAKE_PROCESS";
/// File the fake CLI creates once it has closed its output.
#[cfg(test)]
const FAKE_MARKER_ENV: &str = "MTSA_GUI_FAKE_PROCESS_MARKER";

/// A portable fake CLI: this test executable, running only
/// [`tests::fake_process_entry`] in the given mode. Its stdout starts with
/// the test harness's own lines, so it is not a JSON document.
///
/// - `close-output-and-hang`: closes stdout and stderr and stays alive.
/// - `diagnostics:N`: writes N short stderr lines and one 5000-byte line
///   without a newline, then exits with status 3.
/// - `diagnostics-forever`: writes stderr lines until it is killed.
/// - `progress:N`: writes N `--progress json` events and stays alive.
/// - `stdout-forever`: writes stdout until it is killed.
#[cfg(test)]
pub fn fake_process(mode: &str) -> Command {
    let mut command = Command::new(std::env::current_exe().expect("the test executable"));
    command
        .args([
            "--exact",
            "cli::process::tests::fake_process_entry",
            "--ignored",
            "--nocapture",
            "--test-threads=1",
        ])
        .env(FAKE_ENV, mode);
    command
}

/// The `close-output-and-hang` fake CLI, which creates `marker` once it has
/// closed its output.
#[cfg(test)]
pub fn closes_output_and_hangs(marker: &std::path::Path) -> Command {
    let _ = std::fs::remove_file(marker);
    let mut command = fake_process("close-output-and-hang");
    command.env(FAKE_MARKER_ENV, marker);
    command
}

/// A command that writes the file to stdout, on Unix and on Windows.
#[cfg(test)]
pub fn prints_file(path: &std::path::Path) -> Command {
    #[cfg(windows)]
    let command = {
        let mut command = Command::new("cmd");
        command.args(["/D", "/C", "type"]).arg(path);
        command
    };
    #[cfg(not(windows))]
    let command = {
        let mut command = Command::new("cat");
        command.arg(path);
        command
    };
    command
}

/// Waits until the fake CLI from [`closes_output_and_hangs`] has closed its
/// output, and then a little longer, so the readers have seen the end of it.
#[cfg(test)]
pub fn wait_for_closed_output(marker: &std::path::Path) {
    let deadline = std::time::Instant::now() + Duration::from_secs(30);
    while !marker.exists() {
        assert!(
            std::time::Instant::now() < deadline,
            "the fake CLI did not close its output"
        );
        thread::sleep(Duration::from_millis(10));
    }
    thread::sleep(Duration::from_millis(300));
}

/// Runs `stop` on another thread and checks that it returns promptly: the UI
/// thread must never wait for the process.
#[cfg(test)]
pub fn returns_promptly(stop: impl FnOnce() + Send + 'static) -> bool {
    let (done_tx, done) = mpsc::channel();
    thread::spawn(move || {
        stop();
        let _ = done_tx.send(());
    });
    done.recv_timeout(Duration::from_secs(2)).is_ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    /// The body of the fake CLI; does nothing in an ordinary test run.
    #[test]
    #[ignore = "runs as a fake CLI process"]
    fn fake_process_entry() {
        let Some(mode) = std::env::var_os(FAKE_ENV) else {
            return;
        };
        let mode = mode.to_string_lossy().into_owned();
        let (mode, count) = mode.split_once(':').unwrap_or((&mode, "0"));
        let count: u64 = count.parse().expect("a count");
        match mode {
            "close-output-and-hang" => {
                close_stdout_and_stderr();
                if let Some(marker) = std::env::var_os(FAKE_MARKER_ENV) {
                    std::fs::write(marker, b"closed").expect("write the marker");
                }
            }
            "diagnostics" => {
                let mut stderr = io::stderr().lock();
                for n in 0..count {
                    let _ = writeln!(stderr, "diagnostic line {n}");
                }
                let _ = stderr.write_all(&[b'x'; 5000]);
                let _ = stderr.flush();
                std::process::exit(3);
            }
            "diagnostics-forever" => {
                let mut stderr = io::stderr().lock();
                while writeln!(stderr, "diagnostic line").is_ok() {}
            }
            "progress" => {
                let mut stderr = io::stderr().lock();
                for n in 1..=count {
                    let _ = writeln!(
                        stderr,
                        r#"{{"type":"progress","processed_bytes":{n},"total_bytes":{count},"done":{}}}"#,
                        n == count
                    );
                }
                let _ = stderr.flush();
            }
            "stdout-forever" => {
                let mut stdout = io::stdout().lock();
                while stdout.write_all(&[b' '; 4096]).is_ok() {}
            }
            other => panic!("unknown fake CLI mode {other}"),
        }
        thread::sleep(Duration::from_secs(60));
    }

    #[cfg(unix)]
    fn close_stdout_and_stderr() {
        use std::os::fd::{FromRawFd, OwnedFd};
        for fd in [1, 2] {
            // SAFETY: test-only fake CLI; nothing in this process uses the
            // standard output or error again, so closing them is sound.
            drop(unsafe { OwnedFd::from_raw_fd(fd) });
        }
    }

    #[cfg(windows)]
    fn close_stdout_and_stderr() {
        use std::os::windows::io::{AsRawHandle, FromRawHandle, OwnedHandle};
        for handle in [io::stdout().as_raw_handle(), io::stderr().as_raw_handle()] {
            // SAFETY: test-only fake CLI; nothing in this process uses the
            // standard output or error again, so closing them is sound.
            drop(unsafe { OwnedHandle::from_raw_handle(handle) });
        }
    }

    fn marker(name: &str) -> std::path::PathBuf {
        let dir = std::env::temp_dir().join(format!(
            "mtsa-gui-test-process-{name}-{}",
            std::process::id()
        ));
        std::fs::create_dir_all(&dir).expect("temp dir");
        dir.join("closed")
    }

    fn read_all(mut pipe: Pipe<ChildStdout>) -> Output<io::Result<Vec<u8>>> {
        let mut out = Vec::new();
        Output {
            value: pipe.read_to_end(&mut out).map(|_| out),
            refused: false,
        }
    }

    #[test]
    fn cancels_a_process_that_closed_its_output_and_hangs() {
        let marker = marker("cancel");
        let (finished_tx, finished) = mpsc::channel();
        let process = Process::spawn(
            closes_output_and_hangs(&marker),
            Box::new(read_all),
            Some(Box::new(|mut stderr| {
                let mut text = String::new();
                let _ = stderr.read_to_string(&mut text);
                text
            })),
            move |_| {
                let _ = finished_tx.send(());
            },
        )
        .expect("spawn");
        let exited = process.exited();
        wait_for_closed_output(&marker);
        assert!(!exited.is_set(), "the fake CLI ended by itself");
        assert!(
            returns_promptly(move || process.cancel()),
            "cancel waited for the process"
        );
        assert!(wait_until_released(&exited), "the process was not killed");
        assert!(
            finished.recv_timeout(Duration::from_millis(200)).is_err(),
            "a cancelled run delivered a result"
        );
    }

    #[test]
    fn delivers_the_output_of_a_finished_process() {
        let (finished_tx, finished) = mpsc::channel();
        let mut command = Command::new(std::env::current_exe().expect("test executable"));
        command.args(["--list", "--format", "terse"]);
        let process = Process::spawn(command, Box::new(read_all), None, move |run| {
            let _ = finished_tx.send(run);
        })
        .expect("spawn");
        let run = finished
            .recv_timeout(Duration::from_secs(30))
            .expect("the process finished");
        assert!(run.status.expect("status").success());
        let stdout = String::from_utf8(run.stdout.expect("stdout")).expect("text");
        assert!(stdout.contains("fake_process_entry"), "{stdout}");
        assert!(run.diagnostics.is_empty());
        assert!(wait_until_released(&process.exited()));
    }

    /// A producer that never finishes: cancelling stops it and ends both
    /// readers, which drop what they had read.
    #[test]
    fn cancelling_an_endless_producer_releases_its_readers() {
        for mode in ["stdout-forever", "diagnostics-forever"] {
            let process = Process::spawn(
                fake_process(mode),
                Box::new(|mut pipe| Output {
                    value: io::copy(&mut pipe, &mut io::sink()).map(|_| ()),
                    refused: false,
                }),
                Some(Box::new(|mut pipe| {
                    let _ = io::copy(&mut pipe, &mut io::sink());
                    String::new()
                })),
                |_| {},
            )
            .expect("spawn");
            let exited = process.exited();
            thread::sleep(Duration::from_millis(300));
            assert!(!exited.is_set(), "{mode}: the fake CLI ended by itself");
            assert!(returns_promptly(move || drop(process)), "{mode}");
            assert!(
                wait_until_released(&exited),
                "{mode}: the readers kept running"
            );
        }
    }

    /// A reader that refuses the output has the process killed at once,
    /// without waiting for it to end by itself.
    #[test]
    fn refusing_the_output_stops_the_process() {
        let (finished_tx, finished) = mpsc::channel();
        let process = Process::spawn(
            fake_process("stdout-forever"),
            Box::new(|mut pipe| {
                let mut first = [0; 64];
                let _ = pipe.read_exact(&mut first);
                Output {
                    value: "refused",
                    refused: true,
                }
            }),
            Some(Box::new(|mut pipe| {
                let _ = io::copy(&mut pipe, &mut io::sink());
                String::new()
            })),
            move |run| {
                let _ = finished_tx.send(run.stdout);
            },
        )
        .expect("spawn");
        assert_eq!(
            finished.recv_timeout(Duration::from_secs(30)),
            Ok("refused")
        );
        assert!(wait_until_released(&process.exited()));
    }
}
