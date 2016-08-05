package options

// Options commad line flags
type Options struct {
	dumpHeader          bool
	dumpPayload         bool
	dumpAdaptationField bool
	dumpPsi             bool
	notDumpTimestamp    bool
}

// DumpHeader return flag data "--dump-ts-header"
func (o *Options) DumpHeader() bool { return o.dumpHeader }

// DumpPayload return flag data "--dump-ts-payload"
func (o *Options) DumpPayload() bool { return o.dumpPayload }

// DumpAdaptationField return flag data "--dump-adaptation-field"
func (o *Options) DumpAdaptationField() bool { return o.dumpAdaptationField }

// DumpPsi return flag data "--dump-psi"
func (o *Options) DumpPsi() bool { return o.dumpPsi }

// NotDumpTimestamp return flag data "--not-dump-timestamp"
func (o *Options) NotDumpTimestamp() bool { return o.notDumpTimestamp }

// SetDumpHeader set value to "--dump-ts-header"
func (o *Options) SetDumpHeader(v bool) { o.dumpHeader = v }

// SetDumpPayload set value to "--dump-ts-payload"
func (o *Options) SetDumpPayload(v bool) { o.dumpPayload = v }

// SetDumpAdaptationField set value to "--dump-adaptation-field"
func (o *Options) SetDumpAdaptationField(v bool) { o.dumpAdaptationField = v }

// SetDumpPsi set value to "--dump-psi"
func (o *Options) SetDumpPsi(v bool) { o.dumpPsi = v }

// SetNotDumpTimestamp set value to "--not-dump-timestamp"
func (o *Options) SetNotDumpTimestamp(v bool) { o.notDumpTimestamp = v }
