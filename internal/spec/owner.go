package spec

import (
	"strings"

	"github.com/qiankunli/case-code-review/internal/unit"
)

// OwnerFinder yields the spec/case/rule/link of a changed method's (or nested
// func's) *enclosing* symbol — its class/type, or outer func. Without it, a
// class-level marker only fires when the whole class is the changed symbol, which
// almost never happens; changing `Svc.create` must still surface class `Svc`'s
// class-level @rule ("Svc is per-request — do not cache"). This is the enclosing
// (owner) axis, distinct from the changed symbol's own markers (SpecFinder et al).
//
// The enclosing symbol-id is the changed symbol-id with its trailing `.segment`
// stripped (`a::C.m` → `a::C`, Go `a::Recv.Method` → `a::Recv`); a top-level
// symbol (no dot in the symbol part) has no owner.
type OwnerFinder struct{ Index Index }

func (f OwnerFinder) Find(u unit.Unit) []unit.Clue {
	own := make(map[string]bool, len(u.AllSymbols()))
	for _, s := range u.AllSymbols() {
		own[s] = true
	}
	var clues []unit.Clue
	seen := make(map[string]bool)
	for _, sym := range u.AllSymbols() {
		owner, ok := enclosingSymbol(sym)
		if !ok || own[owner] || seen[owner] {
			continue // no owner, or the owner is itself a changed symbol (SpecFinder covers it)
		}
		seen[owner] = true
		e := f.Index[owner]
		name := ownerName(owner)
		if r := f.Index.Render([]string{owner}); r != "" {
			clues = append(clues, unit.Clue{Kind: unit.ClueSpec, Text: r})
		}
		for _, ru := range e.Rules {
			clues = append(clues, unit.Clue{Kind: unit.ClueRule, Text: "(enclosing type `" + name + "`) " + ru})
		}
		for _, l := range e.Links {
			kind := "doc"
			if strings.Contains(l, "::") {
				kind = "function"
			}
			clues = append(clues, unit.Clue{Kind: unit.ClueLink, Text: l + " (" + kind + ")", Ref: l})
		}
	}
	return clues
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
