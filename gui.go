//go:build wails

package main

import (
	"fmt"
	"os"
)

// runGUICmd launches the Eigen desktop GUI (Wails v3 + Svelte). app.Run blocks
// until the window closes. Shutdown runs on BOTH the normal and error paths so
// no pump goroutine, daemon connection, or daemon-side session view is orphaned.
//
// Built only under the `wails` tag (the GUI binary: `-tags 'wails production
// webkit2_41'` / `make gui-desktop`). A bare `go build .` (CI's `make gate`)
// compiles the !wails stub in gui_stub.go instead, so the default build pulls
// in no webkitgtk — matching main, where the GUI is the browser/HTTP shell.
func runGUICmd(_ []string) {
	app, bridge := buildGUIApp()
	err := app.Run()
	bridge.Shutdown()
	if err != nil {
		fmt.Fprintln(os.Stderr, "eigen gui:", err)
		os.Exit(1)
	}
}
