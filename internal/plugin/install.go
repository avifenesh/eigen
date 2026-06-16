package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/skill"
)

// pluginRootVar is the Claude placeholder that expands to a plugin's installed
// root dir. Bundled scripts/MCP commands reference it so they never hardcode a
// path. On install we rewrite it to eigenRootVar (OUR namespace) and provide the
// value via the EIGEN_PLUGIN_ROOT env param — so the path lives in ONE env
// variable, not smeared as a literal through every arg. The MCP loader expands
// ${EIGEN_PLUGIN_ROOT} in command/args at launch against the server's env.
const pluginRootVar = "${CLAUDE_PLUGIN_ROOT}"

// eigenRootEnv is our namespaced env var carrying the bundle root.
const eigenRootEnv = "EIGEN_PLUGIN_ROOT"

// eigenRootVar is the reference form used in stored configs/commands/skills.
const eigenRootVar = "${" + eigenRootEnv + "}"

// InstallOptions controls a plugin install.
type InstallOptions struct {
	Scanner   skill.Scanner // vets each skill/command body; RISKY aborts unless Force
	Force     bool          // install despite a RISKY scan verdict
	Overwrite bool          // replace an already-installed plugin
	Tree      TreeFetcher   // injectable repo-tarball fetch (default DefaultTreeFetcher)
}

// InstallResult reports what an install wired in.
type InstallResult struct {
	Plugin   InstalledPlugin
	Scans    []ScanFinding // per-component scan verdicts (RISKY ones, when forced)
	Warnings []string      // non-fatal notes (e.g. commands/agents not wired in v1)
}

// ScanFinding is one component's risky scan verdict (surfaced to the user).
type ScanFinding struct {
	Component string
	Reasons   []string
}

// AddMarketplace fetches a catalog repo, parses its marketplace.json, and
// records it. ref is optional (default branch). Returns the parsed catalog so a
// caller can immediately list its plugins.
func (r *Registry) AddMarketplace(ctx context.Context, source string, fetch TreeFetcher) (*Marketplace, MarketRecord, error) {
	if fetch == nil {
		fetch = DefaultTreeFetcher
	}
	ref, err := skill.ParseGitHubRef(source)
	if err != nil {
		return nil, MarketRecord{}, err
	}
	tmp, err := os.MkdirTemp("", "eigen-market-*")
	if err != nil {
		return nil, MarketRecord{}, err
	}
	defer os.RemoveAll(tmp)

	root, err := fetch(ctx, ref.Owner, ref.Repo, ref.Ref, tmp)
	if err != nil {
		return nil, MarketRecord{}, err
	}
	// The marketplace manifest may be at the repo root or under the ref's path.
	base, err := safeJoinUnder(root, ref.Path, "marketplace repo")
	if err != nil {
		return nil, MarketRecord{}, err
	}
	mfPath := filepath.Join(base, ".claude-plugin", "marketplace.json")
	b, err := os.ReadFile(mfPath)
	if err != nil {
		return nil, MarketRecord{}, fmt.Errorf("no .claude-plugin/marketplace.json in %s", source)
	}
	mkt, err := ParseMarketplace(b)
	if err != nil {
		return nil, MarketRecord{}, err
	}
	rec := MarketRecord{Name: mkt.Name, Source: source, Owner: mkt.Owner.Name}
	if err := r.AddMarket(rec); err != nil {
		return nil, MarketRecord{}, err
	}
	return mkt, rec, nil
}

