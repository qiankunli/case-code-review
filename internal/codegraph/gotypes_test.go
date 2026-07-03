package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// fixture: a tiny module where main calls into pkg store both directly and
// via a method, plus an interface-dispatch call that must NOT become an edge.
func writeTypedFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, src string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module fixture.local/m\n\ngo 1.21\n")
	write("store/store.go", `package store

type Store struct{}

func (s *Store) Save(k string) error { return nil }

func Open() *Store { return &Store{} }

type Sink interface{ Save(k string) error }
`)
	write("app/app.go", `package app

import "fixture.local/m/store"

func Run() error {
	s := store.Open()          // direct cross-package call
	if err := s.Save("k"); err != nil { // concrete method call
		return err
	}
	var sink store.Sink = s
	return sink.Save("k2") // interface dispatch — must NOT create an edge
}
`)
	write("app/app_test.go", `package app

import "testing"

func TestRun(t *testing.T) { _ = Run() } // test files are excluded
`)
	return dir
}

func TestBuildGoCallGraph_ResolvedEdges(t *testing.T) {
	g, err := BuildGoCallGraph(context.Background(), writeTypedFixture(t))
	if err != nil {
		t.Fatalf("BuildGoCallGraph: %v", err)
	}

	callees := g.Callees("app/app.go::Run")
	if !slices.Contains(callees, "store/store.go::Open") {
		t.Errorf("direct cross-package edge missing; callees=%v", callees)
	}
	if !slices.Contains(callees, "store/store.go::Store.Save") {
		t.Errorf("concrete method edge missing; callees=%v", callees)
	}

	callers := g.Callers("store/store.go::Open")
	if !slices.Contains(callers, "app/app.go::Run") {
		t.Errorf("reverse edge missing; callers=%v", callers)
	}

	// Interface dispatch stays edge-less: exactly the two concrete edges above.
	if len(callees) != 2 {
		t.Errorf("interface dispatch must not add edges; callees=%v", callees)
	}
	// Test files excluded: no caller from app_test.go.
	for _, c := range g.Callers("app/app.go::Run") {
		if c != "" {
			t.Errorf("Run should have no in-repo callers (test files excluded), got %v", g.Callers("app/app.go::Run"))
		}
	}
}

func TestBuildGoCallGraph_FailsOnNonModule(t *testing.T) {
	dir := t.TempDir() // empty: no go.mod, no packages
	if _, err := BuildGoCallGraph(context.Background(), dir); err == nil {
		t.Error("expected error for a directory with no Go packages")
	}
}
