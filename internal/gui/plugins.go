package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/plugin"
	"github.com/avifenesh/eigen/internal/skill"
)

// Plugins bridge layer. Surfaces the installed plugins + configured
// marketplaces from the local plugin registry (~/.eigen). Read-only listing
// plus the safe management ops the user-command layer already allows: enable/
// disable/remove a marketplace, remove an installed plugin.
//
// The GUI also exposes an INSTALL path that mirrors the skill-add UX: add a
// marketplace catalog (GitHub owner/repo, https URL, or local path), browse its
// installable plugins, then install one by name. Every install is security-
// scanned exactly like `eigen skill add` — the scanner vets each bundled
// skill/command/agent/hook/MCP body before anything is wired, and a RISKY
// verdict ABORTS the install (the bridge never Forces). When no small model is
// credentialed to scan, the install FAILS CLOSED rather than silently wiring
// unvetted bundle code.

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
	Name        string `json:"name"`
	Source      string `json:"source"`
	Owner       string `json:"owner,omitempty"`
	Disabled    bool   `json:"disabled"`
	AddedMs     int64  `json:"addedMs"`
	Description string `json:"description,omitempty"` // from the parsed catalog (AddMarketplace only)
	Version     string `json:"version,omitempty"`     // from the parsed catalog (AddMarketplace only)
	PluginCount int    `json:"pluginCount,omitempty"` // # of installable plugins listed (AddMarketplace only)
}

// PluginPreviewDTO is one installable plugin listed in a recorded marketplace:
// a lightweight, read-only summary (name/description + component counts), not
// the full prompt bodies. Mirrors plugin.PluginPreview's shape for the gallery.
type PluginPreviewDTO struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Marketplace string `json:"marketplace"`
	Version     string `json:"version,omitempty"`
	Skills      int    `json:"skills"`
	Agents      int    `json:"agents"`
	Commands    int    `json:"commands"`
	MCPServers  int    `json:"mcpServers"`
	Hooks       int    `json:"hooks"`
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
		plugins = append(plugins, installedPluginDTO(reg, p))
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

// installedPluginDTO converts a registry InstalledPlugin into the wire DTO,
// deriving its enabled state from the on-disk markers. Shared by the read-only
// snapshot (Plugins) and the install path (InstallPlugin) so both report the
// same shape — including the scan audit.
func installedPluginDTO(reg *plugin.Registry, p plugin.InstalledPlugin) InstalledPluginDTO {
	var scans []ScanFindingDTO
	if len(p.Scans) > 0 {
		scans = make([]ScanFindingDTO, 0, len(p.Scans))
		for _, sc := range p.Scans {
			scans = append(scans, ScanFindingDTO{Component: sc.Component, Reasons: sc.Reasons})
		}
	}
	return InstalledPluginDTO{
		Name: p.Name, Marketplace: p.Marketplace, Version: p.Version,
		Description: p.Description, InstalledMs: p.Installed.UnixMilli(),
		Enabled: pluginEnabled(reg, p),
		Skills:  p.Skills, Agents: p.Agents, MCPServers: p.MCPServers,
		Commands: p.Commands, Hooks: p.Hooks,
		ScanStatus: p.ScanStatus, ScanCount: p.ScanCount, Scans: scans, Warnings: p.Warnings,
	}
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

// --- install path (mirrors the skill-add UX) ---

// pluginInstallScanner builds the security scanner for plugin installs using a
// small/cheap model (EIGEN_SMALL_MODEL → grok composer → Haiku), mirroring
// main's smallProvider precedence — the SAME ladder skills.go installScanner
// uses. It vets each bundled skill/command/agent/hook/MCP body before anything
// is wired; a RISKY verdict aborts the install (the bridge never Forces).
// Returns nil when no provider can be credentialed, so the caller fails closed.
//
// Duplicated from skills.go on purpose: the task scopes this change to
// plugins.go and forbids editing skills.go. Keep the two ladders in sync.
func pluginInstallScanner() skill.Scanner {
	if sm := os.Getenv("EIGEN_SMALL_MODEL"); sm != "" {
		if p, err := llm.New("", sm); err == nil {
			return skill.ProviderScanner{P: p}
		}
	}
	if llm.ProviderAvailable("grok") {
		if p, err := llm.New("grok", "grok-composer-2.5-fast"); err == nil {
			return skill.ProviderScanner{P: p}
		}
	}
	if p, err := llm.New("converse", "us.anthropic.claude-haiku-4-5-20251001-v1:0"); err == nil {
		return skill.ProviderScanner{P: p}
	}
	return nil
}

// pluginInstallOptions builds the InstallOptions every GUI install shares: scan
// on, never Force, never Overwrite, default tarball fetch. A nil scanner (no
// credentialed small model) is surfaced as an error rather than silently wiring
// unvetted bundle code — fail closed, exactly like skills.go installOptions.
func pluginInstallOptions() (plugin.InstallOptions, error) {
	sc := pluginInstallScanner()
	if sc == nil {
		return plugin.InstallOptions{}, errors.New("cannot scan plugin: no credentialed model available (set EIGEN_SMALL_MODEL or credential a provider)")
	}
	return plugin.InstallOptions{Scanner: sc, Tree: plugin.DefaultTreeFetcher}, nil
}

// AddMarketplace records a plugin catalog so its plugins become installable.
// source is a GitHub owner/repo[@ref], an https URL to a marketplace.json, or a
// local path to a directory/file (the same source forms skill-add accepts).
// It fetches and parses the catalog, persists the record, and returns the
// recorded marketplace as a DTO carrying the catalog's description/version and
// the number of installable plugins it lists. This is read+record only; nothing
// is installed or scanned here.
func (b *Bridge) AddMarketplace(source string) (*MarketplaceDTO, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, errors.New("marketplace source is empty")
	}
	reg, err := plugin.NewRegistry()
	if err != nil {
		return nil, err
	}
	mkt, rec, err := reg.AddMarketplace(context.Background(), source, plugin.DefaultTreeFetcher)
	if err != nil {
		return nil, err
	}
	dto := &MarketplaceDTO{
		Name: rec.Name, Source: rec.Source, Owner: rec.Owner,
		Disabled: rec.Disabled, AddedMs: rec.Added.UnixMilli(),
	}
	if mkt != nil {
		dto.Description = mkt.Metadata.Description
		dto.Version = mkt.Metadata.Version
		dto.PluginCount = len(mkt.Plugins)
		if dto.Owner == "" {
			dto.Owner = mkt.Owner.Name
		}
	}
	return dto, nil
}

