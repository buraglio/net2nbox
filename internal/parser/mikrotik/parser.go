// Package mikrotik implements a parser for MikroTik RouterOS v7 export files
// (produced by /export or /export compact in the RouterOS CLI).
//
// Export format overview:
//
//	# <date> by RouterOS <version>
//	# software id = <id>
//	## model = <model>
//	# serial number = <serial>
//
//	/section/path
//	add key=value key2="quoted value" ...
//	set [ find default-name=ether1 ] key=value ...
package mikrotik

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/buraglio/net2nbox/internal/model"
	"github.com/buraglio/net2nbox/internal/parser"
)

func init() {
	parser.DefaultRegistry.Register(&Parser{})
}

// Parser handles MikroTik RouterOS v7 configuration exports.
type Parser struct{}

func (p *Parser) Vendor() string { return "MikroTik" }

func (p *Parser) Detect(content string) bool {
	return strings.Contains(content, "by RouterOS") ||
		strings.Contains(content, "/interface ethernet") ||
		strings.Contains(content, "routeros")
}

// Parse parses a full RouterOS export and returns normalized device data.
func (p *Parser) Parse(content string) (*model.DeviceData, error) {
	lines := strings.Split(content, "\n")
	lines = joinContinuationLines(lines)

	device := &model.DeviceData{
		Vendor:   "MikroTik",
		Platform: "RouterOS",
	}
	parseHeader(lines, device)

	sections := buildSections(lines)
	ifaceMap := make(map[string]*model.InterfaceData)

	for _, sec := range sections {
		switch sec.path {
		case "/system identity":
			parseSystemIdentity(sec, device)
		case "/interface ethernet":
			parseEthernetSection(sec, ifaceMap)
		case "/interface bridge":
			parseBridgeSection(sec, ifaceMap)
		case "/interface vlan":
			parseVLANSection(sec, ifaceMap)
		case "/interface bonding":
			parseBondingSection(sec, ifaceMap)
		case "/interface bridge port":
			parseBridgePortSection(sec, ifaceMap)
		case "/interface eoip", "/interface gre", "/interface ipip",
			"/interface vxlan", "/interface l2tp-client", "/interface sstp-client",
			"/interface ovpn-client", "/interface wireguard":
			parseTunnelSection(sec, ifaceMap)
		case "/interface pppoe-client":
			parsePPPoESection(sec, ifaceMap)
		case "/interface lte":
			parseLTESection(sec, ifaceMap)
		case "/interface wireless":
			parseWirelessSection(sec, ifaceMap)
		case "/ip address":
			parseIPv4Section(sec, ifaceMap)
		case "/ipv6 address":
			parseIPv6Section(sec, ifaceMap)
		}
	}

	for _, iface := range ifaceMap {
		device.Interfaces = append(device.Interfaces, *iface)
	}
	return device, nil
}

var (
	reVersion  = regexp.MustCompile(`by RouterOS\s+([\d.]+)`)
	reModel    = regexp.MustCompile(`(?i)^##+\s*model\s*=\s*(.+)`)
	reSerial   = regexp.MustCompile(`(?i)^#\s*serial number\s*=\s*(.+)`)
	reSoftware = regexp.MustCompile(`(?i)^#\s*software id\s*=\s*(.+)`)
)

func parseHeader(lines []string, d *model.DeviceData) {
	for _, l := range lines {
		if !strings.HasPrefix(l, "#") {
			break // header ends at first non-comment line
		}
		if m := reVersion.FindStringSubmatch(l); m != nil {
			d.OSVersion = strings.TrimSpace(m[1])
		}
		if m := reModel.FindStringSubmatch(l); m != nil {
			d.Model = strings.TrimSpace(m[1])
		}
		if m := reSerial.FindStringSubmatch(l); m != nil {
			d.SerialNumber = strings.TrimSpace(m[1])
		}
		if m := reSoftware.FindStringSubmatch(l); m != nil {
			d.SoftwareID = strings.TrimSpace(m[1])
		}
	}
}

type section struct {
	path     string
	commands []*command
}

type command struct {
	typ      string            // "add" | "set" | "remove"
	selector map[string]string // from [ find key=val ... ]
	params   map[string]string // key=value pairs
}

