package llmloop

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qiankunli/case-code-review/internal/config/template"
	"github.com/qiankunli/case-code-review/internal/diff"
	"github.com/qiankunli/case-code-review/internal/llm"
	"github.com/qiankunli/case-code-review/internal/model"
	"github.com/qiankunli/case-code-review/internal/msg"
	"github.com/qiankunli/case-code-review/internal/session"
	"github.com/qiankunli/case-code-review/internal/stdout"
	"github.com/qiankunli/case-code-review/internal/telemetry"
	"github.com/qiankunli/case-code-review/internal/tool"
)

const (
	// wrapUpTimeReserve: when less than this remains on the ctx deadline,
	// stop investigating and force a verdict. Sized to fit wrapUpMaxRounds
	// typical LLM rounds — smaller and the wrap-up itself gets cut off.
	wrapUpTimeReserve = 90 * time.Second
	// wrapUpRoundReserve: force the verdict when this many tool rounds
	// remain (counting the current one), so round-budget exhaustion ends in
	// an explicit task_done instead of a silent partial.
	wrapUpRoundReserve = 2
	// wrapUpMaxRounds caps the rounds granted once a deadline wrap-up is
	// issued: what's left of the budget goes to concluding, not digging.
	wrapUpMaxRounds = 2
)

// wrapUpPrompt forces an explicit verdict when a budget is nearly gone. It
// must demand task_done: chains ending without it are recorded as
// unit_incomplete and their silence must not read as a clean review.
const wrapUpPrompt = "BUDGET NEARLY EXHAUSTED — stop investigating now. Based only on the evidence you already gathered: call code_comment for each issue you are confident about, then call task_done. If you found no issues in what you managed to review, call task_done and state explicitly which parts you reviewed. Do not call any other tools."

// Outcome is how a unit's main loop terminated — the terminal state a debrief
// records. RunPerFile computes it anyway (completed / truncation reason /
// deadline / LLM error); returning it saves callers re-deriving the ending from
// warnings, where two units of one file would collide on the path key.
type Outcome struct {
	State  string // OutcomeCompleted | OutcomeTruncated | OutcomeTimeout | OutcomeLLMError
	Reason string // truncation reason or error text; "" when completed
}

const (
	OutcomeCompleted = "completed" // explicit task_done
	OutcomeTruncated = "truncated" // ended without task_done (rounds / empty rounds / compression)
	OutcomeTimeout   = "timeout"   // ctx deadline exceeded
	OutcomeLLMError  = "llm_error" // a completion call failed terminally
)

// Deps bundles all per-call dependencies the Runner needs. Both
// internal/agent (diff review) and internal/scan (full-file scan) build a
// Deps from their own state and hand it to NewRunner.
type Deps struct {
	LLMClient         llm.LLMClient
	Model             string
	Template          template.Template
	Tools             *tool.Registry
	MainToolDefs      []llm.ToolDef
	CommentCollector  *tool.CommentCollector
	CommentWorkerPool *CommentWorkerPool
	Session           *session.SessionHistory
	// RelocationEnabled gates the LLM re-location sub-call (ablation feature gate).
	// When false, an unresolved comment keeps its model-reported line (no extra LLM).
	RelocationEnabled bool
	// FileDedupEnabled gates file_read result deduplication (the file_dedup
	// feature gate): a later read covering an earlier one stubs the earlier
	// copy in place, so the model pays for file content once.
	FileDedupEnabled bool
	// FileEvictEnabled gates eviction of File messages under token pressure
	// (the file_evict feature gate): before paying for an LLM compression
	// pass, shed the re-derivable slice of the context — file content the
	// model can always read again.
	FileEvictEnabled bool
	// DiffLookup is consulted by the code_comment tool path to resolve
	// line numbers against the file's diff (or against full file content
	// in scan mode — scan adapters return a synthetic Diff whose
	// NewFileContent is the whole file and Diff is empty).
	DiffLookup func(path string) *model.Diff
}

