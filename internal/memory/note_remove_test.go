package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoveCuratedNote(t *testing.T) {
	s := testStore(t)
	if err := s.Rewrite("## One\n\n- a\n\n## Two\n\n- b\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCuratedNote(0); err != nil {
		t.Fatal(err)
	}
	got := s.Read()
	if strings.Contains(got, "One") || strings.Contains(got, "- a") {
		t.Fatalf("first section should be removed, got:\n%s", got)
	}
	if !strings.Contains(got, "Two") {
		t.Fatalf("second section should remain, got:\n%s", got)
	}
}

func TestRemoveAdHocNote(t *testing.T) {
	s := testStore(t)
	if err := s.AddAdHocNote("first fact", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAdHocNote("second fact", time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveAdHocNote(0); err != nil {
		t.Fatal(err)
	}
	notes := s.AdHocNotes(0)
	if len(notes) != 1 || !strings.Contains(notes[0], "second") {
		t.Fatalf("want one ad-hoc note with second, got %v", notes)
	}
}

func testStore(t *testing.T) *Store {
	t.Helper()
	base := t.TempDir()
	s := &Store{dir: filepath.Join(base, "proj-test")}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return s
}
