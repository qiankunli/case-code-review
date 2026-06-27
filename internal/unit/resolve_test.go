package unit

import "testing"

func TestGoFuncIDAt(t *testing.T) {
	src := `package p

func Alpha() {
	helper()
}

func (s *Svc) Beta() int {
	return 1
}
`
	cases := []struct {
		line int
		want string
		ok   bool
	}{
		{4, "p.go::Alpha", true},     // helper() call, inside Alpha
		{8, "p.go::Svc.Beta", true},  // inside the method (receiver normalized)
		{1, "", false},               // package line, outside any func
	}
	for _, c := range cases {
		id, ok := GoFuncIDAt("p.go", src, c.line)
		if ok != c.ok || id != c.want {
			t.Errorf("line %d -> (%q,%v); want (%q,%v)", c.line, id, ok, c.want, c.ok)
		}
	}

	if _, ok := GoFuncIDAt("p.go", "func (", 1); ok {
		t.Error("unparseable source should resolve to false")
	}
}
