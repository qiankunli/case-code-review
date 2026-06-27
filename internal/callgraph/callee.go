package callgraph

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/qiankunli/case-code-review/internal/gitcmd"
	"github.com/qiankunli/case-code-review/internal/spec"
	"github.com/qiankunli/case-code-review/internal/unit"
)

// CalleeFinder surfaces the contracts a changed function DEPENDS ON: it extracts
// the function's callees (go/ast), resolves each to its definition (git grep),
// and — if a callee has a spec — attaches it as a ClueCallee. Symmetric to
// CallerFinder (which walks up to the governing spec); this looks down to what
// the change relies on, so the reviewer can check the change still honours those
// callees' contracts. Go-only and depth-1; bounded by Max, degrading to nil.
type CalleeFinder struct {
	RepoDir string
	Index   spec.Index
	Runner  *gitcmd.Runner
	Max     int
}

func (f CalleeFinder) Find(u unit.Unit) []unit.Clue {
	if f.Index == nil || f.RepoDir == "" || u.Scope != unit.ScopeFunc {
		return nil
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
	for _, symID := range u.Symbols {
		path, sym, ok := unit.SplitID(symID)
		if !ok {
			continue
		}
		src, err := os.ReadFile(filepath.Join(f.RepoDir, path))
		if err != nil {
			continue
		}
		for _, callee := range unit.GoCalleesOf(path, string(src), sym) {
			for _, id := range f.resolveDefs(callee, max) {
				if self[id] || emitted[id] { // skip self-recursion / dupes
					continue
				}
				e, ok := f.Index[id]
				if !ok || (e.Spec == "" && len(e.Cases) == 0) {
					continue
				}
				emitted[id] = true
				clues = append(clues, unit.Clue{
					Kind: unit.ClueCallee,
					Text: "(depends on callee " + id + ", which guarantees)\n" + f.Index.Render([]string{id}),
					Ref:  id,
				})
				if len(emitted) >= max {
					return clues
				}
			}
		}
	}
	return clues
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
