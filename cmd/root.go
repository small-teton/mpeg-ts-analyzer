package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
	"github.com/small-teton/mpeg-ts-analyzer/v2/tsparser"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var opt options.Options

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use: "mpeg-ts-analyzer [input file path]",
	Args: func(cmd *cobra.Command, args []string) error {
		if opt.Capabilities {
			return cobra.NoArgs(cmd, args)
		}
		return cobra.ExactArgs(1)(cmd, args)
	},
	Short: "An analyzer for MPEG-2 Transport Stream (ISO/IEC 13818-1)",
	Long:  "It can parse TS header, Adaptation Field, PSI (PAT/PMT) and PES header. It also validates continuity_counter (TS header) and CRC32 (PSI).",
	PreRunE: func(cmd *cobra.Command, _ []string) error {
		if opt.Capabilities {
			var others []string
			cmd.Flags().Visit(func(f *pflag.Flag) {
				if f.Name != "capabilities" {
					others = append(others, "--"+f.Name)
				}
			})
			if len(others) > 0 {
				return fmt.Errorf("--capabilities cannot be combined with %s", strings.Join(others, ", "))
			}
		}
		return tsparser.ValidateOptions(opt)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if opt.Capabilities {
			return tsparser.WriteCapabilities(cmd.OutOrStdout(), cmd.Root().Version)
		}
		return tsparser.ParseTsFile(args[0], opt)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute(version string) {
	rootCmd.Version = version
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	var complianceErr *tsparser.ComplianceError
	if errors.As(err, &complianceErr) {
		return 2
	}
	return 1
}

func init() {
	rootCmd.Flags().StringVar(&opt.Format, "format", "text", "Output format: text or json.")
	rootCmd.Flags().BoolVar(&opt.DumpHeader, "dump-ts-header", false, "Dump TS packet header.")
	rootCmd.Flags().BoolVar(&opt.DumpPayload, "dump-ts-payload", false, "Dump TS packet payload binary.")
	rootCmd.Flags().BoolVar(&opt.DumpAdaptationField, "dump-adaptation-field", false, "Dump TS packet adaptation_field detail.")
	rootCmd.Flags().BoolVar(&opt.DumpPsi, "dump-psi", false, "Dump PSI (PAT/PMT) detail.")
	rootCmd.Flags().BoolVar(&opt.DumpPesHeader, "dump-pes-header", false, "Dump PES packet header detail.")
	rootCmd.Flags().BoolVar(&opt.DumpTimestamp, "dump-timestamp", false, "Dump PCR/PTS/DTS timestamps.")
	rootCmd.Flags().BoolVar(&opt.DumpPcrJitter, "dump-pcr-jitter", false, "Analyze PCR jitter (per-interval deviation from the expected PCR).")
	rootCmd.Flags().BoolVar(&opt.DumpBitrate, "dump-bitrate", false, "Summarize per-PID average/peak bitrate (PCR time base) for the analyzed program.")
	rootCmd.Flags().BoolVar(&opt.FailOnError, "fail-on-error", false, "Exit with code 2 when a timing compliance check reports NG.")
	rootCmd.Flags().BoolVar(&opt.ListPrograms, "list-programs", false, "List every program (program_number, PMT PID, elementary streams) and exit.")
	rootCmd.Flags().IntVar(&opt.Program, "program", 0, "Analyze only this program_number (default: the sole program; a multi-program stream is listed instead).")
	rootCmd.Flags().Int64Var(&opt.Offset, "offset", 0, "Start reading from this byte offset.")
	rootCmd.Flags().Int64Var(&opt.Limit, "limit", 0, "Stop reading after this many bytes (0 = no limit).")
	rootCmd.Flags().StringVar(&opt.Progress, "progress", "", "Write progress events to stderr in this format: json.")
	rootCmd.Flags().BoolVar(&opt.Capabilities, "capabilities", false, "Print the supported machine-readable outputs as JSON and exit (no input file).")
}
