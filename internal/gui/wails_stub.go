//go:build !wails

package gui

import "fmt"

// Tagless counterparts of wails.go: no Wails import, so `go build
// ./internal/gui/` needs no webkitgtk and guiserver can sit in `make gate`.
// The dialog methods still EXIST (the reflect dispatcher exposes every Bridge
// method by name) but fail closed — the Qt client opens QFileDialog itself and
// passes explicit paths as plain args, so these should never be hit in
// practice.

// wailsHost is empty tagless: there is no app handle because there is no host
// window. Embedded in Bridge (bridge.go) so that file never names a Wails type.
type wailsHost struct{}

// promptForPath is the tagless stub of the native open-file dialog: without a
// host window there is nothing to prompt with. Callers surface this error to
// clients that should have supplied a path argument instead.
func (b *Bridge) promptForPath(string, string, bool) (string, error) {
	return "", fmt.Errorf("native dialog unavailable (no host UI); pass an explicit path")
}
