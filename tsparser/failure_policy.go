package tsparser

import (
	"fmt"
	"slices"
	"strings"
)

// failOnAny selects every failure category.
const failOnAny = "any"

// failureCategory is one group of transport-health verdicts that --fail-on can
// select. failures returns the name of each check of the category whose
// reported status is a finding. It reads the same JSON statuses the report
// prints, so the exit status never disagrees with the report: a healthy,
// skipped or insufficient_data status is never a failure, and report-only
// results (packet gaps, mux lead, bitrate) and the PCR jitter estimate have
// no category at all.
type failureCategory struct {
	name     string
	failures func(a *pesAnalysis) []string
}

// failureCategories are the selectable categories in their fixed order, the
// order of the Overview table: the policy, its JSON object and the error list
// them in this order whatever the order of the arguments.
var failureCategories = []failureCategory{
	{"timing", func(a *pesAnalysis) []string { return a.compliance.failures() }},
	{"timestamp", func(a *pesAnalysis) []string {
		return failingCheck("PTS/DTS anomalies", reportAnomalies(a.anomalies).Status, "anomalies")
	}},
	{"pes_headers", func(a *pesAnalysis) []string {
		headers, _ := reportPESHealth(a.pes)
		return failingCheck("PES headers", headers.Status, "errors")
	}},
	{"pts_repetition", func(a *pesAnalysis) []string {
		_, repetition := reportPESHealth(a.pes)
		return failingCheck("PTS repetition", repetition.Status, "ng")
	}},
	{"continuity", func(a *pesAnalysis) []string {
		return failingCheck("Continuity counter", reportContinuity(a.continuity).Status, "errors")
	}},
	{"transport_errors", func(a *pesAnalysis) []string {
		errors, _ := reportTransportTrust(a.trust)
		return failingCheck("Transport errors", errors.Status, "affected", "data_excluded")
	}},
	{"scrambled_packets", func(a *pesAnalysis) []string {
		_, scrambled := reportTransportTrust(a.trust)
		return failingCheck("Scrambled packets", scrambled.Status, "affected", "data_excluded")
	}},
	{"sync", func(a *pesAnalysis) []string {
		return failingCheck("TS synchronization", reportSync(a.sync).Status, "recovered", "lost")
	}},
	{"psi_health", func(a *pesAnalysis) []string {
		if a.health == nil {
			return nil
		}
		return failingCheck("PAT/PMT health", reportPsiHealth(a.health).Status, "ng")
	}},
	{"referenced_pids", func(a *pesAnalysis) []string {
		if a.references == nil {
			return nil
		}
		return failingCheck("Referenced PIDs", reportPidReferences(a.references).Referenced.Status, "absent", "silent")
	}},
	{"unreferenced_pids", func(a *pesAnalysis) []string {
		if a.references == nil {
			return nil
		}
		return failingCheck("Unreferenced PIDs", reportPidReferences(a.references).Unreferenced.Status, "unreferenced")
	}},
}

// failingCheck returns name when status is one of failing.
func failingCheck(name, status string, failing ...string) []string {
	if slices.Contains(failing, status) {
		return []string{name}
	}
	return nil
}

// FailureCategories returns the categories --fail-on accepts besides "any",
// in their fixed order.
func FailureCategories() []string {
	names := make([]string, len(failureCategories))
	for i, c := range failureCategories {
		names[i] = c.name
	}
	return names
}

// failurePolicy is the set of selected categories, one bit per entry of
// failureCategories.
type failurePolicy uint32

// parseFailurePolicy reads the --fail-on values and --fail-on-error. Each
// value is a comma-separated list of category names or "any"; repeated
// values and duplicate names add nothing. --fail-on-error selects timing, so
// combining it with --fail-on gives the union. An empty or unknown name is an
// error.
func parseFailurePolicy(failOn []string, failOnError bool) (failurePolicy, error) {
	var policy failurePolicy
	if failOnError {
		policy |= 1 // timing
	}
	for _, value := range failOn {
		for name := range strings.SplitSeq(value, ",") {
			index := slices.IndexFunc(failureCategories, func(c failureCategory) bool { return c.name == name })
			switch {
			case name == failOnAny:
				policy |= 1<<len(failureCategories) - 1
			case index >= 0:
				policy |= 1 << index
			case name == "":
				return 0, fmt.Errorf("--fail-on %q has an empty category (expected a comma-separated list of: %s)", value, failOnChoices())
			default:
				return 0, fmt.Errorf("unknown --fail-on category %q (expected a comma-separated list of: %s)", name, failOnChoices())
			}
		}
	}
	return policy, nil
}

func failOnChoices() string {
	return failOnAny + ", " + strings.Join(FailureCategories(), ", ")
}

// categories returns the selected category names in their fixed order.
func (p failurePolicy) categories() []string {
	names := []string{}
	for i, c := range failureCategories {
		if p&(1<<i) != 0 {
			names = append(names, c.name)
		}
	}
	return names
}

// evaluate returns the selected categories with a finding, and the checks
// that reported it, both in their fixed order.
func (p failurePolicy) evaluate(a *pesAnalysis) (categories, checks []string) {
	categories = []string{}
	for i, c := range failureCategories {
		if p&(1<<i) == 0 {
			continue
		}
		if failed := c.failures(a); len(failed) > 0 {
			categories = append(categories, c.name)
			checks = append(checks, failed...)
		}
	}
	return categories, checks
}

// ReportFailurePolicy is the selected --fail-on / --fail-on-error policy and
// its result for the analyzed program. Status is failed when at least one
// selected category reported a finding (the command then exits 2), passed
// otherwise.
type ReportFailurePolicy struct {
	Status           string   `json:"status"`
	Categories       []string `json:"categories"`
	FailedCategories []string `json:"failed_categories"`
}

// reportFailurePolicy returns nil when no policy is selected, so the field
// is left out of reports produced without one.
func reportFailurePolicy(a *pesAnalysis) *ReportFailurePolicy {
	if a.policy == 0 {
		return nil
	}
	r := &ReportFailurePolicy{Status: "passed", Categories: a.policy.categories(), FailedCategories: a.failedCategories}
	if len(a.failedCategories) > 0 {
		r.Status = "failed"
	}
	return r
}
