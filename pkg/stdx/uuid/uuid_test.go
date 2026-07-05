package uuid

import (
	"regexp"
	"testing"
)

func TestV4(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, b := V4(), V4()
	if !re.MatchString(a) || !re.MatchString(b) {
		t.Fatalf("not v4-shaped: %s / %s", a, b)
	}
	if a == b {
		t.Fatal("two V4 calls returned the same id")
	}
}
