package feature

import "testing"

func TestEnabled_DefaultsAllOn(t *testing.T) {
	var s Set // nil = all defaults
	for _, n := range Names() {
		if !s.Enabled(Gate(n)) {
			t.Errorf("gate %q should default on", n)
		}
	}
}

func TestResolve_PrecedenceCliWinsOverEnvOverConfig(t *testing.T) {
	config := map[Gate]bool{CallChain: false, Routing: false}
	env := map[Gate]bool{CallChain: true} // env overrides config
	cli := map[Gate]bool{Routing: true}   // cli overrides config
	s, err := Resolve(config, env, cli)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Enabled(CallChain) {
		t.Error("env should override config for callchain -> on")
	}
	if !s.Enabled(Routing) {
		t.Error("cli should override config for routing -> on")
	}
	if !s.Enabled(Plan) {
		t.Error("untouched gate should stay default on")
	}
}

func TestResolve_UnknownGateRejected(t *testing.T) {
	if _, err := Resolve(map[Gate]bool{Gate("bogus"): false}); err == nil {
		t.Fatal("unknown gate should error")
	}
}

func TestResolved_FillsEveryGate(t *testing.T) {
	s, _ := Resolve(map[Gate]bool{CallChain: false})
	r := s.Resolved()
	if len(r) != len(Names()) {
		t.Errorf("Resolved() should list all %d gates, got %d", len(Names()), len(r))
	}
	if r["callchain"] != false || r["plan"] != true {
		t.Errorf("Resolved mismatch: %v", r)
	}
}

func TestParse(t *testing.T) {
	m, err := Parse("callchain=off, routing=false ,caller_callee=1")
	if err != nil {
		t.Fatal(err)
	}
	if m[CallChain] != false || m[Routing] != false || m[CallerCallee] != true {
		t.Errorf("parse mismatch: %v", m)
	}
	if _, err := Parse("callchain"); err == nil {
		t.Error("missing = should error")
	}
	if _, err := Parse("callchain=maybe"); err == nil {
		t.Error("bad bool should error")
	}
}
