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
		return "\n" + renderPromptStatus(p.status)
	}
	return ""
}

func renderPromptStatus(status string) string {
	lines := strings.Split(status, "\n")
	for i, line := range lines {
		lines[i] = "  " + strings.TrimRight(line, " ")
	}
	return sFaint.Render(strings.Join(lines, "\n"))
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

type pluginPreviewDoneMsg struct {
	key     string
	preview *plugin.PluginPreview
	err     string
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
// shorthand and explicit safety flags as the TUI slash command.
func runPluginInstall(d *Data, name string) string {
	args, err := parsePluginInstallInput(name)
	if err != nil {
		return "install failed: " + err.Error()
	}
	return runPluginInstallArgs(d, args)
}

func runPluginInstallFrom(d *Data, name, market string) string {
	return runPluginInstallArgs(d, pluginInstallInput{name: strings.TrimSpace(name), marketplace: strings.TrimSpace(market)})
}

func runPluginInstallArgs(d *Data, args pluginInstallInput) string {
	reg, err := appPluginRegistry()
	if err != nil {
		return "error: " + err.Error()
	}
	res, scanned, err := installOnePlugin(context.Background(), d, reg, args)
	if err != nil {
		return pluginInstallFailureStatus(args.name, err)
	}
	return formatPluginInstallStatus(res, scanned)
}

func runPluginBatchInstall(d *Data, names []string, market string) string {
	reg, err := appPluginRegistry()
	if err != nil {
		return "error: " + err.Error()
	}
	var okNames, results, failures []string
	ctx := context.Background()
	for _, name := range names {
		args := pluginInstallInput{name: strings.TrimSpace(name), marketplace: strings.TrimSpace(market)}
		res, scanned, err := installOnePlugin(ctx, d, reg, args)
		if err != nil {
			failures = append(failures, name+" (rolled back/no plugin recorded): "+err.Error())
			continue
		}
		okNames = append(okNames, res.Plugin.Name)
		results = append(results, pluginInstallResultLine(res, scanned))
	}
	if len(failures) == 0 {
		return fmt.Sprintf("✓ installed %d plugin(s): %s\n  results: %s", len(okNames), strings.Join(okNames, ", "), strings.Join(results, "; "))
	}
	if len(okNames) == 0 {
		return fmt.Sprintf("install failed for %d plugin(s): %s", len(failures), strings.Join(failures, "; "))
	}
	return fmt.Sprintf("⚠ installed %d/%d plugin(s): %s\n  results: %s\n  failed: %s", len(okNames), len(names), strings.Join(okNames, ", "), strings.Join(results, "; "), strings.Join(failures, "; "))
}

func installOnePlugin(parent context.Context, d *Data, reg *plugin.Registry, args pluginInstallInput) (*plugin.InstallResult, bool, error) {
	opts := plugin.InstallOptions{Force: args.force, Overwrite: args.overwrite}
	scanned := false
	if !args.noScan && d != nil && d.Small != nil {
		opts.Scanner = skill.ProviderScanner{P: d.Small}
		scanned = true
	}
	ctx, cancel := context.WithTimeout(parent, 120*time.Second)
	defer cancel()
	res, err := reg.InstallPlugin(ctx, args.name, args.marketplace, opts)
	return res, scanned, err
}

func runPluginPreview(name, market string) (*plugin.PluginPreview, error) {
	reg, err := appPluginRegistry()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return reg.PreviewPlugin(ctx, name, market, nil)
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
		args := pluginInstallInput{name: pl.Name, marketplace: rec.Name, overwrite: true}
		if _, _, err := installOnePlugin(context.Background(), d, reg, args); err != nil {
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

func formatPluginInstallStatus(res *plugin.InstallResult, scanned bool) string {
	pl := res.Plugin
	var b strings.Builder
	switch {
	case len(res.Scans) > 0:
		fmt.Fprintf(&b, "⚠ installed %q despite scan flags", pl.Name)
	case scanned:
		fmt.Fprintf(&b, "✓ scan clean — installed %q", pl.Name)
	default:
		fmt.Fprintf(&b, "installed %q (scan skipped)", pl.Name)
	}
	fmt.Fprintf(&b, "\nresult: %s", pluginInstallCounts(pl))
	b.WriteString("\nscan verdict: " + pluginScanVerdict(pl))
	for _, s := range res.Scans {
		fmt.Fprintf(&b, "\nscan flag %s", s.Component)
		for _, r := range s.Reasons {
			b.WriteString("\n  - " + r)
		}
	}
	for _, w := range res.Warnings {
		b.WriteString("\nwarning: " + w)
	}
	if len(pl.MCPServers) > 0 {
		b.WriteString("\nnote: MCP servers are gated; the agent unlocks them via search_tools")
	}
	return b.String()
}

func pluginInstallFailureStatus(name string, err error) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "install failed — rolled back/no plugin recorded: " + err.Error()
	}
	return fmt.Sprintf("install failed for %q — rolled back/no plugin recorded: %s", name, err.Error())
}

func pluginInstallResultLine(res *plugin.InstallResult, scanned bool) string {
	pl := res.Plugin
	verdict := pluginScanVerdict(pl)
	if len(res.Scans) > 0 {
		return fmt.Sprintf("%s: forced (%s)", pl.Name, pluginInstallCounts(pl))
	}
	if scanned {
		return fmt.Sprintf("%s: clean (%s)", pl.Name, pluginInstallCounts(pl))
	}
	return fmt.Sprintf("%s: %s (%s)", pl.Name, verdict, pluginInstallCounts(pl))
}

type pluginInstallInput struct {
	name        string
	marketplace string
	force       bool
	overwrite   bool
	noScan      bool
}

func parsePluginInstallInput(input string) (pluginInstallInput, error) {
	var out pluginInstallInput
	var src string
	fields := strings.Fields(input)
	for i := 0; i < len(fields); i++ {
		a := fields[i]
		switch {
		case strings.HasPrefix(a, "--marketplace="):
			out.marketplace = strings.TrimSpace(strings.TrimPrefix(a, "--marketplace="))
		case a == "--marketplace":
			if i+1 >= len(fields) || strings.HasPrefix(fields[i+1], "-") {
				return out, fmt.Errorf("--marketplace needs a value")
			}
			out.marketplace = strings.TrimSpace(fields[i+1])
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
				return out, fmt.Errorf("unexpected extra argument %q", a)
			}
			src = a
		}
	}
	if src == "" {
		return out, fmt.Errorf("missing plugin name")
	}
	name, market := splitPluginMarket(src)
	out.name = name
	if out.marketplace == "" {
		out.marketplace = market
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
