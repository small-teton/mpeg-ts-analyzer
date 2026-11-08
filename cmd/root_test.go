package cmd

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
	"github.com/small-teton/mpeg-ts-analyzer/v2/tsparser"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestExecuteExitStatus(t *testing.T) {
	if scenario := os.Getenv("MPEG_TS_TEST_EXECUTE"); scenario != "" {
		rootCmd.SetArgs([]string{"input.ts"})
		rootCmd.RunE = func(_ *cobra.Command, _ []string) error {
			switch scenario {
			case "compliance":
				return &tsparser.ComplianceError{Checks: []string{"PCR-PTS/DTS max gap"}}
			case "parse":
				return errors.New("parse failed")
			default:
				return nil
			}
		}
		Execute("test")
		return
	}
	for _, tt := range []struct {
		scenario string
		code     int
		message  string
	}{
		{"success", 0, ""},
		{"parse", 1, "parse failed"},
		{"compliance", 2, "compliance check failed: PCR-PTS/DTS max gap"},
	} {
		t.Run(tt.scenario, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestExecuteExitStatus$")
			command.Env = append(os.Environ(), "MPEG_TS_TEST_EXECUTE="+tt.scenario)
			stderr, err := command.Output()
			code := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatal(err)
				}
				code, stderr = exitErr.ExitCode(), exitErr.Stderr
			}
			if code != tt.code || !strings.Contains(string(stderr), tt.message) {
				t.Fatalf("exit=%d, stderr=%q; want exit=%d, message=%q", code, stderr, tt.code, tt.message)
			}
		})
	}
}

// TestFailOnExitStatus runs the real command on the bundled samples: a
// selected category with a finding exits 2, a healthy, unselected, report-only
// or listing run exits 0, and an invalid policy or an incompatible output
// exits 1.
func TestFailOnExitStatus(t *testing.T) {
	if args := os.Getenv("MPEG_TS_TEST_ARGS"); args != "" {
		rootCmd.SetArgs(strings.Split(args, "\x1f"))
		Execute("test")
		return
	}
	const (
		healthy     = "../sample_data/sample_188byte_video_mpeg2_320x240_25fps_audio_mp2_48000Hz.ts"
		findings188 = "../sample_data/sample_188byte_all_findings.ts"
		findings192 = "../sample_data/sample_192byte_all_findings.ts"
		findings204 = "../sample_data/sample_204byte_all_findings.ts"
		syncLoss    = "../sample_data/sample_204byte_sync_loss.ts"
		references  = "../sample_data/sample_192byte_pid_references.ts"
		packetGaps  = "../sample_data/sample_204byte_packet_gaps.ts"
		twoPrograms = "../sample_data/sample_188byte_two_programs_mpeg2_160x120_25fps_mp2.ts"
	)
	for _, tt := range []struct {
		args    []string
		code    int
		message string
	}{
		{[]string{findings188}, 0, ""},
		{[]string{healthy, "--fail-on", "any"}, 0, ""},
		{[]string{healthy, "--format", "json", "--fail-on", "any", "--fail-on-error"}, 0, ""},
		{[]string{findings188, "--fail-on-error"}, 2, "compliance check failed: Max PCR interval, PCR-PTS/DTS max gap"},
		{[]string{findings192, "--fail-on", "timing"}, 2, "compliance check failed: Max PCR interval, PCR-PTS/DTS max gap"},
		{[]string{findings204, "--fail-on", "continuity"}, 2, "compliance check failed: Continuity counter"},
		{[]string{findings204, "--format", "json", "--fail-on", "sync,referenced_pids,unreferenced_pids"}, 0, ""},
		{[]string{findings188, "--fail-on", "continuity,timestamp", "--fail-on-error"}, 2,
			"compliance check failed: Max PCR interval, PCR-PTS/DTS max gap, PTS/DTS anomalies, Continuity counter"},
		{[]string{findings192, "--fail-on", "any"}, 2,
			"compliance check failed: Max PCR interval, PCR-PTS/DTS max gap, PTS/DTS anomalies, PES headers, PTS repetition, Continuity counter, Transport errors, Scrambled packets, PAT/PMT health"},
		{[]string{syncLoss, "--fail-on", "sync"}, 2, "compliance check failed: TS synchronization"},
		{[]string{references, "--fail-on", "referenced_pids", "--fail-on", "unreferenced_pids"}, 2, "compliance check failed: Referenced PIDs, Unreferenced PIDs"},
		{[]string{packetGaps, "--fail-on", "timing,continuity,pes_headers,pts_repetition,referenced_pids,unreferenced_pids"}, 0, ""},
		{[]string{twoPrograms, "--fail-on", "any"}, 0, ""},
		{[]string{findings188, "--list-programs", "--fail-on", "any"}, 0, ""},
		{[]string{findings188, "--fail-on", "bogus"}, 1, `unknown --fail-on category "bogus"`},
		{[]string{findings188, "--fail-on", "timing,"}, 1, `--fail-on "timing," has an empty category`},
		{[]string{findings188, "--fail-on", "pcr_jitter"}, 1, `unknown --fail-on category "pcr_jitter"`},
		{[]string{findings188, "--format", "json", "--output", "analysis", "--fail-on", "any"}, 1, "cannot be combined with --program, --list-programs, --fail-on-error or --fail-on"},
		{[]string{findings188, "--format", "json", "--output", "packets", "--fail-on", "any"}, 1, "--fail-on judges an analysis and cannot be combined with --output packets"},
		{[]string{"--capabilities", "--fail-on", "any"}, 1, "--capabilities cannot be combined with --fail-on"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestFailOnExitStatus$")
			command.Env = append(os.Environ(), "MPEG_TS_TEST_ARGS="+strings.Join(tt.args, "\x1f"))
			var stderr strings.Builder
			command.Stderr = &stderr
			stdout, err := command.Output()
			code := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatal(err)
				}
				code = exitErr.ExitCode()
			}
			if code != tt.code || !strings.Contains(stderr.String(), tt.message) {
				t.Fatalf("exit=%d, stderr=%q; want exit=%d, message=%q", code, stderr.String(), tt.code, tt.message)
			}
			// The report is written whatever the policy decides.
			if tt.code != 1 && !strings.Contains(string(stdout), "Compliance Check Results:") && !strings.Contains(string(stdout), `"schema_version": "2.0"`) &&
				!strings.Contains(string(stdout), "Program") {
				t.Errorf("no report on stdout: %s", stdout)
			}
		})
	}
}

