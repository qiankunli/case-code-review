// Package uuid provides the one identifier shape this codebase needs — a
// random (v4) UUID string — without pulling a full UUID dependency.
package uuid

import (
	"crypto/rand"
	"fmt"
	"io"
	"time"
)

// V4 returns a random RFC-4122 version-4 UUID. On the (practically
// impossible) failure of the system's entropy source it degrades to a
// timestamp-based fallback rather than panicking — callers use these as
// correlation ids, where a collision is a nuisance and a crash is not
// an acceptable trade.
func V4() string {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 1
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
