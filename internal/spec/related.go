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

// This file is the factored context pipeline of docs/context-model.md: the
// relation axis (RelationCollector: unit → related symbols) × the source axis
// (cluesFor: symbol → authored marks + derived docstring). RelatedFinder composes
// the two into one unit.ClueFinder, so adding a relation or a source never
// multiplies finder types.

// identifier matches a source identifier (Go/Python); used to scan a Unit's diff
// for names it references.
var identifier = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// RelatedSymbol is one symbol reached from a review unit along a typed relation —
// what the relation axis hands to the source axis.
type RelatedSymbol struct {
	ID       string // spec-index key ("" when the symbol isn't indexed)
	Relation unit.Relation
	Name     string // bare name as referenced (labels authored marks)
	Ref      string // Clue.Ref for the doc clue (owner: symbol-id; used: fqn)
	DocFile  string // source file for docstring extraction ("" = no doc)
	DocName  string // symbol name inside DocFile
}

// RelationCollector finds the symbols related to a unit along one relation.
type RelationCollector interface {
	Related(u unit.Unit) []RelatedSymbol
}

// --- self: the changed symbols themselves ---

type selfCollector struct{}

func (selfCollector) Related(u unit.Unit) []RelatedSymbol {
	var out []RelatedSymbol
	for _, sym := range u.AllSymbols() {
		name := sym
		if _, after, ok := strings.Cut(sym, "::"); ok {
			name = after
		}
		out = append(out, RelatedSymbol{ID: sym, Relation: unit.RelSelf, Name: name})
	}
	return out
}

// --- owner: a changed method's enclosing type (or nested func's outer func) ---

// Without the owner relation, a class-level marker only fires when the whole
// class is the changed symbol — which almost never happens; changing `Svc.create`
// must still surface class `Svc`'s @rule and docstring.
type ownerCollector struct{ repoDir string }

func (c ownerCollector) Related(u unit.Unit) []RelatedSymbol {
	own := make(map[string]bool, len(u.AllSymbols()))
	for _, s := range u.AllSymbols() {
		own[s] = true
	}
	seen := map[string]bool{}
	var out []RelatedSymbol
	for _, sym := range u.AllSymbols() {
		owner, ok := enclosingSymbol(sym)
		if !ok || own[owner] || seen[owner] {
			continue // no owner, or the owner is itself a changed symbol (self covers it)
		}
		seen[owner] = true
		rs := RelatedSymbol{ID: owner, Relation: unit.RelOwner, Name: ownerName(owner), Ref: owner}
		if rel, name, ok := strings.Cut(owner, "::"); ok && c.repoDir != "" {
			rs.DocFile = filepath.Join(c.repoDir, rel)
			rs.DocName = name
		}
		out = append(out, rs)
	}
	return out
}

// enclosingSymbol returns the symbol-id of id's immediate enclosing symbol
// (`<relpath>::Base.method` → `<relpath>::Base`), or false when the symbol part
// has no dot (a top-level func/type — no owner).
func enclosingSymbol(id string) (string, bool) {
	rel, sym, ok := strings.Cut(id, "::")
	if !ok {
		return "", false
	}
	i := strings.LastIndex(sym, ".")
	if i < 0 {
		return "", false
	}
	return rel + "::" + sym[:i], true
}

// ownerName is the bare symbol name of an enclosing symbol-id (for labelling).
func ownerName(ownerID string) string {
	if _, sym, ok := strings.Cut(ownerID, "::"); ok {
		return sym
	}
	return ownerID
}

// --- used: types/funcs the diff references (callee ⊇ class) ---

// usedCollector resolves a referenced name two ways, precise first: (1) via the
// referencing file's imports to the symbol's fqn (Python from-imports, Go
// pkg.Symbol selectors) — disambiguating same-named types and reaching a
// *dependency's* symbols cross-repo; (2) failing that, by bare name against the
// index's non-method symbols (intra-repo). An import-resolved symbol also carries
// its source file, so its docstring is available even when it has no spec entry
// (adoption-free).
type usedCollector struct {
	byName  map[string][]string // bare symbol name -> symbol-ids (non-method only)
	byFqn   map[string]string   // fqn -> symbol-id (precise, incl. cross-repo deps)
	repoDir string
}

