package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/tool"
)

// serverConfig is one MCP server entry in mcp.json.
type serverConfig struct {
	Name    string            `json:"name"`
	Command []string          `json:"command"`
	Env     map[string]string `json:"env"`

	// URL and Type describe a REMOTE MCP server (HTTP / SSE transport) rather
	// than a stdio subprocess. The plugin layer (discoverMCP/addMCPServer)
	// already parses and persists url+type, so they must round-trip here or a
	// remote server entry would look like a config with an empty command and be
	// silently rejected. eigen's transport is stdio-only today, so a remote
	// entry is recognized and reported as unsupported (see LoadTools) rather
	// than dropped.
	URL  string `json:"url,omitempty"`
	Type string `json:"type,omitempty"`

	// Description is the one-line "what is this server" shown to the model at
	// the top level of progressive disclosure (e.g. "drive the user's real
	// Chrome", "isolated Linux sandbox"). REQUIRED in practice: it's how eigen
	// presents the server before the model drills in. When empty, eigen falls
	// back to the server's own MCP `instructions`, then warns — an undescribed
	// server is opaque to the model.
	Description string `json:"description"`

	// Tools, when non-empty, is an allowlist: only the named server tools are
	// exposed to the model (names as the server declares them, without the
	// "<server>_" prefix; "*" suffix allowed for prefix matches). Tool schemas
	// ride along on EVERY model request, so a server with dozens of tools can
	// quietly cost thousands of tokens per call — allowlist what you use.
	Tools []string `json:"tools"`

	// ExcludeTools removes specific server tools (same name syntax). Applied
	// after Tools.
	ExcludeTools []string `json:"exclude_tools"`

	// Disabled skips this server entirely (kept in config, not connected) —
	// toggled from the app's plugins page.
	Disabled bool `json:"disabled,omitempty"`
}

type mcpConfig struct {
	Servers []serverConfig `json:"servers"`
}

const connectTimeout = 15 * time.Second

// connectFailCooldown is how long a failed lazy connect is remembered so that
// repeated tool calls fail fast (returning the cached error) instead of each
// spawning a fresh subprocess and blocking up to connectTimeout before failing
// the same way. It's short enough that a server which comes up after a transient
// failure recovers on the next call past the window.
const connectFailCooldown = 5 * time.Second

// Handle is a per-session MCP resource returned by LoadTools. It may represent
// a lazily-started server; callers should keep it for the session lifetime and
// Close it when the session exits.
type Handle interface{ Close() error }

// LoadTools reads an mcp.json config and returns its tools wrapped as eigen
// tool Definitions (named "<server>_<tool>"). Servers are probed briefly to
// learn their tool schemas, then closed; the long-lived MCP subprocess is
// started lazily on first tool invocation. A missing config file yields no
// tools and no error; a server that fails to probe is reported in errs but does
// not abort the others.
func LoadTools(ctx context.Context, path string) (defs []tool.Definition, clients []Handle, errs []error) {
	var cfg mcpConfig
	if data, err := os.ReadFile(path); err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, []error{err}
		}
		// Missing config is fine — built-ins may still apply.
	} else if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, nil, []error{fmt.Errorf("%s: %w", path, err)}
	}

	// Built-in servers (e.g. the agent workspace) are auto-registered as
	// first-class capabilities when their binary is present, UNLESS the user
	// already configured a server with the same name (their config wins).
	cfg.Servers = withBuiltinServers(cfg.Servers)

	for _, sc := range cfg.Servers {
		if sc.Disabled {
			continue
		}
		// Remote MCP (HTTP / SSE) servers carry a url/type but no command. eigen's
		// transport is stdio-only, so we can't connect them yet — but we MUST NOT
		// silently drop them as if they were a malformed stdio entry. Report them
		// explicitly so the failure is visible (and easy to grep for) rather than a
		// remote server that just never shows up.
		if isRemoteServer(sc) {
			errs = append(errs, fmt.Errorf("mcp %q: remote MCP (%s) not yet supported (url=%s) — eigen currently connects stdio MCP servers only", serverName(sc), remoteType(sc), sc.URL))
			fmt.Fprintf(os.Stderr, "eigen: mcp %q: remote MCP (%s) not yet supported — only stdio (command) MCP servers connect today; skipping %s\n", serverName(sc), remoteType(sc), sc.URL)
			continue
		}
		if sc.Name == "" || len(sc.Command) == 0 {
			errs = append(errs, fmt.Errorf("mcp server with empty name or command"))
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, connectTimeout)
		env := serverEnv(sc.Env)
		command := expandCommand(sc.Command, env)
		probe, err := Connect(cctx, command, env)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("mcp %q: %w", sc.Name, err))
			continue
		}
		lctx, cancel := context.WithTimeout(ctx, connectTimeout)
		specs, err := probe.ListTools(lctx)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("mcp %q: list tools: %w", sc.Name, err))
			_ = probe.Close()
			continue
		}
		// Group description (Level-0 frontmatter): config description wins, else
		// the server's own MCP instructions, else a warning + generic gist.
		gist := strings.TrimSpace(sc.Description)
		if gist == "" {
			gist = firstSentence(probe.Instructions())
		}
		_ = probe.Close()
		client := newLazyClient(sc.Name, command, env)
		clients = append(clients, client)
		if gist == "" {
			gist = sc.Name + " MCP server"
			fmt.Fprintf(os.Stderr, "eigen: mcp %q has no description — add \"description\" in mcp.json so the model knows what it's for\n", sc.Name)
		}
		kept := 0
		for _, sp := range specs {
			if !toolAllowed(sc, sp.Name) {
				continue
			}
			defs = append(defs, wrapLazy(client, sc.Name, gist, sp))
			kept++
		}
		if len(sc.Tools) > 0 || len(sc.ExcludeTools) > 0 {
			fmt.Fprintf(os.Stderr, "eigen: mcp %q: %d/%d tools exposed (filtered by mcp.json)\n", sc.Name, kept, len(specs))
		}
	}
	return defs, clients, errs
}

