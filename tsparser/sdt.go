package tsparser

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"slices"

	"github.com/small-teton/mpeg-ts-analyzer/v2/options"
)

// Service names come from the SDT actual (Service Description Table, ETSI
// EN 300 468 clause 5.2.3), table_id 0x42 on PID 0x0011. The PID also carries
// the SDT of other transport streams (table_id 0x46) and the BAT (0x4A), which
// are not used. A service_id equals the program_number of the PAT. Sections
// are assembled with the PAT and PMT rules (psiAssembler); the latest complete
// table of the scan is reported. An SDT is never required: a problem with one
// leaves the names out without a warning, and no verdict depends on it.

const (
	sdtPidValue                 = 0x0011
	sdtActualTableID            = 0x42
	serviceDescriptorTag        = 0x48
	sdtSectionHeaderSize        = 11 // through reserved_future_use after original_network_id
	sdtServiceHeaderSize        = 5
	sdtListingSearchDurationPcr = 3000 * pcrTicksPerMs
)

// sdtListingSearchPacketsNoPcr ends the search for the program listing when no
// PCR has been seen; a variable so tests can lower it.
var sdtListingSearchPacketsNoPcr = 100000

// sdtService is one service of an SDT actual, with its first service_descriptor.
type sdtService struct {
	id                   uint16
	hasServiceDescriptor bool
	serviceType          uint8
	provider, name       []byte
}

// sdtSection is the service loop of one SDT section.
type sdtSection struct {
	raw      []byte
	services []sdtService
}

func parseSdtSection(data []byte) (*sdtSection, error) {
	section := &sdtSection{raw: data}
	end := len(data) - 4 // CRC_32
	for i := sdtSectionHeaderSize; i < end; {
		if end-i < sdtServiceHeaderSize {
			return nil, fmt.Errorf("service entry truncated at byte %d", i)
		}
		service := sdtService{id: uint16(data[i])<<8 | uint16(data[i+1])}
		loopLength := int(data[i+3]&0x0F)<<8 | int(data[i+4])
		i += sdtServiceHeaderSize
		if loopLength > end-i {
			return nil, fmt.Errorf("descriptors_loop_length %d of service %d exceeds the section", loopLength, service.id)
		}
		for d := data[i : i+loopLength]; len(d) >= 2; d = d[min(2+int(d[1]), len(d)):] {
			if d[0] == serviceDescriptorTag && !service.hasServiceDescriptor {
				service.parseServiceDescriptor(d[2:min(2+int(d[1]), len(d))])
			}
		}
		i += loopLength
		section.services = append(section.services, service)
	}
	return section, nil
}

// parseServiceDescriptor reads service_type, service_provider_name and
// service_name. A truncated descriptor is ignored.
func (s *sdtService) parseServiceDescriptor(body []byte) {
	if len(body) < 2 {
		return
	}
	providerEnd := 2 + int(body[1])
	if providerEnd >= len(body) {
		return
	}
	nameEnd := providerEnd + 1 + int(body[providerEnd])
	if nameEnd > len(body) {
		return
	}
	s.hasServiceDescriptor, s.serviceType = true, body[0]
	s.provider, s.name = body[2:providerEnd], body[providerEnd+1:nameEnd]
}

// sdtCollector follows the SDT actual on PID 0x0011. Versions are applied as
// the PAT and PMT tracker applies them: a table with the active
// version_number never replaces the active one, and a table byte-identical to
// the one last applied with its version_number is a late retransmission of a
// superseded version, so the names never roll back. version_number wraps after
// 31, so a reused version with different content is applied.
type sdtCollector struct {
	reader    psiSectionReader
	assembler *psiAssembler[*sdtSection]
	services  map[uint16]sdtService // of the active table; nil before one
	version   uint8                 // of the active table
	history   map[uint8][][]byte    // sections of the last table applied with each version_number
}

func newSdtCollector() *sdtCollector {
	return &sdtCollector{
		assembler: &psiAssembler[*sdtSection]{name: "SDT", tableID: sdtActualTableID, anyExtension: true, parse: parseSdtSection},
		history:   make(map[uint8][][]byte),
	}
}

// observe passes one packet of the scan at offset pos. It reports whether the
// packet completed a table that was applied.
func (c *sdtCollector) observe(tsPacket *TsPacket, pos int64) bool {
	if tsPacket.Pid() != sdtPidValue {
		return false
	}
	sections, _ := c.reader.push(tsPacket, pos)
	applied := false
	for _, section := range sections {
		table, ok := c.assembler.add(section.data, section.pos)
		if !ok {
			continue
		}
		version := table[0].raw[5] >> 1 & 0x1F
		raw := make([][]byte, len(table))
		for i, s := range table {
			raw[i] = s.raw
		}
		if c.services != nil && version == c.version {
			continue
		}
		if previous, seen := c.history[version]; seen && slices.EqualFunc(previous, raw, bytes.Equal) {
			continue
		}
		c.history[version], c.version = raw, version
		// A service listed in several sections keeps its first entry.
		c.services = make(map[uint16]sdtService)
		for _, s := range table {
			for _, service := range s.services {
				if _, listed := c.services[service.id]; !listed {
					c.services[service.id] = service
				}
			}
		}
		applied = true
	}
	c.assembler.diagnostics = nil
	return applied
}

// service returns the entry of the program, if the SDT lists it.
func (c *sdtCollector) service(programNumber uint16) (sdtService, bool) {
	if c == nil {
		return sdtService{}, false
	}
	service, ok := c.services[programNumber]
	return service, ok
}

