package codegraph

import (
	"os"
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/spec"
	"github.com/qiankunli/case-code-review/internal/unit"
)

func requireTypeScriptCompiler(t *testing.T) {
	t.Helper()
	if os.Getenv("NODE_PATH") == "" {
		t.Skip("NODE_PATH does not provide a TypeScript compiler for this test")
	}
}

func TestCalleeFinder_TypeScript(t *testing.T) {
	requireTypeScriptCompiler(t)
	repo := newRepo(t, map[string]string{
		"svc.ts":      "export function create() {\n  return validate();\n}\n",
		"validate.ts": "export function validate() {\n  return true;\n}\n",
	})
	idx, err := spec.Parse([]byte(`{"validate.ts::validate": {"spec": "reject invalid input"}}`))
	if err != nil {
		t.Fatal(err)
	}
	u := unit.UnitOf(unit.Fragment{Path: "svc.ts", Symbols: []string{"svc.ts::create"}})
	clues := (CalleeFinder{RepoDir: repo, Index: idx, Kinds: spec.KindGates{Spec: true}}).Find(u)
	if len(clues) != 1 || clues[0].Ref != "validate.ts::validate" ||
		!strings.Contains(clues[0].Text, "reject invalid input") {
		t.Fatalf("want TypeScript callee contract, got %+v", clues)
	}
}

func TestCallerFinder_TypeScript(t *testing.T) {
	requireTypeScriptCompiler(t)
	repo := newRepo(t, map[string]string{
		"entry.ts":  "export const handle = () => {\n  return helper();\n};\n",
		"helper.ts": "export const helper = () => true;\n",
	})
	idx, err := spec.Parse([]byte(`{"entry.ts::handle": {"spec": "governing contract"}}`))
	if err != nil {
		t.Fatal(err)
	}
	u := unit.UnitOf(unit.Fragment{Path: "helper.ts", Symbols: []string{"helper.ts::helper"}})
	clues := (CallerFinder{RepoDir: repo, Index: idx, Kinds: spec.KindGates{Spec: true}}).Find(u)
	if len(clues) != 1 || clues[0].Ref != "entry.ts::handle" {
		t.Fatalf("want TypeScript caller contract, got %+v", clues)
	}
}
