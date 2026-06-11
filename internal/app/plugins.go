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
	Kind   string // "mcp" | "plugin" | "lsp" | "hook"
	Name   string
	Detail string // command / event / etc.
	Source string // which config file declared it
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
			Name    string   `json:"name"`
			Command []string `json:"command"`
			Tools   []string `json:"tools"`
		} `json:"servers"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	var rows []ExtRow
	for _, s := range cfg.Servers {
		detail := strings.Join(s.Command, " ")
		if n := len(s.Tools); n > 0 {
			detail += fmt.Sprintf(" · %d tools allowlisted", n)
		}
		rows = append(rows, ExtRow{Kind: "mcp", Name: s.Name, Detail: detail, Source: src})
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
	}
	if json.Unmarshal(data, &specs) != nil {
		return nil
	}
	var rows []ExtRow
	for _, p := range specs {
		detail := strings.Join(p.Command, " ")
		if p.ReadOnly {
			detail += " · read-only"
		}
		rows = append(rows, ExtRow{Kind: "plugin", Name: p.Name, Detail: detail, Source: src})
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
		} `json:"servers"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	var rows []ExtRow
	for _, s := range cfg.Servers {
		detail := strings.Join(s.Command, " ")
		if len(s.Languages) > 0 {
			detail += " · " + strings.Join(s.Languages, ",")
		}
		rows = append(rows, ExtRow{Kind: "lsp", Name: s.Name, Detail: detail, Source: src})
	}
	return rows
}

func loadHookRows(path, src string) []ExtRow {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var specs []struct {
		Event   string   `json:"event"`
		Command []string `json:"command"`
		Tool    string   `json:"tool"`
	}
	if json.Unmarshal(data, &specs) != nil {
		var wrap struct {
			Hooks []struct {
				Event   string   `json:"event"`
				Command []string `json:"command"`
				Tool    string   `json:"tool"`
			} `json:"hooks"`
		}
		if json.Unmarshal(data, &wrap) != nil {
			return nil
		}
		specs = wrap.Hooks
	}
	var rows []ExtRow
	for _, h := range specs {
		name := h.Event
		if h.Tool != "" {
			name += ":" + h.Tool
		}
		rows = append(rows, ExtRow{Kind: "hook", Name: name, Detail: strings.Join(h.Command, " "), Source: src})
	}
	return rows
}

// pluginsState: the read-only extensions page (MCP, plugins, LSP, hooks).
type pluginsState struct {
	list   list
	rows   []ExtRow
	loaded bool
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
	if p.list.key(key, m.height-6) {
		return m, nil
	}
	if key == "R" { // manual refresh (capital: bare letters are page-jumps)
		p.loaded = false
		p.load()
	}
	return m, nil
}

func (p *pluginsState) view(m *Model, w, h int) string {
	p.load()
	out := pageTitle("plugins", "mcp servers · plugin tools · lsp · hooks", w)
	if len(p.rows) == 0 {
		out += "\n" + sDim.Render("  no extensions configured") + "\n"
		out += sFaint.Render("  add: ~/.eigen/{mcp,plugins,lsp,hooks}.json (or per-project .eigen/)")
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
		kind := kindStyle(r.Kind).Render(pad(r.Kind, 8))
		name := sText.Render(pad(truncate(r.Name, 28), 30))
		srcCol := sDim.Render(pad(r.Source, 9))
		out += cursor + kind + name + srcCol + "\n"
	}
	if i := p.list.cursor; i < len(p.rows) {
		out += "\n" + sFaint.Render("  "+truncate(p.rows[i].Detail, w-6)) + "\n"
	}
	out += sFaint.Render("  R refresh · read-only (edit the json configs)")
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
