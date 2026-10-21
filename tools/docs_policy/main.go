package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	flagDefinitionRE = regexp.MustCompile(`Flags\(\)\.\w+Var\([^,]+,\s*"([a-z0-9-]+)"`)
	markdownLinkRE   = regexp.MustCompile(`!?\[[^\]]*\]\(([^)]+)\)`)
	cliContractRE    = regexp.MustCompile(`Flags\(\)\.|^\s*(Use|Short|Long|Args)\s*:|exitCode\s*\(|os\.Exit\s*\(|return\s+[0-9]+`)
	jsonContractRE   = regexp.MustCompile(`json:"|jsonSchemaVersion`)
	outputChangeRE   = regexp.MustCompile(`fmt\.(Print|Fprint)|dumpLine\s*\(|\b(Status|Reason)\s*[:=]`)
)

type findings struct {
	failures []string
	warnings []string
}

func main() {
	base := flag.String("base", "", "base commit for the pull-request diff")
	head := flag.String("head", "HEAD", "head commit for the pull-request diff")
	prBodyFile := flag.String("pr-body-file", "", "optional pull-request body file for local verification")
	skipPRBodyCheck := flag.Bool("skip-pr-body-check", false, "skip pull-request template sections while retaining all repository checks")
	warningsFile := flag.String("warnings-file", "", "path for warning-only Markdown")
	summaryFile := flag.String("summary-file", "", "path to append the complete Markdown summary")
	flag.Parse()

	if *base == "" {
		fmt.Fprintln(os.Stderr, "docs-policy: --base is required")
		os.Exit(2)
	}

	changed, patches, err := loadDiff(*base, *head)
	if err != nil {
		fmt.Fprintf(os.Stderr, "docs-policy: %v\n", err)
		os.Exit(2)
	}
	body := ""
	checkBody := false
	if *skipPRBodyCheck {
		// Dependabot owns its generated PR body, so it cannot follow the
		// repository's human-authored pull-request template.
	} else if *prBodyFile != "" {
		data, readErr := os.ReadFile(*prBodyFile)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "docs-policy: read PR body: %v\n", readErr)
			os.Exit(2)
		}
		body = string(data)
		checkBody = true
	} else if eventPath := os.Getenv("GITHUB_EVENT_PATH"); eventPath != "" {
		body, err = pullRequestBody(eventPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "docs-policy: %v\n", err)
			os.Exit(2)
		}
		checkBody = true
	}
	if checkBody && body == "" {
		body = "\n"
	}

	result := evaluate(".", changed, patches, body)
	if err := writeReports(result, *warningsFile, *summaryFile); err != nil {
		fmt.Fprintf(os.Stderr, "docs-policy: %v\n", err)
		os.Exit(2)
	}

	printFindings(result)
	if len(result.failures) > 0 {
		os.Exit(1)
	}
}

func loadDiff(base, head string) (map[string]bool, map[string]string, error) {
	rangeArg := base + "..." + head
	out, err := gitCommand("diff", "--name-only", "--diff-filter=ACMR", "-z", rangeArg).Output()
	if err != nil {
		return nil, nil, fmt.Errorf("list changed files: %w", err)
	}
	changed := make(map[string]bool)
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			changed[filepath.ToSlash(name)] = true
		}
	}

	patches := make(map[string]string)
	for name := range changed {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		patch, err := gitCommand("diff", "--unified=0", rangeArg, "--", name).Output()
		if err != nil {
			return nil, nil, fmt.Errorf("read diff for %s: %w", name, err)
		}
		patches[name] = string(patch)
	}
	return changed, patches, nil
}

