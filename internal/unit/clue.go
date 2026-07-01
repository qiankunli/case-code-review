package unit

// ClueKind identifies what kind of review context a Clue carries.
type ClueKind string

const (
	ClueSpec ClueKind = "spec" // contract + cases bound to the function
	ClueRule ClueKind = "rule" // function-level review criteria (@rule)
	ClueLink ClueKind = "link" // curated see-also pointer (@link)
	// Reserved for a future call-graph finder; not emitted yet.
	ClueCaller ClueKind = "caller"
	ClueCallee ClueKind = "callee"
	// ClueHistory carries a prior review's findings on this unit, so the reviewer
	// can judge whether the current change addresses them (the review-history
	// feedback loop — a per-unit input alongside spec/rule).
	ClueHistory ClueKind = "history"
	// ClueRef carries a rule from a symbol the diff *references* (not the changed
	// symbol itself) — e.g. a type used in the change whose class-level @rule is a
	// usage constraint ("per-request only"). Reference axis, not the own-symbol axis.
	ClueRef ClueKind = "ref"
)

// Clue is one piece of context found for a review Unit — a contract, a review
// rule, a see-also pointer, a caller, etc. Text is the inline content shown to
// the reviewer; Ref is an optional pointer (a doc path or a symbol-id) to fetch on
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
