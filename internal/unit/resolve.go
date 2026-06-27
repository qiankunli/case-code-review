package unit

// GoFuncIDAt parses Go source and returns the unit-id of the function enclosing
// the given 1-indexed line, or ("", false) when the line is outside any function
// or src can't be parsed. Caller resolution uses it to map a grep hit's line to
// the function that contains the call. Go-only by construction (it reuses the
// go/ast span parser); other languages get their own resolver later.
func GoFuncIDAt(path, src string, line int) (string, bool) {
	spans, err := parseGoFuncs(path, src)
	if err != nil {
		return "", false
	}
	for _, s := range spans {
		if line >= s.start && line <= s.end {
			return s.id, true
		}
	}
	return "", false
}
