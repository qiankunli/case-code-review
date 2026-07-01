package agent

import (
	"testing"

	"github.com/qiankunli/case-code-review/internal/unit"
)

func TestDedupClues(t *testing.T) {
	in := unit.Dossier{
		{Kind: unit.ClueSpec, Relation: unit.RelSelf, Text: "contract"},
		{Kind: unit.ClueSpec, Relation: unit.RelSelf, Text: "contract"},   // exact dup → dropped
		{Kind: unit.ClueSpec, Relation: unit.RelOwner, Text: "contract"},  // same text, diff relation → kept
		{Kind: unit.ClueRule, Relation: unit.RelUsed, Text: "per-request"},
	}
	got := dedupClues(in)
	if len(got) != 3 {
		t.Fatalf("want 3 after dedup, got %d: %+v", len(got), got)
	}
}

func TestCountClues_RelationKindMatrix(t *testing.T) {
	d := unit.Dossier{
		{Kind: unit.ClueSpec, Relation: unit.RelSelf},
		{Kind: unit.ClueRule, Relation: unit.RelOwner},
		{Kind: unit.ClueDoc, Relation: unit.RelUsed},
		{Kind: unit.ClueSpec, Relation: unit.RelCaller},
	}
	m := countClues(d)
	for _, cell := range []string{"self/spec", "owner/rule", "used/doc", "caller/spec"} {
		if m[cell] != 1 {
			t.Errorf("matrix cell %q = %d, want 1 (got %v)", cell, m[cell], m)
		}
	}
}
