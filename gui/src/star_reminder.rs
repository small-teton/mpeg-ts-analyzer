//! The GitHub star reminder (docs/gui/design.md, "Star reminder"): a small
//! row of the root application shell, above the status bar, shown after the
//! GUI has completed a few successful analyses.
//!
//! The reminder is local only. It never runs `gh`, calls the GitHub API, asks
//! for a login or reports anything: it cannot know whether the user has
//! already starred the project, so **Don't ask again** suppresses it for good.

use eframe::egui;
use serde::{Deserialize, Serialize};

use crate::icons;

/// Key of the reminder's state in eframe's storage, next to the settings.
/// Kept apart from [`crate::settings::Settings`], so Restore defaults (which
/// resets preferences) does not bring a completed reminder back.
pub const STORAGE_KEY: &str = "star_reminder";

/// Successful analyses before the reminder is first shown.
pub const FIRST_REMINDER_AFTER: u64 = 3;
/// Further successful analyses before a closed reminder is shown again.
pub const REMIND_AGAIN_AFTER: u64 = 5;

/// The page **Star on GitHub** opens.
pub const REPOSITORY_URL: &str = "https://github.com/small-teton/mpeg-ts-analyzer";

pub const HEADING: &str = "Is mpeg-ts-analyzer useful to you?";
pub const BODY: &str = "A GitHub star helps other people find this project.";
pub const STAR_ACTION: &str = "Star on GitHub";
pub const DONT_ASK_ACTION: &str = "Don't ask again";
/// Accessible name and tooltip of the close button.
pub const CLOSE_ACTION: &str = "Close the reminder";

/// Where the reminder is in its lifecycle:
/// `Pending -> Eligible -> Deferred -> Eligible -> … -> Completed`.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub enum ReminderState {
    /// Not shown yet: waiting for [`FIRST_REMINDER_AFTER`] successful analyses.
    #[default]
    Pending,
    /// Shown, until the user chooses one of its actions.
    Eligible,
    /// Closed: shown again once `successful_analyses` reaches `until`.
    Deferred { until: u64 },
    /// **Star on GitHub** or **Don't ask again**: never shown again.
    Completed,
}

/// The persisted reminder: how many analyses have succeeded and its state.
/// Fields missing from an older save take their defaults, so an existing
/// installation starts at zero analyses, pending.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct StarReminder {
    pub successful_analyses: u64,
    pub state: ReminderState,
}

/// What the user chose in the reminder.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Action {
    Star,
    DontAskAgain,
    Close,
}

impl StarReminder {
    /// The reminder saved by the previous run: absent (a first run, or a GUI
    /// from before the reminder) or unreadable, it starts pending at zero.
    pub fn load(storage: Option<&dyn eframe::Storage>) -> Self {
        storage
            .and_then(|storage| eframe::get_value(storage, STORAGE_KEY))
            .unwrap_or_default()
    }

    /// Whether the reminder row is shown.
    pub fn is_shown(&self) -> bool {
        self.state == ReminderState::Eligible
    }

    /// Counts an analysis the GUI completed and accepted; cancelled, failed
    /// and rejected runs are not counted.
    pub fn record_successful_analysis(&mut self) {
        self.successful_analyses = self.successful_analyses.saturating_add(1);
        let due = match self.state {
            ReminderState::Pending => self.successful_analyses >= FIRST_REMINDER_AFTER,
            ReminderState::Deferred { until } => self.successful_analyses >= until,
            ReminderState::Eligible | ReminderState::Completed => false,
        };
        if due {
            self.state = ReminderState::Eligible;
        }
    }

    /// The close button: shown again after [`REMIND_AGAIN_AFTER`] more
    /// successful analyses, however often it has been closed before.
    pub fn defer(&mut self) {
        if self.is_shown() {
            self.state = ReminderState::Deferred {
                until: self.successful_analyses.saturating_add(REMIND_AGAIN_AFTER),
            };
        }
    }

