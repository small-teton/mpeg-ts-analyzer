package tsparser

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// The fixtures are the single-program sample with a PMT version change and a
// PAT version change moving the PMT (tools/generate_psi_version_change).
var psiVersionChangeFixtures = []struct {
	path string
	m2ts bool
}{
	{"../sample_data/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts", false},
	{"../sample_data/sample_192byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts", true},
}

func TestPsiVersionChangeFixtures(t *testing.T) {
	for _, fixture := range psiVersionChangeFixtures {
		packetSize := int64(188)
		if fixture.m2ts {
			packetSize = 192
		}
		report := summary(t, fixture.path, options.Options{DumpBitrate: true, DumpPcrJitter: true})
		// TSDuck reports the PMT version 1 in packet 545 and the PAT version 1
		// in packet 1131 (tsp -P tables --packet-index).
		if len(report.PSITransitions) != 2 || report.Status != "complete" || len(report.Warnings) != 0 {
			t.Fatalf("%s: %+v", fixture.path, report)
		}
		pmt, pat := report.PSITransitions[0], report.PSITransitions[1]
		if pmt.OffsetBytes != 545*packetSize || pmt.PCRPID != "0x0200" || len(pmt.ElementaryStreamsAdded) != 1 || len(pmt.ElementaryStreamsRemoved) != 1 || len(pmt.StreamTypeChanges) != 1 {
			t.Errorf("%s PMT transition: %s", fixture.path, toJSON(t, pmt))
		}
		if pat.OffsetBytes != 1131*packetSize || len(pat.PMTPIDChanges) != 1 || pat.PMTPIDChanges[0].PMTPID != "0x1100" {
			t.Errorf("%s PAT transition: %s", fixture.path, toJSON(t, pat))
		}
		if report.ContinuityCounter.Status != "ok" || report.PCRJitter.SegmentCount != 2 || report.Bitrate.SegmentCount != 2 || report.Compliance.MaxPCRInterval.Status != "ok" {
			t.Errorf("%s analysis: %+v %+v %+v", fixture.path, report.ContinuityCounter, report.PCRJitter, report.Bitrate)
		}
		analysis, err := decodeRangeAnalysis(t, fixture.path, options.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if toJSON(t, analysis.PSITransitions) != toJSON(t, report.PSITransitions) {
			t.Errorf("%s: analysis transitions differ", fixture.path)
		}
	}
}

func TestPsiVersionChangeFixtureGenerator(t *testing.T) {
	const input = "../sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts"
	for _, fixture := range psiVersionChangeFixtures {
		path := filepath.Join(t.TempDir(), "fixture.ts")
		args := []string{"run", "../tools/generate_psi_version_change", "-input", input, "-output", path}
		if fixture.m2ts {
			args = append(args, "-m2ts")
		}
		if output, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("generate fixture: %v\n%s", err, output)
		}
		generated, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		committed, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, committed) {
			t.Errorf("%s does not match the generator output", fixture.path)
		}
	}
}
