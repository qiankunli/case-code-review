package spec

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/qiankunli/case-code-review/internal/unit"
)

// DepDocFinder surfaces the docstring of a symbol the Unit's diff references,
// read on demand from its source — including a dependency's source in the venv.
// Adoption-free: it needs no spec-case markers, so it covers any documented
// dependency (e.g. a framework SDK's `PhaseEventMiddleware` whose class docstring
// says "per-request only"). Python-only for now, resolving `from mod import Name`;
// plain `import mod` + attribute access, and Go, follow later.
type DepDocFinder struct {
	RepoDir string
}

type importedSym struct{ module, name string } // resolved source module + real symbol name

func (f DepDocFinder) Find(u unit.Unit) []unit.Clue {
	if f.RepoDir == "" {
		return nil
	}
	// 1. Referencing files' imports (imports sit outside the diff hunk, so read the
	//    unit's Python member files in full): local name -> {module, real name}.
	imports := map[string]importedSym{}
	for _, p := range u.Paths() {
		if !strings.HasSuffix(p, ".py") {
			continue
		}
		if src, err := os.ReadFile(filepath.Join(f.RepoDir, p)); err == nil {
			maps.Copy(imports, parsePyFromImports(string(src)))
		}
	}
	if len(imports) == 0 {
		return nil
	}

	// 2. Roots to resolve a dotted module to a source file: venv site-packages
	//    (dependencies) + the repo itself (intra-repo imports).
	var roots []string
	if ve := os.Getenv("VIRTUAL_ENV"); ve != "" {
		roots = append(roots, sitePackageDirs(ve)...)
	}
	roots = append(roots, sitePackageDirs(filepath.Join(f.RepoDir, ".venv"))...)
	roots = append(roots, f.RepoDir)

	// 3. For each referenced identifier that was imported, read its module file and
	//    grab the symbol's docstring.
	var clues []unit.Clue
	seen := map[string]bool{}
	for _, name := range identifier.FindAllString(u.Diff(), -1) {
		sym, ok := imports[name]
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		file, ok := resolvePyModuleFile(sym.module, roots)
		if !ok {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if doc := extractPyDocstring(string(src), sym.name); doc != "" {
			clues = append(clues, unit.Clue{Kind: unit.ClueDoc, Text: doc, Ref: sym.module + "." + sym.name})
		}
	}
	return clues
}

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

// extractPyDocstring returns the summary docstring (first paragraph) of `class
// name` / `def name` in src — the string literal immediately after the def
// header. Best-effort line scan (no full Python parse); "" when absent.
func extractPyDocstring(src, name string) string {
	defRe := regexp.MustCompile(`^[ \t]*(?:async[ \t]+)?(?:class|def)[ \t]+` + regexp.QuoteMeta(name) + `\b`)
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if !defRe.MatchString(line) {
			continue
		}
		// Skip to the end of the (possibly multi-line) signature, then to the first
		// non-blank body line.
		j := i
		for j < len(lines) && !strings.HasSuffix(strings.TrimRight(lines[j], " \t"), ":") {
			j++
		}
		for j++; j < len(lines) && strings.TrimSpace(lines[j]) == ""; j++ {
		}
		if j < len(lines) {
			return docstringAt(lines, j)
		}
	}
	return ""
}

// docstringAt reads a triple/single-quoted string starting at lines[start] and
// returns its first paragraph, whitespace-collapsed. "" when the body line isn't
// a string literal.
func docstringAt(lines []string, start int) string {
	body := strings.TrimSpace(lines[start])
	quote := ""
	for _, q := range []string{`"""`, "'''", `"`, `'`} {
		if strings.HasPrefix(body, q) {
			quote = q
			break
		}
	}
	if quote == "" {
		return ""
	}
	rest := strings.TrimPrefix(body, quote)
	if before, _, ok := strings.Cut(rest, quote); ok { // single-line docstring
		return summarizeDoc(before)
	}
	collected := []string{rest}
	for k := start + 1; k < len(lines); k++ {
		if idx := strings.Index(lines[k], quote); idx >= 0 {
			collected = append(collected, lines[k][:idx])
			break
		}
		collected = append(collected, lines[k])
	}
	return summarizeDoc(strings.Join(collected, "\n"))
}

// summarizeDoc trims to the first paragraph and collapses whitespace.
func summarizeDoc(doc string) string {
	doc = strings.TrimSpace(doc)
	if i := strings.Index(doc, "\n\n"); i >= 0 {
		doc = doc[:i]
	}
	return strings.Join(strings.Fields(doc), " ")
}
