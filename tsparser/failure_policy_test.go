package tsparser

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

var allFailureCategories = []string{
	"timing", "timestamp", "pes_headers", "pts_repetition", "continuity", "transport_errors",
	"scrambled_packets", "sync", "psi_health", "referenced_pids", "unreferenced_pids",
}

func TestFailureCategories(t *testing.T) {
	if got := FailureCategories(); !reflect.DeepEqual(got, allFailureCategories) {
		t.Errorf("FailureCategories() = %v", got)
	}
}

// --fail-on takes comma-separated and repeated values in any order; the
// policy is the union, in the fixed category order, with --fail-on-error
// adding timing. Duplicates add nothing; empty and unknown names are errors.
func TestParseFailurePolicy(t *testing.T) {
	for _, tt := range []struct {
		failOn      []string
		failOnError bool
		want        []string
	}{
		{nil, false, []string{}},
		{nil, true, []string{"timing"}},
		{[]string{"timing"}, false, []string{"timing"}},
		{[]string{"timing"}, true, []string{"timing"}},
		{[]string{"continuity"}, true, []string{"timing", "continuity"}},
		{[]string{"sync,timing"}, false, []string{"timing", "sync"}},
		{[]string{"continuity,timestamp", "continuity", "timestamp,continuity"}, false, []string{"timestamp", "continuity"}},
		{[]string{"any"}, false, allFailureCategories},
		{[]string{"timing", "any", "sync,any"}, true, allFailureCategories},
		{[]string{"unreferenced_pids,referenced_pids,psi_health,scrambled_packets,transport_errors,pts_repetition,pes_headers"}, false,
			[]string{"pes_headers", "pts_repetition", "transport_errors", "scrambled_packets", "psi_health", "referenced_pids", "unreferenced_pids"}},
	} {
		policy, err := parseFailurePolicy(tt.failOn, tt.failOnError)
		if err != nil || !reflect.DeepEqual(policy.categories(), tt.want) {
			t.Errorf("%q %v: %v %v, want %v", tt.failOn, tt.failOnError, policy.categories(), err, tt.want)
		}
	}
	for _, tt := range []struct {
		failOn  []string
		message string
	}{
		{[]string{""}, `--fail-on "" has an empty category`},
		{[]string{"timing,"}, `--fail-on "timing," has an empty category`},
		{[]string{"timing,,sync"}, `--fail-on "timing,,sync" has an empty category`},
		{[]string{"timing", "bogus"}, `unknown --fail-on category "bogus"`},
		{[]string{"Timing"}, `unknown --fail-on category "Timing"`},
		{[]string{"timing, sync"}, `unknown --fail-on category " sync"`},
		{[]string{"pcr_jitter"}, `unknown --fail-on category "pcr_jitter"`},
		{[]string{"packet_gaps"}, `unknown --fail-on category "packet_gaps"`},
		{[]string{"none"}, `unknown --fail-on category "none"`},
	} {
		_, err := parseFailurePolicy(tt.failOn, true)
		want := tt.message + " (expected a comma-separated list of: any, timing, timestamp, pes_headers, pts_repetition, continuity, transport_errors, scrambled_packets, sync, psi_health, referenced_pids, unreferenced_pids)"
		if err == nil || err.Error() != want {
			t.Errorf("%q: err = %v, want %q", tt.failOn, err, want)
		}
		if err := ValidateOptions(options.Options{FailOn: tt.failOn}); err == nil || err.Error() != want {
			t.Errorf("ValidateOptions %q: err = %v", tt.failOn, err)
		}
	}
}

// failurePolicyFixture is a stream with the categories that fail on it. Every
// other category is healthy, skipped or insufficient_data there.
type failurePolicyFixture struct {
	name    string
	paths   [3]string // 188, 192, 204 bytes; "" derives 204 from 188
	failing []string
}

