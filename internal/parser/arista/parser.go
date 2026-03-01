// Package arista implements a parser for Arista EOS configuration files.
//
// Arista EOS is syntactically close to Cisco IOS but uses CIDR notation
// for IP addresses rather than dotted-decimal masks.
//
// Typical EOS config excerpt:
//
//	hostname leaf01
//	!
//	interface Ethernet1
//	   description Spine uplink
//	   ip address 10.0.0.1/31
//	   ipv6 address 2001:db8::1/127
//	   no shutdown
//	!
package arista

import (
	"regexp"
	"strings"

	"github.com/buraglio/net2nbox/internal/model"
	"github.com/buraglio/net2nbox/internal/parser"
)

func init() {
	parser.DefaultRegistry.Register(&Parser{})
}

// Parser handles Arista EOS configuration files.
type Parser struct{}

func (p *Parser) Vendor() string { return "Arista" }

func (p *Parser) Detect(content string) bool {
	return strings.Contains(content, "Arista") ||
		strings.Contains(content, "EOS") ||
		(strings.Contains(content, "interface Ethernet") &&
			strings.Contains(content, "   ip address "))
}

// Parse extracts device data from an Arista EOS configuration.
func (p *Parser) Parse(content string) (*model.DeviceData, error) {
	device := &model.DeviceData{
		Vendor:   "Arista",
		Platform: "EOS",
	}

	lines := strings.Split(content, "\n")
	ifaceMap := make(map[string]*model.InterfaceData)
	var curIface *model.InterfaceData

	reHostname := regexp.MustCompile(`^hostname\s+(\S+)`)
	reVersion := regexp.MustCompile(`^! Command: show running-config.*EOS\s+([\d.]+)`)
	reSerial := regexp.MustCompile(`^! Serial Number:\s+(\S+)`)
	reInterface := regexp.MustCompile(`^interface\s+(\S+)`)
	reIPv4 := regexp.MustCompile(`^\s+ip address\s+(\S+/\d+)`)
	reIPv6 := regexp.MustCompile(`^\s+ipv6 address\s+(\S+/\d+)`)
	reDesc := regexp.MustCompile(`^\s+description\s+(.+)`)

	for _, raw := range lines {
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
		if strings.HasPrefix(l, "!") {
			curIface = nil
			continue
		}
		if m := reInterface.FindStringSubmatch(l); m != nil {
			name := m[1]
			iface := &model.InterfaceData{
				Name:    name,
				Type:    aristaInterfaceType(name),
				Enabled: true,
			}
			ifaceMap[name] = iface
			curIface = iface
			continue
		}
		if curIface == nil {
			continue
		}
		if m := reDesc.FindStringSubmatch(l); m != nil {
			curIface.Description = strings.TrimSpace(m[1])
			continue
		}
		if m := reIPv4.FindStringSubmatch(l); m != nil {
			curIface.IPAddresses = append(curIface.IPAddresses, model.IPAddressData{
				Address: m[1],
				Family:  4,
			})
			continue
		}
		if m := reIPv6.FindStringSubmatch(l); m != nil {
			curIface.IPAddresses = append(curIface.IPAddresses, model.IPAddressData{
				Address: m[1],
				Family:  6,
			})
			continue
		}
		if regexp.MustCompile(`^\s+shutdown`).MatchString(l) {
			curIface.Enabled = false
		}
	}

	for _, iface := range ifaceMap {
		device.Interfaces = append(device.Interfaces, *iface)
	}
	return device, nil
}

func aristaInterfaceType(name string) string {
	ln := strings.ToLower(name)
	switch {
	case strings.HasPrefix(ln, "hundredgige"):
		return "100gbase-x-qsfp28"
	case strings.HasPrefix(ln, "fortygige"):
		return "40gbase-x-qsfpp"
	case strings.HasPrefix(ln, "twentyfivegige"):
		return "25gbase-x-sfp28"
	case strings.HasPrefix(ln, "tengige"):
		return "10gbase-x-sfpp"
	case strings.HasPrefix(ln, "ethernet"):
		return "1000base-t"
	case strings.HasPrefix(ln, "management"):
		return "1000base-t"
	case strings.HasPrefix(ln, "loopback"):
		return "virtual"
	case strings.HasPrefix(ln, "vlan"):
		return "virtual"
	case strings.HasPrefix(ln, "port-channel"):
		return "lag"
	case strings.HasPrefix(ln, "tunnel"):
		return "virtual"
	default:
		return "other"
	}
}
