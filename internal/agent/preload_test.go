package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/tool"
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

func TestBuildUnitSource(t *testing.T) {
	a := newPreloadAgent(t, map[string]string{
		"pkg/a.go": "package a\n\nfunc F() {}\n",
	})
	got := a.buildUnitSource(context.Background(), []string{"pkg/a.go"})
	// Mirrors file_read's numbered-line format so inline source and tool output
	// look identical to the model.
	if !strings.Contains(got, "File: pkg/a.go (Total lines: 3)") {
		t.Fatalf("missing file header, got:\n%s", got)
	}
	if !strings.Contains(got, "1|package a") || !strings.Contains(got, "3|func F() {}") {
		t.Fatalf("missing numbered lines, got:\n%s", got)
	}

	// A missing path is skipped; when nothing could be inlined the sentinel fills
	// the placeholder instead of an empty block.
	if got := a.buildUnitSource(context.Background(), []string{"gone.go"}); got != sourceNotPreloaded {
		t.Fatalf("want sentinel for unreadable paths, got:\n%s", got)
	}

	// A file over the budget is named but not inlined; the budget failure of one
	// file doesn't block a later small file.
	big := strings.Repeat("x", preloadSourceBudget+1)
	a2 := newPreloadAgent(t, map[string]string{"big.go": big, "small.go": "ok\n"})
	got2 := a2.buildUnitSource(context.Background(), []string{"big.go", "small.go"})
	if strings.Contains(got2, big[:64]) {
		t.Fatal("oversized file must not be inlined")
	}
	if !strings.Contains(got2, "exceeds the preload budget") || !strings.Contains(got2, "1|ok") {
		t.Fatalf("want budget note + small file inlined, got:\n%s", got2[:200])
	}

	// No file_read tool registered → sentinel (preload is best-effort only).
	a3 := &Agent{args: Args{Tools: tool.NewRegistry()}}
	if got := a3.buildUnitSource(context.Background(), nil); got != sourceNotPreloaded {
		t.Fatalf("want sentinel without FileReader, got: %s", got)
	}
}
