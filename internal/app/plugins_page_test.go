package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
	pluginpkg "github.com/avifenesh/eigen/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
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
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agents", "demo-agent-reviewer.md"), []byte("---\nname: demo-agent-reviewer\ndescription: review\n---\nReview carefully.\n"), 0o644); err != nil {
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
		Agents:      []string{"demo-agent-reviewer"},
		Commands:    []string{"demo-review"},
		MCPServers:  []string{"demo-mcp"},
		AgentRoles:  []pluginpkg.InstalledAgentRole{{Name: "demo-agent-reviewer", SourceName: "reviewer", Kind: "general", Difficulty: "easy", Tools: []string{"read", "grep"}, ReadOnly: true}},
		ScanStatus:  pluginpkg.ScanStatusForced,
		ScanCount:   1,
		Warnings:    []string{"forced install: security scan reported risky components", "1 Codex app integration(s) not wired yet"},
		Scans:       []pluginpkg.ScanFinding{{Component: "skill:demo-skill", Reasons: []string{"forced test finding"}}},
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
		"4 Hooks 0",
		"my plugins",
		"demo",
		"enabled",
		"1 skill",
		"1 agent",
		"task roles",
		"demo-agent-reviewer",
		"from core",
		"scan verdict",
		"forced install",
		"scan flags",
		"security scan reported risky components",
		"forced test finding",
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
	for _, want := range []string{"1 plugins", "reviewer", "Review code changes", "coding"} {
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

	root := filepath.Join(os.Getenv("HOME"), ".eigen")
	mustWriteAppTest(t, filepath.Join(root, "hooks.json"), `{"hooks":[{"event":"session_stop","command":["echo","done"]}]}`)
	m.plugins.reload()
	m.Update(key("4"))
	v = m.plugins.view(m, 100, 30)
	for _, want := range []string{"hooks", "session_stop", "hook", "echo done", "space toggle hook"} {
		if !strings.Contains(v, want) {
			t.Fatalf("hooks tab missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "demo-mcp") {
		t.Fatalf("hooks tab should not show non-hook wiring:\n%s", v)
	}
}

type pluginPageScanProvider struct {
	riskyNames map[string]bool
}

func (p pluginPageScanProvider) Name() string    { return "plugin-page-scan" }
func (p pluginPageScanProvider) ModelID() string { return "plugin-page-scan" }
func (p pluginPageScanProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	text := ""
	if len(req.Messages) > 0 {
		text = req.Messages[len(req.Messages)-1].Text
	}
	for name := range p.riskyNames {
		if strings.Contains(text, "Skill name: "+name+"\n") {
			return &llm.Response{Text: "VERDICT: RISKY\nREASONS:\n- command risky"}, nil
		}
	}
	return &llm.Response{Text: "VERDICT: SAFE"}, nil
}

func TestPluginsPageSurfacesScanResultsAndRollback(t *testing.T) {
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
	for _, name := range []string{"alpha", "beta"} {
		mustWriteAppTest(t, filepath.Join(market, "plugins", name, ".claude-plugin", "plugin.json"), `{"name":"`+name+`"}`)
		mustWriteAppTest(t, filepath.Join(market, "plugins", name, "skills", "main", "SKILL.md"), "---\nname: main\ndescription: "+name+"\n---\n"+name+"\n")
		mustWriteAppTest(t, filepath.Join(market, "plugins", name, "commands", "do-it.md"), "do it\n")
	}
	reg := pluginpkg.NewRegistryAt(filepath.Join(home, ".eigen"))
	if _, _, err := reg.AddMarketplace(context.Background(), market, nil); err != nil {
		t.Fatal(err)
	}
	data := testData()
	data.Small = pluginPageScanProvider{riskyNames: map[string]bool{"do-it": true}}

	status := runPluginInstall(data, "alpha --force")
	for _, want := range []string{"despite scan flags", "result:", "scan verdict: forced install", "scan flag command:do-it", "warning: forced install"} {
		if !strings.Contains(status, want) {
			t.Fatalf("forced install status missing %q:\n%s", want, status)
		}
	}
	rec, ok := reg.InstalledByName("alpha")
	if !ok {
		t.Fatal("forced install should record alpha")
	}
	if rec.ScanStatus != pluginpkg.ScanStatusForced || rec.ScanCount != 2 || len(rec.Scans) != 1 {
		t.Fatalf("forced install should persist scan details, got %+v", rec)
	}
	if len(rec.Warnings) == 0 || !strings.Contains(rec.Warnings[0], "forced install") {
		t.Fatalf("forced install warning should be recorded, got %+v", rec.Warnings)
	}

	m := NewAt(data, PagePlugins)
	m.width, m.height = 120, 36
	v := m.plugins.view(m, 100, 30)
	for _, want := range []string{"scan forced", "scan verdict", "forced install", "forced scan flags", "command risky"} {
		if !strings.Contains(v, want) {
			t.Fatalf("plugins page missing forced scan detail %q:\n%s", want, v)
		}
	}

	status = runPluginInstall(data, "beta")
	for _, want := range []string{"install failed for \"beta\"", "rolled back/no plugin recorded", "command risky"} {
		if !strings.Contains(status, want) {
			t.Fatalf("rollback status missing %q:\n%s", want, status)
		}
	}
	if _, ok := reg.InstalledByName("beta"); ok {
		t.Fatal("failed install should not record beta")
	}
	if _, err := os.Stat(filepath.Join(reg.SkillsDir(), "beta-main")); err == nil {
		t.Fatal("failed install should roll back beta skill files")
	}
}

func TestPluginsPageMouseTabsAndHookRows(t *testing.T) {
	seedPluginPage(t)
	root := filepath.Join(os.Getenv("HOME"), ".eigen")
	hooksPath := filepath.Join(root, "hooks.json")
	mustWriteAppTest(t, hooksPath, `{"hooks":[{"event":"session_start","command":["echo","start"]},{"event":"turn_done","command":["echo","done"]}]}`)

	m := NewAt(testData(), PagePlugins)
	m.width, m.height = 120, 36
	l := m.computeLayout()
	_ = m.plugins.view(m, l.inner.w, l.inner.h)
	hit, ok := pluginTabHit(&m.plugins, pluginsTabHooks)
	if !ok {
		t.Fatal("hooks tab should be in the click map")
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + hit.x0, Y: l.inner.y + hit.line})
	if m.plugins.tab != pluginsTabHooks {
		t.Fatalf("clicking hooks tab should switch tabs, got %v", m.plugins.tab)
	}

	_ = m.plugins.view(m, l.inner.w, l.inner.h)
	line := rowLine(&m.plugins.clicks, 1)
	if line < 0 {
		t.Fatal("second hook row should be clickable")
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: l.inner.y + line})
	if m.plugins.list.cursor != 1 {
		t.Fatalf("first hook row click should select row 1, got %d", m.plugins.list.cursor)
	}

	_ = m.plugins.view(m, l.inner.w, l.inner.h)
	line = rowLine(&m.plugins.clicks, 1)
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: l.inner.y + line})
	rows := loadHookRows(hooksPath, "user")
	if len(rows) != 2 || !rows[1].Disabled {
		t.Fatalf("second hook row click should toggle disabled, got %+v", rows)
	}
}

