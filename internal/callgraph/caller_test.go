package callgraph

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/spec"
	"github.com/qiankunli/case-code-review/internal/unit"
)

func TestFuncName(t *testing.T) {
	for id, want := range map[string]string{
		"pkg/x.go::Service.Get": "Get",
		"pkg/x.go::Helper":      "Helper",
		"not-an-id":             "",
	} {
		if got := funcName(id); got != want {
			t.Errorf("funcName(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestCallerFinder_InheritsSpecFromCaller(t *testing.T) {
	repo := newRepo(t, map[string]string{
		"handler.go": "package p\n\nfunc Handle() { helper() }\n",
		"helper.go":  "package p\n\nfunc helper() {}\n",
	})
	idx, err := spec.Parse([]byte(`{"handler.go::Handle": {"spec": "must validate tenant before any write"}}`))
	if err != nil {
		t.Fatal(err)
	}

	// helper has no spec of its own -> walk up to its caller Handle (which does).
	u := unit.Unit{Scope: unit.ScopeFunc, Path: "helper.go", Symbols: []string{"helper.go::helper"}}
	clues := CallerFinder{RepoDir: repo, Index: idx}.Find(u)

	if len(clues) != 1 {
		t.Fatalf("want 1 inherited caller clue, got %d: %+v", len(clues), clues)
	}
	if clues[0].Kind != unit.ClueCaller || clues[0].Ref != "handler.go::Handle" ||
		!strings.Contains(clues[0].Text, "must validate tenant") {
		t.Errorf("clue off: %+v", clues[0])
	}
}

func TestCallerFinder_OwnSpecShortCircuits(t *testing.T) {
	idx, _ := spec.Parse([]byte(`{"helper.go::helper": {"spec": "own contract"}}`))
	// Own spec present -> no walk at all (no repo needed).
	u := unit.Unit{Scope: unit.ScopeFunc, Path: "helper.go", Symbols: []string{"helper.go::helper"}}
	if clues := (CallerFinder{RepoDir: t.TempDir(), Index: idx}).Find(u); clues != nil {
		t.Errorf("own spec should short-circuit, got %+v", clues)
	}
}

func TestCallerFinder_Degrades(t *testing.T) {
	u := unit.Unit{Scope: unit.ScopeFunc, Symbols: []string{"a.go::f"}}
	// no index / no repo
	if got := (CallerFinder{}).Find(u); got != nil {
		t.Errorf("no repo/index should degrade to nil, got %+v", got)
	}
	// file-scope unit has no function to walk from
	idx, _ := spec.Parse([]byte(`{"x.go::Caller": {"spec": "s"}}`))
	file := unit.Unit{Scope: unit.ScopeFile, Path: "a.go"}
	if got := (CallerFinder{RepoDir: t.TempDir(), Index: idx}).Find(file); got != nil {
		t.Errorf("file-scope unit should degrade to nil, got %+v", got)
	}
}

// newRepo creates a git repo (git grep needs one) with the given files and
// returns its path. It skips the test when git is unavailable.
func newRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	for name, content := range files {
		p := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"add", "-A"},
		{"commit", "-m", "x"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}
