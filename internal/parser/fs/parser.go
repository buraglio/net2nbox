// Package fs implements a parser for FS.com FSOS configuration files.
//
// FSOS (used on FS N-Series and S-Series switches) has a configuration
// language similar to Cisco IOS. Interface names follow the pattern
// "GigabitEthernet1/0/1" or "TenGigabitEthernet1/0/1".
//
// Typical FSOS config excerpt:
//
//	hostname fs-switch-01
//	!
//	interface GigabitEthernet1/0/1
//	 description Uplink
//	 ip address 192.168.1.1 255.255.255.0
//	 no shutdown
//	!
package fs

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

// Parser handles FS.com FSOS configuration files.
type Parser struct{}

func (p *Parser) Vendor() string { return "FS" }

func (p *Parser) Detect(content string) bool {
	return strings.Contains(content, "FSOS") ||
		strings.Contains(content, "FS.com") ||
		(strings.Contains(content, "interface GigabitEthernet") &&
			strings.Contains(content, "interface TenGigabitEthernet"))
}

// Parse extracts device data from an FSOS configuration.
func (p *Parser) Parse(content string) (*model.DeviceData, error) {
	device := &model.DeviceData{
		Vendor:   "FS",
		Platform: "FSOS",
	}

	lines := strings.Split(content, "\n")
	ifaceMap := make(map[string]*model.InterfaceData)
	var curIface *model.InterfaceData

	reHostname := regexp.MustCompile(`^hostname\s+(\S+)`)
	reVersion := regexp.MustCompile(`^!FSOS Version\s+(\S+)`)
	reSerial := regexp.MustCompile(`^!System serial number\s+:\s+(\S+)`)
	reInterface := regexp.MustCompile(`^interface\s+(\S+)`)
	reIPv4 := regexp.MustCompile(`^\s+ip address\s+(\S+)\s+(\S+)`)
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
				Type:    fsInterfaceType(name),
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

func fsInterfaceType(name string) string {
	ln := strings.ToLower(name)
	switch {
	case strings.HasPrefix(ln, "hundredgigabitethernet"):
		return "100gbase-x-qsfp28"
	case strings.HasPrefix(ln, "fortygigabitethernet"):
		return "40gbase-x-qsfpp"
	case strings.HasPrefix(ln, "twentyfivegigabitethernet"):
		return "25gbase-x-sfp28"
	case strings.HasPrefix(ln, "tengigabitethernet"):
		return "10gbase-x-sfpp"
	case strings.HasPrefix(ln, "gigabitethernet"):
		return "1000base-t"
	case strings.HasPrefix(ln, "fastethernet"):
		return "100base-tx"
	case strings.HasPrefix(ln, "loopback"):
		return "virtual"
	case strings.HasPrefix(ln, "vlan"):
		return "virtual"
	case strings.HasPrefix(ln, "port-channel"):
		return "lag"
	case strings.HasPrefix(ln, "managementethernet"):
		return "1000base-t"
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
