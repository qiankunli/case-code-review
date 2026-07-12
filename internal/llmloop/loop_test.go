package llmloop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/qiankunli/case-code-review/internal/llm"
	"github.com/qiankunli/case-code-review/internal/session"
	"github.com/qiankunli/case-code-review/internal/tool"
)

func TestExecuteToolCall_CodeCommentOverridesHallucinatedPath(t *testing.T) {
	collector := tool.NewCommentCollector()
	reg := tool.NewRegistry()
	reg.Register(&tool.CodeCommentProvider{Collector: collector})
	reg.Freeze()

	r := NewRunner(Deps{
		Tools:            reg,
		CommentCollector: collector,
	})

	args := map[string]any{
		"path": "wrong.go",
		"comments": []any{
			map[string]any{
				"content":       "issue",
				"existing_code": "foo",
			},
		},
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}

	cp := r.executeToolCall(context.Background(), session.Scope{ID: "correct.go", Kind: "file", Type: "file", Paths: []string{"correct.go"}}, llm.ToolCall{
		Function: llm.FunctionCall{
			Name:      "code_comment",
			Arguments: string(argsJSON),
		},
	}, nil, "")
	if cp.Data != tool.CommentSucceed {
		t.Fatalf("unexpected result: %+v", cp)
	}

	comments := collector.Comments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Path != "correct.go" {
		t.Errorf("path override: got %q, want %q", comments[0].Path, "correct.go")
	}
}

func TestRunnerModelsUsed(t *testing.T) {
	r := NewRunner(Deps{})
	if got := r.ModelsUsed(); len(got) != 0 {
		t.Errorf("fresh runner should report no models, got %v", got)
	}
	r.recordModel("deepseek-v4-pro")
	r.recordModel("deepseek-v4-pro")
	r.recordModel("seed-2.1-turbo")
	r.recordModel("") // empty alias (single-model / non-routing) is ignored

	got := r.ModelsUsed()
	if len(got) != 2 || got["deepseek-v4-pro"] != 2 || got["seed-2.1-turbo"] != 1 {
		t.Errorf("ModelsUsed deduped counts wrong: %v", got)
	}
	// returned map is a copy — mutating it must not affect the runner
	got["deepseek-v4-pro"] = 99
	if r.ModelsUsed()["deepseek-v4-pro"] != 2 {
		t.Error("ModelsUsed must return a copy, not the internal map")
	}
}

func TestExecuteToolCall_CodeCommentKeepsScopeMemberPath(t *testing.T) {
	collector := tool.NewCommentCollector()
	reg := tool.NewRegistry()
	reg.Register(&tool.CodeCommentProvider{Collector: collector})
	reg.Freeze()

	r := NewRunner(Deps{Tools: reg, CommentCollector: collector})

	// A call-chain scope spans two files; a comment on the SECOND member
	// must keep its path instead of being re-anchored to the first.
	args, err := json.Marshal(map[string]any{
		"path": "b.go",
		"comments": []any{
			map[string]any{"content": "issue", "existing_code": "foo"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cp := r.executeToolCall(context.Background(), session.Scope{ID: "chain", Kind: "unit", Type: "callchain", Paths: []string{"a.go", "b.go"}}, llm.ToolCall{
		Function: llm.FunctionCall{Name: "code_comment", Arguments: string(args)},
	}, nil, "")
	if cp.Data != tool.CommentSucceed {
		t.Fatalf("unexpected result: %+v", cp)
	}
	comments := collector.Comments()
	if len(comments) != 1 || comments[0].Path != "b.go" {
		t.Fatalf("member path must be kept, got %+v", comments)
	}
}

func TestExtractFacts_ToolSpecificPathArgs(t *testing.T) {
	calls := []llm.ToolCall{
		{Function: llm.FunctionCall{Name: "file_read",
			Arguments: `{"file_path":"a.go","start_line":3,"end_line":9}`}},
		// code_comment names its path argument "path", not "file_path" — the
		// flag fact must still be harvested (regression: it was silently dropped).
		{Function: llm.FunctionCall{Name: "code_comment",
			Arguments: `{"path":"b.go","content":"issue"}`}},
		{Function: llm.FunctionCall{Name: "code_comment", Arguments: `{"content":"no path"}`}},
	}
	facts := extractFacts("scope", 2, calls)
	if len(facts) != 2 {
		t.Fatalf("want 2 facts (read + flag), got %d: %+v", len(facts), facts)
	}
	if facts[0].Text != "read a.go:3-9" || facts[0].Paths[0] != "a.go" {
		t.Fatalf("unexpected read fact: %+v", facts[0])
	}
	if facts[1].Text != "flagged an issue in b.go" || facts[1].Paths[0] != "b.go" {
		t.Fatalf("unexpected flag fact: %+v", facts[1])
	}
}
