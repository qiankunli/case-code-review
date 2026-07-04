package session

import "time"

// Debrief is a unit's terminal record — one line per review Unit written when
// its loop ends. Briefing goes in, debrief comes out: it captures what the run
// knew at that moment and what post-hoc analysis cannot reconstruct (outcome
// vs policy skip, unit formation, briefing degradations), plus a per-unit cost
// rollup so eval never re-aggregates raw llm_response records.
//
// Field groups mirror the metric dashboards (eval/README.md): outcome →
// robustness, formed → granularity, clues/materials/usage → context, the
// aggregated tail → cost.
type Debrief struct {
	// Outcome is the loop's terminal state: llmloop's completed / truncated /
	// timeout / llm_error, or "skipped_policy" when a governor (token guard)
	// decided not to run the loop at all — a deliberate skip, not a failure.
	Outcome string
	// Reason qualifies non-completed outcomes (truncation cause, error, policy).
	Reason string

	// Formed is unit.Formation: why the unit has its shape (func/file/coalesce/
	// chain). Distinguishes the cost governor's coalesced file units from
	// naturally whole-file ones — the granularity dashboard's too-coarse signal.
	Formed     string
	Fragments  int
	Insertions int64
	Deletions  int64

	// Degradations lists briefing content dropped after assembly (token guard:
	// related bodies first, then own source). Budget misses during assembly are
	// per-material entries in Materials instead.
	Degradations []string
	// Clues is the dossier tally on the relation×kind matrix ("caller/spec" -> n).
	Clues map[string]int
	// ClueRefs are the deduped symbol-ids the dossier points at — kept because
	// coverage COUNTS can stay flat while the pointed-at symbols all change
	// (the typed-graph lesson: compare content, not counts).
	ClueRefs []string
	// Materials records each briefing material's fate: "whole <path>",
	// "ranged <path>", "budget_miss <path>", "dropped <label>".
	Materials []string
	// UsageSites is how many pre-grepped use sites the briefing carried.
	UsageSites int

	// Cost rollup — filled by WriteDebrief from the scope's task records.
	Rounds     map[string]int // task type -> LLM rounds
	ToolCalls  map[string]int // tool name -> calls
	Tokens     TokenUsage
	DurationMs int64
}

// WriteDebrief fills the debrief's cost rollup from the scope's recorded
// traffic and persists it as a "debrief" record. The caller supplies only what
// it alone knows (outcome, briefing fate); cost is aggregated here so it can't
// drift from the llm_request/llm_response records it summarizes.
func (sh *SessionHistory) WriteDebrief(sc Scope, d Debrief) {
	ss := sh.GetOrCreateScope(sc)
	d.Rounds, d.ToolCalls, d.Tokens, d.DurationMs = ss.aggregate()
	if p := sh.persist; p != nil {
		p.WriteDebrief(ss, d)
	}
}

// aggregate sums this scope's recorded LLM traffic: rounds and duration per
// task type, tool-call counts, and token usage (cache split kept — real cost
// differs by an order of magnitude).
func (ss *ScopeSession) aggregate() (rounds map[string]int, toolCalls map[string]int, tokens TokenUsage, durationMs int64) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	rounds = make(map[string]int, len(ss.TaskRecords))
	toolCalls = map[string]int{}
	var dur time.Duration
	for taskType, recs := range ss.TaskRecords {
		rounds[string(taskType)] = len(recs)
		for _, r := range recs {
			dur += r.Duration
			if r.Response == nil {
				continue
			}
			for _, tc := range r.Response.ToolCalls {
				toolCalls[tc.Function.Name]++
			}
			if u := r.Response.Usage; u != nil {
				tokens.PromptTokens += u.PromptTokens
				tokens.CompletionTokens += u.CompletionTokens
				tokens.CacheReadTokens += u.CacheReadTokens
				tokens.CacheWriteTokens += u.CacheWriteTokens
			}
		}
	}
	return rounds, toolCalls, tokens, dur.Milliseconds()
}
