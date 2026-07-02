package spec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// extractGoDoc returns the summary doc comment (first paragraph) of a Go symbol
// in src, parsed with go/ast (the same infrastructure the splitter trusts for
// function boundaries — no regex approximation, so grouped `type (...)` decls
// and unusual formatting resolve correctly). name is the symbol part of a
// symbol-id: a bare `Foo` for a func/type, or `Recv.Method` for a method
// (matched against its receiver's type name). Best-effort; "" when the file
// doesn't parse or the symbol has no doc.
func extractGoDoc(src, name string) string {
	f, err := parser.ParseFile(token.NewFileSet(), "src.go", src, parser.ParseComments)
	if err != nil {
		return ""
	}
	recv, method, isMethod := strings.Cut(name, ".")
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if isMethod {
				if decl.Recv != nil && recvTypeName(decl.Recv) == recv && decl.Name.Name == method {
					return docSummary(decl.Doc)
				}
			} else if decl.Recv == nil && decl.Name.Name == name {
				return docSummary(decl.Doc)
			}
		case *ast.GenDecl:
			if decl.Tok != token.TYPE || isMethod {
				continue
			}
			for _, s := range decl.Specs {
				ts, ok := s.(*ast.TypeSpec)
				if !ok || ts.Name.Name != name {
					continue
				}
				// grouped decl: the doc sits on the TypeSpec; single decl: on the GenDecl.
				if ts.Doc != nil {
					return docSummary(ts.Doc)
				}
				return docSummary(decl.Doc)
			}
		}
	}
	return ""
}

// recvTypeName unwraps a method receiver to its type name (`*Recv`, `Recv`,
// `Recv[T]` all yield "Recv").
func recvTypeName(recv *ast.FieldList) string {
	if len(recv.List) == 0 {
		return ""
	}
	t := recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if idx, ok := t.(*ast.IndexExpr); ok { // generic receiver
		t = idx.X
	}
	if ident, ok := t.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

func docSummary(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	return summarizeDoc(doc.Text()) // reuse: first paragraph, whitespace-collapsed
}
