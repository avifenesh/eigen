package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/transcript"
)

// Persistence: a daemon session is durable. The conversation streams to
// ~/.eigen/daemon/sessions/<id>.jsonl after every message (the agent's
// Persist hook — the same continuous-autosave mechanism the local chat uses),
// with a sidecar <id>.meta.json recording what the daemon needs to resurrect
// the session on restart: its dir, model, perm, goal, title. Killing the
// daemon therefore loses nothing: the next start rebuilds each session's
// agent (per dir, via the injected Builder) and resumes its history under the
// SAME id, so views reattach exactly where they were.

// SessionsDir is where daemon session transcripts live.
func SessionsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".eigen", "daemon", "sessions")
}

// persistMeta is the sidecar state for resurrecting a session.
type persistMeta struct {
	ID    string `json:"id"`
	Dir   string `json:"dir"`
	Model string `json:"model"`
	Title string `json:"title,omitempty"`
	Perm  string `json:"perm,omitempty"`
	Goal  string `json:"goal,omitempty"`
	// LastAttached is when a view last attached (unix seconds). "Last used by
	// ME" — transcript mtime lies (the titler and background persistence touch
	// files), so listings sort by this when present.
	LastAttached int64 `json:"last_attached,omitempty"`
}

func transcriptPath(dir, id string) string { return filepath.Join(dir, id+".jsonl") }
func metaPath(dir, id string) string       { return filepath.Join(dir, id+".meta.json") }

// saveMeta writes the sidecar (best-effort; persistence must not break turns).
func saveMeta(dir string, m persistMeta) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(metaPath(dir, m.ID), b, 0o644)
}

// loadPersisted scans the sessions dir and returns every resurrectable
// session (meta + history), oldest id first so re-Add keeps stable ordering.
func loadPersisted(dir string) []persisted {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []persisted
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".meta.json") {
			continue
		}
		var m persistMeta
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || json.Unmarshal(b, &m) != nil || m.ID == "" {
			continue
		}
		msgs, _ := transcript.Load(transcriptPath(dir, m.ID))
		out = append(out, persisted{meta: m, history: msgs})
	}
	// Order by numeric id (s1, s2, …) so seq continues correctly.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if idNum(out[j].meta.ID) < idNum(out[i].meta.ID) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

type persisted struct {
	meta    persistMeta
	history []llm.Message
}

// idNum extracts the numeric part of a session id ("s12" → 12).
func idNum(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "s"))
	return n
}

// removePersisted deletes a session's durable files.
func removePersisted(dir, id string) {
	_ = os.Remove(transcriptPath(dir, id))
	_ = os.Remove(metaPath(dir, id))
}

// PersistedInfo is a durable session as listed from disk (works whether or
// not the daemon is running — the files are the source of truth).
type PersistedInfo struct {
	ID      string
	Dir     string
	Model   string
	Title   string
	Msgs    int
	Updated int64 // unix seconds: last attach when known, else transcript mtime
}

// ListPersisted lists durable daemon sessions from the default sessions dir.
// Title falls back to a snippet of the first user message when meta has none.
func ListPersisted() []PersistedInfo {
	dir := SessionsDir()
	var out []PersistedInfo
	for _, p := range loadPersisted(dir) {
		info := PersistedInfo{ID: p.meta.ID, Dir: p.meta.Dir, Model: p.meta.Model, Title: p.meta.Title, Msgs: len(p.history)}
		// "Last used by ME": prefer the attach timestamp over transcript mtime
		// (the titler/persistence touch files without the user being there).
		if p.meta.LastAttached > 0 {
			info.Updated = p.meta.LastAttached
		} else if fi, err := os.Stat(transcriptPath(dir, p.meta.ID)); err == nil {
			info.Updated = fi.ModTime().Unix()
		}
		if info.Title == "" {
			for _, m := range p.history {
				if m.Role == llm.RoleUser && strings.TrimSpace(m.Text) != "" {
					info.Title = snippet(m.Text, 64)
					break
				}
			}
		}
		out = append(out, info)
	}
	return out
}

// snippet returns the first line of s, truncated to n runes.
func snippet(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// DeletePersisted removes a durable daemon session's files from the default
// dir (used when no daemon is running; with one running, use the remove op).
func DeletePersisted(id string) { removePersisted(SessionsDir(), id) }

// PersistedTranscriptPath returns the durable transcript path for a session id.
func PersistedTranscriptPath(id string) string { return transcriptPath(SessionsDir(), id) }