func TestExitCode(t *testing.T) {
	if got := exitCode(errors.New("parse failed")); got != 1 {
		t.Errorf("ordinary error exit code = %d, want 1", got)
	}
	if got := exitCode(&tsparser.ComplianceError{Checks: []string{"Max PCR interval"}}); got != 2 {
		t.Errorf("compliance error exit code = %d, want 2", got)
	}
}

func TestCapabilitiesNeedsNoInput(t *testing.T) {
	defer func() { opt = options.Options{} }()
	var out strings.Builder
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"--capabilities"})
	defer rootCmd.SetOut(nil)
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"capabilities_version": "1.0"`) {
		t.Errorf("unexpected output: %s", out.String())
	}

	rootCmd.SetArgs([]string{"--capabilities", "input.ts"})
	if err := rootCmd.Execute(); err == nil {
		t.Error("expected an error when an input file is given with --capabilities")
	}
}

// resetFlags undoes what an Execute call parsed into the shared root command.
func resetFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		rootCmd.Flags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
		opt = options.Options{}
	})
}

func TestRejectsInvalidOptionsBeforeRunning(t *testing.T) {
	for _, tt := range []struct {
		args    []string
		message string
	}{
		{[]string{"--capabilities", "--progress", "xml"}, "--capabilities cannot be combined with --progress"},
		{[]string{"--capabilities", "--format", "xml"}, "--capabilities cannot be combined with --format"},
		{[]string{"--capabilities", "--format", "json", "--offset", "188"}, "--capabilities cannot be combined with --format, --offset"},
		{[]string{"input.ts", "--progress", "xml"}, `unsupported progress format "xml"`},
		{[]string{"input.ts", "--offset", "-1"}, "--offset must not be negative"},
		{[]string{"input.ts", "--limit", "-9223372036854775808"}, "--limit must not be negative"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			resetFlags(t)
			rootCmd.SetArgs(tt.args)
			defer func(run func(*cobra.Command, []string) error) { rootCmd.RunE = run }(rootCmd.RunE)
			rootCmd.RunE = func(*cobra.Command, []string) error {
				t.Fatal("the command ran despite invalid options")
				return nil
			}
			err := rootCmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Errorf("err = %v, want %q", err, tt.message)
			}
		})
	}
}
