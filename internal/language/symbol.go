package language

import "strings"

// SymbolIDSep separates the repository-relative path from the language symbol.
const SymbolIDSep = "::"

// SymbolID builds ccr's canonical source symbol identity.
func SymbolID(relpath, owner, name string) string {
	symbol := name
	if owner != "" {
		symbol = owner + "." + name
	}
	return relpath + SymbolIDSep + symbol
}

// SplitSymbolID splits a canonical source symbol identity.
func SplitSymbolID(id string) (relpath, symbol string, ok bool) {
	i := strings.LastIndex(id, SymbolIDSep)
	if i < 0 {
		return "", "", false
	}
	return id[:i], id[i+len(SymbolIDSep):], true
}

// SymbolName returns the language-level symbol part of a canonical symbol-id.
func SymbolName(id string) (string, bool) {
	_, symbol, ok := SplitSymbolID(id)
	return symbol, ok
}

// BareName returns the final member of a language symbol (Svc.Get -> Get).
func BareName(symbol string) string {
	if i := strings.LastIndex(symbol, "."); i >= 0 {
		return symbol[i+1:]
	}
	return symbol
}

// BareSymbolName returns the final member name from a canonical symbol-id.
func BareSymbolName(id string) string {
	symbol, ok := SymbolName(id)
	if !ok {
		return ""
	}
	return BareName(symbol)
}

// EnclosingSymbolID returns the immediate owner of a nested symbol.
func EnclosingSymbolID(id string) (string, bool) {
	path, symbol, ok := SplitSymbolID(id)
	if !ok {
		return "", false
	}
	i := strings.LastIndex(symbol, ".")
	if i < 0 {
		return "", false
	}
	return path + SymbolIDSep + symbol[:i], true
}

// SymbolHasName reports whether id names the requested bare member.
func SymbolHasName(id, name string) bool {
	return BareSymbolName(id) == name
}
