package tsparser

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// parsedPacket parses a 188-byte packet at pos, with transport_error_indicator
// set when tei is and transport_scrambling_control set to control.
func parsedPacket(t *testing.T, data []byte, pos int64, tei bool, control byte) *TsPacket {
	t.Helper()
	data = append([]byte(nil), data...)
	if tei {
		data[1] |= 0x80
	}
	data[3] = data[3]&0x3F | control<<6
	p := NewTsPacket()
	p.Initialize(pos, options.Options{})
	p.Append(data)
	if err := p.Parse(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTransportTrustExclusion(t *testing.T) {
	// Program 1 from offset 1880: video 0x0100, PCR on 0x0050 (no payload
	// read), PMT 0x1000. A stopped program 2 had video 0x0200 and PMT 0x1001.
	analyzer := newProgramAnalyzer(options.Options{}, 1880, 1, 0x1000, 0x0050, []ProgramInfo{{elementaryPid: 0x0100, streamType: 0x02}}, false)
	stopped := newProgramAnalyzer(options.Options{}, 0, 2, 0x1001, 0x0200, []ProgramInfo{{elementaryPid: 0x0200, streamType: 0x02}}, false)
	stopped.stop(188)
	unset := newProgramAnalyzer(options.Options{}, 0, 3, 0x1002, 0, nil, false)
	psi := &psiTracker{programs: []*trackedProgram{
		{pmtPid: 0x1000, analyzer: analyzer},
		{pmtPid: 0x1001, analyzer: stopped, removed: true},
	}}
	analyzers := []*programAnalyzer{analyzer, stopped, unset}
	payload := func(pid uint16) []byte { return buildTsPacket(pid, true, 0, []byte{0x00, 0x00, 0x01, 0xE0}) }
	afOnly := buildPcrPacket

	for _, tt := range []struct {
		name          string
		data          []byte
		pos           int64
		tei           bool
		control       byte
		psi           *psiTracker
		wantTei       int // -1: not counted, 0: counted, 1: counted and excluded
		wantScrambled int
	}{
		{"clean video", payload(0x0100), 1880, false, 0, psi, -1, -1},
		{"TEI video before the program starts", payload(0x0100), 1692, true, 0, psi, 0, -1},
		{"TEI video", payload(0x0100), 1880, true, 0, psi, 1, -1},
		{"TEI on the stopped program's video", payload(0x0200), 1880, true, 0, psi, 0, -1},
		{"TEI on the PCR_PID", afOnly(0x0050, 27_000_000), 1880, true, 0, psi, 1, -1},
		{"TEI on PID 0 with PCR_PID 0 unset", payload(0x0000), 1880, true, 0, nil, 0, -1},
		{"TEI PAT", payload(0x0000), 1880, true, 0, psi, 1, -1},
		{"TEI PMT", payload(0x1000), 1880, true, 0, psi, 1, -1},
		{"TEI PMT before the program starts", payload(0x1000), 1692, true, 0, psi, 0, -1},
		{"TEI on the removed program's PMT", payload(0x1001), 1880, true, 0, psi, 0, -1},
		{"scrambled video", payload(0x0100), 1880, false, 2, psi, -1, 1},
		{"scrambled video without payload", afOnly(0x0100, 27_000_000), 1880, false, 3, psi, -1, 0},
		{"scrambled PCR_PID with payload", payload(0x0050), 1880, false, 2, psi, -1, 0},
		{"scrambled PMT", payload(0x1000), 1880, false, 1, psi, -1, 1},
		{"TEI and scrambled video", payload(0x0100), 1880, true, 2, psi, 1, 1},
		{"TEI and scrambled null packet", buildStuffingPacket(), 1880, true, 3, psi, 0, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTransportTrustSummary()
			s.observe(parsedPacket(t, tt.data, tt.pos, tt.tei, tt.control), tt.pos, analyzers, tt.psi)
			tei, scrambled := reportTransportTrust(s)
			for _, c := range []struct {
				name string
				got  *ReportAffectedPackets
				want int
			}{{"transport_errors", tei, tt.wantTei}, {"scrambled_packets", scrambled, tt.wantScrambled}} {
				count, excluded := c.got.PacketCount, c.got.ExcludedPacketCount
				switch c.want {
				case -1:
					if count != 0 || c.got.Status != "ok" || c.got.FirstOffsetBytes != nil {
						t.Errorf("%s: %s", c.name, toJSON(t, c.got))
					}
				case 0:
					if count != 1 || excluded != 0 || c.got.Status != "affected" || *c.got.FirstOffsetBytes != tt.pos {
						t.Errorf("%s: %s", c.name, toJSON(t, c.got))
					}
				default:
					if count != 1 || excluded != 1 || c.got.Status != "data_excluded" {
						t.Errorf("%s: %s", c.name, toJSON(t, c.got))
					}
				}
			}
		})
	}
}

func TestProgramAnalyzerTrust(t *testing.T) {
	const video, pcrPid = 0x0100, 0x0050
	newAnalyzer := func() *programAnalyzer {
		return newProgramAnalyzer(options.Options{}, 0, 1, 0x1000, pcrPid, []ProgramInfo{{elementaryPid: video, streamType: 0x02}}, true)
	}
	pesStart := buildTsPacket(video, true, 0, []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00})[0:188]
	// buildTsPacket writes a pointer_field; drop it so the payload is the PES.
	copy(pesStart[4:], pesStart[5:])

	t.Run("TEI before any PES", func(t *testing.T) {
		a := newAnalyzer()
		a.observe(parsedPacket(t, pesStart, 0, true, 0), 0, false)
		if a.pesMap[video] != nil {
			t.Error("a PES started from a packet with a transport error")
		}
		a.observe(parsedPacket(t, pesStart, 188, false, 0), 188, false)
		if a.pesMap[video] == nil || a.pesMap[video].pos != 188 {
			t.Error("the next unaffected start did not start a PES")
		}
	})
	t.Run("scrambled packet without payload keeps the PES", func(t *testing.T) {
		a := newAnalyzer()
		a.observe(parsedPacket(t, pesStart, 0, false, 0), 0, false)
		a.observe(parsedPacket(t, buildPcrPacket(video, 27_000_000), 188, false, 2), 188, false)
		if a.pesMap[video] == nil || a.pesMap[video].pos != 0 {
			t.Error("a scrambled packet without payload ended the PES")
		}
		a.observe(parsedPacket(t, pesStart, 376, false, 3), 376, false)
		if a.pesMap[video] != nil || len(a.series.pes) != 1 || a.series.pes[0].pos != 0 {
			t.Errorf("a scrambled payload did not end the PES in progress: %+v", a.series.pes)
		}
		// Continuation packets are ignored until the next unaffected start.
		a.observe(parsedPacket(t, buildTsPacket(video, false, 3, nil), 564, false, 0), 564, false)
		if a.pesMap[video] != nil {
			t.Error("a continuation packet started a PES")
		}
	})
	t.Run("PCR", func(t *testing.T) {
		a := newAnalyzer()
		a.observe(parsedPacket(t, buildPcrPacket(pcrPid, 27_000_000), 0, true, 0), 0, false)
		if a.clock.valid || len(a.series.pcr) != 0 {
			t.Error("the PCR of a packet with a transport error was used")
		}
		a.observe(parsedPacket(t, buildPcrPacket(pcrPid, 27_000_000), 188, false, 2), 188, false)
		if !a.clock.valid || a.clock.pcr != 27_000_000 {
			t.Error("the clear PCR of a scrambled packet was not used")
		}
	})
}

