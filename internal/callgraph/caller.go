// Package callgraph supplies call-graph-derived review context via two
// ClueFinders: CallerFinder recovers a changed function's GOVERNING spec by
// walking one hop up to its callers; CalleeFinder surfaces the contracts the
// function DEPENDS ON by looking one hop down to its callees. Both are
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

// CallerFinder supplies a changed function's governing spec when the function
// carries none of its own: it greps one hop up to its callers, resolves each hit
// to the calling function's unit-id, and — if a caller has a spec — attaches it
// as an inherited ClueCaller. Spec lives on entry functions (api-handlers) while
// a diff often lands on a deep helper, so the contract to preserve is the
// caller's. Go-only and depth-1 for now; bounded by Max and degrading to nil on
// any miss, so it is always safe to install.
type CallerFinder struct {
	RepoDir string
	Index   spec.Index
	Runner  *gitcmd.Runner // optional; falls back to exec when nil
	Max     int            // cap on resolved spec-bearing callers (0 -> default)
}

func (f CallerFinder) Find(u unit.Unit) []unit.Clue {
	// Only function units have a name to walk from, and we need both a repo to
	// grep and an index to resolve specs against.
	if f.Index == nil || f.RepoDir == "" || u.Scope != unit.ScopeFunc {
		return nil
	}
	// Own-spec short-circuit: a function with its own contract needs no walk —
	// this is what keeps a widely-called utility (huge fan-in) from exploding.
	for _, sym := range u.Symbols {
		if e, ok := f.Index[sym]; ok && (e.Spec != "" || len(e.Cases) > 0) {
			return nil
		}
	}

	max := f.Max
	if max <= 0 {
		max = defaultMaxResults
	}
	self := map[string]bool{}
	for _, sym := range u.Symbols {
		self[sym] = true
	}

	emitted := map[string]bool{}
	var clues []unit.Clue
	for _, sym := range u.Symbols {
		name := funcName(sym)
		if name == "" {
			continue
		}
		// Word-match the called name; resolution + dedup narrows to Max, so over-fetch.
		for _, h := range grepGo(f.RepoDir, f.Runner, []string{"-w", "-e", name}, max*4) {
			id, ok := funcIDAt(f.RepoDir, h)
			if !ok || self[id] || emitted[id] { // skip the definition / recursion / dupes
				continue
			}
			e, ok := f.Index[id]
			if !ok || (e.Spec == "" && len(e.Cases) == 0) {
				continue
			}
			emitted[id] = true
			clues = append(clues, unit.Clue{
				Kind: unit.ClueCaller,
				Text: "(governing spec inherited from caller " + id + ")\n" + f.Index.Render([]string{id}),
				Ref:  id,
			})
			if len(emitted) >= max {
				return clues
			}
		}
	}
	return clues
}

// funcName returns the bare function/method name from a unit-id, for grepping:
// "pkg/x.go::Service.Get" -> "Get", "pkg/x.go::Helper" -> "Helper".
func funcName(unitID string) string {
	_, symbol, ok := unit.SplitID(unitID)
	if !ok {
		return ""
	}
	if i := strings.LastIndex(symbol, "."); i >= 0 {
		return symbol[i+1:]
	}
	return symbol
}
