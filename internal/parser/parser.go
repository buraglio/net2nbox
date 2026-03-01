// Package parser provides the vendor-agnostic parsing interface and
// a global registry of vendor-specific implementations.
package parser

import (
	"fmt"
	"strings"

	"github.com/buraglio/net2nbox/internal/model"
)

// Parser is the interface all vendor-specific parsers must implement.
type Parser interface {
	// Vendor returns the canonical vendor name, e.g. "MikroTik".
	Vendor() string
	// Detect returns true if this parser is likely able to handle content.
	// Implementations should inspect the first few hundred bytes only.
	Detect(content string) bool
	// Parse extracts normalized device data from a raw configuration string.
	Parse(content string) (*model.DeviceData, error)
}

// Registry maps vendor keys to Parser instances.
type Registry struct {
	parsers map[string]Parser
}

// DefaultRegistry is the global parser registry populated by init() calls
// in each vendor sub-package.
var DefaultRegistry = NewRegistry()

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{parsers: make(map[string]Parser)}
}

// Register adds a Parser to the registry, keyed by its Vendor() value.
func (r *Registry) Register(p Parser) {
	r.parsers[strings.ToLower(p.Vendor())] = p
}

// Get retrieves a Parser by vendor key (case-insensitive).
func (r *Registry) Get(vendor string) (Parser, bool) {
	p, ok := r.parsers[strings.ToLower(vendor)]
	return p, ok
}

// Detect probes all registered parsers and returns the first that claims
// to handle the content. Returns nil if none match.
func (r *Registry) Detect(content string) Parser {
	for _, p := range r.parsers {
		if p.Detect(content) {
			return p
		}
	}
	return nil
}

// Names returns the list of registered vendor names.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.parsers))
	for k := range r.parsers {
		out = append(out, k)
	}
	return out
}

// ParseFile is a convenience wrapper that auto-detects or uses the named vendor.
// vendor may be empty to trigger auto-detection.
func (r *Registry) ParseFile(content, vendor string) (*model.DeviceData, error) {
	var p Parser
	if vendor != "" {
		var ok bool
		p, ok = r.Get(vendor)
		if !ok {
			return nil, fmt.Errorf("no parser registered for vendor %q (registered: %s)",
				vendor, strings.Join(r.Names(), ", "))
		}
	} else {
		p = r.Detect(content)
		if p == nil {
			return nil, fmt.Errorf("unable to auto-detect vendor from config content; specify --vendor explicitly")
		}
	}
	return p.Parse(content)
}