func pullRequestBody(eventPath string) (string, error) {
	if eventPath == "" {
		return "", nil
	}
	data, err := os.ReadFile(eventPath)
	if err != nil {
		return "", fmt.Errorf("read GitHub event: %w", err)
	}
	var event struct {
		PullRequest struct {
			Body string `json:"body"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return "", fmt.Errorf("decode GitHub event: %w", err)
	}
	return event.PullRequest.Body, nil
}

func gitCommand(args ...string) *exec.Cmd {
	command := exec.Command("git", args...)
	for _, entry := range os.Environ() {
		key := strings.SplitN(entry, "=", 2)[0]
		switch key {
		case "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_PREFIX", "GIT_WORK_TREE":
			continue
		}
		command.Env = append(command.Env, entry)
	}
	return command
}

func evaluate(root string, changed map[string]bool, patches map[string]string, body string) findings {
	var result findings

	if body != "" {
		for _, heading := range []string{"Why", "What changed", "Compatibility", "Documentation", "Validation"} {
			if !hasHeading(body, heading) {
				result.failures = append(result.failures, fmt.Sprintf("PR description is missing the `## %s` section from `.github/pull_request_template.md`.", heading))
			}
		}
	}

	rootSource, err := os.ReadFile(filepath.Join(root, "cmd", "root.go"))
	if err != nil {
		result.failures = append(result.failures, "Could not read `cmd/root.go` to verify documented flags.")
	} else {
		optionsText, readErr := os.ReadFile(filepath.Join(root, "OPTIONS.md"))
		if readErr != nil {
			result.failures = append(result.failures, "Could not read `OPTIONS.md` to verify documented flags.")
		} else {
			for _, name := range definedFlags(string(rootSource)) {
				if !documentedFlag(string(optionsText), name) {
					result.failures = append(result.failures, fmt.Sprintf("CLI flag `--%s` is not documented in `OPTIONS.md`.", name))
				}
			}
		}
	}

	result.failures = append(result.failures, brokenLocalLinks(root)...)

	cliContract := patchMatches(patches["cmd/root.go"], cliContractRE) ||
		(changed["options/options.go"] && hasMeaningfulChangedLine(patches["options/options.go"]))
	jsonContract := patchMatches(patches["tsparser/report.go"], jsonContractRE)

	if cliContract {
		requireDocumentation(&result, changed, body, "public CLI interface", []string{"README.md", "OPTIONS.md"})
	}
	if jsonContract {
		requireDocumentation(&result, changed, body, "JSON contract", []string{"README.md", "OPTIONS.md", "docs/json-schema-v1.json"})
	}

	var ambiguous []string
	for name, patch := range patches {
		if strings.HasSuffix(name, "_test.go") || !strings.HasPrefix(name, "tsparser/") {
			continue
		}
		if patchMatches(patch, outputChangeRE) {
			ambiguous = append(ambiguous, name)
		}
	}
	if changed["cmd/root.go"] && !cliContract {
		ambiguous = append(ambiguous, "cmd/root.go")
	}
	if len(ambiguous) > 0 && (!changed["README.md"] || !changed["OPTIONS.md"]) {
		sort.Strings(ambiguous)
		result.warnings = append(result.warnings,
			"Possible user-visible output changes were detected in `"+strings.Join(ambiguous, "`, `")+"`. Confirm the documentation impact in the PR `Documentation` section; update `README.md` and `OPTIONS.md` if behavior or output changed.")
	}

	sort.Strings(result.failures)
	sort.Strings(result.warnings)
	return result
}

func definedFlags(source string) []string {
	matches := flagDefinitionRE.FindAllStringSubmatch(source, -1)
	names := make([]string, 0, len(matches))
	seen := make(map[string]bool)
	for _, match := range matches {
		if !seen[match[1]] {
			seen[match[1]] = true
			names = append(names, match[1])
		}
	}
	sort.Strings(names)
	return names
}

func documentedFlag(document, name string) bool {
	pattern := regexp.MustCompile(`--` + regexp.QuoteMeta(name) + `(?:[^a-z0-9-]|$)`)
	return pattern.MatchString(strings.ToLower(document))
}

