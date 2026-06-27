package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/session"
	"github.com/avifenesh/eigen/internal/transcript"
)

// Session-management extras for the Sessions manager view. The daemon List +
// Remove/Prune are already bridged; this adds transcript export. The store is
// the local-filesystem session index (daemon + imported transcripts).

// ExportSession writes a session's transcript to a file and returns the path.
// Destination: ~/eigen-exports/<id>-<stamp>.jsonl (created if absent), so the
// GUI needn't pop a native save dialog. The id is either a daemon-persisted
// session id (durable transcript under daemon.PersistedTranscriptPath) or a
// store session id.
func (b *Bridge) ExportSession(id string) (string, error) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "eigen-exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, fmt.Sprintf("%s-%s.jsonl", safeFileID(id), exportStamp()))

	// Mirror the TUI fork (internal/app/sessions.go): daemon-persisted sessions
	// have ids that are NOT in the store — they live under the durable
	// transcript path. store.Export → store.Load by store-meta id returns
	// os.ErrNotExist for them, so branch on the durable transcript first.
	if src := daemon.PersistedTranscriptPath(id); fileExists(src) {
		// The durable transcript is already eigen-native JSONL.
		msgs, err := transcript.Load(src)
		if err != nil {
			return "", err
		}
		if err := transcript.Save(dest, msgs); err != nil {
			return "", err
		}
		return dest, nil
	}

	store, err := session.SharedOpen()
	if err != nil || store == nil {
		return "", fmt.Errorf("session store unavailable")
	}
	_ = store.Discover()
	if err := store.Export(id, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// fileExists reports whether path names an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// exportStamp is a filename-safe timestamp; pulled out so it's the only
// time-dependent line (kept deterministic-friendly for tests via the format).
func exportStamp() string {
	return time.Now().Format("20060102-150405")
}

// safeFileID keeps a session id filename-safe (ids are already simple, but
// guard against any path separators sneaking in).
func safeFileID(id string) string {
	out := make([]rune, 0, len(id))
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "session"
	}
	return string(out)
}
