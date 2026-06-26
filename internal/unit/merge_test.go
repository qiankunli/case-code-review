package unit

import (
	"testing"

	"github.com/qiankunli/case-code-review/internal/model"
)

// fileWith builds a FileDiffUnits for path with n function diff units (ids
// path::f0..f{n-1}), each covering one line — enough for the merger's counting.
func fileWith(path string, n int) FileDiffUnits {
	d := model.Diff{NewPath: path, Diff: "@@ -1,1 +1,1 @@\n-a\n+b\n"}
	var us []Unit
	for range n {
		id := FuncID(path, "", "f")
		us = append(us, Unit{ID: id, Scope: ScopeFunc, Path: path, Symbols: []string{id}, Diff: d.Diff})
	}
	return FileDiffUnits{Diff: d, Units: us}
}

func TestWatermarkMerger_UnderWatermarkKeepsDiffUnits(t *testing.T) {
	m := WatermarkMerger{Watermark: 10}
	loop := m.Merge([]FileDiffUnits{fileWith("a.go", 2), fileWith("b.go", 1)})
	if len(loop) != 3 {
		t.Fatalf("under watermark: want 3 loop units (diff units kept), got %d", len(loop))
	}
	for _, u := range loop {
		if u.Scope != ScopeFunc {
			t.Errorf("want func scope, got %v", u.Scope)
		}
	}
}

func TestWatermarkMerger_OverWatermarkCoalescesMultiUnitFiles(t *testing.T) {
	// 9 single-unit files + 1 two-unit file = 11 > 10: only the multi-unit file
	// coalesces -> 9 func loop units + 1 file loop unit.
	var files []FileDiffUnits
	for i := range 9 {
		files = append(files, fileWith("s.go", 1))
		files[i].Diff.NewPath = string(rune('a'+i)) + ".go"
		files[i].Units[0].Path = files[i].Diff.NewPath
	}
	files = append(files, fileWith("multi.go", 2))

	loop := WatermarkMerger{Watermark: 10}.Merge(files)
	funcs, fileUnits := 0, 0
	for _, u := range loop {
		switch u.Scope {
		case ScopeFunc:
			funcs++
		case ScopeFile:
			fileUnits++
		}
	}
	if funcs != 9 || fileUnits != 1 {
		t.Fatalf("want 9 func + 1 file loop unit, got %d func + %d file", funcs, fileUnits)
	}
}
