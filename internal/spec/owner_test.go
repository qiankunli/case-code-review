package spec

import (
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/unit"
)

func TestEnclosingSymbol(t *testing.T) {
	cases := []struct {
		id, want string
		ok       bool
	}{
		{"api.py::Svc.get", "api.py::Svc", true},
		{"handler.go::Service.CreateNotebook", "handler.go::Service", true},
		{"api.py::Outer.Inner.method", "api.py::Outer.Inner", true}, // immediate owner
		{"api.py::top_level", "", false},                            // no dot → no owner
		{"noscope", "", false},                                      // no ::
	}
	for _, c := range cases {
		got, ok := enclosingSymbol(c.id)
		if got != c.want || ok != c.ok {
			t.Errorf("enclosingSymbol(%q) = (%q,%v), want (%q,%v)", c.id, got, ok, c.want, c.ok)
		}
	}
}

func TestOwnerFinder(t *testing.T) {
	idx, err := Parse([]byte(`{
	  "trace.py::PhaseEventMiddleware": { "spec": "per-request lifecycle", "cases": [], "rules": ["per-request only — do not cache"], "links": ["docs/mw.md"] }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	// changing a *method* of PhaseEventMiddleware surfaces the class's markers
	u := unit.UnitOf(unit.Fragment{Path: "trace.py", Symbols: []string{"trace.py::PhaseEventMiddleware.dispatch"}})
	clues := OwnerFinder{Index: idx}.Find(u)

	var kinds []unit.ClueKind
	var ruleText, specText string
	for _, c := range clues {
		kinds = append(kinds, c.Kind)
		switch c.Kind {
		case unit.ClueRule:
			ruleText = c.Text
		case unit.ClueSpec:
			specText = c.Text
		}
	}
	if !strings.Contains(specText, "per-request lifecycle") {
		t.Errorf("want enclosing spec, got clues %+v", clues)
	}
	if !strings.Contains(ruleText, "per-request only") || !strings.Contains(ruleText, "PhaseEventMiddleware") {
		t.Errorf("want labelled enclosing rule, got %q", ruleText)
	}

	// when the class itself is the changed symbol, OwnerFinder stays silent
	// (SpecFinder covers own symbols) — top-level symbol has no owner anyway.
	self := unit.UnitOf(unit.Fragment{Path: "trace.py", Symbols: []string{"trace.py::PhaseEventMiddleware"}})
	if got := (OwnerFinder{Index: idx}).Find(self); got != nil {
		t.Errorf("top-level symbol has no owner, got %+v", got)
	}
}
