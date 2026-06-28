package unit

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// GoFuncIDAt parses Go source and returns the unit-id of the function enclosing
// the given 1-indexed line, or ("", false) when the line is outside any function
// or src can't be parsed. Caller resolution uses it to map a grep hit's line to
// the function that contains the call. Go-only by construction (it reuses the
// go/ast span parser); other languages get their own resolver later.
func GoFuncIDAt(path, src string, line int) (string, bool) {
	spans, err := parseGoFuncs(path, src)
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

// GoCalleesOf parses Go source and returns the names of the functions/methods
// called inside the function identified by symbol ("Name" for a free function,
// "Recv.Method" for a method), deduplicated. Names are bare — the call's final
// identifier — so x.Validate() and pkg.Validate() both yield "Validate"; callee
// resolution then greps for a matching definition. Returns nil if the symbol
// isn't found or src can't be parsed. Go-only; callee extraction for other
// languages comes later.
func GoCalleesOf(path, src, symbol string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var body *ast.BlockStmt
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		sym := fd.Name.Name
		if recv := recvTypeName(fd); recv != "" {
			sym = recv + recvSep + fd.Name.Name
		}
		if sym == symbol {
			body = fd.Body
			break
		}
	}
	if body == nil {
		return nil
	}

	seen := map[string]bool{}
	var names []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch fn := call.Fun.(type) {
		case *ast.Ident: // bare call: foo()
			name = fn.Name
		case *ast.SelectorExpr: // x.Method() / pkg.Func()
			name = fn.Sel.Name
		}
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
		return true
	})
	return names
}
