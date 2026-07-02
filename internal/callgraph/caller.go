// Package callgraph supplies call-graph-derived review context via two
// ClueFinders: CallerFinder recovers a changed function's GOVERNING spec by
// walking up to its callers; CalleeFinder surfaces the contracts the function
// DEPENDS ON by walking down to its callees. Both share one bounded walk
// (walkForSpecs) and differ only in their neighbor function. They are
// deliberately lightweight (git grep + go/ast, no whole-repo type checking) so
// they work on a diff that may not even compile, and degrade to nothing whenever
// they can't help.
package callgraph

import (
	"strings"

	"github.com/qiankunli/case-code-review/internal/gitcmd"
	"github.com/qiankunli/case-code-review/internal/spec"
	"github.com/qiankunli/case-code-review/internal/unit"
)

// CallerFinder walks up to a changed function's callers. With the spec kind on
// and a spec index present, it supplies the governing spec the function inherits
// (up to Depth hops, stopping each branch at the nearest spec-bearing ancestor):
// spec lives on entry functions (api-handlers) while a diff often lands on a
// deep helper, so the contract to preserve is the caller's. With the doc kind on
// it emits direct callers' docstrings (what context this function is used in) —
// doc is a derived mark, needing no spec.json; the two payloads are peers, each
// behind its own kind gate. Bounded by Max/Depth, degrading to nil on any miss.
type CallerFinder struct {
	RepoDir string
	Index   spec.Index     // may be nil: doc-only mode still works
	Runner  *gitcmd.Runner // optional; falls back to exec when nil
	Max     int            // cap on resolved spec-bearing callers (0 -> default)
	Depth   int            // hops to walk up (0 -> default 2)
	Kinds   spec.KindGates // Spec: emit inherited specs; Doc: emit direct callers' docstrings
}

func (f CallerFinder) Find(u unit.Unit) []unit.Clue {
	// Only function units have a name to walk from, and we need a repo to grep.
	if f.RepoDir == "" || u.Scope != unit.ScopeFunc {
		return nil
	}
	emitSpec := f.Kinds.Spec && f.Index != nil
	if !emitSpec && !f.Kinds.Doc {
		return nil
	}
	// Own-spec short-circuit: a function with its own contract needs no inherited
	// one — this keeps a widely-called utility (huge fan-in) from exploding.
	if emitSpec {
		for _, sym := range u.AllSymbols() {
			if e, ok := f.Index[sym]; ok && (e.Spec != "" || len(e.Cases) > 0) {
				emitSpec = false
				break
			}
		}
		if !emitSpec && !f.Kinds.Doc {
			return nil
		}
	}
	max := f.Max
	if max <= 0 {
		max = defaultMaxResults
	}
	var doc *docRider
	if f.Kinds.Doc {
		doc = &docRider{repoDir: f.RepoDir, relation: unit.RelCaller}
	}
	cfg := walkCfg{idx: f.Index, depth: f.Depth, max: max, spec: emitSpec, doc: doc}
	return walkNeighbors(cfg, u.AllSymbols(), f.callers, func(id string) unit.Clue {
		return unit.Clue{
			Kind:     unit.ClueSpec,
			Relation: unit.RelCaller,
			Text:     f.Index.Render([]string{id}),
			Ref:      id,
		}
	})
}

// callers returns the symbol-ids of functions that call funcID — git grep the
// function's name, then resolve each call site to its enclosing function.
func (f CallerFinder) callers(funcID string) []string {
	name := funcName(funcID)
	if name == "" {
		return nil
	}
	// An unexported callee can only be called from its own package — scope the
	// grep there so a same-named function elsewhere isn't mistaken for a caller.
	path, _, _ := unit.SplitID(funcID)
	scope := unexportedScope(path, name)
	var ids []string
	seen := map[string]bool{}
	for _, h := range grepGo(f.RepoDir, f.Runner, []string{"-w", "-e", name}, defaultMaxResults*4, scope) {
		id, ok := funcIDAt(f.RepoDir, h)
		if !ok || id == funcID || seen[id] { // skip funcID's own definition / recursion / dupes
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// funcName returns the bare function/method name from a symbol-id, for grepping:
// "pkg/x.go::Service.Get" -> "Get", "pkg/x.go::Helper" -> "Helper".
func funcName(symbolID string) string {
	_, symbol, ok := unit.SplitID(symbolID)
	if !ok {
		return ""
	}
	if i := strings.LastIndex(symbol, "."); i >= 0 {
		return symbol[i+1:]
	}
	return symbol
}
