//! In-app update from GitHub Releases (docs/gui/design.md, "Updates").
//!
//! The newest published `gui-vX.Y.Z` release is found through the Releases
//! API; its `_manifest.json` names the bundled CLI and its `_update.json` the
//! package for this platform and its signature. The GUI downloads the package
//! itself and verifies the signature against [`PUBLIC_KEY`], including the
//! file name the signature was made for; `cargo-packager-updater` only
//! installs the verified bytes. Every request uses [`client`], whose URLs are
//! checked with [`validate_url`] and whose redirects stay on GitHub.

use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::sync::mpsc::{Receiver, channel};
use std::time::Duration;

use cargo_packager_updater::reqwest::blocking::Client;
use cargo_packager_updater::reqwest::{header, redirect};
use cargo_packager_updater::semver::Version;
use cargo_packager_updater::url::Url;
use cargo_packager_updater::{
    Config, RemoteRelease, Update, UpdateFormat, WindowsConfig, WindowsUpdateInstallMode,
};
use eframe::egui;
use serde::Deserialize;

/// The repository whose releases are checked.
const OWNER_REPO: &str = "small-teton/mpeg-ts-analyzer";
const RELEASES_API: &str =
    "https://api.github.com/repos/small-teton/mpeg-ts-analyzer/releases?per_page=100";
/// The public half of the update signing key (`gui/updater.pub`), in
/// cargo-packager's Base64-wrapped minisign format.
const PUBLIC_KEY: &str = include_str!("../updater.pub");
const TIMEOUT: Duration = Duration::from_secs(30);

/// The version of this GUI.
fn current_version() -> Version {
    Version::parse(env!("CARGO_PKG_VERSION")).expect("CARGO_PKG_VERSION is semver")
}

fn user_agent() -> String {
    format!("mpeg-ts-analyzer-gui/{}", env!("CARGO_PKG_VERSION"))
}

// ---------------------------------------------------------------------------
// Finding a release

#[derive(Clone, Debug, Deserialize)]
struct Release {
    tag_name: String,
    #[serde(default)]
    draft: bool,
    #[serde(default)]
    prerelease: bool,
    html_url: String,
    #[serde(default)]
    assets: Vec<Asset>,
}

#[derive(Clone, Debug, Deserialize)]
struct Asset {
    name: String,
    browser_download_url: String,
}

/// The newest published `gui-vX.Y.Z` release newer than `current`, with its
/// version. Drafts, prereleases and other tags (the CLI's `vX.Y.Z`) are
/// ignored; equal and lower versions are never returned.
fn newest_release(releases: &[Release], current: &Version) -> Option<(Version, Release)> {
    releases
        .iter()
        .filter(|r| !r.draft && !r.prerelease)
        .filter_map(|r| Some((gui_tag_version(&r.tag_name)?, r)))
        .filter(|(version, _)| version > current)
        .max_by(|a, b| a.0.cmp(&b.0))
        .map(|(version, release)| (version, release.clone()))
}

/// The version of a `gui-vX.Y.Z` tag; `None` for any other tag.
fn gui_tag_version(tag: &str) -> Option<Version> {
    let version = Version::parse(tag.strip_prefix("gui-v")?).ok()?;
    (version.pre.is_empty() && version.build.is_empty()).then_some(version)
}

/// Accepts only HTTPS URLs to this repository on the GitHub API or to its
/// release downloads on github.com (which redirect to GitHub's download
/// hosts); nothing else is ever contacted.
pub(crate) fn validate_url(url: &str) -> Result<Url, String> {
    let parsed = Url::parse(url).map_err(|e| format!("invalid URL {url}: {e}"))?;
    let allowed = parsed.scheme() == "https"
        && parsed.username().is_empty()
        && parsed.password().is_none()
        && parsed.port().is_none()
        && match parsed.host_str() {
            Some("api.github.com") => parsed.path().starts_with(&format!("/repos/{OWNER_REPO}/")),
            Some("github.com") => parsed
                .path()
                .starts_with(&format!("/{OWNER_REPO}/releases/download/")),
            _ => false,
        };
    if allowed {
        Ok(parsed)
    } else {
        Err(format!("refusing to download from {url}"))
    }
}

/// Redirects are followed only to GitHub's own hosts over HTTPS.
fn redirect_policy() -> redirect::Policy {
    redirect::Policy::custom(|attempt| {
        let url = attempt.url().clone();
        let github = url.scheme() == "https"
            && url.host_str().is_some_and(|host| {
                host == "github.com"
                    || host == "api.github.com"
                    || host.ends_with(".githubusercontent.com")
            });
        if !github {
            attempt.error(format!("refusing to follow a redirect to {url}"))
        } else if attempt.previous().len() >= 5 {
            attempt.error("too many redirects")
        } else {
            attempt.follow()
        }
    })
}

fn client() -> Result<Client, String> {
    Client::builder()
        .user_agent(user_agent())
        .timeout(TIMEOUT)
        .redirect(redirect_policy())
        .build()
        .map_err(|e| format!("cannot set up HTTPS: {e}"))
}

fn get_json<T: serde::de::DeserializeOwned>(client: &Client, url: &str) -> Result<T, String> {
    let url = validate_url(url)?;
    let response = client
        .get(url.clone())
        .header(header::ACCEPT, "application/json")
        .send()
        .map_err(|e| format!("cannot reach GitHub: {e}"))?;
    if !response.status().is_success() {
        return Err(format!("{url} answered {}", response.status()));
    }
    response
        .json()
        .map_err(|e| format!("unexpected response from {url}: {e}"))
}

/// The fields of a release's `_manifest.json` that the update window shows.
#[derive(Debug, Deserialize)]
struct ReleaseManifest {
    gui_version: String,
    bundled_cli_version: String,
}

// ---------------------------------------------------------------------------
// How this installation can be updated

