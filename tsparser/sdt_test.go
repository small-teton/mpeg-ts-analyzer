package tsparser

import (
	"bytes"
	"strings"
	"testing"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// sdtTestSection builds an SDT section; body is the service loop.
func sdtTestSection(tableID, version, number, last byte, services ...[]byte) []byte {
	body := []byte{0x00, 0x01, 0xFF} // original_network_id, reserved_future_use
	for _, service := range services {
		body = append(body, service...)
	}
	length := 5 + len(body) + 4
	section := []byte{tableID, 0xF0 | byte(length>>8), byte(length), 0x00, 0x01, 0xC1 | version<<1, number, last}
	section = append(section, body...)
	c := crc32(section)
	return append(section, byte(c>>24), byte(c>>16), byte(c>>8), byte(c))
}

func sdtTestService(id uint16, descriptors ...[]byte) []byte {
	loop := bytes.Join(descriptors, nil)
	entry := []byte{byte(id >> 8), byte(id), 0xFC, 0x80 | byte(len(loop)>>8), byte(len(loop))}
	return append(entry, loop...)
}

func serviceDescriptor(serviceType byte, provider, name string) []byte {
	d := []byte{serviceDescriptorTag, byte(3 + len(provider) + len(name)), serviceType, byte(len(provider))}
	d = append(d, provider...)
	d = append(d, byte(len(name)))
	return append(d, name...)
}

func observeSdtPacket(c *sdtCollector, packet []byte) bool {
	tsPacket := NewTsPacket()
	tsPacket.Initialize(0, options.Options{})
	tsPacket.Append(packet)
	if err := tsPacket.Parse(); err != nil {
		panic(err)
	}
	return c.observe(tsPacket, 0)
}

func TestParseSdtSection(t *testing.T) {
	section := sdtTestSection(0x42, 0, 0, 0,
		sdtTestService(1, []byte{0x5F, 0x04, 0, 0, 0, 1}, serviceDescriptor(0x19, "Provider", "Name"), serviceDescriptor(0x02, "Other", "Ignored")),
		sdtTestService(2),
		sdtTestService(3, []byte{0x48, 0x01, 0x01}),             // too short for the provider length
		sdtTestService(4, []byte{0x48, 0x02, 0x01, 0x05}),       // provider beyond the descriptor
		sdtTestService(5, []byte{0x48, 0x03, 0x01, 0x00, 0x05}), // name beyond the descriptor
		sdtTestService(6, []byte{0x48, 0x09, 0x01}),             // descriptor beyond the loop
	)
	parsed, err := parseSdtSection(section)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.services) != 6 {
		t.Fatalf("services: %+v", parsed.services)
	}
	first := parsed.services[0]
	if first.id != 1 || !first.hasServiceDescriptor || first.serviceType != 0x19 || string(first.provider) != "Provider" || string(first.name) != "Name" {
		t.Errorf("first service: %+v", first)
	}
	for _, service := range parsed.services[1:] {
		if service.hasServiceDescriptor {
			t.Errorf("service %d has a service_descriptor: %+v", service.id, service)
		}
	}

	truncated := sdtTestSection(0x42, 0, 0, 0, []byte{0x00, 0x01, 0xFC})
	if _, err := parseSdtSection(truncated); err == nil || !strings.Contains(err.Error(), "service entry truncated") {
		t.Errorf("truncated entry: %v", err)
	}
	overlong := sdtTestSection(0x42, 0, 0, 0, []byte{0x00, 0x07, 0xFC, 0x80, 0x10})
	if _, err := parseSdtSection(overlong); err == nil || !strings.Contains(err.Error(), "descriptors_loop_length 16 of service 7 exceeds the section") {
		t.Errorf("overlong loop: %v", err)
	}
}

