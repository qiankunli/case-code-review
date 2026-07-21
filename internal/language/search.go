package language

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// ReferenceScope returns the package directory that soundly bounds references
// to name. Go's unexported identifiers cannot escape their package; other
// languages and exported Go names remain repository-wide.
func ReferenceScope(path, name string) string {
	lang, ok := Detect(path)
	if !ok || lang != Go || name == "" || unicode.IsUpper([]rune(name)[0]) {
		return ""
	}
	return filepath.ToSlash(filepath.Dir(path))
}

// DefinitionSearchPattern is a best-effort PCRE hint for locating definition
// lines before Analyzer verifies each hit. Syntax knowledge stays here so a
// backend migration does not leak grammar branches into codegraph.
func DefinitionSearchPattern(name string) string {
	quoted := regexp.QuoteMeta(name)
	return `(func(\s+|\s*\([^)]*\)\s*)|def\s+|function\s+)` + quoted + `\s*(<[^>]*>|\[[^]]*\])?\s*\(` +
		`|(const|let|var)\s+` + quoted + `\s*=\s*(async\s*)?(\([^)]*\)|[A-Za-z_$][A-Za-z0-9_$]*)\s*=>` +
		`|^\s*((public|private|protected|static|async|readonly|abstract|override)\s+)*` + quoted + `\s*(<[^>]*>)?\s*\([^)]*\)\s*(:[^={]+)?\s*\{`
}

// IsCommentLine reports whether a grep hit is comment-only in its language.
// It intentionally remains line-granular; trailing comments are not removed.
func IsCommentLine(path, text string) bool {
	lang, ok := Detect(path)
	if !ok {
		return false
	}
	trimmed := strings.TrimSpace(text)
	switch lang {
	case Python:
		return strings.HasPrefix(trimmed, "#")
	default:
		return strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*")
	}
}
