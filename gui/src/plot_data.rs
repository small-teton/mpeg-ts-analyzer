//! Plot series derived from one program of the range analysis document.
//!
//! Time is elapsed program time in seconds (`elapsed_ms` of the CLI), which
//! stops across PCR discontinuities, so every plot shares one time axis.
//! Values that carry only a byte offset (events, jitter samples) are placed at
//! the elapsed time of the last PCR at or before that offset.

use egui_plot::PlotPoint;

use crate::psi::{self, NULL_PID, ProgramPids};
use crate::report::{AnalysisReport, PcrPoint, Program};

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
    /// `points` reduced to at most about `MAX_PLOT_POINTS` for drawing, in
    /// the form egui_plot borrows, so frames draw it without a copy.
    pub drawn: Vec<PlotPoint>,
}

/// PCR→PTS/DTS gaps of one PID.
#[derive(Debug, PartialEq)]
pub struct GapSeries {
    pub pid: String,
    pub label: String,
    pub points: Vec<Point>,
    /// `points` reduced for drawing, one line per stretch of layouts that list
    /// the PID, so a PID that leaves and comes back is not joined across.
    pub drawn: Vec<Vec<PlotPoint>>,
}

/// An applied PAT or PMT change.
#[derive(Debug, PartialEq)]
pub struct Marker {
    pub seconds: f64,
    pub offset_bytes: u64,
    pub pid: String,
    pub label: String,
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
    /// Valid horizontal plot range in elapsed seconds.
    pub time_range: (f64, f64),
    /// PCR interval (ms) at each PCR that has one.
    pub pcr_interval: Vec<Point>,
    /// `pcr_interval` reduced for drawing.
    pub pcr_drawn: Vec<PlotPoint>,
    /// PCR intervals above the compliance limit, drawn as markers.
    pub pcr_over_limit: Vec<PlotPoint>,
    /// PCR→PTS/DTS gap (ms) per elementary-stream PID, in PID order.
    pub gaps: Vec<GapSeries>,
    /// PAT changes and the program's PMT changes, drawn as vertical lines.
    pub markers: Vec<Marker>,
    /// Timestamp anomalies of the program and continuity errors on its PIDs.
    pub events: Vec<Event>,
    /// `events` as markers on the zero line of the events plot.
    pub event_drawn: Vec<PlotPoint>,
}

/// What the Bitrate screen draws for one program.
#[derive(Debug, Default)]
pub struct Bitrate {
    /// Valid horizontal plot range in elapsed seconds.
    pub time_range: (f64, f64),
    /// Mbps per PID as steps over the windows, in PID order, without PAT and
    /// PMT (see `ProgramPids::is_psi`).
    pub pids: Vec<PidSeries>,
    /// The sum over the program's PIDs, without null packets, as steps
    /// reduced for drawing.
    pub program_drawn: Vec<PlotPoint>,
    /// PCR jitter (ms) per sample.
    pub jitter: Vec<Point>,
    /// `jitter` reduced for drawing.
    pub jitter_drawn: Vec<PlotPoint>,
    /// Histogram of `jitter`: (center, count, width) per bin.
    pub jitter_histogram: Vec<(f64, f64, f64)>,
    /// Fixed Y range of the bitrate plot (Mbps), from 0 to above the highest window.
    pub mbps_range: (f64, f64),
    /// Fixed Y range of the jitter plot (ms), symmetric around 0.
    pub jitter_range: (f64, f64),
    /// What the program's PIDs carry, for the table.
    pub program_pids: ProgramPids,
}

/// Bins of the jitter histogram.
const HISTOGRAM_BINS: usize = 30;

/// Elapsed seconds of the last PCR at or before `offset`, or of the first PCR
/// when `offset` comes earlier. `pcr` is in stream order.
pub fn seconds_at(pcr: &[PcrPoint], offset: u64) -> Option<f64> {
    let index = pcr.partition_point(|p| p.offset_bytes <= offset);
    pcr.get(index.saturating_sub(1))
        .map(|p| p.elapsed_ms / 1000.0)
}