/// How the running GUI was installed, which decides whether it can update
/// itself.
#[derive(Clone, Debug, PartialEq)]
pub(crate) enum InstallKind {
    /// A macOS app bundle, replaced in place.
    MacApp(PathBuf),
    /// The per-user NSIS installation on Windows; the new installer runs.
    Nsis,
    /// A Linux AppImage, replaced in place.
    AppImage(PathBuf),
    /// Only a link to the release page, for the given reason.
    NotifyOnly(String),
}

impl InstallKind {
    /// Detects the installation of the running GUI.
    pub(crate) fn detect() -> Self {
        let Ok(exe) = std::env::current_exe() else {
            return InstallKind::NotifyOnly("The GUI's location is unknown.".to_owned());
        };
        let os = if cfg!(target_os = "macos") {
            Os::Mac
        } else if cfg!(windows) {
            Os::Windows
        } else if cfg!(target_os = "linux") {
            Os::Linux
        } else {
            Os::Other
        };
        detect_from(
            os,
            &exe,
            std::env::var_os("APPIMAGE").map(PathBuf::from),
            std::env::var_os("LOCALAPPDATA").map(PathBuf::from),
            &std::env::temp_dir(),
            directory_writable,
        )
    }
}

#[derive(Clone, Copy, Debug, PartialEq)]
enum Os {
    Mac,
    Windows,
    Linux,
    Other,
}

const NOT_PACKAGED: &str =
    "This GUI was not installed from a release package, so it cannot update itself.";

fn detect_from(
    os: Os,
    exe: &Path,
    appimage: Option<PathBuf>,
    local_app_data: Option<PathBuf>,
    temp_dir: &Path,
    writable: impl Fn(&Path) -> bool,
) -> InstallKind {
    match os {
        Os::Mac => {
            let Some(app) = mac_app_bundle(exe) else {
                return InstallKind::NotifyOnly(NOT_PACKAGED.to_owned());
            };
            if app.starts_with("/Volumes") {
                InstallKind::NotifyOnly(
                    "The app is running from the disk image. Copy it to Applications, open it from there, and update again."
                        .to_owned(),
                )
            } else if app.components().any(|c| c.as_os_str() == "AppTranslocation") {
                InstallKind::NotifyOnly(
                    "macOS is running the app from a temporary location (App Translocation). Move it to Applications, open it from there, and update again."
                        .to_owned(),
                )
            } else if !mac_path_is_safe(&app) || !mac_path_is_safe(temp_dir) {
                // When it cannot move the app, cargo-packager-updater falls
                // back to an administrator shell command that quotes the app
                // path and a new directory under the temporary folder.
                InstallKind::NotifyOnly(
                    "The app's location or the temporary folder (TMPDIR) contains characters the updater cannot handle safely (quotes, backslashes, $, ` or line breaks). Move the app to Applications and update again."
                        .to_owned(),
                )
            } else {
                InstallKind::MacApp(app)
            }
        }
        Os::Windows => {
            let dir = exe.parent();
            let installed = local_app_data
                .map(|base| base.join("mpeg-ts-analyzer"))
                .is_some_and(|install| dir == Some(install.as_path()))
                && dir.is_some_and(|dir| dir.join("uninstall.exe").is_file());
            if installed {
                InstallKind::Nsis
            } else {
                InstallKind::NotifyOnly(
                    "This is a portable copy. Download the new version from the release page."
                        .to_owned(),
                )
            }
        }
        Os::Linux => match appimage {
            Some(path) if path.parent().is_some_and(&writable) => InstallKind::AppImage(path),
            Some(_) => InstallKind::NotifyOnly(
                "The AppImage's folder is not writable, so it cannot be replaced.".to_owned(),
            ),
            None => InstallKind::NotifyOnly(
                "This GUI was installed with a package manager. Install the new package from the release page."
                    .to_owned(),
            ),
        },
        Os::Other => InstallKind::NotifyOnly(NOT_PACKAGED.to_owned()),
    }
}

/// The `.app` bundle containing `exe` (`<name>.app/Contents/MacOS/<exe>`).
fn mac_app_bundle(exe: &Path) -> Option<PathBuf> {
    let macos = exe.parent()?;
    let contents = macos.parent()?;
    let app = contents.parent()?;
    (macos.file_name()? == "MacOS"
        && contents.file_name()? == "Contents"
        && app.extension()? == "app")
        .then(|| app.to_path_buf())
}

/// Whether `path` can go into the updater's quoted `osascript` shell command
/// unchanged: valid UTF-8 without quotes, backslashes or control characters.
fn mac_path_is_safe(path: &Path) -> bool {
    path.to_str().is_some_and(|path| {
        !path
            .chars()
            .any(|c| matches!(c, '\'' | '"' | '\\' | '`' | '$') || c.is_control())
    })
}

fn directory_writable(dir: &Path) -> bool {
    let probe = dir.join(format!(
        ".mpeg-ts-analyzer-gui-write-test-{}",
        std::process::id()
    ));
    let ok = std::fs::File::create(&probe).is_ok();
    let _ = std::fs::remove_file(&probe);
    ok
}

// ---------------------------------------------------------------------------
// Checking and installing

/// A newer release.
pub(crate) struct Available {
    pub version: Version,
    pub bundled_cli_version: String,
    pub release_url: String,
    /// The update to download, verify and install, when this installation
    /// can install it.
    update: Option<Box<Update>>,
    /// The package's file name, which its signature must name.
    package_name: String,
    /// Why the update cannot be installed from the GUI, when it cannot.
    pub notify_only: Option<String>,
}

impl Available {
    /// An update to `version` bundling CLI 9.8.7, which cannot be downloaded:
    /// what tests offer instead of a release on GitHub.
    #[cfg(test)]
    pub(crate) fn for_test(version: &str, notify_only: Option<&str>) -> Self {
        Self {
            version: Version::parse(version).expect("test version"),
            bundled_cli_version: "9.8.7".to_owned(),
            release_url: format!("https://github.com/{OWNER_REPO}/releases/tag/gui-v{version}"),
            update: None,
            package_name: String::new(),
            notify_only: notify_only.map(str::to_owned),
        }
    }
}

