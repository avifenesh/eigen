package main

import (
	"embed"

	"github.com/avifenesh/eigen/internal/gui"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// guiAssets is the built Svelte frontend, embedded into the binary. The
// frontend is built with `vite build` into internal/gui/frontend/dist before
// `go build`/`wails3 build`.
//
//go:embed all:internal/gui/frontend/dist
var guiAssets embed.FS

// buildGUIApp constructs the Wails v3 desktop app + the daemon bridge. The
// caller owns the bridge so it can Shutdown on BOTH the normal and error exit
// paths (no orphan pumps/goroutines/daemon views).
func buildGUIApp() (*application.App, *gui.Bridge) {
	bridge := gui.NewBridge(ensureDaemon)
	app := application.New(application.Options{
		Name:        "eigen",
		Description: "Eigen desktop GUI",
		Services:    []application.Service{application.NewService(bridge)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(guiAssets)},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	bridge.SetApp(app)
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Eigen",
		Width:            1180,
		Height:           760,
		BackgroundColour: application.NewRGB(11, 14, 15), // --bg-base #0B0E0F
		URL:              "/",
	})
	return app, bridge
}