// Runner is a per-session (across files) executor of the LLM tool-use
// loop. Token counters, warnings, and the optional background compression
// job are aggregated across every RunPerFile call.
type Runner struct {
	deps                  Deps
	totalInputTokens      int64 // atomically updated
	totalOutputTokens     int64
	totalCacheReadTokens  int64
	totalCacheWriteTokens int64
	warningsMu            sync.Mutex
	warnings              []AgentWarning
	toolCallsMu           sync.Mutex
	toolCalls             map[string]int64
	modelsMu              sync.Mutex
	models                map[string]int // routing alias -> #responses it served this run (dedup by key)
	compressionMu         sync.Mutex
	pendingJob            *compressionJob
}

// NewRunner returns a Runner bound to the given dependencies.
func NewRunner(deps Deps) *Runner {
	return &Runner{deps: deps}
}

// TotalInputTokens returns the accumulated input/prompt tokens from all LLM calls.
func (r *Runner) TotalInputTokens() int64 { return atomic.LoadInt64(&r.totalInputTokens) }

// TotalOutputTokens returns the accumulated completion tokens from all LLM calls.
func (r *Runner) TotalOutputTokens() int64 { return atomic.LoadInt64(&r.totalOutputTokens) }

// TotalCacheReadTokens returns the accumulated cache read tokens.
func (r *Runner) TotalCacheReadTokens() int64 { return atomic.LoadInt64(&r.totalCacheReadTokens) }

// TotalCacheWriteTokens returns the accumulated cache write tokens.
func (r *Runner) TotalCacheWriteTokens() int64 { return atomic.LoadInt64(&r.totalCacheWriteTokens) }

// TotalTokensUsed returns input + output.
func (r *Runner) TotalTokensUsed() int64 {
	return r.TotalInputTokens() + r.TotalOutputTokens()
}

// Warnings returns a copy of the accumulated warnings.
func (r *Runner) Warnings() []AgentWarning {
	r.warningsMu.Lock()
	defer r.warningsMu.Unlock()
	out := make([]AgentWarning, len(r.warnings))
	copy(out, r.warnings)
	return out
}

// RecordWarning adds a non-fatal warning.
func (r *Runner) RecordWarning(warningType, file, message string) {
	r.warningsMu.Lock()
	r.warnings = append(r.warnings, AgentWarning{
		File:    file,
		Message: message,
		Type:    warningType,
	})
	r.warningsMu.Unlock()
}

// ToolCalls returns a snapshot of the per-tool call counts.
func (r *Runner) ToolCalls() map[string]int64 {
	r.toolCallsMu.Lock()
	defer r.toolCallsMu.Unlock()
	out := make(map[string]int64, len(r.toolCalls))
	for k, v := range r.toolCalls {
		out[k] = v
	}
	return out
}

func (r *Runner) recordToolCall(name string) {
	r.toolCallsMu.Lock()
	if r.toolCalls == nil {
		r.toolCalls = make(map[string]int64)
	}
	r.toolCalls[name]++
	r.toolCallsMu.Unlock()
}

// recordModel counts one response served by a routing alias. Empty alias
// (single-model / non-routing config) is ignored — there's no alias to report.
func (r *Runner) recordModel(alias string) {
	if alias == "" {
		return
	}
	r.modelsMu.Lock()
	if r.models == nil {
		r.models = make(map[string]int)
	}
	r.models[alias]++
	r.modelsMu.Unlock()
}

// ModelsUsed returns the run's model identity: each routing alias that served a
// response mapped to how many it served (deduped by key). Run-level — present
// even when no finding was produced. Empty for a single-model (non-routing) run.
func (r *Runner) ModelsUsed() map[string]int {
	r.modelsMu.Lock()
	defer r.modelsMu.Unlock()
	out := make(map[string]int, len(r.models))
	for alias, n := range r.models {
		out[alias] = n
	}
	return out
}

// RecordUsage adds the prompt/completion/cache tokens reported by an LLM
// response to the runner's aggregate counters. Used by callers (plan phase
// in agent / future scan phases) that perform their own LLM calls outside
// RunPerFile.
func (r *Runner) RecordUsage(u *llm.UsageInfo) {
	if u == nil {
		return
	}
	atomic.AddInt64(&r.totalInputTokens, u.PromptTokens)
	atomic.AddInt64(&r.totalOutputTokens, u.CompletionTokens)
	atomic.AddInt64(&r.totalCacheReadTokens, u.CacheReadTokens)
	atomic.AddInt64(&r.totalCacheWriteTokens, u.CacheWriteTokens)
}

