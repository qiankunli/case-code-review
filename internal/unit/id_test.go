package unit

import "testing"

func TestFuncID(t *testing.T) {
	tests := []struct {
		relpath, recv, name string
		want                string
	}{
		{"internal/notebook/service.go", "", "GetByName", "internal/notebook/service.go::GetByName"},
		{"internal/notebook/service.go", "NotebookService", "Get", "internal/notebook/service.go::NotebookService.Get"},
		{"app/api/notebook.py", "", "get_notebook", "app/api/notebook.py::get_notebook"},
	}
	for _, tt := range tests {
		if got := FuncID(tt.relpath, tt.recv, tt.name); got != tt.want {
			t.Errorf("FuncID(%q,%q,%q) = %q, want %q", tt.relpath, tt.recv, tt.name, got, tt.want)
		}
	}
}

func TestSplitID(t *testing.T) {
	// Round-trips a function id.
	relpath, symbol, ok := SplitID("internal/notebook/service.go::NotebookService.Get")
	if !ok {
		t.Fatal("expected ok for a function id")
	}
	if relpath != "internal/notebook/service.go" || symbol != "NotebookService.Get" {
		t.Errorf("got relpath=%q symbol=%q", relpath, symbol)
	}

	// A file-scope id is a bare path with no separator → not a function id.
	if _, _, ok := SplitID("internal/notebook/service.go"); ok {
		t.Error("file-scope id should not parse as a function id")
	}
}