var failurePolicyFixtures = []failurePolicyFixture{
	{"healthy", [3]string{sample188Path, sample192Path, ""}, []string{}},
	{"all findings", [3]string{allFindingsFixtures[0].path, allFindingsFixtures[1].path, allFindingsFixtures[2].path},
		// PCR jitter is ng here but has no category.
		[]string{"timing", "timestamp", "pes_headers", "pts_repetition", "continuity", "transport_errors", "scrambled_packets", "psi_health"}},
	{"sync loss", [3]string{"../sample_data/diagnostics/sample_188byte_sync_loss.ts", "../sample_data/diagnostics/sample_192byte_sync_loss.ts", "../sample_data/diagnostics/sample_204byte_sync_loss.ts"},
		[]string{"continuity", "sync"}},
	// One referenced PID is insufficient_data, and does not fail.
	{"PID references", [3]string{"../sample_data/diagnostics/sample_188byte_pid_references.ts", "../sample_data/diagnostics/sample_192byte_pid_references.ts", ""},
		[]string{"timestamp", "pts_repetition", "transport_errors", "referenced_pids", "unreferenced_pids"}},
	// The PCR-PTS/DTS gap is skipped and both PES checks insufficient_data.
	{"PCR discontinuities", [3]string{"../sample_data/diagnostics/sample_188byte_pcr_discontinuities.ts", "../sample_data/diagnostics/sample_192byte_pcr_discontinuities.ts", ""},
		[]string{"timing"}},
	// Referenced PIDs are insufficient_data.
	{"PES health", [3]string{"../sample_data/diagnostics/sample_188byte_pes_health.ts", "../sample_data/diagnostics/sample_192byte_pes_health.ts", ""},
		[]string{"pes_headers", "pts_repetition"}},
	{"transport errors", [3]string{"../sample_data/diagnostics/sample_188byte_transport_errors.ts", "../sample_data/diagnostics/sample_192byte_transport_errors.ts", ""},
		[]string{"transport_errors", "scrambled_packets", "psi_health", "unreferenced_pids"}},
	// Report-only packet gaps never fail; the forward PTS gaps are anomalies.
	{"packet gaps", [3]string{"../sample_data/diagnostics/sample_188byte_packet_gaps.ts", "../sample_data/diagnostics/sample_192byte_packet_gaps.ts", "../sample_data/diagnostics/sample_204byte_packet_gaps.ts"},
		[]string{"timestamp"}},
}

func (f failurePolicyFixture) path(t *testing.T, size int) string {
	t.Helper()
	if p := f.paths[size]; p != "" {
		return p
	}
	data, err := os.ReadFile(f.paths[0])
	if err != nil {
		t.Fatal(err)
	}
	return writeTestFile(t, "fixture.ts", withTestTrailers(data))
}

// Each category alone, every category with any, and the default fail exactly
// as the reported statuses say, on 188-byte TS, 192-byte M2TS and 204-byte
// TS. The complete report is written before the failure is returned.
func TestFailurePolicyOnFixtures(t *testing.T) {
	covered := map[string]bool{}
	for _, fixture := range failurePolicyFixtures {
		for size, packetSize := range []int{188, 192, 204} {
			path := fixture.path(t, size)
			name := func(failOn []string) string {
				return fixture.name + " " + strings.Join(failOn, ",") + " " + string(rune('0'+size))
			}
			run := func(failOn []string) (AnalysisReport, []string) {
				t.Helper()
				report, err := decodeJSONReport(t, func() error { return ParseTsFile(path, options.Options{Format: "json", FailOn: failOn}) })
				if report.Status != "complete" || report.Input.PacketSizeBytes != packetSize {
					t.Fatalf("%s: %s", name(failOn), toJSON(t, report))
				}
				if err == nil {
					return report, []string{}
				}
				var complianceErr *ComplianceError
				if !errors.As(err, &complianceErr) || len(complianceErr.Checks) < len(complianceErr.Categories) {
					t.Fatalf("%s: err = %v", name(failOn), err)
				}
				return report, complianceErr.Categories
			}

			report, failed := run(nil)
			if !reflect.DeepEqual(failed, []string{}) || report.FailurePolicy != nil {
				t.Errorf("%s: default run failed %v %s", name(nil), failed, toJSON(t, report.FailurePolicy))
			}
			report, failed = run([]string{"any"})
			wantPolicy := &ReportFailurePolicy{Status: "failed", Categories: allFailureCategories, FailedCategories: fixture.failing}
			if len(fixture.failing) == 0 {
				wantPolicy.Status = "passed"
			}
			if !reflect.DeepEqual(failed, fixture.failing) || !jsonEqual(t, report.FailurePolicy, wantPolicy) {
				t.Errorf("%s: failed %v, policy %s; want %v", name([]string{"any"}), failed, toJSON(t, report.FailurePolicy), fixture.failing)
			}
			for _, category := range allFailureCategories {
				want := []string{}
				if strings.Contains(" "+strings.Join(fixture.failing, " ")+" ", " "+category+" ") {
					want = []string{category}
					covered[category] = true
				}
				report, failed := run([]string{category})
				if !reflect.DeepEqual(failed, want) || report.FailurePolicy == nil || !reflect.DeepEqual(report.FailurePolicy.FailedCategories, want) ||
					!reflect.DeepEqual(report.FailurePolicy.Categories, []string{category}) {
					t.Errorf("%s: failed %v, policy %s; want %v", name([]string{category}), failed, toJSON(t, report.FailurePolicy), want)
				}
			}
		}
	}
	for _, category := range allFailureCategories {
		if !covered[category] {
			t.Errorf("no fixture fails %s", category)
		}
	}
}