// CollectPendingComments awaits any async comment-processing workers and
// returns the aggregated comments from the collector. Safe to call once
// per session at the end.
func (r *Runner) CollectPendingComments() []model.LlmComment {
	if r.deps.CommentWorkerPool != nil {
		r.deps.CommentWorkerPool.Await()
	}
	return r.deps.CommentCollector.Comments()
}

// RunPerFile drives the main LLM conversation loop for a single file.
// It sends messages with the configured tool definitions, executes any
// tool calls returned by the model, and collects review comments until
// task_done is called or limits are reached. Token usage and warnings
// are aggregated on the Runner across all files.
//
// The conversation's currency is the review-domain []msg.Msg; the wire form
// (llm.Message) exists only at the msg.Lower call sites — the API request,
// the session record, and the compression prompt (see docs/message-model.md).
//
// Truncation discipline: a chain that ends WITHOUT task_done is a partial
// verdict, and silence would read as "clean" downstream (measured on real
// trajectories: the timed-out chains were exactly the big-PR blind spots).
// Two guards enforce an explicit ending: a wrap-up turn is injected when the
// time or round budget is nearly exhausted (forcing a verdict while there is
// still budget to say it), and any chain that still ends without task_done
// records a unit_incomplete warning instead of returning silently.
func (r *Runner) RunPerFile(ctx context.Context, messages []msg.Msg, sc session.Scope) (Outcome, error) {
	newPath := sc.Path() // representative path for logging / code_comment anchoring
	toolReqCount := r.deps.Template.MaxToolRequestTimes
	const maxConsecutiveEmptyRounds = 3
	consecutiveEmptyRounds := 0
	wrapUpIssued := false
	completed := false
	truncateReason := ""

	for toolReqCount > 0 {
		select {
		case <-ctx.Done():
			r.RecordWarning("unit_incomplete", newPath,
				"review ended without task_done (deadline exceeded); verdict is partial — do not read as clean")
			return Outcome{State: OutcomeTimeout, Reason: "deadline exceeded"}, ctx.Err()
		default:
		}

		if !wrapUpIssued {
			if dl, ok := ctx.Deadline(); ok && time.Until(dl) < wrapUpTimeReserve {
				wrapUpIssued = true
				// Cap the remaining rounds: the point of wrap-up is to spend
				// what's left of the deadline on a verdict, not on more digging.
				if toolReqCount > wrapUpMaxRounds {
					toolReqCount = wrapUpMaxRounds
				}
				messages = append(messages, msg.Text("user", wrapUpPrompt))
				fmt.Fprintf(stdout.Writer(), "[ccr] Budget nearly exhausted for %s — forcing wrap-up (deadline)\n", newPath)
			} else if toolReqCount == wrapUpRoundReserve {
				wrapUpIssued = true
				messages = append(messages, msg.Text("user", wrapUpPrompt))
				fmt.Fprintf(stdout.Writer(), "[ccr] Budget nearly exhausted for %s — forcing wrap-up (rounds)\n", newPath)
			}
		}

		toolReqCount--

		// Lower once per round: the session record and the API call see the same
		// wire form the model does.
		wire := msg.Lower(messages)
		fs := r.deps.Session.GetOrCreateScope(sc)
		rec := fs.AppendTaskRecord(session.MainTask, wire)
		startTime := time.Now()

		resp, err := r.deps.LLMClient.CompletionsWithCtx(ctx, llm.ChatRequest{
			Model:     r.deps.Model,
			Messages:  wire,
			Tools:     r.deps.MainToolDefs,
			MaxTokens: r.deps.Template.MaxTokens,
		})
		duration := time.Since(startTime)
		if err != nil {
			rec.SetError(err, duration)
			telemetry.RecordLLMRequest(ctx, r.deps.Model, duration, 0, "error")
			// A deadline hit mid-call surfaces as an LLM error; report it as the
			// timeout it is so debriefs don't misclassify slow units as API failures.
			if ctx.Err() != nil {
				return Outcome{State: OutcomeTimeout, Reason: "deadline exceeded"}, fmt.Errorf("LLM completion error: %w", err)
			}
			return Outcome{State: OutcomeLLMError, Reason: err.Error()}, fmt.Errorf("LLM completion error: %w", err)
		}
		rec.SetResponse(resp, duration)
		r.recordModel(resp.Alias) // run-level model identity; counts every response, not just ones with findings
		totalTokens := int64(0)
		if resp.Usage != nil {
			totalTokens = resp.Usage.TotalTokens
			atomic.AddInt64(&r.totalInputTokens, resp.Usage.PromptTokens)
			atomic.AddInt64(&r.totalOutputTokens, resp.Usage.CompletionTokens)
			atomic.AddInt64(&r.totalCacheReadTokens, resp.Usage.CacheReadTokens)
			atomic.AddInt64(&r.totalCacheWriteTokens, resp.Usage.CacheWriteTokens)
		}
		telemetry.RecordLLMRequest(ctx, r.deps.Model, duration, totalTokens, "ok")

		content := resp.Content()
		calls := resp.ToolCalls()

		if len(calls) == 0 {
			fmt.Fprintf(stdout.Writer(), "[ccr] No tool calls parsed for %s, retrying...\n", newPath)
			messages = append(messages, msg.Text("user", "You did not successfully call any tools. Please try again or use task_done if finished."))
			if content != "" {
				messages = append(messages[:len(messages)-1], msg.Text("assistant", content), messages[len(messages)-1])
			}
			continue
		}

		var results []tool.ToolCallResult
		taskCompleted := false
		hasValidResult := false

		for _, call := range calls {
			cp := r.executeToolCall(ctx, sc, call, rec, resp.Alias)
			if cp.Completed {
				results = append(results, tool.ToolCallResult{
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Result:     "Task completed successfully.",
				})
				taskCompleted = true
			} else if cp.Data != "" {
				results = append(results, tool.ToolCallResult{
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Result:     cp.Data,
				})
				hasValidResult = true
			} else {
				results = append(results, tool.ToolCallResult{
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Result:     "Error: Tool execution returned no result.",
				})
			}
		}

		if taskCompleted {
			completed = true
			break
		}
		if !hasValidResult {
			consecutiveEmptyRounds++
			if consecutiveEmptyRounds >= maxConsecutiveEmptyRounds {
				fmt.Fprintf(stdout.Writer(), "[ccr] Too many empty retries for %s, stopping.\n", newPath)
				truncateReason = "consecutive empty tool rounds"
				break
			}
			fmt.Fprintf(stdout.Writer(), "[ccr] No valid tool results for %s, retrying...\n", newPath)
		} else {
			consecutiveEmptyRounds = 0
		}

		succeed := r.addNextMessage(ctx, content, calls, results, &messages, sc)
		if !succeed {
			fmt.Fprintf(stdout.Writer(), "[ccr] Context compression exceeded threshold for %s, stopping.\n", newPath)
			truncateReason = "context compression limit"
			break
		}
	}

	if !completed {
		if truncateReason == "" {
			truncateReason = "tool-round budget exhausted"
			fmt.Fprintf(stdout.Writer(), "[ccr] Max tool requests reached for %s.\n", newPath)
		}
		r.RecordWarning("unit_incomplete", newPath,
			"review ended without task_done ("+truncateReason+"); verdict is partial — do not read as clean")
		return Outcome{State: OutcomeTruncated, Reason: truncateReason}, nil
	}
	return Outcome{State: OutcomeCompleted}, nil
}

