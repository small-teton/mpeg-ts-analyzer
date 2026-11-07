package tsparser

import (
	"fmt"

	"github.com/small-teton/mpeg-ts-analyzer/v2/internal/analysis"
)

// Mux lead and audio/video lead difference (report-only multiplexing
// diagnostics).
//
// The leads are aggregated by analysis.PESCheck from the same PES
// observations as the PES header and PTS repetition checks (see
// pes_health.go): every PES packet with a valid header and a PTS, placed on the
// program's PCR timeline by arrivalAt. arrivalAt interpolates between the same
// two PCRs by byte position as Pes.referencePcrMs, so a lead is the value
// Pes.BracketedTimestampDelay gives the PCR-PTS/DTS gap check for the same PES
// packet; the gap check keeps its largest value per program and judges it,
// the mux lead only reports. The result carries no verdict and never affects
// the exit status.

// ReportMuxLead is the mux lead of every elementary PID of the analyzed
// programs and the A/V lead difference of every program with a video and an
// audio stream.
type ReportMuxLead struct {
	TimeBasis string                `json:"time_basis"`
	PIDs      []ReportMuxLeadPID    `json:"pids"`
	Programs  []ReportAVLeadProgram `json:"programs"`
}

// ReportMuxLeadPID is the mux lead of one elementary PID. The lead fields are
// present when at least one lead was measured.
type ReportMuxLeadPID struct {
	ReportPESStream
	AVClass            string   `json:"av_class"`
	Status             string   `json:"status"`
	Reason             string   `json:"reason,omitempty"`
	SampleCount        int      `json:"sample_count"`
	UnbracketedCount   int      `json:"unbracketed_count"`
	DTSSampleCount     int      `json:"dts_sample_count"`
	NegativeCount      int      `json:"negative_count"`
	MinLeadMs          *float64 `json:"min_lead_ms,omitempty"`
	MinLeadOffsetBytes *int64   `json:"min_lead_offset_bytes,omitempty"`
	MaxLeadMs          *float64 `json:"max_lead_ms,omitempty"`
	MaxLeadOffsetBytes *int64   `json:"max_lead_offset_bytes,omitempty"`
	SpreadMs           *float64 `json:"spread_ms,omitempty"`
}

// ReportAVLeadDifference is the largest lead difference between an audio PID
// and the program's video. The difference fields are present when the leads
// were compared at least once.
type ReportAVLeadDifference struct {
	VideoPID              string   `json:"video_pid,omitempty"`
	MaxAVLeadDifferenceMs *float64 `json:"max_av_lead_difference_ms,omitempty"`
	AudioLeadMs           *float64 `json:"audio_lead_ms,omitempty"`
	VideoLeadMs           *float64 `json:"video_lead_ms,omitempty"`
	AudioOffsetBytes      *int64   `json:"audio_offset_bytes,omitempty"`
	VideoOffsetBytes      *int64   `json:"video_offset_bytes,omitempty"`
}

// ReportAVLeadAudio is the A/V lead difference of one audio PID.
type ReportAVLeadAudio struct {
	PID             string `json:"pid"`
	Status          string `json:"status"`
	Reason          string `json:"reason,omitempty"`
	ComparisonCount int    `json:"comparison_count"`
	ReportAVLeadDifference
}

// ReportAVLeadProgram is the A/V lead difference of one program: its worst
// audio PID (audio_pid and the difference fields, when any was compared) and
// every audio PID.
type ReportAVLeadProgram struct {
	ProgramNumber uint16 `json:"program_number"`
	Status        string `json:"status"`
	Reason        string `json:"reason,omitempty"`
	AudioPID      string `json:"audio_pid,omitempty"`
	ReportAVLeadDifference
	Audio []ReportAVLeadAudio `json:"audio"`
}

