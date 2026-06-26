package unit

import (
	"os/exec"
	"testing"

	"github.com/qiankunli/case-code-review/internal/model"
)

func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
}

func TestPyFuncSplitter_ByFunctionAndMethod(t *testing.T) {
	requirePython3(t)
	src := `def alpha():
    return 1


class Svc:
    def do(self):
        return 2
`
	rawDiff := `diff --git a/p.py b/p.py
--- a/p.py
+++ b/p.py
@@ -2,1 +2,1 @@
-    return 0
+    return 1
@@ -7,1 +7,1 @@
-        return 0
+        return 2
`
	units, err := PyFuncSplitter{}.Split(model.Diff{NewPath: "p.py", Diff: rawDiff, NewFileContent: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Fatalf("want 2 units, got %d: %v", len(units), ids(units))
	}
	a := findUnit(t, units, "p.py::alpha")
	if a.Scope != ScopeFunc || len(a.Symbols) != 1 || a.Symbols[0] != "p.py::alpha" {
		t.Errorf("alpha fields off: %+v", a)
	}
	// method id is Class.method
	findUnit(t, units, "p.py::Svc.do")
}

func TestPyFuncSplitter_SyntaxErrorFallsBack(t *testing.T) {
	requirePython3(t)
	units, err := PyFuncSplitter{}.Split(model.Diff{
		NewPath:        "p.py",
		Diff:           "@@ -1,1 +1,1 @@\n-a\n+b\n",
		NewFileContent: "def f(:\n  bad",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 || units[0].Scope != ScopeFile {
		t.Fatalf("syntax error should fall back to one file unit, got %v", ids(units))
	}
}

func TestPyFuncSplitter_NonPyIsFileScope(t *testing.T) {
	units, err := PyFuncSplitter{}.Split(model.Diff{
		NewPath:        "a.txt",
		Diff:           "@@ -1,1 +1,1 @@\n-a\n+b\n",
		NewFileContent: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 || units[0].Scope != ScopeFile {
		t.Fatalf("non-py should be one file unit, got %v", ids(units))
	}
}

func TestAutoSplitter_RoutesByExtension(t *testing.T) {
	// .txt -> file scope (no language splitter), regardless of python3 presence.
	units, err := AutoSplitter{}.Split(model.Diff{
		NewPath:        "notes.txt",
		Diff:           "@@ -1,1 +1,1 @@\n-a\n+b\n",
		NewFileContent: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 || units[0].Scope != ScopeFile {
		t.Fatalf("txt should route to file scope, got %v", ids(units))
	}
}
