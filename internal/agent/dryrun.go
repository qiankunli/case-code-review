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

// DryRunContext loads diffs, splits into review units, runs the context finders,
// and returns each unit's assembled context — with no LLM call. It is the engine
// behind `ccr review --dry-run`: it shows exactly what each unit's review would
// see (spec/case/rule/link + caller/callee), so spec.json coverage and the
// call-graph resolution can be inspected for free.
func (a *Agent) DryRunContext(ctx context.Context) ([]UnitContext, error) {
	if err := a.loadDiffs(ctx); err != nil {
		return nil, fmt.Errorf("load diffs: %w", err)
	}
	units, err := a.splitUnits()
	if err != nil {
		return nil, fmt.Errorf("split units: %w", err)
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
	return out, nil
}
