package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qiankunli/case-code-review/internal/config/template"
	"github.com/qiankunli/case-code-review/internal/llm"
	"github.com/qiankunli/case-code-review/internal/session"
	"github.com/qiankunli/case-code-review/internal/stdout"
	"github.com/qiankunli/case-code-review/internal/telemetry"
)

// renderMessages substitutes placeholders into a conversation's messages.
func renderMessages(msgs []template.ChatMessage, repl map[string]string) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		c := m.Content
		for k, v := range repl {
			c = strings.ReplaceAll(c, k, v)
		}
		out = append(out, llm.NewTextMessage(m.Role, c))
	}
	return out
}

// The consistency pass is the session-level aggregation surface: one agentic
// loop over the WHOLE diff, run after per-unit reviews. It exists because a
// contradiction between two files belongs to no single unit's scope — each
// side looks fine alone (measured failure mode: a Dockerfile builder version
// vs go.mod, missed twice on the eval baseline). It only hunts cross-file
// mismatches; single-point findings are the unit loops' job.

const (
	// maxDiffChars bounds the full-diff injection. Oversized sweeps lose
	// precision anyway; the tail is dropped with an explicit marker so the
	// model knows coverage is partial.
	maxDiffChars = 160 * 1024
	// maxContractFileBytes / maxContractFiles bound the contract-surface
	// injection (these files are small by nature; a huge one is not a
	// contract surface anymore).
	maxContractFileBytes = 16 * 1024
	maxContractFiles     = 8
)

// contractSurfaceNames are basenames whose files define the repo's
// cross-cutting expectations — the usual "other side" of a cross-file
// contradiction. Deliberately a small, stable set.
var contractSurfaceNames = map[string]bool{
	"go.mod":             true,
	"package.json":       true,
	"pyproject.toml":     true,
	"Makefile":           true,
	"Dockerfile":         true,
	"docker-compose.yml": true,
	".env.example":       true,
}

// runConsistencyPass executes the sweep and lets its code_comment output flow
// into the shared collector. Runs AFTER runReviewFilters on purpose: the
// filter judges comments against one file's diff hunks, which would wrongly
// kill findings whose evidence lives in another file.
func (a *Agent) runConsistencyPass(ctx context.Context) {
	ct := a.args.Template.ConsistencyTask
	if ct == nil || len(ct.Messages) == 0 {
		return
	}
	var paths []string
	for i := range a.diffs {
		if !a.diffs[i].IsDeleted && a.diffs[i].NewPath != "" {
			paths = append(paths, a.diffs[i].NewPath)
		}
	}
	if len(paths) == 0 {
		return
	}

	messages := renderMessages(ct.Messages, map[string]string{
		"{{diff_all}}":       a.buildFullDiff(),
		"{{contract_files}}": a.buildContractSurface(),
		"{{repo_map}}":       a.repoMap,
	})

	if ct.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(ct.Timeout)*time.Second)
		defer cancel()
	}

	start := time.Now()
	fmt.Fprintf(stdout.Writer(), "[ccr] Consistency sweep across %d changed file(s)\n", len(paths))
	// Session scope covers every changed file so code_comment may anchor on
	// any of them (the loop snaps only out-of-scope paths back).
	sc := session.Scope{ID: "__consistency__", Kind: "file", Type: "consistency", Paths: paths}
	if err := a.runner.RunPerFile(ctx, messages, sc); err != nil {
		fmt.Fprintf(stdout.Writer(), "[ccr] Consistency sweep failed: %v\n", err)
		a.recordWarning("consistency_error", "", err.Error())
	}
	telemetry.Event(ctx, "consistency.done",
		telemetry.AnyToAttr("files", len(paths)),
		telemetry.AnyToAttr("duration.ms", time.Since(start).Milliseconds()))
}

// buildFullDiff concatenates every reviewed file's diff, bounded by
// maxDiffChars with an explicit truncation marker.
func (a *Agent) buildFullDiff() string {
	var sb strings.Builder
	for i := range a.diffs {
		d := &a.diffs[i]
		if d.IsDeleted || d.NewPath == "" {
			continue
		}
		if sb.Len()+len(d.Diff) > maxDiffChars {
			sb.WriteString("\n[... remaining files omitted: diff exceeds the sweep budget — coverage is partial ...]\n")
			break
		}
		fmt.Fprintf(&sb, "--- %s ---\n%s\n", d.NewPath, d.Diff)
	}
	return sb.String()
}

// buildContractSurface reads the repo's contract-surface files (top two
// directory levels — build/ and deploy dirs live shallow), bounded and
// deterministic. Unreadable or oversized files are skipped silently.
func (a *Agent) buildContractSurface() string {
	var found []string
	root := a.args.RepoDir
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			name := d.Name()
			if rel != "." && (strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			// Contract files live shallow; don't sweep the whole tree.
			if strings.Count(rel, "/") >= 2 {
				return filepath.SkipDir
			}
			return nil
		}
		if contractSurfaceNames[d.Name()] || d.Name() == ".env.example" {
			found = append(found, rel)
		}
		return nil
	})
	sort.Strings(found)
	if len(found) > maxContractFiles {
		found = found[:maxContractFiles]
	}
	var sb strings.Builder
	for _, rel := range found {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || len(data) > maxContractFileBytes {
			continue
		}
		fmt.Fprintf(&sb, "== %s ==\n%s\n", rel, strings.TrimRight(string(data), "\n"))
	}
	return sb.String()
}
