package spec

import (
	"os"
	"path/filepath"
	"regexp"
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

// NewReferenceFinder precomputes the name/fqn -> symbol-id indexes once (not per Unit).
func NewReferenceFinder(idx Index, repoDir string) ReferenceFinder {
	byName := make(map[string][]string)
	byFqn := make(map[string]string)
	for id, e := range idx {
		if e.Fqn != "" {
			byFqn[e.Fqn] = id
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
	imports := f.importFqns(u) // referenced name -> fqn (from the unit's Python imports)

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
	for _, name := range identifier.FindAllString(u.Diff(), -1) {
		if fqn, ok := imports[name]; ok { // precise: import-resolved fqn
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

// importFqns resolves the unit's Python member-file imports to fqns (local name ->
// module.realname), so a referenced name can be matched precisely against fqn-keyed
// entries. Empty when there's no repoDir or no Python members.
func (f ReferenceFinder) importFqns(u unit.Unit) map[string]string {
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
			continue
		}
		for local, sym := range parsePyFromImports(string(src)) {
			out[local] = sym.module + "." + sym.name
		}
	}
	return out
}
