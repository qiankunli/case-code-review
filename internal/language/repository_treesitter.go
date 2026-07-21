package language

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const treeSitterScanTimeout = 30 * time.Second

var treeSitterSkipDirs = map[string]bool{
	"vendor": true, "node_modules": true, "testdata": true,
	"__pycache__": true, "venv": true, "site-packages": true,
}

// scanTreeSitterRepository indexes languages handled by the in-process grammar
// backend. Go and Python keep their existing repository scanners until their
// parity migrations are complete, so each source file has exactly one owner.
func scanTreeSitterRepository(repoDir string) *RepositoryIndex {
	index := &RepositoryIndex{Definitions: map[string][]IndexedDefinition{}, References: map[string]map[string]int{}}
	ctx, cancel := context.WithTimeout(context.Background(), treeSitterScanTimeout)
	defer cancel()

	count := 0
	_ = filepath.WalkDir(repoDir, func(path string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil || count >= maxScanFiles {
			return filepath.SkipAll
		}
		if err != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if name != "." && (strings.HasPrefix(name, ".") || treeSitterSkipDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repoDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		language, ok := Detect(rel)
		if !ok || language == Go || language == Python || isRepositoryTestFile(name) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > maxFileBytes {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		count++
		analysis, err := analyzeTreeSitter(ctx, language, Source{Path: rel, Content: string(content)})
		if err != nil {
			return nil
		}
		for _, definition := range analysis.Definitions {
			index.Definitions[rel] = append(index.Definitions[rel], IndexedDefinition{
				Name: definition.Name, SymbolID: definition.SymbolID, Path: rel,
				Line: definition.Span.Start, Signature: definition.Signature,
			})
		}
		if len(analysis.References) > 0 {
			index.References[rel] = analysis.References
		}
		return nil
	})
	return index
}

func isRepositoryTestFile(name string) bool {
	lower := strings.ToLower(name)
	extension := filepath.Ext(lower)
	base := strings.TrimSuffix(lower, extension)
	return strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec.") ||
		strings.HasPrefix(base, "test_") || strings.HasPrefix(base, "test-") ||
		strings.HasSuffix(base, "_test") || strings.HasSuffix(base, "_spec")
}
