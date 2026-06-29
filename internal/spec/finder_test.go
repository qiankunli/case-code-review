package spec

import (
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/unit"
)

func TestFinders(t *testing.T) {
	idx, err := Parse([]byte(`{
	  "a.go::Foo": {
	    "spec": "must stay tenant-scoped",
	    "cases": [{ "id": "happy", "desc": "ok", "expect": "200" }],
	    "rules": ["hot path; watch new sync DB calls"],
	    "links": ["docs/x.md", "a.go::Bar"]
	  }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	u := unit.UnitOf(unit.Fragment{Path: "a.go", Symbols: []string{"a.go::Foo"}})

	spec := SpecFinder{Index: idx}.Find(u)
	if len(spec) != 1 || spec[0].Kind != unit.ClueSpec || !strings.Contains(spec[0].Text, "tenant-scoped") {
		t.Errorf("SpecFinder: %+v", spec)
	}

	rules := RuleFinder{Index: idx}.Find(u)
	if len(rules) != 1 || rules[0].Kind != unit.ClueRule || rules[0].Text != "hot path; watch new sync DB calls" {
		t.Errorf("RuleFinder: %+v", rules)
	}

	links := LinkFinder{Index: idx}.Find(u)
	if len(links) != 2 || links[0].Ref != "docs/x.md" || links[0].Text != "docs/x.md (doc)" ||
		links[1].Text != "a.go::Bar (function)" {
		t.Errorf("LinkFinder should label doc vs function and keep Ref: %+v", links)
	}
}

func TestFindersNilAndUnknownSafe(t *testing.T) {
	var nilIdx Index
	u := unit.UnitOf(unit.Fragment{Path: "x", Symbols: []string{"x::Unknown"}})
	for _, f := range []unit.ClueFinder{SpecFinder{nilIdx}, RuleFinder{nilIdx}, LinkFinder{nilIdx}} {
		if got := f.Find(u); got != nil {
			t.Errorf("nil index should find nothing, got %+v", got)
		}
	}
}