// newUsedCollector precomputes the name/fqn indexes once (not per Unit). Symbol-ids
// are processed in sorted order so the winner is deterministic when two entries
// share an fqn (possible across merged spec.json layers).
func newUsedCollector(idx Index, repoDir string) usedCollector {
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
	return usedCollector{byName: byName, byFqn: byFqn, repoDir: repoDir}
}

func (c usedCollector) Related(u unit.Unit) []RelatedSymbol {
	own := make(map[string]bool, len(u.AllSymbols()))
	for _, s := range u.AllSymbols() {
		own[s] = true
	}
	diff := u.Diff()
	var out []RelatedSymbol
	emitted := map[string]bool{}
	emit := func(rs RelatedSymbol) {
		if rs.ID != "" && own[rs.ID] {
			return // the Unit's own symbol — the self relation covers it
		}
		key := rs.ID + "\x00" + rs.Ref
		if emitted[key] {
			return
		}
		emitted[key] = true
		out = append(out, rs)
	}
	resolved := map[string]bool{} // names resolved precisely — skip their bare-name fallback

	// Go: `pkg.Symbol` selectors → importpath.Symbol fqn.
	if goImp := c.goImportPaths(u); len(goImp) > 0 {
		for _, m := range goSelector.FindAllStringSubmatch(diff, -1) {
			pkg, sym := m[1], m[2]
			path, ok := goImp[pkg]
			if !ok {
				continue
			}
			fqn := path + "." + sym
			id, ok := c.byFqn[fqn]
			if !ok {
				continue
			}
			rs := RelatedSymbol{ID: id, Relation: unit.RelUsed, Name: sym, Ref: fqn}
			// doc when the resolved entry's source is in this repo (a dependency's
			// relpath isn't; its doc would need module-cache resolution — not yet).
			if rel, dn, ok := strings.Cut(id, "::"); ok && c.repoDir != "" {
				if p := filepath.Join(c.repoDir, rel); fileExists(p) {
					rs.DocFile, rs.DocName = p, dn
				}
			}
			emit(rs)
			resolved[sym] = true
		}
	}

	// Python: `from mod import Name` resolves a bare name to fqn; bare-name fallback.
	pyImp := c.pyImports(u)
	roots := pyModuleRoots(c.repoDir)
	for _, name := range identifier.FindAllString(diff, -1) {
		if resolved[name] {
			continue
		}
		if sym, ok := pyImp[name]; ok {
			fqn := sym.module + "." + sym.name
			rs := RelatedSymbol{Relation: unit.RelUsed, Name: name, Ref: fqn}
			if file, ok := resolvePyModuleFile(sym.module, roots); ok {
				rs.DocFile, rs.DocName = file, sym.name
			}
			if id, ok := c.byFqn[fqn]; ok {
				rs.ID = id
				emit(rs)
				continue // resolved precisely — skip bare-name (avoids same-name mismatch)
			}
			if rs.DocFile != "" {
				emit(rs) // unindexed but resolvable source: docstring-only (adoption-free)
			}
		}
		for _, id := range c.byName[name] {
			emit(RelatedSymbol{ID: id, Relation: unit.RelUsed, Name: name, Ref: name})
		}
	}
	return out
}

// pyImports maps the unit's Python member-file from-imports: local name -> source.
func (c usedCollector) pyImports(u unit.Unit) map[string]importedSym {
	if c.repoDir == "" {
		return nil
	}
	out := map[string]importedSym{}
	for _, p := range u.Paths() {
		if !strings.HasSuffix(p, ".py") {
			continue
		}
		if src, err := os.ReadFile(filepath.Join(c.repoDir, p)); err == nil {
			maps.Copy(out, parsePyFromImports(string(src)))
		} // best-effort: an unreadable member just skips its imports
	}
	return out
}

// goImportPaths maps the unit's Go member-file import selector names to import paths.
func (c usedCollector) goImportPaths(u unit.Unit) map[string]string {
	if c.repoDir == "" {
		return nil
	}
	out := map[string]string{}
	for _, p := range u.Paths() {
		if !strings.HasSuffix(p, ".go") {
			continue
		}
		if src, err := os.ReadFile(filepath.Join(c.repoDir, p)); err == nil {
			maps.Copy(out, parseGoImports(string(src)))
		}
	}
	return out
}

