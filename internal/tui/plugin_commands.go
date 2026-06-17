package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/plugin"
	"github.com/avifenesh/eigen/internal/skill"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	marketplaceTimeout = 90 * time.Second
	pluginTimeout      = 120 * time.Second
)

// pluginCommand is the TUI wrapper for the existing plugin registry/installer.
// It is only reachable from a user-typed slash command (not an agent tool), and
// is intentionally not marked safeWhileRunning: installing/removing extensions
// changes future session capabilities and should not race an in-flight turn.
func (m *model) pluginCommand(arg string) tea.Cmd {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		m.note("usage: /plugin list | install <name>[@marketplace] [--force] [--overwrite] [--no-scan] | remove <name> | enable <name> | disable <name>")
		return nil
	}
	reg, err := plugin.NewRegistry()
	if err != nil {
		m.commandError("plugin: " + err.Error())
		return nil
	}
	sub := fields[0]
	rest := fields[1:]
	switch sub {
	case "list", "ls":
		m.pluginList(reg)
	case "install", "add":
		m.pluginInstall(reg, rest)
	case "remove", "rm", "uninstall":
		if len(rest) == 0 {
			m.note("usage: /plugin remove <name>")
			return nil
		}
		ok, err := reg.Uninstall(rest[0])
		if err != nil {
			m.commandError("plugin remove: " + err.Error())
			return nil
		}
		if !ok {
			m.note(fmt.Sprintf("no plugin %q installed", rest[0]))
			return nil
		}
		m.note(fmt.Sprintf("removed plugin %q (skills, commands, MCP servers, hooks, and bundle)", rest[0]))
	case "enable", "disable":
		if len(rest) == 0 {
			m.note("usage: /plugin " + sub + " <name>")
			return nil
		}
		enabled := sub == "enable"
		ok, err := reg.SetEnabled(rest[0], enabled)
		if err != nil {
			m.commandError("plugin " + sub + ": " + err.Error())
			return nil
		}
		if !ok {
			m.note(fmt.Sprintf("no plugin %q installed", rest[0]))
			return nil
		}
		state := "disabled"
		if enabled {
			state = "enabled"
		}
		m.note(fmt.Sprintf("%s plugin %q (applies to NEW sessions)", state, rest[0]))
	default:
		m.note("unknown /plugin subcommand " + sub + " (want: list | install | remove | enable | disable)")
	}
	return nil
}

func (m *model) pluginList(reg *plugin.Registry) {
	installed, err := reg.Installed()
	if err != nil {
		m.commandError("plugin list: " + err.Error())
		return
	}
	if len(installed) == 0 {
		m.note("no plugins installed (/plugin install <name>[@marketplace])")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d plugin(s) installed:", len(installed))
	for _, p := range installed {
		fmt.Fprintf(&b, "\n  • %s", p.Name)
		if p.Description != "" {
			b.WriteString(" — " + p.Description)
		}
		fmt.Fprintf(&b, "\n    %d skill(s), %d command(s), %d MCP server(s), %d hook(s)", len(p.Skills), len(p.Commands), len(p.MCPServers), p.Hooks)
		if p.Marketplace != "" {
			b.WriteString(" · from " + p.Marketplace)
		}
	}
	m.note(b.String())
}

func (m *model) pluginInstall(reg *plugin.Registry, args []string) {
	parsed, err := parsePluginInstallArgs(args)
	if err != nil {
		m.note("usage: /plugin install <name>[@marketplace] [--marketplace M] [--force] [--overwrite] [--no-scan]")
		return
	}
	opts := plugin.InstallOptions{Force: parsed.force, Overwrite: parsed.overwrite}
	if !parsed.noScan {
		scanner, err := m.pluginScanner()
		if err != nil {
			m.commandError("plugin install: scanner unavailable (use --no-scan to skip): " + err.Error())
			return
		}
		opts.Scanner = scanner
	}
	ctx, cancel := context.WithTimeout(context.Background(), pluginTimeout)
	defer cancel()
	res, err := reg.InstallPlugin(ctx, parsed.name, parsed.marketplace, opts)
	if err != nil {
		m.commandError("plugin install: " + err.Error())
		return
	}
	m.note(formatPluginInstallResult(res, opts.Scanner != nil))
}

func (m *model) pluginScanner() (skill.Scanner, error) {
	var p llm.Provider
	if m.backend != nil {
		p = m.backend.Provider()
	}
	if p == nil {
		if m.newProvider == nil {
			return nil, fmt.Errorf("provider unavailable")
		}
		var err error
		p, err = m.newProvider(m.provName, m.modelID)
		if err != nil {
			return nil, err
		}
	}
	return skill.ProviderScanner{P: p}, nil
}

func formatPluginInstallResult(res *plugin.InstallResult, scanned bool) string {
	p := res.Plugin
	var b strings.Builder
	switch {
	case len(res.Scans) > 0:
		fmt.Fprintf(&b, "⚠ installed %q despite scan flags", p.Name)
		for _, s := range res.Scans {
			fmt.Fprintf(&b, "\n  [%s]", s.Component)
			for _, r := range s.Reasons {
				b.WriteString("\n    - " + r)
			}
		}
	case scanned:
		fmt.Fprintf(&b, "✓ scan clean — installed %q", p.Name)
	default:
		fmt.Fprintf(&b, "installed %q (scan skipped)", p.Name)
	}
	fmt.Fprintf(&b, "\n  %d skill(s), %d command(s), %d MCP server(s), %d hook(s)", len(p.Skills), len(p.Commands), len(p.MCPServers), p.Hooks)
	for _, w := range res.Warnings {
		b.WriteString("\n  note: " + w)
	}
	if len(p.MCPServers) > 0 {
		b.WriteString("\n  MCP servers are niche (gated) — the agent unlocks them via search_tools.")
	}
	return b.String()
}

// marketplaceCommand is the TUI wrapper for marketplace registry operations.
func (m *model) marketplaceCommand(arg string) tea.Cmd {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		m.note("usage: /marketplace list | add <owner/repo[/sub][@ref]|url> | update [name] | remove <name>")
		return nil
	}
	reg, err := plugin.NewRegistry()
	if err != nil {
		m.commandError("marketplace: " + err.Error())
		return nil
	}
	sub := fields[0]
	rest := fields[1:]
	switch sub {
	case "list", "ls":
		m.marketplaceList(reg)
	case "add":
		if len(rest) == 0 {
			m.note("usage: /marketplace add <owner/repo[/sub][@ref] | url>")
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), marketplaceTimeout)
		defer cancel()
		mkt, rec, err := reg.AddMarketplace(ctx, rest[0], nil)
		if err != nil {
			m.commandError("marketplace add: " + err.Error())
			return nil
		}
		m.note(formatMarketplaceAdded(mkt, rec))
	case "remove", "rm":
		if len(rest) == 0 {
			m.note("usage: /marketplace remove <name>")
			return nil
		}
		ok, err := reg.RemoveMarket(rest[0])
		if err != nil {
			m.commandError("marketplace remove: " + err.Error())
			return nil
		}
		if !ok {
			m.note(fmt.Sprintf("no marketplace %q", rest[0]))
			return nil
		}
		m.note(fmt.Sprintf("removed marketplace %q (installed plugins are unaffected)", rest[0]))
	case "update", "refresh":
		target := ""
		if len(rest) > 0 {
			target = rest[0]
		}
		m.marketplaceUpdate(reg, target)
	default:
		m.note("unknown /marketplace subcommand " + sub + " (want: list | add | update | remove)")
	}
	return nil
}

