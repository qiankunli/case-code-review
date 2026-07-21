package language

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// v1 Go backend: a pure-syntax go/ast sweep. Defs are package-level
// declarations with a sliced one-line signature; refs are identifier
// occurrence counts. No type checking — the edges it feeds are
// ConfidenceLow by construction, which the ranking consumer tolerates.

// scanGoRepository extracts defs/refs from all non-test .go files under repoDir.
// Vendor trees, hidden dirs and testdata are skipped. Best-effort: files
// that fail to parse are ignored.
func scanGoRepository(repoDir string) *RepositoryIndex {
	ex := &RepositoryIndex{
		Definitions: map[string][]IndexedDefinition{},
		References:  map[string]map[string]int{},
	}
	var files []string
	_ = filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if name != "." && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if len(files) > maxScanFiles {
		files = files[:maxScanFiles]
	}
	for _, path := range files {
		rel, err := filepath.Rel(repoDir, path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		scanGoFile(path, rel, ex)
	}
	return ex
}

func scanGoFile(path, rel string, ex *RepositoryIndex) {
	// Stat first: an oversized (generated/vendored) file should be skipped
	// without paying its full read+allocation.
	if fi, err := os.Stat(path); err != nil || fi.Size() > maxFileBytes {
		return
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return
	}
	analysis, err := analyzeGo(Source{Path: rel, Content: string(src)})
	if err != nil {
		return
	}
	for _, definition := range analysis.Definitions {
		ex.Definitions[rel] = append(ex.Definitions[rel], IndexedDefinition{
			Name: definition.Name, SymbolID: definition.SymbolID, Path: rel,
			Line: definition.Span.Start, Signature: definition.Signature,
		})
	}
	ex.References[rel] = analysis.References
}
