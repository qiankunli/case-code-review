// Package spec consumes spec.json — the generated artifact (produced by specgen
// from +spec/+case markers) that binds a contract and its cases to a code
// symbol by symbol-id. ccr injects the matching spec/case as the review's
// contract checklist: the change must be shown not to break it.
package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
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
	// Fqn is the symbol's language-native fully-qualified name (Python dotted
	// import path, Go importpath.Symbol). Present on entries from dependency
	// spec.json; the cross-repo identity a reference resolves to. May be empty.
	Fqn   string   `json:"fqn,omitempty"`
	Spec  string   `json:"spec,omitempty"`
	Cases []Case   `json:"cases,omitempty"`
	Links []string `json:"links,omitempty"`
	Rules []string `json:"rules,omitempty"`
}

// Index is spec.json: symbol-id (<relpath>::<symbol>) -> Entry.
type Index map[string]Entry

// Parse decodes spec.json bytes.
func Parse(data []byte) (Index, error) {
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse spec.json: %w", err)
	}
	return idx, nil
}

// Load reads spec.json from the priority chain and merges the layers, mirroring
// how review rules are loaded:
//
//  1. customPath (--spec)        — highest
//  2. <repoDir>/.casecodereview/spec.json   — project
//  3. ~/.casecodereview/spec.json           — global (lowest)
//
// Higher layers override same-keyed (symbol-id) entries. Project/global layers are
// optional (skipped if absent); a non-empty customPath that is missing is an
// error. Returns a nil Index if no layer exists.
func Load(repoDir, customPath string) (Index, error) {
	merged := Index{}
	found := false

	// Load low → high so higher layers win on key collision.
	// Dependency spec.json (shipped inside installed deps) is the lowest layer:
	// local spec always overrides a dependency's on key collision. Best-effort —
	// dependency discovery never fails a review.
	if repoDir != "" {
		if dep := loadDepSpecs(repoDir); len(dep) > 0 {
			mergeInto(merged, dep)
			found = true
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if err := mergeOptional(merged, filepath.Join(home, ".casecodereview", "spec.json"), &found); err != nil {
			return nil, err
		}
	}
	if repoDir != "" {
		if err := mergeOptional(merged, filepath.Join(repoDir, ".casecodereview", "spec.json"), &found); err != nil {
			return nil, err
		}
	}
	if customPath != "" {
		data, err := os.ReadFile(customPath) // required: a given --spec path must exist
		if err != nil {
			return nil, err
		}
		idx, err := Parse(data)
		if err != nil {
			return nil, err
		}
		mergeInto(merged, idx)
		found = true
	}

	if !found {
		return nil, nil
	}
	return merged, nil
}

// mergeOptional loads and merges path if it exists; a missing file is skipped.
func mergeOptional(dst Index, path string, found *bool) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	idx, err := Parse(data)
	if err != nil {
		return err
	}
	mergeInto(dst, idx)
	*found = true
	return nil
}

func mergeInto(dst, src Index) {
	maps.Copy(dst, src)
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
