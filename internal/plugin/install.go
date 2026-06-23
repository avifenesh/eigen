package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/skill"
)

// pluginRootVar is the Claude placeholder that expands to a plugin's installed
// root dir. Codex plugins use CODEX_PLUGIN_ROOT for the same purpose. Bundled
// scripts/MCP commands reference it so they never hardcode a path. On install we
// rewrite either placeholder to eigenRootVar (OUR namespace) and provide the
// value via the EIGEN_PLUGIN_ROOT env param — so the path lives in ONE env
// variable, not smeared as a literal through every arg. The MCP loader expands
// ${EIGEN_PLUGIN_ROOT} in command/args at launch against the server's env.
const pluginRootVar = "${CLAUDE_PLUGIN_ROOT}"

const codexPluginRootVar = "${CODEX_PLUGIN_ROOT}"

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
	Warnings []string      // non-fatal notes (e.g. app integrations that could not be wired)
}

// ScanFinding is one component's risky scan verdict (surfaced to the user).
type ScanFinding struct {
	Component string   `json:"component"`
	Reasons   []string `json:"reasons"`
}

// PluginPreview is a read-only manifest/component summary for marketplace UI.
// It intentionally carries names/counts, not full prompt bodies.
type PluginPreview struct {
	Entry       PluginEntry     `json:"entry"`
	Marketplace string          `json:"marketplace"`
	Manifest    *PluginManifest `json:"manifest,omitempty"`
	Skills      []string        `json:"skills,omitempty"`
	Agents      []string        `json:"agents,omitempty"`
	Commands    []string        `json:"commands,omitempty"`
	MCPServers  []string        `json:"mcp_servers,omitempty"`
	Hooks       int             `json:"hooks,omitempty"`
	Apps        int             `json:"apps,omitempty"`
	Warnings    []string        `json:"warnings,omitempty"`
}

// AddMarketplace fetches a catalog repo, parses its marketplace.json, and
// records it. ref is optional (default branch). Returns the parsed catalog so a
// caller can immediately list its plugins.
func (r *Registry) AddMarketplace(ctx context.Context, source string, fetch TreeFetcher) (*Marketplace, MarketRecord, error) {
	if fetch == nil {
		fetch = DefaultTreeFetcher
	}
	if mkt, rec, ok, err := r.addDirectMarketplace(ctx, source); ok || err != nil {
		return mkt, rec, err
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
	b, _, err := readMarketplaceManifest(base)
	if err != nil {
		return nil, MarketRecord{}, fmt.Errorf("no marketplace.json in %s (looked for .claude-plugin/marketplace.json, .agents/plugins/marketplace.json, marketplace.json): %w", source, err)
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
	if mkt, base, ok, err := r.fetchDirectMarketplace(ctx, rec.Source); ok || err != nil {
		return mkt, base, func() {}, err
	}
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
	b, _, err := readMarketplaceManifest(base)
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

func (r *Registry) addDirectMarketplace(ctx context.Context, source string) (*Marketplace, MarketRecord, bool, error) {
	mkt, base, ok, err := r.fetchDirectMarketplace(ctx, source)
	if !ok || err != nil {
		return nil, MarketRecord{}, ok, err
	}
	rec := MarketRecord{Name: mkt.Name, Source: source, Owner: mkt.Owner.Name}
	if err := r.AddMarket(rec); err != nil {
		return nil, MarketRecord{}, true, err
	}
	_ = base
	return mkt, rec, true, nil
}

func (r *Registry) fetchDirectMarketplace(ctx context.Context, source string) (*Marketplace, string, bool, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, "", false, nil
	}
	if strings.HasPrefix(source, "http://") {
		return nil, "", true, fmt.Errorf("marketplace URL must use https: %s", source)
	}
	if isHTTP(source) && !strings.Contains(source, "github.com/") {
		b, err := fetchURL(ctx, source)
		if err != nil {
			return nil, "", true, err
		}
		mkt, err := ParseMarketplace(b)
		return mkt, "", true, err
	}
	if isHTTP(source) && strings.HasSuffix(strings.ToLower(strings.Split(source, "?")[0]), ".json") {
		b, err := fetchURL(ctx, source)
		if err != nil {
			return nil, "", true, err
		}
		mkt, err := ParseMarketplace(b)
		return mkt, "", true, err
	}
	if isLocalPath(source) {
		path := source
		if strings.HasPrefix(path, "file://") {
			path = strings.TrimPrefix(path, "file://")
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, "", true, err
		}
		base := path
		var b []byte
		if info.IsDir() {
			var err error
			b, _, err = readMarketplaceManifest(path)
			if err != nil {
				return nil, "", true, err
			}
		} else {
			var err error
			b, err = os.ReadFile(path)
			if err != nil {
				return nil, "", true, err
			}
			base = filepath.Dir(path)
		}
		mkt, err := ParseMarketplace(b)
		return mkt, base, true, err
	}
	return nil, "", false, nil
}

func readMarketplaceManifest(base string) ([]byte, string, error) {
	candidates := []string{
		filepath.Join(base, ".claude-plugin", "marketplace.json"),
		filepath.Join(base, ".agents", "plugins", "marketplace.json"),
		filepath.Join(base, "marketplace.json"),
	}
	var last error
	for _, p := range candidates {
		b, err := os.ReadFile(p)
		if err == nil {
			return b, p, nil
		}
		last = err
	}
	if last == nil {
		last = os.ErrNotExist
	}
	return nil, "", last
}

func fetchURL(ctx context.Context, url string) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("marketplace URL must use https: %s", url)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "eigen/plugin-install")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFileBytes {
		return nil, fmt.Errorf("remote marketplace exceeds %d bytes", maxFileBytes)
	}
	return b, nil
}

