use std::path::PathBuf;

/// Overrides the CLI location, mainly for development against a locally built CLI.
const CLI_ENV: &str = "MPEG_TS_ANALYZER_CLI";
const CLI_NAME: &str = "mpeg-ts-analyzer";

pub fn locate() -> PathBuf {
    std::env::var_os(CLI_ENV).map_or_else(|| PathBuf::from(CLI_NAME), PathBuf::from)
}
