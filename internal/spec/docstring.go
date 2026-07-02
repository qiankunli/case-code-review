package spec

import (
	"os"
	"path/filepath"
	"strings"
)

// SymbolDocstring reads a symbol's docstring from its source file in the repo
// (adoption-free — no marker needed), given its symbol-id `<relpath>::<name>`:
// a Python docstring or a Go doc comment, by file extension. "" when the language
// isn't supported, the file isn't readable, or the symbol has no docstring.
// Shared by the owner/used relations and the callgraph caller/callee walk.
func SymbolDocstring(repoDir, symbolID string) string {
	if repoDir == "" {
		return ""
	}
	rel, name, ok := strings.Cut(symbolID, "::")
	if !ok {
		return ""
	}
	return extractDocFromFile(filepath.Join(repoDir, rel), name)
}

// extractDocFromFile extracts name's summary docstring from the source file at
// path, dispatching by extension. Best-effort: any failure yields "".
func extractDocFromFile(path, name string) string {
	src, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	switch {
	case strings.HasSuffix(path, ".py"):
		return extractPyDocstring(string(src), name)
	case strings.HasSuffix(path, ".go"):
		return extractGoDoc(string(src), name)
	}
	return ""
}
