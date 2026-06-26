package unit

import "github.com/qiankunli/case-code-review/internal/model"

// The split pipeline names two kinds of Unit:
//
//   - a DIFF UNIT is what a Splitter finds in one file's diff — one per changed
//     function, plus a residual for changes outside any function.
//   - a REVIEW UNIT is what a Merger consolidates the diff units into — the unit
//     that actually triggers a review loop. A review unit is a diff unit as-is, or
//     several diff units coalesced up the granularity ladder (function → class →
//     file → module/directory) when a change is large.
//
// Two stages, two abstractions: Splitter (diff → diff units) and Merger (diff
// units → review units).

// FileDiffUnits pairs one file's diff units with their source diff. It is the
// input to a Merger, which needs the file diff to build a coalesced file unit.
type FileDiffUnits struct {
	Diff  model.Diff
	Units []Unit
}

// Merger consolidates per-file diff units into review units, coarsening up the
// granularity ladder by a strategy when there are too many — trading
// per-function focus for fewer review loops.
type Merger interface {
	Merge(files []FileDiffUnits) []Unit
}

// WatermarkMerger keeps diff units as review units until their total exceeds
// Watermark, then coalesces each file that split into more than one unit into a
// single file review unit. This is the function → file rung of the ladder; class
// and module/directory rungs are future strategies. Coalescing preserves spec
// context (CoalesceFile unions the members' function ids), so the governor caps
// loop count, not context.
type WatermarkMerger struct {
	Watermark int
}

func (m WatermarkMerger) Merge(files []FileDiffUnits) []Unit {
	total := 0
	for _, f := range files {
		total += len(f.Units)
	}
	coarsen := total > m.Watermark

	var review []Unit
	for _, f := range files {
		if coarsen && len(f.Units) > 1 {
			review = append(review, CoalesceFile(f.Diff, f.Units))
			continue
		}
		review = append(review, f.Units...)
	}
	return review
}
