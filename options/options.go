package options

// Options represents command-line flags.
type Options struct {
	DumpHeader          bool
	DumpPayload         bool
	DumpAdaptationField bool
	DumpPsi             bool
	DumpPesHeader       bool
	DumpTimestamp       bool
	DumpPcrJitter       bool
	DumpBitrate         bool
	FailOnError         bool
	ListPrograms        bool
	Program             int
	Offset              int64
	Limit               int64
}