func buildSections(lines []string) []*section {
	var sections []*section
	var cur *section

	for _, raw := range lines {
		l := strings.TrimRight(raw, "\r ")
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		// New section: line starts with "/" and is not an inline comment/command
		if strings.HasPrefix(l, "/") {
			cur = &section{path: strings.TrimSpace(l)}
			sections = append(sections, cur)
			continue
		}
		if cur == nil {
			continue
		}
		if cmd := parseLine(l); cmd != nil {
			cur.commands = append(cur.commands, cmd)
		}
	}
	return sections
}

func parseLine(l string) *command {
	l = strings.TrimSpace(l)
	var typ string
	switch {
	case strings.HasPrefix(l, "add "), l == "add":
		typ = "add"
		l = strings.TrimSpace(strings.TrimPrefix(l, "add"))
	case strings.HasPrefix(l, "set "), l == "set":
		typ = "set"
		l = strings.TrimSpace(strings.TrimPrefix(l, "set"))
	case strings.HasPrefix(l, "remove "):
		typ = "remove"
		l = strings.TrimSpace(strings.TrimPrefix(l, "remove"))
	default:
		return nil
	}

	cmd := &command{
		typ:      typ,
		selector: make(map[string]string),
		params:   make(map[string]string),
	}

	// Extract [ find key=val ... ] selector
	if strings.HasPrefix(l, "[ find ") {
		end := strings.Index(l, " ]")
		if end >= 0 {
			selectorStr := l[7:end] // skip "[ find "
			cmd.selector = parseKV(selectorStr)
			l = strings.TrimLeft(l[end+2:], " \t")
		}
	}
	cmd.params = parseKV(l)
	return cmd
}

// parseKV parses a RouterOS key=value string, correctly handling quoted values.
func parseKV(s string) map[string]string {
	result := make(map[string]string)
	s = strings.TrimSpace(s)
	for len(s) > 0 {
		s = strings.TrimLeft(s, " \t")
		if len(s) == 0 {
			break
		}
		eqIdx := strings.IndexByte(s, '=')
		if eqIdx < 0 {
			break
		}
		key := strings.TrimSpace(s[:eqIdx])
		s = s[eqIdx+1:]

		var value string
		if len(s) > 0 && s[0] == '"' {
			s = s[1:] // skip opening quote
			var sb strings.Builder
			i := 0
			for i < len(s) {
				if s[i] == '"' {
					break
				}
				if s[i] == '\\' && i+1 < len(s) {
					i++ // skip backslash; write the escaped character
					sb.WriteByte(s[i])
				} else {
					sb.WriteByte(s[i])
				}
				i++
			}
			value = sb.String()
			if i < len(s) {
				s = s[i+1:] // skip closing quote
			} else {
				s = ""
			}
		} else {
			spIdx := strings.IndexAny(s, " \t")
			if spIdx < 0 {
				value = s
				s = ""
			} else {
				value = s[:spIdx]
				s = s[spIdx:]
			}
		}
		if key != "" {
			result[key] = value
		}
	}
	return result
}

// joinContinuationLines merges RouterOS lines ending with "\" into one.
func joinContinuationLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	var buf strings.Builder
	for _, l := range lines {
		if strings.HasSuffix(l, "\\") {
			buf.WriteString(strings.TrimSuffix(l, "\\"))
		} else {
			buf.WriteString(l)
			out = append(out, buf.String())
			buf.Reset()
		}
	}
	if buf.Len() > 0 {
		out = append(out, buf.String())
	}
	return out
}

func parseSystemIdentity(sec *section, d *model.DeviceData) {
	for _, cmd := range sec.commands {
		if name, ok := cmd.params["name"]; ok {
			d.Hostname = name
		}
	}
}

func parseEthernetSection(sec *section, ifaceMap map[string]*model.InterfaceData) {
	for _, cmd := range sec.commands {
		if cmd.typ != "set" {
			continue
		}
		// Prefer explicit 'name' param; fall back to selector 'default-name'
		ifName := cmd.selector["default-name"]
		if n, ok := cmd.params["name"]; ok && n != "" {
			ifName = n
		}
		if ifName == "" {
			continue
		}
		iface := getOrCreate(ifaceMap, ifName, inferType(ifName))
		applyCommonParams(iface, cmd.params)
	}
}

