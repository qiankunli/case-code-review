package language

import "testing"

func TestSymbolID(t *testing.T) {
	if got := SymbolID("internal/service.go", "Service", "Get"); got != "internal/service.go::Service.Get" {
		t.Fatalf("SymbolID = %q", got)
	}
	path, symbol, ok := SplitSymbolID("internal/service.go::Service.Get")
	if !ok || path != "internal/service.go" || symbol != "Service.Get" {
		t.Fatalf("SplitSymbolID = (%q, %q, %v)", path, symbol, ok)
	}
	if _, _, ok := SplitSymbolID("internal/service.go"); ok {
		t.Fatal("file scope id must not parse as a symbol id")
	}
}

func TestSymbolRelationships(t *testing.T) {
	if got := BareSymbolName("pkg/x.go::Service.Get"); got != "Get" {
		t.Fatalf("BareSymbolName = %q", got)
	}
	if got := BareSymbolName("not-an-id"); got != "" {
		t.Fatalf("invalid BareSymbolName = %q", got)
	}
	if owner, ok := EnclosingSymbolID("api.py::Outer.Inner.method"); !ok || owner != "api.py::Outer.Inner" {
		t.Fatalf("EnclosingSymbolID = (%q, %v)", owner, ok)
	}
	if _, ok := EnclosingSymbolID("api.py::top_level"); ok {
		t.Fatal("top-level symbol must not have an owner")
	}
	if !SymbolHasName("svc.go::Svc.Create", "Create") || SymbolHasName("svc.go::Svc.Create", "create") {
		t.Fatal("SymbolHasName must compare the final member exactly")
	}
}
