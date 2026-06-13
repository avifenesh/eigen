package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Built-in MCP servers: capabilities eigen registers as first-class when their
// binary is present, so the user gets desktop/computer-use control without
// hand-editing mcp.json. A user-configured server of the same name overrides
// the built-in entirely (their command/allowlist wins).

// workspaceTools is the curated allowlist for the agent workspace server — the
// 27 tools that cover the real workflows (lifecycle, tmux terminal, run/launch
// + logs, observe/screenshot, browser control, basic input, cleanup), keeping
// the per-request schema cost ~5k tokens instead of ~17k for all 82.
var workspaceTools = []string{
	"workspace_start", "workspace_stop", "workspace_status", "workspace_list", "workspace_doctor",
	"workspace_run_in_terminal", "workspace_terminal_read", "workspace_terminal_input",
	"workspace_launch_app", "workspace_run_app", "workspace_wait_app", "workspace_kill_app", "workspace_read_app_log",
	"workspace_observe", "workspace_screenshot", "workspace_list_windows", "workspace_events",
	"workspace_open_browser", "workspace_browser_navigate", "workspace_browser_snapshot",
	"workspace_browser_click", "workspace_browser_targets",
	"workspace_click", "workspace_key", "workspace_type_text", "workspace_paste_text",
	"workspace_cleanup_stale",
}

// chromeBridgeTools is the full agent-chrome-bridge tool set — control of the
// user's REAL, already-open, logged-in Chrome (their tabs/sessions), which a
// throwaway Playwright browser can't do. Includes the tab-lock tools (dormant
// unless a multi-agent flow needs them).
var chromeBridgeTools = []string{
	"chrome_health", "chrome_action_log",
	"chrome_lock_tab", "chrome_unlock_tab", "chrome_locks",
	"chrome_tabs", "chrome_active_tab", "chrome_select_tab", "chrome_new_tab", "chrome_close_tab",
	"chrome_reload", "chrome_back", "chrome_forward",
	"chrome_snapshot", "chrome_find", "chrome_extract_links", "chrome_extract_tables",
	"chrome_read_article", "chrome_get_network",
	"chrome_navigate", "chrome_click", "chrome_type", "chrome_scroll",
	"chrome_wait_for_selector", "chrome_wait_for_text", "chrome_wait_until_idle",
	"chrome_screenshot",
	"chrome_cdp_health", "chrome_cdp_click", "chrome_cdp_key", "chrome_cdp_type",
	"chrome_get_console",
}

// withBuiltinServers appends auto-detected built-in servers to the user's list,
// skipping any whose name the user already configured.
func withBuiltinServers(user []serverConfig) []serverConfig {
	have := map[string]bool{}
	for _, s := range user {
		have[s.Name] = true
	}
	if !have["workspace"] {
		if bin := WorkspaceBinary(); bin != "" {
			user = append(user, serverConfig{
				Name:    "workspace",
				Command: []string{bin, "mcp", "--headless"},
				Tools:   workspaceTools,
			})
		}
	}
	// agent-chrome-bridge: drives the user's real logged-in Chrome via an MV3
	// extension + native host. Registered when its MCP server script and a node
	// runtime are both present; a user-configured "chrome" server wins.
	if !have["chrome"] {
		if script, node := ChromeBridge(); script != "" && node != "" {
			user = append(user, serverConfig{
				Name:    "chrome",
				Command: []string{node, script},
				Tools:   chromeBridgeTools,
			})
		}
	}
	return user
}

// ChromeBridge locates the agent-chrome-bridge MCP server script and a node
// runtime to run it. Script: EIGEN_CHROME_BRIDGE (a script path or the repo
// dir), then ~/projects/agent-chrome-bridge/bin/mcp-server.js. Node:
// EIGEN_NODE_BIN, then PATH, then common nvm/local locations (the daemon's
// minimal PATH often lacks an nvm node). Returns ("","") when either is absent
// (the built-in is simply not registered).
func ChromeBridge() (script, node string) {
	script = chromeBridgeScript()
	if script == "" {
		return "", ""
	}
	node = findNode()
	if node == "" {
		return "", ""
	}
	return script, node
}

func chromeBridgeScript() string {
	if p := os.Getenv("EIGEN_CHROME_BRIDGE"); p != "" {
		// Accept either the server script directly or the repo directory.
		if filepath.Base(p) == "mcp-server.js" && isExecutableOrFile(p) {
			return p
		}
		cand := filepath.Join(p, "bin", "mcp-server.js")
		if isExecutableOrFile(cand) {
			return cand
		}
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, "projects", "agent-chrome-bridge", "bin", "mcp-server.js")
		if isExecutableOrFile(p) {
			return p
		}
	}
	return ""
}

// findNode resolves a node runtime, tolerating the daemon's minimal PATH which
// usually misses an nvm install.
func findNode() string {
	if p := os.Getenv("EIGEN_NODE_BIN"); p != "" {
		if isExecutable(p) {
			return p
		}
		return ""
	}
	if p, err := exec.LookPath("node"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/usr/local/bin/node", "/usr/bin/node",
		filepath.Join(home, ".local", "bin", "node"),
	}
	// nvm: pick the newest version dir under ~/.nvm/versions/node.
	if home != "" {
		nvm := filepath.Join(home, ".nvm", "versions", "node")
		if entries, err := os.ReadDir(nvm); err == nil {
			// ReadDir is sorted ascending; iterate reverse for newest-first.
			for i := len(entries) - 1; i >= 0; i-- {
				candidates = append(candidates, filepath.Join(nvm, entries[i].Name(), "bin", "node"))
			}
		}
	}
	for _, p := range candidates {
		if isExecutable(p) {
			return p
		}
	}
	return ""
}

// isExecutableOrFile reports whether p exists as a regular file (scripts need
// not be +x since they're run via the node interpreter).
func isExecutableOrFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// WorkspaceBinary locates the agent-workspace-linux binary: an explicit
// override, then PATH, then the conventional ~/.local/bin install location.
// Returns "" when not found (the built-in is simply not registered).
func WorkspaceBinary() string {
	if p := os.Getenv("EIGEN_WORKSPACE_BIN"); p != "" {
		if isExecutable(p) {
			return p
		}
		return ""
	}
	if p, err := exec.LookPath("agent-workspace-linux"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", "agent-workspace-linux")
		if isExecutable(p) {
			return p
		}
	}
	return ""
}

func isExecutable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}