// lazyClient owns a per-session MCP server that is started only when one of
// its tools is actually invoked. LoadTools still probes each server once to
// learn schemas for search_tools, but those probe processes are closed before
// the session starts serving turns.
type lazyClient struct {
	name    string
	command []string
	env     []string
	connect func(context.Context, []string, []string) (*Client, error)

	// failCooldown is how long a failed connect is remembered (fail fast). Zero
	// disables the cooldown entirely — every failed call reconnects immediately.
	failCooldown time.Duration
	// clock is overridable in tests; nil means time.Now.
	clock func() time.Time

	mu     sync.Mutex
	client *Client
	closed bool

	// lastErr / retryAfter cache the most recent connect failure. While now() is
	// before retryAfter, get() returns lastErr without spawning a process, so a
	// server that's down doesn't stall every tool call for connectTimeout.
	lastErr    error
	retryAfter time.Time
}

func newLazyClient(name string, command, env []string) *lazyClient {
	return &lazyClient{
		name:         name,
		command:      append([]string(nil), command...),
		env:          append([]string(nil), env...),
		connect:      Connect,
		failCooldown: connectFailCooldown,
	}
}

func (c *lazyClient) get(ctx context.Context) (*Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, fmt.Errorf("mcp %q: client closed", c.name)
	}
	if c.client != nil {
		if c.client.alive() {
			return c.client, nil
		}
		// The cached connection's read loop died (stream closed, or a fatal
		// read error such as an oversized JSON-RPC line). Drop it and reconnect
		// below rather than handing out a corpse that fails every call forever.
		_ = c.client.Close()
		c.client = nil
	}
	// Fail fast while a recent connect failure is still cooling down: a server
	// that's down (binary missing, crash on startup) would otherwise have every
	// tool call spawn a fresh subprocess and block up to connectTimeout before
	// failing identically. Past the cooldown we fall through and retry, so a
	// server that recovers is picked up.
	if c.lastErr != nil && c.now().Before(c.retryAfter) {
		return nil, c.lastErr
	}
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	client, err := c.connect(cctx, c.command, c.env)
	cancel()
	if err != nil {
		c.lastErr = fmt.Errorf("mcp %q: %w", c.name, err)
		if c.failCooldown > 0 {
			c.retryAfter = c.now().Add(c.failCooldown)
		}
		return nil, c.lastErr
	}
	c.lastErr = nil
	c.retryAfter = time.Time{}
	c.client = client
	return client, nil
}

// now returns the lazyClient's clock, defaulting to time.Now when unset (tests
// override it to exercise the cooldown deterministically).
func (c *lazyClient) now() time.Time {
	if c.clock != nil {
		return c.clock()
	}
	return time.Now()
}

func (c *lazyClient) CallToolRich(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
	client, err := c.get(ctx)
	if err != nil {
		return ToolResult{}, err
	}
	return client.CallToolRich(ctx, name, args)
}

func (c *lazyClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	client := c.client
	c.client = nil
	c.mu.Unlock()
	if client != nil {
		return client.Close()
	}
	return nil
}

// started reports whether the long-lived server has been opened. Tests use it
// to prove LoadTools returns lazy handles; production only relies on Close.
func (c *lazyClient) started() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client != nil
}

