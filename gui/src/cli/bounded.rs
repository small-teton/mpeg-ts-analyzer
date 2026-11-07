//! Bounded reading of what a CLI process writes.
//!
//! Any executable can be chosen as the CLI in Settings, so nothing it writes
//! may grow the GUI's memory without limit. A JSON document is parsed straight
//! from the pipe (the raw bytes never coexist with the parsed value) through a
//! [`Limited`] reader that rejects the first byte past the limit for that kind
//! of output instead of truncating it. stderr is read a bounded line at a time
//! and only a bounded tail of its diagnostics is kept ([`DiagnosticTail`]).
//! The limits are documented in `docs/gui/cli-interface.md`.

use std::collections::VecDeque;
use std::io::{self, BufRead, Read};

const KIB: u64 = 1024;
const MIB: u64 = 1024 * KIB;

/// The limits on what the GUI reads from a CLI run. [`Limits::DEFAULT`] is
/// used outside tests; tests make them small to prove the bounded behavior
/// without writing anything near the production sizes.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Limits {
    /// `--capabilities` JSON: a short protocol reply (about 500 bytes today).
    pub capabilities: u64,
    /// `--output packets` JSON: a fixed part plus a part per requested packet
    /// (about 300 bytes each today; a page of 100 is about 26 KiB).
    pub packets_base: u64,
    pub packets_per_packet: u64,
    /// `--output packet` JSON: one decoded packet (under 8 KiB today).
    pub packet_detail: u64,
    /// `--output analysis` JSON: a fixed part (PSI layout, summaries) plus a
    /// part per byte of the analyzed range for the PCR, PES, jitter and
    /// bitrate series. The samples write 0.22–0.24 bytes per input byte; one
    /// program with a PCR, a jitter sample and a timestamped PES start in every
    /// 188-byte packet writes about 1.3, so 4 leaves room for several programs
    /// that share PIDs.
    pub analysis_base: u64,
    pub analysis_per_input_byte: u64,
    /// The stderr diagnostics kept: the latest lines up to this many bytes.
    pub diagnostics: usize,
    /// The longest stderr line kept whole; the rest of a longer one is
    /// dropped and counted. Progress events are far shorter.
    pub diagnostic_line: usize,
}

impl Limits {
    pub const DEFAULT: Limits = Limits {
        capabilities: 64 * KIB,
        packets_base: MIB,
        packets_per_packet: 4 * KIB,
        packet_detail: MIB,
        analysis_base: 64 * MIB,
        analysis_per_input_byte: 4,
        diagnostics: 64 * 1024,
        diagnostic_line: 4 * 1024,
    };

    pub fn capabilities(&self) -> OutputLimit {
        OutputLimit {
            output: "--capabilities",
            bytes: self.capabilities,
            basis: "the limit for a capabilities reply".to_owned(),
            advice: "Check that the CLI chosen in Settings is mpeg-ts-analyzer.",
        }
    }

    pub fn packets_page(&self, count: u64) -> OutputLimit {
        OutputLimit {
            output: "--output packets",
            bytes: self
                .packets_base
                .saturating_add(count.saturating_mul(self.packets_per_packet)),
            basis: format!("the limit for a page of {count} packets"),
            advice: "Check the CLI chosen in Settings.",
        }
    }

    pub fn packet_detail(&self) -> OutputLimit {
        OutputLimit {
            output: "--output packet",
            bytes: self.packet_detail,
            basis: "the limit for one decoded packet".to_owned(),
            advice: "Check the CLI chosen in Settings.",
        }
    }

    pub fn analysis(&self, range_bytes: u64) -> OutputLimit {
        OutputLimit {
            output: "--output analysis",
            bytes: self
                .analysis_base
                .saturating_add(range_bytes.saturating_mul(self.analysis_per_input_byte)),
            basis: format!("the limit for a {} range", format_bytes(range_bytes)),
            advice: "Analyze a smaller range, or check the CLI chosen in Settings.",
        }
    }
}

/// The most the GUI reads of one kind of output.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct OutputLimit {
    /// The CLI option that writes the output.
    pub output: &'static str,
    pub bytes: u64,
    /// Why the limit is what it is, for the error message.
    pub basis: String,
    pub advice: &'static str,
}

impl OutputLimit {
    /// The error shown when the output went past the limit.
    pub fn rejected(&self) -> String {
        format!(
            "The CLI wrote more than {} of {} JSON ({}); the output was rejected. {}",
            format_bytes(self.bytes),
            self.output,
            self.basis,
            self.advice
        )
    }
}

/// `1536` → "1.5 KiB"; whole units without decimals.
pub fn format_bytes(bytes: u64) -> String {
    for (unit, size) in [("GiB", 1024 * MIB), ("MiB", MIB), ("KiB", KIB)] {
        if bytes >= size {
            return if bytes.is_multiple_of(size) {
                format!("{} {unit}", bytes / size)
            } else {
                format!("{:.1} {unit}", bytes as f64 / size as f64)
            };
        }
    }
    format!("{bytes} bytes")
}