/// Checks GitHub for a newer GUI release. Blocking; run it off the UI thread.
fn check(current: &Version, kind: &InstallKind) -> Result<Option<Available>, String> {
    let client = client()?;
    let releases: Vec<Release> = get_json(&client, RELEASES_API)?;
    let Some((version, release)) = newest_release(&releases, current) else {
        return Ok(None);
    };
    let asset = |suffix: &str| {
        let name = format!("mpeg-ts-analyzer-gui_{version}_{suffix}");
        release
            .assets
            .iter()
            .find(|asset| asset.name == name)
            .map(|asset| asset.browser_download_url.clone())
    };
    // Only opened in the browser, never requested by the GUI.
    let release_url = format!(
        "https://github.com/{OWNER_REPO}/releases/tag/{}",
        release.tag_name
    );
    if release.html_url != release_url {
        return Err(format!("unexpected release page {}", release.html_url));
    }

    let manifest_url = asset("manifest.json")
        .ok_or_else(|| format!("release {} has no manifest", release.tag_name))?;
    let manifest: ReleaseManifest = get_json(&client, &manifest_url)?;
    if manifest.gui_version != version.to_string() {
        return Err(format!(
            "the manifest of {} names GUI {}",
            release.tag_name, manifest.gui_version
        ));
    }

    let mut available = Available {
        version: version.clone(),
        bundled_cli_version: manifest.bundled_cli_version,
        release_url,
        update: None,
        package_name: String::new(),
        notify_only: None,
    };
    let Some(update_url) = asset("update.json") else {
        available.notify_only = Some(
            "This release cannot be installed from the GUI. Download it from the release page."
                .to_owned(),
        );
        return Ok(Some(available));
    };
    if let InstallKind::NotifyOnly(reason) = kind {
        available.notify_only = Some(reason.clone());
        return Ok(Some(available));
    }

    let (target, suffix, extract_path) = match kind {
        InstallKind::MacApp(app) => ("macos", "macos_arm64.app.tar.gz", app.clone()),
        InstallKind::AppImage(path) => ("linux", "linux_x86_64.AppImage", path.clone()),
        InstallKind::Nsis => (
            "windows",
            "windows_x86_64_setup.exe",
            std::env::current_exe()
                .ok()
                .and_then(|exe| exe.parent().map(Path::to_path_buf))
                .unwrap_or_default(),
        ),
        InstallKind::NotifyOnly(_) => unreachable!("handled above"),
    };
    let package_name = format!("mpeg-ts-analyzer-gui_{version}_{suffix}");
    let remote: RemoteRelease = get_json(&client, &update_url)?;
    if remote.version != version {
        return Err(format!(
            "the update manifest of {} announces version {}",
            release.tag_name, remote.version
        ));
    }
    let platform = cargo_packager_updater::target()
        .ok_or_else(|| "this platform has no updates".to_owned())?;
    let fail = |e: cargo_packager_updater::Error| {
        format!(
            "the update of {} has no package for {platform}: {e}",
            release.tag_name
        )
    };
    let download_url = remote.download_url(&platform).map_err(fail)?.clone();
    let signature = remote.signature(&platform).map_err(fail)?.clone();
    let format = remote.format(&platform).map_err(fail)?;
    validate_url(download_url.as_str())?;
    if download_url.path_segments().and_then(Iterator::last) != Some(package_name.as_str()) {
        return Err(format!("unexpected update package {download_url}"));
    }
    let format_matches = matches!(
        (kind, format),
        (InstallKind::MacApp(_), UpdateFormat::App)
            | (InstallKind::AppImage(_), UpdateFormat::AppImage)
            | (InstallKind::Nsis, UpdateFormat::Nsis)
    );
    if !format_matches {
        return Err(format!("unexpected update format {format}"));
    }
    let update = Update {
        config: Config {
            endpoints: Vec::new(),
            pubkey: PUBLIC_KEY.trim().to_owned(),
            windows: Some(WindowsConfig {
                installer_args: None,
                install_mode: Some(WindowsUpdateInstallMode::Passive),
            }),
        },
        body: remote.notes.clone(),
        current_version: current.to_string(),
        version: version.to_string(),
        date: remote.pub_date,
        target: target.to_owned(),
        extract_path,
        download_url,
        signature,
        timeout: Some(TIMEOUT),
        headers: header::HeaderMap::new(),
        format,
    };
    available.package_name = package_name;
    available.update = Some(Box::new(update));
    Ok(Some(available))
}

/// Downloads the package of `update` and verifies it; the bytes are returned
/// only when the signature is valid and names `package_name`.
fn download_verified(update: &Update, package_name: &str) -> Result<Vec<u8>, String> {
    let url = validate_url(update.download_url.as_str())?;
    let response = client()?
        .get(url.clone())
        .send()
        .map_err(|e| format!("cannot download {url}: {e}"))?;
    if !response.status().is_success() {
        return Err(format!("{url} answered {}", response.status()));
    }
    let bytes = response
        .bytes()
        .map_err(|e| format!("cannot download {url}: {e}"))?;
    verify_signature(&bytes, &update.signature, PUBLIC_KEY, package_name)?;
    Ok(bytes.to_vec())
}