func TestPluginsPageMouseCardsMarkEveryLine(t *testing.T) {
	seedPluginPage(t)
	m := NewAt(testData(), PagePlugins)
	m.width, m.height = 120, 36
	l := m.computeLayout()
	_ = m.plugins.view(m, l.inner.w, l.inner.h)
	if got := clickLineCount(&m.plugins.clicks, 0); got < 2 {
		t.Fatalf("installed plugin card should mark both rendered lines, got %d", got)
	}

	m.Update(key("2"))
	_ = m.plugins.view(m, l.inner.w, l.inner.h)
	if got := clickLineCount(&m.plugins.clicks, 0); got < 2 {
		t.Fatalf("marketplace card should mark both rendered lines, got %d", got)
	}
}

func TestPluginsMarketplaceCatalogMouseRows(t *testing.T) {
	seedPluginPage(t)
	m := NewAt(testData(), PagePlugins)
	m.width, m.height = 120, 36
	m.plugins.setTab(pluginsTabMarketplace)
	m.plugins.catalogMarket = "core"
	m.plugins.catalog = []pluginpkg.PluginEntry{
		{Name: "alpha", Description: "first"},
		{Name: "beta", Description: "second"},
	}
	l := m.computeLayout()
	_ = m.plugins.view(m, l.inner.w, l.inner.h)
	line := rowLine(&m.plugins.catalogClicks, 1)
	if line < 0 {
		t.Fatal("catalog row should be in the click map")
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: l.inner.y + line})
	if !m.plugins.catalogFocus || m.plugins.catalogList.cursor != 1 {
		t.Fatalf("catalog click should focus/select beta, focus=%v cursor=%d", m.plugins.catalogFocus, m.plugins.catalogList.cursor)
	}

	_ = m.plugins.view(m, l.inner.w, l.inner.h)
	line = rowLine(&m.plugins.catalogClicks, 1)
	_, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: l.inner.y + line})
	if cmd == nil || !m.plugins.prompt.busy {
		t.Fatal("second catalog row click should start installing the selected plugin")
	}
}

func pluginTabHit(p *pluginsState, tab pluginsTab) (pluginTabClick, bool) {
	for _, hit := range p.tabClicks.hits {
		if hit.tab == tab {
			return hit, true
		}
	}
	return pluginTabClick{}, false
}

func clickLineCount(c *clickMap, idx int) int {
	count := 0
	for _, got := range c.line2idx {
		if got == idx {
			count++
		}
	}
	return count
}

