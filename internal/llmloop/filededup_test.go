package llmloop

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/config/template"
	"github.com/qiankunli/case-code-review/internal/llm"
	"github.com/qiankunli/case-code-review/internal/msg"
	"github.com/qiankunli/case-code-review/internal/session"
	"github.com/qiankunli/case-code-review/internal/tool"
)

// fileReadStub returns a canned file_read-shaped result for every call, so the
// loop produces promotable File messages.
type fileReadStub struct{ body string }

func (fileReadStub) Tool() tool.Tool { return tool.FileRead }
func (s fileReadStub) Execute(context.Context, map[string]any) (string, error) {
	return s.body, nil
}

func TestRunPerFile_FileDedupStubsCoveredRead(t *testing.T) {
	result := fmt.Sprintf("File: pkg/a.go (Total lines: 3)\nIS_TRUNCATED: false\nLINE_RANGE: 1-3\n%s",
		"1|package a\n2|\n3|func F() {}\n")

	client := &scriptedClient{responses: []*llm.ChatResponse{
		toolCallResp("file_read", `{"file_path":"pkg/a.go"}`), // round 1: read
		toolCallResp("file_read", `{"file_path":"pkg/a.go"}`), // round 2: same read again
		toolCallResp("task_done", `{}`),
	}}
	reg := tool.NewRegistry()
	reg.Register(fileReadStub{body: result})
	reg.Freeze()
	r := NewRunner(Deps{
		LLMClient:        client,
		Template:         template.Template{MaxToolRequestTimes: 10, MaxTokens: 10000},
		Tools:            reg,
		CommentCollector: tool.NewCommentCollector(),
		Session:          session.New(".", "test", "m", session.SessionOptions{}),
		FileDedupEnabled: true,
	})

	if _, err := r.RunPerFile(context.Background(), msg.Wrap([]llm.Message{llm.NewTextMessage("user", "review")}), scope()); err != nil {
		t.Fatalf("RunPerFile: %v", err)
	}
	if len(client.requests) != 3 {
		t.Fatalf("expected 3 rounds, got %d", len(client.requests))
	}

	// Round 3's request is built after the duplicate read landed: the FIRST
	// copy must be stubbed, the second kept in full, pairing ids intact.
	var full, stubs int
	for _, m := range client.requests[2].Messages {
		if m.Role != "tool" {
			continue
		}
		text := m.ExtractText()
		switch {
		case strings.Contains(text, "superseded"):
			stubs++
			if m.ToolCallID == "" {
				t.Fatalf("stub lost its tool_call pairing: %+v", m)
			}
		case strings.Contains(text, "func F() {}"):
			full++
		}
	}
	if stubs != 1 || full != 1 {
		t.Fatalf("want 1 stub + 1 full copy, got stubs=%d full=%d", stubs, full)
	}
}

func TestRunPerFile_FileDedupGateOff(t *testing.T) {
	result := fmt.Sprintf("File: pkg/a.go (Total lines: 1)\nIS_TRUNCATED: false\nLINE_RANGE: 1-1\n%s", "1|x\n")
	client := &scriptedClient{responses: []*llm.ChatResponse{
		toolCallResp("file_read", `{"file_path":"pkg/a.go"}`),
		toolCallResp("file_read", `{"file_path":"pkg/a.go"}`),
		toolCallResp("task_done", `{}`),
	}}
	reg := tool.NewRegistry()
	reg.Register(fileReadStub{body: result})
	reg.Freeze()
	r := NewRunner(Deps{
		LLMClient:        client,
		Template:         template.Template{MaxToolRequestTimes: 10, MaxTokens: 10000},
		Tools:            reg,
		CommentCollector: tool.NewCommentCollector(),
		Session:          session.New(".", "test", "m", session.SessionOptions{}),
	})
	if _, err := r.RunPerFile(context.Background(), msg.Wrap([]llm.Message{llm.NewTextMessage("user", "review")}), scope()); err != nil {
		t.Fatalf("RunPerFile: %v", err)
	}
	for _, m := range client.requests[2].Messages {
		if strings.Contains(m.ExtractText(), "superseded") {
			t.Fatal("gate off must not stub anything")
		}
	}
}

func TestEvictFiles(t *testing.T) {
	mk := func(path string, lines int) *msg.File {
		body := strings.Repeat("1|some code line here\n", lines)
		result := fmt.Sprintf("File: %s (Total lines: %d)\nIS_TRUNCATED: false\nLINE_RANGE: 1-%d\n%s", path, lines, lines, body)
		f, ok := msg.FileFromToolResult("file_read", result, llm.NewToolResultMessage("c", result))
		if !ok {
			t.Fatalf("promotion failed for %s", path)
		}
		return f
	}
	oldest := mk("a.go", 60)
	middle := mk("b.go", 60)
	newest := mk("c.go", 60)
	msgs := []msg.Msg{msg.Text("user", "task"), oldest, middle, newest}

	// A limit reachable by shedding exactly the two oldest files (stubs still
	// cost tokens — measure one on a sacrificial twin instead of guessing).
	sacrifice := mk("a.go", 60)
	sacrifice.Stub(msg.StubEvicted)
	stubTokens := countMsgTokens([]msg.Msg{sacrifice})
	limit := countMsgTokens([]msg.Msg{msgs[0], newest}) + 2*stubTokens + 5
	if n := evictFiles(msgs, limit); n != 2 {
		t.Fatalf("evicted = %d, want 2", n)
	}
	if !oldest.Stubbed() || !middle.Stubbed() || newest.Stubbed() {
		t.Fatal("must evict oldest-first and keep the newest read")
	}
	if got := countMsgTokens(msgs); got > limit {
		t.Fatalf("still over limit after eviction: %d > %d", got, limit)
	}

	// Under-limit conversations are untouched.
	if n := evictFiles(msgs, 1<<20); n != 0 {
		t.Fatal("must not evict under the limit")
	}
}