func patchMatches(patch string, pattern *regexp.Regexp) bool {
	for _, line := range changedLines(patch) {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

func hasMeaningfulChangedLine(patch string) bool {
	for _, line := range changedLines(patch) {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && trimmed != "{" && trimmed != "}" && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "package ") {
			return true
		}
	}
	return false
}

func changedLines(patch string) []string {
	var lines []string
	for _, line := range strings.Split(patch, "\n") {
		if len(line) < 2 || (line[0] != '+' && line[0] != '-') || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		lines = append(lines, line[1:])
	}
	return lines
}

func requireDocumentation(result *findings, changed map[string]bool, body, kind string, required []string) {
	section := sectionBody(body, "Documentation")
	for _, name := range required {
		mentioned := mentionedFile(section, name)
		if !changed[name] && !documentedAsUnaffected(section, name) {
			result.failures = append(result.failures, fmt.Sprintf("The %s changed, but `%s` was not updated. Update it or explain why it is unaffected in the PR `Documentation` section.", kind, name))
		}
		if body != "" && !mentioned {
			result.failures = append(result.failures, fmt.Sprintf("The PR `Documentation` section must mention `%s` when the %s changes.", name, kind))
		}
	}
}

func documentedAsUnaffected(section, name string) bool {
	contextRE := regexp.MustCompile(`[;]|\.(?:\s+|$)`)
	for _, context := range contextRE.Split(strings.ReplaceAll(section, "\r\n", "\n"), -1) {
		if !mentionedFile(context, name) {
			continue
		}
		lower := strings.ToLower(context)
		for _, phrase := range []string{"not affected", "not applicable", "no update", "does not need", "unchanged"} {
			if strings.Contains(lower, phrase) {
				return true
			}
		}
	}
	return false
}

func mentionedFile(text, name string) bool {
	pattern := regexp.MustCompile(`(?i)(?:^|[^a-z0-9_./-])` + regexp.QuoteMeta(name) + `(?:$|[^a-z0-9_./-])`)
	return pattern.MatchString(text)
}

func hasHeading(body, heading string) bool {
	return sectionBody(body, heading) != ""
}

func sectionBody(body, heading string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	want := "## " + strings.ToLower(heading)
	start := -1
	for i, line := range lines {
		if strings.ToLower(strings.TrimSpace(line)) == want {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

func brokenLocalLinks(root string) []string {
	out, err := gitCommand("-C", root, "ls-files", "-z", "--", "*.md").Output()
	if err != nil {
		return []string{"Could not list Markdown files for local-link validation."}
	}
	var markdownFiles []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			markdownFiles = append(markdownFiles, filepath.ToSlash(name))
		}
	}
	return brokenLocalLinksInFiles(root, markdownFiles)
}

func brokenLocalLinksInFiles(root string, markdownFiles []string) []string {
	var broken []string
	for _, name := range markdownFiles {
		path := filepath.Join(root, filepath.FromSlash(name))
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			broken = append(broken, fmt.Sprintf("Could not read `%s` for local-link validation.", name))
			continue
		}
		inFence := false
		for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			for _, match := range markdownLinkRE.FindAllStringSubmatch(line, -1) {
				fields := strings.Fields(match[1])
				if len(fields) == 0 {
					continue
				}
				target := strings.Trim(fields[0], "<>")
				target = strings.SplitN(target, "#", 2)[0]
				if target == "" || strings.HasPrefix(target, "/") || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
					continue
				}
				path := filepath.Join(root, filepath.Dir(filepath.FromSlash(name)), filepath.FromSlash(target))
				if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
					broken = append(broken, fmt.Sprintf("`%s` links to missing local path `%s`.", name, target))
				}
			}
		}
	}
	return broken
}

func writeReports(result findings, warningsFile, summaryFile string) error {
	if warningsFile != "" {
		content := ""
		if len(result.warnings) > 0 {
			content = markdownList(result.warnings)
		}
		if err := os.WriteFile(warningsFile, []byte(content), 0o600); err != nil {
			return fmt.Errorf("write warnings report: %w", err)
		}
	}
	if summaryFile != "" {
		var summary strings.Builder
		summary.WriteString("## Documentation policy\n\n")
		if len(result.failures) == 0 {
			summary.WriteString("No blocking documentation-policy failures.\n")
		} else {
			summary.WriteString("### Blocking failures\n\n")
			summary.WriteString(markdownList(result.failures))
		}
		if len(result.warnings) > 0 {
			summary.WriteString("\n### Warnings\n\n")
			summary.WriteString(markdownList(result.warnings))
		}
		file, err := os.OpenFile(summaryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("open summary report: %w", err)
		}
		_, writeErr := file.WriteString(summary.String())
		closeErr := file.Close()
		if writeErr != nil {
			return fmt.Errorf("write summary report: %w", writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close summary report: %w", closeErr)
		}
	}
	return nil
}

func markdownList(items []string) string {
	var b strings.Builder
	for _, item := range items {
		fmt.Fprintf(&b, "- %s\n", item)
	}
	return b.String()
}

func printFindings(result findings) {
	for _, failure := range result.failures {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", failure)
	}
	for _, warning := range result.warnings {
		fmt.Printf("WARNING: %s\n", warning)
	}
	if len(result.failures) == 0 && len(result.warnings) == 0 {
		fmt.Println("Documentation policy checks passed.")
	}
}