func TestPsiSectionReaderScrambled(t *testing.T) {
	// A section longer than one packet (section_length 0x0FF).
	pat := []byte{0x00, 0xB0, 0xFF, 0x00, 0x01, 0xC1, 0x00, 0x00}
	var r psiSectionReader
	// The first part of a section, then a scrambled packet: the partial
	// section is discarded.
	if sections, _ := r.push(parsedPacket(t, buildTsPacket(0, true, 0, pat), 0, false, 0), 0); len(sections) != 0 || !r.collecting {
		t.Fatal("the section did not span packets")
	}
	sections, problems := r.push(parsedPacket(t, buildTsPacket(0, false, 1, nil), 188, false, 2), 188)
	if len(sections) != 0 || len(problems) != 1 || !strings.Contains(problems[0], "transport_scrambling_control set") || r.collecting {
		t.Errorf("sections %v problems %v collecting %v", sections, problems, r.collecting)
	}
	// A scrambled packet without payload changes nothing.
	r.collecting, r.buf = true, []byte{0x00}
	if sections, problems := r.push(parsedPacket(t, buildPcrPacket(0, 0), 376, false, 2), 376); len(sections) != 0 || len(problems) != 0 || !r.collecting {
		t.Errorf("sections %v problems %v", sections, problems)
	}
}

