package options

import "testing"

func TestOptions(t *testing.T) {
	var opt Options

	opt.DumpHeader = true
	if !opt.DumpHeader {
		t.Errorf("DumpHeader: expected true, got false")
	}

	opt.DumpPayload = true
	if !opt.DumpPayload {
		t.Errorf("DumpPayload: expected true, got false")
	}

	opt.DumpAdaptationField = true
	if !opt.DumpAdaptationField {
		t.Errorf("DumpAdaptationField: expected true, got false")
	}

	opt.DumpPsi = true
	if !opt.DumpPsi {
		t.Errorf("DumpPsi: expected true, got false")
	}

	opt.DumpPesHeader = true
	if !opt.DumpPesHeader {
		t.Errorf("DumpPesHeader: expected true, got false")
	}

	opt.DumpTimestamp = true
	if !opt.DumpTimestamp {
		t.Errorf("DumpTimestamp: expected true, got false")
	}

	opt.DumpPcrJitter = true
	if !opt.DumpPcrJitter {
		t.Errorf("DumpPcrJitter: expected true, got false")
	}

	opt.ListPrograms = true
	if !opt.ListPrograms {
		t.Errorf("ListPrograms: expected true, got false")
	}

	opt.Program = 3
	if opt.Program != 3 {
		t.Errorf("Program: expected 3, got %d", opt.Program)
	}
}