func TestSdtCollector(t *testing.T) {
	var none *sdtCollector
	if _, ok := none.service(1); ok {
		t.Error("nil collector lists a service")
	}

	c := newSdtCollector()
	other := sdtTestSection(0x46, 0, 0, 0, sdtTestService(1, serviceDescriptor(1, "P", "Other TS")))
	if observeSdtPacket(c, buildTsPacket(0x0011, true, 0, other)) || observeSdtPacket(c, buildTsPacket(0x0012, true, 0, sdtTestSection(0x42, 0, 0, 0))) {
		t.Fatal("SDT other or another PID completed a table")
	}
	section0 := sdtTestSection(0x42, 0, 0, 1, sdtTestService(1, serviceDescriptor(1, "P", "One")))
	section1 := sdtTestSection(0x42, 0, 1, 1, sdtTestService(1, serviceDescriptor(1, "P", "Duplicate")), sdtTestService(2))
	if observeSdtPacket(c, buildTsPacket(0x0011, true, 1, section0)) {
		t.Fatal("first section completed the table")
	}
	if !observeSdtPacket(c, buildTsPacket(0x0011, true, 2, section1)) {
		t.Fatal("second section did not complete the table")
	}
	if r := c.reportService(1); r == nil || r.Name != "One" || r.ProviderName != "P" || r.ServiceType != "0x01" || r.ServiceTypeDescription != "digital television service" {
		t.Errorf("service 1: %+v", r)
	}
	if r := c.reportService(2); r == nil || *r != (ReportService{}) {
		t.Errorf("service 2 without a service_descriptor: %+v", r)
	}
	if r := c.reportService(3); r != nil {
		t.Errorf("unlisted service: %+v", r)
	}

	// A later version replaces the table.
	update := sdtTestSection(0x42, 1, 0, 0, sdtTestService(2, serviceDescriptor(0x02, "\x15Pr\xc3\xb6", "\x11\x00A")))
	if !observeSdtPacket(c, buildTsPacket(0x0011, true, 3, update)) {
		t.Fatal("version 1 did not complete")
	}
	if r := c.reportService(1); r != nil {
		t.Errorf("service 1 kept after the update: %+v", r)
	}
	want := ReportService{ServiceType: "0x02", ServiceTypeDescription: "digital radio sound service", ProviderName: "Prö", NameHex: "110041"}
	if r := c.reportService(2); r == nil || *r != want {
		t.Errorf("service 2: %+v", r)
	}
	if got := c.describeService(2); got != `Service: name (undecodable) 0x110041, provider "Prö", service_type 0x02 (digital radio sound service)` {
		t.Errorf("describeService: %s", got)
	}
	if got := c.describeService(1); got != "" {
		t.Errorf("describeService of an unlisted program: %q", got)
	}
}

func TestServiceTypeString(t *testing.T) {
	for serviceType := range 256 {
		if serviceTypeString(uint8(serviceType)) == "" {
			t.Errorf("service_type 0x%02x has no description", serviceType)
		}
	}
	for serviceType, want := range map[uint8]string{0x01: "digital television service", 0x09: "reserved", 0x1F: "HEVC digital television service", 0x80: "user defined", 0xFE: "user defined", 0xFF: "reserved"} {
		if got := serviceTypeString(serviceType); got != want {
			t.Errorf("service_type 0x%02x: %q, want %q", serviceType, got, want)
		}
	}
}

// sdtStream has two programs with PMTs and an SDT naming both after the given
// number of PCR packets on 0x0100, 40 ms apart.
func sdtStream(pcrsBeforeSdt int) *trackingStream {
	s := newTrackingStream().start(patSection([2]uint16{1, 0x1000}, [2]uint16{2, 0x1001}), map[uint16][]byte{
		0x1000: pmtSection(1, 0x0100, [2]uint16{0x02, 0x0100}),
		0x1001: pmtSection(2, 0x0100, [2]uint16{0x03, 0x0101}),
	}, 0x1000, 0x1001)
	for i := range pcrsBeforeSdt {
		s.pcr(0x0100, uint64(i)*40)
	}
	sdt := sdtTestSection(0x42, 0, 0, 0, sdtTestService(1, serviceDescriptor(0x01, "Provider", "First")), sdtTestService(2, serviceDescriptor(0x02, "Provider", "Second")))
	return s.section(0x0011, sdt).section(0x0011, sdt)
}

