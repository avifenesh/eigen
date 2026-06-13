package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// uiPrefs are window-layout preferences that persist across eigen sessions —
// distinct from config.json (which holds defaults for NEW agent sessions).
// These are how the chat window itself looks: side-panel widths the user
// dragged to. Stored at ~/.eigen/ui.json, written atomically, best-effort
// (a missing or malformed file just yields zero — the built-in defaults).
type uiPrefs struct {
	RailW  int `json:"rail_w,omitempty"`  // left rail/sidebar width (0 = default)
	RightW int `json:"right_w,omitempty"` // right panel width (0 = default)
}

func uiPrefsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".eigen", "ui.json")
}

// loadUIPrefs reads ~/.eigen/ui.json. Any error yields zero prefs (defaults).
func loadUIPrefs() uiPrefs {
	path := uiPrefsPath()
	if path == "" {
		return uiPrefs{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return uiPrefs{}
	}
	var p uiPrefs
	if json.Unmarshal(data, &p) != nil {
		return uiPrefs{}
	}
	return p
}

// saveUIPrefs writes ~/.eigen/ui.json atomically (temp + rename). Best-effort:
// errors are ignored — losing a layout pref is never worth interrupting a turn.
func saveUIPrefs(p uiPrefs) {
	path := uiPrefsPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// persistPanelWidths saves the current user-set panel widths. Called after a
// resize settles (drag release or a keyboard resize step).
func (m *model) persistPanelWidths() {
	saveUIPrefs(uiPrefs{RailW: m.railW, RightW: m.rightW})
}
