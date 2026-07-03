package codegraph

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiankunli/case-code-review/internal/unit"
)

// v1 Go backend: a pure-syntax go/ast sweep. Defs are package-level
// declarations with a sliced one-line signature; refs are identifier
// occurrence counts. No type checking — the edges it feeds are
// ConfidenceLow by construction, which the ranking consumer tolerates.

const (
	// maxScanFiles bounds the sweep on huge repos: the map degrades (fewer
	// files ranked) instead of the build stalling. Files are walked in
	// deterministic (sorted) order so the truncation is stable.
	maxScanFiles = 2000
	// maxFileBytes skips generated/vendored monsters that would dominate
	// both parse time and ref counts.
	maxFileBytes = 512 * 1024
)

// ScanGo extracts defs/refs from all non-test .go files under repoDir.
// Vendor trees, hidden dirs and testdata are skipped. Best-effort: files
// that fail to parse are ignored.
func ScanGo(repoDir string) *Extraction {
	ex := &Extraction{
		Defs: map[string][]Def{},
		Refs: map[string]map[string]int{},
	}
	var files []string
	_ = filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if name != "." && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if len(files) > maxScanFiles {
		files = files[:maxScanFiles]
	}
	for _, path := range files {
		rel, err := filepath.Rel(repoDir, path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		scanGoFile(path, rel, ex)
	}
	return ex
}

func scanGoFile(path, rel string, ex *Extraction) {
	// Stat first: an oversized (generated/vendored) file should be skipped
	// without paying its full read+allocation.
	if fi, err := os.Stat(path); err != nil || fi.Size() > maxFileBytes {
		return
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return
	}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			recv := receiverType(d)
			ident := d.Name.Name
			if recv != "" {
				ident = recv + "." + d.Name.Name
			}
			ex.Defs[rel] = append(ex.Defs[rel], Def{
				Ident:     ident,
				SymbolID:  unit.FuncID(rel, recv, d.Name.Name),
				File:      rel,
				Line:      fset.Position(d.Pos()).Line,
				Signature: sliceSignature(src, fset, d.Pos(), d.Type.End()),
			})
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				ex.Defs[rel] = append(ex.Defs[rel], Def{
					Ident:     ts.Name.Name,
					SymbolID:  rel + "::" + ts.Name.Name,
					File:      rel,
					Line:      fset.Position(ts.Pos()).Line,
					Signature: "type " + ts.Name.Name + typeShape(ts),
				})
			}
		}
	}

	refs := map[string]int{}
	ast.Inspect(f, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || len(id.Name) < 3 {
			return true
		}
		refs[id.Name]++
		return true
	})
	// Method idents ("Recv.Name") never appear as a single *ast.Ident, so
	// also count selector pairs x.Sel under the bare Sel name — the pairing
	// axis defs use for methods is "Recv.Name", matched via the second pass
	// below in pairMethodRefs.
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || len(sel.Sel.Name) < 3 {
			return true
		}
		refs[sel.Sel.Name]++
		return true
	})
	ex.Refs[rel] = refs
}

// PairMethodIdents rewrites method defs ("Recv.Name") to also be reachable
// by their bare method name in ref pairing: refs are counted per bare ident
// (a call site says x.Save(), not Store.Save), so a method def gets an
// additional alias def under the bare name. The alias shares SymbolID, so
// downstream consumers still land on the canonical symbol.
func PairMethodIdents(ex *Extraction) {
	for f, defs := range ex.Defs {
		for _, d := range defs {
			if i := strings.LastIndex(d.Ident, "."); i > 0 {
				alias := d
				alias.Ident = d.Ident[i+1:]
				ex.Defs[f] = append(ex.Defs[f], alias)
			}
		}
	}
}

func receiverType(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return ""
	}
	t := d.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.IndexExpr: // generic receiver T[P]
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.ParenExpr: // rare but legal: func (t (MyType)) M()
			t = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// sliceSignature cuts the source between pos and end, collapsing whitespace
// runs so multi-line signatures render as one line.
func sliceSignature(src []byte, fset *token.FileSet, pos, end token.Pos) string {
	s := fset.Position(pos).Offset
	e := fset.Position(end).Offset
	if s < 0 || e > len(src) || s >= e {
		return ""
	}
	return strings.Join(strings.Fields(string(src[s:e])), " ")
}

func typeShape(ts *ast.TypeSpec) string {
	switch ts.Type.(type) {
	case *ast.StructType:
		return " struct"
	case *ast.InterfaceType:
		return " interface"
	default:
		return ""
	}
}
