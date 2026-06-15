package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMigrateFlatToTiered(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".eigen", "memory")
	os.MkdirAll(base, 0o755)
	// Pre-v2 flat file for /p (key = eigen-style base-hash).
	flat := filepath.Join(base, key("/p")+".md")
	os.WriteFile(flat, []byte("- old note\n"), 0o644)
	os.WriteFile(flat+".20260101-000000.bak", []byte("- backup\n"), 0o644)

	s, _ := Open("/p")
	if got := s.Read(); !strings.Contains(got, "old note") {
		t.Fatalf("flat file should migrate into MEMORY.md, got %q", got)
	}
	if _, err := os.Stat(s.MemoryPath()); err != nil {
		t.Fatalf("MEMORY.md should exist: %v", err)
	}
	if _, err := os.Stat(flat); err == nil {
		t.Fatal("legacy flat file should be renamed away after migration")
	}
	// legacy backup moved into the scope dir.
	if baks, _ := filepath.Glob(filepath.Join(s.Dir(), "MEMORY.md.*.bak")); len(baks) == 0 {
		t.Fatal("legacy .bak should be moved into the scope dir")
	}
}

func TestInjectsSummaryNotFullMemory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, _ := Open("/p")
	s.Append("a very long working-memory note that should NOT be injected verbatim")
	// No SUMMARY.md yet → injects MEMORY.md (no regression).
	if !strings.Contains(s.Section(), "should NOT be injected verbatim") {
		t.Fatal("without a summary, MEMORY.md is injected")
	}
	// With SUMMARY.md → only the summary is injected.
	os.WriteFile(s.SummaryPath(), []byte("tiny summary"), 0o644)
	sec := s.Section()
	if !strings.Contains(sec, "tiny summary") {
		t.Fatalf("summary should be injected, got %q", sec)
	}
	if strings.Contains(sec, "should NOT be injected verbatim") {
		t.Fatal("full MEMORY.md must NOT be injected once a SUMMARY.md exists")
	}
}

func TestBansInjectedAsConstraints(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, _ := Open("/p")
	os.MkdirAll(s.Dir(), 0o755)
	os.WriteFile(s.BansPath(), []byte("### No hedging\nDo not hedge."), 0o644)
	sec := s.Section()
	if !strings.Contains(sec, "BANNED BEHAVIORS") || !strings.Contains(sec, "No hedging") {
		t.Fatalf("bans should inject as hard constraints, got %q", sec)
	}
	if !strings.Contains(sec, "the rule wins") {
		t.Fatal("bans framing must assert system priority")
	}
}

func TestPathRemainsMemoryFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, _ := Open("/p")
	if filepath.Base(s.Path()) != "MEMORY.md" {
		t.Fatalf("Path() should point at MEMORY.md, got %s", s.Path())
	}
}

func TestWriteAndReadRollouts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	_, err := s.WriteRollout("first-thing", "# First\noutcome: success\n", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	s.WriteRollout("second-thing", "# Second\n", time.Unix(2, 0))
	raws := s.RawSummaries(0)
	if len(raws) != 2 || !strings.Contains(raws[0], "First") || !strings.Contains(raws[1], "Second") {
		t.Fatalf("rollouts should read chronologically, got %d: %v", len(raws), raws)
	}
	// raw is NOT injected.
	if strings.Contains(s.Section(), "First") {
		t.Fatal("raw rollout summaries must NEVER be injected")
	}
	// limit returns most-recent.
	if got := s.RawSummaries(1); len(got) != 1 || !strings.Contains(got[0], "Second") {
		t.Fatalf("limit should return most recent, got %v", got)
	}
}
