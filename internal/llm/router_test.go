package llm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
)

// fakeClient is a programmable LLMClient for router tests: it records call count and
// returns a fixed resp/err.
type fakeClient struct {
	calls int
	resp  *ChatResponse
	err   error
}

func (f *fakeClient) CompletionsWithCtx(context.Context, ChatRequest) (*ChatResponse, error) {
	f.calls++
	return f.resp, f.err
}

func newRouter(members ...routerMember) *LLMRouter {
	return &LLMRouter{members: members, cooldown: make(map[int]time.Time)}
}

func TestLLMRouter_FalloverThenSuccess(t *testing.T) {
	c0 := &fakeClient{err: errors.New("network blip")} // unknown error → fallover
	c1 := &fakeClient{resp: &ChatResponse{ID: "ok"}}
	r := newRouter(routerMember{client: c0, label: "a"}, routerMember{client: c1, label: "b"})

	resp, err := r.CompletionsWithCtx(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "ok" {
		t.Fatalf("expected fallback response, got %+v", resp)
	}
	if c0.calls != 1 || c1.calls != 1 {
		t.Fatalf("calls c0=%d c1=%d, want 1/1", c0.calls, c1.calls)
	}

	// member 0 is now parked: a second call should skip it and hit c1 directly.
	if _, err := r.CompletionsWithCtx(context.Background(), ChatRequest{}); err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if c0.calls != 1 {
		t.Fatalf("parked member retried: c0.calls=%d, want 1", c0.calls)
	}
	if c1.calls != 2 {
		t.Fatalf("c1.calls=%d, want 2", c1.calls)
	}
}

func TestLLMRouter_ClientErrorShortCircuits(t *testing.T) {
	c0 := &fakeClient{err: &openai.Error{StatusCode: 400}} // bad request → no fallover
	c1 := &fakeClient{resp: &ChatResponse{ID: "ok"}}
	r := newRouter(routerMember{client: c0, label: "a"}, routerMember{client: c1, label: "b"})

	if _, err := r.CompletionsWithCtx(context.Background(), ChatRequest{}); err == nil {
		t.Fatal("expected error to propagate, got nil")
	}
	if c1.calls != 0 {
		t.Fatalf("next model tried on client-side error: c1.calls=%d, want 0", c1.calls)
	}
}

func TestLLMRouter_AllExhausted(t *testing.T) {
	c0 := &fakeClient{err: errors.New("down")}
	c1 := &fakeClient{err: errors.New("down")}
	r := newRouter(routerMember{client: c0, label: "a"}, routerMember{client: c1, label: "b"})

	_, err := r.CompletionsWithCtx(context.Background(), ChatRequest{})
	if err == nil {
		t.Fatal("expected exhausted error, got nil")
	}
	if c0.calls != 1 || c1.calls != 1 {
		t.Fatalf("calls c0=%d c1=%d, want both 1", c0.calls, c1.calls)
	}
}

func TestLLMRouter_StopsWhenContextDone(t *testing.T) {
	c0 := &fakeClient{err: errors.New("boom")}
	c1 := &fakeClient{resp: &ChatResponse{ID: "ok"}}
	r := newRouter(routerMember{client: c0, label: "a"}, routerMember{client: c1, label: "b"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // shared budget exhausted — no member can succeed

	if _, err := r.CompletionsWithCtx(ctx, ChatRequest{}); err == nil {
		t.Fatal("expected error when ctx is done, got nil")
	}
	if c1.calls != 0 {
		t.Fatalf("fell over despite done ctx: c1.calls=%d, want 0", c1.calls)
	}
}

func TestShouldFallover(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unknown", errors.New("boom"), true},
		{"canceled", context.Canceled, false},
		{"openai 400", &openai.Error{StatusCode: 400}, false},
		{"openai 429", &openai.Error{StatusCode: 429}, true},
		{"anthropic 503", &anthropic.Error{StatusCode: 503}, true},
		{"anthropic 401", &anthropic.Error{StatusCode: 401}, true},
	}
	for _, c := range cases {
		if got := shouldFallover(c.err); got != c.want {
			t.Errorf("%s: shouldFallover=%v, want %v", c.name, got, c.want)
		}
	}
}

func TestNewLLMRouter_SinglePoolNoRouter(t *testing.T) {
	ep := ResolvedEndpoint{URL: "https://x.example.com", Protocol: "openai", Model: "m", Token: "t"}
	if _, isRouter := NewLLMRouter([]ResolvedEndpoint{ep}, "").(*LLMRouter); isRouter {
		t.Fatal("single-model pool should not be wrapped in a router")
	}
	two := NewLLMRouter([]ResolvedEndpoint{ep, ep}, "")
	if _, isRouter := two.(*LLMRouter); !isRouter {
		t.Fatal("multi-model pool should be a router")
	}
}

func TestResolveModels_Chain(t *testing.T) {
	for _, k := range []string{"OCR_LLM_URL", "OCR_LLM_TOKEN", "OCR_LLM_MODEL", "ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL"} {
		t.Setenv(k, "")
	}
	cfg := configFile{
		Routing: routingConfig{
			Models: []modelRef{{Provider: "p1", Alias: "primary"}, {Provider: "p2", Model: "m2b"}},
			Policy: "priority",
		},
		CustomProviders: map[string]providerEntryConfig{
			"p1": {URL: "https://a.example.com", Protocol: "openai", APIKey: "k1", Model: "m1"},
			"p2": {URL: "https://b.example.com", Protocol: "openai", APIKey: "k2", Model: "m2"},
		},
	}
	data, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	eps, policy, err := ResolveModels(cfgPath)
	if err != nil {
		t.Fatalf("ResolveModels: %v", err)
	}
	if policy != policyPriority {
		t.Errorf("policy=%q, want %q", policy, policyPriority)
	}
	if len(eps) != 2 {
		t.Fatalf("pool size=%d, want 2", len(eps))
	}
	if eps[0].Model != "m1" {
		t.Errorf("eps[0].Model=%q, want m1", eps[0].Model)
	}
	if eps[1].Model != "m2b" { // explicit ref.Model wins over the provider's default m2
		t.Errorf("eps[1].Model=%q, want m2b (ref.Model overrides provider default)", eps[1].Model)
	}
	if eps[0].Alias != "primary" { // routing.models[].alias flows to the endpoint
		t.Errorf("eps[0].Alias=%q, want primary", eps[0].Alias)
	}
}

func TestLLMRouter_StampsAliasOnResponse(t *testing.T) {
	c := &fakeClient{resp: &ChatResponse{ID: "ok"}}
	r := &LLMRouter{
		members:  []routerMember{{client: c, label: "x", alias: "deepseek"}},
		cooldown: make(map[int]time.Time),
	}
	resp, err := r.CompletionsWithCtx(context.Background(), ChatRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Alias != "deepseek" { // router attributes the response to the member that served it
		t.Fatalf("resp.Alias=%q, want deepseek", resp.Alias)
	}
}

func TestResolveModels_RejectsUnknownPolicy(t *testing.T) {
	for _, k := range []string{"OCR_LLM_URL", "OCR_LLM_TOKEN", "OCR_LLM_MODEL", "ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL"} {
		t.Setenv(k, "")
	}
	cfg := configFile{
		Routing: routingConfig{
			Models: []modelRef{{Provider: "p1"}},
			Policy: "weighted", // not supported yet → reserved, must error rather than silently ignore
		},
		CustomProviders: map[string]providerEntryConfig{
			"p1": {URL: "https://a.example.com", Protocol: "openai", APIKey: "k1", Model: "m1"},
		},
	}
	data, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolveModels(cfgPath); err == nil {
		t.Fatal("expected error for unsupported routing.policy, got nil")
	}
}

func TestLLMRouter_RoundRobinSpreadsLoad(t *testing.T) {
	c0 := &fakeClient{resp: &ChatResponse{ID: "0"}}
	c1 := &fakeClient{resp: &ChatResponse{ID: "1"}}
	c2 := &fakeClient{resp: &ChatResponse{ID: "2"}}
	r := &LLMRouter{
		members:  []routerMember{{client: c0, label: "a"}, {client: c1, label: "b"}, {client: c2, label: "c"}},
		policy:   policyRoundRobin,
		cooldown: make(map[int]time.Time),
	}
	// All members succeed → only the first in each call's order() is hit. Over len(pool)
	// calls, round-robin must rotate the start so each member is picked exactly once.
	for i := range 3 {
		if _, err := r.CompletionsWithCtx(context.Background(), ChatRequest{}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if c0.calls != 1 || c1.calls != 1 || c2.calls != 1 {
		t.Fatalf("round-robin did not spread load: c0=%d c1=%d c2=%d, want 1/1/1", c0.calls, c1.calls, c2.calls)
	}
}

func TestResolveModels_AcceptsRoundRobin(t *testing.T) {
	for _, k := range []string{"OCR_LLM_URL", "OCR_LLM_TOKEN", "OCR_LLM_MODEL", "ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL"} {
		t.Setenv(k, "")
	}
	cfg := configFile{
		Routing: routingConfig{
			Models: []modelRef{{Provider: "p1"}, {Provider: "p2"}},
			Policy: "round-robin",
		},
		CustomProviders: map[string]providerEntryConfig{
			"p1": {URL: "https://a.example.com", Protocol: "openai", APIKey: "k1", Model: "m1"},
			"p2": {URL: "https://b.example.com", Protocol: "openai", APIKey: "k2", Model: "m2"},
		},
	}
	data, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, policy, err := ResolveModels(cfgPath)
	if err != nil {
		t.Fatalf("ResolveModels: %v", err)
	}
	if policy != policyRoundRobin {
		t.Errorf("policy=%q, want %q", policy, policyRoundRobin)
	}
}
