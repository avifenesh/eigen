//go:build !wails

package main

import (
	"fmt"
	"os"
)

// runGUICmd stub for builds without the `wails` tag. The Wails desktop GUI pulls
// in webkitgtk via cgo, which the default build (and CI's `make gate`) must not
// require — so `eigen gui` is only wired in the GUI binary built with
// `-tags 'wails production'` (see `make gui-desktop`). A bare binary
// tells the user how to get the GUI instead of failing to compile.
func runGUICmd(_ []string) {
	fmt.Fprintln(os.Stderr, "eigen: this build has no desktop GUI — rebuild with `make gui-desktop` "+
		"(go build -tags 'wails production').")
	os.Exit(1)
}
