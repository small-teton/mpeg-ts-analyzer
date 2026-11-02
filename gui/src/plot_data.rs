//! Plot series derived from one program of the range analysis document.
//!
//! Time is elapsed program time in seconds (`elapsed_ms` of the CLI), which
//! stops across PCR discontinuities, so every plot shares one time axis.
//! Values that carry only a byte offset (events, jitter samples) are placed at
//! the elapsed time of the last PCR at or before that offset.

use crate::report::{AnalysisReport, PcrPoint, Program, stream_type_name};

/// The most points a line is drawn with; longer series are decimated.
pub const MAX_PLOT_POINTS: usize = 4000;

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Point {
    pub seconds: f64,
    pub value: f64,
    pub offset_bytes: u64,
}

#[derive(Debug, PartialEq)]
pub struct PidSeries {
    pub pid: String,
    pub label: String,
    pub points: Vec<Point>,
}

#[derive(Debug, PartialEq)]
pub struct Event {
    pub seconds: f64,
    pub offset_bytes: u64,
    pub pid: String,
    pub label: String,
}

/// What the Timeline draws for one program.
#[derive(Debug, Default)]
pub struct Timeline {
    /// PCR interval (ms) at each PCR that has one.
    pub pcr_interval: Vec<Point>,
    /// PCR→PTS/DTS gap (ms) per elementary-stream PID, in PID order.
    pub gaps: Vec<PidSeries>,
    /// Timestamp anomalies of the program and continuity errors on its PIDs.
    pub events: Vec<Event>,
}

/// What the Bitrate screen draws for one program.
#[derive(Debug, Default)]
pub struct Bitrate {
    /// Mbps per PID as steps over the windows, in PID order.
    pub pids: Vec<PidSeries>,
    /// The sum over the program's PIDs, without null packets.
    pub program: Vec<[f64; 2]>,
    /// PCR jitter (ms) per sample.
    pub jitter: Vec<Point>,
}

/// Elapsed seconds of the last PCR at or before `offset`, or of the first PCR
/// when `offset` comes earlier. `pcr` is in stream order.
pub fn seconds_at(pcr: &[PcrPoint], offset: u64) -> Option<f64> {
    let index = pcr.partition_point(|p| p.offset_bytes <= offset);
    pcr.get(index.saturating_sub(1))
        .map(|p| p.elapsed_ms / 1000.0)
}

/// A PID with what the program's PMT says it carries.
pub fn pid_label(program: &Program, pid: &str) -> String {
    match program.elementary_streams.iter().find(|s| s.pid == pid) {
        Some(stream) => format!("{pid} {}", stream_type_name(&stream.stream_type)),
        None if program.pcr_pid.as_deref() == Some(pid) => format!("{pid} PCR"),
        None => pid.to_owned(),
    }
}

pub fn timeline(report: &AnalysisReport, program: &Program) -> Timeline {
    let Some(series) = &program.series else {
        return Timeline::default();
    };
    let pcr_interval = series
        .pcr
        .iter()
        .filter_map(|p| {
            p.interval_ms.map(|ms| Point {
                seconds: p.elapsed_ms / 1000.0,
                value: ms,
                offset_bytes: p.offset_bytes,
            })
        })
        .collect();

    let mut gaps: Vec<PidSeries> = Vec::new();
    for pes in &series.pes {
        let (Some(elapsed), Some(gap)) = (pes.elapsed_ms, pes.pcr_gap_ms) else {
            continue;
        };
        let point = Point {
            seconds: elapsed / 1000.0,
            value: gap,
            offset_bytes: pes.offset_bytes,
        };
        match gaps.iter_mut().find(|s| s.pid == pes.pid) {
            Some(existing) => existing.points.push(point),
            None => gaps.push(PidSeries {
                pid: pes.pid.clone(),
                label: pid_label(program, &pes.pid),
                points: vec![point],
            }),
        }
    }
    gaps.sort_by(|a, b| a.pid.cmp(&b.pid));

    let program_pids: Vec<&str> = program
        .elementary_streams
        .iter()
        .map(|s| s.pid.as_str())
        .chain(program.pcr_pid.as_deref())
        .chain([program.pmt_pid.as_str()])
        .collect();
    let mut events: Vec<Event> = series
        .timestamp_events
        .iter()
        .filter_map(|e| {
            Some(Event {
                seconds: seconds_at(&series.pcr, e.offset_bytes)?,
                offset_bytes: e.offset_bytes,
                pid: e.pid.clone(),
                label: format!("{} {}", e.kind, e.pid),
            })
        })
        .collect();
    events.extend(
        report
            .continuity_events
            .iter()
            .filter(|e| program_pids.contains(&e.pid.as_str()))
            .filter_map(|e| {
                Some(Event {
                    seconds: seconds_at(&series.pcr, e.offset_bytes)?,
                    offset_bytes: e.offset_bytes,
                    pid: e.pid.clone(),
                    label: format!(
                        "CC {} expected {}, got {}",
                        e.pid, e.expected_cc, e.actual_cc
                    ),
                })
            }),
    );
    events.sort_by_key(|e| e.offset_bytes);

    Timeline {
        pcr_interval,
        gaps,
        events,
    }
}

