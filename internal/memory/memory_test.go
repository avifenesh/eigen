package memory

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestAppendAndRead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Open("/some/project")
	if err != nil {
		t.Fatal(err)
	}
	if s.Read() != "" {
		t.Fatal("fresh memory should be empty")
	}
	if err := s.Append("use go test ./... to run tests"); err != nil {
		t.Fatal(err)
	}
	if err := s.Append("the build entrypoint is main.go"); err != nil {
		t.Fatal(err)
	}
	got := s.Read()
	if !strings.Contains(got, "go test ./...") || !strings.Contains(got, "main.go") {
		t.Fatalf("notes not persisted:\n%s", got)
	}
	if strings.Count(got, "\n- ") != 1 && !strings.HasPrefix(got, "- ") {
		t.Fatalf("each note should be its own bullet:\n%s", got)
	}
}

func TestSectionEmptyWhenNoNotes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	if s.Section() != "" {
		t.Fatal("no notes should yield an empty section")
	}
	_ = s.Append("a note")
	if !strings.Contains(s.Section(), "a note") {
		t.Fatal("section should include the note")
	}
}

func TestSeparateProjectsSeparateFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, _ := Open("/project/a")
	b, _ := Open("/project/b")
	if a.Path() == b.Path() {
		t.Fatal("different projects must use different memory files")
	}
	_ = a.Append("only in a")
	if strings.Contains(b.Read(), "only in a") {
		t.Fatal("project b should not see project a's notes")
	}
}

func TestAppendCollapsesNewlines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	_ = s.Append("line one\nline two")
	got := s.Read()
	if strings.Count(strings.TrimSpace(got), "\n") != 0 {
		t.Fatalf("a multiline note should collapse to one bullet:\n%s", got)
	}
}

func TestEmptyNoteRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	if err := s.Append("   "); err == nil {
		t.Fatal("blank note should error")
	}
}

func TestNilStoreSafe(t *testing.T) {
	var s *Store
	if s.Read() != "" || s.Section() != "" {
		t.Fatal("nil store reads should be empty")
	}
	if err := s.Append("x"); err == nil {
		t.Fatal("nil store append should error, not panic")
	}
}

func TestSnapshotAndRewrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")

	// Snapshot of a missing file is a no-op.
	if bak, err := s.Snapshot(); err != nil || bak != "" {
		t.Fatalf("snapshot of missing file: bak=%q err=%v", bak, err)
	}

	if err := s.Append("first note"); err != nil {
		t.Fatal(err)
	}
	before := s.Read()

	// Rewrite snapshots the old content, then replaces it.
	if err := s.Rewrite("- 2026-01-01 — consolidated\n"); err != nil {
		t.Fatal(err)
	}
	if got := s.Read(); !strings.Contains(got, "consolidated") {
		t.Fatalf("rewrite did not apply: %q", got)
	}
	baks := s.Backups()
	if len(baks) != 1 {
		t.Fatalf("want 1 backup, got %d", len(baks))
	}
	bak, _ := os.ReadFile(baks[0])
	if string(bak) != before {
		t.Fatalf("backup should hold the pre-rewrite content")
	}
}

func TestBackupPruning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	_ = s.Append("note")
	// Create more than maxBackups snapshots with distinct names.
	for i := 0; i < maxBackups+3; i++ {
		bak := fmt.Sprintf("%s.2026010%d-00000%d.bak", s.Path(), i%9, i)
		if err := os.WriteFile(bak, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s.pruneBackups()
	if got := len(s.Backups()); got > maxBackups {
		t.Fatalf("pruning should cap backups at %d, got %d", maxBackups, got)
	}
}

func TestAppendRedactsSecrets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	if err := s.Append("the key is AKIA_REDACTED_EXAMPLE and api_key=redacted-example-secret works"); err != nil {
		t.Fatal(err)
	}
	got := s.Read()
	if strings.Contains(got, "AKIA_REDACTED_EXAMPLE") || strings.Contains(got, "redacted-example-secret") {
		t.Fatalf("secrets must be redacted, got %q", got)
	}
	if !strings.Contains(got, Redacted) {
		t.Fatalf("redaction placeholder missing: %q", got)
	}
	if !strings.Contains(got, "api_key=") {
		t.Fatalf("key name should be preserved: %q", got)
	}
}

func TestSectionStalenessFraming(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	_ = s.Append("a fact")
	sec := s.Section()
	if !strings.Contains(sec, "may be stale") || !strings.Contains(sec, "not instructions") {
		t.Fatalf("section should frame notes as possibly stale data: %q", sec)
	}
}

func TestGlobalStoreSeparateFromProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	proj, _ := Open("/some/project")
	glob, _ := OpenGlobal()
	if !glob.IsGlobal() || proj.IsGlobal() {
		t.Fatal("IsGlobal should distinguish the stores")
	}
	if proj.Path() == glob.Path() {
		t.Fatal("global and project stores must be different files")
	}
	_ = proj.Append("project fact")
	_ = glob.Append("global rule")
	if strings.Contains(proj.Read(), "global rule") || strings.Contains(glob.Read(), "project fact") {
		t.Fatal("global and project notes must not bleed into each other")
	}
}

func TestGlobalSectionLabel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	glob, _ := OpenGlobal()
	_ = glob.Append("user commits often")
	sec := glob.Section()
	if !strings.Contains(sec, "Global memory") || !strings.Contains(sec, "cross-project") {
		t.Fatalf("global section should be labeled as cross-project: %q", sec)
	}
}

func TestSectionsCombinesGlobalThenProject(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	proj, _ := Open("/p")
	glob, _ := OpenGlobal()
	_ = proj.Append("PROJECTNOTE")
	_ = glob.Append("GLOBALNOTE")
	combined := Sections(glob, proj)
	gi := strings.Index(combined, "GLOBALNOTE")
	pi := strings.Index(combined, "PROJECTNOTE")
	if gi < 0 || pi < 0 || gi > pi {
		t.Fatalf("Sections should place global before project: %q", combined)
	}
	// Empty stores contribute nothing.
	if Sections(nil, nil) != "" {
		t.Fatal("Sections of nil stores should be empty")
	}
}
