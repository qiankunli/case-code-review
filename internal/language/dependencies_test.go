package language

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDependencyRoots_GoModuleCache(t *testing.T) {
	repo := t.TempDir()
	cache := t.TempDir()
	t.Setenv("GOMODCACHE", cache)
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/org/app\n\nrequire github.com/org/Framework v1.2.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cache, "github.com/org/!framework@v1.2.3")
	if roots := DependencyRoots(repo); !containsPath(roots, want) {
		t.Fatalf("dependency roots %v do not contain %q", roots, want)
	}
}

func TestDependencyRoots_PythonVirtualEnv(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, ".venv", "lib", "python3.11", "site-packages", "framework")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if roots := DependencyRoots(repo); !containsPath(roots, root) {
		t.Fatalf("dependency roots %v do not contain %q", roots, root)
	}
}

func TestParseGoRequires(t *testing.T) {
	got := parseGoRequires("module github.com/org/app\n\nrequire github.com/org/framework v1.2.3\n\nrequire (\n\tgithub.com/a/b v0.1.0 // indirect\n\tgithub.com/c/d v2.0.0\n)\n")
	want := map[string]string{
		"github.com/org/framework": "v1.2.3",
		"github.com/a/b":           "v0.1.0",
		"github.com/c/d":           "v2.0.0",
	}
	for path, version := range want {
		if got[path] != version {
			t.Errorf("%s = %q, want %q", path, got[path], version)
		}
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
