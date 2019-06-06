package options

// Options commad line flags
type Options struct {
	dumpHeader          bool
	dumpPayload         bool
	dumpAdaptationField bool
	dumpPsi             bool
	dumpPesHeader       bool
	dumpTimestamp    	bool
}

// DumpHeader return flag data "--dump-ts-header"
func (o *Options) DumpHeader() bool { return o.dumpHeader }

// DumpPayload return flag data "--dump-ts-payload"
func (o *Options) DumpPayload() bool { return o.dumpPayload }

// DumpAdaptationField return flag data "--dump-adaptation-field"
func (o *Options) DumpAdaptationField() bool { return o.dumpAdaptationField }

// DumpPsi return flag data "--dump-psi"
func (o *Options) DumpPsi() bool { return o.dumpPsi }

// DumpPesHeader return flag data "--dump-pes-header"
func (o *Options) DumpPesHeader() bool { return o.dumpPesHeader }

// NotDumpTimestamp return flag data "--not-dump-timestamp"
func (o *Options) DumpTimestamp() bool { return o.dumpTimestamp }

// SetDumpHeader set value to "--dump-ts-header"
func (o *Options) SetDumpHeader(v bool) { o.dumpHeader = v }

// SetDumpPayload set value to "--dump-ts-payload"
func (o *Options) SetDumpPayload(v bool) { o.dumpPayload = v }

// SetDumpAdaptationField set value to "--dump-adaptation-field"
func (o *Options) SetDumpAdaptationField(v bool) { o.dumpAdaptationField = v }

// SetDumpPsi set value to "--dump-psi"
func (o *Options) SetDumpPsi(v bool) { o.dumpPsi = v }

// SetDumpPesHeader set value to "--dump-pes-header"
func (o *Options) SetDumpPesHeader(v bool) { o.dumpPesHeader = v }

// SetDumpTimestamp set value to "--dump-timestamp"
func (o *Options) SetDumpTimestamp(v bool) { o.dumpTimestamp = v }