/// Verifies a cargo-packager signature (Base64 of the minisign text) of
/// `bytes` against `public_key` (the same encoding). The signed trusted
/// comment must name `package_name`, which contains the version: a genuine
/// package of another version or platform is rejected too.
fn verify_signature(
    bytes: &[u8],
    signature: &str,
    public_key: &str,
    package_name: &str,
) -> Result<(), String> {
    use base64::Engine;
    let decode = |text: &str, what: &str| {
        base64::engine::general_purpose::STANDARD
            .decode(text.trim())
            .ok()
            .and_then(|bytes| String::from_utf8(bytes).ok())
            .ok_or_else(|| format!("the {what} is not valid"))
    };
    let public_key = minisign_verify::PublicKey::decode(&decode(public_key, "public key")?)
        .map_err(|e| format!("the public key is not valid: {e}"))?;
    let signature = minisign_verify::Signature::decode(&decode(signature, "signature")?)
        .map_err(|e| format!("the signature is not valid: {e}"))?;
    public_key
        .verify(bytes, &signature, false)
        .map_err(|e| format!("the signature does not match: {e}"))?;
    let signed_name = signature
        .trusted_comment()
        .split('\t')
        .find_map(|field| field.strip_prefix("file:"));
    if signed_name != Some(package_name) {
        return Err(format!(
            "the signature is for {}, not {package_name}",
            signed_name.unwrap_or("an unnamed file")
        ));
    }
    Ok(())
}

enum Message {
    Checked(Result<Option<Available>, String>),
    /// Installed in place (macOS, AppImage); a restart starts the new version.
    Installed(Result<(), String>),
    /// Downloaded and verified (Windows); installed when the GUI exits.
    Downloaded(Result<Vec<u8>, String>),
}

pub(crate) enum Phase {
    Idle,
    Checking,
    UpToDate,
    Available(Box<Available>),
    Failed(String),
    Installing(Box<Available>),
    Installed(Box<Available>),
}

/// Looks for a newer release; [`check`] outside tests.
type Checker =
    Arc<dyn Fn(&Version, &InstallKind) -> Result<Option<Available>, String> + Send + Sync>;

/// The checker of a new [`Updates`]. Tests never reach the network: they
/// get a checker that fails, and install their own with `with_checker`.
fn default_checker() -> Checker {
    if cfg!(test) {
        Arc::new(|_: &Version, _: &InstallKind| Err("tests do not reach the network".to_owned()))
    } else {
        Arc::new(check)
    }
}

/// The update state of the app and its window.
pub(crate) struct Updates {
    pub phase: Phase,
    kind: InstallKind,
    checker: Checker,
    /// Whether the user asked; a startup check stays silent on failure.
    manual: bool,
    /// The startup check has opened the window once; it does not again.
    prompted: bool,
    pub window_open: bool,
    /// The window was shown last frame; it is centered whenever it opens.
    window_shown: bool,
    receiver: Option<Receiver<Message>>,
    /// A downloaded Windows installer, run from `on_exit`.
    install_on_exit: Option<(Box<Update>, Vec<u8>)>,
}

impl Updates {
    pub(crate) fn new(kind: InstallKind) -> Self {
        Self {
            phase: Phase::Idle,
            kind,
            checker: default_checker(),
            manual: false,
            prompted: false,
            window_open: false,
            window_shown: false,
            receiver: None,
            install_on_exit: None,
        }
    }

    /// The state shown by tests, without any network access.
    #[cfg(test)]
    pub(crate) fn with_phase(phase: Phase, kind: InstallKind) -> Self {
        Self {
            phase,
            window_open: true,
            ..Self::new(kind)
        }
    }

    /// Replaces the release lookup, so tests can offer an update offline.
    #[cfg(test)]
    pub(crate) fn with_checker(
        mut self,
        checker: impl Fn(&Version, &InstallKind) -> Result<Option<Available>, String>
        + Send
        + Sync
        + 'static,
    ) -> Self {
        self.checker = Arc::new(checker);
        self
    }

    /// Checks for an update off the UI thread. `manual` opens the window and
    /// reports failures; the startup check opens it only for an update that
    /// was not skipped (see [`Updates::poll`]).
    pub(crate) fn check(&mut self, ctx: &egui::Context, manual: bool) {
        if self.receiver.is_some() {
            self.manual |= manual;
            self.window_open |= manual;
            return;
        }
        self.manual = manual;
        self.window_open |= manual;
        self.phase = Phase::Checking;
        let (sender, receiver) = channel();
        let kind = self.kind.clone();
        let checker = Arc::clone(&self.checker);
        let ctx = ctx.clone();
        std::thread::spawn(move || {
            let _ = sender.send(Message::Checked(checker(&current_version(), &kind)));
            ctx.request_repaint();
        });
        self.receiver = Some(receiver);
    }

    /// Whether a check is running.
    #[cfg(test)]
    pub(crate) fn checking(&self) -> bool {
        matches!(self.phase, Phase::Checking)
    }

    /// Takes in the result of a check, an install or a download. The first
    /// startup check that finds a version other than `skipped` opens the
    /// window, once per launch.
    pub(crate) fn poll(&mut self, ctx: &egui::Context, skipped: Option<&str>) {
        let Some(message) = self.receiver.as_ref().and_then(|r| r.try_recv().ok()) else {
            return;
        };
        self.receiver = None;
        let phase = std::mem::replace(&mut self.phase, Phase::Idle);
        self.phase = match (message, phase) {
            (Message::Checked(Ok(Some(available))), _) => {
                if !self.manual
                    && !self.prompted
                    && skipped != Some(available.version.to_string().as_str())
                {
                    self.prompted = true;
                    self.window_open = true;
                }
                Phase::Available(Box::new(available))
            }
            (Message::Checked(Ok(None)), _) => Phase::UpToDate,
            (Message::Checked(Err(error)), _) => {
                if !self.manual {
                    eprintln!("update check failed: {error}");
                }
                Phase::Failed(error)
            }
            (Message::Installed(Ok(())), Phase::Installing(available)) => {
                Phase::Installed(available)
            }
            (Message::Downloaded(Ok(bytes)), Phase::Installing(mut available)) => {
                if let Some(update) = available.update.take() {
                    self.install_on_exit = Some((update, bytes));
                    // eframe saves the settings, then `on_exit` starts the
                    // installer, which restarts the GUI when it is done.
                    ctx.send_viewport_cmd(egui::ViewportCommand::Close);
                }
                Phase::Installing(available)
            }
            (Message::Installed(Err(error)) | Message::Downloaded(Err(error)), _) => {
                self.window_open = true;
                Phase::Failed(format!("The update was not installed: {error}"))
            }
            (_, phase) => phase,
        };
    }