// --fail-on-error is --fail-on timing: the same failed checks, in the same
// error, with the same report; combined with --fail-on it adds timing.
func TestFailOnErrorIsTimingCategory(t *testing.T) {
	for _, fixture := range allFindingsFixtures {
		var errs [2]error
		var outs [2]string
		for i, opts := range []options.Options{{FailOnError: true}, {FailOn: []string{"timing"}}} {
			outs[i] = captureStdout(t, func() { errs[i] = ParseTsFile(fixture.path, opts) })
		}
		if outs[0] != outs[1] || errs[0] == nil || errs[1] == nil || errs[0].Error() != errs[1].Error() ||
			errs[0].Error() != "compliance check failed: Max PCR interval, PCR-PTS/DTS max gap" ||
			!strings.Contains(outs[0], "Compliance Check Results:") || !strings.Contains(outs[0], "Mux Lead (report only") {
			t.Errorf("%s: %v / %v", fixture.path, errs[0], errs[1])
		}
		report, err := decodeJSONReport(t, func() error {
			return ParseTsFile(fixture.path, options.Options{Format: "json", FailOnError: true, FailOn: []string{"continuity,sync"}})
		})
		var complianceErr *ComplianceError
		if !errors.As(err, &complianceErr) ||
			!reflect.DeepEqual(complianceErr.Categories, []string{"timing", "continuity"}) ||
			!reflect.DeepEqual(complianceErr.Checks, []string{"Max PCR interval", "PCR-PTS/DTS max gap", "Continuity counter"}) ||
			!jsonEqual(t, report.FailurePolicy, ReportFailurePolicy{Status: "failed", Categories: []string{"timing", "continuity", "sync"}, FailedCategories: []string{"timing", "continuity"}}) {
			t.Errorf("%s: %v %s", fixture.path, err, toJSON(t, report.FailurePolicy))
		}
	}
}

// A range that ends in a loss of synchronization is incomplete and exits 0 by
// default; --fail-on sync fails it.
func TestFailOnSyncLost(t *testing.T) {
	path := "../sample_data/diagnostics/sample_188byte_sync_loss.ts"
	report, err := decodeJSONReport(t, func() error {
		return ParseTsFile(path, options.Options{Format: "json", Limit: 23000})
	})
	if err != nil || report.Status != "incomplete" || report.Sync.Status != "lost" {
		t.Fatalf("default: %v %s", err, toJSON(t, report.Sync))
	}
	report, err = decodeJSONReport(t, func() error {
		return ParseTsFile(path, options.Options{Format: "json", Limit: 23000, FailOn: []string{"sync"}})
	})
	var complianceErr *ComplianceError
	if !errors.As(err, &complianceErr) || complianceErr.Error() != "compliance check failed: TS synchronization" ||
		report.Status != "incomplete" || report.Reason != syncLostReason || report.FailurePolicy.Status != "failed" {
		t.Errorf("--fail-on sync: %v %s", err, toJSON(t, report))
	}
}

