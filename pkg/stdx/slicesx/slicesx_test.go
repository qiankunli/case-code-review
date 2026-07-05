package slicesx

import (
	"reflect"
	"testing"
)

func TestUniq(t *testing.T) {
	got := Uniq([]string{"a", "b", "a", "c", "b"})
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("Uniq = %v", got)
	}
	if Uniq[string](nil) != nil {
		t.Fatal("nil in, nil out")
	}
}

func TestUniqBy(t *testing.T) {
	type kv struct{ k, v string }
	got := UniqBy([]kv{{"a", "1"}, {"b", "2"}, {"a", "3"}}, func(e kv) string { return e.k })
	// First occurrence wins, order preserved.
	if !reflect.DeepEqual(got, []kv{{"a", "1"}, {"b", "2"}}) {
		t.Fatalf("UniqBy = %v", got)
	}
}
