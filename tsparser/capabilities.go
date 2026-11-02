package tsparser

import (
	"encoding/json"
	"io"
)

// capabilitiesVersion versions the capabilities document itself.
const capabilitiesVersion = "1.0"

// Capabilities describes what this CLI can produce, so callers such as the
// desktop GUI can reject an incompatible CLI before running an analysis.
type Capabilities struct {
	CapabilitiesVersion string             `json:"capabilities_version"`
	CLIVersion          string             `json:"cli_version"`
	Outputs             []CapabilityOutput `json:"outputs"`
	ProgressFormats     []string           `json:"progress_formats"`
}

// CapabilityOutput is one machine-readable output and its schema version.
type CapabilityOutput struct {
	Name          string `json:"name"`
	Format        string `json:"format"`
	SchemaVersion string `json:"schema_version"`
}

// WriteCapabilities writes the capabilities document as JSON.
func WriteCapabilities(w io.Writer, cliVersion string) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(Capabilities{
		CapabilitiesVersion: capabilitiesVersion,
		CLIVersion:          cliVersion,
		Outputs:             []CapabilityOutput{{Name: "summary", Format: "json", SchemaVersion: jsonSchemaVersion}},
		ProgressFormats:     []string{"json"},
	})
}
