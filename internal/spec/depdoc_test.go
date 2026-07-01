package spec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qiankunli/case-code-review/internal/unit"
)

func TestParsePyFromImports(t *testing.T) {
	src := `from framework.middleware.trace import PhaseEventMiddleware
from a.b import X, Y as Z
import os
`
	got := parsePyFromImports(src)
	if s := got["PhaseEventMiddleware"]; s.module != "framework.middleware.trace" || s.name != "PhaseEventMiddleware" {
		t.Errorf("PhaseEventMiddleware -> %+v", s)
	}
	if s := got["X"]; s.module != "a.b" || s.name != "X" {
		t.Errorf("X -> %+v", s)
	}
	if s := got["Z"]; s.module != "a.b" || s.name != "Y" { // alias: local Z -> real Y
		t.Errorf("Z -> %+v", s)
	}
	if _, ok := got["os"]; ok {
		t.Error("plain `import os` should not be captured by from-import parser")
	}
}

func TestExtractPyDocstring(t *testing.T) {
	src := `class PhaseEventMiddleware:
    """Per-request only — do not cache/reuse.

    Accumulates events across a run.
    """
    def __init__(self): ...

def one_liner():
    'just one line'

def undocumented():
    return 1
`
	if got := extractPyDocstring(src, "PhaseEventMiddleware"); got != "Per-request only — do not cache/reuse." {
		t.Errorf("class docstring summary = %q", got)
	}
	if got := extractPyDocstring(src, "one_liner"); got != "just one line" {
		t.Errorf("one-liner = %q", got)
	}
	if got := extractPyDocstring(src, "undocumented"); got != "" {
		t.Errorf("undocumented should be empty, got %q", got)
	}
}

func TestDepDocFinder_ReadsDependencyDocstring(t *testing.T) {
	repo := t.TempDir()
	// consumer file importing a dependency type
	write(t, filepath.Join(repo, "app", "handler.py"),
		"from framework.middleware.trace import PhaseEventMiddleware\n\ndef create():\n    return PhaseEventMiddleware()\n")
	// the dependency, installed under the repo's .venv
	write(t, filepath.Join(repo, ".venv", "lib", "python3.11", "site-packages", "framework", "middleware", "trace.py"),
		"class PhaseEventMiddleware:\n    \"\"\"Per-request only — do not cache/reuse.\"\"\"\n    pass\n")

	u := unit.UnitOf(unit.Fragment{
		Path:    "app/handler.py",
		Symbols: []string{"app/handler.py::create"},
		Diff:    "+    return PhaseEventMiddleware()\n",
	})
	clues := DepDocFinder{RepoDir: repo}.Find(u)
	if len(clues) != 1 || clues[0].Kind != unit.ClueDoc ||
		clues[0].Ref != "framework.middleware.trace.PhaseEventMiddleware" ||
		clues[0].Text != "Per-request only — do not cache/reuse." {
		t.Fatalf("want one ClueDoc from the dependency docstring, got %+v", clues)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
