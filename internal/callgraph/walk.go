package callgraph

import (
	"github.com/qiankunli/case-code-review/internal/spec"
	"github.com/qiankunli/case-code-review/internal/unit"
)

const defaultDepth = 2

// neighborFunc returns the call-graph neighbors of a function (its callers, or
// its callees) as unit-ids. CallerFinder and CalleeFinder differ only in this.
type neighborFunc func(funcID string) []string

// walkForSpecs walks up to depth hops outward from start along neighborFn,
// emitting a clue (via mkClue) for each neighbor that carries a spec. It keeps
// the NEAREST spec-bearing neighbor on each branch: a spec-bearing neighbor is
// emitted and not recursed past; a neighbor with no spec of its own is followed
// one hop further (until depth runs out). visited breaks cycles and dedupes;
// max caps the emitted clues. This is the shared engine for caller (walk up to
// the governing spec) and callee (walk down to depended-on contracts).
func walkForSpecs(idx spec.Index, start []string, neighborFn neighborFunc, depth, max int, mkClue func(id string) unit.Clue) []unit.Clue {
	if depth <= 0 {
		depth = defaultDepth
	}
	visited := map[string]bool{}
	for _, s := range start {
		visited[s] = true
	}
	frontier := append([]string(nil), start...)

	var clues []unit.Clue
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []string
		for _, f := range frontier {
			for _, nb := range neighborFn(f) {
				if visited[nb] {
					continue
				}
				visited[nb] = true
				if e, ok := idx[nb]; ok && (e.Spec != "" || len(e.Cases) > 0) {
					clues = append(clues, mkClue(nb))
					if len(clues) >= max {
						return clues
					}
					continue // nearest spec on this branch — don't recurse past it
				}
				next = append(next, nb) // no spec yet — follow one hop further
			}
		}
		frontier = next
	}
	return clues
}
