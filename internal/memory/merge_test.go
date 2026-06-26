package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMergeFrom_FoldsAndArchives(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	when := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	dst, err := OpenByKey("proj-aaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if err := dst.Rewrite("# Dest\n- dest fact\n"); err != nil {
		t.Fatal(err)
	}
	src, err := OpenByKey("proj-bbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	if err := src.Rewrite("# Src\n- src fact\n"); err != nil {
		t.Fatal(err)
	}
	if err := src.AddAdHocNote("an ad-hoc fact from the orphan", when); err != nil {
		t.Fatal(err)
	}

	res, err := dst.MergeFrom(src, when)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected a merge result")
	}
	if res.AdHocCopied != 1 {
		t.Fatalf("ad-hoc copied = %d, want 1", res.AdHocCopied)
	}
	if res.MemoryBytes == 0 {
		t.Fatal("expected source memory folded in")
	}

	// Destination MEMORY.md now contains BOTH facts + a provenance header.
	merged := dst.Read()
	for _, want := range []string{"dest fact", "src fact", "Merged from scope proj-bbbbbbbb"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged MEMORY.md missing %q:\n%s", want, merged)
		}
	}
	// Source scope archived (renamed), not deleted, not live.
	if res.ArchivedAs == "" {
		t.Fatal("source should have been archived")
	}
	if _, err := os.Stat(src.Dir()); !os.IsNotExist(err) {
		t.Fatal("source scope dir should be gone (renamed to archive)")
	}
	if _, err := os.Stat(res.ArchivedAs); err != nil {
		t.Fatalf("archive dir missing: %v", err)
	}
	// The orphan's ad-hoc note landed in the destination's notes dir.
	notes, _ := filepath.Glob(filepath.Join(dst.AdHocNotesDir(), "merged-proj-bbbbbbbb-*.md"))
	if len(notes) != 1 {
		t.Fatalf("expected 1 merged ad-hoc note, got %d", len(notes))
	}
}

func TestMergeFrom_SameScopeNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := OpenByKey("proj-cccccccc")
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.MergeFrom(s, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res != nil {
		t.Fatalf("same-scope merge should be a no-op, got %+v", res)
	}
}

// TestReconcileScopes_MergesLegacyIntoCanonical simulates the live bug: a repo
// dir whose legacy cwd key already has a scope on disk, distinct from its
// canonical git-root key. Reconcile should fold the legacy scope into canonical.
func TestReconcileScopes_MergesFragment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	when := time.Date(2026, 6, 26, 9, 0, 0, 0, time.UTC)

	repo := t.TempDir()
	gitInit(t, repo)
	sub := filepath.Join(repo, "internal", "gui")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pre-create the LEGACY (subdir-cwd) scope with content, as if an older
	// build had keyed memory off the subdir.
	legacyKey := key(mustAbs(t, sub))
	legacy, err := OpenByKey(legacyKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Rewrite("# Legacy subdir scope\n- gui detail\n"); err != nil {
		t.Fatal(err)
	}

	canonicalKey := key(canonicalProjectDir(repo))
	if legacyKey == canonicalKey {
		t.Skip("subdir resolved to same key (git unavailable) — nothing to reconcile")
	}

	merges, err := ReconcileScopes([]string{sub}, when)
	if err != nil {
		t.Fatal(err)
	}
	if len(merges) != 1 {
		t.Fatalf("expected 1 merge, got %d", len(merges))
	}
	canonical, _ := OpenByKey(canonicalKey)
	if !strings.Contains(canonical.Read(), "gui detail") {
		t.Fatalf("canonical scope missing folded content:\n%s", canonical.Read())
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
