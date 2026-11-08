// Hide the console window on Windows release builds.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod app;
mod cli;
mod icons;
mod plot_data;
mod presentation;
mod psi;
mod range_bar;
mod report;
mod settings;
mod theme;
mod update;
mod views;

use eframe::egui;
use std::ffi::OsString;

const HELP: &str = "mpeg-ts-analyzer GUI\n\nUsage:\n  mpeg-ts-analyzer-gui [input file] [--offset BYTES] [--limit BYTES]\n  mpeg-ts-analyzer-gui --check-cli\n\nOptions:\n  --offset BYTES  Start analysis at this byte offset (default: 0)\n  --limit BYTES   Analyze at most this many bytes (default: 67108864; 0 = to EOF)\n  --check-cli     Check the bundled/discovered CLI without opening a window\n  -h, --help      Print this help\n";

#[derive(Debug, PartialEq)]
enum Startup {
    Gui(Option<app::LaunchRequest>),
    CheckCli,
    Help,
}

fn main() -> eframe::Result {
    let startup = parse_args(std::env::args_os().skip(1)).unwrap_or_else(|error| {
        eprintln!("{error}\n\n{HELP}");
        std::process::exit(2);
    });
    let launch = match startup {
        Startup::CheckCli => {
            // The CLI path from Settings is not read: packages are checked as
            // installed, before anyone has changed the settings.
            std::process::exit(if cli::report_check("") { 0 } else { 1 });
        }
        Startup::Help => {
            print!("{HELP}");
            return Ok(());
        }
        Startup::Gui(launch) => launch,
    };
    let mut viewport = egui::ViewportBuilder::default()
        .with_title("mpeg-ts-analyzer")
        // Matches the .desktop file of the Linux packages.
        .with_app_id("mpeg-ts-analyzer-gui")
        .with_inner_size([1280.0, 800.0])
        .with_drag_and_drop(true);
    if let Ok(icon) =
        eframe::icon_data::from_png_bytes(include_bytes!("../assets/icons/256x256.png"))
    {
        viewport = viewport.with_icon(icon);
    }
    let options = eframe::NativeOptions {
        viewport,
        ..Default::default()
    };
    eframe::run_native(
        "mpeg-ts-analyzer",
        options,
        Box::new(move |cc| {
            theme::apply(&cc.egui_ctx);
            Ok(Box::new(app::App::new(cc, launch)))
        }),
    )
}

fn parse_args(args: impl IntoIterator<Item = OsString>) -> Result<Startup, String> {
    let args: Vec<OsString> = args.into_iter().collect();
    if args.len() == 1 && args[0] == "--check-cli" {
        return Ok(Startup::CheckCli);
    }
    if args.len() == 1 && (args[0] == "--help" || args[0] == "-h") {
        return Ok(Startup::Help);
    }

    let mut input = None;
    let mut offset = 0;
    let mut limit = None;
    let mut positional = false;
    let mut index = 0;
    while index < args.len() {
        let argument = &args[index];
        let text = argument.to_str();
        if !positional && text == Some("--") {
            positional = true;
        } else if !positional && matches!(text, Some("--offset" | "--limit")) {
            let flag = text.unwrap();
            index += 1;
            let value = args
                .get(index)
                .ok_or_else(|| format!("{flag} needs a byte count"))?;
            set_range_arg(flag, value, &mut offset, &mut limit)?;
        } else if !positional
            && let Some((flag, value)) = text.and_then(|text| text.split_once('='))
            && matches!(flag, "--offset" | "--limit")
        {
            set_range_arg(flag, &OsString::from(value), &mut offset, &mut limit)?;
        } else if !positional && text.is_some_and(|text| text.starts_with('-')) {
            return Err(format!("unknown option: {}", argument.to_string_lossy()));
        } else if input.replace(argument.into()).is_some() {
            return Err("only one input file can be opened at startup".to_owned());
        }
        index += 1;
    }

    match input {
        Some(input) => Ok(Startup::Gui(Some(app::LaunchRequest {
            input,
            offset,
            limit,
        }))),
        None if offset != 0 || limit.is_some() => {
            Err("--offset and --limit require an input file".to_owned())
        }
        None => Ok(Startup::Gui(None)),
    }
}

fn set_range_arg(
    flag: &str,
    value: &std::ffi::OsStr,
    offset: &mut u64,
    limit: &mut Option<u64>,
) -> Result<(), String> {
    let value = value
        .to_str()
        .and_then(|value| value.parse::<u64>().ok())
        .ok_or_else(|| format!("{flag} must be a non-negative integer byte count"))?;
    match flag {
        "--offset" => *offset = value,
        "--limit" => *limit = Some(value),
        _ => unreachable!(),
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(args: &[&str]) -> Result<Startup, String> {
        parse_args(args.iter().map(OsString::from))
    }

    #[test]
    fn parses_an_input_range_for_automatic_analysis() {
        assert_eq!(
            parse(&["sample.ts", "--offset", "188", "--limit=376"]),
            Ok(Startup::Gui(Some(app::LaunchRequest {
                input: "sample.ts".into(),
                offset: 188,
                limit: Some(376),
            })))
        );
        assert_eq!(parse(&[]), Ok(Startup::Gui(None)));
        assert_eq!(parse(&["--check-cli"]), Ok(Startup::CheckCli));
    }

    #[test]
    fn rejects_ambiguous_or_invalid_arguments() {
        assert!(
            parse(&["--offset", "1"])
                .unwrap_err()
                .contains("input file")
        );
        assert!(parse(&["a.ts", "b.ts"]).is_err());
        assert!(parse(&["a.ts", "--limit", "-1"]).is_err());
        assert!(parse(&["--unknown"]).is_err());
    }
}
