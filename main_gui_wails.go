//go:build wails

package main

import (
	"context"
	"embed"
	"os"

	"github.com/avifenesh/eigen/internal/feed"
	"github.com/avifenesh/eigen/internal/gui"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/session"
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
	bridge := gui.NewBridge(ensureDaemon, guiSuggester(), guiProjectDirs)
	app := application.New(application.Options{
		Name:        "eigen",
		Description: "Eigen desktop GUI",
		Services:    []application.Service{application.NewService(bridge)},
		Assets:      application.AssetOptions{Handler: gui.TasksAPIHandler(application.AssetFileServerFS(guiAssets))},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	bridge.SetApp(app)
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Eigen",
		// Tighter default than the old 1180×760 — that opened too wide, leaving
		// the rail + (now-collapsed) dock framing a self-capped content column
		// with big empty gutters. 1040×720 fills better and reads denser; a
		// MinWidth/MinHeight keeps the rail+composer layout from breaking when
		// dragged small.
		Width:            1040,
		Height:           720,
		MinWidth:         720,
		MinHeight:        520,
		BackgroundColour: application.NewRGB(11, 14, 15), // --bg-base #0B0E0F
		URL:              "/",
	})
	return app, bridge
}

// guiSuggester adapts a suggestion model into the proactive feed's Suggester.
// Mirrors the TUI: EIGEN_SUGGEST_MODEL, else glm-5.2 (1M-ctx flagship GLM with
// web_search included, mostly-idle quota), else the usual small model; nil only
// if none can be built — the feed then yields just git/github/memory signals
// (no LLM ideas). glm-5.2's web_search defaults to "auto", so the suggester can
// ground ideas in live web data, not just the local snapshot.
func guiSuggester() feed.Suggester {
	prov := guiSuggestProvider()
	if prov == nil {
		return nil
	}
	return func(ctx context.Context, system, prompt string) (string, error) {
		resp, err := prov.Complete(ctx, llm.Request{
			System:   system,
			Messages: []llm.Message{{Role: llm.RoleUser, Text: prompt}},
		})
		if err != nil {
			return "", err
		}
		return resp.Text, nil
	}
}

func guiSuggestProvider() llm.Provider {
	if id := os.Getenv("EIGEN_SUGGEST_MODEL"); id != "" {
		if p, err := llm.New("", id); err == nil {
			return p
		}
	}
	// Prefer glm-5.2 (1M-ctx, web_search included) but wrap it in a fallback to
	// the small model: if GLM rejects on quota/billing (e.g. a drained z.ai
	// balance), the fallback carries ideas AND GLM is frozen for the rest of the
	// day so the next scans don't keep hitting a dead account. See llm.NewFallback.
	small := smallProvider(nil)
	if llm.ProviderAvailable("glm") {
		if p, err := llm.New("glm", "glm-5.2"); err == nil {
			return llm.NewFallback(p, small)
		}
	}
	return small
}

// guiProjectDirs returns the distinct working dirs across saved sessions
// (newest-first), the universe the feed scans for loose ends.
func guiProjectDirs() []string {
	store, err := session.SharedOpen()
	if err != nil || store == nil {
		if wd, e := os.Getwd(); e == nil {
			return []string{wd}
		}
		return nil
	}
	_ = store.Discover()
	seen := map[string]bool{}
	var dirs []string
	for _, m := range store.List() {
		if m.Cwd == "" || seen[m.Cwd] {
			continue
		}
		if st, e := os.Stat(m.Cwd); e != nil || !st.IsDir() {
			continue
		}
		seen[m.Cwd] = true
		dirs = append(dirs, m.Cwd)
	}
	if len(dirs) == 0 {
		if wd, e := os.Getwd(); e == nil {
			dirs = append(dirs, wd)
		}
	}
	return dirs
}
