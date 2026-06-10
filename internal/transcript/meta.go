package transcript

import (
	"encoding/json"
	"os"
)

// SessionMeta records the live session config alongside an eigen-native
// transcript, so resuming (via --resume/--continue or a /rebuild exec) can
// continue exactly as the conversation was — same provider, model, permission
// posture, reasoning effort, and live-search mode — rather than resetting to
// launch defaults. Every field is optional; empty fields are simply not
// restored.
type SessionMeta struct {
	// Dir is the working directory (project root) the session ran in — the
	// grouping key for the app's Projects page.
	Dir      string `json:"dir,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Perm     string `json:"perm,omitempty"`
	Effort   string `json:"effort,omitempty"`
	Search   string `json:"search,omitempty"`
	Goal     string `json:"goal,omitempty"`

	// Loop: a prompt re-submitted on an interval while idle (until cleared).
	LoopPrompt string `json:"loop_prompt,omitempty"`
	LoopEvery  string `json:"loop_every,omitempty"` // time.Duration string
}

// metaPath returns the sidecar meta path for a session JSONL file.
func metaPath(sessionPath string) string { return sessionPath + ".meta.json" }

// SaveMeta writes the session meta sidecar next to the given session file. A
// write failure is returned but is non-fatal to the caller (meta is best-effort).
func SaveMeta(sessionPath string, m SessionMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath(sessionPath), b, 0o644)
}

// LoadMeta reads the session meta sidecar for a session file. The second return
// is false when no (readable, valid) sidecar exists.
func LoadMeta(sessionPath string) (SessionMeta, bool) {
	data, err := os.ReadFile(metaPath(sessionPath))
	if err != nil {
		return SessionMeta{}, false
	}
	var m SessionMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return SessionMeta{}, false
	}
	return m, true
}
