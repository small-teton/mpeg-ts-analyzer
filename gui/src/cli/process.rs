//! One CLI run in a child process, owned by a supervisor thread.
//!
//! The supervisor thread is the only owner of the [`Child`]: it polls the
//! process, reaps it, and kills it when asked. The UI thread only sends a
//! message, so cancelling never waits for the process or for a lock that a
//! waiting thread holds, even when the CLI has closed its output and keeps
//! running. stdout and stderr are drained on threads of their own, so neither
//! a full pipe nor one closed early holds up the other or the supervisor.

use std::io::{self, BufReader, Read};
use std::process::{ChildStderr, Command, ExitStatus, Stdio};
use std::sync::mpsc::{self, RecvTimeoutError, Sender};
use std::thread;
use std::time::Duration;

/// How often the supervisor checks whether the process has exited. A cancel
/// request is acted on as soon as it arrives.
const POLL_INTERVAL: Duration = Duration::from_millis(20);

/// Reads a process's stderr to its end and returns the diagnostics to keep.
pub type StderrReader = Box<dyn FnOnce(ChildStderr) -> String + Send>;

/// What a run that ended by itself left behind.
pub struct Finished {
    pub status: io::Result<ExitStatus>,
    pub stdout: io::Result<Vec<u8>>,
    /// What the stderr reader returned; empty when stderr was not read.
    pub diagnostics: String,
}

enum Event {
    Cancel,
    Stdout(io::Result<Vec<u8>>),
    Stderr(String),
}

/// A running CLI process. Dropping it cancels the run: the process is killed
/// and reaped, and the result is not delivered.
pub struct Process {
    control: Sender<Event>,
    #[cfg(test)]
    exited: ExitProbe,
}

impl Process {
    /// Starts `command` with stdout piped, and stderr piped to `stderr` when
    /// given (otherwise discarded). Once the process has exited and both
    /// outputs are read to their end, `finish` runs on the supervisor thread;
    /// it does not run for a cancelled process.
    pub fn spawn(
        mut command: Command,
        stderr: Option<StderrReader>,
        finish: impl FnOnce(Finished) + Send + 'static,
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

        let stdout = child.stdout.take().expect("stdout is piped");
        let stdout_events = control.clone();
        thread::spawn(move || {
            let mut out = Vec::new();
            let read = BufReader::new(stdout).read_to_end(&mut out).map(|_| out);
            let _ = stdout_events.send(Event::Stdout(read));
        });
        let mut diagnostics = match (stderr, child.stderr.take()) {
            (Some(read), Some(pipe)) => {
                let stderr_events = control.clone();
                thread::spawn(move || {
                    let _ = stderr_events.send(Event::Stderr(read(pipe)));
                });
                None
            }
            _ => Some(String::new()),
        };

        #[cfg(test)]
        let exited = ExitProbe::default();
        #[cfg(test)]
        let reaped = exited.clone();
        thread::spawn(move || {
            let mut status: Option<io::Result<ExitStatus>> = None;
            let mut stdout = None;
            loop {
                match events.recv_timeout(POLL_INTERVAL) {
                    // The handle is gone, or asked for the run to stop.
                    Ok(Event::Cancel) | Err(RecvTimeoutError::Disconnected) => {
                        if status.is_none() {
                            let _ = child.kill();
                            let _ = child.wait();
                        }
                        #[cfg(test)]
                        reaped.set();
                        return;
                    }
                    Ok(Event::Stdout(read)) => stdout = Some(read),
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
            #[cfg(test)]
            exited,
        })
    }

    /// Asks the supervisor to kill the process. Returns at once.
    pub fn cancel(&self) {
        // The supervisor is gone once the run has ended; nothing to stop then.
        let _ = self.control.send(Event::Cancel);
    }

    /// Whether the process has exited and been reaped, for tests.
    #[cfg(test)]
    pub fn exited(&self) -> ExitProbe {
        self.exited.clone()
    }
}

impl Drop for Process {
    fn drop(&mut self) {
        self.cancel();
    }
}

/// Set by the supervisor once the process has exited and been reaped.
#[cfg(test)]
#[derive(Clone, Default)]
pub struct ExitProbe(std::sync::Arc<std::sync::atomic::AtomicBool>);

#[cfg(test)]
impl ExitProbe {
    fn set(&self) {
        self.0.store(true, std::sync::atomic::Ordering::SeqCst);
    }

    pub fn is_set(&self) -> bool {
        self.0.load(std::sync::atomic::Ordering::SeqCst)
    }
}

/// Waits until a killed or finished process has been reaped.
#[cfg(test)]
pub fn wait_until_exited(probe: &ExitProbe) -> bool {
    let deadline = std::time::Instant::now() + Duration::from_secs(10);
    while std::time::Instant::now() < deadline {
        if probe.is_set() {
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
/// [`tests::fake_process_entry`], which closes its stdout and stderr, creates
/// `marker`, and then stays alive for a minute.
#[cfg(test)]
pub fn closes_output_and_hangs(marker: &std::path::Path) -> Command {
    let _ = std::fs::remove_file(marker);
    let mut command = Command::new(std::env::current_exe().expect("the test executable"));
    command
        .args([
            "--exact",
            "cli::process::tests::fake_process_entry",
            "--ignored",
            "--nocapture",
            "--test-threads=1",
        ])
        .env(FAKE_ENV, "close-output-and-hang")
        .env(FAKE_MARKER_ENV, marker);
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

    /// The body of the fake CLI; does nothing in an ordinary test run.
    #[test]
    #[ignore = "runs as a fake CLI process"]
    fn fake_process_entry() {
        if std::env::var_os(FAKE_ENV).is_none() {
            return;
        }
        close_stdout_and_stderr();
        if let Some(marker) = std::env::var_os(FAKE_MARKER_ENV) {
            std::fs::write(marker, b"closed").expect("write the marker");
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

    #[test]
    fn cancels_a_process_that_closed_its_output_and_hangs() {
        let marker = marker("cancel");
        let (finished_tx, finished) = mpsc::channel();
        let process = Process::spawn(
            closes_output_and_hangs(&marker),
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
        assert!(wait_until_exited(&exited), "the process was not killed");
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
        let process = Process::spawn(command, None, move |run| {
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
        assert!(wait_until_exited(&process.exited()));
    }
}