    /// **Star on GitHub** (once the page has opened) or **Don't ask again**.
    pub fn complete(&mut self) {
        self.state = ReminderState::Completed;
    }

    /// Applies an action. `open` opens [`REPOSITORY_URL`] for **Star on
    /// GitHub**; when it fails, the reminder stays shown (the user wanted to
    /// star and could not) and the error is returned to be shown in the row.
    pub fn apply(
        &mut self,
        action: Action,
        open: &mut dyn FnMut(&str) -> Result<(), String>,
    ) -> Result<(), String> {
        match action {
            Action::Star => {
                open(REPOSITORY_URL)?;
                self.complete();
            }
            Action::DontAskAgain => self.complete(),
            Action::Close => self.defer(),
        }
        Ok(())
    }
}

/// One breathing cycle of the reminder's highlight, in seconds.
pub const PULSE_PERIOD: f64 = 2.0;
/// Frame interval while the reminder breathes (about 18 frames a second):
/// smooth enough for a slow fade, and the app is idle again once it hides.
pub const PULSE_FRAME: std::time::Duration = std::time::Duration::from_millis(55);
/// Highlight strength when motion is turned off: a static warm tint.
pub const STATIC_HIGHLIGHT: f32 = 0.5;

/// Strength of the highlight `elapsed` seconds after the reminder appeared: a
/// slow 0 → 1 → 0 breath every [`PULSE_PERIOD`] seconds, for as long as the
/// reminder is shown. Only colors follow it, never the layout.
pub fn pulse(elapsed: f64) -> f32 {
    let phase = elapsed.max(0.0) / PULSE_PERIOD * std::f64::consts::TAU;
    (0.5 * (1.0 - phase.cos())) as f32
}

/// Opens `url` in the default browser.
#[cfg_attr(test, allow(dead_code))]
pub fn open_in_browser(url: &str) -> Result<(), String> {
    webbrowser::open(url).map_err(|err| err.to_string())
}

/// Draws the reminder row. `open_error` is why the last **Star on GitHub**
/// could not open the browser. The row never takes keyboard focus by
/// itself; its buttons are reached with Tab like any other.
pub fn show(ui: &mut egui::Ui, open_error: Option<&str>) -> Option<Action> {
    let mut action = None;
    let accent = crate::theme::star_accent(ui.visuals());
    let star = || icons::text(icons::STAR).size(STAR_ICON_SIZE).color(accent);
    let heading = || crate::theme::emphasis(HEADING);
    let actions_width = actions_width(ui);
    let copy_width = text_width(ui, star())
        + text_width(ui, heading())
        + text_width(ui, egui::RichText::new(BODY))
        + 2.0 * ui.spacing().item_spacing.x;
    let one_row =
        copy_width + actions_width + 2.0 * ui.spacing().item_spacing.x <= ui.available_width();
    if one_row {
        ui.horizontal(|ui| {
            ui.label(star());
            ui.label(heading());
            ui.label(BODY);
            let gap = ui.available_width() - actions_width;
            if gap > 0.0 {
                ui.add_space(gap);
            }
            action = buttons(ui);
        });
    } else {
        // Narrow: the copy wraps, and the actions follow on their own row.
        ui.horizontal_wrapped(|ui| {
            ui.label(star());
            ui.label(heading());
            ui.label(BODY);
        });
        ui.horizontal_wrapped(|ui| action = buttons(ui));
    }
    if let Some(error) = open_error {
        ui.horizontal_wrapped(|ui| {
            ui.colored_label(
                ui.visuals().warn_fg_color,
                format!("The browser did not open ({error}). The project page is"),
            );
            ui.add(
                egui::Label::new(egui::RichText::new(REPOSITORY_URL).monospace()).selectable(true),
            );
        });
    }
    action
}

const STAR_ICON_SIZE: f32 = 20.0;

