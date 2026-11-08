//! How the Packets views show a decoded packet: field values in one numeric
//! notation, and what kind of packet it is.
//!
//! The CLI writes each value in the notation of the specification's syntax
//! (`0x1001` for a PID, `23` for a section_length). The GUI shows numbers
//! decimal first with the hexadecimal in parentheses, padded to the field's
//! width: `4097 (0x1001)`, `23 (0x017)`. The JSON is left as it is.

use crate::report::{DecodedPacket, PacketField, PacketUnit};

/// Fields shown as the CLI writes them: a checksum is a bit pattern rather
/// than a quantity, and a fixed prefix is compared with the specification's
/// hexadecimal. The clock references are base × 300 + extension, a 27 MHz
/// count rather than the bits on the wire, so a hexadecimal of it would read
/// as an encoding it is not; their base and extension parts get one.
const AS_WRITTEN: [&str; 5] = [
    "CRC_32",
    "sync_byte",
    "packet_start_code_prefix",
    "program_clock_reference",
    "original_program_clock_reference",
];

/// Fields this wide or narrower stay decimal: one hexadecimal digit adds
/// nothing a reader needs (single-bit flags, continuity_counter).
const PLAIN_BITS: usize = 4;

/// A field value for display: decimal first with the hexadecimal padded to
/// `bits`, keeping any description the CLI appended, e.g.
/// `0x03 (11172 audio)` → `3 (0x03, 11172 audio)` and
/// `930600 (10.340000 s)` → `930600 (0x0000E3328, 10.340000 s)`.
/// Bit patterns (`111`, `000000000011100`), counts (`184 bytes`) and other
/// text are returned unchanged.
pub fn field_value(name: &str, value: &str, bits: usize) -> String {
    if AS_WRITTEN.contains(&name) {
        return value.to_owned();
    }
    let (number, note) = match value.split_once(' ') {
        Some((number, rest)) => match rest.strip_prefix('(').and_then(|r| r.strip_suffix(')')) {
            Some(note) => (number, Some(note)),
            None => return value.to_owned(),
        },
        None => (value, None),
    };
    let parsed = if let Some(hex) = number.strip_prefix("0x") {
        u64::from_str_radix(hex, 16).ok()
    } else if is_bit_pattern(number, bits) {
        None
    } else if number.bytes().all(|b| b.is_ascii_digit()) {
        number.parse::<u64>().ok()
    } else {
        None
    };
    let Some(n) = parsed else {
        return value.to_owned();
    };
    if bits <= PLAIN_BITS && !number.starts_with("0x") {
        return value.to_owned();
    }
    let digits = bits.div_ceil(4).max(1);
    let hex = format!("0x{n:0digits$X}");
    match note {
        Some(note) => format!("{n} ({hex}, {note})"),
        None => format!("{n} ({hex})"),
    }
}

/// Bits written out one by one: as many 0/1 digits as the field is wide.
/// A decimal number of two or more digits cannot be that: it would not fit.
fn is_bit_pattern(text: &str, bits: usize) -> bool {
    bits > 1 && text.len() == bits && text.bytes().all(|b| b == b'0' || b == b'1')
}

/// The width of a decoded field: its exact bits, else its whole bytes.
pub fn field_bits(field: &PacketField) -> usize {
    let bits: usize = field.bits.iter().map(|b| b.bit_length).sum();
    if bits > 0 {
        bits
    } else {
        field.byte_length * 8
    }
}

/// [`field_value`] of a decoded field.
pub fn field_text(field: &PacketField) -> String {
    field_value(&field.name, &field.value, field_bits(field))
}

