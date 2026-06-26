package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = `{
  "internal/notebook/handler.go::Service.CreateNotebook": {
    "spec": "tenant/user header required; (tenant,user,name) unique",
    "cases": [
      { "id": "happy_minimal", "desc": "name-only create succeeds", "expect": "201" },
      { "id": "duplicate_name", "desc": "duplicate name", "expect": "409", "forbid": "second row written" }
    ]
  },
  "internal/notebook/handler.go::Service.GetNotebook": {
    "spec": "locate by id or name; never cross tenant"
  }
}`

func TestParseAndRender(t *testing.T) {
	idx, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}

	// A unit covering the CreateNotebook symbol gets its spec + both cases.
	out := idx.Render([]string{"internal/notebook/handler.go::Service.CreateNotebook"})
	for _, want := range []string{
		"Service.CreateNotebook",
		"spec: tenant/user header required",
		"- happy_minimal: name-only create succeeds [expect: 201]",
		"[forbid: second row written]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q in:\n%s", want, out)
		}
	}

	// A symbol with no spec/case match contributes nothing.
	if got := idx.Render([]string{"internal/x/y.go::Unknown"}); got != "" {
		t.Errorf("unknown symbol should render empty, got %q", got)
	}

	// A coalesced unit covering several symbols unions their specs.
	multi := idx.Render([]string{
		"internal/notebook/handler.go::Service.CreateNotebook",
		"internal/notebook/handler.go::Service.GetNotebook",
	})
	if !strings.Contains(multi, "CreateNotebook") || !strings.Contains(multi, "GetNotebook") {
		t.Errorf("coalesced render should cover both symbols:\n%s", multi)
	}
}

func TestRenderNilIndexSafe(t *testing.T) {
	var idx Index // nil
	if got := idx.Render([]string{"any::Symbol"}); got != "" {
		t.Errorf("nil index should render empty, got %q", got)
	}
}

func TestRenderRulesAndLinks(t *testing.T) {
	idx, err := Parse([]byte(`{
	  "a.go::Foo": {
	    "rules": ["hot path; watch new sync DB calls"],
	    "links": ["docs/x.md", "a.go::Bar"]
	  }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	syms := []string{"a.go::Foo"}

	if got := idx.RenderRules(syms); !strings.Contains(got, "hot path; watch new sync DB calls") {
		t.Errorf("rules render: %q", got)
	}
	links := idx.RenderLinks(syms)
	if !strings.Contains(links, "docs/x.md (doc)") || !strings.Contains(links, "a.go::Bar (function)") {
		t.Errorf("links render should label doc vs function: %q", links)
	}

	// nil-safe + no-match -> empty
	var nilIdx Index
	if nilIdx.RenderRules(syms) != "" || nilIdx.RenderLinks(syms) != "" {
		t.Error("nil index should render empty for rules/links")
	}
	if idx.RenderRules([]string{"a.go::Unknown"}) != "" {
		t.Error("unknown symbol should render empty rules")
	}
}

func TestLoadChainCustomOverridesProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate ~/.ccr/spec.json (none)
	repo := t.TempDir()
	mustWrite(t, filepath.Join(repo, ".ccr", "spec.json"), `{
	  "a.go::Foo": {"spec": "project-foo"},
	  "a.go::Bar": {"spec": "project-bar"}
	}`)
	custom := filepath.Join(t.TempDir(), "custom.json")
	mustWrite(t, custom, `{
	  "a.go::Foo": {"spec": "custom-foo"},
	  "a.go::Baz": {"spec": "custom-baz"}
	}`)

	idx, err := Load(repo, custom)
	if err != nil {
		t.Fatal(err)
	}
	if idx["a.go::Foo"].Spec != "custom-foo" {
		t.Errorf("custom should override project: got %q", idx["a.go::Foo"].Spec)
	}
	if idx["a.go::Bar"].Spec != "project-bar" {
		t.Errorf("project-only entry should survive: got %q", idx["a.go::Bar"].Spec)
	}
	if idx["a.go::Baz"].Spec != "custom-baz" {
		t.Errorf("custom-only entry missing")
	}
}

func TestLoadNoLayersReturnsNil(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	idx, err := Load(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if idx != nil {
		t.Errorf("no layers should return nil, got %v", idx)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
