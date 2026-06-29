package unit

import (
	"strings"

	"github.com/qiankunli/case-code-review/internal/model"
)

// AutoSplitter routes each changed file to its language splitter — Go via
// go/ast in-process, Python via python3 — and falls back to file scope for
// everything else. It is the agent's default splitter. Each language splitter
// degrades to file scope on its own when it can't parse, so AutoSplitter is
// always safe.
type AutoSplitter struct{}

func (AutoSplitter) Split(d model.Diff) ([]Fragment, error) {
	switch {
	case strings.HasSuffix(d.NewPath, ".go"):
		return GoFuncSplitter{}.Split(d)
	case strings.HasSuffix(d.NewPath, ".py"):
		return PyFuncSplitter{}.Split(d)
	default:
		return FileSplitter{}.Split(d)
	}
}
