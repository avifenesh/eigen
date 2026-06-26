package connector

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/avifenesh/eigen/internal/mcp"
)

// DefaultPath is the per-user connector store (~/.eigen/connectors.json).
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "connectors.json"
	}
	return filepath.Join(home, ".eigen", "connectors.json")
}

var (
	defaultMu  sync.Mutex
	defaultMgr *Manager
)

// Default returns the process-wide Manager (created on first use), so the GUI
// bridge, CLI, and the MCP auth hook all share one token store + refresh state.
func Default() *Manager {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultMgr == nil {
		defaultMgr = NewManager(DefaultPath())
	}
	return defaultMgr
}

// Install wires the default Manager into mcp.RemoteAuthProvider so remote MCP
// servers ("connectors") authenticate with OAuth-managed bearer tokens that
// refresh transparently. Idempotent; call once at process startup before
// mcp.LoadTools. A server with no connected token falls through to the loader's
// static bearer_token_env_var path.
func Install() {
	m := Default()
	mcp.RemoteAuthProvider = m.AuthHeaderFunc
}
