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

func TestGoCalleesOf(t *testing.T) {
	src := `package p

func (s *Svc) Create(req Req) error {
	validate(req)
	return s.store(req)
}

func other() {}
`
	got := GoCalleesOf("p.go", src, "Svc.Create") // bare names: free call + selector
	want := map[string]bool{"validate": true, "store": true}
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected callee %q in %v", n, got)
		}
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("missing callees %v (got %v)", want, got)
	}

	if GoCalleesOf("p.go", src, "Nope") != nil {
		t.Error("unknown symbol should resolve to nil")
	}
	if GoCalleesOf("p.go", "func (", "X") != nil {
		t.Error("unparseable source should resolve to nil")
	}
}
