package language

import "testing"

func TestExtractPyDocstring(t *testing.T) {
	src := `class PhaseEventMiddleware:
    """Per-request only — do not cache/reuse.

    Accumulates events across a run.
    """
    def __init__(self): ...

def one_liner():
    'just one line'

def undocumented():
    return 1
`
	if got := extractPyDocstring(src, "PhaseEventMiddleware"); got != "Per-request only — do not cache/reuse." {
		t.Errorf("class docstring summary = %q", got)
	}
	if got := extractPyDocstring(src, "one_liner"); got != "just one line" {
		t.Errorf("one-liner = %q", got)
	}
	if got := extractPyDocstring(src, "undocumented"); got != "" {
		t.Errorf("undocumented should be empty, got %q", got)
	}
}
