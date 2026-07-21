package codegraph

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeTSFixture(t *testing.T) string {
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
	write("src/service.ts", `export interface Config {
  enabled: boolean;
}

export enum Status {
  Ready,
}

export class Service {
  run(config: Config) {
    return config.enabled && validate(config);
  }
}

export const handler = () => new Service().run({ enabled: true });

export const workers = {
  resolve: () => handler(),
};
`)
	write("src/view.tsx", `import { Service } from "./service";

const service = new Service();

export const View = () => (
  <button onClick={() => service.run({ enabled: true })}>run</button>
);

export function submit() {
  return service.run({ enabled: true });
}
`)
	write("src/format.js", `export function formatValue(value) {
  return String(value);
}
`)
	write("src/service.test.ts", `export function ignoredTestHelper() {
  return true;
}
`)
	return dir
}

func requireTypeScriptScan(t *testing.T, dir string) *Extraction {
	t.Helper()
	ex, err := scanTS(dir)
	var exitErr *exec.ExitError
	var execErr *exec.Error
	if errors.As(err, &execErr) || (errors.As(err, &exitErr) && exitErr.ExitCode() == 3) {
		t.Skip("node and the TypeScript compiler are not available")
	}
	if err != nil {
		t.Fatalf("ScanTS: %v", err)
	}
	return ex
}

func findDef(defs []Def, ident string) *Def {
	for i := range defs {
		if defs[i].Ident == ident {
			return &defs[i]
		}
	}
	return nil
}

func TestScanTS_DefsRefsAndSkips(t *testing.T) {
	dir := writeTSFixture(t)
	ex := requireTypeScriptScan(t, dir)

	if _, ok := ex.Defs["src/service.test.ts"]; ok {
		t.Error("test files must be skipped")
	}
	serviceDefs := ex.Defs["src/service.ts"]
	for _, ident := range []string{"Config", "Status", "Service", "Service.run", "handler", "workers.resolve"} {
		if findDef(serviceDefs, ident) == nil {
			t.Errorf("%s not extracted: %+v", ident, serviceDefs)
		}
	}
	run := findDef(serviceDefs, "Service.run")
	if run == nil {
		t.Fatal("Service.run not extracted")
	}
	if run.SymbolID != "src/service.ts::Service.run" {
		t.Errorf("SymbolID = %q", run.SymbolID)
	}
	if !strings.Contains(run.Signature, "run(config: Config)") {
		t.Errorf("Signature = %q", run.Signature)
	}
	if findDef(ex.Defs["src/view.tsx"], "View") == nil || findDef(ex.Defs["src/view.tsx"], "submit") == nil {
		t.Errorf("TSX defs missing: %+v", ex.Defs["src/view.tsx"])
	}
	if findDef(ex.Defs["src/format.js"], "formatValue") == nil {
		t.Errorf("JavaScript defs missing: %+v", ex.Defs["src/format.js"])
	}
	if ex.Refs["src/view.tsx"]["run"] < 2 {
		t.Errorf("view.tsx should reference run >=2 times, got %d", ex.Refs["src/view.tsx"]["run"])
	}
}

func TestScanTS_MissingCompilerDegrades(t *testing.T) {
	t.Setenv("PATH", "")
	if ex := ScanTS(writeTSFixture(t)); ex != nil {
		t.Fatalf("missing Node should return nil, got %+v", ex)
	}
}

func TestScan_MergesTypeScript(t *testing.T) {
	dir := writeTSFixture(t)
	requireTypeScriptScan(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc Entrypoint() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := Scan(dir)
	if len(ex.Defs["main.go"]) != 1 {
		t.Errorf("Go defs missing from merged scan: %+v", ex.Defs["main.go"])
	}
	if findDef(ex.Defs["src/service.ts"], "Service.run") == nil {
		t.Errorf("TypeScript defs missing from merged scan: %+v", ex.Defs["src/service.ts"])
	}
}

func TestBuildMap_TypeScriptSeeds(t *testing.T) {
	ex := requireTypeScriptScan(t, writeTSFixture(t))
	PairMethodIdents(ex)
	m := BuildMap(ex, MapRequest{
		SeedFiles:  []string{"src/view.tsx"},
		SeedIdents: []string{"run"},
	})
	if !strings.Contains(m, "src/service.ts:") || !strings.Contains(m, "run(config: Config)") {
		t.Errorf("map should surface Service.run from the seeded change:\n%s", m)
	}
}