// executeToolCall dispatches a single tool call from the LLM response and
// records the result in session history. code_comment handling includes
// optional async dispatch through CommentWorkerPool plus line-number
// resolution / re-location.
// alias is the routing alias of the model that produced this tool call's response;
// it is stamped onto any comments parsed here so multi-model output can be compared.
func (r *Runner) executeToolCall(ctx context.Context, sc session.Scope, call llm.ToolCall, rec *session.TaskRecord, alias string) tool.TaskCheckpoint {
	newPath := sc.Path() // representative path; code_comment anchors to it
	t := tool.OfName(call.Function.Name)
	if !t.IsKnown() {
		return tool.Of(tool.NotAvailableMsg)
	}

	if t == tool.TaskDone {
		return tool.Complete()
	}

	p := lookupTool(r.deps.Tools, t)
	if p == nil {
		return tool.Of(tool.NotAvailableMsg)
	}

	r.recordToolCall(t.Name())

	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return tool.Of(fmt.Sprintf("Error parsing tool arguments for %s: %v", t.Name(), err))
	}

	// Snap code_comment's path back into the scope when the model hallucinated
	// one — but keep any path that IS a scope member: a multi-file (call-chain)
	// unit legitimately comments on members beyond the representative first
	// path, and re-anchoring those pins the finding on the wrong file.
	if t == tool.CodeComment && newPath != "" {
		if p, _ := args["path"].(string); p == "" || !slices.Contains(sc.Paths, p) {
			args["path"] = newPath
		}
	}

	startTime := time.Now()

	if t == tool.CodeComment {
		telemetry.PrintToolCallStarted(t.Name(), args)

		comments, errMsg := tool.ParseComments(args)
		if errMsg != "" {
			telemetry.RecordToolCall(ctx, t.Name(), time.Since(startTime), false)
			return tool.Of(errMsg)
		}
		// Attribute each finding to the model that produced it (for multi-model compare).
		for i := range comments {
			comments[i].Alias = alias
		}

		resolveAndCollect := func(rctx context.Context) {
			for i := range comments {
				cm := &comments[i]
				var d *model.Diff
				if r.deps.DiffLookup != nil {
					d = r.deps.DiffLookup(cm.Path)
				}
				if d != nil {
					if !diff.ResolveComment(cm, d) && r.deps.RelocationEnabled && r.deps.Template.ReLocationTask != nil {
						rlStart := time.Now()
						_, resp, msgs := diff.ReLocateComment(rctx, cm, d, r.deps.LLMClient, r.deps.Template.ReLocationTask, r.deps.Model, r.deps.Template.MaxTokens)
						if msgs != nil {
							// Re-location happens inside this Unit's review loop, so it
							// records under the same scope (not the comment's file path).
							fs := r.deps.Session.GetOrCreateScope(sc)
							rlRec := fs.AppendTaskRecord(session.ReLocationTask, msgs)
							if resp != nil {
								rlRec.SetResponse(resp, time.Since(rlStart))
								if resp.Usage != nil {
									atomic.AddInt64(&r.totalInputTokens, resp.Usage.PromptTokens)
									atomic.AddInt64(&r.totalOutputTokens, resp.Usage.CompletionTokens)
									atomic.AddInt64(&r.totalCacheReadTokens, resp.Usage.CacheReadTokens)
									atomic.AddInt64(&r.totalCacheWriteTokens, resp.Usage.CacheWriteTokens)
								}
							} else {
								rlRec.SetError(fmt.Errorf("re-location LLM call failed"), time.Since(rlStart))
							}
						}
					}
				}
				r.deps.CommentCollector.Add(*cm)
			}
		}

		if r.deps.CommentWorkerPool != nil {
			if rec != nil {
				rec.AddToolResult(t.Name(), call.Function.Arguments, "(async)")
			}
			pool := r.deps.CommentWorkerPool
			asyncCtx := context.WithoutCancel(ctx)
			toolName := t.Name()
			pool.Submit(func() ([]model.LlmComment, error) {
				resolveAndCollect(asyncCtx)
				telemetry.PrintToolCallFinished(toolName, time.Since(startTime))
				return []model.LlmComment{}, nil
			})
			telemetry.RecordToolCall(asyncCtx, toolName, time.Since(startTime), true)
			return tool.Of(tool.CommentSucceed)
		}

		resolveAndCollect(ctx)
		dur := time.Since(startTime)
		telemetry.RecordToolCall(ctx, t.Name(), dur, true)
		telemetry.PrintToolCallFinished(t.Name(), dur)
		if rec != nil {
			rec.AddToolResult(t.Name(), call.Function.Arguments, tool.CommentSucceed)
		}
		return tool.Of(tool.CommentSucceed)
	}

	// Synchronous path for all other tools
	telemetry.PrintToolCallStarted(t.Name(), args)
	result, err := p.Execute(ctx, args)
	dur := time.Since(startTime)
	ok := err == nil
	telemetry.RecordToolCall(ctx, t.Name(), dur, ok)

	if err != nil {
		telemetry.PrintToolCallError(t.Name(), err)
		return tool.Of(fmt.Sprintf("Error executing tool %s: %v", t.Name(), err))
	}
	telemetry.PrintToolCallFinished(t.Name(), dur)
	if rec != nil {
		rec.AddToolResult(t.Name(), call.Function.Arguments, result)
	}
	return tool.Of(result)
}

