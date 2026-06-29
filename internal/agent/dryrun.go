package agent

import (
	"context"
	"fmt"
	"strings"
)

// UnitContext is the review context ccr would inject for one review unit — what
// `--dry-run` prints instead of running the LLM.
type UnitContext struct {
	ID        string
	Path      string
	SpecCases string // contract: own spec/case + inherited caller spec + depended-on callee contracts
	Rules     string // path-glob rule.json + function-level @rule
	SeeAlso   string // curated @link pointers
}

// DryRun loads diffs once and returns the complete no-LLM view behind
// `ccr review --dry-run`: the file-selection preview (which files are reviewed /
// excluded — the `--preview` subset) plus each review unit's assembled context
// (spec/case/rule/link + caller/callee). So both file filtering and spec.json /
// call-graph coverage can be inspected in one pass, for free.
func (a *Agent) DryRun(ctx context.Context) (*DiffPreview, []UnitContext, error) {
	if err := a.loadDiffs(ctx); err != nil {
		return nil, nil, fmt.Errorf("load diffs: %w", err)
	}
	preview := a.buildPreview()
	units, err := a.splitUnits()
	if err != nil {
		return nil, nil, fmt.Errorf("split units: %w", err)
	}

	out := make([]UnitContext, 0, len(units))
	for _, u := range units {
		// Mirror reviewUnit's context assembly: clues + the path-glob rule.json.
		specCases, specRules, seeAlso := renderClues(u.Clues)
		rule := a.resolveSystemRule(strings.ToLower(u.Path))
		if specRules != "" {
			if rule != "" {
				rule += "\n"
			}
			rule += specRules
		}
		out = append(out, UnitContext{
			ID:        u.ID,
			Path:      u.Path,
			SpecCases: specCases,
			Rules:     rule,
			SeeAlso:   seeAlso,
		})
	}
	return preview, out, nil
}