func TestAppPaletteSurfacesPluginAgentRoles(t *testing.T) {
	seedPluginPage(t)
	m := NewAt(testData(), PageHome)
	m.width, m.height = 120, 36
	m.palette.openPalette(m)
	for _, r := range "demo agent reviewer" {
		m.palette.update(m, string(r), []rune{r})
	}
	if len(m.palette.matches) == 0 {
		t.Fatal("plugin agent role should be searchable in app palette")
	}
	idx := m.palette.matches[0]
	if !strings.Contains(m.palette.cmds[idx].name, "demo-agent-reviewer") {
		t.Fatalf("top palette match should be plugin agent role, got %q", m.palette.cmds[idx].name)
	}
	m.palette.update(m, "enter", nil)
	if m.active != PagePlugins || m.plugins.tab != pluginsTabInstalled || !strings.Contains(m.plugins.err, "task role") {
		t.Fatalf("palette role should navigate to plugin detail, active=%v tab=%v err=%q", m.active, m.plugins.tab, m.plugins.err)
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
	_, cmd = m.Update(key("v"))
	if cmd == nil || !m.plugins.prompt.busy {
		t.Fatal("v should start a visible plugin preview job")
	}
	m.Update(cmd())
	if m.plugins.catalogPreview == nil || m.plugins.catalogPreview.Entry.Name != "alpha" {
		t.Fatalf("expected alpha preview, got %+v", m.plugins.catalogPreview)
	}
	if v := m.plugins.view(m, 100, 30); !strings.Contains(v, "manifest preview") || !strings.Contains(v, "will install") {
		t.Fatalf("preview should render manifest/component summary:\n%s", v)
	}
	m.height = 16
	m.contentScroll = 0
	m.handleContentScrollKey("pgdown")
	if m.contentScroll == 0 {
		t.Fatal("long plugin preview should be scrollable with pgdown")
	}
	m.setActive(PageHome)
	if m.contentScroll != 0 {
		t.Fatal("switching pages should reset generic content scroll")
	}
	m.setActive(PagePlugins)
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

func TestMarketplaceCatalogHidesInstalledPlugins(t *testing.T) {
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
	if _, err := reg.InstallPlugin(context.Background(), "alpha", "", pluginpkg.InstallOptions{}); err != nil {
		t.Fatal(err)
	}

	m := NewAt(testData(), PagePlugins)
	m.width, m.height = 120, 36
	m.Update(key("2"))
	_, cmd := m.Update(key("enter"))
	m.Update(cmd())
	if len(m.plugins.catalog) != 1 || m.plugins.catalog[0].Name != "beta" {
		t.Fatalf("catalog should hide installed alpha and show only beta, got %+v", m.plugins.catalog)
	}
	v := m.plugins.view(m, 100, 30)
	if strings.Contains(v, "alpha") || !strings.Contains(v, "beta") {
		t.Fatalf("rendered catalog should omit installed alpha and show beta:\n%s", v)
	}
}

func TestMarketplaceShiftUUpdatesInstalledAndShowsNewPlugins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	market := filepath.Join(t.TempDir(), "market")
	manifest := func(withBeta bool) string {
		plugins := `{"name": "alpha", "source": "./plugins/alpha", "description": "first"}`
		if withBeta {
			plugins += `,{"name": "beta", "source": "./plugins/beta", "description": "second"}`
		}
		return `{"name":"local-market","plugins":[` + plugins + `]}`
	}
	mustWriteAppTest(t, filepath.Join(market, ".claude-plugin", "marketplace.json"), manifest(false))
	mustWriteAppTest(t, filepath.Join(market, "plugins", "alpha", ".claude-plugin", "plugin.json"), `{"name":"alpha"}`)
	mustWriteAppTest(t, filepath.Join(market, "plugins", "alpha", "skills", "main", "SKILL.md"), "---\nname: main\ndescription: alpha\n---\nv1\n")
	mustWriteAppTest(t, filepath.Join(market, "plugins", "beta", ".claude-plugin", "plugin.json"), `{"name":"beta"}`)
	mustWriteAppTest(t, filepath.Join(market, "plugins", "beta", "skills", "main", "SKILL.md"), "---\nname: main\ndescription: beta\n---\nbeta\n")
	reg := pluginpkg.NewRegistryAt(filepath.Join(home, ".eigen"))
	if _, _, err := reg.AddMarketplace(context.Background(), market, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.InstallPlugin(context.Background(), "alpha", "", pluginpkg.InstallOptions{}); err != nil {
		t.Fatal(err)
	}

	mustWriteAppTest(t, filepath.Join(market, ".claude-plugin", "marketplace.json"), manifest(true))
	mustWriteAppTest(t, filepath.Join(market, "plugins", "alpha", "skills", "main", "SKILL.md"), "---\nname: main\ndescription: alpha\n---\nv2\n")

	m := NewAt(testData(), PagePlugins)
	m.width, m.height = 120, 36
	m.Update(key("2"))
	_, cmd := m.Update(key("U"))
	if cmd == nil || !m.plugins.prompt.busy {
		t.Fatal("shift-U should start a visible marketplace update job")
	}
	m.Update(cmd())
	b, err := os.ReadFile(filepath.Join(reg.SkillsDir(), "alpha-main", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "v2") {
		t.Fatalf("shift-U should overwrite installed alpha files, got:\n%s", b)
	}
	if len(m.plugins.catalog) != 1 || m.plugins.catalog[0].Name != "beta" {
		t.Fatalf("shift-U should add new uninstalled beta to catalog and hide alpha, got %+v", m.plugins.catalog)
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
