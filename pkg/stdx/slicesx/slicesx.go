// Package slicesx extends the standard slices package with transforms it
// deliberately omits (the Map/Filter family lives in third-party libs; only
// what this codebase actually repeats earns a slot here).
package slicesx

// Uniq returns s without duplicates, keeping the FIRST occurrence and the
// original order — the "seen map" loop this codebase kept re-writing.
func Uniq[T comparable](s []T) []T {
	return UniqBy(s, func(v T) T { return v })
}

// UniqBy returns s without key-duplicates, keeping the first occurrence of
// each key and the original order.
func UniqBy[T any, K comparable](s []T, key func(T) K) []T {
	if s == nil {
		return nil
	}
	seen := make(map[K]bool, len(s))
	out := make([]T, 0, len(s))
	for _, v := range s {
		k := key(v)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
	}
	return out
}
