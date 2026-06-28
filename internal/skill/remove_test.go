package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveInstalledSkill(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "gone", "desc", "body")
	if err := Remove(dir, "gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone")); !os.IsNotExist(err) {
		t.Fatal("skill dir should be gone")
	}
}

func TestRemoveMissingSkill(t *testing.T) {
	dir := t.TempDir()
	if err := Remove(dir, "nope"); err == nil {
		t.Fatal("expected error for missing skill")
	}
}