pub fn timeline(report: &AnalysisReport, program: &Program) -> Timeline {
    let Some(series) = &program.series else {
        return Timeline {
            time_range: EMPTY_TIME_RANGE,
            ..Timeline::default()
        };
    };
    let pcr_interval: Vec<Point> = series
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
    let limit = program
        .compliance
        .as_ref()
        .map_or(100.0, |c| c.max_pcr_interval.limit_ms);
    // Markers come from the reduced series, which keeps each bucket's highest
    // interval, so they stay bounded and every bucket over the limit shows.
    let pcr_drawn = plot_points(decimate(&pcr_interval, MAX_PLOT_POINTS));
    let pcr_over_limit = pcr_drawn.iter().copied().filter(|p| p.y > limit).collect();

    let program_pids = ProgramPids::new(report, program);
    let mut gaps: Vec<GapSeries> = Vec::new();
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
            None => gaps.push(GapSeries {
                pid: pes.pid.clone(),
                label: format!("{} {}", pes.pid, program_pids.carried_by(&pes.pid, ""))
                    .trim_end()
                    .to_owned(),
                points: vec![point],
                drawn: Vec::new(),
            }),
        }
    }
    gaps.sort_by(|a, b| a.pid.cmp(&b.pid));
    // Only where a layout of the program lists the PID.
    for gap in &mut gaps {
        gap.points
            .retain(|p| program_pids.presence(&gap.pid, p.offset_bytes).is_some());
    }
    gaps.retain(|gap| !gap.points.is_empty());
    for gap in &mut gaps {
        let presence = |point: &Point| program_pids.presence(&gap.pid, point.offset_bytes);
        let stretches: Vec<&[Point]> = gap
            .points
            .chunk_by(|a, b| presence(a) == presence(b))
            .collect();
        let budget = MAX_PLOT_POINTS / stretches.len().max(1);
        gap.drawn = stretches
            .into_iter()
            .map(|points| plot_points(decimate(points, budget)))
            .collect();
    }
    let markers: Vec<Marker> = report
        .psi_transitions
        .iter()
        .filter(|t| psi::concerns(t, program))
        .filter_map(|t| {
            Some(Marker {
                seconds: seconds_at(&series.pcr, t.offset_bytes)?,
                offset_bytes: t.offset_bytes,
                pid: t.pid.clone(),
                label: psi::describe(t),
            })
        })
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
            .filter(|e| program_pids.belongs(&e.pid, e.offset_bytes))
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
    let event_drawn = events
        .iter()
        .map(|e| PlotPoint::new(e.seconds, 0.0))
        .collect();

    let elapsed = series
        .pcr
        .last()
        .map_or(0.0, |point| point.elapsed_ms / 1000.0);

    Timeline {
        time_range: elapsed_range(elapsed),
        pcr_drawn,
        pcr_interval,
        pcr_over_limit,
        gaps,
        markers,
        events,
        event_drawn,
    }
}

/// Y ranges for a program with nothing to plot.
const EMPTY_MBPS_RANGE: (f64, f64) = (0.0, 1.0);
const EMPTY_JITTER_RANGE: (f64, f64) = (-0.001, 0.001);
const EMPTY_TIME_RANGE: (f64, f64) = (0.0, 1.0);

fn elapsed_range(seconds: f64) -> (f64, f64) {
    if seconds.is_finite() && seconds > 0.0 {
        (0.0, seconds)
    } else {
        EMPTY_TIME_RANGE
    }
}

