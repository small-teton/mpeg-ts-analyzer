package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefinedFlags(t *testing.T) {
	source := `rootCmd.Flags().BoolVar(&opt.Dump, "dump", false, "Dump.")
rootCmd.Flags().StringVar(&opt.Format, "format", "text", "Format.")`
	got := strings.Join(definedFlags(source), ",")
	if got != "dump,format" {
		t.Fatalf("definedFlags() = %q", got)
	}
}

func TestEvaluateRequiresCLIDocumentation(t *testing.T) {
	root := policyFixture(t)
	changed := map[string]bool{"cmd/root.go": true}
	patches := map[string]string{"cmd/root.go": "+rootCmd.Flags().BoolVar(&opt.New, \"new\", false, \"New.\")\n"}
	body := completeBody("No documentation files were updated.")

	result := evaluate(root, changed, patches, body)
	joined := strings.Join(result.failures, "\n")
	if !strings.Contains(joined, "`README.md` was not updated") || !strings.Contains(joined, "`OPTIONS.md` was not updated") {
		t.Fatalf("missing documentation failures: %s", joined)
	}
}

func TestEvaluateAcceptsExplicitUnaffectedDocumentation(t *testing.T) {
	root := policyFixture(t)
	changed := map[string]bool{"cmd/root.go": true}
	patches := map[string]string{"cmd/root.go": "+rootCmd.Flags().BoolVar(&opt.New, \"new\", false, \"New.\")\n"}
	body := completeBody("`README.md` is not affected. `OPTIONS.md` does not need an update.")

	result := evaluate(root, changed, patches, body)
	if len(result.failures) != 0 {
		t.Fatalf("unexpected failures: %v", result.failures)
	}
}

func TestEvaluateWarnsForPossibleOutputChange(t *testing.T) {
	root := policyFixture(t)
	changed := map[string]bool{"tsparser/stream.go": true}
	patches := map[string]string{"tsparser/stream.go": "+fmt.Println(\"changed\")\n"}

	result := evaluate(root, changed, patches, completeBody("No interface changes."))
	if len(result.failures) != 0 || len(result.warnings) != 1 {
		t.Fatalf("failures=%v warnings=%v", result.failures, result.warnings)
	}
}

func TestEvaluateFindsMissingTemplateSectionAndLocalLink(t *testing.T) {
	root := policyFixture(t)
	writeFile(t, root, "BROKEN.md", "[missing](not-there.md)\n")

	result := evaluate(root, map[string]bool{}, map[string]string{}, "## Why\n\nReason")
	joined := strings.Join(result.failures, "\n")
	if !strings.Contains(joined, "missing the `## Documentation`") || !strings.Contains(joined, "missing local path") {
		t.Fatalf("unexpected failures: %s", joined)
	}
}

func policyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "cmd/root.go", `rootCmd.Flags().BoolVar(&opt.Dump, "dump", false, "Dump.")`)
	writeFile(t, root, "OPTIONS.md", "Use `--dump`.\n")
	writeFile(t, root, "README.md", "# Test\n")
	return root
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func completeBody(documentation string) string {
	return "## Why\n\nReason\n\n## What changed\n\nChange\n\n## Compatibility\n\nCompatible\n\n## Documentation\n\n" + documentation + "\n\n## Validation\n\nTested\n"
}