func isHTTP(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func isLocalPath(s string) bool {
	return strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "file://")
}

// PreviewPlugin resolves a marketplace plugin and returns its manifest/component
// summary without scanning or wiring anything. It may fetch plugin sources, so
// callers should run it off the UI goroutine.
func (r *Registry) PreviewPlugin(ctx context.Context, pluginName, mktName string, fetch TreeFetcher) (*PluginPreview, error) {
	if err := SafeName(pluginName); err != nil {
		return nil, err
	}
	if fetch == nil {
		fetch = DefaultTreeFetcher
	}
	entry, mkt, base, cleanup, err := r.resolvePlugin(ctx, pluginName, mktName, fetch)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	pluginRoot, extraCleanup, err := r.resolvePluginRoot(ctx, entry, base, fetch)
	if err != nil {
		return nil, err
	}
	defer extraCleanup()
	comps, err := Discover(pluginRoot, !entry.strictMode())
	if err != nil {
		return nil, err
	}
	return buildPluginPreview(entry, mkt, comps), nil
}

func buildPluginPreview(entry PluginEntry, mkt string, comps *Components) *PluginPreview {
	pv := &PluginPreview{Entry: entry, Marketplace: mkt}
	if comps == nil {
		return pv
	}
	pv.Manifest = comps.Manifest
	for _, sf := range comps.Skills {
		pv.Skills = append(pv.Skills, sf.Name)
	}
	for _, af := range comps.Agents {
		pv.Agents = append(pv.Agents, af.Name)
	}
	for _, cf := range comps.Commands {
		pv.Commands = append(pv.Commands, cf.Name)
	}
	for _, s := range comps.MCPServers {
		pv.MCPServers = append(pv.MCPServers, s.Name)
	}
	for _, s := range comps.AppServers {
		pv.MCPServers = append(pv.MCPServers, "app-"+s.Name)
	}
	pv.Hooks = len(comps.Hooks)
	pv.Apps = max(0, comps.Apps-len(comps.AppServers))
	if pv.Apps > 0 {
		pv.Warnings = append(pv.Warnings, fmt.Sprintf("%d Codex app integration(s) could not be wired as MCP servers", pv.Apps))
	}
	return pv
}

