package spec

import (
	"os"
	"path/filepath"
	"strings"
)

// loadDepSpecs discovers spec.json shipped inside the review repo's installed
// dependencies (Model A: spec travels with the package) and returns their merged
// index. Best-effort: any failure (no venv / module cache / unreadable file)
// yields an empty index — a dependency's spec must never fail a review.
//
// Dependency entries keep their own (dependency-relative) symbol-id keys and carry
// fqn. The used relation matches them by import-resolved fqn (or bare name), so a
// diff that uses a dependency's type picks up that type's rule.
func loadDepSpecs(repoDir string) Index {
	out := Index{}
	for _, p := range append(goDepSpecPaths(repoDir), pyDepSpecPaths(repoDir)...) {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if idx, err := Parse(data); err == nil {
			mergeInto(out, idx)
		}
	}
	return out
}

// --- Go: $GOMODCACHE/<escaped module>@<version>/spec.json ---

// goDepSpecPaths returns the candidate spec.json paths for the repo's go.mod
// requires, resolved against the module cache. Empty when there's no go.mod.
func goDepSpecPaths(repoDir string) []string {
	data, err := os.ReadFile(filepath.Join(repoDir, "go.mod"))
	if err != nil {
		return nil
	}
	cache := goModCache()
	if cache == "" {
		return nil
	}
	var paths []string
	for path, version := range parseGoRequires(string(data)) {
		paths = append(paths, filepath.Join(cache, escapeModulePath(path)+"@"+version, "spec.json"))
	}
	return paths
}

// goModCache resolves $GOMODCACHE (or $GOPATH/pkg/mod, or ~/go/pkg/mod) without
// shelling out to `go`.
func goModCache() string {
	if c := os.Getenv("GOMODCACHE"); c != "" {
		return c
	}
	gp := os.Getenv("GOPATH")
	if gp == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		gp = filepath.Join(home, "go")
	}
	if i := strings.IndexByte(gp, os.PathListSeparator); i >= 0 {
		gp = gp[:i] // GOPATH may be a list; the module cache lives under the first
	}
	return filepath.Join(gp, "pkg", "mod")
}

// parseGoRequires extracts module path -> version from a go.mod's require lines
// (both the single-line and block forms). replace/exclude are ignored (v1).
func parseGoRequires(gomod string) map[string]string {
	out := map[string]string{}
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i]) // drop `// indirect` etc.
		}
		switch {
		case line == "require (":
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
			if path, ver, ok := splitPathVersion(line); ok {
				out[path] = ver
			}
		case strings.HasPrefix(line, "require "):
			if path, ver, ok := splitPathVersion(strings.TrimPrefix(line, "require ")); ok {
				out[path] = ver
			}
		}
	}
	return out
}

func splitPathVersion(s string) (path, version string, ok bool) {
	f := strings.Fields(s)
	if len(f) < 2 || !strings.HasPrefix(f[1], "v") {
		return "", "", false
	}
	return f[0], f[1], true
}

// escapeModulePath applies Go's module-cache escaping: an uppercase letter C
// becomes "!c" (so github.com/BurntSushi -> github.com/!burnt!sushi), keeping the
// path a case-insensitive-filesystem-safe directory name.
func escapeModulePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// --- Python: <venv>/lib/python*/site-packages/<pkg>/spec.json ---

// pyDepSpecPaths returns candidate spec.json paths under the active venv's
// site-packages (checked at each installed package's root).
func pyDepSpecPaths(repoDir string) []string {
	var venvs []string
	if ve := os.Getenv("VIRTUAL_ENV"); ve != "" {
		venvs = append(venvs, ve)
	}
	venvs = append(venvs, filepath.Join(repoDir, ".venv"))

	var paths []string
	seen := map[string]bool{}
	for _, venv := range venvs {
		for _, sp := range sitePackageDirs(venv) {
			entries, err := os.ReadDir(sp)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				p := filepath.Join(sp, e.Name(), "spec.json")
				if !seen[p] {
					seen[p] = true
					paths = append(paths, p)
				}
			}
		}
	}
	return paths
}

// sitePackageDirs returns the site-packages dir(s) inside a venv (POSIX
// lib/python*/site-packages and Windows Lib/site-packages).
func sitePackageDirs(venv string) []string {
	var out []string
	if matches, err := filepath.Glob(filepath.Join(venv, "lib", "python*", "site-packages")); err == nil {
		out = append(out, matches...)
	}
	if win := filepath.Join(venv, "Lib", "site-packages"); dirExists(win) {
		out = append(out, win)
	}
	return out
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
