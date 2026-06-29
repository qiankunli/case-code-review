package spec

import (
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
		return []unit.Clue{{Kind: unit.ClueSpec, Text: r}}
	}
	return nil
}

// RuleFinder yields one rule clue per @rule on the Unit's symbols.
type RuleFinder struct{ Index Index }

func (f RuleFinder) Find(u unit.Unit) []unit.Clue {
	var clues []unit.Clue
	for _, sym := range u.AllSymbols() {
		for _, r := range f.Index[sym].Rules {
			clues = append(clues, unit.Clue{Kind: unit.ClueRule, Text: r})
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
			clues = append(clues, unit.Clue{Kind: unit.ClueLink, Text: l + " (" + kind + ")", Ref: l})
		}
	}
	return clues
}