    /// Downloads, verifies and installs the update.
    fn install(&mut self, ctx: &egui::Context) {
        let Phase::Available(available) = std::mem::replace(&mut self.phase, Phase::Idle) else {
            return;
        };
        let Some(update) = available.update.clone() else {
            self.phase = Phase::Available(available);
            return;
        };
        let package_name = available.package_name.clone();
        let (sender, receiver) = channel();
        let windows = matches!(self.kind, InstallKind::Nsis);
        let ctx = ctx.clone();
        std::thread::spawn(move || {
            let downloaded = download_verified(&update, &package_name);
            let message = if windows {
                // Installing on Windows exits the process; it happens in
                // `on_exit`, after the settings were saved.
                Message::Downloaded(downloaded)
            } else {
                Message::Installed(
                    downloaded.and_then(|bytes| update.install(bytes).map_err(|e| e.to_string())),
                )
            };
            let _ = sender.send(message);
            ctx.request_repaint();
        });
        self.receiver = Some(receiver);
        self.phase = Phase::Installing(available);
    }

    /// Runs a downloaded Windows installer; called after the settings were saved.
    pub(crate) fn on_exit(&mut self) {
        if let Some((update, bytes)) = self.install_on_exit.take()
            && let Err(error) = update.install(bytes)
        {
            eprintln!("the update was not installed: {error}");
        }
    }

    /// The version a check has found, if it has found one.
    pub(crate) fn available_version(&self) -> Option<&Version> {
        match &self.phase {
            Phase::Available(available) => Some(&available.version),
            _ => None,
        }
    }

    /// The navigation rail's update button:
    /// shows the window, with a new manual check unless an update was found
    /// or is being installed.
    pub(crate) fn open(&mut self, ctx: &egui::Context) {
        match self.phase {
            Phase::Available(_) | Phase::Installing(_) | Phase::Installed(_) => {
                self.window_open = true;
            }
            Phase::Idle | Phase::Checking | Phase::UpToDate | Phase::Failed(_) => {
                self.check(ctx, true);
            }
        }
    }

    /// The window that shows the check and offers the update. Skip this
    /// version stores the version in `skipped`.
    pub(crate) fn window(
        &mut self,
        ctx: &egui::Context,
        analysis_running: bool,
        skipped: &mut Option<String>,
    ) {
        if !self.window_open {
            self.window_shown = false;
            return;
        }
        let mut open = true;
        let mut action = None;
        // Centered whenever it opens, also over a position eframe saved from
        // an earlier run, so a window opened at startup does not cover the
        // rail. It can still be moved while it is open.
        let id = egui::Id::unique("software_update_window");
        let mut window = egui::Window::new("Software update")
            .id(id)
            .open(&mut open)
            .collapsible(false)
            .resizable(false)
            .default_width(380.0)
            .pivot(egui::Align2::CENTER_CENTER);
        if !std::mem::replace(&mut self.window_shown, true) {
            let center = ctx.content_rect().center();
            // egui 0.37 restores a window's remembered position over
            // `current_pos` (title-bar dragging), so move the remembered
            // state too.
            if let Some(mut state) = egui::AreaState::load(ctx, id) {
                state.pivot = egui::Align2::CENTER_CENTER;
                state.pivot_pos = Some(center);
                ctx.memory_mut(|memory| {
                    memory
                        .areas_mut()
                        .set_state(egui::LayerId::new(egui::Order::Middle, id), state);
                });
            }
            window = window.current_pos(center);
        }
        window.show(ctx, |ui| {
            action = self.window_contents(ui, analysis_running);
        });
        self.window_open &= open;
        match action {
            Some(Action::Check) => self.check(ctx, true),
            Some(Action::Install) => self.install(ctx),
            Some(Action::Restart) => self.restart(ctx),
            Some(Action::Close) => self.window_open = false,
            Some(Action::Skip) => {
                if let Some(version) = self.available_version() {
                    *skipped = Some(version.to_string());
                }
                self.window_open = false;
            }
            None => {}
        }
    }

    fn window_contents(&self, ui: &mut egui::Ui, analysis_running: bool) -> Option<Action> {
        let current = env!("CARGO_PKG_VERSION");
        let mut action = None;
        match &self.phase {
            Phase::Idle => {
                ui.label(format!("You have GUI {current}."));
                if ui.button("Check for updates").clicked() {
                    action = Some(Action::Check);
                }
            }
            Phase::Checking => {
                ui.horizontal(|ui| {
                    ui.spinner();
                    ui.label("Checking for updates…");
                });
            }
            Phase::UpToDate => {
                ui.label(format!("GUI {current} is the newest version."));
                if ui.button("OK").clicked() {
                    action = Some(Action::Close);
                }
            }
            Phase::Failed(error) => {
                ui.colored_label(ui.visuals().warn_fg_color, error);
                if ui.button("Try again").clicked() {
                    action = Some(Action::Check);
                }
            }
            Phase::Available(available) => {
                describe(ui, available);
                ui.add_space(8.0);
                if let Some(reason) = &available.notify_only {
                    ui.weak(reason);
                    ui.add_space(4.0);
                }
                ui.horizontal(|ui| {
                    if available.notify_only.is_some() {
                        if ui.button("Open release page").clicked() {
                            ui.ctx()
                                .open_url(egui::OpenUrl::new_tab(&available.release_url));
                        }
                    } else {
                        let install = ui
                            .add_enabled(!analysis_running, egui::Button::new("Install"))
                            .on_disabled_hover_text("Wait until the analysis has finished.");
                        if install.clicked() {
                            action = Some(Action::Install);
                        }
                    }
                    if ui.button("Later").clicked() {
                        action = Some(Action::Close);
                    }
                    if ui.button("Skip this version").clicked() {
                        action = Some(Action::Skip);
                    }
                });
                if available.notify_only.is_none() {
                    ui.weak(install_note(&self.kind));
                }
            }
            Phase::Installing(available) => {
                ui.horizontal(|ui| {
                    ui.spinner();
                    ui.label(format!("Installing GUI {}…", available.version));
                });
            }
            Phase::Installed(available) => {
                ui.label(format!(
                    "GUI {} is installed. Restart to use it.",
                    available.version
                ));
                ui.horizontal(|ui| {
                    if ui.button("Restart now").clicked() {
                        action = Some(Action::Restart);
                    }
                    if ui.button("Later").clicked() {
                        action = Some(Action::Close);
                    }
                });
            }
        }
        action
    }

