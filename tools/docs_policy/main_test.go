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

func TestDocumentedFlagRequiresExactName(t *testing.T) {
	if documentedFlag("Use `--dump-ts-header`.", "dump") {
		t.Fatal("--dump-ts-header must not document --dump")
	}
	if !documentedFlag("Use `--dump` or `--format=json`.", "dump") {
		t.Fatal("expected exact --dump match")
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

func TestEvaluateRequiresDocumentationForOptionsChange(t *testing.T) {
	root := policyFixture(t)
	changed := map[string]bool{"options/options.go": true}
	patches := map[string]string{"options/options.go": "+\tNewOption bool\n"}

	result := evaluate(root, changed, patches, completeBody("No documentation files were updated."))
	joined := strings.Join(result.failures, "\n")
	if !strings.Contains(joined, "public CLI interface") {
		t.Fatalf("missing CLI documentation failure: %s", joined)
	}
}

func TestEvaluateAcceptsDocumentedJSONContractChange(t *testing.T) {
	root := policyFixture(t)
	writeFile(t, root, "docs/json-schema-v1.json", "{}\n")
	trackFiles(t, root)
	changed := map[string]bool{
		"tsparser/report.go":       true,
		"README.md":                true,
		"OPTIONS.md":               true,
		"docs/json-schema-v1.json": true,
	}
	patches := map[string]string{"tsparser/report.go": "+\tValue string `json:\"value\"`\n"}
	body := completeBody("- Updated `README.md`.\n- Updated `OPTIONS.md`.\n- Updated `docs/json-schema-v1.json`.")

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
	trackFiles(t, root)

	result := evaluate(root, map[string]bool{}, map[string]string{}, "## Why\n\nReason")
	joined := strings.Join(result.failures, "\n")
	if !strings.Contains(joined, "missing the `## Documentation`") || !strings.Contains(joined, "missing local path") {
		t.Fatalf("unexpected failures: %s", joined)
	}
}

func TestDocumentedAsUnaffectedUsesExactFileAndLine(t *testing.T) {
	tests := []struct {
		name    string
		section string
		file    string
		want    bool
	}{
		{"same line", "- `README.md` is unchanged.", "README.md", true},
		{"nested path", "- `docs/demo/README.md` is unchanged.", "README.md", false},
		{"neighbor line", "- `OPTIONS.md` updated.\n- Schema is unchanged.", "OPTIONS.md", false},
		{"neighbor file", "- `OPTIONS.md` updated; `README.md` is unchanged.", "OPTIONS.md", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := documentedAsUnaffected(test.section, test.file); got != test.want {
				t.Fatalf("documentedAsUnaffected() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSectionBodyHandlesCRLFAndHeadingLevels(t *testing.T) {
	body := "## Documentation\r\n\r\nFirst\r\n### Detail\r\nSecond\r\n## Validation\r\nDone\r\n"
	if got := sectionBody(body, "Documentation"); got != "First\n### Detail\nSecond" {
		t.Fatalf("sectionBody() = %q", got)
	}
	if hasHeading(body, "Missing") {
		t.Fatal("unexpected missing heading")
	}
}

func TestPullRequestBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(path, []byte(`{"pull_request":{"body":"description"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	body, err := pullRequestBody(path)
	if err != nil || body != "description" {
		t.Fatalf("pullRequestBody() = %q, %v", body, err)
	}
	if _, err := pullRequestBody(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected missing-event error")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pullRequestBody(bad); err == nil {
		t.Fatal("expected malformed-event error")
	}
}

func TestLoadPRBodySkip(t *testing.T) {
	body, check, err := loadPRBody(true, filepath.Join(t.TempDir(), "missing-body.md"), filepath.Join(t.TempDir(), "missing-event.json"))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" || check {
		t.Fatalf("loadPRBody(skip) = %q, %v; want empty body and no check", body, check)
	}
}

func TestLoadDiff(t *testing.T) {
	clearGitEnvironment(t)
	root := t.TempDir()
	writeFile(t, root, "example.go", "package example\n")
	writeFile(t, root, "README.md", "# Before\n")
	if err := gitCommand("-C", root, "init", "--quiet").Run(); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "config", "user.email", "test@example.com")
	trackFiles(t, root)
	runGit(t, root, "commit", "--quiet", "-m", "base")
	base := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	writeFile(t, root, "example.go", "package example\n\nconst changed = true\n")
	writeFile(t, root, "README.md", "# After\n")
	trackFiles(t, root)
	runGit(t, root, "commit", "--quiet", "-m", "change")
	t.Chdir(root)

	changed, patches, err := loadDiff(base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !changed["example.go"] || !changed["README.md"] || !strings.Contains(patches["example.go"], "const changed") {
		t.Fatalf("changed=%v patches=%v", changed, patches)
	}
}

func clearGitEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR",
		"GIT_DIR",
		"GIT_INDEX_FILE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_PREFIX",
		"GIT_WORK_TREE",
	} {
		value, ok := os.LookupEnv(key)
		if !ok {
			continue
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Setenv(key, value); err != nil {
				t.Errorf("restore %s: %v", key, err)
			}
		})
	}
}

func TestBrokenLocalLinksIgnoresFencedExamples(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "README.md", "```md\n[example](missing.md)\n```\n[real](also-missing.md)\n")
	broken := brokenLocalLinksInFiles(root, []string{"README.md"})
	if len(broken) != 1 || !strings.Contains(broken[0], "also-missing.md") {
		t.Fatalf("broken links = %v", broken)
	}
}

func TestWriteReportsAndTruncateWarnings(t *testing.T) {
	root := t.TempDir()
	warnings := filepath.Join(root, "warnings.md")
	summary := filepath.Join(root, "summary.md")
	result := findings{failures: []string{"failure"}, warnings: []string{"warning"}}
	if err := writeReports(result, warnings, summary); err != nil {
		t.Fatal(err)
	}
	warningData, _ := os.ReadFile(warnings)
	summaryData, _ := os.ReadFile(summary)
	if !strings.Contains(string(warningData), "- warning") || !strings.Contains(string(summaryData), "Blocking failures") {
		t.Fatalf("warnings=%q summary=%q", warningData, summaryData)
	}
	if err := writeReports(findings{}, warnings, summary); err != nil {
		t.Fatal(err)
	}
	warningData, _ = os.ReadFile(warnings)
	if len(warningData) != 0 {
		t.Fatalf("warnings file was not truncated: %q", warningData)
	}
}

func policyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "cmd/root.go", `rootCmd.Flags().BoolVar(&opt.Dump, "dump", false, "Dump.")`)
	writeFile(t, root, "OPTIONS.md", "Use `--dump`.\n")
	writeFile(t, root, "README.md", "# Test\n")
	if err := gitCommand("-C", root, "init", "--quiet").Run(); err != nil {
		t.Fatal(err)
	}
	trackFiles(t, root)
	return root
}

func trackFiles(t *testing.T, root string) {
	t.Helper()
	if output, err := gitCommand("-C", root, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", root}, args...)
	output, err := gitCommand(commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
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
