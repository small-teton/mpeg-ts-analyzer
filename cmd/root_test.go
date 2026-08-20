package cmd

import (
	"errors"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/tsparser"
)

func TestExitCode(t *testing.T) {
	if got := exitCode(errors.New("parse failed")); got != 1 {
		t.Errorf("ordinary error exit code = %d, want 1", got)
	}
	if got := exitCode(&tsparser.ComplianceError{Checks: []string{"Max PCR interval"}}); got != 2 {
		t.Errorf("compliance error exit code = %d, want 2", got)
	}
}
