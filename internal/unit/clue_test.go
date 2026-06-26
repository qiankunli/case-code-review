package unit

import (
	"testing"

	"github.com/qiankunli/case-code-review/internal/model"
)

func TestCoalesceFileUnionsAndDedupsClues(t *testing.T) {
	d := model.Diff{NewPath: "a.go", Diff: "@@ -1 +1 @@\n-a\n+b\n"}
	m1 := Unit{Path: "a.go", Symbols: []string{"a.go::F1"}, Clues: []Clue{
		{Kind: ClueRule, Text: "shared"}, // identical to m2's -> deduped
		{Kind: ClueSpec, Text: "F1 spec"},
	}}
	m2 := Unit{Path: "a.go", Symbols: []string{"a.go::F2"}, Clues: []Clue{
		{Kind: ClueRule, Text: "shared"},
		{Kind: ClueSpec, Text: "F2 spec"},
	}}

	merged := CoalesceFile(d, []Unit{m1, m2})
	if len(merged.Symbols) != 2 {
		t.Errorf("want both symbols retained, got %v", merged.Symbols)
	}
	// shared rule once + F1 spec + F2 spec = 3
	if len(merged.Clues) != 3 {
		t.Fatalf("want 3 unioned/deduped clues, got %d: %+v", len(merged.Clues), merged.Clues)
	}
}
