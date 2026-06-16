package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// pluginsState: the extensions page (MCP, plugins, LSP, hooks) — browse, toggle,
// and install marketplaces/plugins inline.
type pluginsState struct {
	list   list
	rows   []ExtRow
	loaded bool
	err    string // last toggle error ("" = none)
	prompt installPrompt
}

func (p *pluginsState) init(*Data) {}

func (p *pluginsState) load() {
	if p.loaded {
		return
	}
	p.rows = loadExtensions()
	p.list.count = len(p.rows)
	p.loaded = true
}

func (p *pluginsState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p.load()
	key := msg.String()

	// Inline install prompt active: capture text input.
	if p.prompt.active {
		if src, ok := p.prompt.key(key, msg.Runes); ok {
			p.prompt.busy = true
			var status string
			switch p.prompt.kind {
			case "marketplace":
				status = runMarketplaceAdd(m.data, src)
			case "plugin":
				status = runPluginInstall(m.data, src)
			}
			p.prompt.busy = false
			p.prompt.close()
			p.prompt.status = status
			p.loaded = false
			p.load()
		}
		return m, nil
	}

	if p.list.key(key, m.height-6) {
		return m, nil
	}
	switch key {
	case "R": // manual refresh (capital: bare letters are page-jumps)
		p.loaded = false
		p.load()
	case "a": // add a marketplace catalog
		p.prompt.open("marketplace", "marketplace (owner/repo[/sub][@ref])")
	case "i": // install a plugin by name from any added marketplace
		p.prompt.open("plugin", "plugin name to install")
	case "X", "delete": // uninstall the selected plugin-installed extension's owning plugin
		p.uninstallSelected(m)
	case " ", "enter":
		// Toggle the selected extension on/off (persists "disabled": true in
		// its config file; applies to NEW sessions — running ones keep their
		// already-connected servers).
		if p.list.cursor < len(p.rows) {
			r := p.rows[p.list.cursor]
			if _, err := toggleDisabled(r.Path, r.Kind, r.Index); err != nil {
				p.err = err.Error()
			} else {
				p.err = ""
				p.loaded = false
				p.load()
			}
		}
	}
	return m, nil
}

// uninstallSelected removes the plugin that owns the selected extension row, if
// it's a plugin-installed component (matched by the "<plugin>-" name prefix
// against the installed-plugins registry).
func (p *pluginsState) uninstallSelected(m *Model) {
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
			if _, err := reg.Uninstall(pl.Name); err != nil {
				p.err = err.Error()
			} else {
				p.prompt.status = "removed plugin " + pl.Name
				p.loaded = false
				p.load()
			}
			return
		}
	}
	p.err = "not a plugin-installed extension (only plugins can be uninstalled here)"
}

func (p *pluginsState) view(m *Model, w, h int) string {
	p.load()
	out := pageTitle("plugins", "mcp servers · plugin tools · lsp · hooks", w)
	if len(p.rows) == 0 {
		out += "\n" + sDim.Render("  no extensions configured") + "\n"
		out += sFaint.Render("  add: ~/.eigen/{mcp,plugins,lsp,hooks}.json (or per-project .eigen/)") + "\n"
		out += p.prompt.render()
		out += "\n" + sFaint.Render("  a add-marketplace · i install-plugin")
		return out
	}
	visible := h - 6
	if visible < 3 {
		visible = 3
	}
	start := p.list.top
	for i := start; i < len(p.rows) && i < start+visible; i++ {
		r := p.rows[i]
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
	if p.err != "" {
		out += sErr.Render("  "+truncate(p.err, w-4)) + "\n"
	}
	out += p.prompt.render()
	out += sFaint.Render("  space toggle · a add-marketplace · i install-plugin · X uninstall · R refresh")
	return out
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
