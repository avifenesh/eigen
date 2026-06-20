//go:build !smoke

package main

import (
	"fmt"
	"os"
)

// Production builds do not contain app-smoke/tui-smoke behavior. If someone
// tries those hidden test hooks in a release binary, fail explicitly instead of
// silently succeeding or falling through into a real agent task.
func runSmokeCommand(arg string) bool {
	if arg != "app-smoke" && arg != "tui-smoke" {
		return false
	}
	fmt.Fprintln(os.Stderr, "eigen: smoke commands require a smoke-tagged test helper")
	os.Exit(2)
	return true
}

func runTestSmokeCommand(string) bool { return false }