/// What the packet is, for the detailed inspection page's badge: `PAT`,
/// `PMT`, a PSI/SI table name, `PES · H.264 video`, `PCR only`, `Null`...
/// `carries` is what the analysis says the packet's PID carries (see
/// `PsiLayout::label`); it names the stream of a PES packet.
pub fn packet_kind(packet: &DecodedPacket, unit: Option<&PacketUnit>, carries: &str) -> String {
    let field = |layer: &str, name: &str| {
        packet
            .layers
            .iter()
            .find(|l| l.name == layer)?
            .fields
            .iter()
            .find(|f| f.name == name)
            .map(|f| f.value.as_str())
    };
    let pid = field("ts_header", "PID").and_then(parse_hex);
    if pid == Some(0x1FFF) {
        return "Null".to_owned();
    }
    let has = |layer: &str| packet.layers.iter().any(|l| l.name == layer);
    if has("pat") {
        return "PAT".to_owned();
    }
    if has("pmt") {
        return "PMT".to_owned();
    }
    if has("psi_section") {
        return field("psi_section", "table_id")
            .and_then(parse_hex)
            .map_or_else(|| "PSI section".to_owned(), table_name);
    }
    let stream = || {
        let known = !carries.is_empty()
            && !matches!(carries, "PCR" | "unannounced" | "null" | "PAT")
            && !carries.starts_with("PMT");
        if known {
            return carries.to_owned();
        }
        let id = field("pes_header", "stream_id")
            .or_else(|| unit.and_then(|u| u.stream_id.as_deref()))
            .and_then(parse_hex);
        match id {
            Some(0xC0..=0xDF) => "audio".to_owned(),
            Some(0xE0..=0xEF) => "video".to_owned(),
            Some(0xBD) => "private stream 1".to_owned(),
            Some(0xBE) => "padding".to_owned(),
            Some(id) => format!("stream_id 0x{id:02X}"),
            None => String::new(),
        }
    };
    let joined = |head: &str, tail: String| {
        if tail.is_empty() {
            head.to_owned()
        } else {
            format!("{head} · {tail}")
        }
    };
    if has("pes_header") {
        return joined("PES", stream());
    }
    let adaptation_only =
        field("ts_header", "adaptation_field_control").is_some_and(|v| v.starts_with('2'));
    if adaptation_only {
        return if field("adaptation_field", "program_clock_reference").is_some() {
            "PCR only".to_owned()
        } else {
            "Adaptation field only".to_owned()
        };
    }
    match unit.map(|u| u.kind.as_str()) {
        Some("pes") => joined("PES payload", stream()),
        Some("psi") => {
            let table = unit
                .and_then(|u| u.table_id.as_deref())
                .and_then(parse_hex)
                .map_or_else(|| "PSI".to_owned(), table_name);
            format!("{table} · continued")
        }
        _ if !carries.is_empty() && carries != "unannounced" => carries.to_owned(),
        _ => "TS packet".to_owned(),
    }
}

fn parse_hex(text: &str) -> Option<u64> {
    let digits = text.split(' ').next()?.strip_prefix("0x")?;
    u64::from_str_radix(digits, 16).ok()
}

