package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	Dir string `json:"dir,omitempty"`
	// Title is a user-set session name (/rename); empty = derived from the
	// first user message.
	Title    string `json:"title,omitempty"`
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

// SaveMeta writes the session meta sidecar next to the given session file. The
// write is atomic (temp file + rename) so a crash or force-exit mid-write — which
// happens every turn — never leaves a half-written .meta.json that LoadMeta would
// reject, silently resetting provider/model/perm/effort/search/goal/loop to
// defaults on resume. A write failure is returned but is non-fatal to the caller
// (meta is best-effort).
func SaveMeta(sessionPath string, m SessionMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := metaPath(sessionPath)
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	// Best-effort directory fsync makes the rename durable across sudden power loss.
	_ = syncDir(filepath.Dir(path))
	return nil
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
