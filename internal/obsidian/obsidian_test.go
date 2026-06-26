package obsidian

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newVault makes a temp Obsidian vault (a dir with .obsidian) + points
// EIGEN_OBSIDIAN_VAULT at it.
func newVault(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EIGEN_OBSIDIAN_VAULT", dir)
	return dir
}

func TestVaultWriteReadSearchList(t *testing.T) {
	dir := newVault(t)
	if !Available() {
		t.Fatal("temp vault should be Available")
	}

	// Write, then read back.
	rel, err := Write("Inbox/Idea", "# Idea\nwire sxc into eigen")
	if err != nil {
		t.Fatal(err)
	}
	if rel != filepath.Join("Inbox", "Idea.md") {
		t.Fatalf("write path = %q", rel)
	}
	got, err := Read(rel)
	if err != nil || !strings.Contains(got, "wire sxc") {
		t.Fatalf("read: %q %v", got, err)
	}

	// Append.
	if _, err := Append("Inbox/Idea", "extra line"); err != nil {
		t.Fatal(err)
	}
	got, _ = Read(rel)
	if !strings.Contains(got, "extra line") {
		t.Fatal("append did not add text")
	}

	// A second note for search/list.
	if _, err := Write("notes/calendar", "meeting notes about standup"); err != nil {
		t.Fatal(err)
	}

	// Search by content.
	res, err := Search("standup", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Title != "calendar" {
		t.Fatalf("content search: %+v", res)
	}
	// Search by title.
	res, _ = Search("idea", 10)
	if len(res) != 1 || res[0].Title != "Idea" {
		t.Fatalf("title search: %+v", res)
	}
	// List returns both.
	all, _ := List(0)
	if len(all) != 2 {
		t.Fatalf("list should return 2 notes, got %d", len(all))
	}

	// System dirs are skipped.
	os.MkdirAll(filepath.Join(dir, ".obsidian", "plugins"), 0o755)
	os.WriteFile(filepath.Join(dir, ".obsidian", "plugins", "x.md"), []byte("config"), 0o644)
	all, _ = List(0)
	if len(all) != 2 {
		t.Fatalf(".obsidian notes must be skipped, got %d", len(all))
	}
}

func TestSafeJoinRefusesEscape(t *testing.T) {
	newVault(t)
	for _, bad := range []string{"../outside.md", "../../etc/passwd", "/abs/evil.md"} {
		if _, err := Read(bad); err == nil {
			t.Errorf("Read(%q) should be refused", bad)
		}
		if _, err := Write(bad, "x"); err == nil {
			t.Errorf("Write(%q) should be refused", bad)
		}
	}
}

func TestNoVaultErrors(t *testing.T) {
	// Point at a non-vault dir.
	t.Setenv("EIGEN_OBSIDIAN_VAULT", t.TempDir())
	if Available() {
		t.Fatal("a dir without .obsidian is not a vault")
	}
	if _, err := List(0); err == nil {
		t.Error("List on a non-vault should error")
	}
}
