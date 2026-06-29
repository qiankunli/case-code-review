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
	frags, err := PyFuncSplitter{}.Split(model.Diff{NewPath: "p.py", Diff: rawDiff, NewFileContent: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 2 {
		t.Fatalf("want 2 fragments, got %d: %v", len(frags), ids(frags))
	}
	a := findFrag(t, frags, "p.py::alpha")
	if UnitOf(a).Scope != ScopeFunc || len(a.Symbols) != 1 || a.Symbols[0] != "p.py::alpha" {
		t.Errorf("alpha fields off: %+v", a)
	}
	// method id is Class.method
	findFrag(t, frags, "p.py::Svc.do")
}

func TestPyFuncSplitter_SyntaxErrorFallsBack(t *testing.T) {
	requirePython3(t)
	frags, err := PyFuncSplitter{}.Split(model.Diff{
		NewPath:        "p.py",
		Diff:           "@@ -1,1 +1,1 @@\n-a\n+b\n",
		NewFileContent: "def f(:\n  bad",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 1 || UnitOf(frags[0]).Scope != ScopeFile {
		t.Fatalf("syntax error should fall back to one file fragment, got %v", ids(frags))
	}
}

func TestPyFuncSplitter_NonPyIsFileScope(t *testing.T) {
	frags, err := PyFuncSplitter{}.Split(model.Diff{
		NewPath:        "a.txt",
		Diff:           "@@ -1,1 +1,1 @@\n-a\n+b\n",
		NewFileContent: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 1 || UnitOf(frags[0]).Scope != ScopeFile {
		t.Fatalf("non-py should be one file fragment, got %v", ids(frags))
	}
}

func TestAutoSplitter_RoutesByExtension(t *testing.T) {
	// .txt -> file scope (no language splitter), regardless of python3 presence.
	frags, err := AutoSplitter{}.Split(model.Diff{
		NewPath:        "notes.txt",
		Diff:           "@@ -1,1 +1,1 @@\n-a\n+b\n",
		NewFileContent: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frags) != 1 || UnitOf(frags[0]).Scope != ScopeFile {
		t.Fatalf("txt should route to file scope, got %v", ids(frags))
	}
}

func TestPyFuncIDAt(t *testing.T) {
	requirePython3(t)
	src := "def alpha():\n    helper()\n\n\nclass Svc:\n    def do(self):\n        return validate()\n"
	if id, ok := PyFuncIDAt("p.py", src, 2); !ok || id != "p.py::alpha" {
		t.Errorf("line 2 -> (%q,%v); want p.py::alpha", id, ok)
	}
	if id, ok := PyFuncIDAt("p.py", src, 7); !ok || id != "p.py::Svc.do" { // method id is Class.method
		t.Errorf("line 7 -> (%q,%v); want p.py::Svc.do", id, ok)
	}
	if _, ok := PyFuncIDAt("p.py", "def (", 1); ok {
		t.Error("unparseable source should resolve to false")
	}
}

func TestPyCalleesOf(t *testing.T) {
	requirePython3(t)
	src := "class Svc:\n    def create(self, req):\n        validate(req)\n        return self.store(req)\n"
	got := PyCalleesOf("p.py", src, "Svc.create")
	want := map[string]bool{"validate": true, "store": true} // Name call + Attribute call
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected callee %q in %v", n, got)
		}
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("missing callees %v (got %v)", want, got)
	}
	if PyCalleesOf("p.py", src, "Nope") != nil {
		t.Error("unknown symbol should resolve to nil")
	}
}
