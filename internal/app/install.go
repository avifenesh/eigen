package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/plugin"
	"github.com/avifenesh/eigen/internal/skill"
)

// installPrompt is a tiny inline text-input + status line shared by the plugins
// and skills pages, so a source can be typed and installed without leaving the
// app. It mirrors configState's inline editor (no bubbles/textinput dependency).
type installPrompt struct {
	active   bool
	label    string // what we're asking for (shown as the prompt)
	kind     string // "marketplace" | "plugin" | "skill"
	input    string
	status   string // result/feedback line ("" = none)
	busy     bool   // an install/fetch is running in a background tea.Cmd
	busyText string
}

// open starts the prompt for a given kind.
func (p *installPrompt) open(kind, label string) {
	p.active = true
	p.kind = kind
	p.label = label
	p.input = ""
	p.status = ""
}

func (p *installPrompt) close() {
	p.active = false
	p.input = ""
}

func (p *installPrompt) startBusy(kind, label, input, text string) {
	p.active = true
	p.kind = kind
	p.label = label
	p.input = input
	p.status = ""
	p.busy = true
	p.busyText = text
}

func (p *installPrompt) finish(status string) {
	p.busy = false
	p.busyText = ""
	p.close()
	p.status = status
}

// key feeds a keystroke to the active prompt. Returns (submitted source, true)
// when the user pressed enter on a non-empty input; the caller runs the install.
func (p *installPrompt) key(key string, runes []rune) (string, bool) {
	switch key {
	case "esc":
		p.close()
	case "enter":
		src := strings.TrimSpace(p.input)
		if src != "" {
			return src, true
		}
		p.close()
	case "backspace":
		if len(p.input) > 0 {
			p.input = p.input[:len(p.input)-1]
		}
	default:
		if len(runes) > 0 {
			p.input += string(runes)
		} else if key == "space" || key == " " {
			p.input += " "
		}
	}
	return "", false
}

// render draws the prompt/status line.
func (p *installPrompt) render() string {
	if p.busy {
		text := p.busyText
		if text == "" {
			text = "installing " + p.input + " … (scanning + fetching)"
		}
		return "\n" + sAccent.Render("  "+text)
	}
	if p.active {
		return "\n" + sAccent.Render("  "+p.label+": ") + sText.Render(p.input+"▏") + "\n" + sFaint.Render("  enter install · esc cancel")
	}
	if p.status != "" {
		return "\n" + sFaint.Render("  "+p.status)
	}
	return ""
}

// appPluginRegistry builds the plugin registry (global ~/.eigen).
type installDoneMsg struct {
	page   Page
	kind   string
	status string
	tab    pluginsTab
}

type marketplaceRefreshDoneMsg struct {
	marketName string
	status     string
	catalog    []plugin.PluginEntry
	focus      bool
}

func appPluginRegistry() (*plugin.Registry, error) { return plugin.NewRegistry() }

