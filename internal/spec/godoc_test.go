package spec

import (
	"path/filepath"
	"testing"
)

func TestExtractGoDoc(t *testing.T) {
	src := `package trace

// PhaseEventMiddleware accumulates phase events for one request.
// Per-request only — do not cache/reuse.
//
// Internal details beyond the first paragraph.
type PhaseEventMiddleware struct{}

// Dispatch handles one event.
func (m *PhaseEventMiddleware) Dispatch() {}

func Undocumented() {}
`
	if got := extractGoDoc(src, "PhaseEventMiddleware"); got != "PhaseEventMiddleware accumulates phase events for one request. Per-request only — do not cache/reuse." {
		t.Errorf("type doc = %q", got)
	}
	if got := extractGoDoc(src, "PhaseEventMiddleware.Dispatch"); got != "Dispatch handles one event." {
		t.Errorf("method doc = %q", got)
	}
	if got := extractGoDoc(src, "Undocumented"); got != "" {
		t.Errorf("undocumented should be empty, got %q", got)
	}
}

func TestSymbolDocstring_Go(t *testing.T) {
	repo := t.TempDir()
	write(t, filepath.Join(repo, "mw", "trace.go"),
		"package mw\n\n// Middleware is per-request only.\ntype Middleware struct{}\n")
	if got := SymbolDocstring(repo, "mw/trace.go::Middleware"); got != "Middleware is per-request only." {
		t.Errorf("SymbolDocstring(go) = %q", got)
	}
}
