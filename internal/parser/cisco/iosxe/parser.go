// Package iosxe implements a parser for Cisco IOS-XE configuration files.
//
// Typical IOS-XE config excerpt:
//
//	hostname router1
//	!
//	interface GigabitEthernet0/0/0
//	 description WAN uplink
//	 ip address 203.0.113.1 255.255.255.252
//	 ipv6 address 2001:db8::1/64
//	 no shutdown
//	!
package iosxe

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/buraglio/net2nbox/internal/model"
	"github.com/buraglio/net2nbox/internal/parser"
)

func init() {
	parser.DefaultRegistry.Register(&Parser{})
}

// Parser handles Cisco IOS-XE configuration files.
type Parser struct{}

func (p *Parser) Vendor() string { return "Cisco" }

func (p *Parser) Detect(content string) bool {
	return (strings.Contains(content, "IOS-XE") ||
		strings.Contains(content, "IOS XE") ||
		strings.Contains(content, "Cisco IOS Software")) &&
		!strings.Contains(content, "IOS XR")
}

// Parse extracts device data from a Cisco IOS-XE configuration.
func (p *Parser) Parse(content string) (*model.DeviceData, error) {
	device := &model.DeviceData{
		Vendor:   "Cisco",
		Platform: "IOS-XE",
	}

	lines := strings.Split(content, "\n")
	ifaceMap := make(map[string]*model.InterfaceData)
	var curIface *model.InterfaceData

	reHostname := regexp.MustCompile(`^hostname\s+(\S+)`)
	reVersion := regexp.MustCompile(`^version\s+(\S+)`)
	reSerial := regexp.MustCompile(`(?i)processor board id\s+(\S+)`)
	reInterface := regexp.MustCompile(`^interface\s+(\S+)`)
	reIPv4 := regexp.MustCompile(`^\s+ip address\s+(\S+)\s+(\S+)`)
	reIPv6 := regexp.MustCompile(`^\s+ipv6 address\s+(\S+)`)
	reDesc := regexp.MustCompile(`^\s+description\s+(.+)`)
	reShutdown := regexp.MustCompile(`^\s+(no\s+)?shutdown`)

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
				Type:    iosxeInterfaceType(name),
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
			cidr := maskToCIDR(m[1], m[2])
			if cidr != "" {
				curIface.IPAddresses = append(curIface.IPAddresses, model.IPAddressData{
					Address: cidr,
					Family:  4,
				})
			}
			continue
		}
		if m := reIPv6.FindStringSubmatch(l); m != nil {
			addr := strings.Fields(strings.TrimSpace(m[1]))[0]
			if strings.Contains(addr, "/") {
				curIface.IPAddresses = append(curIface.IPAddresses, model.IPAddressData{
					Address: addr,
					Family:  6,
				})
			}
			continue
		}
		if m := reShutdown.FindStringSubmatch(l); m != nil {
			curIface.Enabled = m[1] != "" // "no shutdown" = enabled
			continue
		}
	}

	for _, iface := range ifaceMap {
		device.Interfaces = append(device.Interfaces, *iface)
	}
	return device, nil
}

// iosxeInterfaceType maps Cisco IOS-XE interface name prefixes to NetBox slugs.
func iosxeInterfaceType(name string) string {
	ln := strings.ToLower(name)
	switch {
	case strings.HasPrefix(ln, "hundredgige"), strings.HasPrefix(ln, "hu"):
		return "100gbase-x-qsfp28"
	case strings.HasPrefix(ln, "fortygige"), strings.HasPrefix(ln, "fo"):
		return "40gbase-x-qsfpp"
	case strings.HasPrefix(ln, "twentyfivegige"):
		return "25gbase-x-sfp28"
	case strings.HasPrefix(ln, "tengige"), strings.HasPrefix(ln, "te"):
		return "10gbase-x-sfpp"
	case strings.HasPrefix(ln, "gigabitethernet"), strings.HasPrefix(ln, "gi"):
		return "1000base-t"
	case strings.HasPrefix(ln, "fastethernet"), strings.HasPrefix(ln, "fa"):
		return "100base-tx"
	case strings.HasPrefix(ln, "loopback"), strings.HasPrefix(ln, "lo"):
		return "virtual"
	case strings.HasPrefix(ln, "tunnel"):
		return "virtual"
	case strings.HasPrefix(ln, "vlan"):
		return "virtual"
	case strings.HasPrefix(ln, "port-channel"):
		return "lag"
	case strings.HasPrefix(ln, "serial"):
		return "other"
	default:
		return "other"
	}
}

// maskToCIDR converts a dotted-decimal subnet mask to CIDR prefix notation.
func maskToCIDR(addr, mask string) string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return ""
	}
	m := net.IPMask(net.ParseIP(mask).To4())
	ones, _ := m.Size()
	return fmt.Sprintf("%s/%d", ip.String(), ones)
}
