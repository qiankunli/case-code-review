package language

import (
	"regexp"
	"strings"
)

// extractPyDocstring returns the summary docstring (first paragraph) of `class
// name` / `def name` in src — the string literal immediately after the def
// header. Best-effort line scan (no full Python parse); "" when absent.
func extractPyDocstring(src, name string) string {
	defRe := regexp.MustCompile(`^[ \t]*(?:async[ \t]+)?(?:class|def)[ \t]+` + regexp.QuoteMeta(name) + `\b`)
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if !defRe.MatchString(line) {
			continue
		}
		// Skip to the end of the (possibly multi-line) signature, then to the first
		// non-blank body line.
		j := i
		for j < len(lines) && !strings.HasSuffix(strings.TrimRight(lines[j], " \t"), ":") {
			j++
		}
		for j++; j < len(lines) && strings.TrimSpace(lines[j]) == ""; j++ {
		}
		if j < len(lines) {
			return docstringAt(lines, j)
		}
	}
	return ""
}

// docstringAt reads a triple/single-quoted string starting at lines[start] and
// returns its first paragraph, whitespace-collapsed. "" when the body line isn't
// a string literal.
func docstringAt(lines []string, start int) string {
	body := strings.TrimSpace(lines[start])
	quote := ""
	for _, q := range []string{`"""`, "'''", `"`, `'`} {
		if strings.HasPrefix(body, q) {
			quote = q
			break
		}
	}
	if quote == "" {
		return ""
	}
	rest := strings.TrimPrefix(body, quote)
	if before, _, ok := strings.Cut(rest, quote); ok { // single-line docstring
		return summarizeDoc(before)
	}
	collected := []string{rest}
	for k := start + 1; k < len(lines); k++ {
		if idx := strings.Index(lines[k], quote); idx >= 0 {
			collected = append(collected, lines[k][:idx])
			break
		}
		collected = append(collected, lines[k])
	}
	return summarizeDoc(strings.Join(collected, "\n"))
}

// summarizeDoc trims to the first paragraph and collapses whitespace.
func summarizeDoc(doc string) string {
	doc = strings.TrimSpace(doc)
	if i := strings.Index(doc, "\n\n"); i >= 0 {
		doc = doc[:i]
	}
	return strings.Join(strings.Fields(doc), " ")
}
