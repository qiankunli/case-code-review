package codegraph

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
}

func writePyFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, src string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("app/store.py", `class Store:
    def resolve(self, key):
        return self.data[key]

    def purge(self, key):
        del self.data[key]
`)
	write("app/api.py", `from app.store import Store

def handle(key):
    s = Store()
    s.resolve(key)
    s.resolve(key)
    s.purge(key)
`)
	write("app/test_api.py", `def test_ignored():
    pass
`)
	return dir
}

func TestScanPy_DefsRefsAndSkips(t *testing.T) {
	requirePython3(t)
	ex := ScanPy(writePyFixture(t))
	if ex == nil {
		t.Fatal("ScanPy returned nil")
	}
	if _, ok := ex.Defs["app/test_api.py"]; ok {
		t.Error("test files must be skipped")
	}
	var resolve *Def
	for i, d := range ex.Defs["app/store.py"] {
		if d.Ident == "Store.resolve" {
			resolve = &ex.Defs["app/store.py"][i]
		}
	}
	if resolve == nil {
		t.Fatalf("Store.resolve not extracted: %+v", ex.Defs["app/store.py"])
	}
	if resolve.SymbolID != "app/store.py::Store.resolve" {
		t.Errorf("SymbolID = %q", resolve.SymbolID)
	}
	if !strings.Contains(resolve.Signature, "def resolve(self, key):") {
		t.Errorf("Signature = %q", resolve.Signature)
	}
	if ex.Refs["app/api.py"]["resolve"] < 2 {
		t.Errorf("api.py should reference resolve >=2 times, got %d", ex.Refs["app/api.py"]["resolve"])
	}
}

func TestScan_MergesGoAndPython(t *testing.T) {
	requirePython3(t)
	dir := writePyFixture(t)
	// Add a Go file to the same repo so both backends contribute.
	if err := os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc Entrypoint() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := Scan(dir)
	if len(ex.Defs["main.go"]) != 1 {
		t.Errorf("Go defs missing from merged scan: %+v", ex.Defs["main.go"])
	}
	if len(ex.Defs["app/store.py"]) == 0 {
		t.Errorf("Python defs missing from merged scan")
	}
}

func TestBuildMap_PythonSeeds(t *testing.T) {
	requirePython3(t)
	ex := Scan(writePyFixture(t))
	PairMethodIdents(ex)
	m := BuildMap(ex, MapRequest{
		SeedFiles:  []string{"app/api.py"},
		SeedIdents: []string{"resolve"},
	})
	if !strings.Contains(m, "app/store.py:") || !strings.Contains(m, "def resolve") {
		t.Errorf("map should surface Store.resolve from the seeded change:\n%s", m)
	}
}
