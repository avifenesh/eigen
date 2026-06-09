// Package memory persists durable, per-project notes for eigen across sessions.
// Notes live at ~/.eigen/memory/<project-key>.md and are injected into the
// system prompt at startup so the agent remembers prior learnings; the agent
// appends to them via the memory tool.
package memory

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store is the memory file for one project.
type Store struct {
	path string
}

// Open returns the memory store for projectDir (its absolute path keys the
// file), creating the memory directory. A blank projectDir uses the cwd.
func Open(projectDir string) (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if projectDir == "" {
		projectDir, _ = os.Getwd()
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		abs = projectDir
	}
	dir := filepath.Join(home, ".eigen", "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(dir, key(abs)+".md")}, nil
}

// Path is the memory file path.
func (s *Store) Path() string { return s.path }

// Read returns the current notes (empty string if none).
func (s *Store) Read() string {
	if s == nil {
		return ""
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return ""
	}
	return string(b)
}

// Append adds a timestamped note as a markdown bullet.
func (s *Store) Append(note string) error {
	if s == nil {
		return fmt.Errorf("memory unavailable")
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return fmt.Errorf("note is empty")
	}
	// Collapse newlines so each note stays a single bullet.
	note = strings.Join(strings.Fields(note), " ")
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "- %s — %s\n", time.Now().Format("2006-01-02"), note)
	return err
}

// Section renders the memory for system-prompt injection (empty when no notes).
func (s *Store) Section() string {
	notes := strings.TrimSpace(s.Read())
	if notes == "" {
		return ""
	}
	return "Project memory (durable notes from past sessions in this project):\n" + notes
}

// key derives a readable, unique filename component from a project path.
func key(abs string) string {
	h := sha1.Sum([]byte(abs))
	base := filepath.Base(abs)
	if base == "" || base == "/" || base == "." {
		base = "root"
	}
	return base + "-" + hex.EncodeToString(h[:])[:8]
}
