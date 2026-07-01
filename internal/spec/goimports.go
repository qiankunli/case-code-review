package spec

import (
	"regexp"
	"strings"
)

// goSelector matches a Go qualified reference `pkg.Symbol` — a lowercase-initial
// package name selecting an exported (uppercase-initial) symbol. This is how a Go
// diff references a type/func from another package (`trace.PhaseEventMiddleware`),
// unlike Python's bare `from x import Name`.
var goSelector = regexp.MustCompile(`\b([a-z][A-Za-z0-9_]*)\.([A-Z][A-Za-z0-9_]*)\b`)

// goImportLine matches one Go import spec: an optional local name/alias (an
// identifier, or `.`/`_`) then the quoted import path.
var goImportLine = regexp.MustCompile(`^(?:([A-Za-z_]\w*|\.|_)\s+)?"([^"]+)"`)

// parseGoImports maps each imported package's local selector name to its import
// path, for both the single (`import "x"`) and block (`import ( ... )`) forms.
// Blank (`_`) and dot (`.`) imports are skipped — they expose no `pkg.Symbol`
// selector. The local name is the alias when given, else the package name derived
// from the path.
func parseGoImports(src string) map[string]string {
	out := map[string]string{}
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "import ("):
			inBlock = true
			continue
		case inBlock && t == ")":
			inBlock = false
			continue
		case !inBlock && strings.HasPrefix(t, "import "):
			t = strings.TrimSpace(strings.TrimPrefix(t, "import "))
		case !inBlock:
			continue
		}
		m := goImportLine.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		alias, path := m[1], m[2]
		if alias == "_" || alias == "." {
			continue
		}
		local := alias
		if local == "" {
			local = goPkgName(path)
		}
		if local != "" {
			out[local] = path
		}
	}
	return out
}

// goPkgName derives the default package selector name from an import path — its
// last segment, skipping a trailing major-version element (`.../v2` → prior
// segment), matching Go's convention. A heuristic (the real package name can
// differ), overridden by an explicit alias.
func goPkgName(path string) string {
	seg := path[strings.LastIndex(path, "/")+1:]
	if len(seg) > 1 && seg[0] == 'v' && isDigits(seg[1:]) {
		if i := strings.LastIndex(path, "/"); i >= 0 {
			rest := path[:i]
			return rest[strings.LastIndex(rest, "/")+1:]
		}
	}
	return seg
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