/// The actions in reading (and Tab) order: the primary action, the
/// low-emphasis permanent dismissal, then close.
fn buttons(ui: &mut egui::Ui) -> Option<Action> {
    let mut action = None;
    if ui.add(star_button()).clicked() {
        action = Some(Action::Star);
    }
    if ui.button(DONT_ASK_ACTION).clicked() {
        action = Some(Action::DontAskAgain);
    }
    let close = ui
        .add(egui::Button::new(icons::text(icons::X)).frame(false))
        .on_hover_text(CLOSE_ACTION);
    close.widget_info(|| egui::WidgetInfo::labeled(egui::Role::Button, true, CLOSE_ACTION));
    if close.clicked() {
        action = Some(Action::Close);
    }
    action
}

fn star_button() -> egui::Button<'static> {
    egui::Button::new(crate::theme::emphasis(STAR_ACTION).color(crate::theme::ON_FILL_TEXT))
        .fill(crate::theme::STAR_FILL)
}

/// Width of the three actions laid out in a row.
fn actions_width(ui: &egui::Ui) -> f32 {
    let padding = 2.0 * ui.spacing().button_padding.x;
    text_width(ui, crate::theme::emphasis(STAR_ACTION))
        + text_width(ui, egui::RichText::new(DONT_ASK_ACTION))
        + text_width(ui, icons::text(icons::X))
        + 3.0 * padding
        + 2.0 * ui.spacing().item_spacing.x
}

