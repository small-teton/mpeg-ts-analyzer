package options

type Options struct {
	dumpHeader          bool
	dumpPayload         bool
	dumpAdaptationField bool
	dumpPsi             bool
	notDumpTimestamp    bool
}

func (o *Options) DumpHeader() bool          { return o.dumpHeader }
func (o *Options) DumpPayload() bool         { return o.dumpPayload }
func (o *Options) DumpAdaptationField() bool { return o.dumpAdaptationField }
func (o *Options) DumpPsi() bool             { return o.dumpPsi }
func (o *Options) NotDumpTimestamp() bool    { return o.notDumpTimestamp }

func (o *Options) SetDumpHeader(v bool)          { o.dumpHeader = v }
func (o *Options) SetDumpPayload(v bool)         { o.dumpPayload = v }
func (o *Options) SetDumpAdaptationField(v bool) { o.dumpAdaptationField = v }
func (o *Options) SetDumpPsi(v bool)             { o.dumpPsi = v }
func (o *Options) SetNotDumpTimestamp(v bool)    { o.notDumpTimestamp = v }
