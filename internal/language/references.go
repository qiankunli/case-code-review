package language

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Reference is one source-level name used by a changed snippet. FQN is set
// when imports resolve the name precisely; SourcePath/SourceName point at a
// resolvable Python module for adoption-free doc extraction.
type Reference struct {
	Name       string
	FQN        string
	SourcePath string
	SourceName string
}

var (
	identifier   = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)
	goSelector   = regexp.MustCompile(`\b([a-z][A-Za-z0-9_]*)\.([A-Z][A-Za-z0-9_]*)\b`)
	goImportLine = regexp.MustCompile(`^(?:([A-Za-z_]\w*|\.|_)\s+)?"([^"]+)"`)
	pyFromImport = regexp.MustCompile(`(?m)^[ \t]*from[ \t]+([\w.]+)[ \t]+import[ \t]+(.+)$`)
)

// ReferencesIn extracts names from a changed snippet and enriches references
// that the source file's imports resolve. It intentionally returns bare names
// alongside precise FQNs: consumers decide whether an FQN hit is authoritative
// enough to suppress same-name fallback.
func (a *Analyzer) ReferencesIn(source Source, snippet string) []Reference {
	lang, _ := Detect(source.Path)
	var out []Reference
	seen := map[Reference]bool{}
	add := func(reference Reference) {
		if reference.Name == "" || seen[reference] {
			return
		}
		seen[reference] = true
		out = append(out, reference)
	}

	switch lang {
	case Go:
		imports := parseGoImports(source.Content)
		for _, match := range goSelector.FindAllStringSubmatch(snippet, -1) {
			if path, ok := imports[match[1]]; ok {
				add(Reference{Name: match[2], FQN: path + "." + match[2]})
			}
		}
	case Python:
		imports := parsePyFromImports(source.Content)
		roots := pythonModuleRoots(a.repoDir)
		for _, name := range identifier.FindAllString(snippet, -1) {
			if imported, ok := imports[name]; ok {
				reference := Reference{Name: name, FQN: imported.module + "." + imported.name, SourceName: imported.name}
				if path, ok := resolvePythonModuleFile(imported.module, roots); ok {
					reference.SourcePath = path
				}
				add(reference)
			}
		}
	}

	for _, name := range identifier.FindAllString(snippet, -1) {
		add(Reference{Name: name})
	}
	return out
}

func parseGoImports(src string) map[string]string {
	out := map[string]string{}
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "import ("):
			inBlock = true
			continue
		case inBlock && trimmed == ")":
			inBlock = false
			continue
		case !inBlock && strings.HasPrefix(trimmed, "import "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "import "))
		case !inBlock:
			continue
		}
		match := goImportLine.FindStringSubmatch(trimmed)
		if match == nil {
			continue
		}
		alias, path := match[1], match[2]
		if alias == "_" || alias == "." {
			continue
		}
		local := alias
		if local == "" {
			local = goPackageName(path)
		}
		if local != "" {
			out[local] = path
		}
	}
	return out
}

func goPackageName(path string) string {
	segment := path[strings.LastIndex(path, "/")+1:]
	if len(segment) > 1 && segment[0] == 'v' && isDigits(segment[1:]) {
		if i := strings.LastIndex(path, "/"); i >= 0 {
			rest := path[:i]
			return rest[strings.LastIndex(rest, "/")+1:]
		}
	}
	return segment
}

func isDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

type importedSymbol struct{ module, name string }

func parsePyFromImports(src string) map[string]importedSymbol {
	out := map[string]importedSymbol{}
	for _, match := range pyFromImport.FindAllStringSubmatch(src, -1) {
		module := match[1]
		clause := strings.TrimSpace(match[2])
		clause = strings.TrimPrefix(clause, "(")
		clause = strings.TrimSuffix(clause, ")")
		clause = strings.TrimSuffix(strings.TrimSpace(clause), "\\")
		for _, part := range strings.Split(clause, ",") {
			fields := strings.Fields(strings.TrimSpace(part))
			if len(fields) == 0 || fields[0] == "*" {
				continue
			}
			name, local := fields[0], fields[0]
			if len(fields) == 3 && fields[1] == "as" {
				local = fields[2]
			}
			out[local] = importedSymbol{module: module, name: name}
		}
	}
	return out
}

func pythonModuleRoots(repoDir string) []string {
	if repoDir == "" {
		return nil
	}
	var roots []string
	if venv := os.Getenv("VIRTUAL_ENV"); venv != "" {
		roots = append(roots, PythonSitePackageDirs(venv)...)
	}
	roots = append(roots, PythonSitePackageDirs(filepath.Join(repoDir, ".venv"))...)
	return append(roots, repoDir)
}

func resolvePythonModuleFile(module string, roots []string) (string, bool) {
	relative := filepath.FromSlash(strings.ReplaceAll(module, ".", "/"))
	for _, root := range roots {
		for _, candidate := range []string{filepath.Join(root, relative+".py"), filepath.Join(root, relative, "__init__.py")} {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, true
			}
		}
	}
	return "", false
}

// PythonSitePackageDirs returns the conventional dependency roots inside a
// virtual environment on POSIX and Windows.
func PythonSitePackageDirs(venv string) []string {
	var out []string
	if matches, err := filepath.Glob(filepath.Join(venv, "lib", "python*", "site-packages")); err == nil {
		out = append(out, matches...)
	}
	if windows := filepath.Join(venv, "Lib", "site-packages"); directoryExists(windows) {
		out = append(out, windows)
	}
	return out
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