// Listing programs, automatically or with --list-programs, analyzes no
// program, so no policy can fail and no policy is reported.
func TestFailOnNeverFailsProgramListing(t *testing.T) {
	for _, opts := range []options.Options{
		{Format: "json", FailOn: []string{"any"}},
		{Format: "json", FailOn: []string{"any"}, FailOnError: true},
	} {
		report, err := decodeJSONReport(t, func() error { return ParseTsFile(twoPrograms188Path, opts) })
		if err != nil || report.Status != "programs_listed" || report.FailurePolicy != nil {
			t.Errorf("%+v: %v %s", opts, err, toJSON(t, report))
		}
	}
	opts := options.Options{Format: "json", FailOn: []string{"any"}, ListPrograms: true}
	report, err := decodeJSONReport(t, func() error { return ParseTsFile(allFindingsFixtures[0].path, opts) })
	if err != nil || report.Status != "programs_listed" || report.FailurePolicy != nil {
		t.Errorf("--list-programs: %v %s", err, toJSON(t, report))
	}
	if err := parseTextWithoutOutput(t, twoPrograms188Path); err != nil {
		t.Error(err)
	}
}

// --fail-on judges the summary analysis: --output analysis reports the
// combination as incompatible, and the packet documents reject it.
func TestFailOnIncompatibleOutputs(t *testing.T) {
	var out bytes.Buffer
	err := parseTsFileJSONTo(sample188Path, options.Options{Format: "json", Output: "analysis", FailOn: []string{"timing"}}, &out)
	if err == nil || !strings.Contains(out.String(), `"reason": "incompatible_options"`) || !strings.Contains(err.Error(), "--fail-on-error or --fail-on") {
		t.Errorf("analysis: %v %s", err, out.String())
	}
	for _, output := range []string{"packets", "packet"} {
		err := ValidateOptions(options.Options{Format: "json", Output: output, FailOn: []string{"timing"}})
		if err == nil || err.Error() != "--fail-on judges an analysis and cannot be combined with --output "+output {
			t.Errorf("%s: %v", output, err)
		}
		// --fail-on-error keeps being accepted there, as before.
		if err := ValidateOptions(options.Options{Format: "json", Output: output, FailOnError: true}); err != nil {
			t.Errorf("%s --fail-on-error: %v", output, err)
		}
	}
}

// Without PSI tracking (the exported BufferPes) the PAT/PMT health and PID
// reference checks do not run, so their categories cannot fail; an invalid
// policy is returned before reading.
func TestBufferPesFailurePolicyWithoutPsiTracking(t *testing.T) {
	var data bytes.Buffer
	data.Write(buildPcrPacket(0x31, 27_000_000))
	data.Write(buildPesTsPacket(0x31, 1, encodePtsOnlyPes(190_001)))
	last := buildPcrPacket(0x31, 29_700_001)
	last[3] |= 1
	data.Write(last)
	programInfos := []ProgramInfo{{streamType: 0x1B, elementaryPid: 0x31}}
	var pos int64
	result, err := bufferPes(bytes.NewReader(data.Bytes()), &pos, 0x30, 0x31, programInfos, nil, options.Options{FailOn: []string{"any"}}, 188, 0, true)
	var complianceErr *ComplianceError
	if !errors.As(err, &complianceErr) || !reflect.DeepEqual(complianceErr.Categories, []string{"timing"}) {
		t.Fatalf("err = %v", err)
	}
	if result.health != nil || result.references != nil {
		t.Errorf("unexpected PSI tracking: %+v", result)
	}
	pos = 0
	if err := BufferPes(bytes.NewReader(data.Bytes()), &pos, 0x30, 0x31, programInfos, options.Options{FailOn: []string{"bogus"}}, 188, 0); err == nil || pos != 0 {
		t.Errorf("invalid policy: %v at %d", err, pos)
	}
}