func TestFindSdt(t *testing.T) {
	for _, m2ts := range []bool{false, true} {
		path := sdtStream(10).write(t, m2ts)
		report := summary(t, path, options.Options{ListPrograms: true})
		if len(report.Programs) != 2 || report.Programs[0].Service == nil || report.Programs[0].Service.Name != "First" || report.Programs[1].Service.Name != "Second" {
			t.Errorf("m2ts=%v: %s", m2ts, toJSON(t, report.Programs))
		}
	}

	// More than 3 s of PCR time before the SDT: the listing gives up.
	report := summary(t, sdtStream(80).write(t, false), options.Options{ListPrograms: true})
	if report.Programs[0].Service != nil {
		t.Errorf("SDT found after the search limit: %+v", report.Programs[0].Service)
	}
	// A --limit range that ends before the SDT.
	report = summary(t, sdtStream(10).write(t, false), options.Options{ListPrograms: true, Limit: 188 * 12})
	if report.Programs[0].Service != nil {
		t.Errorf("SDT found beyond the range: %+v", report.Programs[0].Service)
	}

	// Without a PCR the search ends after a number of packets.
	saved := sdtListingSearchPacketsNoPcr
	defer func() { sdtListingSearchPacketsNoPcr = saved }()
	s := newTrackingStream().start(patSection([2]uint16{1, 0x1000}), map[uint16][]byte{0x1000: pmtSection(1, 0x1FFF, [2]uint16{0x02, 0x0100})}, 0x1000)
	for range 5 {
		s.pes(0x0100, 0)
	}
	s.section(0x0011, sdtTestSection(0x42, 0, 0, 0, sdtTestService(1, serviceDescriptor(0x01, "P", "Late"))))
	path := s.write(t, false)
	sdtListingSearchPacketsNoPcr = 3
	if report := summary(t, path, options.Options{ListPrograms: true}); report.Programs[0].Service != nil {
		t.Errorf("SDT found after the packet limit: %+v", report.Programs[0].Service)
	}
	sdtListingSearchPacketsNoPcr = saved
	if report := summary(t, path, options.Options{ListPrograms: true}); report.Programs[0].Service == nil || report.Programs[0].Service.Name != "Late" {
		t.Errorf("SDT not found within the packet limit: %s", toJSON(t, report.Programs))
	}

	// Read and seek errors leave the listing without names.
	if c := findSdt(&errReadSeeker{data: make([]byte, 376), failAt: -1, seekFailAt: 1}, 0, options.Options{}, 188, 0); c.services != nil {
		t.Error("seek error found an SDT")
	}
	invalid := bytes.Repeat([]byte{0x00}, 188)
	if c := findSdt(bytes.NewReader(invalid), 0, options.Options{}, 188, 0); c.services != nil {
		t.Error("invalid packet found an SDT")
	}
}

func TestListingPrintsServices(t *testing.T) {
	s := newTrackingStream().start(patSection([2]uint16{1, 0x1000}, [2]uint16{2, 0x1001}), map[uint16][]byte{
		0x1000: pmtSection(1, 0x0100, [2]uint16{0x02, 0x0100}),
	}, 0x1000)
	sdt := sdtTestSection(0x42, 0, 0, 0, sdtTestService(1, serviceDescriptor(0x01, "Provider", "First")), sdtTestService(2, serviceDescriptor(0x02, "Provider", "Missing PMT")))
	path := s.section(0x0011, sdt).write(t, false)

	report := summary(t, path, options.Options{ListPrograms: true})
	if report.Programs[1].Reason != "pmt_parse_failed" || report.Programs[1].Service == nil || report.Programs[1].Service.Name != "Missing PMT" {
		t.Errorf("program without a PMT: %s", toJSON(t, report.Programs[1]))
	}
	out := captureStdout(t, func() {
		if err := ParseTsFile(path, options.Options{ListPrograms: true}); err != nil {
			t.Error(err)
		}
	})
	for _, want := range []string{
		"Program 1: PMT PID 0x1000, PCR PID 0x100\nService: name \"First\", provider \"Provider\", service_type 0x01 (digital television service)\nPMT : Program Info",
		"(PMT parse error: PMT of program 2 on PID 0x1001: no valid section found)\nService: name \"Missing PMT\", provider \"Provider\", service_type 0x02 (digital radio sound service)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("listing lacks %q:\n%s", want, out)
		}
	}
}

func TestAnalysisReportsServices(t *testing.T) {
	path := sdtStream(10).pcr(0x0100, 400).write(t, false)
	report, err := decodeRangeAnalysis(t, path, options.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Programs) != 2 || report.Programs[0].Service == nil || report.Programs[0].Service.Name != "First" || report.Programs[1].Service.Name != "Second" {
		t.Errorf("analysis: %s", toJSON(t, report.Programs))
	}
	single := summary(t, path, options.Options{Program: 2})
	if single.Programs[0].Service == nil || single.Programs[0].Service.Name != "Second" {
		t.Errorf("summary: %s", toJSON(t, single.Programs))
	}
	// The text output of an analysis does not mention the SDT.
	out := captureStdout(t, func() { _ = ParseTsFile(path, options.Options{Program: 2}) })
	if strings.Contains(out, "Service:") || strings.Contains(out, "SDT") {
		t.Errorf("text analysis mentions the service:\n%s", out)
	}

	// No SDT: no service.
	plain := sdtStream(0)
	plain.packets = plain.packets[:len(plain.packets)-2]
	report, err = decodeRangeAnalysis(t, plain.pcr(0x0100, 0).pcr(0x0100, 40).write(t, false), options.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Programs[0].Service != nil {
		t.Errorf("service without an SDT: %+v", report.Programs[0].Service)
	}
}