// fetchMarketplace re-fetches a recorded marketplace's catalog into a temp dir,
// returning the parsed catalog + the extracted repo root + a cleanup func.
func (r *Registry) fetchMarketplace(ctx context.Context, rec MarketRecord, fetch TreeFetcher) (*Marketplace, string, func(), error) {
	ref, err := skill.ParseGitHubRef(rec.Source)
	if err != nil {
		return nil, "", nil, err
	}
	tmp, err := os.MkdirTemp("", "eigen-market-*")
	if err != nil {
		return nil, "", nil, err
	}
	cleanup := func() { os.RemoveAll(tmp) }
	root, err := fetch(ctx, ref.Owner, ref.Repo, ref.Ref, tmp)
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}
	base, err := safeJoinUnder(root, ref.Path, "marketplace repo")
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}
	b, err := os.ReadFile(filepath.Join(base, ".claude-plugin", "marketplace.json"))
	if err != nil {
		cleanup()
		return nil, "", nil, fmt.Errorf("marketplace %q catalog missing: %w", rec.Name, err)
	}
	mkt, err := ParseMarketplace(b)
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}
	return mkt, base, cleanup, nil
}

// InstallPlugin installs a plugin by name from a recorded marketplace (mktName
// optional — if empty, the first marketplace listing the plugin wins). It
// fetches the plugin's tree, scans each skill/command body, and wires
// components into the global per-scope configs. CLI-only (the agent never
// calls this).
func (r *Registry) InstallPlugin(ctx context.Context, pluginName, mktName string, opts InstallOptions) (*InstallResult, error) {
	if err := SafeName(pluginName); err != nil {
		return nil, err
	}
	if opts.Tree == nil {
		opts.Tree = DefaultTreeFetcher
	}
	if _, ok := r.InstalledByName(pluginName); ok && !opts.Overwrite {
		return nil, fmt.Errorf("plugin %q already installed (use --overwrite)", pluginName)
	}

	entry, mkt, base, cleanup, err := r.resolvePlugin(ctx, pluginName, mktName, opts.Tree)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Resolve the plugin's on-disk root: local (within the marketplace repo) or
	// an external repo we fetch separately.
	pluginRoot, extraCleanup, err := r.resolvePluginRoot(ctx, entry, base, opts.Tree)
	if err != nil {
		return nil, err
	}
	defer extraCleanup()

	comps, err := Discover(pluginRoot, !entry.strictMode())
	if err != nil {
		return nil, err
	}

	// Cache the bundle (so ${CLAUDE_PLUGIN_ROOT} references resolve at runtime).
	dest := filepath.Join(r.PluginsDir(), pluginName)
	if opts.Overwrite {
		_ = os.RemoveAll(dest)
		r.uninstallFiles(pluginName) // best-effort: clear prior wiring first
	}
	if err := copyTree(pluginRoot, dest); err != nil {
		return nil, fmt.Errorf("cache bundle: %w", err)
	}

	res := &InstallResult{}
	res.Plugin = InstalledPlugin{
		Name: pluginName, Marketplace: mkt, Version: entry.Version,
		Description: entry.Description, Root: dest,
	}

	// 1) Scan + wire skills (copy each skill dir; scan its SKILL.md body).
	for _, sf := range comps.Skills {
		if opts.Scanner != nil {
			sr, serr := opts.Scanner.Scan(ctx, sf.Name, sf.Content)
			if serr != nil {
				return nil, fmt.Errorf("scan skill %q: %w", sf.Name, serr)
			}
			if !sr.Safe {
				res.Scans = append(res.Scans, ScanFinding{Component: "skill:" + sf.Name, Reasons: sr.Reasons})
				if !opts.Force {
					r.uninstallFiles(pluginName)
					_ = os.RemoveAll(dest)
					return nil, &skill.RiskyError{Name: pluginName + "/" + sf.Name, Reasons: sr.Reasons}
				}
			}
		}
		instName := pluginName + "-" + sf.Name // namespace to avoid collisions
		if err := r.installSkillDir(filepath.Join(comps.Root, "skills", sf.Name), instName, dest, opts.Overwrite); err != nil {
			return nil, fmt.Errorf("install skill %q: %w", sf.Name, err)
		}
		res.Plugin.Skills = append(res.Plugin.Skills, instName)
	}

	// 2) Wire MCP servers (niche, gated, ${ROOT}-expanded) into mcp.json.
	for _, s := range comps.MCPServers {
		name := pluginName + "-" + s.Name
		if err := r.addMCPServer(name, s, dest, entry); err != nil {
			return nil, fmt.Errorf("wire mcp %q: %w", s.Name, err)
		}
		res.Plugin.MCPServers = append(res.Plugin.MCPServers, name)
	}

	// 3) Wire hooks (${ROOT}-expanded) into hooks.json.
	if n, err := r.addHooks(comps.Hooks, dest); err != nil {
		return nil, fmt.Errorf("wire hooks: %w", err)
	} else {
		res.Plugin.Hooks = n
	}

	// 4) Wire slash commands (commands/*.md) into ~/.eigen/commands, scanned +
	// namespaced. These become /<plugin>-<name> in the TUI.
	for _, cf := range comps.Commands {
		if opts.Scanner != nil {
			sr, serr := opts.Scanner.Scan(ctx, cf.Name, cf.Content)
			if serr != nil {
				return nil, fmt.Errorf("scan command %q: %w", cf.Name, serr)
			}
			if !sr.Safe {
				res.Scans = append(res.Scans, ScanFinding{Component: "command:" + cf.Name, Reasons: sr.Reasons})
				if !opts.Force {
					r.uninstallFiles(pluginName)
					_ = os.RemoveAll(dest)
					return nil, &skill.RiskyError{Name: pluginName + "/" + cf.Name, Reasons: sr.Reasons}
				}
			}
		}
		instName := pluginName + "-" + cf.Name
		if err := r.installCommand(instName, cf.Content, dest, opts.Overwrite); err != nil {
			return nil, fmt.Errorf("install command %q: %w", cf.Name, err)
		}
		res.Plugin.Commands = append(res.Plugin.Commands, instName)
	}

	// agents: parsed-but-not-wired (no subagent-prompt subsystem yet).
	if comps.Agents > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d agent(s) not wired (subagent prompts deferred to a later version)", comps.Agents))
	}

	if err := r.RecordInstall(res.Plugin); err != nil {
		return nil, err
	}
	return res, nil
}

