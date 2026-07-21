package unit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/language"
	"github.com/qiankunli/case-code-review/internal/model"
)

func requireTypeScriptCompiler(t *testing.T) {
	t.Helper()
	if _, err := language.NewAnalyzer("").Analyze(context.Background(), language.Source{
		Path: "fixture.ts", Content: "const value = 1;\n",
	}); err != nil {
		t.Skip("node and the TypeScript compiler are not available")
	}
}

func TestAutoSplitter_TypeScriptFunctionMethodAndArrow(t *testing.T) {
	requireTypeScriptCompiler(t)
	src := `export function alpha() {
  return helper();
}

class Service {
  run() {
    return validate();
  }
}

const View = () => {
  return <span>ok</span>;
};
`
	rawDiff := `diff --git a/app.tsx b/app.tsx
--- a/app.tsx
+++ b/app.tsx
@@ -2,1 +2,1 @@
-  return oldHelper();
+  return helper();
@@ -7,1 +7,1 @@
-    return oldValidate();
+    return validate();
@@ -12,1 +12,1 @@
-  return <span>old</span>;
+  return <span>ok</span>;
`
	frags, err := AutoSplitter{}.Split(model.Diff{NewPath: "app.tsx", Diff: rawDiff, NewFileContent: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 3 {
		t.Fatalf("want 3 function fragments, got %d: %v", len(frags), ids(frags))
	}
	findFrag(t, frags, "app.tsx::alpha")
	findFrag(t, frags, "app.tsx::Service.run")
	view := findFrag(t, frags, "app.tsx::View")
	if !strings.Contains(view.Diff, "<span>ok</span>") {
		t.Fatalf("View diff not isolated:\n%s", view.Diff)
	}
}

func TestAutoSplitter_TypeScriptFallsBackWithoutCompiler(t *testing.T) {
	t.Setenv("PATH", "")
	frags, err := AutoSplitter{}.Split(model.Diff{
		NewPath:        "app.ts",
		Diff:           "@@ -1,1 +1,1 @@\n-old\n+new\n",
		NewFileContent: "function app() { return 1; }",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 1 || UnitOf(frags[0]).Scope != ScopeFile {
		t.Fatalf("missing compiler should fall back to file scope, got %v", ids(frags))
	}
}

func TestAutoSplitter_TypeScriptFallsBackWithoutCompilerAPI(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	repo := t.TempDir()
	moduleDir := filepath.Join(repo, "node_modules", "typescript")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// TypeScript 7.0 deliberately ships without the legacy stable Compiler API.
	// An installed package is therefore not sufficient to promise AST access.
	if err := os.WriteFile(filepath.Join(moduleDir, "index.js"),
		[]byte(`module.exports = { version: "7.0.0" };`), 0o644); err != nil {
		t.Fatal(err)
	}
	frags, err := (AutoSplitter{RepoDir: repo}).Split(model.Diff{
		NewPath:        "app.ts",
		Diff:           "@@ -1,1 +1,1 @@\n-old\n+new\n",
		NewFileContent: "export function app() { return 1; }",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 1 || UnitOf(frags[0]).Scope != ScopeFile {
		t.Fatalf("unsupported compiler API should fall back to file scope, got %v", ids(frags))
	}
}
