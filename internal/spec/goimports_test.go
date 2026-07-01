package spec

import "testing"

func TestParseGoImports(t *testing.T) {
	src := `package x

import (
	"github.com/org/framework/mw/trace"
	tr "github.com/org/other/trace"
	_ "embed"
	. "fmt"
	"github.com/org/lib/v2"
)

import "strings"
`
	got := parseGoImports(src)
	want := map[string]string{
		"trace":   "github.com/org/framework/mw/trace", // default = last segment
		"tr":      "github.com/org/other/trace",        // explicit alias
		"lib":     "github.com/org/lib/v2",             // /v2 stripped for the name
		"strings": "strings",                           // single-line form
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["embed"]; ok {
		t.Error("blank import must be skipped")
	}
}

func TestGoPkgName(t *testing.T) {
	cases := map[string]string{
		"github.com/org/framework/mw/trace": "trace",
		"github.com/org/lib/v2":             "lib", // major-version element skipped
		"strings":                           "strings",
		"github.com/org/v2foo":              "v2foo", // not a version element
	}
	for in, want := range cases {
		if got := goPkgName(in); got != want {
			t.Errorf("goPkgName(%q) = %q, want %q", in, got, want)
		}
	}
}
