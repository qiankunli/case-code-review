package unit

import (
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/model"
)

func requireTypeScriptCompiler(t *testing.T) {
	t.Helper()
	if _, err := parseTSFuncs("fixture.ts", "const value = 1;\n"); err != nil {
		t.Skip("node and the TypeScript compiler are not available")
	}
}

func TestTSFuncSplitter_FunctionMethodAndArrow(t *testing.T) {
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
	frags, err := TSFuncSplitter{}.Split(model.Diff{NewPath: "app.tsx", Diff: rawDiff, NewFileContent: src})
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

func TestTSFuncSplitter_FallsBackWithoutCompiler(t *testing.T) {
	t.Setenv("PATH", "")
	frags, err := TSFuncSplitter{}.Split(model.Diff{
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

func TestTSResolvers(t *testing.T) {
	requireTypeScriptCompiler(t)
	src := `const helper = () => 1;

class Service {
  run() {
    return helper() + this.load();
  }
}
`
	if id, ok := TSFuncIDAt("app.ts", src, 5); !ok || id != "app.ts::Service.run" {
		t.Fatalf("line 5 = (%q,%v), want Service.run", id, ok)
	}
	if start, end, ok := SymbolSpan("app.ts", src, "app.ts::Service.run"); !ok || start != 4 || end != 6 {
		t.Fatalf("Service.run span = (%d,%d,%v), want (4,6,true)", start, end, ok)
	}
	got := TSCalleesOf("app.ts", src, "Service.run")
	want := map[string]bool{"helper": true, "load": true}
	for _, name := range got {
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("missing callees %v (got %v)", want, got)
	}
}
