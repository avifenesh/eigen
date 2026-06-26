package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetireAdHocNotes_MovesToRetired(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := OpenByKey("proj-retire01")
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if err := s.AddAdHocNote("fold me into memory", when); err != nil {
		t.Fatal(err)
	}
	_, paths := s.adHocNotesWithPaths(0)
	if len(paths) != 1 {
		t.Fatalf("expected 1 ad-hoc note, got %d", len(paths))
	}

	n := s.RetireAdHocNotes(paths)
	if n != 1 {
		t.Fatalf("retired %d, want 1", n)
	}
	// Gone from live notes, present in retired/.
	if live, _ := s.adHocNotesWithPaths(0); len(live) != 0 {
		t.Fatalf("note still live after retire: %d", len(live))
	}
	retired, _ := filepath.Glob(filepath.Join(s.RetiredAdHocDir(), "*.md"))
	if len(retired) != 1 {
		t.Fatalf("expected 1 retired note, got %d", len(retired))
	}
}

func TestPruneEvidence_ArchivesByCountAndAge(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := OpenByKey("proj-forget01")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	// Write keepRecentRollouts + 5 rollout summaries; the 5 oldest should archive.
	total := keepRecentRollouts + 5
	for i := 0; i < total; i++ {
		when := now.Add(-time.Duration(total-i) * time.Hour) // older first
		slug := fmt.Sprintf("rollout-%03d", i)
		if _, err := s.WriteRollout(slug, "# rollout\n- detail\n", when); err != nil {
			t.Fatal(err)
		}
	}
	// A retired ad-hoc note older than retiredMaxAge → should archive.
	if err := os.MkdirAll(s.RetiredAdHocDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	oldNote := filepath.Join(s.RetiredAdHocDir(), "old.md")
	if err := os.WriteFile(oldNote, []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ancient := now.Add(-retiredMaxAge - 24*time.Hour)
	if err := os.Chtimes(oldNote, ancient, ancient); err != nil {
		t.Fatal(err)
	}

	res := s.PruneEvidence(now)
	if res.RolloutsArchived != 5 {
		t.Fatalf("rollouts archived = %d, want 5", res.RolloutsArchived)
	}
	if res.RetiredArchived != 1 {
		t.Fatalf("retired archived = %d, want 1", res.RetiredArchived)
	}
	// Live rollouts back to the cap; archived ones recoverable under archive/.
	live, _ := filepath.Glob(filepath.Join(s.RawDir(), "*.md"))
	if len(live) != keepRecentRollouts {
		t.Fatalf("live rollouts = %d, want %d", len(live), keepRecentRollouts)
	}
	arch, _ := filepath.Glob(filepath.Join(s.RawDir(), "archive", "*.md"))
	if len(arch) != 5 {
		t.Fatalf("archived rollouts on disk = %d, want 5", len(arch))
	}
}

// TestMaybeConsolidate_RetiresFoldedAdHoc is the end-to-end decay guarantee: a
// consolidation run retires the ad-hoc notes it folded, so they don't re-feed
// every future Phase 2.
func TestMaybeConsolidate_RetiresFoldedAdHoc(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := OpenByKey("proj-fold01")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Rewrite("# Mem\n- existing fact\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAdHocNote("a durable fact to absorb into memory", time.Now()); err != nil {
		t.Fatal(err)
	}
	if live, _ := s.adHocNotesWithPaths(0); len(live) != 1 {
		t.Fatalf("setup: expected 1 live ad-hoc note, got %d", len(live))
	}

	p := &Pipeline{
		Store: s,
		// Consolidate echoes the input back unchanged (structured + same size, so
		// the shrink/empty guards pass) — enough to exercise the fold+retire path.
		Consolidate: func(_ context.Context, current string) (string, error) {
			return current, nil
		},
	}
	did, err := p.MaybeConsolidate(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !did {
		t.Fatal("expected consolidation to run")
	}
	// The folded ad-hoc note must now be retired (not live, recoverable in retired/).
	if live, _ := s.adHocNotesWithPaths(0); len(live) != 0 {
		t.Fatalf("ad-hoc note still live after consolidation: %d (should be retired)", len(live))
	}
	retired, _ := filepath.Glob(filepath.Join(s.RetiredAdHocDir(), "*.md"))
	if len(retired) != 1 {
		t.Fatalf("expected 1 retired note, got %d", len(retired))
	}
}
