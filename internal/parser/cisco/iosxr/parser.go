// Package iosxr implements a parser for Cisco IOS-XR configuration files.
//
// IOS-XR differs from IOS-XE in several key ways:
//   - Uses "ipv4 address" instead of "ip address"
//   - Uses CIDR notation for both IPv4 and IPv6
//   - Supports "interface preconfigure" for absent interfaces
//   - Requires "commit" to apply staged changes
//
// Typical IOS-XR config excerpt:
//
//	hostname router1
//	interface HundredGigE0/0/0/0
//	 description Core uplink
//	 ipv4 address 203.0.113.1 255.255.255.252
//	 ipv6 address 2001:db8::1/64
//	!
package iosxr

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

// Parser handles Cisco IOS-XR configuration files.
type Parser struct{}

func (p *Parser) Vendor() string { return "Cisco-IOSXR" }

func (p *Parser) Detect(content string) bool {
	return strings.Contains(content, "IOS XR") ||
		strings.Contains(content, "IOS-XR") ||
		strings.Contains(content, "iosxr") ||
		(strings.Contains(content, "ipv4 address") && strings.Contains(content, "interface"))
}

// Parse extracts device data from a Cisco IOS-XR configuration.
func (p *Parser) Parse(content string) (*model.DeviceData, error) {
	device := &model.DeviceData{
		Vendor:   "Cisco",
		Platform: "IOS-XR",
	}

	lines := strings.Split(content, "\n")
	ifaceMap := make(map[string]*model.InterfaceData)
	var curIface *model.InterfaceData

	reHostname := regexp.MustCompile(`^hostname\s+(\S+)`)
	reVersion := regexp.MustCompile(`^\s*!! IOS XR Software, Version\s+(\S+)`)
	reInterface := regexp.MustCompile(`^(?:interface|interface preconfigure)\s+(\S+)`)
	reIPv4 := regexp.MustCompile(`^\s+ipv4 address\s+(\S+)\s+(\S+)`)
	reIPv6 := regexp.MustCompile(`^\s+ipv6 address\s+(\S+)`)
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
		if strings.HasPrefix(l, "!") {
			curIface = nil
			continue
		}
		if m := reInterface.FindStringSubmatch(l); m != nil {
			name := m[1]
			iface := &model.InterfaceData{
				Name:    name,
				Type:    iosxrInterfaceType(name),
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
		if strings.Contains(l, "shutdown") && !strings.Contains(l, "no shutdown") {
			curIface.Enabled = false
		}
	}

	for _, iface := range ifaceMap {
		device.Interfaces = append(device.Interfaces, *iface)
	}
	return device, nil
}

func iosxrInterfaceType(name string) string {
	ln := strings.ToLower(name)
	switch {
	case strings.HasPrefix(ln, "hundredgige"), strings.HasPrefix(ln, "hu"):
		return "100gbase-x-qsfp28"
	case strings.HasPrefix(ln, "fortygige"), strings.HasPrefix(ln, "fo"):
		return "40gbase-x-qsfpp"
	case strings.HasPrefix(ln, "tengige"), strings.HasPrefix(ln, "te"):
		return "10gbase-x-sfpp"
	case strings.HasPrefix(ln, "gigabitethernet"), strings.HasPrefix(ln, "gi"):
		return "1000base-t"
	case strings.HasPrefix(ln, "bundle-ether"):
		return "lag"
	case strings.HasPrefix(ln, "loopback"), strings.HasPrefix(ln, "lo"):
		return "virtual"
	case strings.HasPrefix(ln, "tunnel-ip"), strings.HasPrefix(ln, "tunnel-te"):
		return "virtual"
	case strings.HasPrefix(ln, "pos"):
		return "other"
	default:
		return "other"
	}
}

func maskToCIDR(addr, mask string) string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return ""
	}
	m := net.IPMask(net.ParseIP(mask).To4())
	ones, _ := m.Size()
	return fmt.Sprintf("%s/%d", ip.String(), ones)
}
