package language

// IndexedDefinition is the compact definition shape needed by repository-wide
// name pairing. File analysis carries richer spans and calls; repository scans
// trade that detail for one bounded pass over the whole tree.
type IndexedDefinition struct {
	Name      string
	SymbolID  string
	Path      string
	Line      int
	Signature string
}

// RepositoryIndex is the language-owned, parser-independent output of a
// bounded repository scan.
type RepositoryIndex struct {
	Definitions map[string][]IndexedDefinition
	References  map[string]map[string]int
}

// ScanRepository runs all currently available language backends. Each backend
// is best-effort, so a missing runtime narrows the index without failing review.
func ScanRepository(repoDir string) *RepositoryIndex {
	merged := &RepositoryIndex{
		Definitions: map[string][]IndexedDefinition{},
		References:  map[string]map[string]int{},
	}
	for _, index := range []*RepositoryIndex{scanGoRepository(repoDir), scanPythonRepository(repoDir), scanTypeScriptRepository(repoDir)} {
		if index == nil {
			continue
		}
		for path, definitions := range index.Definitions {
			merged.Definitions[path] = append(merged.Definitions[path], definitions...)
		}
		for path, references := range index.References {
			if merged.References[path] == nil {
				merged.References[path] = references
				continue
			}
			for name, count := range references {
				merged.References[path][name] += count
			}
		}
	}
	return merged
}
