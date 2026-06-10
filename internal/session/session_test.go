package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/transcript"
)

// writeClaude writes a minimal Claude Code transcript (one user + one assistant
// line) and returns its path, under a freshly isolated HOME.
func writeClaude(t *testing.T, home, name, userText, asstText string) string {
	t.Helper()
	dir := filepath.Join(home, ".claude", "projects", "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	body := `{"type":"user","message":{"role":"user","content":"` + userText + `"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + asstText + `"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// isolate points $HOME at a temp dir so the store, discovery globs, and ingest
// copies never touch the real ~/.eigen.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestDiscoverFindsClaudeSession(t *testing.T) {
	home := isolate(t)
	writeClaude(t, home, "s.jsonl", "hello world", "hi there")

	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Discover(); err != nil {
		t.Fatal(err)
	}
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 discovered session, got %d", len(list))
	}
	if list[0].Source != transcript.SourceClaude {
		t.Fatalf("expected claude source, got %q", list[0].Source)
	}
	if list[0].Ingested {
		t.Fatal("session should not be ingested before first Load")
	}
}

func TestLoadIngestsOnceAndReusesCopy(t *testing.T) {
	home := isolate(t)
	src := writeClaude(t, home, "s.jsonl", "hello world", "hi there")

	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Discover(); err != nil {
		t.Fatal(err)
	}
	id := s.List()[0].ID

	msgs, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != llm.RoleUser || msgs[0].Text != "hello world" {
		t.Fatalf("first message wrong: %+v", msgs[0])
	}
	if msgs[1].Role != llm.RoleAssistant || msgs[1].Text != "hi there" {
		t.Fatalf("second message wrong: %+v", msgs[1])
	}

	// Ingest copy must exist and metadata must be marked ingested.
	ingest := filepath.Join(home, ".eigen", "store", id+".jsonl")
	if _, err := os.Stat(ingest); err != nil {
		t.Fatalf("ingest copy not created: %v", err)
	}
	m := s.Get(id)
	if !m.Ingested {
		t.Fatal("meta not marked ingested after Load")
	}
	if m.Messages != 2 {
		t.Fatalf("meta message count = %d, want 2", m.Messages)
	}
	if m.Fingerprint == "" {
		t.Fatal("fingerprint not set after ingest")
	}

	// Second Load must read the ingest copy, not the source: deleting the
	// source must not break the load.
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	msgs2, err := s.Load(id)
	if err != nil {
		t.Fatalf("second Load failed (did not reuse ingest copy?): %v", err)
	}
	if len(msgs2) != 2 {
		t.Fatalf("second Load returned %d messages, want 2", len(msgs2))
	}
}

func TestListDedupesIdenticalConversations(t *testing.T) {
	home := isolate(t)
	// Two files, identical content => identical fingerprint after ingest.
	writeClaude(t, home, "a.jsonl", "same opening", "reply")
	writeClaude(t, home, "b.jsonl", "same opening", "reply")

	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Discover(); err != nil {
		t.Fatal(err)
	}
	if got := len(s.List()); got != 2 {
		t.Fatalf("before ingest expected 2 distinct metas, got %d", got)
	}
	// Ingest both so fingerprints are populated.
	for _, m := range s.List() {
		if _, err := s.Load(m.ID); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(s.List()); got != 1 {
		t.Fatalf("after ingest expected dedupe to 1, got %d", got)
	}
}

func TestIndexPersistsAcrossOpen(t *testing.T) {
	home := isolate(t)
	writeClaude(t, home, "s.jsonl", "persist me", "ok")

	s1, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := s1.Discover(); err != nil {
		t.Fatal(err)
	}
	id := s1.List()[0].ID
	if _, err := s1.Load(id); err != nil {
		t.Fatal(err)
	}

	// A fresh Open must read the persisted index and the ingested flag.
	s2, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	m := s2.Get(id)
	if m == nil {
		t.Fatal("meta not persisted across Open")
	}
	if !m.Ingested {
		t.Fatal("ingested flag not persisted across Open")
	}
}

func TestLoadUnknownID(t *testing.T) {
	isolate(t)
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("eig_doesnotexist"); err == nil {
		t.Fatal("expected error loading unknown id")
	}
}

func TestReingestWhenSourceChanges(t *testing.T) {
	home := isolate(t)
	writeClaude(t, home, "s.jsonl", "original", "ok")

	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Discover(); err != nil {
		t.Fatal(err)
	}
	id := s.List()[0].ID
	if _, err := s.Load(id); err != nil {
		t.Fatal(err)
	}

	// Rewrite the source with new content and a bumped mtime, then re-discover.
	path := writeClaude(t, home, "s.jsonl", "rewritten opening", "ok")
	future := time.Unix(0, s.Get(id).OriginMod+int64(time.Second))
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if err := s.Discover(); err != nil {
		t.Fatal(err)
	}
	if s.Get(id).Ingested {
		t.Fatal("changed source should clear Ingested for re-ingest")
	}
	msgs, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if msgs[0].Text != "rewritten opening" {
		t.Fatalf("re-ingest did not pick up new content: %q", msgs[0].Text)
	}
}

func TestDiscoverPeeksTitleAndCount(t *testing.T) {
	home := isolate(t)
	// A claude session with a cwd line so peek can extract the project dir.
	dir := filepath.Join(home, ".claude", "projects", "proj")
	os.MkdirAll(dir, 0o755)
	body := `{"type":"user","cwd":"/home/u/myproj","message":{"role":"user","content":"fix the parser"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}` + "\n"
	os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(body), 0o644)

	s, _ := Open()
	s.Discover()
	m := s.List()[0]
	if m.Title != "fix the parser" {
		t.Errorf("peeked title = %q", m.Title)
	}
	if m.Cwd != "/home/u/myproj" {
		t.Errorf("peeked cwd = %q", m.Cwd)
	}
	if m.Messages != 2 {
		t.Errorf("peeked messages = %d", m.Messages)
	}
	if !m.Peeked {
		t.Error("meta should be marked peeked")
	}

	// Second discover must NOT re-peek (Peeked persisted) — title stays.
	s2, _ := Open()
	s2.Discover()
	if s2.List()[0].Title != "fix the parser" {
		t.Error("peeked data should persist across discovers")
	}
}
