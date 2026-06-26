package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/model"
	"github.com/qiankunli/case-code-review/internal/unit"
)

// goFileAndDiff builds a Go file of n trivial functions (f0..f{n-1}) and a diff
// that changes one line in each, so GoFuncSplitter yields n function Units.
func goFileAndDiff(n int) (src, rawDiff string) {
	var s strings.Builder
	s.WriteString("package p\n\n")
	for i := range n {
		fmt.Fprintf(&s, "func f%d() {\n\t_ = %d\n}\n\n", i, i)
	}
	var d strings.Builder
	d.WriteString("diff --git a/p.go b/p.go\n--- a/p.go\n+++ b/p.go\n")
	for i := range n {
		start := 3 + i*4 // each func block + trailing blank line is 4 lines, first at line 3
		fmt.Fprintf(&d, "@@ -%d,3 +%d,3 @@\n func f%d() {\n-\t_ = old\n+\t_ = %d\n }\n", start, start, i, i)
	}
	return s.String(), d.String()
}

func splitOf(t *testing.T, n int) []unit.Unit {
	t.Helper()
	src, diff := goFileAndDiff(n)
	a := &Agent{
		splitter: unit.GoFuncSplitter{},
		diffs:    []model.Diff{{NewPath: "p.go", Diff: diff, NewFileContent: src}},
	}
	units, err := a.splitUnits()
	if err != nil {
		t.Fatal(err)
	}
	return units
}

func TestSplitUnits_UnderBudgetKeepsFunctions(t *testing.T) {
	units := splitOf(t, 3)
	if len(units) != 3 {
		t.Fatalf("want 3 function units, got %d", len(units))
	}
	for _, u := range units {
		if u.Scope != unit.ScopeFunc {
			t.Errorf("unit %s: scope %v, want func", u.ID, u.Scope)
		}
	}
}

func TestSplitUnits_GovernorCoarsensOverBudget(t *testing.T) {
	// One file splitting into more Units than the budget coarsens back to a
	// single file Unit (trading focus for fewer loops).
	units := splitOf(t, defaultUnitBudget+2)
	if len(units) != 1 || units[0].Scope != unit.ScopeFile {
		t.Fatalf("want 1 coarsened file unit, got %d units (%+v)", len(units), units)
	}
}
