package mikrotik

import (
	"reflect"
	"testing"

	"github.com/buraglio/net2nbox/internal/model"
)

// parseKV tests

func TestParseKV_Simple(t *testing.T) {
	got := parseKV("name=ether1 disabled=no mtu=1500")
	want := map[string]string{"name": "ether1", "disabled": "no", "mtu": "1500"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseKV_QuotedValue(t *testing.T) {
	got := parseKV(`comment="WAN uplink" name=wan`)
	want := map[string]string{"comment": "WAN uplink", "name": "wan"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseKV_IPAddress(t *testing.T) {
	got := parseKV("address=192.168.1.1/24 interface=bridge")
	want := map[string]string{"address": "192.168.1.1/24", "interface": "bridge"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseKV_MACAddress(t *testing.T) {
	got := parseKV("admin-mac=D4:01:C3:11:22:33 auto-mac=no")
	want := map[string]string{"admin-mac": "D4:01:C3:11:22:33", "auto-mac": "no"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseKV_Empty(t *testing.T) {
	got := parseKV("")
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParseKV_QuotedWithEscape(t *testing.T) {
	got := parseKV(`comment="link \"to\" ISP" name=wan`)
	if got["comment"] != `link "to" ISP` {
		t.Errorf("expected escaped quote handling, got comment=%q", got["comment"])
	}
}

func TestParseKV_CommaSeparatedSlaves(t *testing.T) {
	got := parseKV("name=bond0 slaves=ether1,ether2,ether3 mode=802.3ad")
	want := map[string]string{"name": "bond0", "slaves": "ether1,ether2,ether3", "mode": "802.3ad"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// parseLine tests

func TestParseLine_Add(t *testing.T) {
	cmd := parseLine("add name=ether1 disabled=no")
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.typ != "add" {
		t.Errorf("type: got %q, want %q", cmd.typ, "add")
	}
	if cmd.params["name"] != "ether1" {
		t.Errorf("name: got %q, want %q", cmd.params["name"], "ether1")
	}
	if len(cmd.selector) != 0 {
		t.Errorf("expected empty selector, got %v", cmd.selector)
	}
}

func TestParseLine_SetWithSelector(t *testing.T) {
	cmd := parseLine(`set [ find default-name=ether1 ] comment="WAN" disabled=no`)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.typ != "set" {
		t.Errorf("type: got %q, want %q", cmd.typ, "set")
	}
	if cmd.selector["default-name"] != "ether1" {
		t.Errorf("selector default-name: got %q, want %q", cmd.selector["default-name"], "ether1")
	}
	if cmd.params["comment"] != "WAN" {
		t.Errorf("comment: got %q, want %q", cmd.params["comment"], "WAN")
	}
}

func TestParseLine_SetWithRename(t *testing.T) {
	cmd := parseLine(`set [ find default-name=sfp-sfpplus1 ] name=sfp-uplink comment="Fiber"`)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.params["name"] != "sfp-uplink" {
		t.Errorf("name: got %q, want %q", cmd.params["name"], "sfp-uplink")
	}
	if cmd.selector["default-name"] != "sfp-sfpplus1" {
		t.Errorf("selector: got %q, want %q", cmd.selector["default-name"], "sfp-sfpplus1")
	}
}

func TestParseLine_UnknownCommand(t *testing.T) {
	cmd := parseLine("/ip address")
	if cmd != nil {
		t.Errorf("expected nil for section path line, got %v", cmd)
	}
}

func TestParseLine_CommentLine(t *testing.T) {
	if parseLine("# this is a comment") != nil {
		t.Error("expected nil for comment line")
	}
}

// inferType tests

func TestInferType(t *testing.T) {
	tests := []struct {
		name     string
		wantType string
	}{
		{"ether1", "1000base-t"},
		{"ether2", "1000base-t"},
		{"sfp1", "1000base-x-sfp"},
		{"sfp-sfpplus1", "10gbase-x-sfpp"},
		{"sfpplus1", "10gbase-x-sfpp"},
		{"sfp28-1", "25gbase-x-sfp28"},
		{"qsfp1", "40gbase-x-qsfpp"},
		{"qsfpplus1", "40gbase-x-qsfpp"},
		{"qsfp28-1", "100gbase-x-qsfp28"},
		{"bridge", "bridge"},
		{"bridge1", "bridge"},
		{"vlan10", "virtual"},
		{"vlan100", "virtual"},
		{"bond0", "lag"},
		{"wlan1", "ieee802.11ax"},
		{"wifi0", "ieee802.11ax"},
		{"lte1", "lte"},
		{"lo", "virtual"},
		{"eoip1", "virtual"},
		{"gre1", "virtual"},
		{"wireguard1", "virtual"},
		{"pppoe-out1", "virtual"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferType(tt.name)
			if got != tt.wantType {
				t.Errorf("inferType(%q) = %q, want %q", tt.name, got, tt.wantType)
			}
		})
	}
}

// normalizeCIDR tests

func TestNormalizeCIDR(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"192.168.1.1/24", "192.168.1.1/24"},
		{"203.0.113.1/30", "203.0.113.1/30"},
		{"10.0.0.1/32", "10.0.0.1/32"},
		{"2001:db8::1/64", "2001:db8::1/64"},
		{"fe80::1/64", "fe80::1/64"},
		{"fd00::/64", "fd00::/64"},
		{"", ""},
		{"invalid/24", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeCIDR(tt.input)
			if got != tt.want {
				t.Errorf("normalizeCIDR(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// normalizeMAC tests

func TestNormalizeMAC(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"D4:01:C3:11:22:33", "D4:01:C3:11:22:33"},
		{"d4:01:c3:11:22:33", "D4:01:C3:11:22:33"},
		{"d4-01-c3-11-22-33", "D4:01:C3:11:22:33"},
		{"d401c3112233", "D4:01:C3:11:22:33"},
		{"D401C3112233", "D4:01:C3:11:22:33"},
		{"00:00:00:00:00:00", "00:00:00:00:00:00"},
		{"FF:FF:FF:FF:FF:FF", "FF:FF:FF:FF:FF:FF"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeMAC(tt.input)
			if got != tt.want {
				t.Errorf("normalizeMAC(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// joinContinuationLines tests

func TestJoinContinuationLines_NoBackslash(t *testing.T) {
	in := []string{"line1", "line2", "line3"}
	got := joinContinuationLines(in)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("got %v, want %v", got, in)
	}
}

func TestJoinContinuationLines_WithBackslash(t *testing.T) {
	in := []string{`add name=foo \`, `    bar=baz`}
	got := joinContinuationLines(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(got), got)
	}
	want := "add name=foo     bar=baz"
	if got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
}

// Full Parse() integration tests

func getIface(t *testing.T, ifaces []model.InterfaceData, name string) *model.InterfaceData {
	t.Helper()
	for i := range ifaces {
		if ifaces[i].Name == name {
			return &ifaces[i]
		}
	}
	return nil
}

func getIP(iface *model.InterfaceData, addr string) *model.IPAddressData {
	for i := range iface.IPAddresses {
		if iface.IPAddresses[i].Address == addr {
			return &iface.IPAddresses[i]
		}
	}
	return nil
}

const minimalConfig = `# feb/28/2026 12:00:00 by RouterOS 7.15.2
# software id = ABCD-1234
## model = RB4011iGS+RM
# serial number = AB1234567890

/system identity
set name=my-router-01

/ip address
add address=192.168.1.1/24 interface=ether1 network=192.168.1.0
`

func TestParse_HeaderFields(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(minimalConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	checks := []struct {
		got, want, field string
	}{
		{d.OSVersion, "7.15.2", "OSVersion"},
		{d.SoftwareID, "ABCD-1234", "SoftwareID"},
		{d.Model, "RB4011iGS+RM", "Model"},
		{d.SerialNumber, "AB1234567890", "SerialNumber"},
		{d.Vendor, "MikroTik", "Vendor"},
		{d.Platform, "RouterOS", "Platform"},
		{d.Hostname, "my-router-01", "Hostname"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.field, c.got, c.want)
		}
	}
}

func TestParse_IPv4Assignment(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(minimalConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	iface := getIface(t, d.Interfaces, "ether1")
	if iface == nil {
		t.Fatal("interface ether1 not found")
	}
	if iface.Type != "1000base-t" {
		t.Errorf("type: got %q, want %q", iface.Type, "1000base-t")
	}
	ip := getIP(iface, "192.168.1.1/24")
	if ip == nil {
		t.Error("IP 192.168.1.1/24 not found on ether1")
	} else if ip.Family != 4 {
		t.Errorf("family: got %d, want 4", ip.Family)
	}
}

const fullConfig = `# feb/28/2026 12:00:00 by RouterOS 7.15.2
# software id = ABCD-1234
## model = RB4011iGS+RM
# serial number = AB1234567890

/interface bridge
add admin-mac=D4:01:C3:11:22:33 auto-mac=no comment=defconf name=bridge

/interface ethernet
set [ find default-name=ether1 ] comment="WAN uplink"
set [ find default-name=ether3 ] disabled=yes
set [ find default-name=sfp-sfpplus1 ] comment="Fiber uplink" name=sfp-uplink

/interface vlan
add interface=bridge name=vlan10 vlan-id=10
add interface=bridge name=vlan20 vlan-id=20

/interface bonding
add mode=802.3ad name=bond0 slaves=ether2,ether3

/interface bridge port
add bridge=bridge interface=ether2
add bridge=bridge interface=ether3

/ip address
add address=203.0.113.1/30 comment="WAN" interface=ether1 network=203.0.113.0
add address=192.168.88.1/24 comment=defconf interface=bridge network=192.168.88.0
add address=10.10.10.1/24 interface=vlan10 network=10.10.10.0

/ipv6 address
add address=2001:db8:1::1/64 advertise=yes comment="WAN IPv6" interface=ether1
add address=fe80::1/64 advertise=no interface=ether1
add address=fd00:88::/64 advertise=yes interface=bridge

/system identity
set name=my-router-01
`

func TestParse_BridgeInterface(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(fullConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	bridge := getIface(t, d.Interfaces, "bridge")
	if bridge == nil {
		t.Fatal("interface bridge not found")
	}
	if bridge.Type != "bridge" {
		t.Errorf("type: got %q, want %q", bridge.Type, "bridge")
	}
	if bridge.MACAddress != "D4:01:C3:11:22:33" {
		t.Errorf("mac: got %q, want %q", bridge.MACAddress, "D4:01:C3:11:22:33")
	}
	if bridge.Description != "defconf" {
		t.Errorf("description: got %q, want %q", bridge.Description, "defconf")
	}
	if len(bridge.BridgeMembers) != 2 {
		t.Errorf("bridge members: got %v, want [ether2 ether3]", bridge.BridgeMembers)
	}
}

func TestParse_EthernetDescription(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(fullConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	ether1 := getIface(t, d.Interfaces, "ether1")
	if ether1 == nil {
		t.Fatal("interface ether1 not found")
	}
	if ether1.Description != "WAN uplink" {
		t.Errorf("description: got %q, want %q", ether1.Description, "WAN uplink")
	}
	if !ether1.Enabled {
		t.Error("ether1 should be enabled")
	}
}

func TestParse_DisabledInterface(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(fullConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	ether3 := getIface(t, d.Interfaces, "ether3")
	if ether3 == nil {
		t.Fatal("interface ether3 not found")
	}
	if ether3.Enabled {
		t.Error("ether3 should be disabled")
	}
}

func TestParse_InterfaceRename(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(fullConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	// sfp-sfpplus1 was renamed to sfp-uplink
	if getIface(t, d.Interfaces, "sfp-sfpplus1") != nil {
		t.Error("original name sfp-sfpplus1 should not appear after rename")
	}
	sfp := getIface(t, d.Interfaces, "sfp-uplink")
	if sfp == nil {
		t.Fatal("renamed interface sfp-uplink not found")
	}
	if sfp.Type != "1000base-x-sfp" {
		t.Errorf("type: got %q, want %q", sfp.Type, "1000base-x-sfp")
	}
	if sfp.Description != "Fiber uplink" {
		t.Errorf("description: got %q, want %q", sfp.Description, "Fiber uplink")
	}
}

func TestParse_VLANInterface(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(fullConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	vlan10 := getIface(t, d.Interfaces, "vlan10")
	if vlan10 == nil {
		t.Fatal("interface vlan10 not found")
	}
	if vlan10.Type != "virtual" {
		t.Errorf("type: got %q, want %q", vlan10.Type, "virtual")
	}
	if vlan10.ParentInterface != "bridge" {
		t.Errorf("parent: got %q, want %q", vlan10.ParentInterface, "bridge")
	}
	if vlan10.VLANid != 10 {
		t.Errorf("vlan-id: got %d, want 10", vlan10.VLANid)
	}
}

func TestParse_BondingInterface(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(fullConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	bond := getIface(t, d.Interfaces, "bond0")
	if bond == nil {
		t.Fatal("interface bond0 not found")
	}
	if bond.Type != "lag" {
		t.Errorf("type: got %q, want %q", bond.Type, "lag")
	}
	members := map[string]bool{}
	for _, m := range bond.BondMembers {
		members[m] = true
	}
	for _, want := range []string{"ether2", "ether3"} {
		if !members[want] {
			t.Errorf("bond member %q missing, got %v", want, bond.BondMembers)
		}
	}
}

func TestParse_IPv4OnEther1(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(fullConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	ether1 := getIface(t, d.Interfaces, "ether1")
	if ether1 == nil {
		t.Fatal("ether1 not found")
	}
	ip := getIP(ether1, "203.0.113.1/30")
	if ip == nil {
		t.Fatal("203.0.113.1/30 not found on ether1")
	}
	if ip.Family != 4 {
		t.Errorf("family: got %d, want 4", ip.Family)
	}
	if ip.Description != "WAN" {
		t.Errorf("description: got %q, want %q", ip.Description, "WAN")
	}
}

func TestParse_IPv6OnEther1(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(fullConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	ether1 := getIface(t, d.Interfaces, "ether1")
	if ether1 == nil {
		t.Fatal("ether1 not found")
	}
	ip := getIP(ether1, "2001:db8:1::1/64")
	if ip == nil {
		t.Fatal("2001:db8:1::1/64 not found on ether1")
	}
	if ip.Family != 6 {
		t.Errorf("family: got %d, want 6", ip.Family)
	}
}

func TestParse_LinkLocalIPv6Present(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(fullConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	ether1 := getIface(t, d.Interfaces, "ether1")
	if ether1 == nil {
		t.Fatal("ether1 not found")
	}
	// Link-local should be present in the parsed data (filtering is importer's job)
	if getIP(ether1, "fe80::1/64") == nil {
		t.Error("link-local fe80::1/64 should be present in parsed output")
	}
}

func TestParse_VLANIPAssignment(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(fullConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	vlan10 := getIface(t, d.Interfaces, "vlan10")
	if vlan10 == nil {
		t.Fatal("vlan10 not found")
	}
	if getIP(vlan10, "10.10.10.1/24") == nil {
		t.Error("10.10.10.1/24 not found on vlan10")
	}
}

func TestParse_InterfaceCount(t *testing.T) {
	p := &Parser{}
	d, err := p.Parse(fullConfig)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	// bridge, ether1, ether2, ether3, sfp-uplink, vlan10, vlan20, bond0
	if len(d.Interfaces) != 8 {
		names := make([]string, len(d.Interfaces))
		for i, iface := range d.Interfaces {
			names[i] = iface.Name
		}
		t.Errorf("expected 8 interfaces, got %d: %v", len(d.Interfaces), names)
	}
}

func TestDetect_RouterOS(t *testing.T) {
	p := &Parser{}
	if !p.Detect("# feb/28/2026 by RouterOS 7.15.2\n/interface ethernet\n") {
		t.Error("Detect should return true for RouterOS config")
	}
}

func TestDetect_NonRouterOS(t *testing.T) {
	p := &Parser{}
	if p.Detect("hostname cisco-router\ninterface GigabitEthernet0/0/0\n") {
		t.Error("Detect should return false for Cisco IOS config")
	}
}
