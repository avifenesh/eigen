// Package obsidian is eigen's native Obsidian integration: read, search, and
// write notes in a local Obsidian vault (a directory of markdown files). A
// vault is just files on disk — no API, no OAuth — so this is a direct-FS
// built-in (like internal/google is a direct-REST built-in), exposed as agent
// tools + a connector status card. It ties into the working-station's
// idea/notes capture: the agent can jot into and recall from the vault.
package obsidian

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// VaultPath resolves the Obsidian vault directory: $EIGEN_OBSIDIAN_VAULT, else
// ~/revuto (the user's vault on this machine), else ~/Obsidian. Returns "" only
// when the home dir is unreadable.
func VaultPath() string {
	if env := strings.TrimSpace(os.Getenv("EIGEN_OBSIDIAN_VAULT")); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, c := range []string{"revuto", "Obsidian", "obsidian"} {
		p := filepath.Join(home, c)
		if isVault(p) {
			return p
		}
	}
	// Default to ~/revuto even if not yet a vault (the user's known location).
	return filepath.Join(home, "revuto")
}

// isVault reports whether dir looks like an Obsidian vault (has a .obsidian dir).
func isVault(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, ".obsidian"))
	return err == nil && fi.IsDir()
}

// Available reports whether the resolved vault exists + is an Obsidian vault.
func Available() bool {
	return isVault(VaultPath())
}

// Note is one markdown note in the vault.
type Note struct {
	Path     string    `json:"path"`     // vault-relative path (the note id)
	Title    string    `json:"title"`    // basename without .md
	Modified time.Time `json:"modified"`
	Size     int64     `json:"size"`
}

// maxVaultScan bounds how many notes we'll walk (a big vault stays responsive).
const maxVaultScan = 5000

// hiddenOrSystem reports vault subpaths to skip: dotfiles (.obsidian/.git) and
// revuto's own machine dirs that aren't user notes.
func hiddenOrSystem(rel string) bool {
	first := rel
	if i := strings.IndexByte(rel, filepath.Separator); i >= 0 {
		first = rel[:i]
	}
	switch first {
	case ".obsidian", ".git", ".locks", ".workspaces", ".trash":
		return true
	}
	return strings.HasPrefix(first, ".")
}

// List returns notes in the vault, newest-modified first, capped at limit.
func List(limit int) ([]Note, error) {
	vault := VaultPath()
	if !isVault(vault) {
		return nil, fmt.Errorf("no Obsidian vault at %s (set EIGEN_OBSIDIAN_VAULT)", vault)
	}
	notes, err := walkNotes(vault, "", nil)
	if err != nil {
		return nil, err
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Modified.After(notes[j].Modified) })
	if limit > 0 && len(notes) > limit {
		notes = notes[:limit]
	}
	return notes, nil
}

// Search returns notes whose title OR content contains query (case-insensitive),
// newest-first, capped. A blank query falls back to List.
func Search(query string, limit int) ([]Note, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return List(limit)
	}
	vault := VaultPath()
	if !isVault(vault) {
		return nil, fmt.Errorf("no Obsidian vault at %s", vault)
	}
	match := func(rel, abs string) bool {
		if strings.Contains(strings.ToLower(rel), q) {
			return true
		}
		// Content search — read the file (notes are small markdown).
		data, err := os.ReadFile(abs)
		return err == nil && strings.Contains(strings.ToLower(string(data)), q)
	}
	notes, err := walkNotes(vault, "", match)
	if err != nil {
		return nil, err
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Modified.After(notes[j].Modified) })
	if limit > 0 && len(notes) > limit {
		notes = notes[:limit]
	}
	return notes, nil
}

// walkNotes walks the vault collecting .md notes, optionally filtered by match
// (rel, abs) → keep. Skips hidden/system dirs; bounded by maxVaultScan.
func walkNotes(vault, _ string, match func(rel, abs string) bool) ([]Note, error) {
	var notes []Note
	scanned := 0
	err := filepath.Walk(vault, func(abs string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		rel, rerr := filepath.Rel(vault, abs)
		if rerr != nil || rel == "." {
			return nil
		}
		if fi.IsDir() {
			if hiddenOrSystem(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(abs), ".md") || hiddenOrSystem(rel) {
			return nil
		}
		scanned++
		if scanned > maxVaultScan {
			return filepath.SkipAll
		}
		if match != nil && !match(rel, abs) {
			return nil
		}
		notes = append(notes, Note{
			Path:     rel,
			Title:    strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)),
			Modified: fi.ModTime(),
			Size:     fi.Size(),
		})
		return nil
	})
	return notes, err
}

// Read returns a note's full markdown content. relPath is vault-relative.
func Read(relPath string) (string, error) {
	abs, err := safeJoin(relPath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Write creates or OVERWRITES a note. relPath is vault-relative; a missing .md
// extension is added. Parent dirs are created. Returns the vault-relative path.
func Write(relPath, content string) (string, error) {
	abs, err := safeJoin(ensureMD(relPath))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return "", err
	}
	vault := VaultPath()
	rel, _ := filepath.Rel(vault, abs)
	return rel, nil
}

// Append adds text to the end of a note (creating it if absent), with a leading
// newline so appended blocks don't run together. Returns the vault-relative path.
func Append(relPath, text string) (string, error) {
	abs, err := safeJoin(ensureMD(relPath))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString("\n" + text + "\n"); err != nil {
		return "", err
	}
	vault := VaultPath()
	rel, _ := filepath.Rel(vault, abs)
	return rel, nil
}

func ensureMD(p string) string {
	if strings.HasSuffix(strings.ToLower(p), ".md") {
		return p
	}
	return p + ".md"
}

// safeJoin resolves a vault-relative path to an absolute one, refusing escapes
// (.. traversal / absolute paths) so a tool call can't read/write outside the
// vault.
func safeJoin(relPath string) (string, error) {
	vault := VaultPath()
	if !isVault(vault) {
		return "", fmt.Errorf("no Obsidian vault at %s", vault)
	}
	p := strings.TrimSpace(relPath)
	if p == "" {
		return "", fmt.Errorf("empty note path")
	}
	// Reject absolute paths and any traversal segment outright (don't silently
	// clamp — a "../x" write should fail, not land in the vault root).
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("note path must be vault-relative, not absolute: %q", relPath)
	}
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return "", fmt.Errorf("note path may not contain ..: %q", relPath)
		}
	}
	abs := filepath.Join(vault, filepath.Clean(p))
	// Final guard: the resolved path must stay inside the vault.
	if rel, err := filepath.Rel(vault, abs); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("note path escapes the vault: %q", relPath)
	}
	return abs, nil
}
