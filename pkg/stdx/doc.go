// Package stdx is the incubating home of qiankunli-project-wide Go utilities —
// "stdlib extensions". The name IS the admission rule: something enters only
// when the standard library doesn't offer it (a hand-rolled max, a slices.Clone
// re-implementation, or a strconv wrapper does NOT belong here — use stdlib).
// Subpackages mirror stdlib naming (slicesx, uuid, …) so call sites read like
// the standard library they extend.
//
// Incubating inside ccr for now; once it stabilizes it moves to its own module
// (github.com/qiankunli/stdx) so hostel and other Go projects import it without
// dragging in a code-review tool. Keep entries dependency-free to make that
// extraction a pure path rename.
package stdx
