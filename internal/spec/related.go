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

// identifier matches a source identifier (Go/Python/JavaScript-family); used to scan a Unit's diff
// for names it references.
var identifier = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

// RelatedSymbol is one symbol reached from a review unit along a typed relation —
// what the relation axis hands to the source axis.
type RelatedSymbol struct {
	ID       string // local symbol-id ("" when the symbol isn't in this repo's index)
	Relation unit.Relation
	Name     string // bare name as referenced (labels authored marks)
	Ref      string // Clue.Ref for the doc clue (owner: symbol-id; used: fqn)
	DocFile  string // source file for docstring extraction ("" = no doc)
	DocName  string // symbol name inside DocFile
	// Entry is the resolved spec entry when the collector already knows it —
	// required for dependency symbols, which have no local symbol-id (they
	// resolve by fqn). Nil means "look ID up in the local index".
	Entry *Entry
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
// **local** index's non-method symbols. Bare names never match dependency
// entries: a dependency symbol is only reachable through its fqn (its relpath
// keys belong to another repo's address space). An import-resolved symbol also
// carries its source file, so its docstring is available even when it has no
// spec entry (adoption-free).
type usedCollector struct {
	byName  map[string][]string // bare symbol name -> local symbol-ids (non-method only)
	byFqn   map[string]fqnHit   // fqn -> resolved entry (local entries win over deps)
	repoDir string
}

// fqnHit is one fqn-resolved entry; id is "" for a dependency entry (no local
// symbol-id exists for it).
type fqnHit struct {
	id    string
	entry Entry
}

// newUsedCollector precomputes the name/fqn indexes once (not per Unit). Local
// symbol-ids are processed in sorted order so the winner is deterministic when
// two entries share an fqn (possible across merged spec.json layers); local
// entries override dependency entries on the same fqn.
func newUsedCollector(cat Catalog, repoDir string) usedCollector {
	byName := make(map[string][]string)
	byFqn := make(map[string]fqnHit)
	for fqn, e := range cat.Deps {
		byFqn[fqn] = fqnHit{entry: e}
	}
	ids := make([]string, 0, len(cat.Local))
	for id := range cat.Local {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := cat.Local[id]
		if e.Fqn != "" {
			byFqn[e.Fqn] = fqnHit{id: id, entry: e}
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
			hit, ok := c.byFqn[fqn]
			if !ok {
				continue
			}
			rs := RelatedSymbol{ID: hit.id, Relation: unit.RelUsed, Name: sym, Ref: fqn, Entry: &hit.entry}
			// doc when the resolved entry is local, so its relpath is this repo's (a
			// dependency's doc would need module-cache resolution — not yet).
			if rel, dn, ok := strings.Cut(hit.id, "::"); ok && c.repoDir != "" {
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
			if hit, ok := c.byFqn[fqn]; ok {
				rs.ID, rs.Entry = hit.id, &hit.entry
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

// KindGates mirrors the spec_case/rule/link/doc feature gates. A gate switches
// its clue KIND off across every relation (self/owner/used alike), so an
// ablation run measures "ccr without that evidence kind" — the gate axis and the
// dry-run relation×kind matrix share one coordinate system. Relations themselves
// are not gated: they're the cheap mechanism, kinds are the evidence.
type KindGates struct{ Spec, Rule, Link, Doc bool }

// RelatedFinder is the unit.ClueFinder over the self/owner/used relations.
type RelatedFinder struct {
	local      Index // this repo's entries; dependency entries reach cluesFor via RelatedSymbol.Entry
	gates      KindGates
	collectors []RelationCollector
}

func NewRelatedFinder(cat Catalog, repoDir string, gates KindGates) RelatedFinder {
	return RelatedFinder{
		local: cat.Local,
		gates: gates,
		collectors: []RelationCollector{
			selfCollector{},
			ownerCollector{repoDir: repoDir},
			newUsedCollector(cat, repoDir),
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

// cluesFor is the source axis: a related symbol's authored marks (its resolved
// entry, or a local-index lookup) and derived docstring (source file). Text is
// RAW content and Ref the source identity — how a clue reached the unit is
// worded at render time from (relation, kind, ref), not here.
func (f RelatedFinder) cluesFor(rs RelatedSymbol) []unit.Clue {
	var clues []unit.Clue
	e := rs.Entry
	if e == nil {
		local := f.local[rs.ID]
		e = &local
	}
	switch rs.Relation {
	case unit.RelSelf:
		if f.gates.Spec {
			if r := f.local.Render([]string{rs.ID}); r != "" {
				clues = append(clues, unit.Clue{Kind: unit.ClueSpec, Relation: unit.RelSelf, Text: r, Ref: rs.ID})
			}
		}
		if f.gates.Rule {
			for _, r := range e.Rules {
				clues = append(clues, unit.Clue{Kind: unit.ClueRule, Relation: unit.RelSelf, Text: r, Ref: rs.ID})
			}
		}
		if f.gates.Link {
			clues = append(clues, linkClues(e.Links, unit.RelSelf)...)
		}
	case unit.RelOwner:
		if f.gates.Spec {
			if r := f.local.Render([]string{rs.ID}); r != "" {
				clues = append(clues, unit.Clue{Kind: unit.ClueSpec, Relation: unit.RelOwner, Text: r, Ref: rs.ID})
			}
		}
		if f.gates.Rule {
			for _, r := range e.Rules {
				clues = append(clues, unit.Clue{Kind: unit.ClueRule, Relation: unit.RelOwner, Text: r, Ref: rs.ID})
			}
		}
		if f.gates.Link {
			clues = append(clues, linkClues(e.Links, unit.RelOwner)...)
		}
	case unit.RelUsed:
		// used injects the referenced symbol's contract (spec) and usage rules —
		// both are constraints on this change. Its cases/links stay out: another
		// symbol's scenario checklist and see-alsos are noise here.
		if f.gates.Spec && e.Spec != "" {
			clues = append(clues, unit.Clue{Kind: unit.ClueSpec, Relation: unit.RelUsed, Text: e.Spec, Ref: rs.Name})
		}
		if f.gates.Rule {
			for _, r := range e.Rules {
				clues = append(clues, unit.Clue{Kind: unit.ClueRule, Relation: unit.RelUsed, Text: r, Ref: rs.Name})
			}
		}
	}
	if f.gates.Doc && rs.DocFile != "" {
		if doc := extractDocFromFile(rs.DocFile, rs.DocName); doc != "" {
			clues = append(clues, unit.Clue{Kind: unit.ClueDoc, Relation: rs.Relation, Text: doc, Ref: rs.Ref})
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