/// How much of the start of the output [`Limited`] keeps when asked, so that
/// a document that fails to parse can still be identified by its envelope.
pub const PREFIX_BYTES: usize = 64 * 1024;

/// A reader that fails on the first byte past `limit` (the output is rejected,
/// never silently truncated) and notes whether anything but whitespace came.
pub struct Limited<R> {
    inner: R,
    limit: u64,
    read: u64,
    exceeded: bool,
    content: bool,
    prefix: Vec<u8>,
    prefix_bytes: usize,
}

impl<R: Read> Limited<R> {
    pub fn new(inner: R, limit: u64) -> Self {
        Self {
            inner,
            limit,
            read: 0,
            exceeded: false,
            content: false,
            prefix: Vec::new(),
            prefix_bytes: 0,
        }
    }

    /// Also keeps the first `bytes` read, for [`Limited::prefix`].
    pub fn keeping_prefix(mut self, bytes: usize) -> Self {
        self.prefix_bytes = bytes;
        self
    }

    /// Whether more than the limit was written.
    pub fn exceeded(&self) -> bool {
        self.exceeded
    }

    /// Whether anything other than whitespace was read.
    pub fn had_content(&self) -> bool {
        self.content
    }

    pub fn prefix(&self) -> &[u8] {
        &self.prefix
    }
}

impl<R: Read> Read for Limited<R> {
    fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
        if self.exceeded {
            return Err(too_large());
        }
        // One byte more than the limit allows is enough to know it was passed.
        let room = (self.limit - self.read).saturating_add(1);
        let wanted = buf.len().min(usize::try_from(room).unwrap_or(usize::MAX));
        let n = self.inner.read(&mut buf[..wanted])?;
        let data = &buf[..n];
        self.read += n as u64;
        self.content |= data.iter().any(|b| !b.is_ascii_whitespace());
        if self.prefix.len() < self.prefix_bytes {
            let keep = (self.prefix_bytes - self.prefix.len()).min(n);
            self.prefix.extend_from_slice(&data[..keep]);
        }
        if self.read > self.limit {
            self.exceeded = true;
            return Err(too_large());
        }
        Ok(n)
    }
}

fn too_large() -> io::Error {
    io::Error::other("the output is larger than its limit")
}

/// Reads one line into `line`, keeping at most `max` bytes of it; the rest of
/// a longer line is consumed and its length returned. `None` at the end of the
/// input. A missing final newline still ends a line.
pub fn read_line(
    reader: &mut impl BufRead,
    max: usize,
    line: &mut Vec<u8>,
) -> io::Result<Option<u64>> {
    line.clear();
    let mut dropped = 0u64;
    let mut started = false;
    loop {
        let buf = reader.fill_buf()?;
        if buf.is_empty() {
            return Ok(started.then_some(dropped));
        }
        started = true;
        let (part, end) = match buf.iter().position(|&b| b == b'\n') {
            Some(at) => (&buf[..at], Some(at + 1)),
            None => (buf, None),
        };
        let keep = (max - line.len()).min(part.len());
        line.extend_from_slice(&part[..keep]);
        dropped += (part.len() - keep) as u64;
        let consumed = end.unwrap_or(part.len());
        reader.consume(consumed);
        if end.is_some() {
            if line.last() == Some(&b'\r') {
                line.pop();
            }
            return Ok(Some(dropped));
        }
    }
}

/// The latest stderr diagnostics, at most `max_bytes` of them, with a count
/// of what was dropped to stay within that.
pub struct DiagnosticTail {
    max_bytes: usize,
    lines: VecDeque<String>,
    bytes: usize,
    omitted_lines: u64,
    omitted_bytes: u64,
}

impl DiagnosticTail {
    pub fn new(max_bytes: usize) -> Self {
        Self {
            max_bytes,
            lines: VecDeque::new(),
            bytes: 0,
            omitted_lines: 0,
            omitted_bytes: 0,
        }
    }

    /// Adds a line whose remaining `dropped` bytes were not kept.
    pub fn push(&mut self, line: &[u8], dropped: u64) {
        let mut text = String::from_utf8_lossy(line).into_owned();
        if dropped > 0 {
            text.push_str(&format!(" [… {dropped} more bytes of this line omitted]"));
        }
        self.bytes += text.len() + 1;
        self.lines.push_back(text);
        // The latest line is always kept; it is at most `diagnostic_line` long.
        while self.bytes > self.max_bytes
            && self.lines.len() > 1
            && let Some(old) = self.lines.pop_front()
        {
            self.bytes -= old.len() + 1;
            self.omitted_lines += 1;
            self.omitted_bytes += old.len() as u64 + 1;
        }
    }

