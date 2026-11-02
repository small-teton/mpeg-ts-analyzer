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
