package spec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGoRequires(t *testing.T) {
	gomod := `module github.com/org/app

go 1.25

require github.com/org/framework v1.2.3

require (
	github.com/a/b v0.1.0 // indirect
	github.com/c/d v2.0.0
)
`
	got := parseGoRequires(gomod)
	want := map[string]string{
		"github.com/org/framework": "v1.2.3",
		"github.com/a/b":           "v0.1.0",
		"github.com/c/d":           "v2.0.0",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestEscapeModulePath(t *testing.T) {
	cases := map[string]string{
		"github.com/org/framework":   "github.com/org/framework",
		"github.com/Org/Framework":   "github.com/!org/!framework",
		"github.com/BurntSushi/toml": "github.com/!burnt!sushi/toml",
	}
	for in, want := range cases {
		if got := escapeModulePath(in); got != want {
			t.Errorf("escape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadDepSpecs_GoModCache(t *testing.T) {
	repo := t.TempDir()
	cache := t.TempDir()
	t.Setenv("GOMODCACHE", cache)

	// repo requires a dependency that ships a spec.json (with an uppercase letter
	// in the path, to exercise escaping).
	os.WriteFile(filepath.Join(repo, "go.mod"),
		[]byte("module github.com/org/app\n\ngo 1.25\n\nrequire github.com/org/Framework v1.2.3\n"), 0o644)

	depDir := filepath.Join(cache, "github.com/org/!framework@v1.2.3")
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(depDir, "spec.json"),
		[]byte(`{"common/middleware/trace.go::PhaseEventMiddleware":{"fqn":"github.com/org/Framework/common/middleware/trace.PhaseEventMiddleware","cases":[],"rules":["per-request only"]}}`), 0o644)

	idx := loadDepSpecs(repo)
	e, ok := idx["common/middleware/trace.go::PhaseEventMiddleware"]
	if !ok {
		t.Fatalf("dep spec not discovered/merged; got %v", keys(idx))
	}
	if e.Fqn == "" || len(e.Rules) != 1 {
		t.Errorf("entry missing fqn/rules: %+v", e)
	}
}

func TestLoadDepSpecs_PythonVenv(t *testing.T) {
	repo := t.TempDir()
	sp := filepath.Join(repo, ".venv", "lib", "python3.11", "site-packages", "framework")
	if err := os.MkdirAll(sp, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(sp, "spec.json"),
		[]byte(`{"framework/mw/trace.py::PhaseEventMiddleware":{"fqn":"framework.mw.trace.PhaseEventMiddleware","cases":[],"rules":["per-request only"]}}`), 0o644)

	idx := loadDepSpecs(repo)
	if _, ok := idx["framework/mw/trace.py::PhaseEventMiddleware"]; !ok {
		t.Fatalf("python venv dep spec not discovered; got %v", keys(idx))
	}
}

func keys(idx Index) []string {
	out := make([]string, 0, len(idx))
	for k := range idx {
		out = append(out, k)
	}
	return out
}