    /// The kept lines, after a marker saying how much came before them.
    pub fn finish(self) -> String {
        let mut text = String::new();
        if self.omitted_lines > 0 {
            text.push_str(&format!(
                "[… {} earlier diagnostic lines ({}) omitted …]\n",
                self.omitted_lines,
                format_bytes(self.omitted_bytes)
            ));
        }
        for line in self.lines {
            text.push_str(&line);
            text.push('\n');
        }
        text
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Endless output: tests never need a buffer of the size they reject.
    struct Endless(u8);

    impl Read for Endless {
        fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
            buf.fill(self.0);
            Ok(buf.len())
        }
    }

    #[test]
    fn accepts_the_limit_and_rejects_one_byte_more() {
        let mut out = Vec::new();
        let mut exact = Limited::new(&[b'x'; 100][..], 100);
        assert_eq!(exact.read_to_end(&mut out).unwrap(), 100);
        assert!(!exact.exceeded() && exact.had_content());

        let mut over = Limited::new(&[b'x'; 101][..], 100);
        assert!(over.read_to_end(&mut Vec::new()).is_err());
        assert!(over.exceeded());

        let mut endless = Limited::new(Endless(b' '), 1000);
        let mut kept = Vec::new();
        assert!(endless.read_to_end(&mut kept).is_err());
        assert!(endless.exceeded() && !endless.had_content());
        assert!(kept.len() <= 1001, "{}", kept.len());
    }

    #[test]
    fn rejects_an_endless_document_while_parsing_it() {
        // `[1,1,1,…` never ends; parsing stops at the limit.
        let mut document = Limited::new(b"[".chain(Endless(b'1')), 10_000);
        let parsed: Result<Vec<u64>, _> =
            serde_json::from_reader(io::BufReader::new(&mut document));
        assert!(parsed.is_err());
        assert!(document.exceeded());
    }

    #[test]
    fn keeps_only_the_asked_prefix() {
        let mut reader = Limited::new(&b"0123456789"[..], 100).keeping_prefix(4);
        reader.read_to_end(&mut Vec::new()).unwrap();
        assert_eq!(reader.prefix(), b"0123");
    }

    #[test]
    fn reads_bounded_lines() {
        let mut input = io::BufReader::with_capacity(4, &b"short\r\nmuch too long\nend"[..]);
        let mut line = Vec::new();
        assert_eq!(read_line(&mut input, 8, &mut line).unwrap(), Some(0));
        assert_eq!(line, b"short");
        assert_eq!(read_line(&mut input, 8, &mut line).unwrap(), Some(5));
        assert_eq!(line, b"much too");
        assert_eq!(read_line(&mut input, 8, &mut line).unwrap(), Some(0));
        assert_eq!(line, b"end");
        assert_eq!(read_line(&mut input, 8, &mut line).unwrap(), None);
    }

    #[test]
    fn an_endless_line_is_read_in_bounded_memory() {
        let mut input = io::BufReader::new(Endless(b'x').take(1_000_000));
        let mut line = Vec::new();
        assert_eq!(
            read_line(&mut input, 16, &mut line).unwrap(),
            Some(1_000_000 - 16)
        );
        assert_eq!(line.len(), 16);
    }

    #[test]
    fn keeps_a_marked_tail_of_the_diagnostics() {
        let mut tail = DiagnosticTail::new(20);
        for n in 0..10 {
            tail.push(format!("line {n}").as_bytes(), 0);
        }
        // "line 7\nline 8\nline 9\n" is 21 bytes; the last two fit.
        assert_eq!(
            tail.finish(),
            "[… 8 earlier diagnostic lines (56 bytes) omitted …]\nline 8\nline 9\n"
        );
        let mut tail = DiagnosticTail::new(1000);
        tail.push(b"cut", 7);
        assert_eq!(tail.finish(), "cut [… 7 more bytes of this line omitted]\n");
    }

    #[test]
    fn describes_the_limits() {
        assert_eq!(format_bytes(512), "512 bytes");
        assert_eq!(format_bytes(1536), "1.5 KiB");
        assert_eq!(format_bytes(64 * MIB), "64 MiB");
        let limit = Limits::DEFAULT.analysis(64 * MIB);
        assert_eq!(limit.bytes, 320 * MIB);
        assert_eq!(
            limit.rejected(),
            "The CLI wrote more than 320 MiB of --output analysis JSON (the limit for a 64 MiB range); the output was rejected. Analyze a smaller range, or check the CLI chosen in Settings."
        );
        assert_eq!(
            Limits::DEFAULT.packets_page(100).bytes,
            MIB + 400 * KIB,
            "a page of 100 packets"
        );
        assert_eq!(Limits::DEFAULT.analysis(u64::MAX).bytes, u64::MAX);
    }
}
