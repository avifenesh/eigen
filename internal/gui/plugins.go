package gui

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/avifenesh/eigen/internal/plugin"
)

// Plugins bridge layer. Surfaces the installed plugins + configured
// marketplaces from the local plugin registry (~/.eigen). Read-only listing
// plus the safe management ops the user-command layer already allows: enable/
// disable/remove a marketplace, remove an installed plugin. Installing a plugin
// is intentionally NOT exposed (untrusted bundle code is scanned at install via
// the CLI; the agent/GUI must not auto-install).

// ScanFindingDTO is one component's risky scan verdict, kept for audit/UI so a
// --force-installed plugin can show WHICH component tripped the scanner and why.
type ScanFindingDTO struct {
	Component string   `json:"component"`
	Reasons   []string `json:"reasons,omitempty"`
}

type InstalledPluginDTO struct {
	Name        string           `json:"name"`
	Marketplace string           `json:"marketplace,omitempty"`
	Version     string           `json:"version,omitempty"`
	Description string           `json:"description,omitempty"`
	InstalledMs int64            `json:"installedMs"`
	Enabled     bool             `json:"enabled"`
	Skills      []string         `json:"skills,omitempty"`
	Agents      []string         `json:"agents,omitempty"`
	MCPServers  []string         `json:"mcpServers,omitempty"`
	Commands    []string         `json:"commands,omitempty"`
	Hooks       int              `json:"hooks,omitempty"`
	ScanStatus  string           `json:"scanStatus,omitempty"`
	ScanCount   int              `json:"scanCount,omitempty"`
	Scans       []ScanFindingDTO `json:"scans,omitempty"`
	Warnings    []string         `json:"warnings,omitempty"`
}

type MarketplaceDTO struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	Owner    string `json:"owner,omitempty"`
	Disabled bool   `json:"disabled"`
	AddedMs  int64  `json:"addedMs"`
}

// PluginsDTO is the plugin/marketplace snapshot.
type PluginsDTO struct {
	Plugins      []InstalledPluginDTO `json:"plugins"`
	Marketplaces []MarketplaceDTO     `json:"marketplaces"`
}

// Plugins returns installed plugins + configured marketplaces.
func (b *Bridge) Plugins() (*PluginsDTO, error) {
	reg, err := plugin.NewRegistry()
	if err != nil {
		return nil, err
	}
	installed, err := reg.Installed()
	if err != nil {
		return nil, err
	}
	markets, err := reg.Markets()
	if err != nil {
		return nil, err
	}

	plugins := make([]InstalledPluginDTO, 0, len(installed))
	for _, p := range installed {
		var scans []ScanFindingDTO
		if len(p.Scans) > 0 {
			scans = make([]ScanFindingDTO, 0, len(p.Scans))
			for _, sc := range p.Scans {
				scans = append(scans, ScanFindingDTO{Component: sc.Component, Reasons: sc.Reasons})
			}
		}
		plugins = append(plugins, InstalledPluginDTO{
			Name: p.Name, Marketplace: p.Marketplace, Version: p.Version,
			Description: p.Description, InstalledMs: p.Installed.UnixMilli(),
			Enabled: pluginEnabled(reg, p),
			Skills:  p.Skills, Agents: p.Agents, MCPServers: p.MCPServers,
			Commands: p.Commands, Hooks: p.Hooks,
			ScanStatus: p.ScanStatus, ScanCount: p.ScanCount, Scans: scans, Warnings: p.Warnings,
		})
	}
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].Name < plugins[j].Name })

	mkts := make([]MarketplaceDTO, 0, len(markets))
	for _, m := range markets {
		mkts = append(mkts, MarketplaceDTO{
			Name: m.Name, Source: m.Source, Owner: m.Owner,
			Disabled: m.Disabled, AddedMs: m.Added.UnixMilli(),
		})
	}
	sort.Slice(mkts, func(i, j int) bool { return mkts[i].Name < mkts[j].Name })

	return &PluginsDTO{Plugins: plugins, Marketplaces: mkts}, nil
}

// SetMarketEnabled enables/disables a marketplace (kept in the registry either
// way; disabled markets are ignored for installs/update-all).
func (b *Bridge) SetMarketEnabled(name string, enabled bool) (bool, error) {
	reg, err := plugin.NewRegistry()
	if err != nil {
		return false, err
	}
	return reg.SetMarketEnabled(name, enabled)
}

// RemoveMarketplace removes a marketplace from the registry.
func (b *Bridge) RemoveMarketplace(name string) (bool, error) {
	reg, err := plugin.NewRegistry()
	if err != nil {
		return false, err
	}
	return reg.RemoveMarket(name)
}

// SetPluginEnabled enables/disables ALL of an installed plugin's wired
// components at once (skills, agents, commands, MCP servers, hooks) without
// uninstalling, so a GUI user can stop a broken plugin and re-enable it later
// instead of removing it. Delegates to the same Registry.SetEnabled the TUI's
// `/plugin enable|disable` uses; applies to NEW sessions only. Returns false if
// no plugin by that name is installed.
func (b *Bridge) SetPluginEnabled(name string, enabled bool) (bool, error) {
	reg, err := plugin.NewRegistry()
	if err != nil {
		return false, err
	}
	return reg.SetEnabled(name, enabled)
}

// RemovePlugin fully uninstalls a plugin: reverses its wiring (skills, agents,
// mcp, hooks, commands) and removes its files, not just the registry record.
func (b *Bridge) RemovePlugin(name string) (bool, error) {
	reg, err := plugin.NewRegistry()
	if err != nil {
		return false, err
	}
	return reg.Uninstall(name)
}

// pluginEnabled derives a plugin's enabled state from the on-disk markers
// Registry.SetEnabled flips, using only the registry's public component dirs.
// Disabling parks each skill/agent/command file aside with a ".disabled"
// suffix, so a plugin reads as disabled the moment any tracked component is
// found parked (and its active file gone). A plugin with no file-backed
// components — MCP/hooks only — has nothing to park here and reads as enabled.
func pluginEnabled(reg *plugin.Registry, p plugin.InstalledPlugin) bool {
	parked := func(active string) bool {
		if _, err := os.Stat(active); err == nil {
			return false // active file present → that component is live
		}
		if _, err := os.Stat(active + ".disabled"); err == nil {
			return true // active gone, parked copy present → disabled
		}
		return false // neither present (already removed) → not a disable signal
	}
	for _, sd := range p.Skills {
		if parked(filepath.Join(reg.SkillsDir(), sd, "SKILL.md")) {
			return false
		}
	}
	for _, an := range p.Agents {
		if parked(filepath.Join(reg.AgentsDir(), an+".md")) {
			return false
		}
	}
	for _, cn := range p.Commands {
		if parked(filepath.Join(reg.CommandsDir(), cn+".md")) {
			return false
		}
	}
	return true
}