func parseBridgeSection(sec *section, ifaceMap map[string]*model.InterfaceData) {
	for _, cmd := range sec.commands {
		if cmd.typ != "add" {
			continue
		}
		name := cmd.params["name"]
		if name == "" {
			continue
		}
		iface := getOrCreate(ifaceMap, name, "bridge")
		// RouterOS bridge exports admin-mac as the stable MAC
		if mac := cmd.params["admin-mac"]; mac != "" {
			iface.MACAddress = normalizeMAC(mac)
		}
		if mac := cmd.params["mac-address"]; mac != "" {
			iface.MACAddress = normalizeMAC(mac)
		}
		applyCommonParams(iface, cmd.params)
	}
}

func parseVLANSection(sec *section, ifaceMap map[string]*model.InterfaceData) {
	for _, cmd := range sec.commands {
		if cmd.typ != "add" {
			continue
		}
		name := cmd.params["name"]
		if name == "" {
			continue
		}
		iface := getOrCreate(ifaceMap, name, "virtual")
		iface.ParentInterface = cmd.params["interface"]
		if vid, err := strconv.Atoi(cmd.params["vlan-id"]); err == nil {
			iface.VLANid = vid
		}
		applyCommonParams(iface, cmd.params)
	}
}

func parseBondingSection(sec *section, ifaceMap map[string]*model.InterfaceData) {
	for _, cmd := range sec.commands {
		if cmd.typ != "add" {
			continue
		}
		name := cmd.params["name"]
		if name == "" {
			continue
		}
		iface := getOrCreate(ifaceMap, name, "lag")
		if slaves := cmd.params["slaves"]; slaves != "" {
			for _, s := range strings.Split(slaves, ",") {
				if s = strings.TrimSpace(s); s != "" {
					iface.BondMembers = append(iface.BondMembers, s)
					getOrCreate(ifaceMap, s, inferType(s))
				}
			}
		}
		applyCommonParams(iface, cmd.params)
	}
}

func parseBridgePortSection(sec *section, ifaceMap map[string]*model.InterfaceData) {
	for _, cmd := range sec.commands {
		if cmd.typ != "add" {
			continue
		}
		bridgeName := cmd.params["bridge"]
		memberName := cmd.params["interface"]
		if bridgeName == "" || memberName == "" {
			continue
		}
		bridge := getOrCreate(ifaceMap, bridgeName, "bridge")
		bridge.BridgeMembers = append(bridge.BridgeMembers, memberName)
	}
}

func parseTunnelSection(sec *section, ifaceMap map[string]*model.InterfaceData) {
	for _, cmd := range sec.commands {
		if cmd.typ != "add" {
			continue
		}
		name := cmd.params["name"]
		if name == "" {
			continue
		}
		iface := getOrCreate(ifaceMap, name, "virtual")
		applyCommonParams(iface, cmd.params)
	}
}

func parsePPPoESection(sec *section, ifaceMap map[string]*model.InterfaceData) {
	for _, cmd := range sec.commands {
		if cmd.typ != "add" {
			continue
		}
		name := cmd.params["name"]
		if name == "" {
			continue
		}
		iface := getOrCreate(ifaceMap, name, "virtual")
		iface.ParentInterface = cmd.params["interface"]
		applyCommonParams(iface, cmd.params)
	}
}

func parseLTESection(sec *section, ifaceMap map[string]*model.InterfaceData) {
	for _, cmd := range sec.commands {
		if cmd.typ != "add" {
			continue
		}
		name := cmd.params["name"]
		if name == "" {
			continue
		}
		iface := getOrCreate(ifaceMap, name, "lte")
		applyCommonParams(iface, cmd.params)
	}
}

func parseWirelessSection(sec *section, ifaceMap map[string]*model.InterfaceData) {
	for _, cmd := range sec.commands {
		switch cmd.typ {
		case "add":
			if name := cmd.params["name"]; name != "" {
				iface := getOrCreate(ifaceMap, name, "ieee802.11ax")
				applyCommonParams(iface, cmd.params)
			}
		case "set":
			// Physical wireless: set [ find default-name=wlan1 ] ...
			wname := cmd.selector["default-name"]
			if n := cmd.params["name"]; n != "" {
				wname = n
			}
			if wname != "" {
				iface := getOrCreate(ifaceMap, wname, "ieee802.11ax")
				applyCommonParams(iface, cmd.params)
			}
		}
	}
}

