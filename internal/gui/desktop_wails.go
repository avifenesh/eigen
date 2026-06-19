//go:build wails && (dev || production)

package gui

import (
	"context"
	"sync"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// DesktopAvailable reports whether this binary includes the Wails desktop
// shell. Wails supplies dev/production tags; this repo adds the wails tag so
// normal CLI builds do not require Linux WebKit/GTK development packages.
const DesktopAvailable = true

// RunDesktop starts Eigen as a Wails desktop app. The frontend is the same
// embedded Eigen GUI used by browser-preview mode, but it runs in a real app
// window and talks to the same daemon-backed Service through Wails' native
// bindings/events. The HTTP handler remains available as a browser-preview and
// fallback path.
func RunDesktop(ctx context.Context, svc *Service) error {
	app := &DesktopApp{svc: svc}
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
		Bind: []interface{}{app},
		OnStartup: func(wctx context.Context) {
			app.setContext(wctx)
			_ = ctx // CLI lifecycle root reserved for future desktop services.
		},
		OnShutdown: func(context.Context) { app.closeStream() },
	})
}

// DesktopApp is the Wails-native bridge used by the frontend. It mirrors the
// HTTP API, but avoids forcing the desktop UI through fetch/SSE.
type DesktopApp struct {
	svc *Service

	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	stream *EventStream
}

func (a *DesktopApp) setContext(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
}

func (a *DesktopApp) Health() (Health, error)                 { return a.svc.Health() }
func (a *DesktopApp) Sessions() ([]daemon.SessionInfo, error) { return a.svc.Sessions() }
func (a *DesktopApp) NewSession(dir, model, perm string) (string, error) {
	return a.svc.NewSession(dir, model, perm)
}
func (a *DesktopApp) State(id string) (*daemon.SessionState, error) { return a.svc.State(id) }
func (a *DesktopApp) Input(id, text string) (bool, error)           { return a.svc.Input(id, text) }
func (a *DesktopApp) Approve(id, approval string, allow bool) error {
	return a.svc.Approve(id, approval, allow)
}
func (a *DesktopApp) Interrupt(id string) error { return a.svc.Interrupt(id) }
func (a *DesktopApp) Resend(id string) error    { return a.svc.Resend(id) }
func (a *DesktopApp) Clear(id string) error     { return a.svc.Clear(id) }
func (a *DesktopApp) Remove(id string) error    { return a.svc.Remove(id) }

// Subscribe streams daemon events into the Wails runtime event bus as
// "gui:event". Only one active session stream is needed for the current
// workspace; selecting another session replaces it.
func (a *DesktopApp) Subscribe(id string) error {
	a.closeStream()
	a.mu.Lock()
	base := a.ctx
	a.mu.Unlock()
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	stream, events, err := a.svc.Events(ctx, id)
	if err != nil {
		cancel()
		return err
	}
	a.mu.Lock()
	a.cancel = cancel
	a.stream = stream
	a.mu.Unlock()
	wruntime.EventsEmit(base, "gui:ready", id)
	go func() {
		defer stream.Close()
		for ev := range events {
			wruntime.EventsEmit(base, "gui:event", ev)
		}
	}()
	return nil
}

func (a *DesktopApp) Unsubscribe() {
	a.closeStream()
}

func (a *DesktopApp) closeStream() {
	a.mu.Lock()
	cancel := a.cancel
	stream := a.stream
	a.cancel = nil
	a.stream = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if stream != nil {
		_ = stream.Close()
	}
}
