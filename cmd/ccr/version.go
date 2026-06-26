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
