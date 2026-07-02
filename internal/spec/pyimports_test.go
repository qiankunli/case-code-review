package spec

import "testing"

func TestParsePyFromImports(t *testing.T) {
	src := `from framework.middleware.trace import PhaseEventMiddleware
from a.b import X, Y as Z
import os
`
	got := parsePyFromImports(src)
	if s := got["PhaseEventMiddleware"]; s.module != "framework.middleware.trace" || s.name != "PhaseEventMiddleware" {
		t.Errorf("PhaseEventMiddleware -> %+v", s)
	}
	if s := got["X"]; s.module != "a.b" || s.name != "X" {
		t.Errorf("X -> %+v", s)
	}
	if s := got["Z"]; s.module != "a.b" || s.name != "Y" { // alias: local Z -> real Y
		t.Errorf("Z -> %+v", s)
	}
	if _, ok := got["os"]; ok {
		t.Error("plain `import os` should not be captured by from-import parser")
	}
}
