package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/feature"
	"github.com/qiankunli/case-code-review/internal/tool"
	"github.com/qiankunli/case-code-review/internal/unit"
)

func newPreloadAgent(t *testing.T, files map[string]string) *Agent {
	t.Helper()
	dir := t.TempDir()
	for p, content := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := tool.NewRegistry()
	reg.Register(tool.NewFileRead(&tool.FileReader{RepoDir: dir, Mode: tool.ModeWorkspace}))
	return &Agent{args: Args{RepoDir: dir, Tools: reg}}
}

// wholeFile is shorthand for an own-source material (what ownSourceBriefer emits).
func wholeFile(path string, symbols ...string) material {
	return material{path: path, symbols: symbols, whole: true}
}

func TestRenderMaterials_WholeFile(t *testing.T) {
	a := newPreloadAgent(t, map[string]string{
		"pkg/a.go": "package a\n\nfunc F() {}\n",
	})
	got, related, _ := a.renderMaterials(context.Background(), []material{wholeFile("pkg/a.go")})
	// Mirrors file_read's numbered-line format so inline source and tool output
	// look identical to the model.
	if !strings.Contains(got, "File: pkg/a.go (Total lines: 3)") {
		t.Fatalf("missing file header, got:\n%s", got)
	}
	if !strings.Contains(got, "1|package a") || !strings.Contains(got, "3|func F() {}") {
		t.Fatalf("missing numbered lines, got:\n%s", got)
	}
	if related != "" {
		t.Fatalf("no aux materials, want empty related source, got:\n%s", related)
	}

	// A missing path is skipped; when nothing could be inlined the sentinel fills
	// the placeholder instead of an empty block.
	if got, _, _ := a.renderMaterials(context.Background(), []material{wholeFile("gone.go")}); got != sourceNotPreloaded {
		t.Fatalf("want sentinel for unreadable paths, got:\n%s", got)
	}

	// No file_read tool registered → sentinel (preload is best-effort only).
	a3 := &Agent{args: Args{Tools: tool.NewRegistry()}}
	if got, _, _ := a3.renderMaterials(context.Background(), nil); got != sourceNotPreloaded {
		t.Fatalf("want sentinel without FileReader, got: %s", got)
	}
}

func TestRenderMaterials_BudgetAndRangedFallback(t *testing.T) {
	// A file over the budget with no symbols is named but not inlined; the budget
	// failure of one file doesn't block a later small file.
	big := strings.Repeat("x", preloadSourceBudget+1)
	a := newPreloadAgent(t, map[string]string{"big.go": big, "small.go": "ok\n"})
	got, _, outcomes := a.renderMaterials(context.Background(), []material{wholeFile("big.go"), wholeFile("small.go")})
	if strings.Contains(got, big[:64]) {
		t.Fatal("oversized file must not be inlined")
	}
	if !strings.Contains(got, "exceeds the preload budget") || !strings.Contains(got, "1|ok") {
		t.Fatalf("want budget note + small file inlined, got:\n%s", got[:200])
	}
	// Each material's fate is reported for the unit's debrief.
	if len(outcomes) != 2 || outcomes[0] != "budget_miss big.go" || outcomes[1] != "whole small.go" {
		t.Fatalf("material outcomes off: %v", outcomes)
	}

	// With symbols, an over-budget file falls back to just those functions' bodies
	// (ranged_preload gate, on by default) instead of being dropped wholesale.
	filler := "// " + strings.Repeat("y", 120)
	var sb strings.Builder
	sb.WriteString("package big\n\n")
	for range preloadSourceBudget / len(filler) {
		sb.WriteString(filler + "\n")
	}
	sb.WriteString("\nfunc Changed() int {\n\treturn 42\n}\n")
	a2 := newPreloadAgent(t, map[string]string{"big.go": sb.String()})
	got2, _, _ := a2.renderMaterials(context.Background(), []material{wholeFile("big.go", "big.go::Changed")})
	if !strings.Contains(got2, "LINE_RANGE: ") || !strings.Contains(got2, "func Changed() int {") {
		t.Fatalf("want ranged fallback with the changed function's body, got:\n%.300s", got2)
	}
	if strings.Contains(got2, filler) {
		t.Fatal("ranged fallback must not inline the rest of the file")
	}

	// Same file, ranged_preload off → back to the named-but-not-inlined note.
	a2.features = feature.Set{feature.RangedPreload: false}
	got3, _, _ := a2.renderMaterials(context.Background(), []material{wholeFile("big.go", "big.go::Changed")})
	if !strings.Contains(got3, "exceeds the preload budget") || strings.Contains(got3, "LINE_RANGE") {
		t.Fatalf("gate off must disable the ranged fallback, got:\n%.300s", got3)
	}
}

