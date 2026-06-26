package unit

import (
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/model"
)

// findUnit returns the unit with the given ID, or fails.
func findUnit(t *testing.T, units []Unit, id string) Unit {
	t.Helper()
	for _, u := range units {
		if u.ID == id {
			return u
		}
	}
	t.Fatalf("no unit with id %q; got %v", id, ids(units))
	return Unit{}
}

func ids(units []Unit) []string {
	out := make([]string, len(units))
	for i, u := range units {
		out[i] = u.ID
	}
	return out
}

func TestGoFuncSplitter_ByFunction(t *testing.T) {
	src := `package foo

import "fmt"

func Alpha() {
	fmt.Println("alpha changed")
}

func Beta(x int) int {
	return x + 1
}
`
	rawDiff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -5,3 +5,3 @@
 func Alpha() {
-	fmt.Println("alpha")
+	fmt.Println("alpha changed")
 }
@@ -9,3 +9,3 @@
 func Beta(x int) int {
-	return x
+	return x + 1
 }
`
	units, err := GoFuncSplitter{}.Split(model.Diff{NewPath: "foo.go", Diff: rawDiff, NewFileContent: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Fatalf("want 2 func units, got %d: %v", len(units), ids(units))
	}

	alpha := findUnit(t, units, "foo.go::Alpha")
	if alpha.Scope != ScopeFunc || alpha.Path != "foo.go" ||
		len(alpha.Symbols) != 1 || alpha.Symbols[0] != "foo.go::Alpha" {
		t.Errorf("alpha unit fields off: %+v", alpha)
	}
	if alpha.Insertions != 1 || alpha.Deletions != 1 {
		t.Errorf("alpha counts: +%d/-%d, want +1/-1", alpha.Insertions, alpha.Deletions)
	}
	if !strings.Contains(alpha.Diff, "alpha changed") || strings.Contains(alpha.Diff, "x + 1") {
		t.Errorf("alpha diff should slice only Alpha's hunk:\n%s", alpha.Diff)
	}

	beta := findUnit(t, units, "foo.go::Beta")
	if beta.Insertions != 1 || beta.Deletions != 1 {
		t.Errorf("beta counts: +%d/-%d", beta.Insertions, beta.Deletions)
	}
}

func TestGoFuncSplitter_MethodAndResidual(t *testing.T) {
	src := `package foo

import (
	"fmt"
	"errors"
)

func (s *Service) Do() error {
	return errors.New("x")
}
`
	_ = src
	rawDiff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -3,3 +3,4 @@
 import (
 	"fmt"
+	"errors"
 )
@@ -8,3 +8,3 @@
 func (s *Service) Do() error {
-	return nil
+	return errors.New("x")
 }
`
	units, err := GoFuncSplitter{}.Split(model.Diff{NewPath: "foo.go", Diff: rawDiff, NewFileContent: src})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Fatalf("want 1 method + 1 residual, got %d: %v", len(units), ids(units))
	}

	// pointer receiver stripped to "Service"
	m := findUnit(t, units, "foo.go::Service.Do")
	if m.Scope != ScopeFunc {
		t.Errorf("method unit scope: %v", m.Scope)
	}

	// residual is the file-scope unit carrying the import hunk
	var residual *Unit
	for i := range units {
		if units[i].Scope == ScopeFile {
			residual = &units[i]
		}
	}
	if residual == nil {
		t.Fatal("expected a residual file unit for the import change")
	}
	if !strings.Contains(residual.Diff, "errors") || strings.Contains(residual.Diff, "return") {
		t.Errorf("residual should hold only the import hunk:\n%s", residual.Diff)
	}
}

func TestGoFuncSplitter_FallsBackOnParseError(t *testing.T) {
	units, err := GoFuncSplitter{}.Split(model.Diff{
		NewPath:        "bad.go",
		Diff:           "@@ -1,1 +1,1 @@\n-x\n+y\n",
		NewFileContent: "package foo\nthis is not valid go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 || units[0].Scope != ScopeFile {
		t.Fatalf("parse error should fall back to one file unit, got %v", ids(units))
	}
}

func TestGoFuncSplitter_NonGoIsFileScope(t *testing.T) {
	units, err := GoFuncSplitter{}.Split(model.Diff{
		NewPath:        "app/api.py",
		Diff:           "@@ -1,1 +1,1 @@\n-x\n+y\n",
		NewFileContent: "def f(): pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 || units[0].Scope != ScopeFile {
		t.Fatalf("non-go should be one file unit, got %v", ids(units))
	}
}
