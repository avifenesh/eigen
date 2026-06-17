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
	active bool
	label  string // what we're asking for (shown as the prompt)
	kind   string // "marketplace" | "plugin" | "skill"
	input  string
	status string // result/feedback line ("" = none)
	busy   bool   // an install is running (synchronous; shows "working…")
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
		return "\n" + sAccent.Render("  installing "+p.input+" … ") + sFaint.Render("(scanning + fetching)")
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
// scanning with the app's small model.
func runPluginInstall(d *Data, name string) string {
	reg, err := appPluginRegistry()
	if err != nil {
		return "error: " + err.Error()
	}
	opts := plugin.InstallOptions{}
	if d != nil && d.Small != nil {
		opts.Scanner = skill.ProviderScanner{P: d.Small}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	res, err := reg.InstallPlugin(ctx, name, "", opts)
	if err != nil {
		return "install failed: " + err.Error()
	}
	pl := res.Plugin
	msg := fmt.Sprintf("✓ installed %q — %d skill(s), %d agent(s), %d command(s), %d mcp, %d hook(s)",
		pl.Name, len(pl.Skills), len(pl.Agents), len(pl.Commands), len(pl.MCPServers), pl.Hooks)
	if len(res.Scans) > 0 {
		msg += "  ⚠ scan flags (kept; --force-style)"
	}
	return msg
}

// runSkillInstall installs a skill from a path or owner/repo[/sub][@ref],
// scanning with the app's small model.
func runSkillInstall(d *Data, source string) string {
	home, _ := os.UserHomeDir()
	opts := skill.InstallOptions{Dir: filepath.Join(home, ".eigen", "skills")}
	if d != nil && d.Small != nil {
		opts.Scanner = skill.ProviderScanner{P: d.Small}
	}
	res, err := installSkillSource(source, opts)
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