    /// Starts the installed new version and closes this one.
    fn restart(&mut self, ctx: &egui::Context) {
        let started = match &self.kind {
            InstallKind::MacApp(app) => std::process::Command::new("open")
                .arg("-n")
                .arg(app)
                .spawn()
                .map(drop),
            InstallKind::AppImage(path) => std::process::Command::new(path).spawn().map(drop),
            _ => Ok(()),
        };
        match started {
            Ok(()) => ctx.send_viewport_cmd(egui::ViewportCommand::Close),
            Err(error) => {
                self.phase = Phase::Failed(format!(
                    "The new version is installed but did not start ({error}). Start it again yourself."
                ))
            }
        }
    }
}

enum Action {
    Check,
    Install,
    Restart,
    Close,
    /// Close, and do not open the window at startup for this version again.
    Skip,
}

fn describe(ui: &mut egui::Ui, available: &Available) {
    ui.label(crate::theme::emphasis(format!(
        "GUI {} is available",
        available.version
    )));
    ui.label(format!(
        "You have {}. The new version bundles CLI {}.",
        env!("CARGO_PKG_VERSION"),
        available.bundled_cli_version
    ));
    ui.hyperlink_to("Release notes", &available.release_url);
}

fn install_note(kind: &InstallKind) -> &'static str {
    match kind {
        InstallKind::Nsis => {
            "The GUI closes, the installer runs, and the new version starts when it is done."
        }
        InstallKind::MacApp(_) => {
            "The app is replaced in place. macOS may ask for an administrator password if you cannot change it."
        }
        _ => "The GUI is replaced in place; restart it afterwards.",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A temporary folder like macOS's default `TMPDIR`.
    const TEST_TMP: &str = "/private/var/folders/xy/abc123/T";

    fn release(tag: &str) -> Release {
        Release {
            tag_name: tag.to_owned(),
            draft: false,
            prerelease: false,
            html_url: format!("https://github.com/{OWNER_REPO}/releases/tag/{tag}"),
            assets: Vec::new(),
        }
    }

    fn v(text: &str) -> Version {
        Version::parse(text).unwrap()
    }

    #[test]
    fn picks_the_newest_published_gui_release() {
        let mut draft = release("gui-v0.9.0");
        draft.draft = true;
        let mut pre = release("gui-v0.8.0");
        pre.prerelease = true;
        let releases = vec![
            release("v9.9.9"),
            release("gui-v0.1.3"),
            release("gui-v0.2.0"),
            release("gui-v0.1.10"),
            draft,
            pre,
            release("gui-v1.0.0-rc.1"),
            release("gui-vbroken"),
        ];
        let (version, found) = newest_release(&releases, &v("0.1.2")).unwrap();
        assert_eq!(version, v("0.2.0"));
        assert_eq!(found.tag_name, "gui-v0.2.0");
    }

    #[test]
    fn never_offers_the_same_or_an_older_version() {
        let releases = vec![release("gui-v0.1.2"), release("gui-v0.1.1")];
        assert!(newest_release(&releases, &v("0.1.2")).is_none());
        assert!(newest_release(&[], &v("0.1.2")).is_none());
    }

    #[test]
    fn accepts_only_github_urls_of_this_repository() {
        for ok in [
            "https://api.github.com/repos/small-teton/mpeg-ts-analyzer/releases?per_page=100",
            "https://github.com/small-teton/mpeg-ts-analyzer/releases/download/gui-v0.2.0/mpeg-ts-analyzer-gui_0.2.0_update.json",
        ] {
            assert!(validate_url(ok).is_ok(), "{ok}");
        }
        for bad in [
            "http://github.com/small-teton/mpeg-ts-analyzer/releases/download/gui-v0.2.0/x",
            "https://example.com/small-teton/mpeg-ts-analyzer/releases/download/x",
            "https://github.com/someone/else/releases/download/gui-v0.2.0/x",
            "https://github.com/small-teton/mpeg-ts-analyzer/archive/x",
            "https://api.github.com/repos/someone/else/releases",
            "https://user@github.com/small-teton/mpeg-ts-analyzer/releases/download/x",
            "https://github.com:8443/small-teton/mpeg-ts-analyzer/releases/download/x",
            "https://github.com.evil.example/small-teton/mpeg-ts-analyzer/releases/download/x",
            "file:///etc/passwd",
            "not a url",
        ] {
            assert!(validate_url(bad).is_err(), "{bad}");
        }
    }

    #[test]
    fn the_public_key_is_a_minisign_key() {
        use base64::Engine;
        let decoded = base64::engine::general_purpose::STANDARD
            .decode(PUBLIC_KEY.trim())
            .unwrap();
        let text = String::from_utf8(decoded).unwrap();
        assert!(text.starts_with("untrusted comment: minisign public key"));
    }

    #[test]
    fn detects_macos_installations() {
        let kind = |exe: &str| {
            detect_from(
                Os::Mac,
                Path::new(exe),
                None,
                None,
                Path::new(TEST_TMP),
                |_| true,
            )
        };
        assert_eq!(
            kind("/Applications/mpeg-ts-analyzer.app/Contents/MacOS/mpeg-ts-analyzer-gui"),
            InstallKind::MacApp(PathBuf::from("/Applications/mpeg-ts-analyzer.app"))
        );
        assert!(matches!(
            kind("/Volumes/mpeg-ts-analyzer/mpeg-ts-analyzer.app/Contents/MacOS/mpeg-ts-analyzer-gui"),
            InstallKind::NotifyOnly(reason) if reason.contains("disk image")
        ));
        assert!(matches!(
            kind("/private/var/folders/x/T/AppTranslocation/ABC/d/mpeg-ts-analyzer.app/Contents/MacOS/mpeg-ts-analyzer-gui"),
            InstallKind::NotifyOnly(reason) if reason.contains("App Translocation")
        ));
        assert_eq!(
            kind("/Users/me/src/mpeg-ts-analyzer/gui/target/debug/mpeg-ts-analyzer-gui"),
            InstallKind::NotifyOnly(NOT_PACKAGED.to_owned())
        );
    }

    #[test]
    fn refuses_mac_paths_the_admin_fallback_cannot_quote() {
        for app in [
            "/Users/me/it's/mpeg-ts-analyzer.app",
            "/Users/me/a\"b/mpeg-ts-analyzer.app",
            "/Users/me/a\\b/mpeg-ts-analyzer.app",
            "/Users/me/a$(id)/mpeg-ts-analyzer.app",
            "/Users/me/a`id`/mpeg-ts-analyzer.app",
            "/Users/me/a\nb/mpeg-ts-analyzer.app",
        ] {
            let exe = format!("{app}/Contents/MacOS/mpeg-ts-analyzer-gui");
            assert!(
                matches!(
                    detect_from(Os::Mac, Path::new(&exe), None, None, Path::new(TEST_TMP), |_| true),
                    InstallKind::NotifyOnly(reason) if reason.contains("cannot handle safely")
                ),
                "{app}"
            );
        }
        assert!(matches!(
            detect_from(
                Os::Mac,
                Path::new(
                    "/Users/me/My Apps (2)/mpeg-ts-analyzer.app/Contents/MacOS/mpeg-ts-analyzer-gui"
                ),
                None,
                None,
                Path::new(TEST_TMP),
                |_| true
            ),
            InstallKind::MacApp(_)
        ));

        // The fallback also quotes a directory under the temporary folder.
        let installed = "/Applications/mpeg-ts-analyzer.app/Contents/MacOS/mpeg-ts-analyzer-gui";
        for tmp in ["/tmp/it's", "/tmp/a$(id)", "/tmp/a\nb"] {
            assert!(
                matches!(
                    detect_from(Os::Mac, Path::new(installed), None, None, Path::new(tmp), |_| true),
                    InstallKind::NotifyOnly(reason) if reason.contains("TMPDIR")
                ),
                "{tmp}"
            );
        }
    }

    const FIXTURE_NAME: &str = "mpeg-ts-analyzer-gui_9.9.9_macos_arm64.app.tar.gz";
    const FIXTURE: &[u8] = include_bytes!("../tests/fixtures/update/package.bin");
    const FIXTURE_SIG: &str = include_str!("../tests/fixtures/update/package.bin.sig");
    const FIXTURE_KEY: &str = include_str!("../tests/fixtures/update/test_key.pub");

    #[test]
    fn accepts_a_valid_signature_for_the_expected_file() {
        verify_signature(FIXTURE, FIXTURE_SIG, FIXTURE_KEY, FIXTURE_NAME).unwrap();
    }

    #[test]
    fn rejects_altered_packages_other_keys_and_other_files() {
        let mut altered = FIXTURE.to_vec();
        altered[0] ^= 1;
        let err = verify_signature(&altered, FIXTURE_SIG, FIXTURE_KEY, FIXTURE_NAME).unwrap_err();
        assert!(err.contains("does not match"), "{err}");

        // The release key did not make the fixture's signature.
        let err = verify_signature(FIXTURE, FIXTURE_SIG, PUBLIC_KEY, FIXTURE_NAME).unwrap_err();
        assert!(err.contains("does not match"), "{err}");

        // A genuine signature of an older package offered as a newer one.
        let newer = "mpeg-ts-analyzer-gui_10.0.0_macos_arm64.app.tar.gz";
        let err = verify_signature(FIXTURE, FIXTURE_SIG, FIXTURE_KEY, newer).unwrap_err();
        assert!(err.contains("not mpeg-ts-analyzer-gui_10.0.0"), "{err}");

        assert!(verify_signature(FIXTURE, "not base64!", FIXTURE_KEY, FIXTURE_NAME).is_err());
    }

    #[test]
    fn detects_linux_installations() {
        let appimage = PathBuf::from("/home/me/Apps/mpeg-ts-analyzer.AppImage");
        let exe = Path::new("/tmp/.mount_abc/usr/bin/mpeg-ts-analyzer-gui");
        assert_eq!(
            detect_from(
                Os::Linux,
                exe,
                Some(appimage.clone()),
                None,
                Path::new(TEST_TMP),
                |_| true
            ),
            InstallKind::AppImage(appimage.clone())
        );
        assert!(matches!(
            detect_from(Os::Linux, exe, Some(appimage), None, Path::new(TEST_TMP), |_| false),
            InstallKind::NotifyOnly(reason) if reason.contains("not writable")
        ));
        assert!(matches!(
            detect_from(Os::Linux, Path::new("/usr/bin/mpeg-ts-analyzer-gui"), None, None, Path::new(TEST_TMP), |_| true),
            InstallKind::NotifyOnly(reason) if reason.contains("package manager")
        ));
    }

    #[test]
    fn detects_windows_installations() {
        let base = std::env::temp_dir().join(format!("mtsa-update-test-{}", std::process::id()));
        let install = base.join("mpeg-ts-analyzer");
        std::fs::create_dir_all(&install).unwrap();
        let exe = install.join("mpeg-ts-analyzer-gui.exe");
        // Without the NSIS uninstaller it is a copy, not an installation.
        assert!(matches!(
            detect_from(Os::Windows, &exe, None, Some(base.clone()), Path::new(TEST_TMP), |_| true),
            InstallKind::NotifyOnly(reason) if reason.contains("portable")
        ));
        std::fs::write(install.join("uninstall.exe"), b"").unwrap();
        assert_eq!(
            detect_from(
                Os::Windows,
                &exe,
                None,
                Some(base.clone()),
                Path::new(TEST_TMP),
                |_| true
            ),
            InstallKind::Nsis
        );
        let elsewhere = Path::new("C:/Tools/mpeg-ts-analyzer/mpeg-ts-analyzer-gui.exe");
        assert!(matches!(
            detect_from(
                Os::Windows,
                elsewhere,
                None,
                Some(base.clone()),
                Path::new(TEST_TMP),
                |_| true
            ),
            InstallKind::NotifyOnly(_)
        ));
        std::fs::remove_dir_all(&base).unwrap();
    }

    /// Against the real GitHub API; run with `cargo test -- --ignored`.
    #[test]
    #[ignore = "needs network access to api.github.com"]
    fn finds_the_newest_release_on_github() {
        let found = check(&v("0.0.1"), &InstallKind::NotifyOnly("test".to_owned()))
            .expect("check")
            .expect("a release newer than 0.0.1");
        assert!(found.version > v("0.0.1"));
        assert!(!found.bundled_cli_version.is_empty());
        assert!(
            found
                .release_url
                .ends_with(&format!("/gui-v{}", found.version))
        );
        assert!(found.notify_only.is_some());
        println!(
            "newest: {} (CLI {}), {}: {:?}",
            found.version, found.bundled_cli_version, found.release_url, found.notify_only
        );
    }

    // The update window, driven through its accessibility tree.

    use egui_kittest::Harness;
    use egui_kittest::kittest::{NodeT, Queryable};

    fn available(notify_only: Option<&str>) -> Box<Available> {
        Box::new(Available::for_test("9.9.9", notify_only))
    }

    fn mac() -> InstallKind {
        InstallKind::MacApp(PathBuf::from("/Applications/mpeg-ts-analyzer.app"))
    }

    /// The updates and the skipped version of the settings.
    type State = (Updates, Option<String>);

    fn window(phase: Phase, kind: InstallKind, running: bool) -> Harness<'static, State> {
        // The app's theme, whose fonts the window's text roles use.
        let mut harness = Harness::builder()
            .with_size(eframe::egui::vec2(800.0, 600.0))
            .with_context_setup(crate::theme::apply)
            .build_ui_state(
                move |ui, (updates, skipped): &mut State| {
                    updates.window(ui.ctx(), running, skipped)
                },
                (Updates::with_phase(phase, kind), None),
            );
        // Not run(): the spinners of Checking and Installing keep repainting.
        harness.run_steps(4);
        harness
    }

    #[test]
    fn an_installable_update_offers_install() {
        let mut harness = window(Phase::Available(available(None)), mac(), false);
        harness.get_by_label("GUI 9.9.9 is available");
        harness.get_by_label_contains("bundles CLI 9.8.7");
        harness.get_by_label("Release notes");
        let install = harness.get_by_label("Install");
        assert!(!install.accesskit_node().is_disabled());
        harness.get_by_label("Later").click();
        harness.run();
        assert!(!harness.state().0.window_open);
        assert_eq!(harness.state().1, None, "Later skipped the version");
    }

    #[test]
    fn skip_this_version_remembers_the_version() {
        for kind in [mac(), InstallKind::NotifyOnly("portable".to_owned())] {
            let notify_only = matches!(kind, InstallKind::NotifyOnly(_)).then_some("portable");
            let mut harness = window(Phase::Available(available(notify_only)), kind, false);
            harness.get_by_label("Skip this version").click();
            harness.run();
            assert!(!harness.state().0.window_open);
            assert_eq!(harness.state().1.as_deref(), Some("9.9.9"));
            // The update is still available, for the rail's badge.
            assert!(harness.state().0.available_version().is_some());
        }
    }

    #[test]
    fn install_waits_for_a_running_analysis() {
        let harness = window(Phase::Available(available(None)), mac(), true);
        assert!(
            harness
                .get_by_label("Install")
                .accesskit_node()
                .is_disabled()
        );
    }

    #[test]
    fn a_notify_only_update_links_to_the_release_page() {
        let reason = "This is a portable copy. Download the new version from the release page.";
        let harness = window(
            Phase::Available(available(Some(reason))),
            InstallKind::NotifyOnly(reason.to_owned()),
            false,
        );
        harness.get_by_label(reason);
        harness.get_by_label("Open release page");
        harness.get_by_label("Later");
        harness.get_by_label("Skip this version");
        assert!(harness.query_by_label("Install").is_none());
    }

    #[test]
    fn shows_every_other_state() {
        let harness = window(Phase::UpToDate, mac(), false);
        harness.get_by_label_contains("is the newest version");

        let harness = window(
            Phase::Failed("cannot reach GitHub: offline".to_owned()),
            mac(),
            false,
        );
        harness.get_by_label("cannot reach GitHub: offline");
        harness.get_by_label("Try again");

        let harness = window(Phase::Checking, mac(), false);
        harness.get_by_label("Checking for updates…");

        let harness = window(Phase::Installing(available(None)), mac(), false);
        harness.get_by_label("Installing GUI 9.9.9…");

        let harness = window(Phase::Installed(available(None)), mac(), false);
        harness.get_by_label("GUI 9.9.9 is installed. Restart to use it.");
        harness.get_by_label("Restart now");
    }
}
