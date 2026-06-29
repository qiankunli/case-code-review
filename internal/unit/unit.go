// Package unit models the two stages of the review pipeline:
//
//   - Fragment — the atom: one file's changed region (a function's hunks, or the
//     file residual). A Splitter's output. Pure data: no context, no grouping.
//   - Unit — the review scope: 1..N Fragments grouped by an axis (one function;
//     same-file coalesced; or a cross-file call chain) plus the context (Clues)
//     gathered for that scope. A Merger's output; the review loop runs once per
//     Unit.
//
// (diff → Fragment → merge → Unit → review loop. See docs/unit-model.md.)
package unit

import (
	"strings"

	"github.com/qiankunli/case-code-review/internal/model"
)

// Scope is how a Unit's Fragments were grouped — set when the Unit is formed.
type Scope string

const (
	// ScopeFile groups a whole file's change (residual / unparseable / coalesced).
	ScopeFile Scope = "file"
	// ScopeFunc is a single function's change.
	ScopeFunc Scope = "func"
	// ScopeCallChain groups call-adjacent changed functions (may span files).
	// Reserved for the call-graph merger; not emitted yet.
	ScopeCallChain Scope = "callchain"
)

// Fragment is the atom: one file's changed region. Symbols are the unit-ids
// (functions) it covers — one for a function fragment, none for a file residual,
// several for a coalesced whole-file fragment. Pure data: a Splitter produces it,
// a Merger groups it; it carries no context.
type Fragment struct {
	Path       string
	Symbols    []string
	Diff       string
	Insertions int64
	Deletions  int64
}

// Unit is the review scope and the currency of the pipeline: the loop runs once
// per Unit. It groups Fragments and carries the Clues found for that scope.
// model.Diff is upstream of this (the Splitter consumes it) and does not flow
// past the split.
type Unit struct {
	// ID is a stable identity for telemetry/span naming.
	ID string
	// Scope is how this Unit's Fragments were grouped.
	Scope Scope
	// Fragments are the changed regions reviewed together (one for a function or
	// file Unit; several across files for a call-chain Unit).
	Fragments []Fragment
	// Clues are the context pieces ClueFinders found for this Unit (spec, rule,
	// link, caller/callee, history), gathered post-merge against AllSymbols.
	Clues []Clue
}

// AllSymbols returns every unit-id this Unit covers across its Fragments — the
// join keys for spec/case/history lookup.
func (u Unit) AllSymbols() []string {
	var out []string
	for _, f := range u.Fragments {
		out = append(out, f.Symbols...)
	}
	return out
}

// Insertions / Deletions sum the change across the Unit's Fragments — sizing the
// Unit by its own change, not any owning file's.
func (u Unit) Insertions() int64 {
	return sumFrag(u.Fragments, func(f Fragment) int64 { return f.Insertions })
}
func (u Unit) Deletions() int64 {
	return sumFrag(u.Fragments, func(f Fragment) int64 { return f.Deletions })
}

func sumFrag(fs []Fragment, pick func(Fragment) int64) int64 {
	var n int64
	for _, f := range fs {
		n += pick(f)
	}
	return n
}

// Path is the Unit's primary file (its first Fragment) — used for telemetry and
// as the comment-anchor default. A call-chain Unit spans files; per-comment paths
// (model.LlmComment.Path) place findings, so this is only a label.
func (u Unit) Path() string {
	if len(u.Fragments) > 0 {
		return u.Fragments[0].Path
	}
	return ""
}

// Paths returns each distinct member file path (for the change-files exclusion).
func (u Unit) Paths() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range u.Fragments {
		if f.Path != "" && !seen[f.Path] {
			seen[f.Path] = true
			out = append(out, f.Path)
		}
	}
	return out
}

// Diff is the diff the Unit reviews: a single Fragment's slice as-is, or — for a
// multi-file Unit — the members concatenated, each under a `// <path>` header so
// the reviewer can tell them apart.
func (u Unit) Diff() string {
	if len(u.Fragments) == 1 {
		return u.Fragments[0].Diff
	}
	var b strings.Builder
	for i, f := range u.Fragments {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("// " + f.Path + "\n" + f.Diff)
	}
	return b.String()
}

// Splitter turns one file's diff into Fragments — one per changed function plus a
// residual, or a single whole-file Fragment when the file can't be parsed.
type Splitter interface {
	Split(d model.Diff) ([]Fragment, error)
}

// FileSplitter is the degenerate Splitter: a single whole-file Fragment.
type FileSplitter struct{}

func (FileSplitter) Split(d model.Diff) ([]Fragment, error) {
	return []Fragment{{
		Path:       d.NewPath,
		Diff:       d.Diff,
		Insertions: d.Insertions,
		Deletions:  d.Deletions,
	}}, nil
}

// UnitOf wraps a single Fragment as its own review Unit: ScopeFunc when it covers
// exactly one function (ID "<path>#<symbol>"), else ScopeFile (ID the path).
func UnitOf(f Fragment) Unit {
	if len(f.Symbols) == 1 {
		return Unit{ID: f.Path + "#" + symbolName(f.Symbols[0]), Scope: ScopeFunc, Fragments: []Fragment{f}}
	}
	return Unit{ID: f.Path, Scope: ScopeFile, Fragments: []Fragment{f}}
}

// CoalesceFile merges a file's Fragments into one ScopeFile Unit reviewing the
// whole-file diff while retaining every Fragment's Symbols — the cost governor's
// function→file rung (caps loop count, not context).
func CoalesceFile(d model.Diff, frags []Fragment) Unit {
	whole := Fragment{Path: d.NewPath, Diff: d.Diff, Insertions: d.Insertions, Deletions: d.Deletions}
	for _, f := range frags {
		whole.Symbols = append(whole.Symbols, f.Symbols...)
	}
	return Unit{ID: d.NewPath, Scope: ScopeFile, Fragments: []Fragment{whole}}
}

// NewChainUnit groups call-adjacent changed functions (possibly across files)
// into one ScopeCallChain review Unit — a requirement's change reviewed along the
// call chain it touched. Callers should pass Fragments in a stable order (e.g.
// sorted by path/symbol) so the ID is deterministic.
func NewChainUnit(frags []Fragment) Unit {
	var names []string
	for _, f := range frags {
		for _, s := range f.Symbols {
			names = append(names, symbolName(s))
		}
	}
	return Unit{ID: "chain:" + strings.Join(names, "+"), Scope: ScopeCallChain, Fragments: frags}
}

// symbolName returns the bare symbol from a unit-id ("p/x.go::Svc.Get" -> "Svc.Get")
// for building a Unit ID; falls back to the whole string when it isn't an id.
func symbolName(unitID string) string {
	if _, sym, ok := SplitID(unitID); ok {
		return sym
	}
	return unitID
}