// reportMuxLead returns the mux_lead result.
func reportMuxLead(check *analysis.PESCheck) ReportMuxLead {
	r := check.MuxLead()
	lead := ReportMuxLead{TimeBasis: pidReferenceTimeBasis, PIDs: []ReportMuxLeadPID{}, Programs: []ReportAVLeadProgram{}}
	for _, p := range r.PIDs {
		row := ReportMuxLeadPID{
			ReportPESStream: ReportPESStream{ProgramNumber: p.Program, PID: reportPID(p.PID), StreamType: fmt.Sprintf("0x%02x", p.StreamType), Label: p.Label},
			AVClass:         p.Class.String(), Status: string(p.Status), Reason: string(p.Reason),
			SampleCount: p.Samples, UnbracketedCount: p.Unbracketed, DTSSampleCount: p.DTSSamples, NegativeCount: p.Negative,
		}
		if p.Samples > 0 {
			minMs, maxMs, spread := leadMs(p.MinTicks), leadMs(p.MaxTicks), leadMs(p.MaxTicks-p.MinTicks)
			minAt, maxAt := p.MinOffset, p.MaxOffset
			row.MinLeadMs, row.MinLeadOffsetBytes, row.MaxLeadMs, row.MaxLeadOffsetBytes, row.SpreadMs = &minMs, &minAt, &maxMs, &maxAt, &spread
		}
		lead.PIDs = append(lead.PIDs, row)
	}
	for _, p := range r.Programs {
		program := ReportAVLeadProgram{ProgramNumber: p.Program, Status: string(p.Status), Reason: string(p.Reason), Audio: []ReportAVLeadAudio{}}
		if p.HasWorst {
			program.AudioPID, program.ReportAVLeadDifference = reportPID(p.Worst.AudioPID), reportAVLeadDifference(p.Worst)
		}
		for _, a := range p.Audio {
			program.Audio = append(program.Audio, ReportAVLeadAudio{PID: reportPID(a.AudioPID), Status: string(a.Status), Reason: string(a.Reason), ComparisonCount: a.Comparisons, ReportAVLeadDifference: reportAVLeadDifference(a)})
		}
		lead.Programs = append(lead.Programs, program)
	}
	return lead
}

func reportAVLeadDifference(a analysis.AVLeadDifference) ReportAVLeadDifference {
	if a.Comparisons == 0 {
		return ReportAVLeadDifference{}
	}
	diff, audio, video := leadMs(a.MaxTicks), leadMs(a.AudioLeadTicks), leadMs(a.VideoLeadTicks)
	audioAt, videoAt := a.AudioOffset, a.VideoOffset
	return ReportAVLeadDifference{VideoPID: reportPID(a.VideoPID), MaxAVLeadDifferenceMs: &diff, AudioLeadMs: &audio, VideoLeadMs: &video, AudioOffsetBytes: &audioAt, VideoOffsetBytes: &videoAt}
}

// leadMs converts 27 MHz ticks to milliseconds, rounded to the microsecond.
func leadMs(ticks int64) float64 { return roundMicro(pcrToMs(float64(ticks))) }

// dumpMuxLead prints the mux lead of every elementary PID and the A/V lead
// difference of every program with a video and an audio stream.
func dumpMuxLead(check *analysis.PESCheck) {
	r := check.MuxLead()
	fmt.Println("-----------------------------")
	fmt.Println("Mux Lead (report only: DTS, or PTS without DTS, minus PCR-interpolated arrival)")
	for _, p := range r.PIDs {
		stream := fmt.Sprintf("Program %d PID 0x%04x (%s)", p.Program, p.PID, p.Label)
		if p.Samples == 0 {
			fmt.Printf("  %s : no lead (%s)%s\n", stream, pesReasonText[p.Reason], unbracketedText(p.Unbracketed))
			continue
		}
		fmt.Printf("  %s : %.3fms to %.3fms (spread %.3fms) over %d PES, min at 0x%08x, max at 0x%08x%s\n",
			stream, leadMs(p.MinTicks), leadMs(p.MaxTicks), leadMs(p.MaxTicks-p.MinTicks), p.Samples, p.MinOffset, p.MaxOffset, unbracketedText(p.Unbracketed))
		if p.Negative > 0 {
			fmt.Printf("    %d PES multiplexed after their DTS/PTS (negative lead: an observation, not an error)\n", p.Negative)
		}
	}
	for _, p := range r.Programs {
		if !p.HasWorst {
			fmt.Printf("  Program %d A/V lead difference: not measured (%s)\n", p.Program, pesReasonText[p.Reason])
			continue
		}
		w := p.Worst
		fmt.Printf("  Program %d A/V lead difference: %.3fms, audio PID 0x%04x (lead %.3fms at 0x%08x) against video PID 0x%04x (lead %.3fms at 0x%08x)\n",
			p.Program, leadMs(w.MaxTicks), w.AudioPID, leadMs(w.AudioLeadTicks), w.AudioOffset, w.VideoPID, leadMs(w.VideoLeadTicks), w.VideoOffset)
		if len(p.Audio) > 1 {
			for _, a := range p.Audio {
				if a.Comparisons == 0 {
					fmt.Printf("    audio PID 0x%04x : not measured (%s)\n", a.AudioPID, pesReasonText[a.Reason])
					continue
				}
				fmt.Printf("    audio PID 0x%04x : %.3fms\n", a.AudioPID, leadMs(a.MaxTicks))
			}
		}
	}
}

func unbracketedText(count int) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf(", %d unbracketed", count)
}
