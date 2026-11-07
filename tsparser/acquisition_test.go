package tsparser

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// asPacketSize turns concatenated 188-byte packets into packets of size:
// unchanged, with a zero TP_extra_header (192) or with test trailers (204).
func asPacketSize(data []byte, size int) []byte {
	switch size {
	case m2tsPacketSize:
		var out []byte
		for i := 0; i+tsPayloadSize <= len(data); i += tsPayloadSize {
			out = append(out, wrapM2TS(data[i:i+tsPayloadSize])...)
		}
		return out
	case trailerPacketSize:
		return withTestTrailers(data)
	}
	return data
}

func nullPackets(n int) []byte { return bytes.Repeat(nullTestPacket(), n) }

func acquisitionPatPacket() []byte {
	return buildTsPacket(patPidValue, true, 0, patSection([2]uint16{1, 0x1000}))
}

// runAcquisition runs the summary JSON on data and returns the report, which
// must be a failed acquisition.
func runAcquisition(t *testing.T, data []byte, opts options.Options) AnalysisReport {
	t.Helper()
	path := writeFixture(t, "acquisition.ts", data)
	opts.Format = "json"
	report, err := decodeJSONReport(t, func() error { return ParseTsFile(path, opts) })
	var acquisitionErr *acquisitionError
	if !errors.As(err, &acquisitionErr) || report.Status != "failed" || report.Reason != acquisitionErr.reason || report.Error != err.Error() || report.Acquisition == nil {
		t.Fatalf("err = %v, report %s", err, toJSON(t, report))
	}
	if report.Sync != nil || len(report.Programs) != 0 || report.PSIHealth != nil || report.TransportErrors != nil {
		t.Errorf("a failed acquisition reported analysis results: %s", toJSON(t, report))
	}
	return report
}

func TestAcquisitionSyncFailures(t *testing.T) {
	statuses := func(r AnalysisReport) string {
		var s []string
		for _, l := range r.Acquisition.Alignment.Layouts {
			s = append(s, l.Status)
		}
		return strings.Join(s, ",")
	}
	cases := []struct {
		name, reason, alignment, layouts, message string
		data                                      []byte
		opts                                      options.Options
		start, end                                int64
	}{
		{
			name: "empty input", data: nil, reason: "insufficient_data_for_sync", alignment: "insufficient_data",
			layouts: "insufficient_data,insufficient_data,insufficient_data", message: "holds only 0 bytes",
		},
		{
			name: "two packets", data: nullPackets(2), reason: "insufficient_data_for_sync", alignment: "insufficient_data",
			layouts: "insufficient_data,insufficient_data,insufficient_data", end: 376,
			message: "188-byte packets need 377 bytes (3 sync bytes one packet apart), 192-byte packets need 389 bytes (3 sync bytes one packet apart), 204-byte packets need 817 bytes (5 sync bytes one packet apart); to inspect a short fragment packet by packet, without program analysis, use --format json --output packets with --packet-size",
		},
		{
			name: "too short for 204 bytes only", data: make([]byte, 500), reason: "insufficient_data_for_sync", alignment: "insufficient_data",
			layouts: "not_found,not_found,insufficient_data", end: 500,
			message: "204-byte packets need 817 bytes (5 sync bytes one packet apart); no 188-byte or 192-byte alignment was found",
		},
		{
			name: "offset past the end", data: nullPackets(10), opts: options.Options{Offset: 5000}, reason: "insufficient_data_for_sync",
			alignment: "insufficient_data", layouts: "insufficient_data,insufficient_data,insufficient_data", start: 5000, end: 5000,
			message: "the range from 0x00001388 to 0x00001388 holds only 0 bytes",
		},
		{
			name: "long unaligned input", data: bytes.Repeat([]byte{0x47, 0x00, 0x00, 0x00, 0x11}, 4000), reason: "sync_not_acquired",
			alignment: "not_acquired", layouts: "not_found,not_found,not_found", end: 20000,
			message: "no TS synchronization found: no 188-byte, 192-byte or 204-byte packet alignment (three sync bytes one packet apart, five for 204 bytes) in the 20000 bytes from 0x00000000 to 0x00004e20; the range may not hold MPEG-2 TS packets of a supported size, or may be damaged",
		},
		{
			name: "limited unaligned range", data: make([]byte, 5000), opts: options.Options{Offset: 100, Limit: 1000}, reason: "sync_not_acquired",
			alignment: "not_acquired", layouts: "not_found,not_found,not_found", start: 100, end: 1100, message: "in the 1000 bytes from 0x00000064 to 0x0000044c",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := runAcquisition(t, c.data, c.opts)
			a := r.Acquisition
			if r.Reason != c.reason || a.Alignment.Status != c.alignment || statuses(r) != c.layouts || !strings.Contains(r.Error, c.message) {
				t.Errorf("report %s", toJSON(t, r))
			}
			if a.RangeStartOffsetBytes != c.start || a.RangeEndOffsetBytes != c.end || a.ScannedBytes != c.end-c.start || a.PAT != nil ||
				r.Input.PacketSizeBytes != 0 || a.Alignment.PacketSizeBytes != 0 || a.Alignment.AlignedPacketCount != 0 || a.Alignment.FirstAlignedOffsetBytes != nil {
				t.Errorf("evidence %s", toJSON(t, a))
			}
			if l := a.Alignment.Layouts[2]; l.PacketSizeBytes != 204 || l.RequiredSyncBytes != 5 || l.RequiredBytes != 817 {
				t.Errorf("204-byte layout %+v", l)
			}
		})
	}
}

