package language

import (
	"os"
	"path/filepath"
	"strings"
)

// DependencyRoots returns installed package roots declared by the repository.
// Consumers decide which assets to read there; language owns how each
// ecosystem maps dependency declarations to installed packages.
func DependencyRoots(repoDir string) []string {
	if repoDir == "" {
		return nil
	}
	return append(goDependencyRoots(repoDir), pythonDependencyRoots(repoDir)...)
}

func goDependencyRoots(repoDir string) []string {
	data, err := os.ReadFile(filepath.Join(repoDir, "go.mod"))
	if err != nil {
		return nil
	}
	cache := goModCache()
	if cache == "" {
		return nil
	}
	var roots []string
	for path, version := range parseGoRequires(string(data)) {
		roots = append(roots, filepath.Join(cache, escapeModulePath(path)+"@"+version))
	}
	return roots
}

func goModCache() string {
	if cache := os.Getenv("GOMODCACHE"); cache != "" {
		return cache
	}
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		gopath = filepath.Join(home, "go")
	}
	if i := strings.IndexByte(gopath, os.PathListSeparator); i >= 0 {
		gopath = gopath[:i]
	}
	return filepath.Join(gopath, "pkg", "mod")
}

func parseGoRequires(gomod string) map[string]string {
	out := map[string]string{}
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "require (":
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
			if path, version, ok := splitPathVersion(line); ok {
				out[path] = version
			}
		case strings.HasPrefix(line, "require "):
			if path, version, ok := splitPathVersion(strings.TrimPrefix(line, "require ")); ok {
				out[path] = version
			}
		}
	}
	return out
}

func splitPathVersion(value string) (path, version string, ok bool) {
	fields := strings.Fields(value)
	if len(fields) < 2 || !strings.HasPrefix(fields[1], "v") {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// escapeModulePath mirrors the Go module cache's case-folding escape.
func escapeModulePath(path string) string {
	var escaped strings.Builder
	for _, r := range path {
		if r >= 'A' && r <= 'Z' {
			escaped.WriteByte('!')
			escaped.WriteRune(r + ('a' - 'A'))
			continue
		}
		escaped.WriteRune(r)
	}
	return escaped.String()
}

func pythonDependencyRoots(repoDir string) []string {
	var virtualEnvs []string
	if virtualEnv := os.Getenv("VIRTUAL_ENV"); virtualEnv != "" {
		virtualEnvs = append(virtualEnvs, virtualEnv)
	}
	virtualEnvs = append(virtualEnvs, filepath.Join(repoDir, ".venv"))

	var roots []string
	seen := map[string]bool{}
	for _, virtualEnv := range virtualEnvs {
		for _, sitePackages := range PythonSitePackageDirs(virtualEnv) {
			entries, err := os.ReadDir(sitePackages)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				root := filepath.Join(sitePackages, entry.Name())
				if !seen[root] {
					seen[root] = true
					roots = append(roots, root)
				}
			}
		}
	}
	return roots
}
