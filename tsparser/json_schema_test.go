package tsparser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// The JSON outputs are validated against the checked-in schemas with
// github.com/santhosh-tekuri/jsonschema/v6 (Draft 2020-12, Apache-2.0, the
// license of this project and of cobra). Only _test.go files import it, so it
// is never linked into the CLI binary.

// Schema files, relative to docs/, by the output they describe.
const (
	summarySchemaFile  = "json-schema-v2.json"
	analysisSchemaFile = "json-schema-analysis-v2.json"
	packetsSchemaFile  = "json-schema-packets-v1.json"
)

var schemaFiles = []string{summarySchemaFile, analysisSchemaFile, packetsSchemaFile}

// docsSiteURL reads site_url from mkdocs.yml: MkDocs publishes docs/ there,
// so every schema's $id must be that URL followed by its file name.
func docsSiteURL(t *testing.T) string {
	t.Helper()
	file, err := os.Open("../mkdocs.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if value, ok := strings.CutPrefix(scanner.Text(), "site_url:"); ok {
			return strings.TrimSpace(value)
		}
	}
	t.Fatal("mkdocs.yml has no site_url")
	return ""
}

// readSchemaDocs decodes the checked-in schema files, keyed by file name.
func readSchemaDocs(t *testing.T) map[string]any {
	t.Helper()
	docs := map[string]any{}
	for _, name := range schemaFiles {
		data, err := os.ReadFile(filepath.Join("../docs", name))
		if err != nil {
			t.Fatal(err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		docs[name] = doc
	}
	return docs
}

// offlineLoader refuses every load: the schemas and their cross-file $refs
// must resolve to the registered local files, never to the network.
type offlineLoader struct{}

func (offlineLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("refusing to load %s: not a checked-in schema", url)
}

// compileSchemas registers each document at its published URL (site + file
// name) and compiles it. A document whose $id is not that URL resolves its
// relative $refs elsewhere and fails to compile.
func compileSchemas(site string, docs map[string]any) (map[string]*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(offlineLoader{})
	for _, name := range schemaFiles {
		if err := compiler.AddResource(site+name, docs[name]); err != nil {
			return nil, err
		}
	}
	schemas := map[string]*jsonschema.Schema{}
	for _, name := range schemaFiles {
		schema, err := compiler.Compile(site + name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		schemas[name] = schema
	}
	return schemas, nil
}

// loadSchemas compiles the checked-in schemas after checking their dialect
// and identifiers.
func loadSchemas(t *testing.T) map[string]*jsonschema.Schema {
	t.Helper()
	site := docsSiteURL(t)
	docs := readSchemaDocs(t)
	for _, name := range schemaFiles {
		doc := docs[name].(map[string]any)
		if doc["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
			t.Errorf("%s: $schema = %v", name, doc["$schema"])
		}
		if doc["$id"] != site+name {
			t.Errorf("%s: $id = %v, want %s", name, doc["$id"], site+name)
		}
	}
	schemas, err := compileSchemas(site, docs)
	if err != nil {
		t.Fatalf("compile schemas: %v", err)
	}
	return schemas
}

// validateJSON validates an emitted document. The error lists every failing
// instance location with the schema keyword that rejected it.
func validateJSON(schema *jsonschema.Schema, data []byte) error {
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("output is not JSON: %w", err)
	}
	if err := schema.Validate(instance); err != nil {
		var validationErr *jsonschema.ValidationError
		if errors.As(err, &validationErr) {
			return fmt.Errorf("%#v", validationErr)
		}
		return err
	}
	return nil
}

// absent and present are pointer expectations that only test existence.
type (
	absent  struct{}
	present struct{}
)

// lookupPointer resolves a JSON Pointer (RFC 6901) in a decoded document.
func lookupPointer(doc any, pointer string) (any, bool) {
	if pointer == "" {
		return doc, true
	}
	for _, token := range strings.Split(pointer[1:], "/") {
		token = strings.NewReplacer("~1", "/", "~0", "~").Replace(token)
		switch v := doc.(type) {
		case map[string]any:
			next, ok := v[token]
			if !ok {
				return nil, false
			}
			doc = next
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(v) {
				return nil, false
			}
			doc = v[i]
		default:
			return nil, false
		}
	}
	return doc, true
}

// checkPointers asserts that each case reached the shape it is meant to
// cover, so a fixture that drifts to another state cannot pass silently.
func checkPointers(t *testing.T, data []byte, want map[string]any) {
	t.Helper()
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for pointer, expected := range want {
		got, ok := lookupPointer(doc, pointer)
		switch expected.(type) {
		case absent:
			if ok {
				t.Errorf("%s: present (%v), want absent", pointer, got)
			}
		case present:
			if !ok {
				t.Errorf("%s: absent, want present", pointer)
			}
		default:
			wantJSON, _ := json.Marshal(expected)
			gotJSON, _ := json.Marshal(got)
			if !ok || !bytes.Equal(gotJSON, wantJSON) {
				t.Errorf("%s = %s (present: %v), want %s", pointer, gotJSON, ok, wantJSON)
			}
		}
	}
}

// writeFixture writes data to a file in a per-test directory.
func writeFixture(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// fullStreamFixture is writeFullStream's single-program stream in a file.
func fullStreamFixture(t *testing.T, pesPackets int, pcrs []uint64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "full.ts")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFullStream(file, pesPackets, pcrs)
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// unparsedPmtFixture is a two-program stream whose second PMT is missing.
func unparsedPmtFixture(t *testing.T) string {
	t.Helper()
	path := multiProgramFile(t, true)
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

// schemaCase runs one real output path and validates what it wrote.
type schemaCase struct {
	name    string
	schema  string
	path    func(t *testing.T) string
	opts    options.Options
	wantErr bool
	want    map[string]any
}

func fixedPath(path string) func(*testing.T) string { return func(*testing.T) string { return path } }

// emit writes the case's document through the same functions the CLI uses.
func (c schemaCase) emit(t *testing.T, w io.Writer) error {
	t.Helper()
	opts := c.opts
	opts.Format = "json"
	path := c.path(t)
	if opts.Output == "packets" || opts.Output == "packet" {
		return writePacketsDocument(path, opts, w)
	}
	return parseTsFileJSONTo(path, opts, w)
}

const psiVersionChange188Path = "../sample_data/sample_188byte_psi_version_change_mpeg2_320x240_25fps_mp2.ts"

func summarySchemaCases() []schemaCase {
	return []schemaCase{
		{
			name: "complete without optional sections", schema: summarySchemaFile, path: fixedPath(sample188Path),
			want: map[string]any{
				"/status": "complete", "/reason": absent{}, "/error": absent{}, "/selected_program_number": 1,
				"/transport_errors/status": "ok", "/transport_errors/first_offset_bytes": absent{}, "/transport_errors/pids": []any{},
				"/scrambled_packets/status": "ok", "/scrambled_packets/packet_count": 0,
				"/pes_headers/status": "ok", "/pes_headers/reason": absent{}, "/pes_headers/first_error_offset_bytes": absent{}, "/pes_headers/errors": []any{},
				"/pts_repetition/status": "ok", "/pts_repetition/limit_ms": 700, "/pts_repetition/first_exceeded_offset_bytes": absent{},
				"/pts_repetition/pids/0/reason": absent{}, "/pts_repetition/pids/0/max_interval_ms": present{},
				"/pid_references/referenced/status": "ok", "/pid_references/unreferenced/status": "ok", "/pid_references/unreferenced/reason": absent{},
				"/pid_references/referenced/pids/0/stream_type": absent{}, "/pid_references/referenced/pids/2/stream_type": "0x02",
				"/pcr_jitter": absent{}, "/bitrate": absent{}, "/psi_transitions": absent{},
				"/warnings": []any{}, "/input/offset_bytes": 0, "/input/limit_bytes": absent{},
				"/continuity_counter/total_error_count": 0, "/continuity_counter/pids": []any{},
				"/timestamp_anomalies/items": []any{}, "/compliance/max_pcr_interval/status": "ok",
			},
		},
		{
			name: "complete with PCR jitter and bitrate, variable delivery", schema: summarySchemaFile, path: fixedPath(sample188Path),
			opts: options.Options{DumpPcrJitter: true, DumpBitrate: true},
			want: map[string]any{
				"/status": "complete", "/pcr_jitter/status": "not_assessable", "/pcr_jitter/reason": present{},
				"/pcr_jitter/estimate/model": "byte_position", "/pcr_jitter/estimate/max_abs_jitter_ns": present{},
				"/pcr_jitter/delivery/assessment": "variable", "/pcr_jitter/delivery/max_deviation_percent": present{},
				"/bitrate/status": "ok", "/bitrate/null_pid/pid": "0x1fff", "/bitrate/reason": absent{},
			},
		},
		{
			name: "complete M2TS with PCR jitter and bitrate", schema: summarySchemaFile, path: fixedPath(sample192Path),
			opts: options.Options{DumpPcrJitter: true, DumpBitrate: true},
			want: map[string]any{"/status": "complete", "/input/packet_size_bytes": 192, "/pcr_jitter/estimate": present{}, "/bitrate/status": "ok"},
		},
		{
			name: "complete with failed checks, judged PCR jitter and --fail-on-error", schema: summarySchemaFile, path: fixedPath(allFindingsFixtures[0].path),
			opts: options.Options{DumpPcrJitter: true, DumpBitrate: true, FailOnError: true}, wantErr: true,
			want: map[string]any{
				"/status": "complete", "/compliance/max_pcr_interval/status": "ng", "/compliance/max_pcr_pts_dts_gap/status": "ng",
				"/compliance/pcr_discontinuities/status": "ok", "/compliance/pcr_discontinuities/reason": absent{},
				"/timestamp_anomalies/status": "anomalies", "/continuity_counter/status": "errors",
				"/pcr_jitter/status": "ng", "/pcr_jitter/reason": absent{}, "/pcr_jitter/delivery/assessment": "constant",
				"/pcr_jitter/delivery/max_deviation_percent": 0, "/pcr_jitter/estimate/min_jitter_ns": -50_000,
			},
		},
		{
			name: "complete with skipped checks and sections", schema: summarySchemaFile,
			path: func(t *testing.T) string { return fullStreamFixture(t, 1, []uint64{13500}) },
			opts: options.Options{DumpPcrJitter: true, DumpBitrate: true},
			want: map[string]any{
				"/status": "complete", "/compliance/max_pcr_interval/status": "skipped", "/compliance/max_pcr_interval/reason": present{},
				"/compliance/max_pcr_interval/value_ms": absent{}, "/compliance/max_pcr_interval/observations": 0,
				"/compliance/pcr_discontinuities/status": "skipped", "/compliance/pcr_discontinuities/reason": present{},
				"/compliance/pcr_discontinuities/pcr_count": 1, "/compliance/pcr_discontinuities/items": []any{},
				"/pcr_jitter/status": "skipped", "/pcr_jitter/reason": present{}, "/pcr_jitter/estimate/observations": 0,
				"/pcr_jitter/estimate/max_abs_jitter_ns": absent{}, "/pcr_jitter/estimate/discontinuities": []any{},
				"/pcr_jitter/delivery/assessment": "unknown", "/pcr_jitter/delivery/window_count": 0,
				"/pcr_jitter/delivery/average_rate_bps": absent{},
				"/bitrate/status":                       "skipped", "/bitrate/reason": present{}, "/bitrate/duration_seconds": 0,
				"/bitrate/pids": []any{}, "/bitrate/null_pid": absent{}, "/bitrate/program_average_bps": 0,
			},
		},
		{
			name: "complete with transport errors and scrambled packets", schema: summarySchemaFile, path: fixedPath(transportErrorsFixtures[0].path),
			want: map[string]any{
				"/status": "complete", "/transport_errors/status": "data_excluded", "/transport_errors/first_offset_bytes": 0,
				"/transport_errors/pids/4/label": "null", "/transport_errors/pids/4/excluded_packet_count": 0,
				"/scrambled_packets/status": "data_excluded", "/scrambled_packets/pids/3/pid": "0x0200",
			},
		},
		{
			name: "complete with PES header errors and an excessive PTS interval", schema: summarySchemaFile, path: fixedPath(pesHealthFixtures[0].path),
			want: map[string]any{
				"/status": "complete", "/pes_headers/status": "errors", "/pes_headers/error_count": 8, "/pes_headers/errors/4/kind": "bad_marker_bits",
				"/pes_headers/pids/2/first_error_offset_bytes": 27 * 188, "/pes_headers/pids/4/status": "insufficient_data", "/pes_headers/pids/4/reason": "no_pes",
				"/pts_repetition/status": "ng", "/pts_repetition/max_interval_ms": 1080, "/pts_repetition/first_exceeded_offset_bytes": 152 * 188,
				"/pts_repetition/pids/2/status": "skipped", "/pts_repetition/pids/2/reason": "not_video_or_audio", "/pts_repetition/pids/2/max_interval_ms": absent{},
				"/pts_repetition/pids/3/reason": "single_pts",
			},
		},
		{
			name: "complete with PID reference findings", schema: summarySchemaFile, path: fixedPath(pidReferencesFixtures[0].path),
			want: map[string]any{
				"/status": "complete", "/pid_references/time_basis": "pcr_interpolated_file_position",
				"/pid_references/referenced/status": "absent", "/pid_references/referenced/pids/3/status": "silent",
				"/pid_references/referenced/pids/4/reason": "intermittent_stream_type", "/pid_references/referenced/pids/4/first_offset_bytes": absent{},
				"/pid_references/unreferenced/status": "unreferenced", "/pid_references/unreferenced/pids/1/status": "insufficient_data",
			},
		},
		{
			name: "complete with unreferenced PIDs skipped for a selected program", schema: summarySchemaFile, path: fixedPath(twoPrograms188Path),
			opts: options.Options{Program: 1},
			want: map[string]any{
				"/status": "complete", "/pid_references/unreferenced/status": "skipped",
				"/pid_references/unreferenced/reason": "program_layouts_unknown", "/pid_references/referenced/pids/4/pid": "0x1001", "/pid_references/referenced/pids/4/role": "pmt",
			},
		},
		{
			name: "complete with PCR discontinuities", schema: summarySchemaFile, path: fixedPath(pcrDiscontinuitiesFixtures[0].path),
			opts: options.Options{DumpPcrJitter: true},
			want: map[string]any{
				"/status": "complete", "/compliance/pcr_discontinuities/status": "ng", "/compliance/pcr_discontinuities/undeclared_count": 2,
				"/compliance/pcr_discontinuities/items/0/kind": "wraparound", "/compliance/pcr_discontinuities/items/4/kind": "forward_jump",
				"/pcr_jitter/estimate/discontinuities/0/kind": "declared",
			},
		},
		{
			name: "complete for a selected program", schema: summarySchemaFile, path: fixedPath(twoPrograms188Path),
			opts: options.Options{Program: 2},
			want: map[string]any{"/status": "complete", "/selected_program_number": 2, "/programs/0/program_number": 2},
		},
		{
			name: "complete with PSI transitions", schema: summarySchemaFile, path: fixedPath(psiVersionChange188Path),
			want: map[string]any{"/status": "complete", "/psi_transitions/0": present{}},
		},
		{
			name: "complete with a truncated-tail warning and a byte limit", schema: summarySchemaFile,
			path: func(t *testing.T) string {
				data, err := os.ReadFile(sample188Path)
				if err != nil {
					t.Fatal(err)
				}
				return writeFixture(t, "truncated.ts", data[:188*400+50])
			},
			opts: options.Options{Limit: 188*400 + 50},
			want: map[string]any{"/status": "complete", "/input/limit_bytes": 188*400 + 50, "/warnings/0/message": present{}},
		},
		{
			name: "programs_listed for a required selection", schema: summarySchemaFile, path: fixedPath(twoPrograms188Path),
			want: map[string]any{
				"/status": "programs_listed", "/reason": "program_selection_required", "/error": absent{},
				"/selected_program_number": absent{}, "/compliance": absent{}, "/programs/1/status": "complete",
				"/programs/1/service": present{},
			},
		},
		{
			name: "programs_listed on request with an unparsed PMT", schema: summarySchemaFile,
			path: unparsedPmtFixture,
			opts: options.Options{ListPrograms: true},
			want: map[string]any{
				"/status": "programs_listed", "/reason": "program_listing_requested", "/programs/1/status": "incomplete",
				"/programs/1/reason": "pmt_parse_failed", "/programs/1/error": present{}, "/programs/1/elementary_streams": []any{},
			},
		},
		{
			name: "incomplete at the end of the input", schema: summarySchemaFile,
			path: func(t *testing.T) string {
				pat := []byte{0x00, 0xB0, 0x0D, 0x00, 0x3F, 0xC1, 0x00, 0x00, 0x00, 0x01, 0xE0, 0x3F, 0x2D, 0xBC, 0xB0, 0x53}
				var data []byte
				data = append(data, buildTsPacket(0x0000, true, 0, pat)...)
				data = append(data, buildStuffingPacket()...)
				data = append(data, buildTsPacket(0x0000, true, 1, pat)...)
				data = append(data, buildStuffingPacket()...)
				return writeFixture(t, "pat-only.ts", data)
			},
			want: map[string]any{
				"/status": "incomplete", "/reason": "end_of_input_before_complete_analysis", "/error": absent{},
				"/programs": []any{}, "/compliance": absent{}, "/transport_errors": absent{}, "/scrambled_packets": absent{}, "/pid_references": absent{}, "/pes_headers": absent{}, "/pts_repetition": absent{},
			},
		},
		{
			name: "failed to open the input", schema: summarySchemaFile, path: fixedPath("/nonexistent/input.ts"), wantErr: true,
			want: map[string]any{"/status": "failed", "/reason": "input_open_failed", "/error": present{}, "/programs": []any{}, "/warnings": []any{}},
		},
		{
			name: "failed on incompatible options", schema: summarySchemaFile, path: fixedPath("unused.ts"),
			opts: options.Options{DumpHeader: true}, wantErr: true,
			want: map[string]any{"/status": "failed", "/reason": "incompatible_options", "/error": present{}},
		},
		{
			name: "failed without a transport stream", schema: summarySchemaFile,
			path:    func(t *testing.T) string { return writeFixture(t, "garbage.ts", bytes.Repeat([]byte{0xFF}, 4096)) },
			wantErr: true,
			want:    map[string]any{"/status": "failed", "/reason": "sync_not_acquired", "/error": present{}, "/sync": absent{}},
		},
		{
			name: "failed with packets but without a PAT", schema: summarySchemaFile,
			path:    func(t *testing.T) string { return writeFixture(t, "no-pat.ts", syncTestStream(nil, 20)[4*188:]) },
			wantErr: true,
			want:    map[string]any{"/status": "failed", "/reason": "analysis_failed", "/error": present{}},
		},
		{
			name: "complete after recovered sync losses", schema: summarySchemaFile, path: fixedPath(syncLossFixtures[0].path),
			want: map[string]any{
				"/status": "complete", "/sync/status": "recovered", "/sync/loss_count": 4, "/sync/resync_count": 4,
				"/sync/first_loss_offset_bytes": 7520, "/sync/unrecovered_offset_bytes": absent{}, "/sync/trailing_partial_packet_bytes": 100,
			},
		},
		{
			name: "incomplete after an unrecovered sync loss", schema: summarySchemaFile,
			path: func(t *testing.T) string { return writeFixture(t, "lost.ts", syncTestStream(make([]byte, 50), 3)) },
			want: map[string]any{
				"/status": "incomplete", "/reason": "sync_lost", "/sync/status": "lost", "/sync/reason": present{},
				"/sync/unrecovered_offset_bytes": present{}, "/sync/resync_count": 0, "/compliance": present{},
			},
		},
	}
}

func analysisSchemaCases() []schemaCase {
	analysis := options.Options{Output: "analysis"}
	synthetic := func(data func() []byte) func(*testing.T) string {
		return func(t *testing.T) string { return writeFixture(t, "synthetic.ts", data()) }
	}
	return []schemaCase{
		{
			name: "complete for two programs", schema: analysisSchemaFile, path: fixedPath(twoPrograms188Path), opts: analysis,
			want: map[string]any{
				"/status": "complete", "/output": "analysis", "/programs/1/pcr_jitter/estimate": present{},
				"/programs/0/bitrate": present{}, "/continuity_counter/total_error_count": 0, "/warnings": []any{},
			},
		},
		{
			name: "complete for two programs in M2TS", schema: analysisSchemaFile, path: fixedPath(twoPrograms192Path), opts: analysis,
			want: map[string]any{"/status": "complete", "/input/packet_size_bytes": 192},
		},
		{
			name: "complete with variable delivery", schema: analysisSchemaFile, path: fixedPath(sample188Path), opts: analysis,
			want: map[string]any{
				"/status": "complete", "/programs/0/pcr_jitter/status": "not_assessable",
				"/programs/0/pcr_jitter/delivery/assessment": "variable", "/programs/0/bitrate/null_pid/pid": "0x1fff",
			},
		},
		{
			name: "complete with every finding", schema: analysisSchemaFile, path: fixedPath(allFindingsFixtures[1].path), opts: analysis,
			want: map[string]any{
				"/status": "complete", "/programs/0/pcr_jitter/status": "ng", "/programs/0/pcr_jitter/delivery/assessment": "constant",
				"/transport_errors/status": "affected", "/scrambled_packets/pids/0/pid": "0x1fff",
				"/pes_headers/status": "errors", "/pts_repetition/status": "ng",
				"/continuity_events/0/expected_cc": 5, "/programs/0/series/timestamp_events/0": present{},
			},
		},
		{
			name: "complete after recovered sync losses in M2TS", schema: analysisSchemaFile, path: fixedPath(syncLossFixtures[1].path), opts: analysis,
			want: map[string]any{
				"/status": "complete", "/sync/status": "recovered", "/sync/skipped_bytes": 1384, "/sync/max_skipped_bytes": 1000,
				"/sync/trailing_partial_packet_offset_bytes": 39400,
			},
		},
		{
			name: "incomplete after an unrecovered sync loss", schema: analysisSchemaFile, opts: analysis,
			path: synthetic(func() []byte { return syncTestStream(make([]byte, 50), 3) }),
			want: map[string]any{"/status": "incomplete", "/reason": "sync_lost", "/sync/status": "lost", "/programs/0/series": present{}},
		},
		{
			name: "complete with transport errors and scrambled packets in M2TS", schema: analysisSchemaFile, path: fixedPath(transportErrorsFixtures[1].path), opts: analysis,
			want: map[string]any{
				"/status": "complete", "/transport_errors/status": "data_excluded", "/transport_errors/packet_count": 6,
				"/scrambled_packets/excluded_packet_count": 5, "/scrambled_packets/first_offset_bytes": 7 * 192,
			},
		},
		{
			name: "complete with PES header errors and an excessive PTS interval in M2TS", schema: analysisSchemaFile, path: fixedPath(pesHealthFixtures[1].path), opts: analysis,
			want: map[string]any{
				"/status": "complete", "/pes_headers/first_error_offset_bytes": 27 * 192, "/pes_headers/unchecked_count": 0,
				"/pts_repetition/max_interval_start_offset_bytes": 98 * 192, "/pts_repetition/pids/0/status": "ng", "/pts_repetition/pids/4/reason": "no_pes",
			},
		},
		{
			name: "complete with PID reference findings in M2TS", schema: analysisSchemaFile, path: fixedPath(pidReferencesFixtures[1].path), opts: analysis,
			want: map[string]any{
				"/status": "complete", "/pid_references/referenced/pids/5/status": "absent",
				"/pid_references/referenced/pids/5/referenced_until_offset_bytes": 571 * 192, "/pid_references/unreferenced/pids/0/pid": "0x0300",
			},
		},
		{
			name: "complete with PCR discontinuities in M2TS", schema: analysisSchemaFile, path: fixedPath(pcrDiscontinuitiesFixtures[1].path), opts: analysis,
			want: map[string]any{
				"/status": "complete", "/programs/0/compliance/pcr_discontinuities/status": "ng",
				"/programs/0/compliance/pcr_discontinuities/items/2/first_offset_bytes": 202 * 192,
				"/programs/0/pcr_jitter/estimate/discontinuities/4/kind":                "pcr_pid_change",
			},
		},
		{
			name: "complete with PSI transitions", schema: analysisSchemaFile, path: fixedPath(psiVersionChange188Path), opts: analysis,
			want: map[string]any{"/status": "complete", "/psi_transitions/0": present{}},
		},
		{
			name: "complete with a program without PMT", schema: analysisSchemaFile, opts: analysis,
			path: synthetic(func() []byte {
				return syntheticStream([]syntheticProgram{
					{number: 1, pmtPid: 0x1000, pcrPid: 0x100, videoPid: 0x100, withPmt: true},
					{number: 2, pmtPid: 0x1001, pcrPid: 0x200, videoPid: 0x200},
				}, nil, -1)
			}),
			want: map[string]any{
				"/status": "complete", "/programs/1/status": "incomplete", "/programs/1/reason": "pmt_parse_failed",
				"/programs/1/error": present{}, "/programs/1/series": absent{}, "/programs/1/analysis_start_offset_bytes": absent{},
			},
		},
		{
			name: "incomplete without any PMT", schema: analysisSchemaFile, opts: analysis,
			path: synthetic(func() []byte {
				data := syntheticStream([]syntheticProgram{{number: 1, pmtPid: 0x1000, pcrPid: 0x100, videoPid: 0x100}}, nil, -1)
				for range 3 {
					data = append(data, buildStuffingPacket()...)
				}
				return data
			}),
			want: map[string]any{
				"/status": "incomplete", "/reason": "no_program_analyzed", "/analysis_start_offset_bytes": absent{},
				"/continuity_counter": absent{}, "/transport_errors": absent{}, "/scrambled_packets": absent{}, "/pid_references": absent{}, "/pes_headers": absent{}, "/pts_repetition": absent{},
			},
		},
		{
			name: "failed on incompatible options", schema: analysisSchemaFile, path: fixedPath(twoPrograms188Path),
			opts: options.Options{Output: "analysis", Program: 1}, wantErr: true,
			want: map[string]any{"/status": "failed", "/reason": "incompatible_options", "/error": present{}, "/programs": []any{}},
		},
		{
			name: "failed to open the input", schema: analysisSchemaFile, path: fixedPath("/nonexistent/input.ts"), opts: analysis, wantErr: true,
			want: map[string]any{"/status": "failed", "/reason": "input_open_failed", "/error": present{}},
		},
	}
}

func packetsSchemaCases() []schemaCase {
	page := func(o options.Options) options.Options { o.Output = "packets"; return o }
	detail := func(o options.Options) options.Options { o.Output = "packet"; return o }
	return []schemaCase{
		{
			name: "page from the start", schema: packetsSchemaFile, path: fixedPath(sample188Path), opts: page(options.Options{Count: 5}),
			want: map[string]any{
				"/status": "complete", "/output": "packets", "/input/offset_bytes": 0, "/first_offset_bytes": 0,
				"/packets/4": present{}, "/next_offset_bytes": 5 * 188, "/end_of_file": false, "/warnings": []any{},
			},
		},
		{
			name: "page with a PCR in M2TS", schema: packetsSchemaFile, path: fixedPath(twoPrograms192Path), opts: page(options.Options{Count: 200}),
			want: map[string]any{"/status": "complete", "/input/packet_size_bytes": 192},
		},
		{
			name: "page with invalid and truncated packets", schema: packetsSchemaFile, opts: page(options.Options{Count: 100}),
			path: func(t *testing.T) string {
				data := bytes.Repeat(buildStuffingPacket(), 2)
				data = append(data, make([]byte, 188)...)
				data = append(data, 0x47, 0x1F)
				return writeFixture(t, "packets.ts", data)
			},
			want: map[string]any{
				"/status": "complete", "/packets/2/valid": false, "/packets/2/pid": absent{}, "/end_of_file": true,
				"/next_offset_bytes": absent{}, "/warnings/0/message": present{},
			},
		},
		{
			name: "page of an empty file", schema: packetsSchemaFile, opts: page(options.Options{Count: 10}),
			path: func(t *testing.T) string { return writeFixture(t, "empty.ts", nil) },
			want: map[string]any{
				"/status": "complete", "/packets": []any{}, "/total_packets": 0, "/input/file_size_bytes": 0,
				"/first_offset_bytes": absent{}, "/end_of_file": true,
			},
		},
		{
			name: "page failed on an invalid count", schema: packetsSchemaFile, path: fixedPath(sample188Path), opts: page(options.Options{Count: 0}), wantErr: true,
			want: map[string]any{"/status": "failed", "/reason": "analysis_failed", "/error": present{}, "/packets": []any{}},
		},
		{
			name: "page failed to open the input", schema: packetsSchemaFile, path: fixedPath("/no/such/file.ts"), opts: page(options.Options{Count: 1}), wantErr: true,
			want: map[string]any{"/status": "failed", "/reason": "input_open_failed", "/error": present{}},
		},
		{
			name: "packet detail of a PAT", schema: packetsSchemaFile, path: fixedPath(sample188Path), opts: detail(options.Options{Offset: 0}),
			want: map[string]any{
				"/status": "complete", "/output": "packet", "/warnings": []any{}, "/packet/offset_bytes": 0,
				"/packet/layers/0": present{}, "/unit/kind": "psi",
			},
		},
		{
			name: "packet detail of a PES start with a PCR", schema: packetsSchemaFile, path: fixedPath(twoPrograms188Path), opts: detail(options.Options{Offset: 29892}),
			want: map[string]any{"/status": "complete", "/packet/offset_bytes": 29892, "/packet/layers/2": present{}, "/unit": present{}},
		},
		{
			name: "packet detail of a truncated PMT", schema: packetsSchemaFile, opts: detail(options.Options{Offset: 0}),
			path: func(t *testing.T) string {
				streams := make([][2]uint16, 40)
				for i := range streams {
					streams[i] = [2]uint16{0x0F, uint16(0x200 + i)}
				}
				var data []byte
				data = append(data, buildTsPacket(0x1000, true, 0, pmtSection(1, 0x100, streams...))...)
				data = append(data, buildStuffingPacket()...)
				data = append(data, buildStuffingPacket()...)
				return writeFixture(t, "pmt.ts", data)
			},
			want: map[string]any{"/status": "complete", "/packet/layers/2/truncated": true},
		},
		{
			name: "packet detail failed outside the file", schema: packetsSchemaFile, path: fixedPath(sample188Path),
			opts: detail(options.Options{Offset: 1 << 40}), wantErr: true,
			want: map[string]any{"/status": "failed", "/reason": "analysis_failed", "/error": present{}, "/packet": absent{}},
		},
	}
}

// TestJSONOutputsMatchSchemas validates every output kind and lifecycle state,
// as written by the CLI's own JSON output functions, against the checked-in
// schemas.
func TestJSONOutputsMatchSchemas(t *testing.T) {
	schemas := loadSchemas(t)
	var cases []schemaCase
	cases = append(cases, summarySchemaCases()...)
	cases = append(cases, analysisSchemaCases()...)
	cases = append(cases, packetsSchemaCases()...)
	for _, c := range cases {
		t.Run(strings.TrimSuffix(c.schema, ".json")+"/"+c.name, func(t *testing.T) {
			var out bytes.Buffer
			err := c.emit(t, &out)
			if (err != nil) != c.wantErr {
				t.Fatalf("error = %v, want error: %v", err, c.wantErr)
			}
			if err := validateJSON(schemas[c.schema], out.Bytes()); err != nil {
				t.Fatalf("output does not match %s:\n%v\noutput:\n%s", c.schema, err, out.Bytes())
			}
			checkPointers(t, out.Bytes(), c.want)
		})
	}
}

// driftedReport stands for a Go change that emits schema_version as a number:
// the field shadows AnalysisReport's in encoding/json.
type driftedReport struct {
	*AnalysisReport
	SchemaVersion float64 `json:"schema_version"`
}

// TestJSONSchemaValidationRejectsMismatches proves the validation above fails
// when emitted fields drift from the schema, and when a schema's $id no longer
// matches where it is published (the relative $refs between the schemas then
// stop resolving to the checked-in files).
func TestJSONSchemaValidationRejectsMismatches(t *testing.T) {
	schemas := loadSchemas(t)

	// One real document per schema, each valid as emitted.
	emitted := map[string][]byte{}
	for schema, c := range map[string]schemaCase{
		summarySchemaFile:  {path: fixedPath(sample188Path), opts: options.Options{DumpPcrJitter: true, DumpBitrate: true}},
		analysisSchemaFile: {path: fixedPath(sample188Path), opts: options.Options{Output: "analysis"}},
		packetsSchemaFile:  {path: fixedPath(sample188Path), opts: options.Options{Output: "packets", Count: 2}},
	} {
		var out bytes.Buffer
		if err := c.emit(t, &out); err != nil {
			t.Fatal(err)
		}
		if err := validateJSON(schemas[schema], out.Bytes()); err != nil {
			t.Fatalf("%s baseline: %v", schema, err)
		}
		emitted[schema] = out.Bytes()
	}

	// A Go struct change that alters an emitted type, written by the real writer.
	var report AnalysisReport
	if err := json.Unmarshal(emitted[summarySchemaFile], &report); err != nil {
		t.Fatal(err)
	}
	var drifted bytes.Buffer
	if err := writeJSONReport(&drifted, driftedReport{AnalysisReport: &report, SchemaVersion: 2}); err != nil {
		t.Fatal(err)
	}
	if err := validateJSON(schemas[summarySchemaFile], drifted.Bytes()); err == nil || !strings.Contains(err.Error(), "/schema_version") {
		t.Errorf("drifted schema_version type: err = %v", err)
	}

	// Renamed (rename != "") or retyped fields in an emitted document. The
	// error names the instance location and the failing rule.
	for _, tc := range []struct {
		schema, pointer string
		value           any
		rename          string
		want            string
	}{
		{schema: summarySchemaFile, pointer: "/programs", rename: "program_list", want: "missing property 'programs'"},
		{schema: summarySchemaFile, pointer: "/input/offset_bytes", value: "0", want: "at '/input/offset_bytes' [S#/$defs/input/properties/offset_bytes/type]: got string, want integer"},
		{schema: summarySchemaFile, pointer: "/input/packet_size_bytes", value: 204, want: "/input/packet_size_bytes"},
		{schema: summarySchemaFile, pointer: "/bitrate/null_pid/pid", value: 8191, want: "/bitrate/null_pid/pid"},
		{schema: summarySchemaFile, pointer: "/pcr_jitter/estimate/observations", value: -1, want: "/pcr_jitter/estimate/observations"},
		{schema: summarySchemaFile, pointer: "/pcr_jitter/delivery", rename: "deliveries", want: "missing property 'delivery'"},
		{schema: analysisSchemaFile, pointer: "/programs/0/pcr_jitter/delivery/assessment", value: 1, want: "/programs/0/pcr_jitter/delivery/assessment"},
		{schema: analysisSchemaFile, pointer: "/output", value: "summary", want: "/output"},
		{schema: packetsSchemaFile, pointer: "/packets/1/offset_bytes", value: "188", want: "/packets/1/offset_bytes"},
		{schema: packetsSchemaFile, pointer: "/total_packets", rename: "packet_total", want: "missing property 'total_packets'"},
	} {
		var doc any
		if err := json.Unmarshal(emitted[tc.schema], &doc); err != nil {
			t.Fatal(err)
		}
		cut := strings.LastIndex(tc.pointer, "/")
		parent, ok := lookupPointer(doc, tc.pointer[:cut])
		key := tc.pointer[cut+1:]
		if !ok {
			t.Fatalf("%s: no %s", tc.schema, tc.pointer)
		}
		if tc.rename != "" {
			object := parent.(map[string]any)
			object[tc.rename] = object[key]
			delete(object, key)
		} else if list, isList := parent.([]any); isList {
			i, _ := strconv.Atoi(key)
			list[i] = tc.value
		} else {
			parent.(map[string]any)[key] = tc.value
		}
		data, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateJSON(schemas[tc.schema], data); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %s: err = %v, want a failure mentioning %q", tc.schema, tc.pointer, err, tc.want)
		}
	}

	// A schema identifier that is not its published URL, such as a GitHub blob
	// URL, makes the cross-file $refs unresolvable offline.
	site := docsSiteURL(t)
	for _, name := range []string{analysisSchemaFile, packetsSchemaFile} {
		docs := readSchemaDocs(t)
		docs[name].(map[string]any)["$id"] = "https://github.com/small-teton/mpeg-ts-analyzer/blob/master/docs/" + name
		if _, err := compileSchemas(site, docs); err == nil || !strings.Contains(err.Error(), "refusing to load") {
			t.Errorf("%s with a blob $id: err = %v", name, err)
		}
	}
}