func parseIPv4Section(sec *section, ifaceMap map[string]*model.InterfaceData) {
	for _, cmd := range sec.commands {
		if cmd.typ != "add" {
			continue
		}
		addr := cmd.params["address"]
		ifName := cmd.params["interface"]
		if addr == "" || ifName == "" {
			continue
		}
		cidr := normalizeCIDR(addr)
		if cidr == "" {
			continue
		}
		iface := getOrCreate(ifaceMap, ifName, inferType(ifName))
		iface.IPAddresses = append(iface.IPAddresses, model.IPAddressData{
			Address:     cidr,
			Family:      4,
			Description: cmd.params["comment"],
		})
	}
}

func parseIPv6Section(sec *section, ifaceMap map[string]*model.InterfaceData) {
	for _, cmd := range sec.commands {
		if cmd.typ != "add" {
			continue
		}
		addr := cmd.params["address"]
		ifName := cmd.params["interface"]
		if addr == "" || ifName == "" {
			continue
		}
		cidr := normalizeCIDR(addr)
		if cidr == "" {
			continue
		}
		iface := getOrCreate(ifaceMap, ifName, inferType(ifName))
		iface.IPAddresses = append(iface.IPAddresses, model.IPAddressData{
			Address:     cidr,
			Family:      6,
			Description: cmd.params["comment"],
		})
	}
}

func getOrCreate(m map[string]*model.InterfaceData, name, ifType string) *model.InterfaceData {
	if iface, ok := m[name]; ok {
		return iface
	}
	iface := &model.InterfaceData{
		Name:    name,
		Type:    ifType,
		Enabled: true,
	}
	m[name] = iface
	return iface
}

// applyCommonParams writes generic interface fields from a parsed key=value map.
func applyCommonParams(iface *model.InterfaceData, p map[string]string) {
	if v, ok := p["comment"]; ok {
		iface.Description = v
	}
	if v, ok := p["disabled"]; ok {
		iface.Enabled = (v == "no" || v == "false")
	}
	if v, ok := p["mtu"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			iface.MTU = n
		}
	}
	if v, ok := p["mac-address"]; ok && v != "" {
		iface.MACAddress = normalizeMAC(v)
	}
}

// inferType maps a RouterOS interface name prefix to a NetBox type slug.
func inferType(name string) string {
	ln := strings.ToLower(name)
	switch {
	case strings.HasPrefix(ln, "sfp28"):
		return "25gbase-x-sfp28"
	case strings.HasPrefix(ln, "sfp-sfpplus"), strings.HasPrefix(ln, "sfpplus"):
		return "10gbase-x-sfpp"
	case strings.HasPrefix(ln, "qsfp28"):
		return "100gbase-x-qsfp28"
	case strings.HasPrefix(ln, "qsfpplus"), strings.HasPrefix(ln, "qsfp"):
		return "40gbase-x-qsfpp"
	case strings.HasPrefix(ln, "sfp"):
		return "1000base-x-sfp"
	case strings.HasPrefix(ln, "ether"):
		return "1000base-t"
	case strings.HasPrefix(ln, "bridge"):
		return "bridge"
	case strings.HasPrefix(ln, "bond"):
		return "lag"
	case strings.HasPrefix(ln, "vlan"):
		return "virtual"
	case strings.HasPrefix(ln, "wlan"), strings.HasPrefix(ln, "wifi"):
		return "ieee802.11ax"
	case strings.HasPrefix(ln, "lte"):
		return "lte"
	case strings.HasPrefix(ln, "lo"):
		return "virtual"
	default:
		return "virtual"
	}
}

// normalizeCIDR validates and normalises an address string to host/prefix form.
// RouterOS always stores the host address (not the network address), so
// "192.168.1.1/24" stays "192.168.1.1/24".
func normalizeCIDR(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "/") {
		return s // no prefix; return as-is, NetBox will reject invalid addrs
	}
	parts := strings.SplitN(s, "/", 2)
	ip := net.ParseIP(parts[0])
	if ip == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s", ip.String(), parts[1])
}

var reNonHex = regexp.MustCompile(`[^0-9A-Fa-f]`)

// normalizeMAC converts various MAC address formats to XX:XX:XX:XX:XX:XX.
func normalizeMAC(s string) string {
	digits := reNonHex.ReplaceAllString(s, "")
	if len(digits) != 12 {
		return strings.ToUpper(s)
	}
	return strings.ToUpper(fmt.Sprintf("%s:%s:%s:%s:%s:%s",
		digits[0:2], digits[2:4], digits[4:6],
		digits[6:8], digits[8:10], digits[10:12]))
}
