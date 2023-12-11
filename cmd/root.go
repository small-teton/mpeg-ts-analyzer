package cmd

import (
	"fmt"
	"os"

	"github.com/cockroachdb/errors"
	"github.com/small-teton/mpeg-ts-analyzer/options"
	"github.com/small-teton/mpeg-ts-analyzer/tsparser"
	"github.com/spf13/cobra"
)

var version string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "./mpeg-ts-analyzer [input file path]",
	Args:  cobra.MaximumNArgs(1),
	Short: "MpegTsAnalyzer is the Analyzer of MPEG2 Transport Stream(ISO_IEC_13818-1)",
	Long:  `It can parse TS header, Adaptation Field, PSI(PAT/PMT) and PES header. Then, it can check continuity_counter(TS header), CRC32(PSI).`,
	Run: func(cmd *cobra.Command, args []string) {
		if versionFlag, _ := cmd.Flags().GetBool("version"); versionFlag {
			fmt.Printf("mpeg-ts-analyzer version %s\n", version)
			return
		}

		if len(args) == 0 {
			fmt.Println("input file path is not specified.")
			return
		}

		opt, _ := loadFlags(cmd)
		tsparser.ParseTsFile(args[0], opt)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().Bool("dump-ts-header", false, "Dump TS packet header.")
	rootCmd.Flags().Bool("dump-ts-payload", false, "Dump TS packet payload binary.")
	rootCmd.Flags().Bool("dump-adaptation-field", false, "Dump TS packet adaptation_field detail.")
	rootCmd.Flags().Bool("dump-psi", false, "Dump PSI(PAT/PMT) detail.")
	rootCmd.Flags().Bool("dump-pes-header", false, "Dump PES packet header detail.")
	rootCmd.Flags().Bool("dump-timestamp", false, "Dump PCR/PTS/DTS timestamps.")
	rootCmd.Flags().Bool("version", false, "show mpeg-ts-analyzer version.")
}

func loadFlags(cmd *cobra.Command) (options.Options, error) {
	var opt options.Options
	dumpHeader, err := cmd.Flags().GetBool("dump-ts-header")
	if err != nil {
		return opt, errors.Wrap(err, "failed flag parse --dump-ts-header")
	}
	dumpPayload, _ := cmd.Flags().GetBool("dump-ts-payload")
	if err != nil {
		return opt, errors.Wrap(err, "failed flag parse --dump-ts-payload")
	}
	dumpAdaptationField, _ := cmd.Flags().GetBool("dump-adaptation-field")
	if err != nil {
		return opt, errors.Wrap(err, "failed flag parse --dump-adaptation-field")
	}
	dumpPsi, _ := cmd.Flags().GetBool("dump-psi")
	if err != nil {
		return opt, errors.Wrap(err, "failed flag parse --dump-psi")
	}
	dumpPesHeader, _ := cmd.Flags().GetBool("dump-pes-header")
	if err != nil {
		return opt, errors.Wrap(err, "failed flag parse --dump-pes-header")
	}
	dumpTimestamp, _ := cmd.Flags().GetBool("dump-timestamp")
	if err != nil {
		return opt, errors.Wrap(err, "failed flag parse --dump-timestamp")
	}
	opt.SetDumpHeader(dumpHeader)
	opt.SetDumpPayload(dumpPayload)
	opt.SetDumpAdaptationField(dumpAdaptationField)
	opt.SetDumpPsi(dumpPsi)
	opt.SetDumpPesHeader(dumpPesHeader)
	opt.SetDumpTimestamp(dumpTimestamp)

	return opt, nil
}
