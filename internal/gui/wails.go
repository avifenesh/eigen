//go:build wails

package gui

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// This file is the ONLY place in the package allowed to import Wails. All
// host-UI coupling (the app handle, service lifecycle hooks, native dialogs,
// the event-bus emitter) lives here behind the `wails` tag; wails_stub.go
// supplies the tagless counterparts so `go build ./internal/gui/` compiles
// without webkitgtk — the property that lets guiserver join `make gate`.

// wailsHost is the Wails-only slice of Bridge state: the app handle needed to
// reach the dialog manager + current window. Embedded in Bridge so bridge.go
// itself never names a Wails type; tagless builds embed the empty struct from
// wails_stub.go instead.
type wailsHost struct {
	app *application.App
}

// SetApp wires the Wails app for native dialogs (and, via NewWailsEmitter,
// event emission). Called from the bootstrap before Run.
func (b *Bridge) SetApp(app *application.App) { b.app = app }

// ServiceStartup is the Wails v3 service lifecycle hook (optional interface).
// Verified signature at v3.0.0-alpha2.105: (context.Context, ServiceOptions) error.
// The real work lives in the host-agnostic Start() so guiserver shares it.
func (b *Bridge) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	b.Start()
	return nil
}

// ServiceShutdown is the Wails v3 shutdown hook. Tears down every pump + the
// control client so no goroutine, connection, or daemon-side view leaks.
func (b *Bridge) ServiceShutdown() error {
	b.Stop()
	return nil
}

// wailsEmitter adapts the Wails app's event bus to the Emitter seam. Wails v3
// Event.Emit is non-blocking (dispatches via go func), satisfying the Emitter
// contract that pump handlers rely on.
type wailsEmitter struct{ app *application.App }

// NewWailsEmitter wraps a Wails app as the bridge's Emitter. The bootstrap
// calls SetEmitter(NewWailsEmitter(app)) alongside SetApp(app).
func NewWailsEmitter(app *application.App) Emitter { return wailsEmitter{app: app} }

func (e wailsEmitter) Emit(name string, data any) { e.app.Event.Emit(name, data) }

// promptForPath opens the native OS open-file dialog and returns the chosen
// absolute path, or "" if the user cancelled (a cancel is NOT an error — the
// caller keeps its current selection). dirsOnly scopes the dialog to folders
// vs files; startDir seeds the initial location ("" lets the OS decide). It
// needs the wired Wails app to reach the dialog manager; with no app there's
// no window to host a dialog, so it fails closed with "no window".
//
// It attaches to the current window when one is available — Window.Current()
// may be nil (e.g. during startup), in which case we prompt unattached, which
// is acceptable on Linux.
func (b *Bridge) promptForPath(title, startDir string, dirsOnly bool) (string, error) {
	if b.app == nil {
		return "", fmt.Errorf("no window")
	}
	dlg := b.app.Dialog.OpenFile().
		CanChooseDirectories(dirsOnly).
		CanChooseFiles(!dirsOnly).
		SetTitle(title)
	if startDir != "" {
		dlg = dlg.SetDirectory(startDir)
	}
	if win := b.app.Window.Current(); win != nil {
		dlg = dlg.AttachToWindow(win)
	}
	// An empty return is the user cancelling — surface "" without an error.
	return dlg.PromptForSingleSelection()
}
