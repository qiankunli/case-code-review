package main

import (
	"fmt"
	"runtime"
)

// Set via ldflags: -X main.Version=x.y.z -X main.GitCommit=abc123 -X main.BuildDate=2026-01-01T00:00:00Z
var (
	Version   = "dev"
	GitCommit = ""
	BuildDate = ""
)

// versionString is the build identity stamped into session manifests
// ("v1.7.1 (abc123)" / "dev").
func versionString() string {
	if GitCommit != "" {
		return Version + " (" + GitCommit + ")"
	}
	return Version
}

func printVersion() {
	fmt.Printf("case-code-review %s", Version)
	if GitCommit != "" {
		fmt.Printf(" (%s)", GitCommit)
	}
	fmt.Printf(" %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if BuildDate != "" {
		fmt.Printf("built at: %s\n", BuildDate)
	}
	fmt.Println("https://github.com/qiankunli/case-code-review")
}
