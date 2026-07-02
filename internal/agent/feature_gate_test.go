package agent

import (
	"testing"

	"github.com/qiankunli/case-code-review/internal/feature"
)

// New should assemble clue finders per the feature gates: the RelatedFinder
// (self/owner/used; self kinds gated inside it — see spec.SelfGates tests) plus
// history when on, and the two costly ones (caller/callee) when on.
func TestNew_ClueGatesControlFinderAssembly(t *testing.T) {
	full := New(Args{}) // nil Features → all gates on
	if len(full.finders) != 2 {
		t.Errorf("all-on: want 2 cheap finders (related + history), got %d", len(full.finders))
	}
	if len(full.costlyFinders) != 2 {
		t.Errorf("all-on: want 2 costly finders (caller/callee), got %d", len(full.costlyFinders))
	}

	off, err := feature.Resolve(map[feature.Gate]bool{
		feature.CallerCallee: false,
		feature.History:      false,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := New(Args{Features: off})
	if len(a.costlyFinders) != 0 {
		t.Errorf("caller_callee off: want 0 costly finders, got %d", len(a.costlyFinders))
	}
	if len(a.finders) != 1 { // related only (history gated off)
		t.Errorf("history off: want 1 cheap finder, got %d", len(a.finders))
	}
}
