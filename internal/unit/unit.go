// Package unit defines the review Unit — the minimal scope of one independent
// review (diff → unit → unit context → review loop). A file is the degenerate
// unit: today's per-file review is exactly one Unit per changed file. Finer
// scopes (e.g. function-level, for precise spec/case association) are produced
// by language-aware Splitter implementations; the dispatch/loop machinery does
// not care which granularity it is fed.
package unit

import "github.com/qiankunli/case-code-review/internal/model"

// Scope is the granularity of a review Unit.
type Scope string

const (
	// ScopeFile is the degenerate scope: one Unit per changed file.
	ScopeFile Scope = "file"
	// ScopeFunc is a function-level scope, produced by a language-aware Splitter.
	// Reserved for the upcoming go/ast splitter; not emitted yet.
	ScopeFunc Scope = "func"
)

// Unit is one independently-reviewed scope of a change, and the currency of
// the review pipeline: the loop runs once per Unit. model.Diff is upstream of
// this — raw git-parse output the Splitter consumes — and does not flow past
// the split. Comments and line resolution resolve against Path (a real file),
// so finer scopes stay compatible with the path-oriented tools.
type Unit struct {
	// ID is a stable identity for the Unit: the file path for file scope,
	// "<path>#<Symbol>" for function scope. Used for telemetry/span naming.
	ID string
	// Scope is the granularity this Unit was split at.
	Scope Scope
	// Path is the owning file path — the identity used for comment placement
	// and diff/line resolution. Never empty.
	Path string
	// Symbols are the function ids this Unit covers — the join keys into
	// spec.json for spec/case association. A function Unit covers one (itself);
	// a file Unit coalesced from several functions by the cost governor covers
	// all of them, so spec/case still resolves after coarsening; a degenerate
	// file Unit (non-Go / unparseable) covers none.
	Symbols []string
	// Diff is the diff slice this Unit reviews: the whole-file diff for file
	// scope, or just the function's hunks for function scope.
	Diff string
	// Insertions and Deletions count the changed lines within THIS Unit (not
	// the owning file). They drive the plan-skip threshold and telemetry, so a
	// small function in a large file is sized by its own change, not the file's.
	Insertions int64
	Deletions  int64
	// Clues are the context pieces ClueFinders found for this Unit (spec,
	// rule, link, …). Filled on the diff unit before merge; CoalesceFile unions
	// the members' clues so a coalesced file Unit keeps them all.
	Clues []Clue
}

// Splitter turns one file's diff into one or more review Units. The default
// FileSplitter is the identity (one Unit per file); language-aware splitters
// (e.g. Go go/ast) cut a file into finer Units.
type Splitter interface {
	Split(d model.Diff) ([]Unit, error)
}

// FileSplitter is the degenerate Splitter: one Unit per file. It preserves
// today's file-granularity review behavior exactly.
type FileSplitter struct{}

// Split returns a single file-scoped Unit wrapping the whole file diff.
func (FileSplitter) Split(d model.Diff) ([]Unit, error) {
	return []Unit{{
		ID:         d.NewPath,
		Scope:      ScopeFile,
		Path:       d.NewPath,
		Diff:       d.Diff,
		Insertions: d.Insertions,
		Deletions:  d.Deletions,
	}}, nil
}

// CoalesceFile merges a file's review Units into a single file-scope Unit. The
// cost governor uses it to trade per-function focus for fewer review loops: the
// merged Unit reviews the whole file diff but retains every member's Symbols, so
// spec/case still resolves for it (the governor caps loop count, not context).
func CoalesceFile(d model.Diff, members []Unit) Unit {
	u, _ := FileSplitter{}.Split(d)
	merged := u[0]
	for _, m := range members {
		merged.Symbols = append(merged.Symbols, m.Symbols...)
		merged.Clues = addClues(merged.Clues, m.Clues)
	}
	return merged
}
