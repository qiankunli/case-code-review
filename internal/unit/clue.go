package unit

import "slices"

// ClueKind identifies what kind of review context a Clue carries.
type ClueKind string

const (
	ClueSpec ClueKind = "spec" // contract + cases bound to the function
	ClueRule ClueKind = "rule" // function-level review criteria (@rule)
	ClueLink ClueKind = "link" // curated see-also pointer (@link)
	// Reserved for a future call-graph finder; not emitted yet.
	ClueCaller ClueKind = "caller"
	ClueCallee ClueKind = "callee"
)

// Clue is one piece of context found for a review Unit — a contract, a review
// rule, a see-also pointer, a caller, etc. Text is the inline content shown to
// the reviewer; Ref is an optional pointer (a doc path or a unit-id) to fetch on
// demand, left empty when the clue is fully inline. Finding is separated from
// rendering: a ClueFinder produces Clues, the review prompt renders them.
type Clue struct {
	Kind ClueKind
	Text string
	Ref  string
}

// ClueFinder finds the Clues relevant to reviewing one Unit. There is one
// implementation per context source — spec/rule/link from spec.json today, and
// later caller/callee from a call graph. Returning nil is fine. An expensive
// finder's bounding strategy (e.g. how far to walk callers) lives inside the
// finder, not in this interface, so adding caller/callee needs no change here.
type ClueFinder interface {
	Find(u Unit) []Clue
}

// addClues appends src to dst, skipping exact duplicates. CoalesceFile uses it
// to union the clues of the functions it merges into one file Unit (same path /
// rule clue can otherwise repeat).
func addClues(dst, src []Clue) []Clue {
	for _, c := range src {
		if !slices.Contains(dst, c) {
			dst = append(dst, c)
		}
	}
	return dst
}