fn text_width(ui: &egui::Ui, text: egui::RichText) -> f32 {
    egui::WidgetText::from(text)
        .into_galley(
            ui,
            Some(egui::TextWrapMode::Extend),
            f32::INFINITY,
            egui::TextStyle::Button,
        )
        .size()
        .x
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    #[derive(Default)]
    struct MemoryStorage(HashMap<String, String>);

    impl eframe::Storage for MemoryStorage {
        fn get_string(&self, key: &str) -> Option<String> {
            self.0.get(key).cloned()
        }
        fn set_string(&mut self, key: &str, value: String) {
            self.0.insert(key.to_owned(), value);
        }
        fn remove_string(&mut self, key: &str) {
            self.0.remove(key);
        }
        fn flush(&mut self) {}
    }

    /// Saves from a GUI without the reminder, or with fewer fields, start
    /// pending at zero; the saved settings are not involved.
    #[test]
    fn older_saves_start_pending_at_zero() {
        assert_eq!(StarReminder::load(None), StarReminder::default());
        let mut storage = MemoryStorage::default();
        storage.0.insert(
            crate::settings::STORAGE_KEY.to_owned(),
            "(cli_path: \"/x\")".to_owned(),
        );
        assert_eq!(StarReminder::load(Some(&storage)), StarReminder::default());
        storage.0.insert(STORAGE_KEY.to_owned(), "()".to_owned());
        assert_eq!(StarReminder::load(Some(&storage)), StarReminder::default());
        storage.0.insert(
            STORAGE_KEY.to_owned(),
            "(successful_analyses: 2)".to_owned(),
        );
        let mut loaded = StarReminder::load(Some(&storage));
        assert_eq!(loaded.state, ReminderState::Pending);
        loaded.record_successful_analysis();
        assert!(loaded.is_shown());
        // Unreadable: started over rather than failing.
        storage
            .0
            .insert(STORAGE_KEY.to_owned(), "(state: Bogus)".to_owned());
        assert_eq!(StarReminder::load(Some(&storage)), StarReminder::default());
    }

    #[test]
    fn every_state_survives_a_restart() {
        for state in [
            ReminderState::Pending,
            ReminderState::Eligible,
            ReminderState::Deferred { until: 13 },
            ReminderState::Completed,
        ] {
            let saved = StarReminder {
                successful_analyses: 8,
                state,
            };
            let mut storage = MemoryStorage::default();
            eframe::set_value(&mut storage, STORAGE_KEY, &saved);
            assert_eq!(StarReminder::load(Some(&storage)), saved);
        }
    }

    fn after(successes: u64) -> StarReminder {
        let mut reminder = StarReminder::default();
        for _ in 0..successes {
            reminder.record_successful_analysis();
        }
        reminder
    }

    #[test]
    fn first_shown_after_three_successful_analyses() {
        assert!(!after(FIRST_REMINDER_AFTER - 1).is_shown());
        let reminder = after(FIRST_REMINDER_AFTER);
        assert!(reminder.is_shown());
        assert_eq!(reminder.successful_analyses, 3);
    }

    #[test]
    fn closing_defers_by_five_analyses_every_time() {
        let mut reminder = after(FIRST_REMINDER_AFTER);
        for round in 0..4 {
            let closed_at = reminder.successful_analyses;
            reminder.defer();
            assert_eq!(
                reminder.state,
                ReminderState::Deferred {
                    until: closed_at + REMIND_AGAIN_AFTER
                },
                "round {round}"
            );
            for _ in 0..REMIND_AGAIN_AFTER - 1 {
                reminder.record_successful_analysis();
                assert!(!reminder.is_shown(), "round {round}");
            }
            reminder.record_successful_analysis();
            assert!(reminder.is_shown(), "round {round}");
        }
        assert_eq!(
            reminder.successful_analyses,
            FIRST_REMINDER_AFTER + 4 * REMIND_AGAIN_AFTER
        );
    }

    #[test]
    fn completion_is_permanent() {
        for action in [Action::Star, Action::DontAskAgain] {
            let mut reminder = after(FIRST_REMINDER_AFTER);
            reminder.apply(action, &mut |_| Ok(())).unwrap();
            assert_eq!(reminder.state, ReminderState::Completed);
            for _ in 0..100 {
                reminder.record_successful_analysis();
            }
            assert!(!reminder.is_shown());
            reminder.defer();
            assert_eq!(reminder.state, ReminderState::Completed);
        }
    }

    #[test]
    fn star_opens_the_repository_and_dont_ask_opens_nothing() {
        let mut opened = Vec::new();
        let mut reminder = after(FIRST_REMINDER_AFTER);
        reminder
            .apply(Action::Star, &mut |url| {
                opened.push(url.to_owned());
                Ok(())
            })
            .unwrap();
        assert_eq!(opened, [REPOSITORY_URL]);
        assert_eq!(
            REPOSITORY_URL,
            "https://github.com/small-teton/mpeg-ts-analyzer"
        );

        let mut reminder = after(FIRST_REMINDER_AFTER);
        reminder
            .apply(Action::DontAskAgain, &mut |_| panic!("opened a URL"))
            .unwrap();
        reminder
            .apply(Action::Close, &mut |_| panic!("opened a URL"))
            .unwrap();
        assert_eq!(reminder.state, ReminderState::Completed);
    }

    /// A browser that does not open leaves the reminder shown, so the user
    /// can try again or use the address shown in the row.
    #[test]
    fn a_failed_browser_open_does_not_complete() {
        let mut reminder = after(FIRST_REMINDER_AFTER);
        let result = reminder.apply(Action::Star, &mut |_| Err("no browser".to_owned()));
        assert_eq!(result, Err("no browser".to_owned()));
        assert!(reminder.is_shown());
        reminder.apply(Action::Star, &mut |_| Ok(())).unwrap();
        assert_eq!(reminder.state, ReminderState::Completed);
    }

    #[test]
    fn breathes_while_shown() {
        assert_eq!(pulse(0.0), 0.0);
        for n in 0..100 {
            let start = PULSE_PERIOD * f64::from(n);
            assert!(pulse(start).abs() < 1e-6, "cycle {n}");
            assert!(
                (pulse(start + PULSE_PERIOD / 2.0) - 1.0).abs() < 1e-6,
                "cycle {n}"
            );
        }
    }

    #[test]
    fn close_does_nothing_unless_shown() {
        let mut reminder = after(1);
        reminder.defer();
        assert_eq!(reminder.state, ReminderState::Pending);
    }
}
