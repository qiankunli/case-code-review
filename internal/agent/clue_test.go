package agent

import (
	"testing"

	"github.com/qiankunli/case-code-review/internal/unit"
)

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
