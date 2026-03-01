package junos

import (
	"testing"

	"github.com/buraglio/net2nbox/internal/model"
)

const junosSetConfig = `set system host-name core-router-01
set interfaces ge-0/0/0 description "WAN uplink"
set interfaces ge-0/0/0 unit 0 family inet address 203.0.113.1/30
set interfaces ge-0/0/0 unit 0 family inet6 address 2001:db8::1/64
set interfaces ge-0/0/1 description "LAN"
set interfaces ge-0/0/1 unit 0 family inet address 192.168.1.1/24
set interfaces lo0 unit 0 family inet address 10.0.0.1/32
set interfaces ae0 description "LAG uplink"
set interfaces ae0 unit 0 family inet address 10.1.1.1/30
set interfaces ge-0/0/2 disable
`

const junosHierConfig = `## Last changed: 2026-02-28
version 21.4R3;
system {
    host-name core-router-01;
}
interfaces {
    ge-0/0/0 {
        description "WAN uplink";
        unit 0 {
            family inet {
                address 203.0.113.1/30;
            }
            family inet6 {
                address 2001:db8::1/64;
            }
        }
    }
    ge-0/0/1 {
        description "LAN";
        unit 0 {
            family inet {
                address 192.168.1.1/24;
            }
        }
    }
    lo0 {
        unit 0 {
            family inet {
                address 10.0.0.1/32;
            }
        }
    }
    ge-0/0/2 {
        disable;
    }
}
`

func findIface(t *testing.T, ifaces []model.InterfaceData, name string) *model.InterfaceData {
	t.Helper()
	for i := range ifaces {
		if ifaces[i].Name == name {
			return &ifaces[i]
		}
	}
	return nil
}

func hasAddr(iface *model.InterfaceData, addr string) bool {
	for _, ip := range iface.IPAddresses {
		if ip.Address == addr {
			return true
		}
	}
	return false
}

func TestDetect_JunOS_SetFormat(t *testing.T) {
	p := &Parser{}
	if !p.Detect("set system host-name r1\nset interfaces ge-0/0/0 unit 0 family inet address 1.1.1.1/30\n") {
		t.Error("expected Detect true for JunOS set format")
	}
}

func TestDetect_JunOS_Hierarchical(t *testing.T) {
	p := &Parser{}
	content := "## Last changed:\ninterfaces {\n    ge-0/0/0 {\n        unit 0 {\n            family inet {\n"
	if !p.Detect(content) {
		t.Error("expected Detect true for JunOS hierarchical format")
	}
}

func TestDetect_JunOS_FalseForRouterOS(t *testing.T) {
	p := &Parser{}
	if p.Detect("# by RouterOS 7.15\n/interface ethernet\n") {
		t.Error("JunOS Detect should return false for RouterOS content")
	}
}

func TestParse_SetFormat_Hostname(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(junosSetConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if d.Hostname != "core-router-01" {
		t.Errorf("Hostname: got %q, want %q", d.Hostname, "core-router-01")
	}
}

func TestParse_SetFormat_VendorPlatform(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(junosSetConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if d.Vendor != "Juniper" {
		t.Errorf("Vendor: got %q, want %q", d.Vendor, "Juniper")
	}
	if d.Platform != "JunOS" {
		t.Errorf("Platform: got %q, want %q", d.Platform, "JunOS")
	}
}

func TestParse_SetFormat_IPv4(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(junosSetConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	iface := findIface(t, d.Interfaces, "ge-0/0/0")
	if iface == nil {
		t.Fatal("ge-0/0/0 not found")
	}
	if iface.Type != "1000base-t" {
		t.Errorf("type: got %q, want %q", iface.Type, "1000base-t")
	}
	if !hasAddr(iface, "203.0.113.1/30") {
		t.Error("203.0.113.1/30 not found on ge-0/0/0")
	}
}

func TestParse_SetFormat_IPv6(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(junosSetConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	iface := findIface(t, d.Interfaces, "ge-0/0/0")
	if iface == nil {
		t.Fatal("ge-0/0/0 not found")
	}
	if !hasAddr(iface, "2001:db8::1/64") {
		t.Error("2001:db8::1/64 not found on ge-0/0/0")
	}
}

func TestParse_SetFormat_Description(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(junosSetConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	iface := findIface(t, d.Interfaces, "ge-0/0/0")
	if iface == nil {
		t.Fatal("ge-0/0/0 not found")
	}
	if iface.Description != "WAN uplink" {
		t.Errorf("Description: got %q, want %q", iface.Description, "WAN uplink")
	}
}

func TestParse_SetFormat_LAGType(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(junosSetConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	iface := findIface(t, d.Interfaces, "ae0")
	if iface == nil {
		t.Fatal("ae0 not found")
	}
	if iface.Type != "lag" {
		t.Errorf("type: got %q, want %q", iface.Type, "lag")
	}
}

func TestParse_SetFormat_LoopbackType(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(junosSetConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	iface := findIface(t, d.Interfaces, "lo0")
	if iface == nil {
		t.Fatal("lo0 not found")
	}
	if iface.Type != "virtual" {
		t.Errorf("type: got %q, want %q", iface.Type, "virtual")
	}
}

func TestParse_SetFormat_DisabledInterface(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(junosSetConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	iface := findIface(t, d.Interfaces, "ge-0/0/2")
	if iface == nil {
		t.Fatal("ge-0/0/2 not found")
	}
	if iface.Enabled {
		t.Error("ge-0/0/2 should be disabled")
	}
}

func TestParse_Hierarchical_Hostname(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(junosHierConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if d.Hostname != "core-router-01" {
		t.Errorf("Hostname: got %q, want %q", d.Hostname, "core-router-01")
	}
}

func TestParse_Hierarchical_OSVersion(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(junosHierConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if d.OSVersion != "21.4R3" {
		t.Errorf("OSVersion: got %q, want %q", d.OSVersion, "21.4R3")
	}
}

func TestParse_Hierarchical_IPv4(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(junosHierConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	iface := findIface(t, d.Interfaces, "ge-0/0/0")
	if iface == nil {
		t.Fatal("ge-0/0/0 not found")
	}
	if !hasAddr(iface, "203.0.113.1/30") {
		var addrs []string
		for _, ip := range iface.IPAddresses {
			addrs = append(addrs, ip.Address)
		}
		t.Errorf("203.0.113.1/30 not found; got: %v", addrs)
	}
}

func TestParse_Hierarchical_IPv6(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(junosHierConfig)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	iface := findIface(t, d.Interfaces, "ge-0/0/0")
	if iface == nil {
		t.Fatal("ge-0/0/0 not found")
	}
	if !hasAddr(iface, "2001:db8::1/64") {
		t.Error("2001:db8::1/64 not found on ge-0/0/0")
	}
}

func TestJunosInterfaceType(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"ge-0/0/0", "1000base-t"},
		{"xe-0/0/0", "10gbase-x-sfpp"},
		{"et-0/0/0", "100gbase-x-qsfp28"},
		{"ae0", "lag"},
		{"ae1", "lag"},
		{"lo0", "virtual"},
		{"gr-0/0/0", "virtual"},
	}
	for _, tt := range tests {
		got := junosInterfaceType(tt.name)
		if got != tt.want {
			t.Errorf("junosInterfaceType(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
