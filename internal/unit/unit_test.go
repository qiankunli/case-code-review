package unit

import (
	"testing"

	"github.com/qiankunli/case-code-review/internal/model"
)

func TestFileSplitter_OneUnitPerFile(t *testing.T) {
	d := model.Diff{
		NewPath:    "internal/foo/bar.go",
		Diff:       "@@ -1,2 +1,3 @@\n+added line\n",
		Insertions: 1,
		Deletions:  0,
	}

	units, err := FileSplitter{}.Split(d)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(units))
	}

	u := units[0]
	if u.Scope != ScopeFile {
		t.Errorf("scope: got %q, want %q", u.Scope, ScopeFile)
	}
	if u.ID != d.NewPath || u.Path != d.NewPath {
		t.Errorf("identity: ID=%q Path=%q, want both %q", u.ID, u.Path, d.NewPath)
	}
	if len(u.Symbols) != 0 {
		t.Errorf("file-scope unit should cover no functions, got Symbols %v", u.Symbols)
	}
	if u.Diff != d.Diff {
		t.Errorf("Diff: got %q, want whole-file diff %q", u.Diff, d.Diff)
	}
	if u.Insertions != d.Insertions || u.Deletions != d.Deletions {
		t.Errorf("counts: got +%d/-%d, want +%d/-%d (file's own)",
			u.Insertions, u.Deletions, d.Insertions, d.Deletions)
	}
}
