package callgraph

import (
	"os"
	"path/filepath"
	"regexp"
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
// callees' contracts. Bounded by Max/Depth, degrading to nil.
type CalleeFinder struct {
	RepoDir string
	Index   spec.Index // may be nil: doc-only mode still works
	Runner  *gitcmd.Runner
	Typed   *TypedGraph // optional; typed answers for Go symbols, grep fallback otherwise
	Max     int
	Depth   int            // hops to walk down (0 -> default 2)
	Kinds   spec.KindGates // Spec: emit depended-on specs; Doc: emit direct callees' docstrings
}

func (f CalleeFinder) Find(u unit.Unit) []unit.Clue {
	// Func and chain units only, same reasoning as CallerFinder.Find.
	if f.RepoDir == "" || (u.Scope != unit.ScopeFunc && u.Scope != unit.ScopeCallChain) {
		return nil
	}
	emitSpec := f.Kinds.Spec && f.Index != nil
	if !emitSpec && !f.Kinds.Doc {
		return nil
	}
	max := f.Max
	if max <= 0 {
		max = defaultMaxResults
	}
	var doc *docRider
	if f.Kinds.Doc {
		doc = &docRider{repoDir: f.RepoDir, relation: unit.RelCallee}
	}
	cfg := walkCfg{idx: f.Index, depth: f.Depth, max: max, spec: emitSpec, doc: doc}
	return walkNeighbors(cfg, u.AllSymbols(), f.callees, func(id string) unit.Clue {
		return unit.Clue{
			Kind:     unit.ClueSpec,
			Relation: unit.RelCallee,
			Text:     f.Index.Render([]string{id}),
			Ref:      id,
		}
	})
}

// callees returns the symbol-ids of functions that funcID calls — extract the
// callees from its body with the language parser, then resolve each name to its definition.
func (f CalleeFinder) callees(funcID string) []string {
	if ids, ok := f.Typed.Callees(funcID); ok {
		return ids
	}
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
	for _, name := range unit.CalleesOfInRepo(f.RepoDir, path, string(src), sym) {
		// An unexported callee is defined in this function's own package — scope
		// the definition grep there so a same-named def elsewhere isn't picked.
		scope := unexportedScope(path, name)
		for _, id := range f.resolveDefs(name, defaultMaxResults, scope) {
			if id == funcID || seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// resolveDefs greps for Go, Python, and JavaScript-family definitions of name,
// then resolves each hit to its enclosing symbol-id.
func (f CalleeFinder) resolveDefs(name string, max int, scopeDir string) []string {
	quoted := regexp.QuoteMeta(name)
	pat := `(func(\s+|\s*\([^)]*\)\s*)|def\s+|function\s+)` + quoted + `\s*(<[^>]*>|\[[^]]*\])?\s*\(` +
		`|(const|let|var)\s+` + quoted + `\s*=\s*(async\s*)?(\([^)]*\)|[A-Za-z_$][A-Za-z0-9_$]*)\s*=>` +
		`|^\s*((public|private|protected|static|async|readonly|abstract|override)\s+)*` + quoted + `\s*(<[^>]*>)?\s*\([^)]*\)\s*(:[^={]+)?\s*\{`
	var ids []string
	seen := map[string]bool{}
	for _, h := range grepCode(f.RepoDir, f.Runner, []string{"-P", "-e", pat}, max*4, scopeDir) {
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
