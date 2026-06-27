package agent

import (
	"testing"

	"github.com/qiankunli/case-code-review/internal/model"
	"github.com/qiankunli/case-code-review/internal/unit"
)

// countingFinder records how many times it is asked to find clues.
type countingFinder struct{ n *int }

func (f countingFinder) Find(unit.Unit) []unit.Clue { *f.n++; return nil }

func TestSplitUnits_CostlyFindersGatedByBudget(t *testing.T) {
	// Below the watermark: units stay fine-grained → costly finder runs per unit.
	var under int
	au := &Agent{
		splitter:      unit.GoFuncSplitter{},
		diffs:         []model.Diff{goDiff("p.go", 3)},
		costlyFinders: []unit.ClueFinder{countingFinder{&under}},
	}
	if _, err := au.splitUnits(); err != nil {
		t.Fatal(err)
	}
	if under != 3 {
		t.Errorf("under watermark: costly finder should run once per diff unit, got %d", under)
	}

	// Above the watermark: units will coalesce → costly finder skipped entirely.
	var over int
	ao := &Agent{
		splitter:      unit.GoFuncSplitter{},
		diffs:         []model.Diff{goDiff("p.go", defaultUnitWatermark+2)},
		costlyFinders: []unit.ClueFinder{countingFinder{&over}},
	}
	if _, err := ao.splitUnits(); err != nil {
		t.Fatal(err)
	}
	if over != 0 {
		t.Errorf("over watermark: costly finder should be skipped, got %d calls", over)
	}
}

func TestRenderClues(t *testing.T) {
	specCases, rules, seeAlso := renderClues([]unit.Clue{
		{Kind: unit.ClueSpec, Text: "F spec\n  - case"},
		{Kind: unit.ClueRule, Text: "watch DB"},
		{Kind: unit.ClueRule, Text: "hot path"},
		{Kind: unit.ClueLink, Text: "docs/x.md (doc)"},
	})
	if specCases != "F spec\n  - case" {
		t.Errorf("specCases: %q", specCases)
	}
	if rules != "- watch DB\n- hot path" {
		t.Errorf("rules: %q", rules)
	}
	if seeAlso != "- docs/x.md (doc)" {
		t.Errorf("seeAlso: %q", seeAlso)
	}

	if s, r, l := renderClues(nil); s != "" || r != "" || l != "" {
		t.Errorf("empty clues should render empty: %q / %q / %q", s, r, l)
	}
}