// addNextMessage extends the conversation with the assistant message and
// tool responses, applying three-zone compression at the soft (60%) and
// warning (80%) MaxTokens thresholds. Returns false when even after
// synchronous compression the conversation is still over the warning
// threshold — caller should stop the loop in that case.
func (r *Runner) addNextMessage(ctx context.Context, assistantContent string, toolCalls []llm.ToolCall, results []tool.ToolCallResult, messages *[]msg.Msg, sc session.Scope) bool {
	maxAllowed := r.deps.Template.MaxTokens
	softLimit := int(float64(maxAllowed) * tokenSoftThreshold)
	warnLimit := int(float64(maxAllowed) * tokenWarningThreshold)

	r.tryApplyPendingCompression(messages)

	tokenCount := countMsgTokens(*messages)

	// Over the warning threshold: shed re-derivable file content first (free,
	// deterministic), and only summarize if that wasn't enough.
	if tokenCount > warnLimit && r.deps.FileEvictEnabled {
		if n := evictFiles(*messages, warnLimit); n > 0 {
			telemetry.Event(ctx, "context.file_evict",
				telemetry.AnyToAttr("file.path", sc.Path()),
				telemetry.AnyToAttr("evicted", n))
		}
		tokenCount = countMsgTokens(*messages)
	}
	if tokenCount > warnLimit {
		r.cancelPendingCompression()
		*messages, _ = r.runCompression(ctx, *messages, sc)
		tokenCount = countMsgTokens(*messages)
	}

	if tokenCount > softLimit && r.pendingJob == nil {
		r.triggerAsyncCompression(ctx, *messages, sc)
	}

	if len(toolCalls) > 0 {
		*messages = append(*messages, msg.Raw{M: llm.NewToolCallMessage(assistantContent, toolCalls)})
	} else if assistantContent != "" {
		*messages = append(*messages, msg.Text("assistant", assistantContent))
	}

	for _, rs := range results {
		var m msg.Msg = msg.Raw{M: llm.NewToolResultMessage(rs.ToolCallID, rs.Result)}
		// file_read results carry a path+range identity — keep it (typed File)
		// so covered re-reads can be deduplicated below.
		if f, ok := msg.FileFromToolResult(rs.Name, rs.ToolCallID, rs.Result); ok {
			m = f
		}
		*messages = append(*messages, m)
	}
	if r.deps.FileDedupEnabled {
		if n := msg.DedupFiles(*messages); n > 0 {
			telemetry.Event(ctx, "context.file_dedup",
				telemetry.AnyToAttr("file.path", sc.Path()),
				telemetry.AnyToAttr("stubbed", n))
		}
	}

	finalCount := countMsgTokens(*messages)
	if finalCount > warnLimit && r.deps.FileEvictEnabled {
		evictFiles(*messages, warnLimit)
		finalCount = countMsgTokens(*messages)
	}
	if finalCount > warnLimit {
		r.cancelPendingCompression()
		*messages, _ = r.runCompression(ctx, *messages, sc)
	}

	return countMsgTokens(*messages) < warnLimit
}

// lookupTool returns the provider for a given tool from the registry, or
// nil when not registered.
func lookupTool(reg *tool.Registry, t tool.Tool) tool.Provider {
	p, ok := reg.Get(t.Name())
	if !ok {
		return nil
	}
	return p
}