// MarketplacePlugins lists the installable plugins from a recorded marketplace.
// It re-fetches the catalog, then runs a read-only PreviewPlugin per listed
// entry to fill in component counts (nothing is scanned or wired). An entry
// whose preview fails to resolve still appears, with the catalog name/
// description and zero counts, so a single unreachable plugin source doesn't
// blank the whole list. Read-only; run off the UI goroutine — fetching is slow.
func (b *Bridge) MarketplacePlugins(mktName string) ([]PluginPreviewDTO, error) {
	mktName = strings.TrimSpace(mktName)
	if mktName == "" {
		return nil, errors.New("marketplace name is empty")
	}
	reg, err := plugin.NewRegistry()
	if err != nil {
		return nil, err
	}
	rec, ok := reg.MarketByName(mktName)
	if !ok {
		return nil, errors.New("marketplace not found: " + mktName)
	}
	mkt, _, err := reg.AddMarketplace(context.Background(), rec.Source, plugin.DefaultTreeFetcher)
	if err != nil {
		return nil, err
	}
	out := make([]PluginPreviewDTO, 0, len(mkt.Plugins))
	for _, entry := range mkt.Plugins {
		dto := PluginPreviewDTO{
			Name:        entry.Name,
			Description: entry.Description,
			Marketplace: rec.Name,
			Version:     entry.Version,
		}
		if pv, perr := reg.PreviewPlugin(context.Background(), entry.Name, rec.Name, plugin.DefaultTreeFetcher); perr == nil && pv != nil {
			dto.Skills = len(pv.Skills)
			dto.Agents = len(pv.Agents)
			dto.Commands = len(pv.Commands)
			dto.MCPServers = len(pv.MCPServers)
			dto.Hooks = pv.Hooks
			if dto.Description == "" && pv.Manifest != nil {
				dto.Description = pv.Manifest.Description
			}
			if dto.Version == "" && pv.Manifest != nil {
				dto.Version = pv.Manifest.Version
			}
		}
		out = append(out, dto)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// InstallPlugin installs a named plugin from a recorded marketplace. mktName is
// optional — if empty, the first marketplace listing the plugin wins. Every
// bundled skill/command/agent/hook/MCP body is security-scanned before it is
// wired; a RISKY verdict ABORTS the install (a *skill.RiskyError is returned,
// flattened to its message by Wails) and Force is never set. When no small
// model is credentialed to scan, the install fails closed with a clear error.
// On success the freshly installed plugin is returned as an InstalledPluginDTO.
func (b *Bridge) InstallPlugin(pluginName, mktName string) (*InstalledPluginDTO, error) {
	pluginName = strings.TrimSpace(pluginName)
	if pluginName == "" {
		return nil, errors.New("plugin name is empty")
	}
	reg, err := plugin.NewRegistry()
	if err != nil {
		return nil, err
	}
	opts, err := pluginInstallOptions()
	if err != nil {
		return nil, err
	}
	res, err := reg.InstallPlugin(context.Background(), pluginName, strings.TrimSpace(mktName), opts)
	if err != nil {
		return nil, err
	}
	dto := installedPluginDTO(reg, res.Plugin)
	return &dto, nil
}
