package app

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/daemon"
	pluginpkg "github.com/avifenesh/eigen/internal/plugin"
	"github.com/charmbracelet/x/ansi"
)

func assertGoldenContains(t *testing.T, goldenPath, rendered string) {
	t.Helper()
	b, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(rendered)
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(plain, line) {
			t.Fatalf("rendered view missing golden token %q from %s:\n%s", line, goldenPath, plain)
		}
	}
}

func TestAppHomeGoldenSnapshotTokens(t *testing.T) {
	m := layoutModel(t, 80, 20)
	m.active = PageHome
	assertGoldenContains(t, "testdata/golden/home_80x20.txt", m.View())
}

func TestAppEveryPageGoldenSnapshotTokens(t *testing.T) {
	for _, p := range pages {
		t.Run(p.name, func(t *testing.T) {
			m := layoutModel(t, 120, 30)
			m.active = p.page
			plain := ansi.Strip(m.View())
			for _, want := range []string{"eigen", p.name} {
				if !strings.Contains(plain, want) {
					t.Fatalf("page %s missing golden token %q:\n%s", p.name, want, plain)
				}
			}
			if strings.Contains(plain, "[home]") {
				t.Fatalf("page %s regressed to classic header buttons:\n%s", p.name, plain)
			}
		})
	}
}

func TestAppPaletteGoldenSnapshotTokens(t *testing.T) {
	m := layoutModel(t, 100, 24)
	m.Update(key(":"))
	for _, r := range "plug" {
		m.Update(key(string(r)))
	}
	assertGoldenContains(t, "testdata/golden/palette_plugins_100x24.txt", m.View())
}

func TestAppLiveSessionsPluginsGoldenSnapshotTokens(t *testing.T) {
	d := testData()
	d.Daemon = &daemon.Client{}
	d.Live = []daemon.SessionInfo{
		{ID: "s1", Title: "build", Dir: "/p", Model: "gpt-5.5", Status: daemon.StatusWorking, Turns: 3, Views: 1, Updated: 1},
		{ID: "s2", Title: "review", Dir: "/q", Status: daemon.StatusApproval},
		{ID: "s3", Title: "idle", Dir: "/r", Status: daemon.StatusIdle},
	}
	d.Sessions = []SessionRow{
		{ID: "d1", Title: "daemon work", Source: "daemon", Dir: "/repo", Msgs: 10, Updated: time.Now().UnixNano()},
		{ID: "c1", Title: "codex work", Source: "codex", Dir: "/repo", Msgs: 5, Updated: time.Now().UnixNano()},
		{ID: "e1", Title: "eigen work", Source: "eigen", Dir: "/repo", Msgs: 2, Updated: time.Now().UnixNano()},
	}

	live := NewAt(d, PageLive)
	live.width, live.height = 120, 30
	assertGoldenContains(t, "testdata/golden/live_120x30.txt", live.View())

	sessions := NewAt(d, PageSessions)
	sessions.width, sessions.height = 120, 30
	assertGoldenContains(t, "testdata/golden/sessions_120x30.txt", sessions.View())

	seedPluginPage(t)
	plugins := NewAt(&Data{Config: config.Config{}, Skills: d.Skills}, PagePlugins)
	plugins.width, plugins.height = 120, 36
	plugins.plugins.load()
	plugins.plugins.catalogMarket = "core"
	plugins.plugins.catalog = []pluginpkg.PluginEntry{{Name: "reviewer", Description: "Review code changes", Category: "coding"}}
	plugins.plugins.tab = pluginsTabMarketplace
	assertGoldenContains(t, "testdata/golden/plugins_marketplace_120x36.txt", plugins.View())
	plugins.plugins.tab = pluginsTabExtensions
	assertGoldenContains(t, "testdata/golden/plugins_wiring_120x36.txt", plugins.View())
}
