package iosxe

import (
	"testing"
)

const iosxeConfig = `! Cisco IOS XE Software, Version 17.9.3a
hostname edge-router-01
!
interface GigabitEthernet0/0/0
 description WAN to ISP
 ip address 203.0.113.1 255.255.255.252
 ipv6 address 2001:db8::1/64
 no shutdown
!
interface GigabitEthernet0/0/1
 description LAN segment
 ip address 192.168.1.1 255.255.255.0
 no shutdown
!
interface GigabitEthernet0/0/2
 description unused
 shutdown
!
interface Loopback0
 ip address 10.0.0.1 255.255.255.255
!
interface Port-channel1
 description LAG uplink
 ip address 10.1.1.1 255.255.255.252
!
`

func TestDetect_IosxeTrue(t *testing.T) {
	p := &Parser{}
	if !p.Detect("! Cisco IOS XE Software, Version 17.9.3a\nhostname r1\n") {
		t.Error("expected Detect true for IOS XE content")
	}
}

func TestDetect_IosxeFalseForXR(t *testing.T) {
	p := &Parser{}
	if p.Detect("!! IOS XR Software, Version 7.5.4\nhostname r1\n") {
		t.Error("IOS-XE Detect should return false for IOS XR content")
	}
}

func TestDetect_IosxeFalseForRouterOS(t *testing.T) {
	p := &Parser{}
	if p.Detect("# by RouterOS 7.15\n/interface ethernet\n") {
		t.Error("IOS-XE Detect should return false for RouterOS content")
	}
}

func TestParse_Hostname(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(iosxeConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if d.Hostname != "edge-router-01" {
		t.Errorf("Hostname: got %q, want %q", d.Hostname, "edge-router-01")
	}
}

func TestParse_VendorPlatform(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(iosxeConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if d.Vendor != "Cisco" {
		t.Errorf("Vendor: got %q, want %q", d.Vendor, "Cisco")
	}
	if d.Platform != "IOS-XE" {
		t.Errorf("Platform: got %q, want %q", d.Platform, "IOS-XE")
	}
}

func TestParse_InterfaceCount(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(iosxeConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	// GE0/0/0, GE0/0/1, GE0/0/2, Loopback0, Port-channel1
	if len(d.Interfaces) != 5 {
		names := make([]string, len(d.Interfaces))
		for i, iface := range d.Interfaces {
			names[i] = iface.Name
		}
		t.Errorf("expected 5 interfaces, got %d: %v", len(d.Interfaces), names)
	}
}

func TestParse_InterfaceTypes(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(iosxeConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	typeByName := make(map[string]string)
	for _, iface := range d.Interfaces {
		typeByName[iface.Name] = iface.Type
	}
	checks := []struct{ name, want string }{
		{"GigabitEthernet0/0/0", "1000base-t"},
		{"Loopback0", "virtual"},
		{"Port-channel1", "lag"},
	}
	for _, c := range checks {
		if got := typeByName[c.name]; got != c.want {
			t.Errorf("type of %s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParse_IPv4WithMask(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(iosxeConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	var ge0 *struct{ ips []string }
	for _, iface := range d.Interfaces {
		if iface.Name == "GigabitEthernet0/0/0" {
			ips := make([]string, len(iface.IPAddresses))
			for i, ip := range iface.IPAddresses {
				ips[i] = ip.Address
			}
			ge0 = &struct{ ips []string }{ips}
		}
	}
	if ge0 == nil {
		t.Fatal("GigabitEthernet0/0/0 not found")
	}
	found := false
	for _, addr := range ge0.ips {
		if addr == "203.0.113.1/30" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 203.0.113.1/30 (converted from /252 mask), got %v", ge0.ips)
	}
}

func TestParse_IPv6(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(iosxeConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	for _, iface := range d.Interfaces {
		if iface.Name != "GigabitEthernet0/0/0" {
			continue
		}
		for _, ip := range iface.IPAddresses {
			if ip.Address == "2001:db8::1/64" && ip.Family == 6 {
				return
			}
		}
		t.Error("2001:db8::1/64 not found on GigabitEthernet0/0/0")
	}
}

func TestParse_ShutdownInterface(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(iosxeConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	for _, iface := range d.Interfaces {
		if iface.Name == "GigabitEthernet0/0/2" {
			if iface.Enabled {
				t.Error("shutdown interface should have Enabled=false")
			}
			return
		}
	}
	t.Fatal("GigabitEthernet0/0/2 not found")
}

func TestParse_Description(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(iosxeConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	for _, iface := range d.Interfaces {
		if iface.Name == "GigabitEthernet0/0/0" {
			if iface.Description != "WAN to ISP" {
				t.Errorf("Description: got %q, want %q", iface.Description, "WAN to ISP")
			}
			return
		}
	}
	t.Fatal("GigabitEthernet0/0/0 not found")
}

func TestMaskToCIDR(t *testing.T) {
	tests := []struct {
		addr, mask, want string
	}{
		{"203.0.113.1", "255.255.255.252", "203.0.113.1/30"},
		{"192.168.1.1", "255.255.255.0", "192.168.1.1/24"},
		{"10.0.0.1", "255.255.255.255", "10.0.0.1/32"},
		{"10.0.0.0", "255.0.0.0", "10.0.0.0/8"},
		{"invalid", "255.255.255.0", ""},
	}
	for _, tt := range tests {
		got := maskToCIDR(tt.addr, tt.mask)
		if got != tt.want {
			t.Errorf("maskToCIDR(%q, %q) = %q, want %q", tt.addr, tt.mask, got, tt.want)
		}
	}
}

func TestIosxeInterfaceType(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"GigabitEthernet0/0/0", "1000base-t"},
		{"TenGigabitEthernet0/0/0", "10gbase-x-sfpp"},
		{"HundredGigE0/0/0", "100gbase-x-qsfp28"},
		{"Port-channel1", "lag"},
		{"Loopback0", "virtual"},
		{"Vlan10", "virtual"},
		{"Tunnel1", "virtual"},
		{"Serial0/0/0", "other"},
	}
	for _, tt := range tests {
		got := iosxeInterfaceType(tt.name)
		if got != tt.want {
			t.Errorf("iosxeInterfaceType(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
