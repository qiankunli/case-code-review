package msg

import (
	"fmt"
	"strings"
	"testing"

	"github.com/qiankunli/case-code-review/internal/llm"
)

func fileResult(path string, total, start, end int, body string) string {
	return fmt.Sprintf("File: %s (Total lines: %d)\nIS_TRUNCATED: false\nLINE_RANGE: %d-%d\n%s",
		path, total, start, end, body)
}

func mkFile(t *testing.T, path string, total, start, end int) *File {
	t.Helper()
	result := fileResult(path, total, start, end, "1|code\n")
	f, ok := FileFromToolResult(FileReadToolName, result, llm.NewToolResultMessage("c1", result))
	if !ok {
		t.Fatalf("expected promotion for %s", path)
	}
	return f
}

func TestFileFromToolResult(t *testing.T) {
	f := mkFile(t, "pkg/a.go", 120, 10, 40)
	if f.Path != "pkg/a.go" || f.Start != 10 || f.End != 40 || f.Total != 120 {
		t.Fatalf("parsed identity off: %+v", f)
	}
	// Other tools and malformed results stay Raw.
	if _, ok := FileFromToolResult("code_search", "hits", llm.Message{}); ok {
		t.Fatal("non-file_read must not promote")
	}
	if _, ok := FileFromToolResult(FileReadToolName, `Error: file "x" not found`, llm.Message{}); ok {
		t.Fatal("error result must not promote")
	}
	// Lowering an un-stubbed File is the original wire message.
	if got := f.Lower(); got.ToolCallID != "c1" || !strings.Contains(got.ExtractText(), "1|code") {
		t.Fatalf("lowered wire off: %+v", got)
	}
}

func TestFileStubKeepsPairing(t *testing.T) {
	f := mkFile(t, "pkg/a.go", 120, 10, 40)
	f.Stub()
	got := f.Lower()
	if got.Role != "tool" || got.ToolCallID != "c1" {
		t.Fatalf("stub must keep the tool_result pairing: %+v", got)
	}
	if !strings.Contains(got.ExtractText(), "superseded") || strings.Contains(got.ExtractText(), "1|code") {
		t.Fatalf("stub must elide content: %q", got.ExtractText())
	}
}

func TestDedupFiles(t *testing.T) {
	old := mkFile(t, "pkg/a.go", 120, 10, 40)
	other := mkFile(t, "pkg/b.go", 50, 1, 50)
	partial := mkFile(t, "pkg/a.go", 120, 5, 20) // overlaps but not covered by newer
	newer := mkFile(t, "pkg/a.go", 120, 10, 60)  // covers old, not partial

	msgs := []Msg{Text("user", "task"), old, other, partial, Text("assistant", "…"), newer}
	if n := DedupFiles(msgs); n != 1 {
		t.Fatalf("stubbed = %d, want 1", n)
	}
	if !old.Stubbed() {
		t.Fatal("covered earlier read must be stubbed")
	}
	if other.Stubbed() || partial.Stubbed() || newer.Stubbed() {
		t.Fatal("uncovered / different-path / newest reads must be kept")
	}
	// Idempotent: a second pass finds nothing new.
	if n := DedupFiles(msgs); n != 0 {
		t.Fatalf("second pass stubbed %d, want 0", n)
	}
}
