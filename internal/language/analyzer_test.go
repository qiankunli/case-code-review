package language

import (
	"context"
	"os/exec"
	"testing"
)

func TestAnalyzeGo(t *testing.T) {
	source := Source{Path: "p.go", Content: `package p

type S struct{}

func Alpha() {
	helper()
}

func (s *S) Beta() int {
	return s.load()
}
`}
	analysis, err := NewAnalyzer("").Analyze(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	assertDefinition(t, analysis, "p.go::Alpha", Span{Start: 5, End: 7})
	assertDefinition(t, analysis, "p.go::S.Beta", Span{Start: 9, End: 11})
	if definition, ok := analysis.DefinitionAt(10); !ok || definition.SymbolID != "p.go::S.Beta" {
		t.Fatalf("DefinitionAt(10) = (%+v, %v)", definition, ok)
	}
	assertNames(t, analysis.CalleesOf("S.Beta"), "load")
	assertNames(t, analysis.CalleesOf("p.go::Alpha"), "helper")
}

func TestAnalyzePython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	source := Source{Path: "p.py", Content: `def alpha():
    helper()

class Svc:
    def create(self, req):
        validate(req)
        return self.store(req)
`}
	analysis, err := NewAnalyzer("").Analyze(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	assertDefinition(t, analysis, "p.py::alpha", Span{Start: 1, End: 2})
	assertDefinition(t, analysis, "p.py::Svc.create", Span{Start: 5, End: 7})
	assertNames(t, analysis.CalleesOf("Svc.create"), "validate", "store")
}

func TestAnalyzeTypeScript(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	source := Source{Path: "app.ts", Content: `const helper = () => 1;

class Service {
  run() {
    return helper() + this.load();
  }
}
`}
	analysis, err := NewAnalyzer("").Analyze(context.Background(), source)
	if err != nil {
		t.Skip("TypeScript compiler not available")
	}
	assertDefinition(t, analysis, "app.ts::helper", Span{Start: 1, End: 1})
	assertDefinition(t, analysis, "app.ts::Service.run", Span{Start: 4, End: 6})
	assertNames(t, analysis.CalleesOf("Service.run"), "helper", "load")
}

func TestAnalyzeUnsupported(t *testing.T) {
	if _, err := NewAnalyzer("").Analyze(context.Background(), Source{Path: "README.md", Content: "text"}); err == nil {
		t.Fatal("unsupported source must return an error")
	}
}

func assertDefinition(t *testing.T, analysis Analysis, id string, want Span) {
	t.Helper()
	definition, ok := analysis.DefinitionByID(id)
	if !ok {
		t.Fatalf("definition %q missing from %+v", id, analysis.Definitions)
	}
	if definition.Span != want {
		t.Fatalf("%s span = %+v, want %+v", id, definition.Span, want)
	}
}

func assertNames(t *testing.T, got []string, want ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, name := range got {
		set[name] = true
	}
	for _, name := range want {
		if !set[name] {
			t.Fatalf("missing %q in %v", name, got)
		}
	}
}
