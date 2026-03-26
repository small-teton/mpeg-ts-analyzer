package options

// Options represents command-line flags.
type Options struct {
	DumpHeader          bool
	DumpPayload         bool
	DumpAdaptationField bool
	DumpPsi             bool
	DumpPesHeader       bool
	DumpTimestamp       bool
}
