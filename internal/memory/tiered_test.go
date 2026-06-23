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
	s.Rewrite("a very long working-memory note that should NOT be injected verbatim")
	// No memory_summary.md yet → injects MEMORY.md (no regression).
	if !strings.Contains(s.Section(), "should NOT be injected verbatim") {
		t.Fatal("without a summary, MEMORY.md is injected")
	}
	// With memory_summary.md → only the summary is injected.
	os.WriteFile(s.SummaryPath(), []byte("tiny summary"), 0o644)
	sec := s.Section()
	if !strings.Contains(sec, "tiny summary") {
		t.Fatalf("summary should be injected, got %q", sec)
	}
	if strings.Contains(sec, "should NOT be injected verbatim") {
		t.Fatal("full MEMORY.md must NOT be injected once a memory_summary.md exists")
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

func TestBansAddUpdateRemoveList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	if r, _ := s.AddBan("No hedging", "Do not start with 'I think'."); r {
		t.Fatal("first add is not a replace")
	}
	if r, _ := s.AddBan("No call it a day", "Do not suggest stopping."); r {
		t.Fatal("second distinct add is not a replace")
	}
	// update by title (case-insensitive).
	r, _ := s.AddBan("no hedging", "Updated rule.")
	if !r {
		t.Fatal("same-title add should replace")
	}
	bans := s.ListBans()
	if len(bans) != 2 {
		t.Fatalf("want 2 bans, got %d: %+v", len(bans), bans)
	}
	// the updated one carries the new rule.
	var found bool
	for _, b := range bans {
		if b.Title == "No hedging" && b.Rule == "Updated rule." {
			found = true
		}
	}
	if !found {
		t.Fatalf("update should change the rule, got %+v", bans)
	}
	// injected as hard constraints.
	if !strings.Contains(s.Section(), "BANNED BEHAVIORS") || !strings.Contains(s.Section(), "Updated rule.") {
		t.Fatal("bans should inject")
	}
	// remove.
	ok, _ := s.RemoveBan("No hedging")
	if !ok || len(s.ListBans()) != 1 {
		t.Fatalf("remove should drop one, got %d", len(s.ListBans()))
	}
	// removing all clears bans.md.
	s.RemoveBan("No call it a day")
	if strings.Contains(s.Section(), "BANNED BEHAVIORS") {
		t.Fatal("no bans → no banned-behaviors section")
	}
}

func TestAddBanRedactsTitle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	// A secret token in the title must be scrubbed too — bans.md is injected as
	// system-priority context, so an unredacted token would persist in prompts.
	if _, err := s.AddBan("Never reuse ghp_0123456789abcdefghij token", "Do not reuse it."); err != nil {
		t.Fatalf("AddBan: %v", err)
	}
	bans := s.ListBans()
	if len(bans) != 1 {
		t.Fatalf("want 1 ban, got %d: %+v", len(bans), bans)
	}
	if strings.Contains(bans[0].Title, "ghp_0123456789abcdefghij") {
		t.Fatalf("title should be redacted, got %q", bans[0].Title)
	}
	if !strings.Contains(bans[0].Title, Redacted) {
		t.Fatalf("title should contain redaction placeholder, got %q", bans[0].Title)
	}
	// And it must not leak through the injected section either.
	if strings.Contains(s.Section(), "ghp_0123456789abcdefghij") {
		t.Fatal("redacted token leaked into injected section")
	}
}
