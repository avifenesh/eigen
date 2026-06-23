package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/avifenesh/eigen/internal/session"
)

// Session-management extras for the Sessions manager view. The daemon List +
// Remove/Prune are already bridged; this adds transcript export. The store is
// the local-filesystem session index (daemon + imported transcripts).

// ExportSession writes a session's transcript to a file and returns the path.
// Destination: ~/eigen-exports/<id>-<stamp>.jsonl (created if absent), so the
// GUI needn't pop a native save dialog. The id is the store session id.
func (b *Bridge) ExportSession(id string) (string, error) {
	store, err := session.Open()
	if err != nil || store == nil {
		return "", fmt.Errorf("session store unavailable")
	}
	_ = store.Discover()
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "eigen-exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, fmt.Sprintf("%s-%s.jsonl", safeFileID(id), exportStamp()))
	if err := store.Export(id, dest); err != nil {
		return "", err
	}
	return dest, nil
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