func (m *model) marketplaceList(reg *plugin.Registry) {
	markets, err := reg.Markets()
	if err != nil {
		m.commandError("marketplace list: " + err.Error())
		return
	}
	if len(markets) == 0 {
		m.note("no marketplaces added (/marketplace add <owner/repo>)")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d marketplace(s):", len(markets))
	for _, mk := range markets {
		when := mk.Added.Format("2006-01-02")
		fmt.Fprintf(&b, "\n  • %-20s %s (added %s)", mk.Name, mk.Source, when)
	}
	m.note(b.String())
}

func (m *model) marketplaceUpdate(reg *plugin.Registry, target string) {
	markets, err := reg.Markets()
	if err != nil {
		m.commandError("marketplace update: " + err.Error())
		return
	}
	if len(markets) == 0 {
		m.note("no marketplaces to update")
		return
	}
	var b strings.Builder
	updated := 0
	for _, mk := range markets {
		if target != "" && !strings.EqualFold(mk.Name, target) {
			continue
		}
		updated++
		ctx, cancel := context.WithTimeout(context.Background(), marketplaceTimeout)
		mkt, rec, err := reg.AddMarketplace(ctx, mk.Source, nil)
		cancel()
		if err != nil {
			fmt.Fprintf(&b, "\n  ✗ %-20s %v", mk.Name, err)
			continue
		}
		fmt.Fprintf(&b, "\n  ✓ %-20s %d plugin(s)", rec.Name, len(mkt.Plugins))
	}
	if updated == 0 {
		m.note(fmt.Sprintf("no marketplace %q", target))
		return
	}
	m.note("marketplace update:" + b.String())
}

func formatMarketplaceAdded(mkt *plugin.Marketplace, rec plugin.MarketRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "✓ added marketplace %q (%d plugin(s)) from %s", rec.Name, len(mkt.Plugins), rec.Source)
	for _, p := range mkt.Plugins {
		fmt.Fprintf(&b, "\n  %-24s %s", p.Name, p.Description)
	}
	b.WriteString("\ninstall one with: /plugin install <name>[@" + rec.Name + "]")
	return b.String()
}

func (m *model) commandError(s string) {
	m.push(&block{kind: blockNote, isErr: true, body: sb(s)})
}

type pluginInstallArgs struct {
	name        string
	marketplace string
	force       bool
	overwrite   bool
	noScan      bool
}

func parsePluginInstallArgs(args []string) (pluginInstallArgs, error) {
	var out pluginInstallArgs
	var src string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "--marketplace="):
			out.marketplace = strings.TrimSpace(strings.TrimPrefix(a, "--marketplace="))
		case a == "--marketplace":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return out, fmt.Errorf("--marketplace needs a value")
			}
			out.marketplace = strings.TrimSpace(args[i+1])
			i++
		case a == "--force":
			out.force = true
		case a == "--overwrite":
			out.overwrite = true
		case a == "--no-scan":
			out.noScan = true
		case strings.HasPrefix(a, "-"):
			return out, fmt.Errorf("unknown flag %s", a)
		default:
			if src != "" {
				return out, fmt.Errorf("unexpected extra argument %s", a)
			}
			src = a
		}
	}
	if src == "" {
		return out, fmt.Errorf("missing plugin name")
	}
	name, srcMarket := splitPluginMarket(src)
	out.name = name
	if out.marketplace == "" {
		out.marketplace = srcMarket
	}
	return out, nil
}

func splitPluginMarket(src string) (name, market string) {
	name = strings.TrimSpace(src)
	if i := strings.LastIndexByte(name, '@'); i > 0 && i < len(name)-1 {
		return strings.TrimSpace(name[:i]), strings.TrimSpace(name[i+1:])
	}
	return name, ""
}