// seekFailReader fails every read after its failAfter-th Seek.
type seekFailReader struct {
	*bytes.Reader
	seeks, failAfter int
}

func (r *seekFailReader) Seek(offset int64, whence int) (int64, error) {
	r.seeks++
	return r.Reader.Seek(offset, whence)
}

func (r *seekFailReader) Read(p []byte) (int, error) {
	if r.seeks >= r.failAfter {
		return 0, errors.New("mock read error")
	}
	return r.Reader.Read(p)
}

// A read error while counting the packets read during the PAT and PMT
// acquisition fails the analysis.
func TestTransportTrustLeadReadError(t *testing.T) {
	data, err := os.ReadFile(transportErrorsFixtures[0].path)
	if err != nil {
		t.Fatal(err)
	}
	// Seek 1 goes to the PAT, seek 2 to the PMT search, seek 3 back to the
	// start of the range.
	reader := &seekFailReader{Reader: bytes.NewReader(data), failAfter: 3}
	captureStdout(t, func() { err = parseTsReader(reader, options.Options{}) })
	if err == nil || err.Error() != "file read error: mock read error" || reader.seeks != 3 {
		t.Errorf("err = %v after %d seeks", err, reader.seeks)
	}
}

// Seek 4 returns to the start of the analysis after counting the lead.
func TestTransportTrustLeadSeekError(t *testing.T) {
	data, err := os.ReadFile(transportErrorsFixtures[0].path)
	if err != nil {
		t.Fatal(err)
	}
	reader := &errReadSeeker{data: data, failAt: -1, seekFailAt: 4}
	captureStdout(t, func() { err = parseTsReader(reader, options.Options{}) })
	if err == nil || err.Error() != "file seek error: mock seek error" {
		t.Errorf("err = %v", err)
	}
}

// A PAT or PMT that only arrives in affected packets is never acquired, so
// the analysis does not report a complete result built from it.
func TestTransportTrustRequiredPsi(t *testing.T) {
	clean, err := os.ReadFile(generateTransportErrors(t, "-clean"))
	if err != nil {
		t.Fatal(err)
	}
	mark := func(pid uint16, tei bool, control byte) string {
		data := append([]byte(nil), clean...)
		for i := 0; i+188 <= len(data); i += 188 {
			if uint16(data[i+1]&0x1F)<<8|uint16(data[i+2]) == pid {
				if tei {
					data[i+1] |= 0x80
				}
				data[i+3] |= control << 6
			}
		}
		return writeFixture(t, "affected.ts", data)
	}

	report, err := decodeJSONReport(t, func() error { return ParseTsFile(mark(0x1000, false, 2), options.Options{Format: "json"}) })
	if err != nil || report.Status != "incomplete" || report.TransportErrors != nil || report.Compliance != nil || len(report.Warnings) == 0 ||
		!strings.Contains(report.Warnings[0].Message, "transport_scrambling_control set") {
		t.Errorf("scrambled PMT: %v %s", err, toJSON(t, report))
	}
	analysis, err := decodeRangeAnalysis(t, mark(0x1000, true, 0), options.Options{})
	if err != nil || analysis.Status != "incomplete" || analysis.Reason != "no_program_analyzed" || analysis.TransportErrors != nil {
		t.Errorf("PMT with transport errors: %v %s", err, toJSON(t, analysis))
	}
	report, err = decodeJSONReport(t, func() error { return ParseTsFile(mark(0x0000, true, 0), options.Options{Format: "json"}) })
	if err == nil || report.Status != "failed" || report.TransportErrors != nil {
		t.Errorf("PAT with transport errors: %v %s", err, toJSON(t, report))
	}
	report, err = decodeJSONReport(t, func() error { return ParseTsFile(mark(0x0000, false, 3), options.Options{Format: "json"}) })
	if err == nil || report.Status != "failed" || report.TransportErrors != nil || !strings.Contains(report.Warnings[0].Message, "PAT: transport_scrambling_control set") {
		t.Errorf("scrambled PAT: %v %s", err, toJSON(t, report))
	}
}
