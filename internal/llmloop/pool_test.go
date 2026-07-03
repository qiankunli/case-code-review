package llmloop

import (
	"testing"

	"github.com/qiankunli/case-code-review/internal/model"
)

func TestCommentWorkerPool_PanicIsIsolated(t *testing.T) {
	p := NewCommentWorkerPool(2)

	p.Submit(func() ([]model.LlmComment, error) {
		panic("boom in submitted work")
	})
	p.Submit(func() ([]model.LlmComment, error) {
		return []model.LlmComment{{Path: "healthy.go", Content: "fine"}}, nil
	})

	// Await must not crash: the recovered panic contributes no comments, and the
	// healthy task's result is still collected.
	results := p.Await()
	if len(results) != 1 {
		t.Fatalf("expected 1 result after a panicking task, got %d", len(results))
	}
	if results[0].Path != "healthy.go" {
		t.Errorf("Path = %q, want healthy.go", results[0].Path)
	}
}