// isRemoteServer reports whether sc describes a remote (HTTP / SSE) MCP server
// rather than a stdio subprocess. A remote entry is identified by a transport
// type of http/sse, or by a bare URL with no command — the shape the plugin
// layer persists for remote servers. A stdio entry that also happens to carry a
// command is treated as stdio (command wins), so a future remote transport can
// be added without re-routing those.
func isRemoteServer(sc serverConfig) bool {
	if len(sc.Command) > 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(sc.Type)) {
	case "http", "sse", "streamable-http", "streamable_http":
		return true
	}
	return strings.TrimSpace(sc.URL) != ""
}

// remoteType returns a human-readable transport label for a remote server entry
// (for log/error messages), defaulting to "http" when only a URL is given.
func remoteType(sc serverConfig) string {
	if t := strings.ToLower(strings.TrimSpace(sc.Type)); t != "" {
		return t
	}
	return "http"
}

// serverName returns sc.Name, or a placeholder when unnamed, for messages.
func serverName(sc serverConfig) string {
	if n := strings.TrimSpace(sc.Name); n != "" {
		return n
	}
	return "<unnamed>"
}

// toolAllowed applies the per-server allowlist/excludelist to a server-declared
// tool name. Patterns match exactly, or as a prefix when ending in "*".
func toolAllowed(sc serverConfig, name string) bool {
	match := func(pat string) bool {
		if strings.HasSuffix(pat, "*") {
			return strings.HasPrefix(name, strings.TrimSuffix(pat, "*"))
		}
		return name == pat
	}
	if len(sc.Tools) > 0 {
		ok := false
		for _, pat := range sc.Tools {
			if match(pat) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, pat := range sc.ExcludeTools {
		if match(pat) {
			return false
		}
	}
	return true
}

type toolCaller interface {
	CallToolRich(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
}

// wrap turns an MCP ToolSpec into an eigen tool.Definition backed by the client.
func wrap(client *Client, server, gist string, sp ToolSpec) tool.Definition {
	return wrapCaller(client, server, gist, sp)
}

func wrapLazy(client *lazyClient, server, gist string, sp ToolSpec) tool.Definition {
	return wrapCaller(client, server, gist, sp)
}

func wrapCaller(client toolCaller, server, gist string, sp ToolSpec) tool.Definition {
	params := slimSchema(sp.InputSchema)
	if len(params) == 0 {
		params = json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	name := sanitize(server) + "_" + sanitize(sp.Name)
	desc := sp.Description
	if desc == "" {
		desc = "MCP tool " + sp.Name + " from server " + server
	}
	toolName := sp.Name
	// Honor the MCP readOnlyHint: a tool the server declares read-only and
	// non-destructive has no side effects, so it can auto-run in gated mode
	// instead of prompting for approval on every call. Anything without an
	// explicit safe hint stays mutating (fail safe).
	readOnly := sp.Annotations != nil && sp.Annotations.ReadOnlyHint && !sp.Annotations.DestructiveHint
	// Screenshot-producing tools (e.g. the sandbox's workspace_screenshot /
	// workspace_observe) return a PNG file PATH rather than an inline image —
	// attach the file so the model can see it. Gated by tool name so an
	// ordinary tool that happens to return a "path" field isn't slurped.
	attachShot := strings.Contains(toolName, "screenshot") || strings.Contains(toolName, "observe")
	capName, capDesc := toolCapability(server, toolName, desc)
	return tool.Definition{
		Name:           name,
		Description:    desc,
		Parameters:     params,
		ReadOnly:       readOnly,
		Capability:     capName,
		CapabilityDesc: capDesc,
		// Progressive disclosure: MCP tools are niche (schema withheld from each
		// request) and grouped by their server, so the model browses the server
		// then opens a tool via search_tools instead of paying for every schema.
		Niche:     true,
		Group:     sanitize(server),
		GroupDesc: gist,
		RunRich: func(ctx context.Context, args json.RawMessage) (tool.Result, error) {
			res, err := client.CallToolRich(ctx, toolName, args)
			if err == nil && attachShot {
				res = attachScreenshotPath(res)
			}
			return tool.Result{Text: res.Text, Images: res.Images}, err
		},
	}
}

func toolCapability(server, toolName, desc string) (string, string) {
	server = strings.ToLower(sanitize(server))
	name := strings.ToLower(sanitize(toolName))
	switch server {
	case "computer_use":
		switch name {
		case "doctor":
			return "diagnostics", "health checks for the real desktop connector"
		case "setup_accessibility", "setup_window_targeting":
			return "accessibility", "enable/verify accessibility and window targeting for real desktop control"
		case "list_apps", "list_windows", "focused_window", "get_app_state", "activate_window":
			return "windows", "inspect apps/windows/focus and activate real desktop windows"
		case "screenshot":
			return "screen", "capture the user's real desktop or a targeted window"
		case "click", "drag", "scroll", "press_key", "type_text":
			return "input", "send pointer, keyboard, scroll, and text input to the real desktop"
		case "perform_action", "set_value":
			return "semantic-actions", "use accessibility semantics to invoke controls or set values"
		}
	case "workspace":
		switch name {
		case "workspace_start", "workspace_stop", "workspace_status", "workspace_list", "workspace_doctor", "workspace_cleanup_stale":
			return "lifecycle", "start, stop, inspect, and clean isolated workspaces"
		case "workspace_run_in_terminal", "workspace_terminal_read", "workspace_terminal_input":
			return "terminal", "run commands and interact with workspace terminals"
		case "workspace_launch_app", "workspace_run_app", "workspace_wait_app", "workspace_kill_app", "workspace_read_app_log":
			return "apps", "launch, monitor, stop, and read logs from GUI apps in the sandbox"
		case "workspace_observe", "workspace_screenshot", "workspace_list_windows", "workspace_events":
			return "screen", "observe screenshots, windows, and events in the isolated desktop"
		case "workspace_open_browser", "workspace_browser_navigate", "workspace_browser_snapshot", "workspace_browser_click", "workspace_browser_targets":
			return "browser", "open and drive the sandbox browser"
		case "workspace_click", "workspace_key", "workspace_type_text", "workspace_paste_text":
			return "input", "send pointer and keyboard input inside the isolated desktop"
		}
	case "chrome":
		switch {
		case name == "chrome_health" || name == "chrome_action_log":
			return "diagnostics", "bridge health and sanitized action logs"
		case name == "chrome_lock_tab" || name == "chrome_unlock_tab" || name == "chrome_locks":
			return "locks", "coordinate tab ownership across agents"
		case strings.Contains(name, "tab") || name == "chrome_reload" || name == "chrome_back" || name == "chrome_forward":
			return "tabs", "list, create, select, close, and navigate browser tabs/history"
		case name == "chrome_snapshot" || name == "chrome_find" || name == "chrome_extract_links" || name == "chrome_extract_tables" || name == "chrome_read_article" || name == "chrome_get_network":
			return "page-read", "read page structure, links, tables, articles, and network observations"
		case name == "chrome_navigate" || name == "chrome_click" || name == "chrome_type" || name == "chrome_scroll":
			return "page-actions", "navigate and interact with the current Chrome page"
		case strings.HasPrefix(name, "chrome_wait_"):
			return "waiting", "wait for selectors, text, or idle page state"
		case name == "chrome_screenshot":
			return "screenshots", "capture Chrome screenshots"
		case strings.HasPrefix(name, "chrome_cdp_") || name == "chrome_get_console":
			return "cdp", "low-level Chrome DevTools Protocol actions and console inspection"
		}
	}
	if desc != "" {
		return "other", "other " + server + " tools"
	}
	return "", ""
}

// slimSchema strips JSON-Schema metadata that costs tokens on every request
// without helping the model pick arguments: "$schema" and "title" keys (at any
// nesting level). The schema's structure, types, descriptions, enums, and
// defaults are preserved. Returns the input unchanged if it isn't a JSON object.
func slimSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	stripped := stripSchemaNoise(v)
	out, err := json.Marshal(stripped)
	if err != nil {
		return raw
	}
	return out
}

// stripSchemaNoise removes "$schema" and "title" keys recursively.
func stripSchemaNoise(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range []string{"$schema", "title"} {
			delete(t, k)
		}
		for k, val := range t {
			t[k] = stripSchemaNoise(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = stripSchemaNoise(val)
		}
		return t
	default:
		return v
	}
}

// serverEnv merges configured env onto the process environment.
func serverEnv(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// expandCommand expands ${VAR} / $VAR references in a server's command and args
// against the given environment (os.Environ + configured env). exec.Command does
// NOT do shell expansion, so a config like ["node", "${EIGEN_PLUGIN_ROOT}/x.js"]
// would otherwise pass the literal "${EIGEN_PLUGIN_ROOT}" to the process. This
// is how plugin-installed MCP servers locate their bundled files: the installer
// sets EIGEN_PLUGIN_ROOT in the server's env and references it in args.
func expandCommand(command []string, env []string) []string {
	lookup := envLookup(env)
	out := make([]string, len(command))
	for i, c := range command {
		out[i] = os.Expand(c, lookup)
	}
	return out
}

// envLookup builds a ${VAR} resolver from a "K=V" environment slice. An unknown
// var expands to "" (shell semantics).
func envLookup(env []string) func(string) string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return func(k string) string { return m[k] }
}

// sanitize keeps tool names to a provider-safe character set.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// firstSentence returns the first sentence/line of s (for a one-line server
// gist derived from a server's MCP instructions), truncated.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexAny(s, ".!"); i > 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		s = s[:117] + "…"
	}
	return s
}
