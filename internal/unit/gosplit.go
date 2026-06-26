package unit

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/qiankunli/case-code-review/internal/diff"
	"github.com/qiankunli/case-code-review/internal/model"
)

// GoFuncSplitter cuts a changed Go file into one review Unit per touched
// top-level function/method, plus a residual file Unit for changes outside any
// function (imports, package-level decls). It is the language-aware Splitter
// that makes spec/case association possible (a func Unit carries a FuncID).
//
// It degrades to FileSplitter for non-Go files, when the new file content is
// unavailable, or when the source can't be parsed — so it is always safe to use
// as the agent's splitter.
type GoFuncSplitter struct{}

// funcSpan is one top-level function's line range and identity in the new file.
type funcSpan struct {
	start, end int // 1-indexed line range in the new file, inclusive
	recv, name string
}

func (GoFuncSplitter) Split(d model.Diff) ([]Unit, error) {
	if !strings.HasSuffix(d.NewPath, ".go") || d.NewFileContent == "" {
		return FileSplitter{}.Split(d)
	}
	spans, err := parseGoFuncs(d.NewPath, d.NewFileContent)
	if err != nil {
		// Unparseable (syntax error / generics edge / build tag) — don't guess,
		// review the whole file.
		return FileSplitter{}.Split(d)
	}

	header := diffHeader(d.Diff)
	hunks := diff.ParseHunks(d.Diff)

	// Group hunks by the function they land in; -1 = residual (outside any func).
	grouped := make(map[int][]diff.Hunk)
	for _, h := range hunks {
		grouped[funcOfHunk(h, spans)] = append(grouped[funcOfHunk(h, spans)], h)
	}

	var units []Unit
	// Func units, in source order so output is stable.
	for i := range spans {
		hs := grouped[i]
		if len(hs) == 0 {
			continue
		}
		id := FuncID(d.NewPath, spans[i].recv, spans[i].name)
		ins, del := countChanges(hs)
		units = append(units, Unit{
			ID:         id,
			Scope:      ScopeFunc,
			Path:       d.NewPath,
			Symbol:     id,
			Diff:       header + renderHunks(hs),
			Insertions: ins,
			Deletions:  del,
		})
	}
	// Residual: changes outside any function.
	if hs := grouped[-1]; len(hs) > 0 {
		ins, del := countChanges(hs)
		units = append(units, Unit{
			ID:         d.NewPath,
			Scope:      ScopeFile,
			Path:       d.NewPath,
			Diff:       header + renderHunks(hs),
			Insertions: ins,
			Deletions:  del,
		})
	}

	// Nothing attributed (e.g. all hunks were pure deletions past EOF) — fall
	// back rather than emit an empty review.
	if len(units) == 0 {
		return FileSplitter{}.Split(d)
	}
	return units, nil
}

// parseGoFuncs returns the line spans of every top-level func/method declaration.
func parseGoFuncs(path, src string) ([]funcSpan, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var spans []funcSpan
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		spans = append(spans, funcSpan{
			start: fset.Position(fd.Pos()).Line,
			end:   fset.Position(fd.End()).Line,
			recv:  recvTypeName(fd),
			name:  fd.Name.Name,
		})
	}
	return spans, nil
}

// recvTypeName returns the method's receiver type name, with pointer and generic
// type parameters stripped (e.g. "*Service[T]" -> "Service"). "" for a free function.
func recvTypeName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	expr := fd.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok { // *T
		expr = star.X
	}
	if idx, ok := expr.(*ast.IndexExpr); ok { // T[P]
		expr = idx.X
	}
	if idx, ok := expr.(*ast.IndexListExpr); ok { // T[P, Q]
		expr = idx.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// funcOfHunk returns the index into spans of the function a hunk belongs to, or
// -1 if it falls outside every function. Attribution uses the new-file line of
// the hunk's first changed line (added line, or the hunk start for a pure deletion).
func funcOfHunk(h diff.Hunk, spans []funcSpan) int {
	line := changedLine(h)
	for i := range spans {
		if line >= spans[i].start && line <= spans[i].end {
			return i
		}
	}
	return -1
}

// changedLine returns the new-file line number of the hunk's first added line,
// or NewStart if the hunk only deletes lines.
func changedLine(h diff.Hunk) int {
	cur := h.NewStart
	for _, l := range h.Lines {
		switch l.Type {
		case diff.HunkAdded:
			return cur
		case diff.HunkDeleted:
			// not present in the new file; doesn't advance the new-file cursor
		default: // context
			cur++
		}
	}
	return h.NewStart
}

func countChanges(hs []diff.Hunk) (ins, del int64) {
	for _, h := range hs {
		for _, l := range h.Lines {
			switch l.Type {
			case diff.HunkAdded:
				ins++
			case diff.HunkDeleted:
				del++
			}
		}
	}
	return ins, del
}

// diffHeader returns the file-level header of a unified diff (everything before
// the first "@@" hunk), so a per-function slice still names its file.
func diffHeader(rawDiff string) string {
	if i := strings.Index(rawDiff, "\n@@"); i >= 0 {
		return rawDiff[:i+1]
	}
	if strings.HasPrefix(rawDiff, "@@") {
		return ""
	}
	return rawDiff
}

// renderHunks reconstructs unified-diff text for the given hunks.
func renderHunks(hs []diff.Hunk) string {
	var b strings.Builder
	for _, h := range hs {
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", h.OldStart, h.OldCount, h.NewStart, h.NewCount)
		for _, l := range h.Lines {
			switch l.Type {
			case diff.HunkAdded:
				b.WriteString("+" + l.Content + "\n")
			case diff.HunkDeleted:
				b.WriteString("-" + l.Content + "\n")
			default:
				b.WriteString(" " + l.Content + "\n")
			}
		}
	}
	return b.String()
}
