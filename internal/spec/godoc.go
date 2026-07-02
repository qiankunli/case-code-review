package spec

import (
	"regexp"
	"strings"
)

// extractGoDoc returns the summary doc comment (first paragraph) of a Go symbol —
// the contiguous `//` lines immediately above its declaration. name is the symbol
// part of a symbol-id: a bare `Foo` for a func/type, or `Recv.Method` for a method
// (matched against its receiver). Best-effort line scan; "" when absent.
func extractGoDoc(src, name string) string {
	var declRe *regexp.Regexp
	if recv, method, ok := strings.Cut(name, "."); ok {
		// func (r Recv) Method(  /  func (r *Recv) Method(
		declRe = regexp.MustCompile(`^func \([A-Za-z_]\w* \*?` + regexp.QuoteMeta(recv) + `\) ` + regexp.QuoteMeta(method) + `\b`)
	} else {
		declRe = regexp.MustCompile(`^(?:func|type) ` + regexp.QuoteMeta(name) + `\b`)
	}
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if !declRe.MatchString(line) {
			continue
		}
		var doc []string
		for j := i - 1; j >= 0; j-- { // walk up over the contiguous // comment block
			t := strings.TrimSpace(lines[j])
			if !strings.HasPrefix(t, "//") {
				break
			}
			doc = append([]string{strings.TrimSpace(t[2:])}, doc...)
		}
		if len(doc) == 0 {
			return ""
		}
		return summarizeDoc(strings.Join(doc, "\n")) // reuse: first paragraph, whitespace-collapsed
	}
	return ""
}
