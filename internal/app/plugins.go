package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	pluginpkg "github.com/avifenesh/eigen/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ExtRow is one extension as the plugins page shows it: an MCP server, a
// plugin tool, an LSP server, or a hook.
type ExtRow struct {
	Kind     string // "mcp" | "plugin" | "lsp" | "hook"
	Name     string
	Detail   string // command / event / etc.
	Source   string // which config file declared it
	Path     string // config file path (for toggling)
	Index    int    // entry index within that file's list
	Disabled bool
}

// loadExtensions reads every extension config (user + project): mcp.json
// servers, plugins.json tools, lsp.json servers, hooks.json hooks. Read-only
// parse — nothing is connected or executed.
func loadExtensions() []ExtRow {
	home, _ := os.UserHomeDir()
	userDir := filepath.Join(home, ".eigen")
	projDir := ".eigen"
	var rows []ExtRow
	for _, dir := range []string{userDir, projDir} {
		src := "user"
		if dir == projDir {
			src = "project"
		}
		rows = append(rows, loadMCPRows(filepath.Join(dir, "mcp.json"), src)...)
		rows = append(rows, loadPluginRows(filepath.Join(dir, "plugins.json"), src)...)
		rows = append(rows, loadLSPRows(filepath.Join(dir, "lsp.json"), src)...)
		rows = append(rows, loadHookRows(filepath.Join(dir, "hooks.json"), src)...)
	}
	return rows
}

func loadMCPRows(path, src string) []ExtRow {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		Servers []struct {
			Name     string   `json:"name"`
			Command  []string `json:"command"`
			Tools    []string `json:"tools"`
			Disabled bool     `json:"disabled"`
		} `json:"servers"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	var rows []ExtRow
	for i, s := range cfg.Servers {
		detail := strings.Join(s.Command, " ")
		if n := len(s.Tools); n > 0 {
			detail += fmt.Sprintf(" · %d tools allowlisted", n)
		}
		rows = append(rows, ExtRow{Kind: "mcp", Name: s.Name, Detail: detail, Source: src,
			Path: path, Index: i, Disabled: s.Disabled})
	}
	return rows
}

func loadPluginRows(path, src string) []ExtRow {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var specs []struct {
		Name     string   `json:"name"`
		Command  []string `json:"command"`
		ReadOnly bool     `json:"readonly"`
		Disabled bool     `json:"disabled"`
	}
	if json.Unmarshal(data, &specs) != nil {
		return nil
	}
	var rows []ExtRow
	for i, p := range specs {
		detail := strings.Join(p.Command, " ")
		if p.ReadOnly {
			detail += " · read-only"
		}
		rows = append(rows, ExtRow{Kind: "plugin", Name: p.Name, Detail: detail, Source: src,
			Path: path, Index: i, Disabled: p.Disabled})
	}
	return rows
}

func loadLSPRows(path, src string) []ExtRow {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		Servers []struct {
			Name      string   `json:"name"`
			Command   []string `json:"command"`
			Languages []string `json:"languages"`
			Disabled  bool     `json:"disabled"`
		} `json:"servers"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	var rows []ExtRow
	for i, s := range cfg.Servers {
		detail := strings.Join(s.Command, " ")
		if len(s.Languages) > 0 {
			detail += " · " + strings.Join(s.Languages, ",")
		}
		rows = append(rows, ExtRow{Kind: "lsp", Name: s.Name, Detail: detail, Source: src,
			Path: path, Index: i, Disabled: s.Disabled})
	}
	return rows
}

func loadHookRows(path, src string) []ExtRow {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	type hookSpec struct {
		Event    string   `json:"event"`
		Command  []string `json:"command"`
		Tool     string   `json:"tool"`
		Disabled bool     `json:"disabled"`
	}
	var specs []hookSpec
	if json.Unmarshal(data, &specs) != nil {
		var wrap struct {
			Hooks []hookSpec `json:"hooks"`
		}
		if json.Unmarshal(data, &wrap) != nil {
			return nil
		}
		specs = wrap.Hooks
	}
	var rows []ExtRow
	for i, h := range specs {
		name := h.Event
		if h.Tool != "" {
			name += ":" + h.Tool
		}
		rows = append(rows, ExtRow{Kind: "hook", Name: name, Detail: strings.Join(h.Command, " "), Source: src,
			Path: path, Index: i, Disabled: h.Disabled})
	}
	return rows
}

// pluginsState is Eigen's Skills & Apps surface: a product page for installed
// plugins, configured marketplaces, and the raw extension wiring they create.
// The shape intentionally mirrors Codex desktop's Skills & Apps treatment:
// header copy, segmented tabs, install affordances, cards, enabled state,
// and a power-user wiring view.
type pluginsState struct {
	list              list
	tab               pluginsTab
	rows              []ExtRow
	installed         []pluginpkg.InstalledPlugin
	markets           []pluginpkg.MarketRecord
	catalogMarket     string
	catalog           []pluginpkg.PluginEntry
	catalogList       list
	catalogFocus      bool
	catalogSelected   map[string]bool // selected catalog plugin names for batch install
	catalogPreview    *pluginpkg.PluginPreview
	catalogPreviewKey string
	loaded            bool
	err               string // last action error/status ("" = none)
	prompt            installPrompt
	confirm           pluginConfirm
	clicks            clickMap
}

type pluginConfirm struct {
	active bool
	kind   string // "plugin" | "marketplace"
	name   string
}

func (c *pluginConfirm) open(kind, name string) {
	*c = pluginConfirm{active: true, kind: kind, name: name}
}

func (c *pluginConfirm) clear() { *c = pluginConfirm{} }

func (c pluginConfirm) render(w int) string {
	if !c.active {
		return ""
	}
	msg := fmt.Sprintf("remove %s %q? y confirm · esc cancel", c.kind, c.name)
	return "\n" + sErr.Render("  "+truncate(msg, w-4))
}

