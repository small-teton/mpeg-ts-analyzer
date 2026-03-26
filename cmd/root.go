package cmd

import (
	"fmt"
	"os"

	"github.com/small-teton/mpeg-ts-analyzer/options"
	"github.com/small-teton/mpeg-ts-analyzer/tsparser"
	"github.com/spf13/cobra"
)

var version string

var opt options.Options

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:     "mpeg-ts-analyzer [input file path]",
	Args:    cobra.ExactArgs(1),
	Short:   "An analyzer for MPEG-2 Transport Stream (ISO/IEC 13818-1)",
	Long:    "It can parse TS header, Adaptation Field, PSI (PAT/PMT) and PES header. It also validates continuity_counter (TS header) and CRC32 (PSI).",
	RunE: func(cmd *cobra.Command, args []string) error {
		return tsparser.ParseTsFile(args[0], opt)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	rootCmd.Version = version
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().BoolVar(&opt.DumpHeader, "dump-ts-header", false, "Dump TS packet header.")
	rootCmd.Flags().BoolVar(&opt.DumpPayload, "dump-ts-payload", false, "Dump TS packet payload binary.")
	rootCmd.Flags().BoolVar(&opt.DumpAdaptationField, "dump-adaptation-field", false, "Dump TS packet adaptation_field detail.")
	rootCmd.Flags().BoolVar(&opt.DumpPsi, "dump-psi", false, "Dump PSI (PAT/PMT) detail.")
	rootCmd.Flags().BoolVar(&opt.DumpPesHeader, "dump-pes-header", false, "Dump PES packet header detail.")
	rootCmd.Flags().BoolVar(&opt.DumpTimestamp, "dump-timestamp", false, "Dump PCR/PTS/DTS timestamps.")
}
