//! The presentation boundary between the CLI's JSON documents and the views.
//!
//! ```text
//! CLI JSON DTOs (crate::report)       -- mirror the schema, tolerate additive fields
//!         -> presentation types (here) -- typed statuses, severity, scope, display text
//!         -> views (Overview, Timeline, Bitrate, Packets)
//! ```
//!
//! The DTOs keep each status as the string the CLI wrote, so a document with
//! a value this GUI does not know still parses. Views do not interpret those
//! strings themselves: they normalize them into [`CheckStatus`] and ask it
//! for its severity, badge text and classification. Nothing here decides a
//! verdict; the CLI remains the source of truth and this module only says how
//! the GUI shows what the CLI reported.

pub mod packet;
pub mod status;

pub use status::CheckStatus;

/// Where a finding applies.
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum Scope {
    /// The selected program, by program_number.
    Program(u16),
    /// Every PID of the range.
    Multiplex,
}

impl Scope {
    pub fn text(self) -> String {
        match self {
            Scope::Program(number) => format!("program {number}"),
            Scope::Multiplex => "whole multiplex".to_owned(),
        }
    }
}
