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

func (PyFuncSplitter) Split(d model.Diff) ([]Unit, error) {
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