// InstallPlugin installs a plugin by name from a recorded marketplace (mktName
// optional — if empty, the first marketplace listing the plugin wins). It
// fetches the plugin's tree, scans each skill/command body, and wires
// components into the global per-scope configs. CLI-only (the agent never
// calls this); only CLI/TUI/app user actions do.
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

	desc := entry.Description
	version := entry.Version
	if comps.Manifest != nil {
		desc = firstNonEmpty(desc, firstNonEmpty(comps.Manifest.Description, comps.Manifest.Interface.ShortDescription))
		version = firstNonEmpty(version, comps.Manifest.Version)
	}
	res := &InstallResult{}
	res.Plugin = InstalledPlugin{
		Name: pluginName, Marketplace: mkt, Version: version,
		Description: desc, Root: dest,
	}
	scanCount := 0
	committed := false
	defer func() {
		if !committed {
			r.cleanupPluginFiles(res.Plugin)
		}
	}()

	// 1) Scan + wire skills (copy each skill dir; scan its SKILL.md body).
	for _, sf := range comps.Skills {
		if opts.Scanner != nil {
			sr, serr := opts.Scanner.Scan(ctx, sf.Name, sf.Content)
			if serr != nil {
				return nil, fmt.Errorf("scan skill %q: %w", sf.Name, serr)
			}
			scanCount++
			if !sr.Safe {
				res.Scans = append(res.Scans, ScanFinding{Component: "skill:" + sf.Name, Reasons: sr.Reasons})
				if !opts.Force {
					return nil, &skill.RiskyError{Name: pluginName + "/" + sf.Name, Reasons: sr.Reasons}
				}
			}
		}
		instName := pluginName + "-" + sf.Name // namespace to avoid collisions
		if err := r.installSkillDir(sf.Dir, instName, dest, opts.Overwrite); err != nil {
			return nil, fmt.Errorf("install skill %q: %w", sf.Name, err)
		}
		res.Plugin.Skills = append(res.Plugin.Skills, instName)
	}

	// 2) Wire MCP servers (niche, gated, ${ROOT}-expanded) into mcp.json. An MCP
	// server is a subprocess command, so scan it like skills/agents — a malicious
	// command is just as dangerous as a malicious instruction. discoverMCP keeps
	// every parsed .mcp.json entry, including ones with neither a runnable command
	// nor a transport the loader can connect/report — the same shape discoverApps
	// drops up front. Skip those here (and warn) so MCPServers counts only servers
	// the loader will actually try, not entries it would silently reject.
	for _, s := range comps.MCPServers {
		if !mcpRunnable(s) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("mcp server %q skipped: no command and no supported transport (url/type) — not runnable", s.Name))
			continue
		}
		if serr := scanCommandComponent(ctx, opts, res, "mcp", pluginName, s.Name, mcpScanBody(s), &scanCount); serr != nil {
			return nil, serr
		}
		name := pluginName + "-" + s.Name
		if err := r.addMCPServer(name, s, dest, entry); err != nil {
			return nil, fmt.Errorf("wire mcp %q: %w", s.Name, err)
		}
		res.Plugin.MCPServers = append(res.Plugin.MCPServers, name)
	}
	for _, s := range comps.AppServers {
		if serr := scanCommandComponent(ctx, opts, res, "mcp", pluginName, "app-"+s.Name, mcpScanBody(s), &scanCount); serr != nil {
			return nil, serr
		}
		name := pluginName + "-app-" + s.Name
		if err := r.addMCPServer(name, s, dest, entry); err != nil {
			return nil, fmt.Errorf("wire app mcp %q: %w", s.Name, err)
		}
		res.Plugin.MCPServers = append(res.Plugin.MCPServers, name)
	}

	// 3) Wire hooks (${ROOT}-expanded) into hooks.json. A hook auto-runs a shell
	// command on agent events, so scan each command body before wiring.
	for i, h := range comps.Hooks {
		if serr := scanCommandComponent(ctx, opts, res, "hook", pluginName, fmt.Sprintf("%s[%d]", h.Event, i), hookScanBody(h), &scanCount); serr != nil {
			return nil, serr
		}
	}
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
			scanCount++
			if !sr.Safe {
				res.Scans = append(res.Scans, ScanFinding{Component: "command:" + cf.Name, Reasons: sr.Reasons})
				if !opts.Force {
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

	// 5) Install Claude/Codex agents as native Eigen task roles. The original
	// markdown prompt is preserved in ~/.eigen/agents/<role>.md, while parsed
	// frontmatter supplies routing and task_group read-only metadata.
	for _, af := range comps.Agents {
		content := af.Content
		if opts.Scanner != nil {
			sr, serr := opts.Scanner.Scan(ctx, af.Name, content)
			if serr != nil {
				return nil, fmt.Errorf("scan agent %q: %w", af.Name, serr)
			}
			scanCount++
			if !sr.Safe {
				res.Scans = append(res.Scans, ScanFinding{Component: "agent:" + af.Name, Reasons: sr.Reasons})
				if !opts.Force {
					return nil, &skill.RiskyError{Name: pluginName + "/" + af.Name, Reasons: sr.Reasons}
				}
			}
		}
		instName := pluginName + "-agent-" + safeComponentName(af.Name)
		if err := r.installAgentFile(instName, content, opts.Overwrite); err != nil {
			return nil, fmt.Errorf("install agent %q: %w", af.Name, err)
		}
		res.Plugin.Agents = append(res.Plugin.Agents, instName)
		role, kindWarn := installedAgentRole(instName, af)
		if kindWarn != "" {
			res.Warnings = append(res.Warnings, fmt.Sprintf("agent %q: %s", af.Name, kindWarn))
		}
		res.Plugin.AgentRoles = append(res.Plugin.AgentRoles, role)
	}

	if unwiredApps := comps.Apps - len(comps.AppServers); unwiredApps > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d Codex app integration(s) could not be wired as MCP servers", unwiredApps))
	}
	switch {
	case opts.Scanner == nil:
		res.Plugin.ScanStatus = ScanStatusSkipped
	case len(res.Scans) > 0:
		res.Plugin.ScanStatus = ScanStatusForced
		res.Warnings = append([]string{"forced install: security scan reported risky components"}, res.Warnings...)
	default:
		res.Plugin.ScanStatus = ScanStatusClean
	}
	res.Plugin.ScanCount = scanCount
	res.Plugin.Scans = append([]ScanFinding(nil), res.Scans...)
	res.Plugin.Warnings = append([]string(nil), res.Warnings...)

	if err := r.RecordInstall(res.Plugin); err != nil {
		return nil, err
	}
	committed = true
	return res, nil
}

