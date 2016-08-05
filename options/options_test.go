package options

import "testing"

func TestDumpHeader(t *testing.T) {
	options := new(Options)

	options.dumpHeader = true
	retVal := options.DumpHeader()
	if retVal != true {
		t.Errorf("actual: true, But got %d", retVal)
	}

	options.dumpHeader = false
	retVal = options.DumpHeader()
	if retVal != false {
		t.Errorf("actual: false, But got %d", retVal)
	}
}

func TestDumpPayload(t *testing.T) {
	options := new(Options)

	options.dumpPayload = true
	retVal := options.DumpPayload()
	if retVal != true {
		t.Errorf("actual: true, But got %d", retVal)
	}

	options.dumpPayload = false
	retVal = options.DumpPayload()
	if retVal != false {
		t.Errorf("actual: false, But got %d", retVal)
	}
}

func TestDumpAdaptationField(t *testing.T) {
	options := new(Options)

	options.dumpAdaptationField = true
	retVal := options.DumpAdaptationField()
	if retVal != true {
		t.Errorf("actual: true, But got %d", retVal)
	}

	options.dumpAdaptationField = false
	retVal = options.DumpAdaptationField()
	if retVal != false {
		t.Errorf("actual: false, But got %d", retVal)
	}
}

func TestDumpPsi(t *testing.T) {
	options := new(Options)

	options.dumpPsi = true
	retVal := options.DumpPsi()
	if retVal != true {
		t.Errorf("actual: true, But got %d", retVal)
	}

	options.dumpPsi = false
	retVal = options.DumpPsi()
	if retVal != false {
		t.Errorf("actual: false, But got %d", retVal)
	}
}

func TestNotDumpTimestamp(t *testing.T) {
	options := new(Options)

	options.notDumpTimestamp = true
	retVal := options.NotDumpTimestamp()
	if retVal != true {
		t.Errorf("actual: true, But got %d", retVal)
	}

	options.notDumpTimestamp = false
	retVal = options.NotDumpTimestamp()
	if retVal != false {
		t.Errorf("actual: false, But got %d", retVal)
	}
}

func TestSetDumpHeader(t *testing.T) {
	options := new(Options)

	options.SetDumpHeader(true)
	retVal := options.dumpHeader
	if retVal != true {
		t.Errorf("actual: true, But got %d", retVal)
	}

	options.SetDumpHeader(false)
	retVal = options.dumpHeader
	if retVal != false {
		t.Errorf("actual: false, But got %d", retVal)
	}
}

func TestSetDumpPayload(t *testing.T) {
	options := new(Options)

	options.SetDumpPayload(true)
	retVal := options.dumpPayload
	if retVal != true {
		t.Errorf("actual: true, But got %d", retVal)
	}

	options.SetDumpPayload(false)
	retVal = options.dumpPayload
	if retVal != false {
		t.Errorf("actual: false, But got %d", retVal)
	}
}

func TestSetDumpAdaptationField(t *testing.T) {
	options := new(Options)

	options.SetDumpAdaptationField(true)
	retVal := options.dumpAdaptationField
	if retVal != true {
		t.Errorf("actual: true, But got %d", retVal)
	}

	options.SetDumpAdaptationField(false)
	retVal = options.dumpAdaptationField
	if retVal != false {
		t.Errorf("actual: false, But got %d", retVal)
	}
}

func TestSetDumpPsi(t *testing.T) {
	options := new(Options)

	options.SetDumpPsi(true)
	retVal := options.dumpPsi
	if retVal != true {
		t.Errorf("actual: true, But got %d", retVal)
	}

	options.SetDumpPsi(false)
	retVal = options.dumpPsi
	if retVal != false {
		t.Errorf("actual: false, But got %d", retVal)
	}
}

func TestnotDumpTimestamp(t *testing.T) {
	options := new(Options)

	options.SetNotDumpTimestamp(true)
	retVal := options.notDumpTimestamp
	if retVal != true {
		t.Errorf("actual: true, But got %d", retVal)
	}

	options.SetNotDumpTimestamp(false)
	retVal = options.notDumpTimestamp
	if retVal != false {
		t.Errorf("actual: false, But got %d", retVal)
	}
}
