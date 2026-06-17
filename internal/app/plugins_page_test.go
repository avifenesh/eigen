package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pluginpkg "github.com/avifenesh/eigen/internal/plugin"
)

func seedPluginPage(t *testing.T) *pluginpkg.Registry {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, ".eigen")
	reg := pluginpkg.NewRegistryAt(root)
	if err := os.MkdirAll(filepath.Join(root, "skills", "demo-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "demo-skill", "SKILL.md"), []byte("# Demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "commands", "demo-review.md"), []byte("review"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mcp.json"), []byte(`{"servers":[{"name":"demo-mcp","command":["demo"]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddMarket(pluginpkg.MarketRecord{Name: "core", Source: "octo/plugins", Owner: "Octo", Added: time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := reg.RecordInstall(pluginpkg.InstalledPlugin{
		Name:        "demo",
		Marketplace: "core",
		Version:     "1.2.3",
		Description: "Demo plugin for tests",
		Root:        filepath.Join(root, "plugins", "demo"),
		Skills:      []string{"demo-skill"},
		Commands:    []string{"demo-review"},
		MCPServers:  []string{"demo-mcp"},
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestPluginsPageRendersProductSurface(t *testing.T) {
	seedPluginPage(t)
	m := NewAt(testData(), PagePlugins)
	m.width, m.height = 120, 36
	v := m.plugins.view(m, 100, 30)
	for _, want := range []string{
		"Plugins make Eigen work your way.",
		"1 Plugins 1",
		"2 Marketplace 1",
		"3 Wiring 1",
		"my plugins",
		"demo",
		"enabled",
		"1 skill",
		"from core",
	} {
		if !strings.Contains(v, want) {
			t.Fatalf("plugins page missing %q:\n%s", want, v)
		}
	}

	m.Update(key("2"))
	v = m.plugins.view(m, 100, 30)
	for _, want := range []string{"marketplaces", "core", "octo/plugins", "/plugin install <name>@core"} {
		if !strings.Contains(v, want) {
			t.Fatalf("marketplace tab missing %q:\n%s", want, v)
		}
	}
	m.plugins.catalogMarket = "core"
	m.plugins.catalog = []pluginpkg.PluginEntry{{Name: "reviewer", Description: "Review code changes", Category: "coding", Version: "0.1.0", Keywords: []string{"review", "git"}}}
	v = m.plugins.view(m, 100, 30)
	for _, want := range []string{"1 plugin catalog", "reviewer", "Review code changes", "coding"} {
		if !strings.Contains(v, want) {
			t.Fatalf("marketplace catalog preview missing %q:\n%s", want, v)
		}
	}

	m.Update(key(" "))
	m.plugins.reload()
	if mk, ok := pluginpkg.NewRegistryAt(filepath.Join(os.Getenv("HOME"), ".eigen")).MarketByName("core"); !ok || !mk.Disabled {
		t.Fatalf("space on marketplace tab should disable marketplace: %+v ok=%v", mk, ok)
	}
	v = m.plugins.view(m, 100, 30)
	if !strings.Contains(v, "disabled") {
		t.Fatalf("disabled marketplace state should render:\n%s", v)
	}
	m.Update(key(" "))
	m.plugins.reload()
	if mk, ok := pluginpkg.NewRegistryAt(filepath.Join(os.Getenv("HOME"), ".eigen")).MarketByName("core"); !ok || mk.Disabled {
		t.Fatalf("second space should re-enable marketplace: %+v ok=%v", mk, ok)
	}

	m.Update(key("3"))
	v = m.plugins.view(m, 100, 30)
	for _, want := range []string{"wired components", "demo-mcp", "mcp"} {
		if !strings.Contains(v, want) {
			t.Fatalf("wiring tab missing %q:\n%s", want, v)
		}
	}
}

func TestPluginsPageUninstallRequiresConfirmation(t *testing.T) {
	reg := seedPluginPage(t)
	m := NewAt(testData(), PagePlugins)
	m.width, m.height = 120, 36
	m.plugins.load()

	m.Update(key("X"))
	if !m.plugins.confirm.active || m.plugins.confirm.kind != "plugin" || m.plugins.confirm.name != "demo" {
		t.Fatalf("X should ask for plugin removal confirmation, got %+v", m.plugins.confirm)
	}
	if _, ok := reg.InstalledByName("demo"); !ok {
		t.Fatal("plugin should not be removed before confirmation")
	}
	v := m.plugins.view(m, 100, 30)
	if !strings.Contains(v, "remove plugin") || !strings.Contains(v, "y confirm") {
		t.Fatalf("confirmation prompt missing:\n%s", v)
	}

	m.Update(key("n"))
	if m.plugins.confirm.active {
		t.Fatal("n should cancel confirmation")
	}
	if _, ok := reg.InstalledByName("demo"); !ok {
		t.Fatal("cancelled removal should keep plugin installed")
	}

	m.Update(key("X"))
	m.Update(key("y"))
	if _, ok := reg.InstalledByName("demo"); ok {
		t.Fatal("confirmed removal should uninstall plugin")
	}
}

func TestPluginsPageCanNavigateCatalogAndInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	market := filepath.Join(t.TempDir(), "market")
	mustWriteAppTest(t, filepath.Join(market, ".claude-plugin", "marketplace.json"), `{
	  "name": "local-market",
	  "plugins": [
	    {"name": "alpha", "source": "./plugins/alpha", "description": "first"},
	    {"name": "beta", "source": "./plugins/beta", "description": "second"}
	  ]
	}`)
	mustWriteAppTest(t, filepath.Join(market, "plugins", "alpha", ".claude-plugin", "plugin.json"), `{"name":"alpha"}`)
	mustWriteAppTest(t, filepath.Join(market, "plugins", "alpha", "skills", "main", "SKILL.md"), "---\nname: main\ndescription: alpha\n---\nalpha\n")
	mustWriteAppTest(t, filepath.Join(market, "plugins", "beta", ".claude-plugin", "plugin.json"), `{"name":"beta"}`)
	mustWriteAppTest(t, filepath.Join(market, "plugins", "beta", "skills", "main", "SKILL.md"), "---\nname: main\ndescription: beta\n---\nbeta\n")
	reg := pluginpkg.NewRegistryAt(filepath.Join(home, ".eigen"))
	if _, _, err := reg.AddMarketplace(context.Background(), market, nil); err != nil {
		t.Fatal(err)
	}

	m := NewAt(testData(), PagePlugins)
	m.width, m.height = 120, 36
	m.Update(key("2"))
	_, cmd := m.Update(key("enter")) // refresh selected marketplace and focus catalog
	if cmd == nil || !m.plugins.prompt.busy {
		t.Fatal("enter should start a visible marketplace refresh job")
	}
	m.Update(cmd())
	if !m.plugins.catalogFocus || len(m.plugins.catalog) != 2 {
		t.Fatalf("enter should focus refreshed catalog, focus=%v catalog=%v", m.plugins.catalogFocus, m.plugins.catalog)
	}
	m.Update(key("j"))              // beta
	_, cmd = m.Update(key("enter")) // install focused catalog plugin
	if cmd == nil || !m.plugins.prompt.busy {
		t.Fatal("enter on focused catalog should start a visible install job")
	}
	m.Update(cmd())
	if _, ok := reg.InstalledByName("beta"); !ok {
		t.Fatal("enter on focused marketplace catalog should install selected plugin")
	}
	if _, ok := reg.InstalledByName("alpha"); ok {
		t.Fatal("catalog navigation should have installed beta, not alpha")
	}
}

func mustWriteAppTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPluginsPageCanMarkCatalogPluginsAndInstallBatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	market := filepath.Join(t.TempDir(), "market")
	mustWriteAppTest(t, filepath.Join(market, ".claude-plugin", "marketplace.json"), `{
	  "name": "local-market",
	  "plugins": [
	    {"name": "alpha", "source": "./plugins/alpha", "description": "first"},
	    {"name": "beta", "source": "./plugins/beta", "description": "second"}
	  ]
	}`)
	mustWriteAppTest(t, filepath.Join(market, "plugins", "alpha", ".claude-plugin", "plugin.json"), `{"name":"alpha"}`)
	mustWriteAppTest(t, filepath.Join(market, "plugins", "alpha", "skills", "main", "SKILL.md"), "---\nname: main\ndescription: alpha\n---\nalpha\n")
	mustWriteAppTest(t, filepath.Join(market, "plugins", "beta", ".claude-plugin", "plugin.json"), `{"name":"beta"}`)
	mustWriteAppTest(t, filepath.Join(market, "plugins", "beta", "skills", "main", "SKILL.md"), "---\nname: main\ndescription: beta\n---\nbeta\n")
	reg := pluginpkg.NewRegistryAt(filepath.Join(home, ".eigen"))
	if _, _, err := reg.AddMarketplace(context.Background(), market, nil); err != nil {
		t.Fatal(err)
	}

	m := NewAt(testData(), PagePlugins)
	m.width, m.height = 120, 36
	m.Update(key("2"))
	_, cmd := m.Update(key("enter"))
	m.Update(cmd())
	m.Update(key(" ")) // mark alpha
	m.Update(key("j"))
	m.Update(key(" ")) // mark beta
	if got := m.plugins.markedCatalogPlugins(); len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("markedCatalogPlugins = %v", got)
	}
	if v := m.plugins.view(m, 100, 30); !strings.Contains(v, "2 marked") || !strings.Contains(v, "●") {
		t.Fatalf("catalog should render marked plugins:\n%s", v)
	}

	_, cmd = m.Update(key("i"))
	if cmd == nil || !m.plugins.prompt.busy {
		t.Fatal("i should install all marked plugins with a visible busy marker")
	}
	m.Update(cmd())
	for _, name := range []string{"alpha", "beta"} {
		if _, ok := reg.InstalledByName(name); !ok {
			t.Fatalf("batch install should install %s", name)
		}
	}
	if len(m.plugins.catalogSelected) != 0 {
		t.Fatal("batch install completion should clear selected marks")
	}
}

func TestPluginsPageToggleInstalledPlugin(t *testing.T) {
	reg := seedPluginPage(t)
	m := NewAt(testData(), PagePlugins)
	m.width, m.height = 120, 36
	m.plugins.load()
	if len(m.plugins.installed) != 1 {
		t.Fatalf("expected seeded plugin, got %d", len(m.plugins.installed))
	}
	if !pluginEnabled(m.plugins.installed[0], m.plugins.rows, reg) {
		t.Fatal("seeded plugin should start enabled")
	}

	m.Update(key("enter"))
	m.plugins.reload()
	if pluginEnabled(m.plugins.installed[0], m.plugins.rows, reg) {
		t.Fatal("enter on selected plugin should disable it")
	}
	if _, err := os.Stat(filepath.Join(reg.SkillsDir(), "demo-skill", "SKILL.md.disabled")); err != nil {
		t.Fatalf("skill should be parked when plugin disabled: %v", err)
	}

	m.Update(key("enter"))
	m.plugins.reload()
	if !pluginEnabled(m.plugins.installed[0], m.plugins.rows, reg) {
		t.Fatal("second enter should re-enable plugin")
	}
}