// scanCommandComponent runs a hook/MCP command body through the scanner with the
// same shape skills/agents use: it counts the scan, records a RISKY verdict in
// res.Scans, and returns a RiskyError (so the caller aborts) unless opts.Force is
// set. A nil scanner or a safe verdict returns nil. kind is "mcp" or "hook".
func scanCommandComponent(ctx context.Context, opts InstallOptions, res *InstallResult, kind, pluginName, compName, body string, scanCount *int) error {
	if opts.Scanner == nil {
		return nil
	}
	sr, serr := opts.Scanner.Scan(ctx, compName, body)
	if serr != nil {
		return fmt.Errorf("scan %s %q: %w", kind, compName, serr)
	}
	*scanCount++
	if !sr.Safe {
		res.Scans = append(res.Scans, ScanFinding{Component: kind + ":" + compName, Reasons: sr.Reasons})
		if !opts.Force {
			return &skill.RiskyError{Name: pluginName + "/" + compName, Reasons: sr.Reasons}
		}
	}
	return nil
}

// mcpRunnable reports whether an MCP server has something the loader can act on:
// a stdio command, or a remote transport (a url, or a type the loader's
// isRemoteServer recognizes — http/sse and the streamable-http aliases). A server
// with none of these is the loader's "empty name or command" reject case; mirror
// the discoverApps len(cmd)==0 && url=="" guard so install never counts an entry
// the loader will silently drop. Kept in sync with mcp.isRemoteServer.
func mcpRunnable(s MCPServer) bool {
	if len(s.Command) > 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(s.Type)) {
	case "http", "sse", "streamable-http", "streamable_http":
		return true
	}
	return strings.TrimSpace(s.URL) != ""
}