pub fn bitrate(program: &Program) -> Bitrate {
    let Some(series) = &program.series else {
        return Bitrate::default();
    };
    let windows = &series.bitrate;
    let mut starts = Vec::with_capacity(windows.duration_seconds.len());
    let mut elapsed = 0.0;
    for duration in &windows.duration_seconds {
        starts.push(elapsed);
        elapsed += duration;
    }
    let steps = |bps: &dyn Fn(usize) -> f64| -> Vec<[f64; 2]> {
        let mut points = Vec::with_capacity(2 * starts.len());
        for (index, start) in starts.iter().enumerate() {
            let mbps = bps(index) / 1e6;
            points.push([*start, mbps]);
            points.push([start + windows.duration_seconds[index], mbps]);
        }
        points
    };

    let mut pids = Vec::new();
    let mut program_bytes = vec![0u64; starts.len()];
    for pid in &windows.pids {
        if pid.pid == NULL_PID {
            continue;
        }
        for (sum, bytes) in program_bytes.iter_mut().zip(&pid.bytes) {
            *sum += bytes;
        }
        pids.push(PidSeries {
            pid: pid.pid.clone(),
            label: format!("{} {}", pid.pid, carried_by(program, &pid.pid, &pid.label)),
            points: steps(&|i| pid.bps.get(i).copied().unwrap_or(0.0))
                .into_iter()
                .map(|[seconds, value]| Point {
                    seconds,
                    value,
                    offset_bytes: 0,
                })
                .collect(),
        });
    }
    let program_series = steps(&|i| program_bytes[i] as f64 * 8.0 / windows.duration_seconds[i]);

    let jitter = series
        .pcr_jitter
        .iter()
        .filter_map(|j| {
            Some(Point {
                seconds: seconds_at(&series.pcr, j.offset_bytes)?,
                value: j.jitter_ns / 1e6,
                offset_bytes: j.offset_bytes,
            })
        })
        .collect();

    Bitrate {
        pids,
        program: program_series,
        jitter,
    }
}

const NULL_PID: &str = "0x1fff";

/// What a PID carries: the codec for elementary streams, else the CLI's label
/// (PAT, PMT, ...).
pub fn carried_by(program: &Program, pid: &str, cli_label: &str) -> String {
    match program.elementary_streams.iter().find(|s| s.pid == pid) {
        Some(stream) => stream_type_name(&stream.stream_type),
        None => cli_label.to_owned(),
    }
}

/// Counts values into `bins` equal-width bins over their range. Returns
/// `(center, count, width)` per bin.
pub fn histogram(values: &[f64], bins: usize) -> Vec<(f64, f64, f64)> {
    let (Some(min), Some(max)) = (
        values.iter().copied().reduce(f64::min),
        values.iter().copied().reduce(f64::max),
    ) else {
        return Vec::new();
    };
    let bins = bins.max(1);
    let width = if max > min {
        (max - min) / bins as f64
    } else {
        1.0
    };
    let mut counts = vec![0.0; bins];
    for value in values {
        let index = (((value - min) / width) as usize).min(bins - 1);
        counts[index] += 1.0;
    }
    counts
        .into_iter()
        .enumerate()
        .map(|(index, count)| (min + width * (index as f64 + 0.5), count, width))
        .collect()
}

/// Reduces a series to at most about `max` points for drawing, keeping the
/// lowest and highest value of each bucket so spikes stay visible.
pub fn decimate(points: &[Point], max: usize) -> Vec<[f64; 2]> {
    if points.len() <= max || max < 2 {
        return points.iter().map(|p| [p.seconds, p.value]).collect();
    }
    let bucket = points.len().div_ceil(max / 2);
    let mut out = Vec::with_capacity(max + 2);
    for chunk in points.chunks(bucket) {
        let low = chunk
            .iter()
            .enumerate()
            .min_by(|a, b| a.1.value.total_cmp(&b.1.value))
            .map(|(i, _)| i);
        let high = chunk
            .iter()
            .enumerate()
            .max_by(|a, b| a.1.value.total_cmp(&b.1.value))
            .map(|(i, _)| i);
        let (Some(low), Some(high)) = (low, high) else {
            continue;
        };
        for index in [low.min(high), low.max(high)] {
            out.push([chunk[index].seconds, chunk[index].value]);
        }
    }
    out.dedup();
    out
}