/// The table a table_id identifies (ISO/IEC 13818-1 Table 2-31, ETSI EN 300 468).
fn table_name(table_id: u64) -> String {
    match table_id {
        0x00 => "PAT",
        0x01 => "CAT",
        0x02 => "PMT",
        0x03 => "TSDT",
        0x40 => "NIT",
        0x41 => "NIT (other network)",
        0x42 => "SDT",
        0x46 => "SDT (other TS)",
        0x4A => "BAT",
        0x4E..=0x6F => "EIT",
        0x70 => "TDT",
        0x71 => "RST",
        0x72 => "ST",
        0x73 => "TOT",
        0x7E => "DIT",
        0x7F => "SIT",
        _ => return format!("PSI table 0x{table_id:02X}"),
    }
    .to_owned()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::report::PacketDetail;

    #[test]
    fn numbers_are_decimal_first_with_padded_hex() {
        assert_eq!(field_value("PID", "0x1001", 13), "4097 (0x1001)");
        assert_eq!(field_value("PID", "0x0000", 13), "0 (0x0000)");
        assert_eq!(field_value("table_id", "0x02", 8), "2 (0x02)");
        assert_eq!(field_value("section_length", "23", 12), "23 (0x017)");
        assert_eq!(field_value("program_number", "2", 16), "2 (0x0002)");
        assert_eq!(field_value("stream_id", "0xe0", 8), "224 (0xE0)");
        assert_eq!(field_value("version_number", "0", 5), "0 (0x00)");
        assert_eq!(
            field_value("stream_type", "0x03 (11172 audio)", 8),
            "3 (0x03, 11172 audio)"
        );
        assert_eq!(
            field_value("PTS", "930600 (10.340000 s)", 33),
            "930600 (0x0000E3328, 10.340000 s)"
        );
        assert_eq!(
            field_value("program_clock_reference_base", "921600", 33),
            "921600 (0x0000E1000)"
        );
    }

    #[test]
    fn flags_patterns_and_text_stay_as_written() {
        // Narrow fields, with or without a description.
        assert_eq!(field_value("PUSI", "1", 1), "1");
        assert_eq!(field_value("continuity_counter", "12", 4), "12");
        assert_eq!(
            field_value("adaptation_field_control", "1 (payload only)", 2),
            "1 (payload only)"
        );
        // Bit patterns, whatever their width.
        assert_eq!(field_value("reserved", "111", 3), "111");
        assert_eq!(field_value("reserved", "11", 2), "11");
        assert_eq!(
            field_value("PTS[29..15]", "000000000011100", 15),
            "000000000011100"
        );
        // Checksums, fixed prefixes and the 27 MHz clock references.
        assert_eq!(field_value("CRC_32", "0x1058355a", 32), "0x1058355a");
        assert_eq!(field_value("sync_byte", "0x47", 8), "0x47");
        assert_eq!(
            field_value("program_clock_reference", "276480000 (10.240000 s)", 42),
            "276480000 (10.240000 s)"
        );
        // Counts and other text.
        assert_eq!(field_value("payload", "184 bytes", 1472), "184 bytes");
        assert_eq!(
            field_value("stuffing_byte", "0xff × 146", 1168),
            "0xff × 146"
        );
        assert_eq!(field_value("x", "0xzz", 8), "0xzz");
        assert_eq!(field_value("x", "-1", 8), "-1");
    }

    fn fixture(json: &str) -> PacketDetail {
        serde_json::from_str(json).unwrap()
    }

    fn kind(detail: &PacketDetail, carries: &str) -> String {
        packet_kind(
            detail.packet.as_ref().unwrap(),
            detail.unit.as_ref(),
            carries,
        )
    }

    #[test]
    fn names_the_kind_of_packet() {
        let pat = fixture(include_str!(
            "../../tests/fixtures/packet_detail_pat_188.json"
        ));
        assert_eq!(kind(&pat, "PAT"), "PAT");
        let pmt = fixture(include_str!(
            "../../tests/fixtures/packet_detail_pmt_1001_188.json"
        ));
        assert_eq!(kind(&pmt, "PMT (program 2)"), "PMT");
        let pes = fixture(include_str!(
            "../../tests/fixtures/packet_detail_pes_pcr_192.json"
        ));
        assert_eq!(kind(&pes, "H.264 video"), "PES · H.264 video");
        // Without the analysis' stream type, the stream_id's class.
        assert_eq!(kind(&pes, ""), "PES · video");
        assert_eq!(kind(&pes, "unannounced"), "PES · video");
    }

    /// The PMT fixture edited into other kinds of packet.
    fn edited(edit: impl FnOnce(&mut serde_json::Value)) -> PacketDetail {
        let mut document: serde_json::Value = serde_json::from_str(include_str!(
            "../../tests/fixtures/packet_detail_pmt_1001_188.json"
        ))
        .unwrap();
        edit(&mut document);
        serde_json::from_value(document).unwrap()
    }

    fn set_ts(document: &mut serde_json::Value, name: &str, value: &str) {
        for field in document["packet"]["layers"][0]["fields"]
            .as_array_mut()
            .unwrap()
        {
            if field["name"] == name {
                field["value"] = value.into();
            }
        }
    }

    #[test]
    fn names_null_adaptation_only_and_continued_packets() {
        let null = edited(|d| set_ts(d, "PID", "0x1fff"));
        assert_eq!(kind(&null, "null"), "Null");

        // Adaptation field only, with and without a PCR.
        let adaptation = |pcr: bool| {
            edited(|d| {
                set_ts(d, "adaptation_field_control", "2 (adaptation field only)");
                let mut fields = vec![serde_json::json!({
                    "name": "adaptation_field_length", "value": "183",
                    "byte_offset": 4, "byte_length": 1})];
                if pcr {
                    fields.push(serde_json::json!({
                        "name": "program_clock_reference", "value": "0 (0.000000 s)",
                        "byte_offset": 6, "byte_length": 6}));
                }
                d["packet"]["layers"] = serde_json::json!([
                    d["packet"]["layers"][0].clone(),
                    {"name": "adaptation_field", "fields": fields},
                ]);
                d["unit"] = serde_json::Value::Null;
            })
        };
        assert_eq!(kind(&adaptation(true), "PCR"), "PCR only");
        assert_eq!(kind(&adaptation(false), ""), "Adaptation field only");

        // Payload of a unit that started in an earlier packet.
        let continued = |unit: serde_json::Value| {
            edited(|d| {
                d["packet"]["layers"] = serde_json::json!([
                    d["packet"]["layers"][0].clone(),
                    d["packet"]["layers"][1].clone(),
                ]);
                d["unit"] = unit;
            })
        };
        let pes = continued(serde_json::json!({
            "kind": "pes", "position": 2, "packets_in_range": 3, "stream_id": "0xc0"}));
        assert_eq!(kind(&pes, "AAC audio"), "PES payload · AAC audio");
        assert_eq!(kind(&pes, ""), "PES payload · audio");
        let psi = continued(serde_json::json!({
            "kind": "psi", "position": 2, "packets_in_range": 2, "table_id": "0x42"}));
        assert_eq!(kind(&psi, ""), "SDT · continued");
        let other = continued(serde_json::Value::Null);
        assert_eq!(kind(&other, "PCR"), "PCR");
        assert_eq!(kind(&other, "unannounced"), "TS packet");

        // A section of another table.
        let sdt = edited(|d| {
            let layer = &mut d["packet"]["layers"][2];
            layer["name"] = "psi_section".into();
            layer["fields"][1]["value"] = "0x42".into();
        });
        assert_eq!(kind(&sdt, ""), "SDT");
        assert_eq!(table_name(0x50), "EIT");
        assert_eq!(table_name(0xC8), "PSI table 0xC8");
    }
}
