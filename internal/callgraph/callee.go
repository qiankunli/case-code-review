package callgraph

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/qiankunli/case-code-review/internal/gitcmd"
	"github.com/qiankunli/case-code-review/internal/spec"
	"github.com/qiankunli/case-code-review/internal/unit"
)

// CalleeFinder surfaces the contracts a changed function DEPENDS ON: it walks
// down to the function's callees (up to Depth hops), stopping each branch at the
// nearest spec-bearing callee and attaching its spec as a ClueCallee. Symmetric
// to CallerFinder (which walks up to the governing spec); this looks down to what
// the change relies on, so the reviewer can check the change still honours those
// callees' contracts. Go-only; bounded by Max/Depth, degrading to nil.
type CalleeFinder struct {
	RepoDir string
	Index   spec.Index
	Runner  *gitcmd.Runner
	Max     int
	Depth   int // hops to walk down (0 -> default 2)
}

func (f CalleeFinder) Find(u unit.Unit) []unit.Clue {
	if f.Index == nil || f.RepoDir == "" || u.Scope != unit.ScopeFunc {
		return nil
	}
	max := f.Max
	if max <= 0 {
		max = defaultMaxResults
	}
	return walkForSpecs(f.Index, u.Symbols, f.callees, f.Depth, max, func(id string) unit.Clue {
		return unit.Clue{
			Kind: unit.ClueCallee,
			Text: "(depends on callee " + id + ", which guarantees)\n" + f.Index.Render([]string{id}),
			Ref:  id,
		}
	})
}

// callees returns the unit-ids of functions that funcID calls — extract the
// callees from its body (go/ast), then resolve each name to its definition.
func (f CalleeFinder) callees(funcID string) []string {
	path, sym, ok := unit.SplitID(funcID)
	if !ok {
		return nil
	}
	src, err := os.ReadFile(filepath.Join(f.RepoDir, path))
	if err != nil {
		return nil
	}
	var ids []string
	seen := map[string]bool{}
	for _, name := range unit.GoCalleesOf(path, string(src), sym) {
		for _, id := range f.resolveDefs(name, defaultMaxResults) {
			if id == funcID || seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// resolveDefs greps for Go definitions of name — a free function `func name(` or
// a method `func (recv) name(` — and resolves each to its unit-id, guarding that
// the resolved function actually carries that name.
func (f CalleeFinder) resolveDefs(name string, max int) []string {
	// -P: `func`, then a space (free func) or a receiver `(...)`, then name, then `(`.
	pat := `func(\s+|\s*\([^)]*\)\s*)` + name + `\s*\(`
	var ids []string
	seen := map[string]bool{}
	for _, h := range grepGo(f.RepoDir, f.Runner, []string{"-P", "-e", pat}, max*4) {
		id, ok := funcIDAt(f.RepoDir, h)
		if !ok || seen[id] {
			continue
		}
		if _, sym, _ := unit.SplitID(id); !symbolHasName(sym, name) {
			continue // the regex landed somewhere that isn't name's definition
		}
		seen[id] = true
		ids = append(ids, id)
		if len(ids) >= max {
			break
		}
	}
	return ids
}

// symbolHasName reports whether a symbol ("Name" or "Recv.Method") names fn.
func symbolHasName(symbol, fn string) bool {
	if i := strings.LastIndex(symbol, "."); i >= 0 {
		return symbol[i+1:] == fn
	}
	return symbol == fn
}