/// The point nearest to `target` among `points`, by a caller-supplied distance.
pub fn nearest<'a>(
    points: impl Iterator<Item = &'a Point>,
    distance: impl Fn(&Point) -> f32,
) -> Option<(&'a Point, f32)> {
    points
        .map(|p| (p, distance(p)))
        .min_by(|a, b| a.1.total_cmp(&b.1))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture() -> AnalysisReport {
        crate::report::parse_analysis(
            include_bytes!("../tests/fixtures/analysis_two_programs_188.json"),
            "1",
        )
        .expect("fixture parses")
    }

    #[test]
    fn timeline_matches_the_cli_series() {
        let report = fixture();
        for program in &report.programs {
            let series = program.series.as_ref().unwrap();
            let timeline = timeline(&report, program);
            let intervals = series
                .pcr
                .iter()
                .filter(|p| p.interval_ms.is_some())
                .count();
            assert_eq!(timeline.pcr_interval.len(), intervals);
            let max_interval = timeline
                .pcr_interval
                .iter()
                .map(|p| p.value)
                .fold(0.0, f64::max);
            let compliance = program.compliance.as_ref().unwrap();
            assert_eq!(Some(max_interval), compliance.max_pcr_interval.value_ms);
            let max_gap = timeline
                .gaps
                .iter()
                .flat_map(|s| &s.points)
                .map(|p| p.value)
                .fold(0.0, f64::max);
            assert_eq!(Some(max_gap), compliance.max_pcr_pts_dts_gap.value_ms);
            // One gap series per elementary stream, labeled with its codec.
            assert_eq!(timeline.gaps.len(), program.elementary_streams.len());
            assert!(timeline.gaps[0].label.contains("MPEG-2 video"));
            for gap in &timeline.gaps {
                assert!(
                    gap.points
                        .windows(2)
                        .all(|w| w[0].offset_bytes <= w[1].offset_bytes)
                );
            }
        }
    }

    #[test]
    fn bitrate_steps_reproduce_the_averages() {
        let report = fixture();
        for program in &report.programs {
            let bitrate = bitrate(program);
            let summary = program.bitrate.as_ref().unwrap();
            let durations = &program.series.as_ref().unwrap().bitrate.duration_seconds;
            let seconds: f64 = durations.iter().sum();
            for pid in &bitrate.pids {
                // Each window is a horizontal step: the area under it is bytes * 8.
                let bits: f64 = pid
                    .points
                    .chunks(2)
                    .map(|step| step[0].value * 1e6 * (step[1].seconds - step[0].seconds))
                    .sum();
                let average = summary
                    .pids
                    .iter()
                    .find(|p| p.pid == pid.pid)
                    .unwrap()
                    .average_bps;
                assert!((bits / seconds - average).abs() < 1e-6, "{}", pid.pid);
            }
            let program_bits: f64 = bitrate
                .program
                .chunks(2)
                .map(|step| step[0][1] * 1e6 * (step[1][0] - step[0][0]))
                .sum();
            assert!((program_bits / seconds - summary.program_average_bps).abs() < 1e-6);
            assert!(bitrate.pids.iter().all(|p| p.pid != NULL_PID));
            assert_eq!(
                bitrate.jitter.len(),
                program.series.as_ref().unwrap().pcr_jitter.len()
            );
        }
    }

    #[test]
    fn places_offsets_on_the_pcr_time_axis() {
        let pcr = |offset_bytes, elapsed_ms| PcrPoint {
            offset_bytes,
            elapsed_ms,
            pcr_ms: 0.0,
            interval_ms: None,
            discontinuity: false,
        };
        let points = [pcr(100, 0.0), pcr(500, 40.0), pcr(900, 80.0)];
        assert_eq!(seconds_at(&points, 50), Some(0.0));
        assert_eq!(seconds_at(&points, 500), Some(0.04));
        assert_eq!(seconds_at(&points, 899), Some(0.04));
        assert_eq!(seconds_at(&points, 10_000), Some(0.08));
        assert_eq!(seconds_at(&[], 10), None);
    }

    #[test]
    fn decimation_keeps_spikes() {
        let points: Vec<Point> = (0..10_000)
            .map(|i| Point {
                seconds: i as f64,
                value: if i == 4321 { 500.0 } else { 40.0 },
                offset_bytes: i,
            })
            .collect();
        let drawn = decimate(&points, 1000);
        assert!(drawn.len() <= 1000);
        assert!(drawn.contains(&[4321.0, 500.0]));
        assert_eq!(decimate(&points[..10], 1000).len(), 10);
    }

    #[test]
    fn histogram_counts_every_value() {
        let bins = histogram(&[-1.0, 0.0, 0.5, 1.0], 4);
        assert_eq!(bins.len(), 4);
        assert_eq!(bins.iter().map(|b| b.1).sum::<f64>(), 4.0);
        assert_eq!(bins[3].1, 2.0); // 0.5 and the maximum fall in the last bin
        assert!(histogram(&[], 4).is_empty());
        assert_eq!(
            histogram(&[2.0, 2.0], 3).iter().map(|b| b.1).sum::<f64>(),
            2.0
        );
    }
}