type pluginsTab int

const (
	pluginsTabInstalled pluginsTab = iota
	pluginsTabMarketplace
	pluginsTabExtensions
)

func (p *pluginsState) init(*Data) {}

func (p *pluginsState) load() {
	if p.loaded {
		return
	}
	p.rows = loadExtensions()
	p.installed = nil
	p.markets = nil
	if reg, err := appPluginRegistry(); err == nil {
		p.installed, _ = reg.Installed()
		p.markets, _ = reg.Markets()
	} else {
		p.err = err.Error()
	}
	p.filterInstalledCatalog()
	p.loaded = true
	p.syncListCount()
}

func (p *pluginsState) reload() {
	p.loaded = false
	p.load()
}

func (p *pluginsState) filterInstalledCatalog() {
	if len(p.catalog) == 0 || len(p.installed) == 0 {
		return
	}
	installed := map[string]bool{}
	for _, pl := range p.installed {
		installed[strings.ToLower(strings.TrimSpace(pl.Name))] = true
	}
	out := p.catalog[:0]
	for _, e := range p.catalog {
		k := strings.ToLower(strings.TrimSpace(e.Name))
		if k == "" || !installed[k] {
			out = append(out, e)
		}
		if installed[k] && p.catalogSelected != nil {
			delete(p.catalogSelected, k)
		}
	}
	p.catalog = out
	if len(p.catalogSelected) == 0 {
		p.catalogSelected = nil
	}
	if len(p.catalog) == 0 {
		p.catalogFocus = false
	}
}

func (p *pluginsState) syncListCount() {
	switch p.tab {
	case pluginsTabInstalled:
		p.list.count = len(p.installed)
	case pluginsTabMarketplace:
		p.list.count = len(p.markets)
	case pluginsTabExtensions:
		p.list.count = len(p.rows)
	}
	p.list.clamp()
	p.catalogList.count = len(p.catalog)
	p.catalogList.clamp()
	if p.catalogList.top > p.catalogList.cursor {
		p.catalogList.top = p.catalogList.cursor
	}
}

func (p *pluginsState) setTab(tab pluginsTab) {
	if p.tab == tab {
		return
	}
	p.tab = tab
	p.list.cursor, p.list.top = 0, 0
	p.catalogFocus = false
	p.catalogSelected = nil
	p.syncListCount()
	p.err = ""
}

func (p *pluginsState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p.load()
	key := msg.String()

	// Inline install prompt active: capture text input. Fetch/install/scan runs in
	// a background tea.Cmd so the page can render a busy line instead of freezing.
	if p.prompt.active {
		if p.prompt.busy {
			return m, nil
		}
		if src, ok := p.prompt.key(key, msg.Runes); ok {
			kind := p.prompt.kind
			data := m.data
			switch kind {
			case "marketplace":
				p.prompt.startBusy(kind, "marketplace", src, "adding marketplace "+src+" … (fetching catalog)")
				return m, func() tea.Msg {
					return installDoneMsg{page: PagePlugins, kind: kind, tab: pluginsTabMarketplace, status: runMarketplaceAdd(data, src)}
				}
			case "plugin":
				p.prompt.startBusy(kind, "plugin", src, "installing plugin "+src+" … (scanning + fetching)")
				return m, func() tea.Msg {
					return installDoneMsg{page: PagePlugins, kind: kind, tab: pluginsTabInstalled, status: runPluginInstall(data, src)}
				}
			}
		}
		return m, nil
	}

	// Destructive actions require an explicit confirmation key so a stray X or
	// stale click cannot silently remove plugin wiring or marketplace records.
	if p.confirm.active {
		switch key {
		case "y", "enter":
			p.applyConfirm()
		case "esc", "backspace", "n":
			p.confirm.clear()
		default:
			p.confirm.clear()
		}
		return m, nil
	}

	switch key {
	case "1":
		p.setTab(pluginsTabInstalled)
		return m, nil
	case "2":
		p.setTab(pluginsTabMarketplace)
		return m, nil
	case "3":
		p.setTab(pluginsTabExtensions)
		return m, nil
	case "left", "H":
		p.setTab(maxPluginTab(p.tab - 1))
		return m, nil
	case "right", "L":
		p.setTab(minPluginTab(p.tab + 1))
		return m, nil
	case "R": // manual refresh (capital: bare letters are page-jumps)
		p.reload()
		p.prompt.status = "refreshed"
		return m, nil
	case "a": // add a marketplace catalog
		p.setTab(pluginsTabMarketplace)
		p.prompt.open("marketplace", "marketplace (owner/repo[/sub][@ref])")
		return m, nil
	case "i": // install a plugin by name from any added marketplace
		if p.tab == pluginsTabMarketplace && p.catalogFocus {
			return m, p.installMarkedCatalogPlugins(m)
		}
		p.prompt.open("plugin", "plugin name[@marketplace]")
		return m, nil
	}

	if p.tab == pluginsTabMarketplace && p.catalogFocus {
		if consumed, cmd := p.updateCatalog(m, key); consumed {
			return m, cmd
		}
	}

	visible := p.visibleRows(m.height)
	if p.list.key(key, visible) {
		p.catalogFocus = false
		p.catalogSelected = nil
		return m, nil
	}

	switch p.tab {
	case pluginsTabInstalled:
		p.updateInstalled(key)
	case pluginsTabMarketplace:
		return m, p.updateMarketplace(m, key)
	case pluginsTabExtensions:
		p.updateExtension(key)
	}
	return m, nil
}

func (p *pluginsState) visibleRows(h int) int {
	// Installed/market rows render as two-line cards; extensions render one-line.
	body := h - 12
	if body < 3 {
		body = 3
	}
	if p.tab == pluginsTabExtensions {
		return body
	}
	return max(1, body/2)
}

