package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/config/template"
	"github.com/qiankunli/case-code-review/internal/llm"
	"github.com/qiankunli/case-code-review/internal/model"
	"github.com/qiankunli/case-code-review/internal/tool"
)

// scriptedLLM replays canned responses and records requests.
type scriptedLLM struct {
	requests  []llm.ChatRequest
	responses []*llm.ChatResponse
}

func (c *scriptedLLM) CompletionsWithCtx(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	c.requests = append(c.requests, req)
	i := len(c.requests) - 1
	if i >= len(c.responses) {
		i = len(c.responses) - 1
	}
	return c.responses[i], nil
}

func respWithCalls(calls ...llm.ToolCall) *llm.ChatResponse {
	return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.ResponseMessage{Role: "assistant", ToolCalls: calls}}}}
}

func TestBuildContractSurface_CollectsShallowContractFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/x\n\ngo 1.26\n")
	write("build/Dockerfile", "FROM golang:1.25\n")
	write("deep/a/b/Makefile", "all:\n") // beyond 2 levels — skipped
	write("main.go", "package main\n")   // not a contract file

	a := New(Args{RepoDir: dir})
	got := a.buildContractSurface()
	if !strings.Contains(got, "== go.mod ==") || !strings.Contains(got, "go 1.26") {
		t.Errorf("go.mod missing:\n%s", got)
	}
	if !strings.Contains(got, "== build/Dockerfile ==") {
		t.Errorf("build/Dockerfile missing:\n%s", got)
	}
	if strings.Contains(got, "deep/a/b/Makefile") {
		t.Errorf("deep file should be skipped:\n%s", got)
	}
	if strings.Contains(got, "main.go") {
		t.Errorf("non-contract file leaked in:\n%s", got)
	}
}

// End-to-end through runConsistencyPass: the sweep prompt carries the full
// diff and contract surface, and a code_comment anchored on the SECOND
// changed file keeps its path (multi-path scope must not re-anchor members).
func TestRunConsistencyPass_CommentKeepsMemberPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	commentArgs, _ := json.Marshal(map[string]any{
		"path": "build/Dockerfile",
		"comments": []any{map[string]any{
			"content":       "builder image golang:1.25 contradicts go.mod (go 1.26)",
			"existing_code": "FROM golang:1.25",
			"start_line":    1,
			"end_line":      1,
		}},
	})
	client := &scriptedLLM{responses: []*llm.ChatResponse{
		respWithCalls(
			llm.ToolCall{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "code_comment", Arguments: string(commentArgs)}},
			llm.ToolCall{ID: "c2", Type: "function", Function: llm.FunctionCall{Name: "task_done", Arguments: "{}"}},
		),
	}}

	collector := tool.NewCommentCollector()
	reg := tool.NewRegistry()
	reg.Register(&tool.CodeCommentProvider{Collector: collector})
	reg.Freeze()
	tpl, err := template.LoadDefault()
	if err != nil {
		t.Fatal(err)
	}
	a := New(Args{
		RepoDir:          dir,
		LLMClient:        client,
		Template:         *tpl,
		Tools:            reg,
		CommentCollector: collector,
	})
	a.diffs = []model.Diff{
		{NewPath: "cmd/main.go", Diff: "@@ -1 +1 @@\n-old\n+new\n"},
		{NewPath: "build/Dockerfile", Diff: "@@ -1 +1 @@\n-FROM golang:1.24\n+FROM golang:1.25\n"},
	}

	a.runConsistencyPass(context.Background())

	if len(client.requests) == 0 {
		t.Fatal("no LLM request issued")
	}
	prompt := ""
	for _, m := range client.requests[0].Messages {
		if s, ok := m.Content.(string); ok {
			prompt += s + "\n"
		}
	}
	if !strings.Contains(prompt, "--- cmd/main.go ---") || !strings.Contains(prompt, "--- build/Dockerfile ---") {
		t.Errorf("full diff not injected:\n%s", prompt)
	}
	if !strings.Contains(prompt, "== go.mod ==") {
		t.Errorf("contract surface not injected:\n%s", prompt)
	}

	comments := collector.Comments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Path != "build/Dockerfile" {
		t.Errorf("comment path = %q, want build/Dockerfile (member path must be kept)", comments[0].Path)
	}
}
