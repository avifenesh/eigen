package main

import (
	"fmt"
	"os"
)

// runGUICmd launches the Eigen desktop GUI (Wails v3 + Svelte). app.Run blocks
// until the window closes. Shutdown runs on BOTH the normal and error paths so
// no pump goroutine, daemon connection, or daemon-side session view is orphaned.
func runGUICmd(_ []string) {
	app, bridge := buildGUIApp()
	err := app.Run()
	bridge.Shutdown()
	if err != nil {
		fmt.Fprintln(os.Stderr, "eigen gui:", err)
		os.Exit(1)
	}
}