// resolvePlugin locates the plugin entry in a (named or any) recorded
// marketplace and returns the catalog base dir + a cleanup func.
func (r *Registry) resolvePlugin(ctx context.Context, pluginName, mktName string, fetch TreeFetcher) (PluginEntry, string, string, func(), error) {
	markets, err := r.Markets()
	if err != nil {
		return PluginEntry{}, "", "", func() {}, err
	}
	if len(markets) == 0 {
		return PluginEntry{}, "", "", func() {}, fmt.Errorf("no marketplaces added (eigen marketplace add <owner/repo>)")
	}
	for _, m := range markets {
		if mktName != "" && !strings.EqualFold(m.Name, mktName) {
			continue
		}
		mkt, base, cleanup, ferr := r.fetchMarketplace(ctx, m, fetch)
		if ferr != nil {
			if mktName != "" {
				return PluginEntry{}, "", "", func() {}, ferr
			}
			continue // try the next marketplace
		}
		if entry, ok := mkt.Find(pluginName); ok {
			return entry, m.Name, base, cleanup, nil
		}
		cleanup()
	}
	where := "any marketplace"
	if mktName != "" {
		where = "marketplace " + mktName
	}
	return PluginEntry{}, "", "", func() {}, fmt.Errorf("plugin %q not found in %s", pluginName, where)
}

