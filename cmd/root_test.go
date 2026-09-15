package cmd

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/tsparser"
	"github.com/spf13/cobra"
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

func TestExitCode(t *testing.T) {
	if got := exitCode(errors.New("parse failed")); got != 1 {
		t.Errorf("ordinary error exit code = %d, want 1", got)
	}
	if got := exitCode(&tsparser.ComplianceError{Checks: []string{"Max PCR interval"}}); got != 2 {
		t.Errorf("compliance error exit code = %d, want 2", got)
	}
}