func TestRenderMaterials_RelatedBodiesSplitAndPriority(t *testing.T) {
	a := newPreloadAgent(t, map[string]string{
		"own.go":      "package p\n\nfunc Changed() {}\n",
		"neighbor.go": "package p\n\nfunc Caller() {\n\tChanged()\n}\n",
	})
	mats := []material{
		wholeFile("own.go", "own.go::Changed"),
		{path: "neighbor.go", symbols: []string{"neighbor.go::Caller"}, label: "caller neighbor.go::Caller", prio: 1},
	}
	unitSource, related, _ := a.renderMaterials(context.Background(), mats)
	if !strings.Contains(unitSource, "File: own.go") || strings.Contains(unitSource, "neighbor.go") {
		t.Fatalf("own source block polluted:\n%s", unitSource)
	}
	if !strings.Contains(related, "// caller neighbor.go::Caller") ||
		!strings.Contains(related, "LINE_RANGE: 3-5") ||
		!strings.Contains(related, "func Caller() {") {
		t.Fatalf("neighbor body missing from related source:\n%s", related)
	}
	// The whole neighbor file is not inlined — only the named function's span.
	if strings.Contains(related, "1|package p") {
		t.Fatalf("related source must carry the body only, got:\n%s", related)
	}
}

func TestBrieferFor_Scopes(t *testing.T) {
	a := &Agent{}
	frag := unit.Fragment{Path: "a.go", Symbols: []string{"a.go::F"}}
	fn := unit.UnitOf(frag)
	mats := a.brieferFor(fn.Scope).materials(fn)
	if len(mats) != 1 || !mats[0].whole || mats[0].path != "a.go" || mats[0].symbols[0] != "a.go::F" {
		t.Fatalf("func briefer materials off: %+v", mats)
	}

	// A chain unit adds neighbor bodies from its dossier's caller/callee refs —
	// but never a member's own file, and only when the neighbor_source gate is on.
	chain := unit.NewChainUnit([]unit.Fragment{
		{Path: "a.go", Symbols: []string{"a.go::F"}},
		{Path: "b.go", Symbols: []string{"b.go::G"}},
	})
	chain.Dossier = unit.Dossier{
		{Kind: unit.ClueSpec, Relation: unit.RelCaller, Ref: "c.go::Entry", Text: "spec"},
		{Kind: unit.ClueDoc, Relation: unit.RelCallee, Ref: "a.go::F2", Text: "member file — skip"},
		{Kind: unit.ClueSpec, Relation: unit.RelOwner, Ref: "d.go::T", Text: "not a call edge — skip"},
	}
	mats = a.brieferFor(chain.Scope).materials(chain)
	var related []material
	for _, m := range mats {
		if m.prio > 0 {
			related = append(related, m)
		}
	}
	if len(related) != 1 || related[0].path != "c.go" || related[0].symbols[0] != "c.go::Entry" {
		t.Fatalf("chain neighbor materials off: %+v", related)
	}

	// neighbor_source off → member source only.
	a.features = feature.Set{feature.NeighborSource: false}
	for _, m := range a.brieferFor(chain.Scope).materials(chain) {
		if m.prio > 0 {
			t.Fatalf("gate off must drop neighbor materials, got %+v", m)
		}
	}
}
