package unit

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/qiankunli/case-code-review/internal/model"
)

// pyAST extracts function/method spans from Python source on stdin and prints
// them as JSON [{sym,start,end}]. It runs under the host's python3 (no third-party
// deps), so parsing is always correct for the host's Python. The span starts at
// the first decorator (so a change to a @spec/@case marker attributes to its
// function) and ends at the body's last line. Nested functions are not emitted —
// a change inside one attributes to its enclosing method via the range.
const pyAST = `
import ast, json, sys
try:
    tree = ast.parse(sys.stdin.read())
except SyntaxError:
    sys.exit(2)
out = []
def start(n):
    return n.decorator_list[0].lineno if n.decorator_list else n.lineno
def walk(node, prefix):
    for c in ast.iter_child_nodes(node):
        if isinstance(c, (ast.FunctionDef, ast.AsyncFunctionDef)):
            out.append({"sym": prefix + c.name, "start": start(c), "end": c.end_lineno})
        elif isinstance(c, ast.ClassDef):
            walk(c, prefix + c.name + ".")
walk(tree, "")
json.dump(out, sys.stdout)
`

// PyFuncSplitter cuts a changed Python file into one Unit per touched
// function/method by parsing the current source with the host's python3 at
// review time (fresh — boundaries are never stored, so they can't go stale).
// It degrades to FileSplitter for non-Python files, missing content, an absent
// python3, or a syntax error — so it is always safe to use.
type PyFuncSplitter struct{}

func (PyFuncSplitter) Split(d model.Diff) ([]Fragment, error) {
	if !strings.HasSuffix(d.NewPath, ".py") || d.NewFileContent == "" {
		return FileSplitter{}.Split(d)
	}
	spans, err := parsePyFuncs(d.NewPath, d.NewFileContent)
	if err != nil {
		return FileSplitter{}.Split(d) // python3 unavailable / unparseable — review whole file
	}
	return splitByFuncSpans(d, spans), nil
}

func parsePyFuncs(path, src string) ([]funcSpan, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", "-c", pyAST)
	cmd.Stdin = strings.NewReader(src)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var raw []struct {
		Sym   string `json:"sym"`
		Start int    `json:"start"`
		End   int    `json:"end"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}

	spans := make([]funcSpan, 0, len(raw))
	for _, r := range raw {
		spans = append(spans, funcSpan{start: r.Start, end: r.End, id: FuncID(path, "", r.Sym)})
	}
	return spans, nil
}

// PyFuncIDAt is the Python counterpart of GoFuncIDAt: it returns the symbol-id of
// the function enclosing the given 1-indexed line, or ("", false) when the line
// is outside any function, python3 is unavailable, or src is unparseable.
func PyFuncIDAt(path, src string, line int) (string, bool) {
	spans, err := parsePyFuncs(path, src)
	if err != nil {
		return "", false
	}
	for _, s := range spans {
		if line >= s.start && line <= s.end {
			return s.id, true
		}
	}
	return "", false
}

// pyCalleesAST reads Python source on stdin and the target symbol (sys.argv[1],
// "func" or "Class.method"), finds that function, and prints the JSON list of
// bare names it calls (Name f() and Attribute x.f() both yield "f"), so callee
// resolution can grep for matching `def` definitions.
const pyCalleesAST = `
import ast, json, sys
target = sys.argv[1] if len(sys.argv) > 1 else ""
try:
    tree = ast.parse(sys.stdin.read())
except SyntaxError:
    print("[]"); sys.exit(0)
hit = []
def find(node, stack):
    for c in ast.iter_child_nodes(node):
        if isinstance(c, (ast.FunctionDef, ast.AsyncFunctionDef)):
            if ".".join(stack + [c.name]) == target:
                hit.append(c)
        elif isinstance(c, ast.ClassDef):
            find(c, stack + [c.name])
find(tree, [])
names, seen = [], set()
if hit:
    for n in ast.walk(hit[0]):
        if isinstance(n, ast.Call):
            f = n.func
            nm = f.id if isinstance(f, ast.Name) else (f.attr if isinstance(f, ast.Attribute) else None)
            if nm and nm not in seen:
                seen.add(nm); names.append(nm)
json.dump(names, sys.stdout)
`

// PyCalleesOf is the Python counterpart of GoCalleesOf: the bare names of the
// functions/methods called inside the function identified by symbol. Returns nil
// if python3 is unavailable, src is unparseable, or the symbol isn't found.
func PyCalleesOf(path, src, symbol string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "python3", "-c", pyCalleesAST, symbol)
	cmd.Stdin = strings.NewReader(src)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var names []string
	if err := json.Unmarshal(out, &names); err != nil || len(names) == 0 {
		return nil // match GoCalleesOf: nil (not an empty slice) when nothing is found
	}
	return names
}
