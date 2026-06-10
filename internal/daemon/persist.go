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
