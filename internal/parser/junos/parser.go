// Package junos implements a parser for Juniper JunOS configuration files.
//
// JunOS uses a hierarchical, curly-brace delimited configuration language.
//
// Typical JunOS config excerpt:
//
//	system {
//	    host-name router1;
//	}
//	interfaces {
//	    ge-0/0/0 {
//	        description "WAN uplink";
//	        unit 0 {
//	            family inet {
//	                address 203.0.113.1/30;
//	            }
//	            family inet6 {
//	                address 2001:db8::1/64;
//	            }
//	        }
//	    }
//	}
package junos

import (
	"regexp"
	"strings"

	"github.com/buraglio/net2nbox/internal/model"
	"github.com/buraglio/net2nbox/internal/parser"
)

func init() {
	parser.DefaultRegistry.Register(&Parser{})
}

// Parser handles Juniper JunOS configuration files.
type Parser struct{}

func (p *Parser) Vendor() string { return "Juniper" }

func (p *Parser) Detect(content string) bool {
	return strings.Contains(content, "## Last changed:") ||
		strings.Contains(content, "## Junos") ||
		strings.Contains(content, "set interfaces") ||
		(strings.Contains(content, "family inet") && strings.Contains(content, "interfaces {"))
}

func (p *Parser) Parse(content string) (*model.DeviceData, error) {
	device := &model.DeviceData{
		Vendor:   "Juniper",
		Platform: "JunOS",
	}

	if isSetFormat(content) {
		return parseSetFormat(content, device)
	}
	return parseHierarchical(content, device)
}

func isSetFormat(content string) bool {
	for _, l := range strings.SplitN(content, "\n", 20) {
		if strings.HasPrefix(strings.TrimSpace(l), "set ") {
			return true
		}
	}
	return false
}

// parseSetFormat handles "set interfaces ge-0/0/0 unit 0 family inet address x.x.x.x/y"
func parseSetFormat(content string, device *model.DeviceData) (*model.DeviceData, error) {
	reHostname := regexp.MustCompile(`^set system host-name\s+(\S+)`)
	reVersion := regexp.MustCompile(`^set version\s+(\S+)`)
	reSerial := regexp.MustCompile(`^set chassis hardware serial-number\s+(\S+)`)
	reIfAddr := regexp.MustCompile(`^set interfaces (\S+) unit \d+ family inet6? address\s+(\S+)`)
	reIfDesc := regexp.MustCompile(`^set interfaces (\S+) description\s+"?([^"]+)"?`)
	reIfDisable := regexp.MustCompile(`^set interfaces (\S+) disable`)

	ifaceMap := make(map[string]*model.InterfaceData)

	for _, raw := range strings.Split(content, "\n") {
		l := strings.TrimRight(raw, "\r")
		if m := reHostname.FindStringSubmatch(l); m != nil {
			device.Hostname = m[1]
			continue
		}
		if m := reVersion.FindStringSubmatch(l); m != nil {
			device.OSVersion = m[1]
			continue
		}
		if m := reSerial.FindStringSubmatch(l); m != nil {
			device.SerialNumber = m[1]
			continue
		}
		if m := reIfAddr.FindStringSubmatch(l); m != nil {
			name, addr := m[1], m[2]
			iface := getOrCreate(ifaceMap, name)
			family := 4
			if strings.Contains(l, "inet6") {
				family = 6
			}
			iface.IPAddresses = append(iface.IPAddresses, model.IPAddressData{
				Address: addr,
				Family:  family,
			})
			continue
		}
		if m := reIfDesc.FindStringSubmatch(l); m != nil {
			iface := getOrCreate(ifaceMap, m[1])
			iface.Description = m[2]
			continue
		}
		if m := reIfDisable.FindStringSubmatch(l); m != nil {
			iface := getOrCreate(ifaceMap, m[1])
			iface.Enabled = false
			continue
		}
	}

	for _, iface := range ifaceMap {
		device.Interfaces = append(device.Interfaces, *iface)
	}
	return device, nil
}

