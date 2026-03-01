package parser_test

import (
	"testing"

	"github.com/buraglio/net2nbox/internal/model"
	"github.com/buraglio/net2nbox/internal/parser"

	// Trigger vendor registrations so DefaultRegistry is populated.
	_ "github.com/buraglio/net2nbox/internal/parser/arista"
	_ "github.com/buraglio/net2nbox/internal/parser/cisco/iosxe"
	_ "github.com/buraglio/net2nbox/internal/parser/cisco/iosxr"
	_ "github.com/buraglio/net2nbox/internal/parser/fs"
	_ "github.com/buraglio/net2nbox/internal/parser/junos"
	_ "github.com/buraglio/net2nbox/internal/parser/mikrotik"
)

// mockParser is a minimal Parser implementation used to test registry mechanics.
type mockParser struct {
	vendor   string
	detected bool
}

func (m *mockParser) Vendor() string                              { return m.vendor }
func (m *mockParser) Detect(content string) bool                 { return m.detected }
func (m *mockParser) Parse(_ string) (*model.DeviceData, error)  {
	return &model.DeviceData{Vendor: m.vendor}, nil
}

func TestRegistry_Register(t *testing.T) {
	r := parser.NewRegistry()
	r.Register(&mockParser{vendor: "TestVendor"})
	_, ok := r.Get("testvendor")
	if !ok {
		t.Error("expected to find registered parser under lowercase key")
	}
}

func TestRegistry_Get_CaseInsensitive(t *testing.T) {
	r := parser.NewRegistry()
	r.Register(&mockParser{vendor: "MyVendor"})

	for _, key := range []string{"myvendor", "MyVendor", "MYVENDOR"} {
		if _, ok := r.Get(key); !ok {
			t.Errorf("Get(%q) not found; expected case-insensitive lookup", key)
		}
	}
}

func TestRegistry_Get_Missing(t *testing.T) {
	r := parser.NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected false for unknown vendor")
	}
}

func TestRegistry_Detect_Match(t *testing.T) {
	r := parser.NewRegistry()
	r.Register(&mockParser{vendor: "Alpha", detected: false})
	r.Register(&mockParser{vendor: "Beta", detected: true})
	p := r.Detect("some config content")
	if p == nil {
		t.Fatal("Detect returned nil, expected a match")
	}
	if p.Vendor() != "Beta" {
		t.Errorf("Vendor: got %q, want %q", p.Vendor(), "Beta")
	}
}

func TestRegistry_Detect_NoMatch(t *testing.T) {
	r := parser.NewRegistry()
	r.Register(&mockParser{vendor: "Alpha", detected: false})
	if r.Detect("some content") != nil {
		t.Error("expected nil when no parser matches")
	}
}

func TestRegistry_ParseFile_ByVendor(t *testing.T) {
	r := parser.NewRegistry()
	r.Register(&mockParser{vendor: "ExplicitVendor"})
	d, err := r.ParseFile("anything", "ExplicitVendor")
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}
	if d.Vendor != "ExplicitVendor" {
		t.Errorf("Vendor: got %q, want %q", d.Vendor, "ExplicitVendor")
	}
}

func TestRegistry_ParseFile_UnknownVendor(t *testing.T) {
	r := parser.NewRegistry()
	_, err := r.ParseFile("content", "noSuchVendor")
	if err == nil {
		t.Error("expected error for unknown vendor, got nil")
	}
}

func TestRegistry_ParseFile_AutoDetect(t *testing.T) {
	r := parser.NewRegistry()
	r.Register(&mockParser{vendor: "AutoVendor", detected: true})
	d, err := r.ParseFile("any content", "")
	if err != nil {
		t.Fatalf("ParseFile error: %v", err)
	}
	if d.Vendor != "AutoVendor" {
		t.Errorf("Vendor: got %q, want %q", d.Vendor, "AutoVendor")
	}
}

func TestRegistry_ParseFile_AutoDetect_NoMatch(t *testing.T) {
	r := parser.NewRegistry()
	r.Register(&mockParser{vendor: "SomeVendor", detected: false})
	_, err := r.ParseFile("unrecognized content", "")
	if err == nil {
		t.Error("expected error when auto-detect finds no match")
	}
}

func TestRegistry_Names(t *testing.T) {
	r := parser.NewRegistry()
	r.Register(&mockParser{vendor: "VendorA"})
	r.Register(&mockParser{vendor: "VendorB"})
	names := r.Names()
	if len(names) != 2 {
		t.Errorf("Names() returned %d entries, want 2", len(names))
	}
}

// DefaultRegistry should have all six vendors registered via init().
func TestDefaultRegistry_HasAllVendors(t *testing.T) {
	expected := []string{"mikrotik", "cisco", "cisco-iosxr", "juniper", "arista", "fs"}
	for _, v := range expected {
		if _, ok := parser.DefaultRegistry.Get(v); !ok {
			t.Errorf("DefaultRegistry missing vendor %q", v)
		}
	}
}

// Auto-detection smoke tests using DefaultRegistry.

func TestDefaultRegistry_DetectMikrotik(t *testing.T) {
	content := "# by RouterOS 7.15.2\n/interface ethernet\n"
	p := parser.DefaultRegistry.Detect(content)
	if p == nil {
		t.Fatal("Detect returned nil for RouterOS content")
	}
	if p.Vendor() != "MikroTik" {
		t.Errorf("expected MikroTik, got %q", p.Vendor())
	}
}

func TestDefaultRegistry_DetectArista(t *testing.T) {
	content := "! Arista EOS\nhostname leaf01\ninterface Ethernet1\n   ip address 10.0.0.1/31\n"
	p := parser.DefaultRegistry.Detect(content)
	if p == nil {
		t.Fatal("Detect returned nil for Arista EOS content")
	}
	if p.Vendor() != "Arista" {
		t.Errorf("expected Arista, got %q", p.Vendor())
	}
}

func TestDefaultRegistry_DetectJunos_SetFormat(t *testing.T) {
	content := "set system host-name router1\nset interfaces ge-0/0/0 unit 0 family inet address 203.0.113.1/30\n"
	p := parser.DefaultRegistry.Detect(content)
	if p == nil {
		t.Fatal("Detect returned nil for JunOS set-format content")
	}
	if p.Vendor() != "Juniper" {
		t.Errorf("expected Juniper, got %q", p.Vendor())
	}
}
