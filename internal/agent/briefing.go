package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/qiankunli/case-code-review/internal/callgraph"
	"github.com/qiankunli/case-code-review/internal/feature"
	"github.com/qiankunli/case-code-review/internal/unit"
)

// The briefing is what a review Unit's loop starts from: the shared user-message
// skeleton (one template for every scope) filled with the unit's diff, its
// dossier's clues, and the source MATERIALS assembled here. Materials exist
// because session traces show the loop's early rounds are otherwise spent
// fetching content ccr already knows the unit needs (file_read of the reviewed
// file, code_search for callers/usages). Metaphor chain: Clue(线索) is gathered
// evidence, Dossier(卷宗) is the deduped case file, Briefing(交底) is what's
// laid on the reviewer's desk before they start.

// material is one piece of source content the briefing wants inlined — the
// render protocol's data contract between the per-scope briefer (which decides
// WHAT the reviewer should see without searching) and renderMaterials (which
// reads, formats, and enforces the byte budget). Keeping the contract
// data-shaped rather than "render yourself" keeps budgeting and the
// file_read-mirroring line format single-sourced across scopes.
type material struct {
	path    string
	symbols []string // the functions in path that matter (span fallback / span-only content)
	whole   bool     // prefer the whole file (the unit's own source); false = symbols' spans only
	label   string   // how the material relates to the unit, e.g. "caller a/b.go::F"; "" for own source
	prio    int      // 0 = the reviewed source itself; higher drops first when the budget tightens
}

// briefer is the render protocol: a Unit scope's answer to "which source belongs
// in this unit's briefing". Implementations are per unit.Scope, return
// descriptors only (no IO), and stay ignorant of budgets and formatting.
type briefer interface {
	materials(u unit.Unit) []material
}

// brieferFor picks the scope's briefer. Func and file units differ only in what
// their Fragments carry, so both present their own source; call-chain units
// additionally surface the bodies of the chain's caller/callee neighbors.
func (a *Agent) brieferFor(s unit.Scope) briefer {
	if s == unit.ScopeCallChain {
		return chainBriefer{neighbors: a.features.Enabled(feature.NeighborSource)}
	}
	return ownSourceBriefer{}
}

// ownSourceBriefer presents the unit's own file(s) whole, carrying the unit's
// symbols so an over-budget file can fall back to just the changed functions'
// bodies (see renderMaterials).
type ownSourceBriefer struct{}

func (ownSourceBriefer) materials(u unit.Unit) []material {
	symbolsByPath := map[string][]string{}
	for _, f := range u.Fragments {
		symbolsByPath[f.Path] = append(symbolsByPath[f.Path], f.Symbols...)
	}
	var out []material
	for _, p := range u.Paths() {
		out = append(out, material{path: p, symbols: symbolsByPath[p], whole: true})
	}
	return out
}

// maxNeighborMaterials caps how many neighbor bodies a chain briefing inlines —
// a chain reviews an edge set, so a handful of far-side bodies carries the
// signal; past that the budget is better spent on member source.
const maxNeighborMaterials = 6

// chainBriefer presents member source first, then the bodies of the
// caller/callee neighbors the dossier already resolved (clue Refs): a chain
// review reasons across call edges, so the far side of each edge should be
// visible without a search. Only bodies of functions OUTSIDE the chain — a
// member is never its sibling's neighbor (walkNeighbors seeds visited).
type chainBriefer struct {
	neighbors bool // the neighbor_source gate
}

func (b chainBriefer) materials(u unit.Unit) []material {
	mats := ownSourceBriefer{}.materials(u)
	if !b.neighbors {
		return mats
	}
	own := map[string]bool{}
	for _, p := range u.Paths() {
		own[p] = true
	}
	seen := map[string]bool{}
	count := 0
	for _, c := range u.Dossier {
		if c.Relation != unit.RelCaller && c.Relation != unit.RelCallee {
			continue
		}
		path, _, ok := unit.SplitID(c.Ref)
		if !ok || own[path] || seen[c.Ref] {
			continue
		}
		seen[c.Ref] = true
		mats = append(mats, material{
			path:    path,
			symbols: []string{c.Ref},
			label:   string(c.Relation) + " " + c.Ref,
			prio:    1,
		})
		count++
		if count >= maxNeighborMaterials {
			break
		}
	}
	return mats
}

// preloadSourceBudget caps the total bytes of source inlined into one briefing.
// Sized so typical units fit whole while a giant file can't crowd the prompt
// toward the token guard (which would skip the unit's review outright).
const preloadSourceBudget = 32 * 1024

// sourceNotPreloaded fills {{unit_source}} when nothing was inlined, so the
// literal placeholder never leaks and the reviewer knows to fetch on demand.
const sourceNotPreloaded = "(not preloaded — fetch what you need via file_read)"

