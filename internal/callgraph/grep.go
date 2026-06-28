package callgraph

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/qiankunli/case-code-review/internal/gitcmd"
	"github.com/qiankunli/case-code-review/internal/unit"
)

const (
	defaultMaxResults = 8
	grepTimeout       = 10 * time.Second
)

type hit struct {
	file string
	line int
}

// grepGo runs `git grep` over the repo's Go files with the given match args
// (e.g. ["-w", "-e", name] for a word match, or ["-P", "-e", pattern] for a
// regex) and returns up to maxHits file:line hits. Returns nil on any error so
// finders degrade silently.
func grepGo(repoDir string, runner *gitcmd.Runner, matchArgs []string, maxHits int) []hit {
	ctx, cancel := context.WithTimeout(context.Background(), grepTimeout)
	defer cancel()

	args := append([]string{"--no-pager", "grep", "-n", "--no-color"}, matchArgs...)
	args = append(args, "--", "*.go")
	out, err := gitOutput(ctx, repoDir, runner, args)
	if err != nil {
		return nil
	}
	var hits []hit
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3) // file:line:content
		if len(parts) < 3 {
			continue
		}
		n, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		hits = append(hits, hit{file: parts[0], line: n})
		if maxHits > 0 && len(hits) >= maxHits {
			break
		}
	}
	return hits
}

func gitOutput(ctx context.Context, repoDir string, runner *gitcmd.Runner, args []string) ([]byte, error) {
	if runner != nil {
		return runner.Output(ctx, repoDir, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// funcIDAt reads the hit's file (under repoDir) and resolves the line to the
// unit-id of its enclosing function.
func funcIDAt(repoDir string, h hit) (string, bool) {
	src, err := os.ReadFile(filepath.Join(repoDir, h.file))
	if err != nil {
		return "", false
	}
	return unit.GoFuncIDAt(h.file, string(src), h.line)
}
