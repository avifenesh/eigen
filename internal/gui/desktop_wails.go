//go:build wails && (dev || production)

package gui

import (
	"context"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// DesktopAvailable reports whether this binary includes the Wails desktop
// shell. Wails supplies dev/production tags; this repo adds the wails tag so
// normal CLI builds do not require Linux WebKit/GTK development packages.
const DesktopAvailable = true

// RunDesktop starts Eigen as a Wails desktop app. The frontend is the same
// embedded Eigen GUI used by browser-preview mode, but it runs in a real app
// window and talks to the same daemon-backed Service through Wails' asset
// server/API handler.
func RunDesktop(ctx context.Context, svc *Service) error {
	return wails.Run(&options.App{
		Title:            "Eigen",
		Width:            1320,
		Height:           900,
		MinWidth:         980,
		MinHeight:        680,
		BackgroundColour: options.NewRGBA(8, 12, 13, 255),
		AssetServer: &assetserver.Options{
			Assets:  StaticAssets(),
			Handler: Handler(svc),
		},
		Bind: []interface{}{svc},
		OnStartup: func(context.Context) {
			// The CLI-owned context remains the lifecycle root for future desktop
			// services; the daemon itself is independent and remains running.
			_ = ctx
		},
	})
}