// findSdt looks for a complete SDT actual from start, for the program listing.
// EN 300 468 requires the SDT actual at least every 2 s, so the search ends 3 s
// of PCR time after the first PCR, or after a fixed number of packets when no
// PCR has been seen by then. It also ends with the input or the range. count
// is the number of packets read, including the current one.
func findSdt(reader io.ReadSeeker, start int64, options options.Options, packetSize int, endOffset int64) *sdtCollector {
	collector := newSdtCollector()
	if _, err := reader.Seek(start, io.SeekStart); err != nil {
		return collector
	}
	tsBuffer := make([]byte, packetSize)
	tsPacket := NewTsPacket()
	var pcrPid uint16
	var firstPcr uint64
	havePcr := false
	for pos, count := start, 1; endOffset <= 0 || pos+int64(packetSize) <= endOffset; pos, count = pos+int64(packetSize), count+1 {
		if _, err := io.ReadFull(reader, tsBuffer); err != nil {
			break
		}
		tsPacket.Initialize(pos, options)
		tsPacket.Append(tsBuffer[packetSize-tsPayloadSize:])
		if tsPacket.Parse() != nil || collector.observe(tsPacket, pos) {
			break
		}
		if tsPacket.HasAf() && tsPacket.adaptationField.PcrFlag() {
			if !havePcr {
				pcrPid, firstPcr, havePcr = tsPacket.Pid(), tsPacket.Pcr(), true
			} else if tsPacket.Pid() == pcrPid && (tsPacket.Pcr()+pcrWrapTicks-firstPcr)%pcrWrapTicks > sdtListingSearchDurationPcr {
				break
			}
		}
		if !havePcr && count >= sdtListingSearchPacketsNoPcr {
			break
		}
	}
	return collector
}

// ReportService describes a program's service from the SDT actual. Each name
// is decoded text, or its raw bytes in hex (with the leading character-table
// byte) when the character table is not supported.
type ReportService struct {
	ServiceType            string `json:"service_type,omitempty"`
	ServiceTypeDescription string `json:"service_type_description,omitempty"`
	ProviderName           string `json:"provider_name,omitempty"`
	ProviderNameHex        string `json:"provider_name_hex,omitempty"`
	Name                   string `json:"name,omitempty"`
	NameHex                string `json:"name_hex,omitempty"`
}

// reportService returns nil when the SDT does not list the program.
func (c *sdtCollector) reportService(programNumber uint16) *ReportService {
	service, ok := c.service(programNumber)
	if !ok {
		return nil
	}
	r := &ReportService{}
	if service.hasServiceDescriptor {
		r.ServiceType = fmt.Sprintf("0x%02x", service.serviceType)
		r.ServiceTypeDescription = serviceTypeString(service.serviceType)
		r.ProviderName, r.ProviderNameHex = reportDVBText(service.provider)
		r.Name, r.NameHex = reportDVBText(service.name)
	}
	return r
}

func reportDVBText(b []byte) (text, rawHex string) {
	if text, ok := decodeDVBText(b); ok {
		return text, ""
	}
	return "", hex.EncodeToString(b)
}

// describeService is the text form for the program listing, empty when the SDT
// does not name the program.
func (c *sdtCollector) describeService(programNumber uint16) string {
	r := c.reportService(programNumber)
	if r == nil || r.ServiceType == "" {
		return ""
	}
	text := func(label, decoded, rawHex string) string {
		if rawHex != "" {
			return fmt.Sprintf("%s (undecodable) 0x%s", label, rawHex)
		}
		return fmt.Sprintf("%s %q", label, decoded)
	}
	return fmt.Sprintf("Service: %s, %s, service_type %s (%s)", text("name", r.Name, r.NameHex), text("provider", r.ProviderName, r.ProviderNameHex), r.ServiceType, r.ServiceTypeDescription)
}

// serviceTypeString names a service_type (EN 300 468 Table 89).
func serviceTypeString(serviceType uint8) string {
	switch serviceType {
	case 0x01:
		return "digital television service"
	case 0x02:
		return "digital radio sound service"
	case 0x03:
		return "Teletext service"
	case 0x04:
		return "NVOD reference service"
	case 0x05:
		return "NVOD time-shifted service"
	case 0x06:
		return "mosaic service"
	case 0x07:
		return "FM radio service"
	case 0x08:
		return "DVB SRM service"
	case 0x0A:
		return "advanced codec digital radio sound service"
	case 0x0B:
		return "H.264/AVC mosaic service"
	case 0x0C:
		return "data broadcast service"
	case 0x0D:
		return "reserved for Common Interface usage"
	case 0x0E:
		return "RCS Map"
	case 0x0F:
		return "RCS FLS"
	case 0x10:
		return "DVB MHP service"
	case 0x11:
		return "MPEG-2 HD digital television service"
	case 0x16:
		return "H.264/AVC SD digital television service"
	case 0x17:
		return "H.264/AVC SD NVOD time-shifted service"
	case 0x18:
		return "H.264/AVC SD NVOD reference service"
	case 0x19:
		return "H.264/AVC HD digital television service"
	case 0x1A:
		return "H.264/AVC HD NVOD time-shifted service"
	case 0x1B:
		return "H.264/AVC HD NVOD reference service"
	case 0x1C:
		return "H.264/AVC frame compatible plano-stereoscopic HD digital television service"
	case 0x1D:
		return "H.264/AVC frame compatible plano-stereoscopic HD NVOD time-shifted service"
	case 0x1E:
		return "H.264/AVC frame compatible plano-stereoscopic HD NVOD reference service"
	case 0x1F:
		return "HEVC digital television service"
	case 0x20:
		return "HEVC UHD digital television service"
	}
	if serviceType >= 0x80 && serviceType <= 0xFE {
		return "user defined"
	}
	return "reserved"
}
