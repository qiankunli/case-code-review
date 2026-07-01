package spec

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/qiankunli/case-code-review/internal/unit"
)

// The three spec.json-backed ClueFinders. They share one Index (spec/case,
// rule, link all live in spec.json) but stay separate so each context kind is a
// distinct, swappable finder — and so caller/callee can join later as their own
// finders without touching these. All are nil-Index safe.

// SpecFinder yields the contract+cases clue for a Unit's symbols (one clue
// carrying the rendered block, empty when none of the symbols carry spec/case).
type SpecFinder struct{ Index Index }

func (f SpecFinder) Find(u unit.Unit) []unit.Clue {
	if r := f.Index.Render(u.AllSymbols()); r != "" {
		return []unit.Clue{{Kind: unit.ClueSpec, Relation: unit.RelSelf, Text: r}}
	}
	return nil
}

// RuleFinder yields one rule clue per @rule on the Unit's symbols.
type RuleFinder struct{ Index Index }

func (f RuleFinder) Find(u unit.Unit) []unit.Clue {
	var clues []unit.Clue
	for _, sym := range u.AllSymbols() {
		for _, r := range f.Index[sym].Rules {
			clues = append(clues, unit.Clue{Kind: unit.ClueRule, Relation: unit.RelSelf, Text: r})
		}
	}
	return clues
}

// LinkFinder yields one see-also clue per @link on the Unit's symbols; Text is
// labelled doc/function for the prompt, Ref keeps the raw reference for the
// reviewer to fetch on demand.
type LinkFinder struct{ Index Index }

func (f LinkFinder) Find(u unit.Unit) []unit.Clue {
	var clues []unit.Clue
	for _, sym := range u.AllSymbols() {
		for _, l := range f.Index[sym].Links {
			kind := "doc"
			if strings.Contains(l, "::") {
				kind = "function"
			}
			clues = append(clues, unit.Clue{Kind: unit.ClueLink, Relation: unit.RelSelf, Text: l + " (" + kind + ")", Ref: l})
		}
	}
	return clues
}

// identifier matches a source identifier (Go/Python); used to scan a Unit's diff
// for names it references.
var identifier = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// ReferenceFinder yields a clue for each @rule on a symbol the Unit's diff
// *references* (a type/func used in the change), as opposed to the changed symbol
// itself. This surfaces type-level usage constraints — e.g. a diff that uses
// `PhaseEventMiddleware` picks up its class rule "per-request only" even though
// the middleware's own definition isn't in the diff.
//
// It resolves a referenced name two ways, precise first: (1) via the referencing
// file's imports to the symbol's fqn, matched against fqn-keyed entries — this
// disambiguates same-named types and is how a *dependency's* rule is reached
// cross-repo; (2) failing that, by bare name against the spec index's non-method
// symbols (intra-repo, imprecise when two files share a name).
type ReferenceFinder struct {
	index   Index
	byName  map[string][]string // bare symbol name -> symbol-ids (non-method only)
	byFqn   map[string]string   // fqn -> symbol-id (precise, incl. cross-repo deps)
	repoDir string              // for reading referencing files' imports (Python)
}

// NewReferenceFinder precomputes the name/fqn -> symbol-id indexes once (not per
// Unit). Symbol-ids are processed in sorted order so the winner is deterministic
// when two entries share an fqn (possible across merged spec.json layers).
func NewReferenceFinder(idx Index, repoDir string) ReferenceFinder {
	byName := make(map[string][]string)
	byFqn := make(map[string]string)
	ids := make([]string, 0, len(idx))
	for id := range idx {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if fqn := idx[id].Fqn; fqn != "" {
			byFqn[fqn] = id
		}
		sym := id
		if _, after, ok := strings.Cut(id, "::"); ok {
			sym = after
		}
		if strings.Contains(sym, ".") {
			continue // a method (Class.method) isn't referenced by a bare name
		}
		byName[sym] = append(byName[sym], id)
	}
	return ReferenceFinder{index: idx, byName: byName, byFqn: byFqn, repoDir: repoDir}
}

func (f ReferenceFinder) Find(u unit.Unit) []unit.Clue {
	if len(f.byName) == 0 && len(f.byFqn) == 0 {
		return nil
	}
	own := make(map[string]bool, len(u.AllSymbols()))
	for _, s := range u.AllSymbols() {
		own[s] = true
	}
	var clues []unit.Clue
	seen := make(map[string]bool) // dedup (symbol-id, rule) across repeated references
	emit := func(name, id string) {
		if own[id] {
			return // the Unit's own symbol — SpecFinder/RuleFinder cover it
		}
		for _, r := range f.index[id].Rules {
			key := id + "\x00" + r
			if seen[key] {
				continue
			}
			seen[key] = true
			clues = append(clues, unit.Clue{
				Kind:     unit.ClueRule,
				Relation: unit.RelUsed,
				Text:     "(used type `" + name + "`) " + r,
				Ref:      name,
			})
		}
	}
	diff := u.Diff()
	resolved := map[string]bool{} // bare names resolved precisely via fqn — skip their bare-name fallback

	// Go: `pkg.Symbol` selectors → importPath.Symbol fqn (precise, disambiguates
	// same-named types across packages).
	if goImp := f.goImportPaths(u); len(goImp) > 0 {
		for _, m := range goSelector.FindAllStringSubmatch(diff, -1) {
			pkg, sym := m[1], m[2]
			path, ok := goImp[pkg]
			if !ok {
				continue
			}
			if id, ok := f.byFqn[path+"."+sym]; ok {
				emit(sym, id)
				resolved[sym] = true // don't also bare-name match this symbol
			}
		}
	}

	// Python: `from mod import Name` resolves a bare name to fqn; else bare-name.
	pyImports := f.pyImportFqns(u)
	for _, name := range identifier.FindAllString(diff, -1) {
		if resolved[name] {
			continue
		}
		if fqn, ok := pyImports[name]; ok { // precise: import-resolved fqn
			if id, ok := f.byFqn[fqn]; ok {
				emit(name, id)
				continue // resolved precisely — skip bare-name (avoids same-name mismatch)
			}
		}
		for _, id := range f.byName[name] { // fallback: bare name (intra-repo / no fqn)
			emit(name, id)
		}
	}
	return clues
}

// pyImportFqns resolves the unit's Python member-file imports to fqns (local name ->
// module.realname). Empty when there's no repoDir or no Python members.
func (f ReferenceFinder) pyImportFqns(u unit.Unit) map[string]string {
	if f.repoDir == "" {
		return nil
	}
	out := map[string]string{}
	for _, p := range u.Paths() {
		if !strings.HasSuffix(p, ".py") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(f.repoDir, p))
		if err != nil {
			continue // best-effort: an unreadable member just skips its imports
		}
		for local, sym := range parsePyFromImports(string(src)) {
			out[local] = sym.module + "." + sym.name
		}
	}
	return out
}

// goImportPaths maps the unit's Go member-file import selector names to import
// paths (see parseGoImports), so a `pkg.Symbol` reference resolves to a fqn.
func (f ReferenceFinder) goImportPaths(u unit.Unit) map[string]string {
	if f.repoDir == "" {
		return nil
	}
	out := map[string]string{}
	for _, p := range u.Paths() {
		if !strings.HasSuffix(p, ".go") {
			continue
		}
		if src, err := os.ReadFile(filepath.Join(f.repoDir, p)); err == nil {
			maps.Copy(out, parseGoImports(string(src)))
		}
	}
	return out
}