pub fn bitrate(report: &AnalysisReport, program: &Program) -> Bitrate {
    let program_pids = ProgramPids::new(report, program);
    // A program whose PMT could not be read has no series.
    let Some(series) = &program.series else {
        return Bitrate {
            time_range: EMPTY_TIME_RANGE,
            mbps_range: EMPTY_MBPS_RANGE,
            jitter_range: EMPTY_JITTER_RANGE,
            program_pids,
            ..Bitrate::default()
        };
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
        if program_pids.is_psi(&pid.pid) {
            continue;
        }
        let pid_steps = steps(&|i| pid.bps.get(i).copied().unwrap_or(0.0));
        pids.push(PidSeries {
            pid: pid.pid.clone(),
            label: format!(
                "{} {}",
                pid.pid,
                program_pids.carried_by(&pid.pid, &pid.label)
            ),
            drawn: plot_points(decimate_steps(&pid_steps, MAX_PLOT_POINTS)),
            points: pid_steps
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

    let jitter: Vec<Point> = series
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

    let values: Vec<f64> = jitter.iter().map(|p| p.value).collect();
    let top = program_series.iter().map(|p| p[1]).fold(0.0, f64::max);
    let deviation = values.iter().map(|v| v.abs()).fold(0.0, f64::max);
    Bitrate {
        time_range: elapsed_range(elapsed),
        mbps_range: (0.0, padded(top, EMPTY_MBPS_RANGE.1)),
        jitter_range: {
            let half = padded(deviation, EMPTY_JITTER_RANGE.1);
            (-half, half)
        },
        pids,
        program_drawn: plot_points(decimate_steps(&program_series, MAX_PLOT_POINTS)),
        jitter_drawn: plot_points(decimate(&jitter, MAX_PLOT_POINTS)),
        jitter_histogram: histogram(&values, HISTOGRAM_BINS),
        jitter,
        program_pids,
    }
}

/// `value` with 10% headroom, or `fallback` when there is nothing to show.
fn padded(value: f64, fallback: f64) -> f64 {
    if value > 0.0 { value * 1.1 } else { fallback }
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

/// Coordinates as egui_plot points. Both are two `f64`s, so the collection
/// reuses the vector's allocation.
fn plot_points(coordinates: Vec<[f64; 2]>) -> Vec<PlotPoint> {
    coordinates.into_iter().map(PlotPoint::from).collect()
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

/// Reduces a step series (two points per window: its start and end) to at
/// most `max` points. The first and last windows and each bucket's lowest and
/// highest window are kept, so spikes and both ends of the range stay visible.
/// The path uses horizontal and vertical lines only. After a kept window it
/// steps to the value of the window that really follows, which then stands
/// for the dropped stretch until the next kept window, so a kept spike stays
/// as wide as it is.
pub fn decimate_steps(steps: &[[f64; 2]], max: usize) -> Vec<[f64; 2]> {
    let windows: &[[[f64; 2]; 2]] = steps.as_chunks::<2>().0;
    if steps.len() <= max || max < 20 {
        return steps.to_vec();
    }
    // Each kept window takes at most five points; two per bucket, plus the
    // first and last window.
    let buckets = (max / 5 - 2) / 2;
    let bucket = windows.len().div_ceil(buckets);
    let mut kept: Vec<usize> = vec![0];
    for (index, chunk) in windows.chunks(bucket).enumerate() {
        let base = index * bucket;
        let by = |better: fn(f64, f64) -> bool| {
            (0..chunk.len())
                .reduce(|best, i| {
                    if better(chunk[i][0][1], chunk[best][0][1]) {
                        i
                    } else {
                        best
                    }
                })
                .unwrap_or(0)
        };
        let (low, high) = (by(|a, b| a < b), by(|a, b| a > b));
        kept.push(base + low.min(high));
        kept.push(base + low.max(high));
    }
    kept.push(windows.len() - 1);
    kept.sort_unstable();
    kept.dedup();

    let mut out: Vec<[f64; 2]> = Vec::with_capacity(5 * kept.len());
    for index in kept {
        let [[start, value], end] = windows[index];
        if let Some(&[x, y]) = out.last() {
            if x != start {
                out.push([start, y]);
            }
            if y != value {
                out.push([start, value]);
            }
        } else {
            out.push([start, value]);
        }
        out.push(end);
        if let Some(&[[_, next], _]) = windows.get(index + 1)
            && next != value
        {
            out.push([end[0], next]);
        }
    }
    out
}

/// The point nearest to `target` among `points`, by a caller-supplied distance.
pub fn nearest(
    points: impl Iterator<Item = Point>,
    distance: impl Fn(&Point) -> f32,
) -> Option<(Point, f32)> {
    points
        .map(|p| (p, distance(&p)))
        .min_by(|a, b| a.1.total_cmp(&b.1))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture() -> AnalysisReport {
        crate::report::parse_analysis(
            include_bytes!("../tests/fixtures/analysis_two_programs_188.json"),
            "2",
        )
        .expect("fixture parses")
    }

    #[test]
    fn timeline_matches_the_cli_series() {
        let report = fixture();
        for program in &report.programs {
            let series = program.series.as_ref().unwrap();
            let timeline = timeline(&report, program);
            assert_eq!(
                timeline.time_range,
                (0.0, series.pcr.last().unwrap().elapsed_ms / 1000.0)
            );
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
            let bitrate = bitrate(&report, program);
            let summary = program.bitrate.as_ref().unwrap();
            let durations = &program.series.as_ref().unwrap().bitrate.duration_seconds;
            let seconds: f64 = durations.iter().sum();
            assert_eq!(bitrate.time_range, (0.0, seconds));
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
            // The fixture is short enough to be drawn without reduction.
            let program_bits: f64 = bitrate
                .program_drawn
                .chunks(2)
                .map(|step| step[0].y * 1e6 * (step[1].x - step[0].x))
                .sum();
            assert!((program_bits / seconds - summary.program_average_bps).abs() < 1e-6);
            assert!(bitrate.pids.iter().all(|p| p.pid != NULL_PID));
            assert!(
                bitrate
                    .pids
                    .iter()
                    .all(|p| !bitrate.program_pids.is_psi(&p.pid))
            );
            assert!(!bitrate.pids.is_empty());
            // The fixed Y ranges hold every drawn value.
            let (low, high) = bitrate.mbps_range;
            assert!(low == 0.0 && bitrate.program_drawn.iter().all(|p| p.y < high));
            let (low, high) = bitrate.jitter_range;
            assert!(low == -high && bitrate.jitter.iter().all(|p| p.value.abs() < high));
            assert_eq!(
                bitrate.jitter.len(),
                program.series.as_ref().unwrap().pcr_jitter.len()
            );
        }
    }

    /// egui_plot needs min < max for fixed bounds.
    #[test]
    fn bitrate_without_series_has_valid_ranges() {
        let mut report = fixture();
        report.programs[0].series = None;
        let data = bitrate(&report, &report.programs[0]);
        let timeline = timeline(&report, &report.programs[0]);
        assert_eq!(data.time_range, EMPTY_TIME_RANGE);
        assert_eq!(timeline.time_range, EMPTY_TIME_RANGE);
        assert_eq!(data.mbps_range, EMPTY_MBPS_RANGE);
        assert_eq!(data.jitter_range, EMPTY_JITTER_RANGE);
        assert!(data.mbps_range.0 < data.mbps_range.1 && data.jitter_range.0 < data.jitter_range.1);
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
    fn step_decimation_keeps_spike_windows_whole() {
        let mut steps = Vec::new();
        for window in 0..100_000 {
            // The first window is neither the lowest nor the highest of its bucket.
            let mbps = match window {
                0 => 5.0,
                1 => 0.5,
                2 => 6.0,
                61_234 => 9.0,
                _ => 1.0,
            };
            steps.push([window as f64, mbps]);
            steps.push([window as f64 + 1.0, mbps]);
        }
        let drawn = decimate_steps(&steps, 4000);
        assert!(drawn.len() <= 4000, "{}", drawn.len());
        // Only horizontal and vertical joins, from the first point to the last.
        assert!(
            drawn
                .windows(2)
                .all(|pair| pair[0][0] == pair[1][0] || pair[0][1] == pair[1][1])
        );
        assert_eq!(drawn.first(), steps.first());
        assert_eq!(drawn.last(), steps.last());
        // The spike is one window wide: up at its start, back down at its end.
        let up = drawn.iter().position(|p| *p == [61_234.0, 9.0]).unwrap();
        assert_eq!(drawn[up - 1], [61_234.0, 1.0]);
        assert_eq!(drawn[up + 1], [61_235.0, 9.0]);
        assert_eq!(drawn[up + 2], [61_235.0, 1.0]);
        assert_eq!(decimate_steps(&steps[..20], 4000), steps[..20]);
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

    /// The PMT change replaces video 0x0100 by 0x0200 and changes the audio
    /// stream type; the PAT change moves the PMT to 0x1100.
    #[test]
    fn follows_pat_and_pmt_changes() {
        for sample in crate::psi::tests::VERSION_CHANGE {
            let Some(report) = crate::psi::tests::analyze(sample) else {
                return;
            };
            let program = &report.programs[0];
            let timeline = timeline(&report, program);
            let series = program.series.as_ref().unwrap();
            let changes: Vec<(u64, Option<f64>)> = report
                .psi_transitions
                .iter()
                .map(|t| (t.offset_bytes, seconds_at(&series.pcr, t.offset_bytes)))
                .collect();
            let markers: Vec<(u64, Option<f64>)> = timeline
                .markers
                .iter()
                .map(|m| (m.offset_bytes, Some(m.seconds)))
                .collect();
            assert_eq!(markers, changes, "{sample}");
            assert!(timeline.markers[0].label.starts_with("PMT 1 v0 → v1"));

            let gap = |pid: &str| timeline.gaps.iter().find(|g| g.pid == pid).unwrap();
            let change = timeline.markers[0].offset_bytes;
            assert!(
                gap("0x0100")
                    .points
                    .iter()
                    .all(|p| p.offset_bytes <= change)
            );
            assert!(gap("0x0200").points.iter().all(|p| p.offset_bytes > change));
            assert_eq!(gap("0x0200").label, "0x0200 MPEG-2 video");
            assert_eq!(gap("0x0101").label, "0x0101 MPEG-1 audio → MPEG-2 audio");
            for pid in ["0x0100", "0x0101", "0x0200"] {
                assert_eq!(gap(pid).drawn.len(), 1, "{sample} {pid}");
            }

            let bitrate = bitrate(&report, program);
            let drawn: Vec<&str> = bitrate.pids.iter().map(|p| p.pid.as_str()).collect();
            assert_eq!(drawn, ["0x0100", "0x0101", "0x0200"], "{sample}");
            for pid in ["0x0000", "0x1000", "0x1100"] {
                assert!(bitrate.program_pids.is_psi(pid));
            }
        }
    }

    /// A PID that a later layout drops and a still later one lists again is
    /// drawn as two lines, with nothing where it is absent. Continuity events
    /// count for the program only where its layout lists their PID.
    #[test]
    fn splits_gap_lines_where_the_pid_is_absent() {
        let mut document: serde_json::Value = serde_json::from_slice(include_bytes!(
            "../tests/fixtures/analysis_two_programs_188.json"
        ))
        .unwrap();
        let program = &document["programs"][0];
        let audio = program["elementary_streams"][1]["pid"].clone();
        let offsets: Vec<u64> = program["series"]["pes"]
            .as_array()
            .unwrap()
            .iter()
            .filter(|p| p["pid"] == audio && p["pcr_gap_ms"].is_number())
            .map(|p| p["offset_bytes"].as_u64().unwrap())
            .collect();
        let (low, high) = (offsets[offsets.len() / 3], offsets[2 * offsets.len() / 3]);
        let entry = |streams: serde_json::Value| {
            serde_json::json!({
                "program_number": program["program_number"],
                "pmt_pid": program["pmt_pid"],
                "pcr_pid": program["pcr_pid"],
                "elementary_streams": streams,
            })
        };
        let all = program["elementary_streams"].clone();
        let video = serde_json::json!([
            program["elementary_streams"][0],
            {"pid": "0x0300", "stream_type": "0x02", "stream_type_description": "video"},
        ]);
        let layout = |start: u64, end: u64, streams: serde_json::Value| {
            serde_json::json!({
                "offset_bytes": start, "end_offset_bytes": end, "transition_indices": [],
                "pat_version": 0, "pat_offset_bytes": 0, "programs": [entry(streams)],
            })
        };
        document["psi_layout"] = serde_json::json!({
            "sections": [],
            "layouts": [
                layout(0, low, all.clone()),
                layout(low, high, video),
                layout(high, u64::MAX, all),
            ],
            "pids": [],
        });
        let event = |pid: &serde_json::Value, offset_bytes: u64| serde_json::json!({"offset_bytes": offset_bytes, "pid": pid, "expected_cc": 1, "actual_cc": 3});
        let added = serde_json::json!("0x0300");
        document["continuity_events"] = serde_json::json!([
            event(&added, offsets[0]),
            event(&audio, low - 1),
            event(&audio, low + 1),
            event(&added, low + 1),
            event(&audio, high + 1),
        ]);
        let report = crate::report::parse_analysis(document.to_string().as_bytes(), "2").unwrap();
        let timeline = timeline(&report, &report.programs[0]);
        let audio = timeline
            .gaps
            .iter()
            .find(|g| g.pid == audio.as_str().unwrap())
            .unwrap();
        assert_eq!(audio.drawn.len(), 2);
        assert!(
            audio
                .points
                .iter()
                .all(|p| !(low..high).contains(&p.offset_bytes))
        );
        let drawn: usize = audio.drawn.iter().map(Vec::len).sum();
        assert_eq!(drawn, audio.points.len());
        assert_eq!(timeline.gaps[0].drawn.len(), 1);
        let events: Vec<(u64, &str)> = timeline
            .events
            .iter()
            .map(|e| (e.offset_bytes, e.pid.as_str()))
            .collect();
        let audio = audio.pid.as_str();
        assert_eq!(
            events,
            [(low - 1, audio), (low + 1, "0x0300"), (high + 1, audio)]
        );
    }

    #[test]
    fn bounds_the_pcr_markers() {
        let pcr: Vec<String> = (0..20_000)
            .map(|i| {
                format!(
                    r#"{{"offset_bytes":{},"elapsed_ms":{},"pcr_ms":0,"interval_ms":150}}"#,
                    i * 188,
                    i * 150
                )
            })
            .collect();
        let mut report: serde_json::Value = serde_json::from_slice(include_bytes!(
            "../tests/fixtures/analysis_two_programs_188.json"
        ))
        .unwrap();
        report["programs"][0]["series"]["pcr"] =
            serde_json::from_str(&format!("[{}]", pcr.join(","))).unwrap();
        let report = crate::report::parse_analysis(report.to_string().as_bytes(), "2").unwrap();
        let timeline = timeline(&report, &report.programs[0]);
        assert_eq!(timeline.pcr_interval.len(), 20_000);
        assert!(timeline.pcr_drawn.len() <= MAX_PLOT_POINTS);
        assert!(!timeline.pcr_over_limit.is_empty());
        assert!(timeline.pcr_over_limit.len() <= MAX_PLOT_POINTS);
    }
}
