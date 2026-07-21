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
	"unicode"

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
	text string // the matched line's content (usage-site rendering; ignored by callers that only resolve)
}

// grepCode runs `git grep` over the repo's function-aware source files with the given match
// args (e.g. ["-w", "-e", name] for a word match, or ["-P", "-e", pattern] for a
// regex) and returns up to maxHits file:line hits. When scopeDir is non-empty the
// grep is restricted to that single package directory (see scopePathspecs).
// Returns nil on any error so finders degrade silently.
func grepCode(repoDir string, runner *gitcmd.Runner, matchArgs []string, maxHits int, scopeDir string) []hit {
	ctx, cancel := context.WithTimeout(context.Background(), grepTimeout)
	defer cancel()

	args := append([]string{"--no-pager", "grep", "-n", "--no-color"}, matchArgs...)
	// funcIDAt dispatches per hit file by extension.
	args = append(args, "--")
	args = append(args, scopePathspecs(scopeDir)...)
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
		hits = append(hits, hit{file: parts[0], line: n, text: parts[2]})
		if maxHits > 0 && len(hits) >= maxHits {
			break
		}
	}
	return hits
}

// scopePathspecs restricts the grep to one package directory when scopeDir is
// set — an unexported Go symbol's callers/defs can only live in its own package,
// so this kills cross-package same-name false positives. :(glob) keeps * from
// crossing '/', so only the directory's direct files match (Go packages are
// non-recursive). Empty scopeDir greps the whole repo (the default).
func scopePathspecs(scopeDir string) []string {
	switch scopeDir {
	case "":
		return []string{"*.go", "*.py", "*.ts", "*.tsx", "*.js", "*.jsx", "*.mjs", "*.cjs"}
	case ".":
		return []string{":(glob)*.go", ":(glob)*.py", ":(glob)*.ts", ":(glob)*.tsx", ":(glob)*.js", ":(glob)*.jsx", ":(glob)*.mjs", ":(glob)*.cjs"}
	default:
		return []string{":(glob)" + scopeDir + "/*.go", ":(glob)" + scopeDir + "/*.py", ":(glob)" + scopeDir + "/*.ts", ":(glob)" + scopeDir + "/*.tsx", ":(glob)" + scopeDir + "/*.js", ":(glob)" + scopeDir + "/*.jsx", ":(glob)" + scopeDir + "/*.mjs", ":(glob)" + scopeDir + "/*.cjs"}
	}
}

// unexportedScope returns the package directory to scope a grep to when name is
// an UNEXPORTED Go symbol (first rune not an uppercase letter), whose callers /
// definitions Go's visibility rules confine to that one directory. Returns ""
// (whole-repo) for exported names, Python paths, or empty input — scoping an
// unexported symbol is sound (there are no out-of-package references to miss).
func unexportedScope(goPath, name string) string {
	if name == "" || !strings.HasSuffix(goPath, ".go") {
		return ""
	}
	if unicode.IsUpper([]rune(name)[0]) {
		return "" // exported — callable from any package
	}
	// ToSlash: git pathspecs always use '/', but filepath.Dir yields '\' on
	// Windows — scopePathspecs would otherwise emit a mixed-separator pattern.
	return filepath.ToSlash(filepath.Dir(goPath))
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
// symbol-id of its enclosing function — dispatched by extension.
func funcIDAt(repoDir string, h hit) (string, bool) {
	src, err := os.ReadFile(filepath.Join(repoDir, h.file))
	if err != nil {
		return "", false
	}
	return unit.FuncIDAtInRepo(repoDir, h.file, string(src), h.line)
}
