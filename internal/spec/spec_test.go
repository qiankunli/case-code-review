package spec

import (
	"strings"
	"testing"
)

const fixture = `{
  "internal/notebook/handler.go::Service.CreateNotebook": {
    "spec": "tenant/user header required; (tenant,user,name) unique",
    "cases": [
      { "id": "happy_minimal", "desc": "name-only create succeeds", "expect": "201" },
      { "id": "duplicate_name", "desc": "duplicate name", "expect": "409", "forbid": "second row written" }
    ]
  },
  "internal/notebook/handler.go::Service.GetNotebook": {
    "spec": "locate by id or name; never cross tenant"
  }
}`

func TestParseAndRender(t *testing.T) {
	idx, err := Parse([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}

	// A unit covering the CreateNotebook symbol gets its spec + both cases.
	out := idx.Render([]string{"internal/notebook/handler.go::Service.CreateNotebook"})
	for _, want := range []string{
		"Service.CreateNotebook",
		"spec: tenant/user header required",
		"- happy_minimal: name-only create succeeds [expect: 201]",
		"[forbid: second row written]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q in:\n%s", want, out)
		}
	}

	// A symbol with no spec/case match contributes nothing.
	if got := idx.Render([]string{"internal/x/y.go::Unknown"}); got != "" {
		t.Errorf("unknown symbol should render empty, got %q", got)
	}

	// A coalesced unit covering several symbols unions their specs.
	multi := idx.Render([]string{
		"internal/notebook/handler.go::Service.CreateNotebook",
		"internal/notebook/handler.go::Service.GetNotebook",
	})
	if !strings.Contains(multi, "CreateNotebook") || !strings.Contains(multi, "GetNotebook") {
		t.Errorf("coalesced render should cover both symbols:\n%s", multi)
	}
}

func TestRenderNilIndexSafe(t *testing.T) {
	var idx Index // nil
	if got := idx.Render([]string{"any::Symbol"}); got != "" {
		t.Errorf("nil index should render empty, got %q", got)
	}
}
