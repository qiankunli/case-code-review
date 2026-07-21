package language

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

// Python backend: defs with one-line signatures plus
// ident occurrence counts, extracted by the HOST's python3 with stdlib ast
// only (the pysplit pattern — no third-party deps, parsing always matches
// the host's Python). One process walks the whole repo: process startup,
// not parsing, dominates per-file invocation cost.
//
// Best-effort: absent python3, a timeout, or bad output degrade to nil and
// the map is built from the other backends alone.

const pyScanTimeout = 30 * time.Second

// pyScanScript walks sys.argv[1] and prints
// {relpath: {"defs": [{ident,symbol,line,sig}], "refs": {name: count}}}.
// Skip rules mirror the other repository backends: hidden dirs, vendor trees,
// tests, and oversized files.
const pyScanScript = `
import ast, json, os, sys

root = sys.argv[1]
SKIP_DIRS = {"vendor", "node_modules", "testdata", "__pycache__", "venv", "site-packages"}
MAX_FILES = 2000
MAX_BYTES = 512 * 1024

out = {}
count = 0
for dirpath, dirnames, filenames in os.walk(root):
    dirnames[:] = sorted(d for d in dirnames if not d.startswith(".") and d not in SKIP_DIRS)
    for fn in sorted(filenames):
        if not fn.endswith(".py") or fn.startswith("test_") or fn.endswith("_test.py"):
            continue
        if count >= MAX_FILES:
            break
        path = os.path.join(dirpath, fn)
        try:
            if os.path.getsize(path) > MAX_BYTES:
                continue
            src = open(path, encoding="utf-8", errors="replace").read()
            tree = ast.parse(src)
        except (OSError, SyntaxError):
            continue
        count += 1
        rel = os.path.relpath(path, root).replace(os.sep, "/")
        lines = src.splitlines()
        defs, refs = [], {}

        def sig_of(node):
            i = node.lineno - 1
            return " ".join(lines[i].split()) if 0 <= i < len(lines) else ""

        def add(node, prefix):
            ident = prefix + node.name
            defs.append({"ident": ident, "symbol": rel + "::" + ident,
                         "line": node.lineno, "sig": sig_of(node)})

        for c in ast.iter_child_nodes(tree):
            if isinstance(c, (ast.FunctionDef, ast.AsyncFunctionDef)):
                add(c, "")
            elif isinstance(c, ast.ClassDef):
                add(c, "")
                for m in ast.iter_child_nodes(c):
                    if isinstance(m, (ast.FunctionDef, ast.AsyncFunctionDef)):
                        add(m, c.name + ".")

        for n in ast.walk(tree):
            name = None
            if isinstance(n, ast.Name) and len(n.id) >= 3:
                name = n.id
            elif isinstance(n, ast.Attribute) and len(n.attr) >= 3:
                name = n.attr
            if name:
                refs[name] = refs.get(name, 0) + 1

        out[rel] = {"defs": defs, "refs": refs}

json.dump(out, sys.stdout)
`

type pyFileScan struct {
	Defs []struct {
		Ident  string `json:"ident"`
		Symbol string `json:"symbol"`
		Line   int    `json:"line"`
		Sig    string `json:"sig"`
	} `json:"defs"`
	Refs map[string]int `json:"refs"`
}

// scanPythonRepository extracts defs/refs from Python files. Returns nil when
// python3 is unavailable or anything goes wrong — callers merge backends
// and a missing one just narrows the map.
func scanPythonRepository(repoDir string) *RepositoryIndex {
	ctx, cancel := context.WithTimeout(context.Background(), pyScanTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", "-c", pyScanScript, repoDir)
	outBytes, err := cmd.Output()
	if err != nil {
		return nil
	}
	var raw map[string]pyFileScan
	if err := json.Unmarshal(outBytes, &raw); err != nil {
		return nil
	}
	ex := &RepositoryIndex{Definitions: map[string][]IndexedDefinition{}, References: map[string]map[string]int{}}
	for rel, fs := range raw {
		for _, d := range fs.Defs {
			ex.Definitions[rel] = append(ex.Definitions[rel], IndexedDefinition{
				Name:      d.Ident,
				SymbolID:  d.Symbol,
				Path:      rel,
				Line:      d.Line,
				Signature: d.Sig,
			})
		}
		if len(fs.Refs) > 0 {
			ex.References[rel] = fs.Refs
		}
	}
	return ex
}
