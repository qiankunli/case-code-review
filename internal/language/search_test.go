package language

import (
	"strings"
	"testing"
)

func TestReferenceScope(t *testing.T) {
	cases := []struct{ path, name, want string }{
		{"internal/foo/x.go", "helper", "internal/foo"},
		{"internal/foo/x.go", "Helper", ""},
		{"x.go", "helper", "."},
		{"mod/x.py", "helper", ""},
		{"x.go", "", ""},
	}
	for _, c := range cases {
		got := ReferenceScope(c.path, c.name)
		if got != c.want {
			t.Errorf("ReferenceScope(%q,%q)=%q want %q", c.path, c.name, got, c.want)
		}
		if strings.ContainsRune(got, '\\') {
			t.Errorf("ReferenceScope(%q,%q)=%q must use slash separators", c.path, c.name, got)
		}
	}
}

func TestCommentLineByLanguage(t *testing.T) {
	if !IsCommentLine("x.go", " // prose") || IsCommentLine("x.go", "*ptr = Get()") {
		t.Fatal("Go comment classification is wrong")
	}
	if !IsCommentLine("x.py", " # prose") || IsCommentLine("x.py", "value = 1 # trailing") {
		t.Fatal("Python comment classification is wrong")
	}
}