func TestAcquisitionPATFailures(t *testing.T) {
	pat := acquisitionPatPacket()
	badCRC := bytes.Clone(pat)
	badCRC[5+8+4] ^= 0xFF
	tei := bytes.Clone(pat)
	tei[1] |= 0x80
	middle := buildTsPacket(patPidValue, false, 0, []byte{1, 2, 3})
	next := bytes.Clone(pat)
	next[5+5] = 0xC0 // current_next_indicator 0
	c := crc32(next[5 : 5+12])
	next[5+12], next[5+13], next[5+14], next[5+15] = byte(c>>24), byte(c>>16), byte(c>>8), byte(c)
	wrongTable := buildTsPacket(patPidValue, true, 0, psiSection(0x02, 1, []byte{0xE1, 0x00, 0xF0, 0x00}))
	badPointer := bytes.Clone(pat)
	badPointer[3], badPointer[4] = 0x11, 0xFF
	programs := make([][2]uint16, 50)
	for i := range programs {
		programs[i] = [2]uint16{uint16(i + 1), uint16(0x1000 + i)}
	}
	long := buildTsPacket(patPidValue, true, 0, patSection(programs...)[:183])

	cases := []struct {
		name, reason, message string
		data                  []byte
		build                 func(size int) []byte
		opts                  options.Options
		aligned, first        int64
		packets, starts       int64
		check                 func(*ReportAcquisitionPAT) bool
	}{
		{
			name: "no PID 0", data: nullPackets(12), reason: "pat_not_observed", aligned: 12,
			message: "PAT not detected: no complete PAT after scanning 12 aligned %d-byte packets (%d bytes) in the range from 0x00000000 to %s; no packet is on PID 0x0000. The range may be a partial capture or a PSI/PES fragment, or may end before the next PAT repetition; program analysis needs a PAT and stops here",
		},
		{
			name: "invalid CRC_32", data: append(bytes.Clone(badCRC), nullPackets(9)...), reason: "pat_not_acquired", aligned: 10, packets: 1, starts: 1,
			message: "; 1 packet on PID 0x0000, 1 starting a section; problems (sections or packets): 1 with an invalid CRC_32. The PAT may be damaged or cut off by the range; program analysis needs a PAT and stops here",
			check:   func(p *ReportAcquisitionPAT) bool { return p.CRCErrorCount == 1 && *p.FirstPacketOffsetBytes == 0 },
		},
		{
			name: "transport error", data: append(nullPackets(2), append(bytes.Clone(tei), nullPackets(7)...)...), reason: "pat_not_acquired", aligned: 10, packets: 1, starts: 1,
			message: "1 with transport_error_indicator or a scrambled payload",
			check:   func(p *ReportAcquisitionPAT) bool { return p.UnusablePacketCount == 1 },
		},
		{
			name: "only the middle of a section", data: append(bytes.Clone(middle), nullPackets(9)...), reason: "pat_not_acquired", aligned: 10, packets: 1,
			message: "1 packet on PID 0x0000, 0 starting a section. The range may hold only the middle of a PAT section; program analysis",
			check:   func(p *ReportAcquisitionPAT) bool { return p.IncompleteCount == 0 },
		},
		{
			name: "a section cut off by the end of the range", data: append(nullPackets(9), long...), reason: "pat_not_acquired", aligned: 10, packets: 1, starts: 1,
			message: "1 incomplete",
			check:   func(p *ReportAcquisitionPAT) bool { return p.IncompleteCount == 1 },
		},
		{
			name: "a complete PAT too close to the end", data: append(nullPackets(9), pat...), reason: "pat_not_acquired", aligned: 10, packets: 1, starts: 1,
			message: "1 complete PAT(s) were assembled, but PAT discovery could not start from it",
			check:   func(p *ReportAcquisitionPAT) bool { return p.CompleteTableCount == 1 },
		},
		{
			name: "a next PAT only", data: append(bytes.Clone(next), nullPackets(9)...), reason: "pat_not_acquired", aligned: 10, packets: 1, starts: 1,
			message: ". No usable PAT section was assembled; program analysis",
		},
		{
			name: "wrong table and malformed section", data: append(append(bytes.Clone(wrongTable), badPointer...), nullPackets(8)...),
			reason: "pat_not_acquired", aligned: 10, packets: 2, starts: 2, message: "1 malformed, 1 with a table_id other than 0x00",
			check: func(p *ReportAcquisitionPAT) bool { return p.WrongTableIDCount == 1 && p.MalformedSectionCount == 1 },
		},
		{
			name: "limited range", data: append(nullPackets(20), pat...), opts: options.Options{Offset: 100, Limit: 188 * 6}, reason: "pat_not_observed", aligned: 5, first: 188,
			message: "a wider --offset/--limit range may include one; program analysis",
		},
		{
			name: "resynchronized after garbage", reason: "pat_not_observed", aligned: 10,
			build: func(size int) []byte {
				return append(append(asPacketSize(nullPackets(5), size), bytes.Repeat([]byte{0, 0x47}, 30)...), asPacketSize(nullPackets(5), size)...)
			},
		},
		{
			name: "an aligned packet that cannot be parsed", data: append(append(nullPackets(4), 0x47, 0x1F, 0xFF, 0x00), append(make([]byte, 184), nullPackets(4)...)...), reason: "pat_not_observed", aligned: 9,
		},
	}
	for _, size := range supportedPacketSizes {
		for _, c := range cases {
			t.Run(strings.Join([]string{c.name, itoa(size)}, "/"), func(t *testing.T) {
				opts := c.opts
				if opts.Offset > 0 {
					opts.Offset = opts.Offset * int64(size) / tsPayloadSize
					opts.Limit = opts.Limit * int64(size) / tsPayloadSize
				}
				data := asPacketSize(c.data, size)
				if c.build != nil {
					data = c.build(size)
				}
				r := runAcquisition(t, data, opts)
				a := r.Acquisition
				if r.Reason != c.reason || a.Alignment.Status != "acquired" || a.Alignment.PacketSizeBytes != size || r.Input.PacketSizeBytes != size ||
					a.Alignment.AlignedPacketCount != c.aligned || *a.Alignment.FirstAlignedOffsetBytes != c.first*int64(size)/tsPayloadSize {
					t.Errorf("report %s", toJSON(t, r))
				}
				p := a.PAT
				wantStatus := map[string]string{"pat_not_observed": "not_observed", "pat_not_acquired": "not_acquired"}[c.reason]
				if p == nil || p.Status != wantStatus || p.PacketCount != c.packets || p.SectionStartCount != c.starts || (c.packets == 0) != (p.FirstPacketOffsetBytes == nil) {
					t.Fatalf("pat %s", toJSON(t, p))
				}
				if c.check != nil && !c.check(p) {
					t.Errorf("pat %s", toJSON(t, p))
				}
				if p.FirstPacketOffsetBytes != nil && c.check != nil && strings.Contains(c.name, "transport") && *p.FirstPacketOffsetBytes != 2*int64(size) {
					t.Errorf("first PAT packet at %d", *p.FirstPacketOffsetBytes)
				}
				message := c.message
				if strings.Contains(message, "%d") {
					message = strings.NewReplacer("%d-byte", itoa(size)+"-byte", "(%d bytes)", "("+itoa(12*size)+" bytes)", "%s", hex8(int64(12*size))).Replace(message)
				}
				if !strings.Contains(r.Error, message) {
					t.Errorf("error %q does not contain %q", r.Error, message)
				}
			})
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func hex8(n int64) string { return fmt.Sprintf("0x%08x", n) }

func TestAcquisitionInRangeAnalysis(t *testing.T) {
	path := writeFixture(t, "fragment.ts", nullPackets(12))
	report, err := decodeRangeAnalysis(t, path, options.Options{})
	if err == nil || report.Status != "failed" || report.Reason != "pat_not_observed" || report.Acquisition == nil ||
		report.Acquisition.Alignment.AlignedPacketCount != 12 || report.Sync != nil || report.PSIHealth != nil {
		t.Errorf("err = %v, report %s", err, toJSON(t, report))
	}
}

func TestAcquisitionTextOutput(t *testing.T) {
	path := writeFixture(t, "short.ts", nullPackets(1))
	var err error
	out := captureStdout(t, func() { err = ParseTsFile(path, options.Options{}) })
	if !strings.Contains(out, "Packet size: 188 bytes (assumed: no packet alignment in the first read)") || err == nil || !strings.HasPrefix(err.Error(), "no TS synchronization found: the range") {
		t.Errorf("err = %v, output %q", err, out)
	}
	path = writeFixture(t, "fragment.ts", nullPackets(3))
	out = captureStdout(t, func() { err = ParseTsFile(path, options.Options{}) })
	if !strings.Contains(out, "Packet size: 188 bytes\n") || err == nil || !strings.HasPrefix(err.Error(), "PAT not detected: no complete PAT after scanning 3 aligned 188-byte packets") {
		t.Errorf("err = %v, output %q", err, out)
	}
}

// seekableFailingReader reads data, then fails; Seek always succeeds.
type seekableFailingReader struct{ failingReader }

func (*seekableFailingReader) Seek(_ int64, whence int) (int64, error) {
	if whence == io.SeekEnd {
		return 10000, nil
	}
	return 0, nil
}

func TestAcquisitionReadErrors(t *testing.T) {
	failure := errors.New("disk failure")
	// While searching for the alignment, then while aligned.
	for _, data := range [][]byte{nullPackets(1), nullPackets(3)} {
		r := &seekableFailingReader{failingReader{data: data, err: failure}}
		if _, err := observeAcquisition(r, 0, 10000, tsPayloadSize); !errors.Is(err, failure) {
			t.Errorf("%d bytes: err = %v", len(data), err)
		}
	}
	if _, err := observeAcquisition(seekFailingReader{bytes.NewReader(nil)}, 0, 10, tsPayloadSize); err == nil || !strings.Contains(err.Error(), "seek failure") {
		t.Errorf("seek: err = %v", err)
	}
	if err := acquisitionFailure(seekFailingReader{bytes.NewReader(nil)}, options.Options{}, &alignmentScan{}, tsPayloadSize, 0); err == nil || !strings.Contains(err.Error(), "seek failure") {
		t.Errorf("size: err = %v", err)
	}
	// The evidence pass fails after the size is known.
	r := &seekableFailingReader{failingReader{data: nil, err: failure}}
	scan := &alignmentScan{found: [3]bool{true, false, false}}
	if err := acquisitionFailure(r, options.Options{}, scan, tsPayloadSize, 0); !errors.Is(err, failure) {
		t.Errorf("evidence: err = %v", err)
	}
}
