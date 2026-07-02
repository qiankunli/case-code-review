package spec

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type importedSym struct{ module, name string } // resolved source module + real symbol name

var pyFromImport = regexp.MustCompile(`(?m)^[ \t]*from[ \t]+([\w.]+)[ \t]+import[ \t]+(.+)$`)

// parsePyFromImports maps each name a `from a.b.c import X, Y as Z` line brings
// into scope (its local name, e.g. Z) to {module, real name} (a.b.c, Y). Plain
// `import a.b.c` is not handled yet.
func parsePyFromImports(src string) map[string]importedSym {
	out := map[string]importedSym{}
	for _, m := range pyFromImport.FindAllStringSubmatch(src, -1) {
		module := m[1]
		clause := strings.TrimSpace(m[2])
		clause = strings.TrimPrefix(clause, "(")
		clause = strings.TrimSuffix(clause, ")")
		clause = strings.TrimSuffix(strings.TrimSpace(clause), "\\") // line continuation
		for _, part := range strings.Split(clause, ",") {
			fields := strings.Fields(strings.TrimSpace(part))
			if len(fields) == 0 || fields[0] == "*" {
				continue
			}
			name := fields[0]
			local := name
			if len(fields) == 3 && fields[1] == "as" {
				local = fields[2]
			}
			out[local] = importedSym{module: module, name: name}
		}
	}
	return out
}

// pyModuleRoots are the directories a dotted Python module resolves under: the
// active venv's site-packages (dependencies) then the repo itself (intra-repo).
func pyModuleRoots(repoDir string) []string {
	if repoDir == "" {
		return nil
	}
	var roots []string
	if ve := os.Getenv("VIRTUAL_ENV"); ve != "" {
		roots = append(roots, sitePackageDirs(ve)...)
	}
	roots = append(roots, sitePackageDirs(filepath.Join(repoDir, ".venv"))...)
	return append(roots, repoDir)
}

// resolvePyModuleFile maps a dotted module (a.b.c) to a source file under one of
// roots: <root>/a/b/c.py or <root>/a/b/c/__init__.py.
func resolvePyModuleFile(module string, roots []string) (string, bool) {
	rel := filepath.FromSlash(strings.ReplaceAll(module, ".", "/"))
	for _, root := range roots {
		if p := filepath.Join(root, rel+".py"); fileExists(p) {
			return p, true
		}
		if p := filepath.Join(root, rel, "__init__.py"); fileExists(p) {
			return p, true
		}
	}
	return "", false
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
