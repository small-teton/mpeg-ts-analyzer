package options

import "testing"

func TestDumpHeader(t *testing.T) {
	options := new(Options)

	options.dumpHeader = true
	retVal := options.DumpHeader()
	if retVal != true {
		t.Errorf("actual: true, But got %t", retVal)
	}

	options.dumpHeader = false
	retVal = options.DumpHeader()
	if retVal != false {
		t.Errorf("actual: false, But got %t", retVal)
	}
}

func TestDumpPayload(t *testing.T) {
	options := new(Options)

	options.dumpPayload = true
	retVal := options.DumpPayload()
	if retVal != true {
		t.Errorf("actual: true, But got %t", retVal)
	}

	options.dumpPayload = false
	retVal = options.DumpPayload()
	if retVal != false {
		t.Errorf("actual: false, But got %t", retVal)
	}
}

func TestDumpAdaptationField(t *testing.T) {
	options := new(Options)

	options.dumpAdaptationField = true
	retVal := options.DumpAdaptationField()
	if retVal != true {
		t.Errorf("actual: true, But got %t", retVal)
	}

	options.dumpAdaptationField = false
	retVal = options.DumpAdaptationField()
	if retVal != false {
		t.Errorf("actual: false, But got %t", retVal)
	}
}

func TestDumpPsi(t *testing.T) {
	options := new(Options)

	options.dumpPsi = true
	retVal := options.DumpPsi()
	if retVal != true {
		t.Errorf("actual: true, But got %t", retVal)
	}

	options.dumpPsi = false
	retVal = options.DumpPsi()
	if retVal != false {
		t.Errorf("actual: false, But got %t", retVal)
	}
}

func TestDumpPesHeader(t *testing.T) {
	options := new(Options)

	options.dumpPesHeader = true
	retVal := options.DumpPesHeader()
	if retVal != true {
		t.Errorf("actual: true, But got %t", retVal)
	}

	options.dumpPesHeader = false
	retVal = options.DumpPesHeader()
	if retVal != false {
		t.Errorf("actual: false, But got %t", retVal)
	}
}

func TestNotDumpTimestamp(t *testing.T) {
	options := new(Options)

	options.dumpTimestamp = true
	retVal := options.DumpTimestamp()
	if retVal != true {
		t.Errorf("actual: true, But got %t", retVal)
	}

	options.dumpTimestamp = false
	retVal = options.DumpTimestamp()
	if retVal != false {
		t.Errorf("actual: false, But got %t", retVal)
	}
}

func TestSetDumpHeader(t *testing.T) {
	options := new(Options)

	options.SetDumpHeader(true)
	retVal := options.dumpHeader
	if retVal != true {
		t.Errorf("actual: true, But got %t", retVal)
	}

	options.SetDumpHeader(false)
	retVal = options.dumpHeader
	if retVal != false {
		t.Errorf("actual: false, But got %t", retVal)
	}
}

func TestSetDumpPayload(t *testing.T) {
	options := new(Options)

	options.SetDumpPayload(true)
	retVal := options.dumpPayload
	if retVal != true {
		t.Errorf("actual: true, But got %t", retVal)
	}

	options.SetDumpPayload(false)
	retVal = options.dumpPayload
	if retVal != false {
		t.Errorf("actual: false, But got %t", retVal)
	}
}

func TestSetDumpAdaptationField(t *testing.T) {
	options := new(Options)

	options.SetDumpAdaptationField(true)
	retVal := options.dumpAdaptationField
	if retVal != true {
		t.Errorf("actual: true, But got %t", retVal)
	}

	options.SetDumpAdaptationField(false)
	retVal = options.dumpAdaptationField
	if retVal != false {
		t.Errorf("actual: false, But got %t", retVal)
	}
}

func TestSetDumpPsi(t *testing.T) {
	options := new(Options)

	options.SetDumpPsi(true)
	retVal := options.dumpPsi
	if retVal != true {
		t.Errorf("actual: true, But got %t", retVal)
	}

	options.SetDumpPsi(false)
	retVal = options.dumpPsi
	if retVal != false {
		t.Errorf("actual: false, But got %t", retVal)
	}
}

func TestSetDumpPesHeader(t *testing.T) {
	options := new(Options)

	options.SetDumpPesHeader(true)
	retVal := options.dumpPesHeader
	if retVal != true {
		t.Errorf("actual: true, But got %t", retVal)
	}

	options.SetDumpPesHeader(false)
	retVal = options.dumpPesHeader
	if retVal != false {
		t.Errorf("actual: false, But got %t", retVal)
	}
}

func TestDumpTimestamp(t *testing.T) {
	options := new(Options)

	options.SetDumpTimestamp(true)
	retVal := options.dumpTimestamp
	if retVal != true {
		t.Errorf("actual: true, But got %t", retVal)
	}

	options.SetDumpTimestamp(false)
	retVal = options.dumpTimestamp
	if retVal != false {
		t.Errorf("actual: false, But got %t", retVal)
	}
}
