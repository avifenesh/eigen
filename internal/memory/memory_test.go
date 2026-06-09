package memory

import (
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