// mcpScanBody renders an MCP server's launch surface (command + args + env) into
// a single text blob for the scanner — the executable bits a malicious server
// would hide its payload in.
func mcpScanBody(s MCPServer) string {
	var b strings.Builder
	for i, c := range s.Command {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(c)
	}
	for _, a := range s.Args {
		b.WriteByte(' ')
		b.WriteString(a)
	}
	if strings.TrimSpace(s.URL) != "" {
		b.WriteByte(' ')
		b.WriteString(s.URL)
	}
	for k, v := range s.Env {
		b.WriteByte('\n')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
	}
	return b.String()
}

// hookScanBody renders a hook's shell command into a single line for the scanner.
func hookScanBody(h HookSpec) string {
	return strings.Join(h.Command, " ")
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
		if m.Disabled {
			if mktName != "" {
				return PluginEntry{}, "", "", func() {}, fmt.Errorf("marketplace %q is disabled (enable it first)", m.Name)
			}
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
		if marketBase == "" {
			return "", func() {}, fmt.Errorf("plugin %q uses a relative source but marketplace %q has no local base", entry.Name, entry.Source.Path)
		}
		root, err := safeJoinUnder(marketBase, entry.Source.Path, "plugin source")
		if err != nil {
			return "", func() {}, err
		}
		return root, func() {}, nil
	}
	// External repo (git/github/url/git-subdir): owner/repo[@ref] or a GitHub URL.
	repo := entry.Source.Repo
	if repo == "" {
		return "", func() {}, fmt.Errorf("plugin source for %q has no repo/url", entry.Name)
	}
	ref, err := skill.ParseGitHubRef(repo)
	if err != nil {
		return "", func() {}, fmt.Errorf("plugin source %q: %w", repo, err)
	}
	if r := entry.Source.EffectiveRef(); r != "" {
		ref.Ref = r
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
	pluginPath := ref.Path
	if strings.TrimSpace(entry.Source.Path) != "" {
		pluginPath = entry.Source.Path
	}
	pluginRoot, err := safeJoinUnder(root, pluginPath, "plugin repo")
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

func (r *Registry) installAgentFile(instName, content string, overwrite bool) error {
	dst := filepath.Join(r.AgentsDir(), instName+".md")
	if overwrite {
		_ = os.Remove(dst)
		_ = os.Remove(dst + ".disabled")
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("agent %q already exists at %s", instName, dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(toEigenRoot(content)), 0o644)
}

// installedAgentRole builds the read-only routing metadata for a plugin agent.
// A second return value carries a non-fatal warning when the agent declared a
// routing kind we could not honor, so the caller can surface it on the install
// result instead of silently dropping the hint.
func installedAgentRole(instName string, af AgentFile) (InstalledAgentRole, string) {
	tools, readOnly := normalizeAgentTools(af.Tools, af.ReadOnly, af.ReadOnlySet)
	kind, warn := normalizeAgentKind(af.Kind)
	return InstalledAgentRole{
		Name:        instName,
		SourceName:  af.Name,
		Description: af.Description,
		Kind:        kind,
		Difficulty:  normalizeAgentDifficulty(af.Difficulty),
		Model:       strings.TrimSpace(af.Model),
		Tools:       tools,
		ReadOnly:    readOnly,
	}, warn
}

// normalizeAgentKind validates a plugin agent's declared routing kind against
// the router's canonical TaskKind set (llm.ParseTaskKind) rather than a list
// duplicated here, mapping any accepted alias (e.g. "web"→"search") to its
// canonical name. It returns the canonical kind plus a non-empty warning when a
// declared kind is recognized by neither — so the dropped hint is surfaced, not
// swallowed. An empty/unset kind is fine (no hint, no warning).
func normalizeAgentKind(kind string) (string, string) {
	trimmed := strings.TrimSpace(kind)
	if trimmed == "" {
		return "", ""
	}
	tk, ok := llm.ParseTaskKind(trimmed)
	if !ok {
		return "", fmt.Sprintf("agent routing kind %q is not recognized (expected one of %s) — routing hint dropped", trimmed, strings.Join(canonicalAgentKinds(), ", "))
	}
	return canonicalAgentKindName(tk), ""
}

// canonicalAgentKindName renders a router TaskKind as its canonical string,
// keeping install metadata aligned with the llm package's enum.
func canonicalAgentKindName(tk llm.TaskKind) string {
	switch tk {
	case llm.TaskSearch:
		return "search"
	case llm.TaskVision:
		return "vision"
	case llm.TaskSocial:
		return "social"
	default:
		return "general"
	}
}

// canonicalAgentKinds lists the canonical kind names (for warning messages),
// derived from the router's TaskKind constants.
func canonicalAgentKinds() []string {
	return []string{
		canonicalAgentKindName(llm.TaskGeneral),
		canonicalAgentKindName(llm.TaskSearch),
		canonicalAgentKindName(llm.TaskVision),
		canonicalAgentKindName(llm.TaskSocial),
	}
}

func normalizeAgentDifficulty(d string) string {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "trivial", "easy", "medium", "hard":
		return strings.ToLower(strings.TrimSpace(d))
	default:
		return ""
	}
}

// normalizeAgentTools is the task_group trust boundary for plugin agents.
// Only local, built-in read-only inspection tools are admitted. Prompt-loading
// or model-calling helpers (skill/review/fetch/websearch) stay out of plugin
// read-only roles even if their tool definitions are non-mutating, because they
// can broaden the prompt surface or require approvals.
func normalizeAgentTools(tools []string, readOnlyFlag, readOnlySet bool) ([]string, bool) {
	if readOnlySet && !readOnlyFlag {
		return nil, false
	}
	if len(tools) == 0 {
		if readOnlyFlag {
			return defaultPluginAgentReadOnlyTools(), true
		}
		return nil, false
	}
	seen := map[string]bool{}
	var out []string
	for _, t := range tools {
		norm, ok := normalizeAgentToolName(t)
		if !ok {
			return nil, false
		}
		if !seen[norm] {
			seen[norm] = true
			out = append(out, norm)
		}
	}
	return out, true
}

func defaultPluginAgentReadOnlyTools() []string {
	return []string{"read", "grep", "glob", "list", "tree", "symbols", "diff"}
}

func normalizeAgentToolName(tool string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(tool))
	key = strings.TrimPrefix(key, "mcp__")
	key = strings.ReplaceAll(key, "-", "_")
	switch key {
	case "read", "grep", "glob", "tree", "symbols", "diff":
		return key, true
	case "ls", "list":
		return "list", true
	default:
		return "", false
	}
}

func safeComponentName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "agent"
	}
	return out
}

func expandRoot(s, root string) string {
	s = strings.ReplaceAll(s, pluginRootVar, root)
	return strings.ReplaceAll(s, codexPluginRootVar, root)
}

// toEigenRoot rewrites the Claude root placeholder to OUR namespaced env ref
// (${EIGEN_PLUGIN_ROOT}), used in MCP command/args/env that the loader expands
// against the server env at launch.
func toEigenRoot(s string) string {
	s = strings.ReplaceAll(s, pluginRootVar, eigenRootVar)
	return strings.ReplaceAll(s, codexPluginRootVar, eigenRootVar)
}

// ExpandInstalledRoot resolves stored ${EIGEN_PLUGIN_ROOT} references for a
// plugin component at runtime. Installers keep the placeholder on disk so
// records can move as one bundle; role prompts expand it from InstalledPlugin.Root.
func ExpandInstalledRoot(s, root string) string {
	if strings.TrimSpace(root) == "" {
		return s
	}
	return strings.ReplaceAll(s, eigenRootVar, root)
}

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
