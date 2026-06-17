package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/plugin"
)

func TestPluginCommandListAndRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	reg, err := plugin.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.RecordInstall(plugin.InstalledPlugin{
		Name:        "demo",
		Description: "demo plugin",
		Marketplace: "core",
		Root:        filepath.Join(home, ".eigen", "plugins", "demo"),
		Skills:      []string{"demo-skill"},
		Commands:    []string{"demo-command"},
		MCPServers:  []string{"demo-mcp"},
		Hooks:       1,
	}); err != nil {
		t.Fatal(err)
	}

	m := testModel(t)
	m.command("/plugin list")
	if got := lastNote(m); !strings.Contains(got, "demo") || !strings.Contains(got, "1 skill") || !strings.Contains(got, "from core") {
		t.Fatalf("/plugin list missing installed plugin details:\n%s", got)
	}

	m.command("/plugin remove demo")
	if got := lastNote(m); !strings.Contains(got, "removed plugin") {
		t.Fatalf("/plugin remove should confirm removal, got %q", got)
	}
	if _, ok := reg.InstalledByName("demo"); ok {
		t.Fatal("/plugin remove should delete the installed-plugin record")
	}
}

func TestPluginCommandNoPlugins(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t)
	m.command("/plugin list")
	if got := lastNote(m); !strings.Contains(got, "no plugins installed") {
		t.Fatalf("expected no-plugins note, got %q", got)
	}
}

func TestMarketplaceCommandListAndRemove(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg, err := plugin.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.AddMarket(plugin.MarketRecord{Name: "core", Source: "octo/plugins", Added: time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}

	m := testModel(t)
	m.command("/marketplace list")
	if got := lastNote(m); !strings.Contains(got, "core") || !strings.Contains(got, "octo/plugins") {
		t.Fatalf("/marketplace list missing marketplace details:\n%s", got)
	}

	m.command("/marketplace remove core")
	if got := lastNote(m); !strings.Contains(got, "removed marketplace") {
		t.Fatalf("/marketplace remove should confirm removal, got %q", got)
	}
	if _, ok := reg.MarketByName("core"); ok {
		t.Fatal("/marketplace remove should delete the market record")
	}
}

func TestPluginInstallArgParsing(t *testing.T) {
	name, market := splitPluginMarket("reviewer@core")
	if name != "reviewer" || market != "core" {
		t.Fatalf("splitPluginMarket = %q %q", name, market)
	}
	parsed, err := parsePluginInstallArgs([]string{"--overwrite", "--marketplace", "core", "reviewer", "--no-scan"})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.name != "reviewer" || parsed.marketplace != "core" || !parsed.overwrite || !parsed.noScan {
		t.Fatalf("parsePluginInstallArgs = %+v", parsed)
	}
	parsed, err = parsePluginInstallArgs([]string{"reviewer@core", "--force"})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.name != "reviewer" || parsed.marketplace != "core" || !parsed.force {
		t.Fatalf("inline marketplace parse = %+v", parsed)
	}
}
