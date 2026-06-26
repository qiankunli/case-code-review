package unit

import "strings"

// IDSep separates the file path from the symbol in a function unit id.
const IDSep = "::"

// recvSep separates the receiver type from the method name in a symbol.
const recvSep = "."

// FuncID builds the canonical id of a function — the single identity shared by
// three producers that must agree, or spec/case can't be joined to code:
//   - the Splitter, as a func Unit's ID/Symbol;
//   - specgen, as a key in spec.json;
//   - the caller-resolver, as a call-graph node id.
//
// Format: "<relpath>::<symbol>". relpath is repo-relative and slash-separated
// (it doubles as the review address space). symbol is "<Name>" for a free
// function, or "<RecvType>.<Method>" for a method — callers must pass recv
// already normalized (pointer star and generic type params stripped), since
// that normalization is language-specific.
//
//	FuncID("internal/notebook/service.go", "", "GetByName")
//	  -> "internal/notebook/service.go::GetByName"
//	FuncID("internal/notebook/service.go", "NotebookService", "Get")
//	  -> "internal/notebook/service.go::NotebookService.Get"
//
// The format is the cross-repo contract; each language (Go here, Python in
// specgen) implements symbol extraction itself but must emit this exact shape.
func FuncID(relpath, recv, name string) string {
	sym := name
	if recv != "" {
		sym = recv + recvSep + name
	}
	return relpath + IDSep + sym
}

// SplitID returns the file path and symbol of a function unit id. ok is false
// if id is not a "<relpath>::<symbol>" function id (e.g. a file-scope id, which
// is a bare path with no separator).
func SplitID(id string) (relpath, symbol string, ok bool) {
	i := strings.LastIndex(id, IDSep)
	if i < 0 {
		return "", "", false
	}
	return id[:i], id[i+len(IDSep):], true
}
