package language

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

func analyzeGo(source Source) (Analysis, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, source.Path, source.Content, parser.SkipObjectResolution)
	if err != nil {
		return Analysis{}, err
	}
	analysis := Analysis{Language: Go, Quality: QualitySyntax, References: map[string]int{}}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			owner := goReceiverName(d)
			kind := KindFunction
			if owner != "" {
				kind = KindMethod
			}
			id := SymbolID(source.Path, owner, d.Name.Name)
			analysis.Definitions = append(analysis.Definitions, Definition{
				SymbolID: id,
				Name:     symbolName(owner, d.Name.Name),
				Owner:    owner,
				Kind:     kind,
				Span: Span{
					Start: fset.Position(d.Pos()).Line,
					End:   fset.Position(d.End()).Line,
				},
				Signature: goSignature([]byte(source.Content), fset, d.Pos(), d.Type.End()),
			})
			ast.Inspect(d.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := goCallName(call)
				if name != "" {
					analysis.Calls = append(analysis.Calls, Call{CallerID: id, Name: name})
				}
				return true
			})
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				kind := KindType
				if _, ok := ts.Type.(*ast.InterfaceType); ok {
					kind = KindInterface
				}
				analysis.Definitions = append(analysis.Definitions, Definition{
					SymbolID:  SymbolID(source.Path, "", ts.Name.Name),
					Name:      ts.Name.Name,
					Kind:      kind,
					Span:      Span{Start: fset.Position(ts.Pos()).Line, End: fset.Position(ts.End()).Line},
					Signature: "type " + ts.Name.Name + goTypeShape(ts),
				})
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if len(n.Name) >= 3 {
				analysis.References[n.Name]++
			}
		case *ast.SelectorExpr:
			if len(n.Sel.Name) >= 3 {
				analysis.References[n.Sel.Name]++
			}
		}
		return true
	})
	return analysis, nil
}

func symbolName(owner, name string) string {
	if owner == "" {
		return name
	}
	return owner + "." + name
}

func goReceiverName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return ""
	}
	t := d.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

func goCallName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	default:
		return ""
	}
}

func goSignature(src []byte, fset *token.FileSet, pos, end token.Pos) string {
	startOffset := fset.Position(pos).Offset
	endOffset := fset.Position(end).Offset
	if startOffset < 0 || endOffset > len(src) || startOffset >= endOffset {
		return ""
	}
	return strings.Join(strings.Fields(string(src[startOffset:endOffset])), " ")
}

func goTypeShape(ts *ast.TypeSpec) string {
	switch ts.Type.(type) {
	case *ast.StructType:
		return " struct"
	case *ast.InterfaceType:
		return " interface"
	default:
		return ""
	}
}