// renderMaterials reads and formats a briefer's materials under one shared byte
// budget, filled in priority order (own source first — an over-budget aux
// material is dropped silently, never at the essentials' expense). Output is
// split by prio: unitSource (prio 0, the reviewed files — fills {{unit_source}})
// and relatedSource (prio >0, context-only bodies — fills {{related_source}}),
// because the two carry different review semantics (review this vs. don't).
//
// Reads go through the file_read tool's own FileReader (mode-aware: workspace
// reads disk, range/commit read `git show <ref>:`) and mirror its numbered-line
// format, so inlined source and tool output look identical to the model. A
// whole-file material over the remaining budget falls back to just its
// functions' bodies (ranged_preload gate); with no symbols (or gate off) it is
// named but not inlined so a ranged file_read still works.
func (a *Agent) renderMaterials(ctx context.Context, mats []material) (unitSource, relatedSource string) {
	fr := a.fileReader()
	if fr == nil {
		return sourceNotPreloaded, ""
	}

	// Stable fill order: essentials before aux. Sort is by prio only (slice order
	// within a prio is already meaningful: member files, then dossier order).
	ordered := make([]material, len(mats))
	copy(ordered, mats)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].prio < ordered[j].prio })

	budget := preloadSourceBudget
	var essential, aux strings.Builder
	for _, m := range ordered {
		out := &essential
		if m.prio > 0 {
			out = &aux
		}
		content, err := fr.Read(ctx, m.path)
		if err != nil {
			continue // deleted/unreadable — the placeholder degrade below handles it
		}
		lines := strings.Split(strings.TrimRight(content, "\n"), "\n")

		if m.whole && len(content) <= budget {
			budget -= len(content)
			fmt.Fprintf(out, "File: %s (Total lines: %d)\n", m.path, len(lines))
			for i, line := range lines {
				fmt.Fprintf(out, "%d|%s\n", i+1, line)
			}
			out.WriteString("\n")
			continue
		}

		// Whole file didn't fit (or was never wanted): inline the named functions'
		// bodies as ranged blocks. Span-only materials always take this path;
		// whole-file fallback additionally needs the ranged_preload gate.
		if len(m.symbols) > 0 && (!m.whole || a.features.Enabled(feature.RangedPreload)) {
			if rendered := renderSpans(m, content, lines, &budget); rendered != "" {
				out.WriteString(rendered)
				continue
			}
		}

		if m.whole {
			fmt.Fprintf(out, "File: %s — %d bytes exceeds the preload budget; read on demand via file_read\n\n", m.path, len(content))
		}
		// Aux material that couldn't be inlined is dropped silently — it's bonus
		// context; a note would spend prompt on what the reviewer can't use.
	}

	unitSource = strings.TrimRight(essential.String(), "\n")
	if unitSource == "" {
		unitSource = sourceNotPreloaded
	}
	return unitSource, strings.TrimRight(aux.String(), "\n")
}

// renderSpans inlines each of m.symbols' bodies from content as a ranged block
// mirroring file_read's range output (File header + LINE_RANGE + numbered
// lines), charging bytes against budget. Returns "" when nothing fit or no
// symbol resolved to a span.
func renderSpans(m material, content string, lines []string, budget *int) string {
	var b strings.Builder
	for _, sym := range m.symbols {
		start, end, ok := unit.SymbolSpan(m.path, content, sym)
		if !ok || start < 1 || end > len(lines) {
			continue
		}
		var s strings.Builder
		if m.label != "" {
			fmt.Fprintf(&s, "// %s\n", m.label)
		}
		fmt.Fprintf(&s, "File: %s (Total lines: %d)\nLINE_RANGE: %d-%d\n", m.path, len(lines), start, end)
		for i := start; i <= end; i++ {
			fmt.Fprintf(&s, "%d|%s\n", i, lines[i-1])
		}
		s.WriteString("\n")
		if s.Len() > *budget {
			continue // this body doesn't fit; a smaller later one still might
		}
		*budget -= s.Len()
		b.WriteString(s.String())
	}
	return b.String()
}

// renderUsageSites pre-greps where else the repo references the unit's changed
// symbols and renders a `path:line: text` blast-radius map for {{usage_sites}}.
// Same cost class as the caller/callee walk, so it honors the same costly-context
// budget gate (a.costlyContext) on top of its own feature gate. Returns "" when
// gated off or nothing was found.
func (a *Agent) renderUsageSites(u unit.Unit) string {
	if !a.features.Enabled(feature.UsageSites) || !a.costlyContext {
		return ""
	}
	symbols := u.AllSymbols()
	if len(symbols) == 0 {
		return ""
	}
	exclude := map[string]bool{}
	for _, p := range u.Paths() {
		exclude[p] = true
	}
	usages := callgraph.FindUsages(a.args.RepoDir, a.args.GitRunner, symbols, exclude)
	if len(usages) == 0 {
		return ""
	}
	var b strings.Builder
	last := ""
	for _, us := range usages {
		if us.Symbol != last {
			fmt.Fprintf(&b, "`%s`:\n", us.Symbol)
			last = us.Symbol
		}
		fmt.Fprintf(&b, "  %s:%d: %s\n", us.File, us.Line, us.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}