// runMarketplaceAdd adds a marketplace catalog by source.
func runMarketplaceAdd(d *Data, source string) string {
	reg, err := appPluginRegistry()
	if err != nil {
		return "error: " + err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	mkt, rec, err := reg.AddMarketplace(ctx, source, nil)
	if err != nil {
		return "marketplace add failed: " + err.Error()
	}
	return fmt.Sprintf("✓ added marketplace %q (%d plugins) — install one with i", rec.Name, len(mkt.Plugins))
}

// runPluginInstall installs a plugin by name from any added marketplace,
// scanning with the app's small model. It accepts the same name@marketplace
// shorthand as the TUI slash command.
func runPluginInstall(d *Data, name string) string {
	pluginName, market := splitPluginMarket(name)
	return runPluginInstallFrom(d, pluginName, market)
}

func runPluginInstallFrom(d *Data, name, market string) string {
	reg, err := appPluginRegistry()
	if err != nil {
		return "error: " + err.Error()
	}
	res, err := installOnePlugin(context.Background(), d, reg, name, market, false)
	if err != nil {
		return "install failed: " + err.Error()
	}
	return formatPluginInstallStatus(res)
}

func runPluginBatchInstall(d *Data, names []string, market string) string {
	reg, err := appPluginRegistry()
	if err != nil {
		return "error: " + err.Error()
	}
	var okNames, failures []string
	ctx := context.Background()
	for _, name := range names {
		res, err := installOnePlugin(ctx, d, reg, name, market, false)
		if err != nil {
			failures = append(failures, name+": "+err.Error())
			continue
		}
		okNames = append(okNames, res.Plugin.Name)
	}
	if len(failures) == 0 {
		return fmt.Sprintf("✓ installed %d plugin(s): %s", len(okNames), strings.Join(okNames, ", "))
	}
	if len(okNames) == 0 {
		return fmt.Sprintf("install failed for %d plugin(s): %s", len(failures), strings.Join(failures, "; "))
	}
	return fmt.Sprintf("⚠ installed %d/%d plugin(s): %s; failed: %s", len(okNames), len(names), strings.Join(okNames, ", "), strings.Join(failures, "; "))
}

func installOnePlugin(parent context.Context, d *Data, reg *plugin.Registry, name, market string, overwrite bool) (*plugin.InstallResult, error) {
	opts := plugin.InstallOptions{Overwrite: overwrite}
	if d != nil && d.Small != nil {
		opts.Scanner = skill.ProviderScanner{P: d.Small}
	}
	ctx, cancel := context.WithTimeout(parent, 120*time.Second)
	defer cancel()
	return reg.InstallPlugin(ctx, name, market, opts)
}

func runMarketplaceUpdate(d *Data, mk plugin.MarketRecord) (marketName, status string, catalog []plugin.PluginEntry) {
	reg, err := appPluginRegistry()
	if err != nil {
		return mk.Name, "update failed: " + err.Error(), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	mkt, rec, err := reg.AddMarketplace(ctx, mk.Source, nil)
	if err != nil {
		return mk.Name, "update failed: " + err.Error(), nil
	}

	installed, _ := reg.Installed()
	var updated, failed []string
	for _, pl := range installed {
		if !strings.EqualFold(pl.Marketplace, rec.Name) {
			continue
		}
		if _, ok := mkt.Find(pl.Name); !ok {
			continue
		}
		if _, err := installOnePlugin(context.Background(), d, reg, pl.Name, rec.Name, true); err != nil {
			failed = append(failed, pl.Name+": "+err.Error())
			continue
		}
		updated = append(updated, pl.Name)
	}

	status = fmt.Sprintf("pulled marketplace %q — %d catalog plugin(s)", rec.Name, len(mkt.Plugins))
	if len(updated) > 0 {
		status += fmt.Sprintf(", updated %d installed: %s", len(updated), strings.Join(updated, ", "))
	}
	if len(failed) > 0 {
		status += fmt.Sprintf(", failed %d: %s", len(failed), strings.Join(failed, "; "))
	}
	return rec.Name, status, append([]plugin.PluginEntry(nil), mkt.Plugins...)
}

func formatPluginInstallStatus(res *plugin.InstallResult) string {
	pl := res.Plugin
	msg := fmt.Sprintf("✓ installed %q — %d skill(s), %d agent(s), %d command(s), %d mcp, %d hook(s)",
		pl.Name, len(pl.Skills), len(pl.Agents), len(pl.Commands), len(pl.MCPServers), pl.Hooks)
	if len(res.Scans) > 0 {
		msg += "  ⚠ scan flags (kept; --force-style)"
	}
	return msg
}

func splitPluginMarket(src string) (name, market string) {
	name = strings.TrimSpace(src)
	if i := strings.LastIndexByte(name, '@'); i > 0 && i < len(name)-1 {
		return strings.TrimSpace(name[:i]), strings.TrimSpace(name[i+1:])
	}
	return name, ""
}

type skillInstallArgs struct {
	source    string
	name      string
	force     bool
	noScan    bool
	overwrite bool
}

func parseSkillInstallInput(input string) (skillInstallArgs, error) {
	var out skillInstallArgs
	fields := strings.Fields(input)
	for i := 0; i < len(fields); i++ {
		a := fields[i]
		switch {
		case a == "--force":
			out.force = true
		case a == "--no-scan":
			out.noScan = true
		case a == "--overwrite":
			out.overwrite = true
		case strings.HasPrefix(a, "--name="):
			out.name = strings.TrimSpace(strings.TrimPrefix(a, "--name="))
		case a == "--name":
			if i+1 >= len(fields) || strings.HasPrefix(fields[i+1], "--") {
				return out, fmt.Errorf("--name needs a value")
			}
			out.name = fields[i+1]
			i++
		case strings.HasPrefix(a, "--"):
			return out, fmt.Errorf("unknown flag %s", a)
		default:
			if out.source != "" {
				return out, fmt.Errorf("unexpected extra argument %q", a)
			}
			out.source = a
		}
	}
	if out.source == "" {
		return out, fmt.Errorf("missing skill source")
	}
	return out, nil
}

// runSkillInstall installs a skill from a path or owner/repo[/sub][@ref],
// scanning with the app's small model. The prompt also accepts the safety
// overrides from the CLI: --force, --no-scan, --overwrite, --name <name>.
func runSkillInstall(d *Data, input string) string {
	args, err := parseSkillInstallInput(input)
	if err != nil {
		return "skill add failed: " + err.Error()
	}
	home, _ := os.UserHomeDir()
	opts := skill.InstallOptions{Dir: filepath.Join(home, ".eigen", "skills"), Name: args.name, Force: args.force, Overwrite: args.overwrite}
	if !args.noScan && d != nil && d.Small != nil {
		opts.Scanner = skill.ProviderScanner{P: d.Small}
	}
	res, err := installSkillSource(args.source, opts)
	if err != nil {
		return "skill add failed: " + err.Error()
	}
	if !res.Scan.Safe {
		return fmt.Sprintf("⚠ installed %q despite scan flags", res.Name)
	}
	return fmt.Sprintf("✓ installed skill %q → %s", res.Name, res.Path)
}

// installSkillSource dispatches to a local-path or GitHub skill install (mirrors
// main.installSkill so the app shell can install without the CLI).
func installSkillSource(source string, opts skill.InstallOptions) (skill.Installed, error) {
	if _, err := os.Stat(source); err == nil {
		return skill.InstallFromPath(context.Background(), source, opts)
	}
	ref, err := skill.ParseGitHubRef(source)
	if err != nil {
		return skill.Installed{}, err
	}
	return skill.InstallFromGitHub(context.Background(), ref, skill.DefaultFetcher, opts)
}
