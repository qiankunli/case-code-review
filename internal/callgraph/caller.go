// Package callgraph supplies call-graph-derived review context. Today it holds
// CallerFinder, the ClueFinder that recovers a changed function's GOVERNING spec
// by walking one hop up to its callers. It is deliberately lightweight (git grep
// + go/ast, no whole-repo type checking) so it works on a diff that may not even
// compile, and degrades to nothing whenever it can't help.
package callgraph

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/qiankunli/case-code-review/internal/gitcmd"
	"github.com/qiankunli/case-code-review/internal/spec"
	"github.com/qiankunli/case-code-review/internal/unit"
)

const (
	defaultMaxCallers = 8
	grepTimeout       = 10 * time.Second
)

// CallerFinder supplies a changed function's governing spec when the function
// carries none of its own: it greps one hop up to its callers, resolves each hit
// to the calling function's unit-id, and — if a caller has a spec — attaches it
// as an inherited ClueCaller. Spec lives on entry functions (api-handlers) while
// a diff often lands on a deep helper, so the contract to preserve is the
// caller's. Go-only and depth-1 for now; bounded by Max and degrading to nil on
// any miss, so it is always safe to install.
type CallerFinder struct {
	RepoDir string
	Index   spec.Index
	Runner  *gitcmd.Runner // optional; falls back to exec when nil
	Max     int            // cap on resolved spec-bearing callers (0 -> default)
}

func (f CallerFinder) Find(u unit.Unit) []unit.Clue {
	// Only function units have a name to walk from, and we need both a repo to
	// grep and an index to resolve specs against.
	if f.Index == nil || f.RepoDir == "" || u.Scope != unit.ScopeFunc {
		return nil
	}
	// Own-spec short-circuit: a function with its own contract needs no walk —
	// this is what keeps a widely-called utility (huge fan-in) from exploding.
	for _, sym := range u.Symbols {
		if e, ok := f.Index[sym]; ok && (e.Spec != "" || len(e.Cases) > 0) {
			return nil
		}
	}

	max := f.Max
	if max <= 0 {
		max = defaultMaxCallers
	}
	self := map[string]bool{}
	for _, sym := range u.Symbols {
		self[sym] = true
	}

	emitted := map[string]bool{}
	var clues []unit.Clue
	for _, sym := range u.Symbols {
		name := funcName(sym)
		if name == "" {
			continue
		}
		for _, h := range f.grepCallers(name, max) {
			id, ok := f.callerID(h)
			if !ok || self[id] || emitted[id] { // skip the definition / recursion / dupes
				continue
			}
			e, ok := f.Index[id]
			if !ok || (e.Spec == "" && len(e.Cases) == 0) {
				continue
			}
			emitted[id] = true
			clues = append(clues, unit.Clue{
				Kind: unit.ClueCaller,
				Text: "(governing spec inherited from caller " + id + ")\n" + f.Index.Render([]string{id}),
				Ref:  id,
			})
			if len(emitted) >= max {
				return clues
			}
		}
	}
	return clues
}

// funcName returns the bare function/method name from a unit-id, for grepping:
// "pkg/x.go::Service.Get" -> "Get", "pkg/x.go::Helper" -> "Helper".
func funcName(unitID string) string {
	_, symbol, ok := unit.SplitID(unitID)
	if !ok {
		return ""
	}
	if i := strings.LastIndex(symbol, "."); i >= 0 {
		return symbol[i+1:]
	}
	return symbol
}

type hit struct {
	file string
	line int
}

// grepCallers word-matches the name across the repo's Go files. It over-fetches
// (resolution + dedup narrows to Max) and returns nil on any error so the finder
// degrades silently.
func (f CallerFinder) grepCallers(name string, max int) []hit {
	ctx, cancel := context.WithTimeout(context.Background(), grepTimeout)
	defer cancel()

	args := []string{"--no-pager", "grep", "-n", "-w", "--no-color", "-e", name, "--", "*.go"}
	out, err := f.gitOutput(ctx, args)
	if err != nil {
		return nil
	}
	var hits []hit
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3) // file:line:content
		if len(parts) < 3 {
			continue
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		hits = append(hits, hit{file: parts[0], line: n})
		if len(hits) >= max*4 {
			break
		}
	}
	return hits
}

func (f CallerFinder) gitOutput(ctx context.Context, args []string) ([]byte, error) {
	if f.Runner != nil {
		out, err := f.Runner.Output(ctx, f.RepoDir, args...)
		return out, err
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = f.RepoDir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// callerID reads the hit's file and resolves the line to its enclosing function.
func (f CallerFinder) callerID(h hit) (string, bool) {
	src, err := os.ReadFile(filepath.Join(f.RepoDir, h.file))
	if err != nil {
		return "", false
	}
	return unit.GoFuncIDAt(h.file, string(src), h.line)
}
