package agent

import (
	"testing"

	"github.com/qiankunli/case-code-review/internal/feature"
)

// New should assemble clue finders per the feature gates: the four cheap finders
// (spec/rule/link/history) and the two costly ones (caller/callee) when on;
// disabled clue kinds drop their finder entirely.
func TestNew_ClueGatesControlFinderAssembly(t *testing.T) {
	full := New(Args{}) // nil Features → all gates on
	if len(full.finders) != 4 {
		t.Errorf("all-on: want 4 cheap finders (spec/rule/link/history), got %d", len(full.finders))
	}
	if len(full.costlyFinders) != 2 {
		t.Errorf("all-on: want 2 costly finders (caller/callee), got %d", len(full.costlyFinders))
	}

	off, err := feature.Resolve(map[feature.Gate]bool{
		feature.CallerCallee: false,
		feature.SpecCase:     false,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := New(Args{Features: off})
	if len(a.costlyFinders) != 0 {
		t.Errorf("caller_callee off: want 0 costly finders, got %d", len(a.costlyFinders))
	}
	if len(a.finders) != 3 { // rule, link, history remain
		t.Errorf("spec_case off: want 3 cheap finders, got %d", len(a.finders))
	}
}