func (p *pluginsState) applyConfirm() {
	kind, name := p.confirm.kind, p.confirm.name
	p.confirm.clear()
	switch kind {
	case "plugin":
		p.removePlugin(name)
	case "marketplace":
		p.removeMarketplace(name)
	}
}

func (p *pluginsState) updateInstalled(key string) {
	if p.list.cursor >= len(p.installed) {
		return
	}
	pl := p.installed[p.list.cursor]
	switch key {
	case " ", "space", "enter":
		reg, err := appPluginRegistry()
		if err != nil {
			p.err = err.Error()
			return
		}
		enable := !pluginEnabled(pl, p.rows, reg)
		ok, err := reg.SetEnabled(pl.Name, enable)
		if err != nil {
			p.err = err.Error()
			return
		}
		if !ok {
			p.err = "plugin no longer installed"
			return
		}
		state := "disabled"
		if enable {
			state = "enabled"
		}
		p.prompt.status = state + " plugin " + pl.Name + " (applies to new sessions)"
		p.err = ""
		p.loaded = false
		p.load()
	case "X", "delete":
		p.confirm.open("plugin", pl.Name)
	}
}

func (p *pluginsState) removePlugin(name string) {
	reg, err := appPluginRegistry()
	if err != nil {
		p.err = err.Error()
		return
	}
	ok, err := reg.Uninstall(name)
	if err != nil {
		p.err = err.Error()
		return
	}
	if !ok {
		p.err = "plugin no longer installed"
		return
	}
	p.prompt.status = "deleted plugin " + name
	p.err = ""
	p.loaded = false
	p.load()
}

func (p *pluginsState) updateMarketplace(m *Model, key string) tea.Cmd {
	if p.list.cursor >= len(p.markets) {
		return nil
	}
	mk := p.markets[p.list.cursor]
	switch key {
	case "U":
		// Shift+U pulls the marketplace and overwrites any installed plugins from it,
		// then shows only still-uninstalled catalog entries.
		return p.updateMarketplaceFromRemote(m, mk, true)
	case "enter":
		if strings.EqualFold(p.catalogMarket, mk.Name) && len(p.catalog) > 0 {
			p.focusCatalog()
		} else {
			return p.refreshMarketplace(mk, true)
		}
	case " ", "space":
		p.setMarketplaceEnabled(mk.Name, mk.Disabled)
	case "X", "delete":
		p.confirm.open("marketplace", mk.Name)
	}
	return nil
}

func (p *pluginsState) setMarketplaceEnabled(name string, enabled bool) {
	reg, err := appPluginRegistry()
	if err != nil {
		p.err = err.Error()
		return
	}
	ok, err := reg.SetMarketEnabled(name, enabled)
	if err != nil {
		p.err = err.Error()
		return
	}
	if !ok {
		p.err = "marketplace no longer present"
		return
	}
	state := "disabled"
	if enabled {
		state = "enabled"
	}
	p.prompt.status = state + " marketplace " + name
	p.err = ""
	p.loaded = false
	p.load()
}

func (p *pluginsState) removeMarketplace(name string) {
	reg, err := appPluginRegistry()
	if err != nil {
		p.err = err.Error()
		return
	}
	ok, err := reg.RemoveMarket(name)
	if err != nil {
		p.err = err.Error()
		return
	}
	if !ok {
		p.err = "marketplace no longer present"
		return
	}
	if strings.EqualFold(p.catalogMarket, name) {
		p.catalogMarket = ""
		p.catalog = nil
	}
	p.prompt.status = "deleted marketplace " + name + " (installed plugins unaffected)"
	p.err = ""
	p.loaded = false
	p.load()
}

