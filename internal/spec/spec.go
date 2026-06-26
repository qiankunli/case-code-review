// Package spec consumes spec.json — the generated artifact (produced by specgen
// from +spec/+case markers) that binds a contract and its cases to a code
// symbol by unit-id. ccr injects the matching spec/case as the review's
// contract checklist: the change must be shown not to break it.
package spec

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Case is one scenario hanging off a symbol's spec — a review checklist item.
// Input/Expect/Forbid are the black-box run fields; review uses ID/Desc (and
// Expect/Forbid as constraints to check the change preserves).
type Case struct {
	ID     string `json:"id"`
	Desc   string `json:"desc,omitempty"`
	Input  string `json:"input,omitempty"`
	Expect string `json:"expect,omitempty"`
	Forbid string `json:"forbid,omitempty"`
}

// Entry is the spec + cases bound to one code symbol.
type Entry struct {
	Spec  string `json:"spec,omitempty"`
	Cases []Case `json:"cases,omitempty"`
}

// Index is spec.json: unit-id (<relpath>::<symbol>) -> Entry.
type Index map[string]Entry

// Parse decodes spec.json bytes.
func Parse(data []byte) (Index, error) {
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse spec.json: %w", err)
	}
	return idx, nil
}

// Load reads and parses spec.json from path.
func Load(path string) (Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Render returns the contract checklist for the given symbols (a review Unit's
// covered functions), or "" if none of them carry spec/case. Safe on a nil
// Index. The text is what the review must verify the change preserves.
func (idx Index) Render(symbols []string) string {
	var b strings.Builder
	for _, sym := range symbols {
		e, ok := idx[sym]
		if !ok || (e.Spec == "" && len(e.Cases) == 0) {
			continue
		}
		fmt.Fprintf(&b, "%s\n", sym)
		if e.Spec != "" {
			fmt.Fprintf(&b, "  spec: %s\n", e.Spec)
		}
		for _, c := range e.Cases {
			b.WriteString("  - " + c.ID)
			if c.Desc != "" {
				b.WriteString(": " + c.Desc)
			}
			if c.Expect != "" {
				b.WriteString(" [expect: " + c.Expect + "]")
			}
			if c.Forbid != "" {
				b.WriteString(" [forbid: " + c.Forbid + "]")
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
