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
	return user
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