func (p *pluginsState) refreshMarketplace(mk pluginpkg.MarketRecord, focus bool) tea.Cmd {
	p.prompt.startBusy("marketplace", "marketplace", mk.Name, "refreshing marketplace "+mk.Name+" … (fetching catalog)")
	return func() tea.Msg {
		reg, err := appPluginRegistry()
		if err != nil {
			return marketplaceRefreshDoneMsg{marketName: mk.Name, status: "refresh failed: " + err.Error()}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		mkt, rec, err := reg.AddMarketplace(ctx, mk.Source, nil)
		if err != nil {
			return marketplaceRefreshDoneMsg{marketName: mk.Name, status: "refresh failed: " + err.Error()}
		}
		return marketplaceRefreshDoneMsg{
			marketName: rec.Name,
			status:     fmt.Sprintf("refreshed %s: %d total", rec.Name, len(mkt.Plugins)),
			catalog:    append([]pluginpkg.PluginEntry(nil), mkt.Plugins...),
			focus:      focus,
		}
	}
}

func (p *pluginsState) updateMarketplaceFromRemote(m *Model, mk pluginpkg.MarketRecord, focus bool) tea.Cmd {
	if mk.Disabled {
		p.prompt.status = "marketplace " + mk.Name + " is disabled (enable it first)"
		return nil
	}
	p.prompt.startBusy("marketplace", "marketplace", mk.Name, "pulling marketplace "+mk.Name+" … (updating installed plugins)")
	data := m.data
	return func() tea.Msg {
		name, status, catalog := runMarketplaceUpdate(data, mk)
		return marketplaceRefreshDoneMsg{marketName: name, status: status, catalog: catalog, focus: focus}
	}
}

func (p *pluginsState) focusCatalog() {
	p.catalogList.count = len(p.catalog)
	p.catalogList.clamp()
	p.catalogFocus = len(p.catalog) > 0
	p.err = ""
}

func (p *pluginsState) updateCatalog(m *Model, key string) (bool, tea.Cmd) {
	if len(p.catalog) == 0 {
		p.catalogFocus = false
		return false, nil
	}
	switch key {
	case "esc", "backspace", "left", "H":
		p.catalogFocus = false
		m.contentScroll = 0
		return true, nil
	case "pgdown", "ctrl+d", "ctrl+f":
		m.scrollContent(max(1, m.computeLayout().inner.h/2))
		return true, nil
	case "pgup", "ctrl+u", "ctrl+b":
		m.scrollContent(-max(1, m.computeLayout().inner.h/2))
		return true, nil
	case "home":
		m.contentScroll = 0
		return true, nil
	case "end":
		m.scrollContent(1 << 20)
		return true, nil
	case " ", "space":
		p.toggleSelectedCatalogPlugin()
		return true, nil
	case "enter":
		return true, p.installSelectedCatalogPlugin(m)
	case "i":
		return true, p.installMarkedCatalogPlugins(m)
	case "v":
		return true, p.previewSelectedCatalogPlugin()
	}
	visible := p.catalogVisibleRows(m.height)
	before := p.catalogList.cursor
	if p.catalogList.key(key, visible) {
		if p.catalogList.cursor != before {
			p.prompt.status = ""
			m.contentScroll = 0
		}
		return true, nil
	}
	// While focused inside the catalog, consume stray keys so page-jump/destructive
	// marketplace shortcuts do not fire accidentally.
	return true, nil
}

func (p *pluginsState) selectedCatalogEntry() (pluginpkg.PluginEntry, bool) {
	if p.catalogList.cursor < 0 || p.catalogList.cursor >= len(p.catalog) {
		return pluginpkg.PluginEntry{}, false
	}
	return p.catalog[p.catalogList.cursor], true
}

func catalogEntryKey(e pluginpkg.PluginEntry) string {
	return strings.ToLower(strings.TrimSpace(e.Name))
}

func catalogPreviewKey(name, market string) string {
	return strings.ToLower(strings.TrimSpace(name)) + "@" + strings.ToLower(strings.TrimSpace(market))
}

func (p *pluginsState) toggleSelectedCatalogPlugin() {
	p.prompt.status = ""
	entry, ok := p.selectedCatalogEntry()
	if !ok || strings.TrimSpace(entry.Name) == "" {
		return
	}
	if p.catalogSelected == nil {
		p.catalogSelected = map[string]bool{}
	}
	k := catalogEntryKey(entry)
	if p.catalogSelected[k] {
		delete(p.catalogSelected, k)
	} else {
		p.catalogSelected[k] = true
	}
}

func (p *pluginsState) markedCatalogPlugins() []string {
	if len(p.catalogSelected) == 0 {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for _, e := range p.catalog {
		k := catalogEntryKey(e)
		if p.catalogSelected[k] && !seen[k] {
			names = append(names, e.Name)
			seen[k] = true
		}
	}
	return names
}

func (p *pluginsState) catalogMarketName() string {
	market := p.catalogMarket
	if market == "" && p.list.cursor < len(p.markets) {
		market = p.markets[p.list.cursor].Name
	}
	return market
}

func (p *pluginsState) previewSelectedCatalogPlugin() tea.Cmd {
	entry, ok := p.selectedCatalogEntry()
	if !ok {
		return nil
	}
	market := p.catalogMarketName()
	key := catalogPreviewKey(entry.Name, market)
	p.prompt.startBusy("plugin", "plugin", entry.Name, "previewing plugin "+entry.Name+" … (fetching manifest)")
	return func() tea.Msg {
		pv, err := runPluginPreview(entry.Name, market)
		if err != nil {
			return pluginPreviewDoneMsg{key: key, err: "preview failed: " + err.Error()}
		}
		return pluginPreviewDoneMsg{key: key, preview: pv}
	}
}

func (p *pluginsState) installSelectedCatalogPlugin(m *Model) tea.Cmd {
	entry, ok := p.selectedCatalogEntry()
	if !ok {
		return nil
	}
	market := p.catalogMarketName()
	label := entry.Name
	if market != "" {
		label += "@" + market
	}
	p.prompt.startBusy("plugin", "plugin", label, "installing plugin "+label+" … (scanning + fetching)")
	data := m.data
	return func() tea.Msg {
		return installDoneMsg{page: PagePlugins, kind: "plugin", tab: pluginsTabInstalled, status: runPluginInstallFrom(data, entry.Name, market)}
	}
}

func (p *pluginsState) installMarkedCatalogPlugins(m *Model) tea.Cmd {
	names := p.markedCatalogPlugins()
	if len(names) == 0 {
		return p.installSelectedCatalogPlugin(m)
	}
	market := p.catalogMarketName()
	label := fmt.Sprintf("%d plugins", len(names))
	if market != "" {
		label += "@" + market
	}
	p.prompt.startBusy("plugin", "plugin", label, fmt.Sprintf("installing %d selected plugin(s) … (scanning + fetching)", len(names)))
	data := m.data
	return func() tea.Msg {
		return installDoneMsg{page: PagePlugins, kind: "plugin", tab: pluginsTabInstalled, status: runPluginBatchInstall(data, names, market)}
	}
}

func (p *pluginsState) catalogVisibleRows(h int) int {
	v := h - 18
	if v < 4 {
		return 4
	}
	if v > 10 {
		return 10
	}
	return v
}

func (p *pluginsState) updateExtension(key string) {
	if p.list.cursor >= len(p.rows) {
		return
	}
	switch key {
	case " ", "space", "enter":
		// Toggle the selected extension on/off (persists "disabled": true in its
		// config file; applies to NEW sessions — running ones keep connected servers).
		r := p.rows[p.list.cursor]
		if _, err := toggleDisabled(r.Path, r.Kind, r.Index); err != nil {
			p.err = err.Error()
		} else {
			p.err = ""
			p.loaded = false
			p.load()
		}
	case "X", "delete":
		p.confirmUninstallSelected()
	}
}

// confirmUninstallSelected asks to remove the plugin that owns the selected
// extension row, if it's a plugin-installed component (matched by the
// "<plugin>-" name prefix against the installed-plugins registry).
func (p *pluginsState) confirmUninstallSelected() {
	if p.list.cursor >= len(p.rows) {
		return
	}
	name := p.rows[p.list.cursor].Name
	reg, err := appPluginRegistry()
	if err != nil {
		p.err = err.Error()
		return
	}
	installed, _ := reg.Installed()
	for _, pl := range installed {
		if strings.HasPrefix(name, pl.Name+"-") || name == pl.Name {
			p.confirm.open("plugin", pl.Name)
			return
		}
	}
	p.err = "not a plugin-installed extension (only plugins can be deleted here)"
}

func (p *pluginsState) view(m *Model, w, h int) string {
	p.load()
	out := pageTitle("plugins", "Plugins make Eigen work your way.", w)
	out += p.hero(w)
	out += p.tabs(w) + "\n"
	p.clicks.reset()
	switch p.tab {
	case pluginsTabInstalled:
		out += p.viewInstalled(w, h-lineCount(out))
	case pluginsTabMarketplace:
		out += p.viewMarketplace(w, h-lineCount(out))
	case pluginsTabExtensions:
		out += p.viewExtensions(w, h-lineCount(out))
	}
	return out
}

func (p *pluginsState) hero(w int) string {
	enabled := 0
	reg, _ := appPluginRegistry()
	for _, pl := range p.installed {
		if pluginEnabled(pl, p.rows, reg) {
			enabled++
		}
	}
	stats := []string{
		fmt.Sprintf("%d installed", len(p.installed)),
		fmt.Sprintf("%d enabled", enabled),
		fmt.Sprintf("%d marketplaces", len(p.markets)),
		fmt.Sprintf("%d MCP/hook wiring", len(p.rows)),
	}
	sub := "  " + sText.Render(strings.Join(stats, sFaint.Render(" · ")))
	if w < 72 {
		return sub + "\n\n"
	}
	return sub + "\n" + sFaint.Render("  Claude/Codex plugins: skills, agents, commands, MCP servers, and hooks.") + "\n\n"
}

func (p *pluginsState) tabs(w int) string {
	tabs := []struct {
		tab   pluginsTab
		name  string
		count int
	}{
		{pluginsTabInstalled, "Plugins", len(p.installed)},
		{pluginsTabMarketplace, "Marketplace", len(p.markets)},
		{pluginsTabExtensions, "Wiring", len(p.rows)},
	}
	var parts []string
	for i, t := range tabs {
		label := fmt.Sprintf("%d %s %d", i+1, t.name, t.count)
		if p.tab == t.tab {
			parts = append(parts, sAccent.Render("[ ")+sTitle.Render(label)+sAccent.Render(" ]"))
		} else {
			parts = append(parts, sFaint.Render("  ")+sDim.Render(label)+sFaint.Render("  "))
		}
	}
	line := "  " + strings.Join(parts, "  ")
	if lipgloss.Width(line) > w {
		line = truncate(line, w)
	}
	return line + "\n" + sFaint.Render("  1/2/3 tabs · ←/→ or H/L switch · j/k move")
}

func (p *pluginsState) viewInstalled(w, h int) string {
	out := sectionLabel("my plugins", w) + "\n"
	if len(p.installed) == 0 {
		out += emptyCard("No plugins installed yet", "Add a marketplace, then install a plugin from it.", w)
		out += p.prompt.render()
		out += "\n" + sFaint.Render("  a add marketplace · i install plugin · R refresh")
		return out
	}
	visible := max(1, (h-6)/2)
	from, to := p.list.window(visible)
	reg, _ := appPluginRegistry()
	for i := from; i < to; i++ {
		pl := p.installed[i]
		p.clicks.mark(lineCount(out), i)
		out += p.pluginCard(i == p.list.cursor, pl, reg, w) + "\n"
	}
	if p.list.cursor < len(p.installed) {
		out += "\n" + p.pluginDetail(p.installed[p.list.cursor], reg, w)
	}
	out += p.confirm.render(w)
	if p.err != "" {
		out += sErr.Render("  "+truncate(p.err, w-4)) + "\n"
	}
	out += p.prompt.render()
	out += "\n" + sFaint.Render("  enter/space toggle · i install · X delete · a add marketplace · R refresh")
	return out
}

func (p *pluginsState) pluginCard(selected bool, pl pluginpkg.InstalledPlugin, reg *pluginpkg.Registry, w int) string {
	status := sOk.Render("enabled")
	if reg != nil && !pluginEnabled(pl, p.rows, reg) {
		status = sDim.Render("disabled")
	}
	meta := pluginCounts(pl)
	if pl.Marketplace != "" {
		meta += " · from " + pl.Marketplace
	}
	if pl.Version != "" {
		meta += " · v" + pl.Version
	}
	name := sText.Render(pad(truncate(pl.Name, 24), 24))
	meta = truncate(meta, max(10, w-42))
	line1 := "◆ " + name + " " + status + "  " + sViolet.Render(meta)
	if selected {
		line1 = row(true, line1)
	} else {
		line1 = row(false, line1)
	}
	desc := pl.Description
	if desc == "" {
		desc = "no description"
	}
	line2 := "  " + sDim.Render(truncate(desc, max(20, w-6)))
	return line1 + "\n" + line2
}

func (p *pluginsState) pluginDetail(pl pluginpkg.InstalledPlugin, reg *pluginpkg.Registry, w int) string {
	var b strings.Builder
	b.WriteString(sectionLabel("details", w) + "\n")
	if pl.Description != "" {
		b.WriteString("  " + sText.Render(wrapTo(pl.Description, w-4, "  ")) + "\n")
	}
	state := "enabled"
	if reg != nil && !pluginEnabled(pl, p.rows, reg) {
		state = "disabled"
	}
	b.WriteString("  " + sDim.Render("state ") + state + sFaint.Render(" · ") + sDim.Render("root ") + truncate(pl.Root, max(12, w-20)) + "\n")
	if pl.Version != "" {
		b.WriteString("  " + sDim.Render("version ") + pl.Version + "\n")
	}
	parts := pluginComponentNames(pl)
	if len(parts) > 0 {
		b.WriteString("  " + sDim.Render("components ") + truncate(strings.Join(parts, " · "), max(20, w-15)) + "\n")
	}
	if pv := installedPluginPreview(pl); pv != "" {
		b.WriteString(pv)
	}
	for _, warn := range pl.Warnings {
		b.WriteString("  " + sWarn.Render("warning ") + sDim.Render(truncate(warn, max(20, w-12))) + "\n")
	}
	if len(pl.Scans) > 0 {
		b.WriteString("\n" + sectionLabel("scan flags", w) + "\n")
		for _, sf := range pl.Scans {
			b.WriteString("  " + sWarn.Render(sf.Component) + "\n")
			for _, reason := range sf.Reasons {
				b.WriteString("    - " + sDim.Render(truncate(reason, max(20, w-8))) + "\n")
			}
		}
	}
	return b.String()
}

func installedPluginPreview(pl pluginpkg.InstalledPlugin) string {
	if strings.TrimSpace(pl.Root) == "" {
		return ""
	}
	comps, err := pluginpkg.Discover(pl.Root, true)
	if err != nil || comps == nil || comps.Manifest == nil {
		return ""
	}
	m := comps.Manifest
	var b strings.Builder
	if m.DisplayName != "" && m.DisplayName != pl.Name {
		b.WriteString("  " + sDim.Render("manifest ") + m.DisplayName + "\n")
	}
	if m.Repository != "" || m.Homepage != "" || m.License != "" {
		var bits []string
		if m.Repository != "" {
			bits = append(bits, "repo "+m.Repository)
		}
		if m.Homepage != "" {
			bits = append(bits, "home "+m.Homepage)
		}
		if m.License != "" {
			bits = append(bits, "license "+m.License)
		}
		b.WriteString("  " + sDim.Render("manifest ") + strings.Join(bits, " · ") + "\n")
	}
	return b.String()
}

func (p *pluginsState) viewMarketplace(w, h int) string {
	out := sectionLabel("marketplaces", w) + "\n"
	if len(p.markets) == 0 {
		out += emptyCard("No marketplaces added", "Add a Claude or Codex marketplace repo, local directory, or HTTPS marketplace.json.", w)
		out += p.prompt.render()
		out += "\n" + sFaint.Render("  a add marketplace · i install by name once added")
		return out
	}
	visible := max(1, (h-6)/2)
	from, to := p.list.window(visible)
	for i := from; i < to; i++ {
		mk := p.markets[i]
		p.clicks.mark(lineCount(out), i)
		out += p.marketCard(i == p.list.cursor, mk, w) + "\n"
	}
	if p.list.cursor < len(p.markets) {
		out += "\n" + p.marketDetail(p.markets[p.list.cursor], w, h-lineCount(out))
	}
	out += p.confirm.render(w)
	if p.err != "" {
		out += sErr.Render("  "+truncate(p.err, w-4)) + "\n"
	}
	out += p.prompt.render()
	if p.catalogFocus {
		out += "\n" + sFaint.Render("  j/k · v preview · space mark · i install marked · enter current · esc")
	} else {
		out += "\n" + sFaint.Render("  space enable/disable · a add marketplace · enter open catalog · Shift+U pull updates · i install by name · X delete")
	}
	return out
}

func (p *pluginsState) marketCard(selected bool, mk pluginpkg.MarketRecord, w int) string {
	installed := 0
	for _, pl := range p.installed {
		if strings.EqualFold(pl.Marketplace, mk.Name) {
			installed++
		}
	}
	stamp := dateLabel(mk.Updated)
	if stamp == "" {
		added := dateLabel(mk.Added)
		if added == "" {
			added = "unknown"
		}
		stamp = "added " + added
	} else {
		stamp = "updated " + stamp
	}
	state := sOk.Render("enabled")
	if mk.Disabled {
		state = sDim.Render("disabled")
	}
	line1 := "◇ " + sText.Render(pad(truncate(mk.Name, 24), 24)) + " " + state + "  " + sViolet.Render(fmt.Sprintf("%d installed", installed))
	if selected {
		line1 = row(true, line1)
	} else {
		line1 = row(false, line1)
	}
	line2 := "  " + sDim.Render(truncate(mk.Source+" · "+stamp, max(20, w-6)))
	return line1 + "\n" + line2
}

func (p *pluginsState) marketDetail(mk pluginpkg.MarketRecord, w, h int) string {
	var b strings.Builder
	b.WriteString(sectionLabel("catalog", w) + "\n")
	if !p.catalogFocus {
		if mk.Owner != "" {
			b.WriteString("  " + sDim.Render("owner ") + mk.Owner + "\n")
		}
		state := "enabled"
		if mk.Disabled {
			state = "disabled (not searched by installs/update-all)"
		}
		b.WriteString("  " + sDim.Render("state ") + state + "\n")
		b.WriteString("  " + sDim.Render("source ") + truncate(mk.Source, max(12, w-12)) + "\n")
		b.WriteString("  " + sDim.Render("install ") + "/plugin install <name>@" + mk.Name + "\n")
	}
	if strings.EqualFold(p.catalogMarket, mk.Name) {
		if len(p.catalog) == 0 {
			b.WriteString("  " + sFaint.Render("catalog refreshed; no plugins listed") + "\n")
		} else {
			marked := len(p.markedCatalogPlugins())
			label := fmt.Sprintf("%d plugins", len(p.catalog))
			if marked > 0 {
				label += fmt.Sprintf(" · %d marked", marked)
			}
			b.WriteString("\n" + sectionLabel(label, w) + "\n")
			p.catalogList.count = len(p.catalog)
			visible := p.catalogVisibleRows(h)
			from, to := p.catalogList.window(visible)
			for i := from; i < to; i++ {
				e := p.catalog[i]
				mark := sFaint.Render("○ ")
				if p.catalogSelected[catalogEntryKey(e)] {
					mark = sOk.Render("● ")
				}
				line := mark + sText.Render(pad(truncate(e.Name, 22), 22))
				meta := catalogEntryMeta(e)
				if meta != "" {
					line += " " + sViolet.Render(truncate(meta, max(10, w-28)))
				}
				if p.catalogFocus && i == p.catalogList.cursor {
					b.WriteString(row(true, line) + "\n")
				} else {
					b.WriteString(row(false, line) + "\n")
				}
				if e.Description != "" && (!p.catalogFocus || i == p.catalogList.cursor) {
					b.WriteString("  " + sDim.Render(truncate(e.Description, max(10, w-6))) + "\n")
				}
			}
			if from > 0 || to < len(p.catalog) {
				b.WriteString(sFaint.Render(fmt.Sprintf("  showing %d-%d of %d", from+1, to, len(p.catalog))) + "\n")
			}
			if p.catalogFocus && p.catalogList.cursor < len(p.catalog) {
				sel := p.catalog[p.catalogList.cursor]
				if p.catalogPreview != nil && p.catalogPreviewKey == catalogPreviewKey(sel.Name, mk.Name) {
					b.WriteString("\n" + p.pluginPreviewBlock(p.catalogPreview, w))
				}
			}
			if !p.catalogFocus {
				b.WriteString("  " + sFaint.Render("enter focus · Shift+U update installed plugins") + "\n")
			}
		}
	} else {
		b.WriteString("  " + sFaint.Render("enter fetches this catalog; Shift+U updates installed plugins too") + "\n")
	}
	if !p.catalogFocus {
		b.WriteString("  " + sFaint.Render("installed plugins are not removed when a marketplace is removed") + "\n")
	}
	return b.String()
}

func (p *pluginsState) viewExtensions(w, h int) string {
	out := sectionLabel("wired components", w) + "\n"
	if len(p.rows) == 0 {
		out += emptyCard("No extension wiring found", "MCP servers, plugin tools, LSP servers, and hooks appear here.", w)
		out += p.prompt.render()
		out += "\n" + sFaint.Render("  a add-marketplace · i install-plugin")
		return out
	}
	visible := max(3, h-6)
	from, to := p.list.window(visible)
	for i := from; i < to; i++ {
		r := p.rows[i]
		p.clicks.mark(lineCount(out), i)
		cursor := "  "
		if i == p.list.cursor {
			cursor = sAccent.Render("▎ ")
		}
		dot := sOk.Render("●")
		nameStyle, kstyle := sText, kindStyle(r.Kind)
		if r.Disabled {
			dot = sFaint.Render("○")
			nameStyle, kstyle = sFaint, sFaint
		}
		kind := kstyle.Render(pad(r.Kind, 8))
		name := nameStyle.Render(pad(truncate(r.Name, 28), 30))
		srcCol := sDim.Render(pad(r.Source, 9))
		out += cursor + dot + " " + kind + name + srcCol + "\n"
	}
	if i := p.list.cursor; i < len(p.rows) {
		out += "\n" + sFaint.Render("  "+truncate(p.rows[i].Detail, w-6)) + "\n"
	}
	out += p.confirm.render(w)
	if p.err != "" {
		out += sErr.Render("  "+truncate(p.err, w-4)) + "\n"
	}
	out += p.prompt.render()
	out += sFaint.Render("  space toggle component · X delete owning plugin · a add-marketplace · i install-plugin · R refresh")
	return out
}

// clickAt selects a rendered card/row. A second click on the selected row
// performs the primary action for the current tab (toggle for plugins/wiring,
// refresh for marketplace), matching enter.
func (p *pluginsState) clickAt(m *Model, localY int) (tea.Cmd, bool) {
	idx, ok := p.clicks.at(localY)
	if !ok || idx < 0 || idx >= p.list.count {
		return nil, false
	}
	if p.list.cursor == idx {
		key := "enter"
		switch p.tab {
		case pluginsTabInstalled:
			p.updateInstalled(key)
		case pluginsTabMarketplace:
			return p.updateMarketplace(m, key), true
		case pluginsTabExtensions:
			p.updateExtension(key)
		}
	} else {
		p.list.cursor = idx
		p.err = ""
	}
	return nil, true
}

func minPluginTab(tab pluginsTab) pluginsTab {
	if tab > pluginsTabExtensions {
		return pluginsTabExtensions
	}
	return tab
}

func maxPluginTab(tab pluginsTab) pluginsTab {
	if tab < pluginsTabInstalled {
		return pluginsTabInstalled
	}
	return tab
}

func pluginCounts(pl pluginpkg.InstalledPlugin) string {
	var parts []string
	if len(pl.Skills) > 0 {
		parts = append(parts, fmt.Sprintf("%d skill", len(pl.Skills)))
	}
	if len(pl.Agents) > 0 {
		parts = append(parts, fmt.Sprintf("%d agent", len(pl.Agents)))
	}
	if len(pl.Commands) > 0 {
		parts = append(parts, fmt.Sprintf("%d cmd", len(pl.Commands)))
	}
	if len(pl.MCPServers) > 0 {
		parts = append(parts, fmt.Sprintf("%d mcp", len(pl.MCPServers)))
	}
	if pl.Hooks > 0 {
		parts = append(parts, fmt.Sprintf("%d hook", pl.Hooks))
	}
	if len(parts) == 0 {
		return "metadata only"
	}
	return strings.Join(parts, " · ")
}

func pluginComponentNames(pl pluginpkg.InstalledPlugin) []string {
	var parts []string
	if len(pl.Skills) > 0 {
		parts = append(parts, "skills: "+strings.Join(pl.Skills, ", "))
	}
	if len(pl.Agents) > 0 {
		parts = append(parts, "agents: "+strings.Join(pl.Agents, ", "))
	}
	if len(pl.Commands) > 0 {
		parts = append(parts, "commands: "+strings.Join(pl.Commands, ", "))
	}
	if len(pl.MCPServers) > 0 {
		parts = append(parts, "mcp: "+strings.Join(pl.MCPServers, ", "))
	}
	if pl.Hooks > 0 {
		parts = append(parts, fmt.Sprintf("%d hook(s)", pl.Hooks))
	}
	return parts
}

func (p *pluginsState) pluginPreviewBlock(pv *pluginpkg.PluginPreview, w int) string {
	if pv == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(sectionLabel("manifest preview · enter install · esc back", w) + "\n")
	name := pv.Entry.Name
	version := pv.Entry.Version
	desc := pv.Entry.Description
	if pv.Manifest != nil {
		name = firstNonEmptyApp(pv.Manifest.DisplayName, firstNonEmptyApp(pv.Manifest.Name, name))
		version = firstNonEmptyApp(pv.Manifest.Version, version)
		desc = firstNonEmptyApp(pv.Manifest.Description, firstNonEmptyApp(pv.Manifest.Interface.ShortDescription, desc))
	}
	b.WriteString("  " + sText.Render(name))
	if version != "" {
		b.WriteString(" " + sViolet.Render("v"+version))
	}
	b.WriteString("\n")
	if desc != "" {
		b.WriteString("  " + sDim.Render(truncate(desc, max(20, w-6))) + "\n")
	}
	b.WriteString("  " + sDim.Render("will install ") + pluginPreviewCounts(pv) + "\n")
	if len(pv.Skills) > 0 {
		b.WriteString("  " + sDim.Render("skills ") + truncate(strings.Join(pv.Skills, ", "), max(20, w-10)) + "\n")
	}
	if len(pv.Agents) > 0 {
		b.WriteString("  " + sDim.Render("agents ") + truncate(strings.Join(pv.Agents, ", "), max(20, w-10)) + "\n")
	}
	if len(pv.Commands) > 0 {
		b.WriteString("  " + sDim.Render("commands ") + truncate(strings.Join(pv.Commands, ", "), max(20, w-12)) + "\n")
	}
	for _, wmsg := range pv.Warnings {
		b.WriteString("  " + sWarn.Render("warning ") + sDim.Render(truncate(wmsg, max(20, w-12))) + "\n")
	}
	return b.String()
}

func pluginPreviewCounts(pv *pluginpkg.PluginPreview) string {
	var parts []string
	if len(pv.Skills) > 0 {
		parts = append(parts, fmt.Sprintf("%d skill(s)", len(pv.Skills)))
	}
	if len(pv.Agents) > 0 {
		parts = append(parts, fmt.Sprintf("%d agent(s)", len(pv.Agents)))
	}
	if len(pv.Commands) > 0 {
		parts = append(parts, fmt.Sprintf("%d command(s)", len(pv.Commands)))
	}
	if len(pv.MCPServers) > 0 {
		parts = append(parts, fmt.Sprintf("%d MCP server(s)", len(pv.MCPServers)))
	}
	if pv.Hooks > 0 {
		parts = append(parts, fmt.Sprintf("%d hook(s)", pv.Hooks))
	}
	if pv.Apps > 0 {
		parts = append(parts, fmt.Sprintf("%d app(s)", pv.Apps))
	}
	if len(parts) == 0 {
		return "metadata only"
	}
	return strings.Join(parts, ", ")
}

func firstNonEmptyApp(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func catalogEntryMeta(e pluginpkg.PluginEntry) string {
	var parts []string
	if e.Category != "" {
		parts = append(parts, e.Category)
	}
	if e.Version != "" {
		parts = append(parts, "v"+e.Version)
	}
	if len(e.Keywords) > 0 {
		parts = append(parts, strings.Join(e.Keywords[:min(len(e.Keywords), 3)], ","))
	}
	return strings.Join(parts, " · ")
}

func pluginEnabled(pl pluginpkg.InstalledPlugin, rows []ExtRow, reg *pluginpkg.Registry) bool {
	seen := false
	active := false
	for _, r := range rows {
		owned := strings.HasPrefix(r.Name, pl.Name+"-") || r.Name == pl.Name
		if !owned && pl.Root != "" && strings.Contains(r.Detail, pl.Root) {
			owned = true
		}
		if !owned {
			continue
		}
		seen = true
		if !r.Disabled {
			active = true
		}
	}
	if active {
		return true
	}
	if reg == nil {
		return !seen
	}
	for _, sd := range pl.Skills {
		if _, err := os.Stat(filepath.Join(reg.SkillsDir(), sd, "SKILL.md")); err == nil {
			return true
		}
	}
	for _, cn := range pl.Commands {
		if _, err := os.Stat(filepath.Join(reg.CommandsDir(), cn+".md")); err == nil {
			return true
		}
	}
	for _, an := range pl.Agents {
		if _, err := os.Stat(filepath.Join(reg.SkillsDir(), an, "SKILL.md")); err == nil {
			return true
		}
	}
	if seen || len(pl.Skills) > 0 || len(pl.Agents) > 0 || len(pl.Commands) > 0 || len(pl.MCPServers) > 0 || pl.Hooks > 0 {
		return false
	}
	// If there are no inspectable components, treat the record as enabled: the
	// registry has no explicit disabled bit, and metadata-only plugins should not
	// look broken by default.
	return true
}

func emptyCard(title, body string, w int) string {
	if w < 20 {
		w = 20
	}
	return "  " + sTitle.Render(title) + "\n" + "  " + sDim.Render(wrapTo(body, w-4, "  ")) + "\n"
}

func dateLabel(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// kindStyle colors an extension kind.
func kindStyle(kind string) lipgloss.Style {
	switch kind {
	case "mcp":
		return sViolet
	case "plugin":
		return sOk
	case "lsp":
		return sAccent
	case "hook":
		return sWarn
	}
	return sDim
}
