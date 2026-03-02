// Package model defines the normalized internal representation of network
// device data extracted from vendor configuration files.
package model

import "fmt"

// DeviceData is the normalized representation of a network device
// extracted from a vendor configuration file.
type DeviceData struct {
	Hostname     string
	Vendor       string // e.g. "MikroTik", "Cisco", "Juniper", "Arista", "FS"
	Model        string // e.g. "RB4011iGS+RM", "ISR4451", "MX480"
	SerialNumber string
	OSVersion    string
	Platform     string // e.g. "RouterOS", "IOS-XE", "IOS-XR", "JunOS", "EOS", "FSOS"
	SoftwareID   string // vendor-specific build/software identifier
	Interfaces   []InterfaceData
	Tags         []string
}

// Summary returns a one-line human-readable description of the device.
func (d *DeviceData) Summary() string {
	return fmt.Sprintf("%s %s — hostname=%q serial=%q interfaces=%d",
		d.Vendor, d.Model, d.Hostname, d.SerialNumber, len(d.Interfaces))
}

// InterfaceData represents a single network interface on the device.
type InterfaceData struct {
	Name            string
	Type            string // NetBox interface type slug (e.g. "1000base-t", "virtual", "lag")
	Description     string
	MACAddress      string
	MTU             int
	Enabled         bool
	IPAddresses     []IPAddressData
	LLDPNeighbors   []LLDPNeighbor // LLDP-discovered neighbors (populated by SNMP)
	VLANMode        string         // "access", "tagged", or "tagged-all"
	AccessVLAN      int            // 802.1Q access VLAN ID
	TaggedVLANs     []int          // 802.1Q trunk VLAN IDs
	ParentInterface string         // parent interface name for sub-interfaces / VLANs
	VLANid          int            // VLAN ID for VLAN-type interfaces
	BondMembers     []string       // member interface names for LAG
	BridgeMembers   []string       // member interface names for bridge
}

// LLDPNeighbor records a single LLDP neighbor seen on an interface.
type LLDPNeighbor struct {
	RemoteSysName  string // LLDP system name (typically the neighbor hostname)
	RemotePortID   string // LLDP port ID (typically the neighbor interface name)
	RemotePortDesc string // LLDP port description (ifAlias or ifDescr)
}

// IPAddressData represents an IP address assignment on an interface.
type IPAddressData struct {
	Address     string // CIDR notation: "192.168.1.1/24" or "2001:db8::1/64"
	Family      int    // 4 or 6
	Description string
	Primary     bool // suggest as device primary IP in NetBox
}
