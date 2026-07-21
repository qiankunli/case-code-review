package language

import (
	"context"
	"os/exec"
	"slices"
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
	source := Source{Path: "app.ts", Content: `const helper = () => 1;

class Service {
  run() {
    return helper() + this.load();
  }
}
`}
	analysis, err := NewAnalyzer("").Analyze(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	assertDefinition(t, analysis, "app.ts::helper", Span{Start: 1, End: 1})
	assertDefinition(t, analysis, "app.ts::Service.run", Span{Start: 4, End: 6})
	assertNames(t, analysis.CalleesOf("Service.run"), "helper", "load")
}

func TestAnalyzeUnsupported(t *testing.T) {
	if _, err := NewAnalyzer("").Analyze(context.Background(), Source{Path: "README.unknown-language", Content: "text"}); err == nil {
		t.Fatal("unsupported source must return an error")
	}
}

func TestAnalyzeJavaWithTreeSitter(t *testing.T) {
	source := Source{Path: "Service.java", Content: `class Service {
  void run() {
    validate();
  }
}
`}
	analysis, err := NewAnalyzer("").Analyze(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Language != Language("java") || analysis.Quality != QualityPartial {
		t.Fatalf("analysis metadata = (%q, %q)", analysis.Language, analysis.Quality)
	}
	assertDefinition(t, analysis, "Service.java::Service.run", Span{Start: 2, End: 4})
	assertNames(t, analysis.CalleesOf("Service.run"), "validate")
}

func TestAnalyzeRustWithTreeSitterTags(t *testing.T) {
	source := Source{Path: "lib.rs", Content: `fn run() {
    validate();
}
`}
	analysis, err := NewAnalyzer("").Analyze(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	assertDefinition(t, analysis, "lib.rs::run", Span{Start: 1, End: 3})
	assertNames(t, analysis.CalleesOf("run"), "validate")
}

func TestStructuredExtensionsIncludeTreeSitterLanguages(t *testing.T) {
	extensions := StructuredExtensions()
	for _, extension := range []string{".go", ".py", ".ts", ".java", ".rs"} {
		if !slices.Contains(extensions, extension) {
			t.Fatalf("StructuredExtensions missing %q", extension)
		}
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
