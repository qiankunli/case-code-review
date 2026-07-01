package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/qiankunli/case-code-review/internal/unit"
)

// UnitContext is the review context ccr would inject for one review unit — what
// `--dry-run` prints (text) or emits (json) instead of running the LLM. The
// structural fields (Scope/Paths/Fragments/Clues) let `--format json` be used to
// compare how features (call-chain merge, clues) change unit shape, for free.
type UnitContext struct {
	ID        string         `json:"id"`
	Path      string         `json:"path"`                 // representative member path
	Scope     string         `json:"scope"`                // func / file / callchain
	Paths     []string       `json:"paths"`                // member files; len>1 = cross-file (call-chain) unit
	Fragments int            `json:"fragments"`            // changed regions merged into this unit
	Clues     map[string]int `json:"clues"`                // "<relation>/<kind>" -> count (e.g. owner/rule, used/doc, caller/spec)
	SpecCases string         `json:"spec_cases,omitempty"` // contract: own spec/case + inherited caller spec + depended-on callee contracts
	Rules     string         `json:"rules,omitempty"`      // path-glob rule.json + function-level @rule
	SeeAlso   string         `json:"see_also,omitempty"`   // curated @link pointers
	Prior     string         `json:"prior,omitempty"`      // a previous review's findings on this unit (to reconcile)
}

// countClues tallies a Unit's Dossier on the relation×kind matrix, keyed
// "<relation>/<kind>" (e.g. self/spec, owner/rule, used/doc, caller/spec) — so
// --dry-run shows which relation contributed which evidence, for free.
func countClues(clues unit.Dossier) map[string]int {
	m := make(map[string]int, len(clues))
	for _, c := range clues {
		m[string(c.Relation)+"/"+string(c.Kind)]++
	}
	return m
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
		specCases, specRules, seeAlso, prior := renderClues(u.Dossier)
		rule := a.resolveSystemRule(strings.ToLower(u.Path()))
		if specRules != "" {
			if rule != "" {
				rule += "\n"
			}
			rule += specRules
		}
		out = append(out, UnitContext{
			ID:        u.ID,
			Path:      u.Path(),
			Scope:     string(u.Scope),
			Paths:     u.Paths(),
			Fragments: len(u.Fragments),
			Clues:     countClues(u.Dossier),
			SpecCases: specCases,
			Rules:     rule,
			SeeAlso:   seeAlso,
			Prior:     prior,
		})
	}
	return preview, out, nil
}