// --- the composed finder: relation axis × source axis ---

// SelfGates mirrors the spec_case/rule/link feature gates for the self relation.
// The owner/used relations are always on: cheap (file reads + map lookups) and
// correctness signals (an authored usage rule / type contract must be honored).
type SelfGates struct{ Spec, Rule, Link bool }

// RelatedFinder is the unit.ClueFinder over the self/owner/used relations.
type RelatedFinder struct {
	index      Index
	gates      SelfGates
	collectors []RelationCollector
}

func NewRelatedFinder(idx Index, repoDir string, gates SelfGates) RelatedFinder {
	return RelatedFinder{
		index: idx,
		gates: gates,
		collectors: []RelationCollector{
			selfCollector{},
			ownerCollector{repoDir: repoDir},
			newUsedCollector(idx, repoDir),
		},
	}
}

func (f RelatedFinder) Find(u unit.Unit) []unit.Clue {
	var clues []unit.Clue
	for _, c := range f.collectors {
		for _, rs := range c.Related(u) {
			clues = append(clues, f.cluesFor(rs)...)
		}
	}
	return clues
}

// cluesFor is the source axis: a related symbol's authored marks (spec index) and
// derived docstring (source file), labelled by the relation that reached it.
func (f RelatedFinder) cluesFor(rs RelatedSymbol) []unit.Clue {
	var clues []unit.Clue
	e := f.index[rs.ID]
	switch rs.Relation {
	case unit.RelSelf:
		if f.gates.Spec {
			if r := f.index.Render([]string{rs.ID}); r != "" {
				clues = append(clues, unit.Clue{Kind: unit.ClueSpec, Relation: unit.RelSelf, Text: r})
			}
		}
		if f.gates.Rule {
			for _, r := range e.Rules {
				clues = append(clues, unit.Clue{Kind: unit.ClueRule, Relation: unit.RelSelf, Text: r})
			}
		}
		if f.gates.Link {
			clues = append(clues, linkClues(e.Links, unit.RelSelf)...)
		}
	case unit.RelOwner:
		if r := f.index.Render([]string{rs.ID}); r != "" {
			clues = append(clues, unit.Clue{Kind: unit.ClueSpec, Relation: unit.RelOwner, Text: r})
		}
		for _, r := range e.Rules {
			clues = append(clues, unit.Clue{Kind: unit.ClueRule, Relation: unit.RelOwner, Text: "(enclosing type `" + rs.Name + "`) " + r})
		}
		clues = append(clues, linkClues(e.Links, unit.RelOwner)...)
	case unit.RelUsed:
		// used injects rules only: a spec/link of a merely-referenced symbol is
		// noise, but its usage rule is a constraint on this change.
		for _, r := range e.Rules {
			clues = append(clues, unit.Clue{Kind: unit.ClueRule, Relation: unit.RelUsed, Text: "(used type `" + rs.Name + "`) " + r, Ref: rs.Name})
		}
	}
	if rs.DocFile != "" {
		if doc := extractDocFromFile(rs.DocFile, rs.DocName); doc != "" {
			label := "used type `" + rs.Ref + "`"
			if rs.Relation == unit.RelOwner {
				label = "enclosing type `" + rs.Name + "`"
			}
			clues = append(clues, unit.Clue{Kind: unit.ClueDoc, Relation: rs.Relation, Text: label + " (docstring): " + doc, Ref: rs.Ref})
		}
	}
	return clues
}

// linkClues labels @link pointers doc/function for the prompt, keeping Ref for
// on-demand fetch.
func linkClues(links []string, rel unit.Relation) []unit.Clue {
	var out []unit.Clue
	for _, l := range links {
		kind := "doc"
		if strings.Contains(l, "::") {
			kind = "function"
		}
		out = append(out, unit.Clue{Kind: unit.ClueLink, Relation: rel, Text: l + " (" + kind + ")", Ref: l})
	}
	return out
}
