// Package language owns source-language facts and the parsers that produce
// them. Review concepts such as units, ranking, and clue traversal consume
// these facts but remain in their own packages.
package language

import (
	"path/filepath"
	"strings"
)

// Language identifies the grammar used to analyze a source file.
type Language string

const (
	Go         Language = "go"
	Python     Language = "python"
	TypeScript Language = "typescript"
	TSX        Language = "tsx"
	JavaScript Language = "javascript"
	JSX        Language = "jsx"
)

var structuredLanguages = []struct {
	extension string
	language  Language
}{
	{".go", Go},
	{".py", Python},
	{".ts", TypeScript},
	{".tsx", TSX},
	{".js", JavaScript},
	{".jsx", JSX},
	{".mjs", JavaScript},
	{".cjs", JavaScript},
}

// Detect identifies a language with a structured-analysis backend from a
// source path. Reviewable languages without a backend still degrade to file
// scope at the unit boundary.
func Detect(path string) (Language, bool) {
	extension := strings.ToLower(filepath.Ext(path))
	for _, candidate := range structuredLanguages {
		if extension == candidate.extension {
			return candidate.language, true
		}
	}
	return "", false
}

// StructuredExtensions returns the source suffixes currently backed by
// Analyzer. Consumers may use them to bound source discovery without learning
// which parser implements each language.
func StructuredExtensions() []string {
	extensions := make([]string, 0, len(structuredLanguages))
	for _, candidate := range structuredLanguages {
		extensions = append(extensions, candidate.extension)
	}
	return extensions
}

// Source is one repository-relative source file analyzed at its current
// contents. RepoDir is kept on Analyzer because project-local tooling is a
// property of the analysis session, not of an individual file.
type Source struct {
	Path    string
	Content string
}

// Kind is the source-level role of a definition.
type Kind string

const (
	KindFunction  Kind = "function"
	KindMethod    Kind = "method"
	KindClass     Kind = "class"
	KindType      Kind = "type"
	KindInterface Kind = "interface"
)

// Span is a 1-indexed inclusive source-line range.
type Span struct {
	Start int
	End   int
}

// Definition is a named source declaration. SymbolID is the stable cross-
// feature join key; Name is its language-level symbol (for example Svc.Run).
type Definition struct {
	SymbolID  string
	Name      string
	Owner     string
	Kind      Kind
	Span      Span
	Signature string
}

// Callable reports whether a definition can own calls and a function review
// unit. Types/classes remain useful to repository maps but do not split diffs.
func (d Definition) Callable() bool {
	return d.Kind == KindFunction || d.Kind == KindMethod
}

// Call is a syntactic call made inside a definition. Name is deliberately
// unresolved; semantic backends may additionally provide resolved call edges.
type Call struct {
	CallerID string
	Name     string
}

// Quality describes how trustworthy the returned language facts are.
type Quality string

const (
	QualitySyntax   Quality = "syntax"
	QualitySemantic Quality = "semantic"
	QualityPartial  Quality = "partial"
)

// Analysis is the parser-independent fact model consumed by ccr. Parser trees,
// query captures, and backend-specific nodes must never cross this boundary.
type Analysis struct {
	Language    Language
	Quality     Quality
	Definitions []Definition
	Calls       []Call
	References  map[string]int
}

// DefinitionAt returns the innermost callable definition containing line.
func (a Analysis) DefinitionAt(line int) (Definition, bool) {
	var best Definition
	found := false
	for _, d := range a.Definitions {
		if !d.Callable() || line < d.Span.Start || line > d.Span.End {
			continue
		}
		if !found || d.Span.End-d.Span.Start < best.Span.End-best.Span.Start {
			best, found = d, true
		}
	}
	return best, found
}

// DefinitionByID returns the definition with the canonical symbol id.
func (a Analysis) DefinitionByID(id string) (Definition, bool) {
	for _, d := range a.Definitions {
		if d.SymbolID == id {
			return d, true
		}
	}
	return Definition{}, false
}

// CalleesOf returns the distinct unresolved call names made by symbol.
func (a Analysis) CalleesOf(symbol string) []string {
	_, target, ok := SplitSymbolID(symbol)
	if !ok {
		target = symbol
	}
	seen := map[string]bool{}
	var names []string
	for _, call := range a.Calls {
		_, caller, callerOK := SplitSymbolID(call.CallerID)
		if !callerOK {
			caller = call.CallerID
		}
		if caller != target || call.Name == "" || seen[call.Name] {
			continue
		}
		seen[call.Name] = true
		names = append(names, call.Name)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}
