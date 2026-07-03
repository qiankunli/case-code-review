package codegraph

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/qiankunli/case-code-review/internal/unit"
)

// Typed backend (phase ②): call edges resolved by the Go type checker via
// go/packages. Unlike the syntax scan (goscan.go, name-paired, ranking-only),
// these edges are precise enough for scope decisions — chain merge and
// caller/callee walks. The precision discipline is "ambiguous → no edge":
// interface-dispatch call sites are skipped entirely rather than guessed at,
// because a wrong caller edge corrupts the governing-spec ascent and a wrong
// adjacency edge corrupts unit merging.
//
// Cost model: one packages.Load of the whole module per review (seconds on a
// mid-size repo), built lazily and once (see callgraph.TypedGraph). Any load
// failure — the diff may not even compile — degrades to the grep heuristics.

const (
	// maxTypedPackages aborts the build on very large modules instead of
	// stalling the review; callers fall back to grep.
	maxTypedPackages = 500
)

// CallGraph holds type-checker-resolved, repo-internal call edges, keyed by
// the same <relpath>::<symbol> ids the rest of ccr speaks.
type CallGraph struct {
	callers map[string][]string // callee id -> caller ids
	callees map[string][]string // caller id -> callee ids
	nodes   int
	edges   int
}

// Callers returns the functions calling id (empty slice = truly no callers
// found by the type checker, as far as the loaded module goes).
func (g *CallGraph) Callers(id string) []string { return g.callers[id] }

// Callees returns the functions id calls.
func (g *CallGraph) Callees(id string) []string { return g.callees[id] }

// Stats returns (distinct functions seen, directed edges) for logging.
func (g *CallGraph) Stats() (nodes, edges int) { return g.nodes, g.edges }

// BuildGoCallGraph type-checks the module at repoDir and extracts direct call
// edges between repo-internal functions. Test files are excluded. Returns an
// error when the module fails to load wholesale; per-file type errors are
// tolerated (packages carries partial results).
func BuildGoCallGraph(ctx context.Context, repoDir string) (*CallGraph, error) {
	absRoot, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}

	cfg := &packages.Config{
		Context: ctx,
		Dir:     repoDir,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}
	if len(pkgs) > maxTypedPackages {
		return nil, fmt.Errorf("module too large for typed graph (%d packages > %d)", len(pkgs), maxTypedPackages)
	}
	// Load "succeeds" on a non-module / empty directory too — it just yields
	// no parseable syntax. Treat that as failure so TypedGraph doesn't claim
	// authoritative empty answers for a repo it never actually saw.
	parsed := 0
	for _, p := range pkgs {
		parsed += len(p.Syntax)
	}
	if parsed == 0 {
		return nil, fmt.Errorf("no parseable Go packages under %s", repoDir)
	}

	g := &CallGraph{callers: map[string][]string{}, callees: map[string][]string{}}
	seenEdge := map[[2]string]bool{}
	seenNode := map[string]bool{}

	for _, pkg := range pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			rel := relIn(absRoot, pkg.Fset, file.Pos())
			if rel == "" {
				continue
			}
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				callerID := unit.FuncID(rel, receiverType(fd), fd.Name.Name)
				seenNode[callerID] = true
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					calleeID := resolveCallee(pkg.TypesInfo, pkg.Fset, absRoot, call)
					if calleeID == "" || calleeID == callerID {
						return true
					}
					seenNode[calleeID] = true
					key := [2]string{callerID, calleeID}
					if !seenEdge[key] {
						seenEdge[key] = true
						g.callees[callerID] = append(g.callees[callerID], calleeID)
						g.callers[calleeID] = append(g.callers[calleeID], callerID)
						g.edges++
					}
					return true
				})
			}
		}
	}
	for _, m := range []map[string][]string{g.callers, g.callees} {
		for k := range m {
			sort.Strings(m[k])
		}
	}
	g.nodes = len(seenNode)
	return g, nil
}

// resolveCallee resolves a call expression to a repo-internal function's
// symbol-id, or "" when the target is external, unresolved, or ambiguous
// (interface dispatch). Precision over recall throughout.
func resolveCallee(info *types.Info, fset *token.FileSet, absRoot string, call *ast.CallExpr) string {
	var obj types.Object
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		obj = info.Uses[fun]
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[fun]; ok {
			// Method call through a value: only concrete receivers give an
			// unambiguous target. An interface method's "definition" is the
			// interface itself — an edge there would be a guess.
			if sel.Kind() != types.MethodVal || types.IsInterface(sel.Recv()) {
				return ""
			}
			obj = sel.Obj()
		} else {
			// Qualified call: pkg.Func.
			obj = info.Uses[fun.Sel]
		}
	default:
		return "" // function literals, indexed expressions, conversions
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return ""
	}
	rel := relIn(absRoot, fset, fn.Pos())
	if rel == "" {
		return "" // stdlib / external dependency — outside the review's repo
	}
	return unit.FuncID(rel, recvTypeName(fn), fn.Name())
}

// recvTypeName extracts the bare receiver type name of a method ("" for a
// free function), stripping pointers and type parameters to match the
// symbol-id contract.
func recvTypeName(fn *types.Func) string {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return ""
	}
	t := sig.Recv().Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return ""
	}
	return named.Obj().Name()
}

// relIn maps a token position into a repo-relative, slash-separated path,
// or "" when the position falls outside absRoot (external code, generated
// cache files) or inside a test file.
func relIn(absRoot string, fset *token.FileSet, pos token.Pos) string {
	if !pos.IsValid() {
		return ""
	}
	f := fset.Position(pos).Filename
	if resolved, err := filepath.EvalSymlinks(f); err == nil {
		f = resolved
	}
	rel, err := filepath.Rel(absRoot, f)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if strings.HasSuffix(rel, "_test.go") {
		return ""
	}
	return rel
}