// resolvePluginRoot returns the plugin bundle's on-disk dir. A local source is
// a subdir of the marketplace repo; a git/github source is fetched separately.
func (r *Registry) resolvePluginRoot(ctx context.Context, entry PluginEntry, marketBase string, fetch TreeFetcher) (string, func(), error) {
	if entry.Source.IsLocal() {
		root, err := safeJoinUnder(marketBase, entry.Source.Path, "plugin source")
		if err != nil {
			return "", func() {}, err
		}
		return root, func() {}, nil
	}
	// External repo (git/github): owner/repo[@ref] in Repo.
	ref, err := skill.ParseGitHubRef(entry.Source.Repo)
	if err != nil {
		return "", func() {}, fmt.Errorf("plugin source %q: %w", entry.Source.Repo, err)
	}
	if entry.Source.Ref != "" {
		ref.Ref = entry.Source.Ref
	}
	tmp, err := os.MkdirTemp("", "eigen-plugin-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(tmp) }
	root, err := fetch(ctx, ref.Owner, ref.Repo, ref.Ref, tmp)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	pluginRoot, err := safeJoinUnder(root, ref.Path, "plugin repo")
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return pluginRoot, cleanup, nil
}

// installSkillDir copies a plugin's skill directory into ~/.eigen/skills/<inst>.
// The Claude plugin-root placeholder in SKILL.md is rewritten to OUR namespace
// (${EIGEN_PLUGIN_ROOT}), and a ".eigen-root" sidecar records the cached bundle
// path — the skill loader (skill.Body) expands the ref from the sidecar at read
// time, so the path lives in one place (consistent with the MCP env param)
// rather than smeared as a literal through the skill text.
func (r *Registry) installSkillDir(srcDir, instName, bundleRoot string, overwrite bool) error {
	dst := filepath.Join(r.SkillsDir(), instName)
	if overwrite {
		_ = os.RemoveAll(dst)
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("skill %q already exists at %s", instName, dst)
	}
	if err := copyTree(srcDir, dst); err != nil {
		return err
	}
	// Rewrite the Claude root placeholder to our namespaced ref in SKILL.md.
	smd := filepath.Join(dst, "SKILL.md")
	if b, err := os.ReadFile(smd); err == nil {
		_ = os.WriteFile(smd, []byte(toEigenRoot(string(b))), 0o644)
	}
	// Record the bundle path for skill.Body's ${EIGEN_PLUGIN_ROOT} expansion.
	_ = os.WriteFile(filepath.Join(dst, ".eigen-root"), []byte(bundleRoot+"\n"), 0o644)
	return nil
}

func expandRoot(s, root string) string { return strings.ReplaceAll(s, pluginRootVar, root) }

// toEigenRoot rewrites the Claude root placeholder to OUR namespaced env ref
// (${EIGEN_PLUGIN_ROOT}), used in MCP command/args/env that the loader expands
// against the server env at launch.
func toEigenRoot(s string) string { return strings.ReplaceAll(s, pluginRootVar, eigenRootVar) }

// installCommand writes a plugin slash command to ~/.eigen/commands/<inst>.md,
// expanding ${CLAUDE_PLUGIN_ROOT} so any bundled-file references resolve.
func (r *Registry) installCommand(instName, content, bundleRoot string, overwrite bool) error {
	dir := r.CommandsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, instName+".md")
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("command %q already exists at %s", instName, path)
		}
	}
	return os.WriteFile(path, []byte(expandRoot(content, bundleRoot)), 0o644)
}

// copyTree recursively copies src into dst (files 0644, dirs 0755). Symlinks
// are skipped. dst is created.
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", src)
	}
	return filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		switch {
		case fi.IsDir():
			return os.MkdirAll(target, 0o755)
		case fi.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.WriteFile(target, b, 0o644)
		default:
			return nil // skip symlinks/special
		}
	})
}

// jsonObj is a free-form JSON object for surgical config edits that preserve
// unknown fields.
type jsonObj = map[string]any

// readObj reads a JSON object file (empty/missing → empty object).
func readObj(path string) (jsonObj, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) || len(strings.TrimSpace(string(b))) == 0 {
		return jsonObj{}, nil
	}
	if err != nil {
		return nil, err
	}
	var o jsonObj
	if err := json.Unmarshal(b, &o); err != nil {
		return nil, err
	}
	if o == nil {
		o = jsonObj{}
	}
	return o, nil
}

func writeObj(path string, o jsonObj) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