// parseHierarchical handles the curly-brace hierarchical format.
func parseHierarchical(content string, device *model.DeviceData) (*model.DeviceData, error) {
	reHostname := regexp.MustCompile(`^\s+host-name\s+(\S+);`)
	reVersion := regexp.MustCompile(`^\s*version\s+([\d.A-Z]+);`)
	reSerial := regexp.MustCompile(`^\s+serial-number\s+(\S+);`)

	lines := strings.Split(content, "\n")
	ifaceMap := make(map[string]*model.InterfaceData)

	type frame struct{ token string }
	stack := []frame{}
	var curIfaceName string

	stackContains := func(tok string) bool {
		for _, f := range stack {
			if f.token == tok {
				return true
			}
		}
		return false
	}

	inInterfaces := func() bool {
		return len(stack) >= 1 && stack[0].token == "interfaces"
	}

	for _, raw := range lines {
		l := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(l)

		if m := reHostname.FindStringSubmatch(l); m != nil && stackContains("system") {
			device.Hostname = m[1]
			continue
		}
		if m := reVersion.FindStringSubmatch(l); m != nil {
			device.OSVersion = m[1]
			continue
		}
		if m := reSerial.FindStringSubmatch(l); m != nil {
			device.SerialNumber = m[1]
			continue
		}

		if strings.HasSuffix(trimmed, "{") {
			token := strings.TrimSuffix(trimmed, " {")
			token = strings.TrimSuffix(token, "{")
			token = strings.TrimSpace(token)
			stack = append(stack, frame{token})

			if len(stack) == 2 && stack[0].token == "interfaces" {
				curIfaceName = stack[1].token
				getOrCreate(ifaceMap, curIfaceName)
			}
			continue
		}
		if trimmed == "}" {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if len(stack) < 2 || stack[0].token != "interfaces" {
				curIfaceName = ""
			}
			continue
		}

		if strings.HasSuffix(trimmed, ";") && inInterfaces() && curIfaceName != "" {
			iface := getOrCreate(ifaceMap, curIfaceName)
			stmt := strings.TrimSuffix(trimmed, ";")

			if strings.HasPrefix(stmt, "description ") {
				iface.Description = strings.Trim(strings.TrimPrefix(stmt, "description "), `"`)
			}
			if stmt == "disable" {
				iface.Enabled = false
			}
			if strings.HasPrefix(stmt, "address ") {
				addr := strings.TrimPrefix(stmt, "address ")
				family := 4
				if stackContains("family inet6") {
					family = 6
				}
				if stackContains("family inet") || stackContains("family inet6") {
					iface.IPAddresses = append(iface.IPAddresses, model.IPAddressData{
						Address: addr,
						Family:  family,
					})
				}
			}
		}
	}

	for _, iface := range ifaceMap {
		device.Interfaces = append(device.Interfaces, *iface)
	}
	return device, nil
}

func getOrCreate(m map[string]*model.InterfaceData, name string) *model.InterfaceData {
	if iface, ok := m[name]; ok {
		return iface
	}
	iface := &model.InterfaceData{
		Name:    name,
		Type:    junosInterfaceType(name),
		Enabled: true,
	}
	m[name] = iface
	return iface
}

// junosInterfaceType maps JunOS interface naming conventions to NetBox type slugs.
// JunOS uses prefix-fpc/pic/port notation: ge-0/0/0, xe-0/0/0, et-0/0/0, etc.
func junosInterfaceType(name string) string {
	ln := strings.ToLower(name)
	switch {
	case strings.HasPrefix(ln, "et-"):
		return "100gbase-x-qsfp28" // 100GE
	case strings.HasPrefix(ln, "fte-"):
		return "40gbase-x-qsfpp" // 40GE
	case strings.HasPrefix(ln, "xe-"):
		return "10gbase-x-sfpp" // 10GE
	case strings.HasPrefix(ln, "ge-"):
		return "1000base-t" // 1GE
	case strings.HasPrefix(ln, "fe-"):
		return "100base-tx" // 100ME
	case strings.HasPrefix(ln, "ae"):
		return "lag" // Aggregated Ethernet
	case strings.HasPrefix(ln, "lo"):
		return "virtual" // Loopback
	case strings.HasPrefix(ln, "gr-"), strings.HasPrefix(ln, "ip-"),
		strings.HasPrefix(ln, "st0"), strings.HasPrefix(ln, "vt-"):
		return "virtual" // Tunnels
	case strings.HasPrefix(ln, "irb"), strings.HasPrefix(ln, "vlan"):
		return "virtual" // IRB/VLAN
	default:
		return "other"
	}
}
