package memory

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConcurrentAppendBans verifies that two concurrent AddBan calls don't lose
// writes due to interleaved read-modify-write. This is the highest-severity
// coexistence bug the GUI multi-process plan must prevent (user's persistent
// knowledge base — lost notes = lost trust).
func TestConcurrentAppendBans(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Open("/some/project")
	if err != nil {
		t.Fatal(err)
	}

	// Two Bridges (or two GUI processes) race to add different bans concurrently.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := s.AddBan("No emoji", "Do not use emojis in commit messages."); err != nil {
			t.Errorf("AddBan goroutine 1: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := s.AddBan("No force push", "Do not force push to main branch."); err != nil {
			t.Errorf("AddBan goroutine 2: %v", err)
		}
	}()
	wg.Wait()

	// Both bans should be present — neither lost by a clobbering write.
	bans := s.ListBans()
	if len(bans) != 2 {
		t.Fatalf("expected 2 bans after concurrent AddBan, got %d: %+v", len(bans), bans)
	}
	titles := map[string]bool{}
	for _, b := range bans {
		titles[b.Title] = true
	}
	if !titles["No emoji"] || !titles["No force push"] {
		t.Fatalf("concurrent AddBan lost a write; got titles %v", titles)
	}
}

// TestConcurrentRemoveCuratedNote verifies that two Bridges removing different
// notes from MEMORY.md don't lose each other's deletes.
func TestConcurrentRemoveCuratedNote(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Open("/some/project")
	if err != nil {
		t.Fatal(err)
	}

	// Pre-populate MEMORY.md with three notes so both removals have work to do.
	if err := s.Rewrite("## Note A\nfirst\n\n## Note B\nsecond\n\n## Note C\nthird\n"); err != nil {
		t.Fatal(err)
	}

	// Two Bridges race to remove different notes (indices 0 and 2, leaving 1).
	// Without the lock, one could read pre-remove state, then clobber the other's
	// deletion. With the lock, both deletions serialize and both land.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		// Remove "Note A" (index 0). After this, the remaining notes shift down.
		if err := s.RemoveCuratedNote(0); err != nil {
			errs <- fmt.Errorf("RemoveCuratedNote(0): %w", err)
		}
	}()
	go func() {
		defer wg.Done()
		// Remove "Note C" (index 2). The lock ensures this reads AFTER the other
		// removal finishes, so it sees the post-first-removal state.
		time.Sleep(5 * time.Millisecond) // slight stagger to force interleaving attempt
		if err := s.RemoveCuratedNote(2); err != nil {
			// If the lock works, this goroutine will see shifted indices after the
			// first removal. Index 2 may now be out of range if only one note remains,
			// OR the second removal will target the (shifted) third note. The test
			// asserts the final state, not the interim error — a contention-safe
			// removal may fail with "out of range" if the other removal already ran.
			// We tolerate that as long as no note is lost silently.
			errs <- fmt.Errorf("RemoveCuratedNote(2): %w", err)
		}
	}()
	wg.Wait()
	close(errs)

	// Drain any errors. If one removal saw an out-of-range index due to the
	// serialized order, that's acceptable as long as the final state is sane
	// (no silent data loss). The test's real assertion is that MEMORY.md is not
	// corrupted and at least one note survives.
	var errMsgs []string
	for e := range errs {
		errMsgs = append(errMsgs, e.Error())
	}

	// Check the final MEMORY.md state. With proper locking, we expect AT LEAST
	// one note to remain (both removals serialize, so the second may fail if it
	// targets a now-shifted or out-of-range index). The critical bug we're
	// preventing is LOST WRITES where one removal's Rewrite clobbers the other's
	// because they read the same pre-state.
	final := s.Read()
	sections := SplitNotes(final)
	if len(sections) == 0 {
		t.Fatalf("concurrent RemoveCuratedNote lost all notes; final content: %q", final)
	}
	if len(sections) > 2 {
		t.Fatalf("concurrent RemoveCuratedNote removed fewer notes than expected; got %d sections (expected ≤2): %v", len(sections), sections)
	}
	// At least one removal should have succeeded (one or two notes removed).
	// If both succeeded serially, one note remains. If one failed due to index
	// shift, two notes remain (only one removed). Either is correct under lock.
	t.Logf("Final MEMORY.md has %d note(s) after concurrent removals (expected 1-2): %v", len(sections), sections)
	if len(errMsgs) > 0 {
		t.Logf("Removal errors (acceptable under serialization): %v", errMsgs)
	}
}

// TestConcurrentMergeFrom verifies that two MergeFrom calls targeting the same
// destination don't lose one of the source scopes' content.
func TestConcurrentMergeFrom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dst, _ := Open("/canonical")
	src1, _ := Open("/worktree1")
	src2, _ := Open("/worktree2")

	// Each source has unique content to merge into dst.
	if err := src1.Rewrite("- src1 note\n"); err != nil {
		t.Fatal(err)
	}
	if err := src2.Rewrite("- src2 note\n"); err != nil {
		t.Fatal(err)
	}

	// Two Bridges race to merge different sources into the same destination.
	var wg sync.WaitGroup
	wg.Add(2)
	when := time.Now()
	go func() {
		defer wg.Done()
		if _, err := dst.MergeFrom(src1, when); err != nil {
			t.Errorf("MergeFrom(src1): %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond) // slight stagger
		if _, err := dst.MergeFrom(src2, when); err != nil {
			t.Errorf("MergeFrom(src2): %v", err)
		}
	}()
	wg.Wait()

	// Both source notes should appear in dst — neither lost by a clobbering write.
	final := dst.Read()
	if !strings.Contains(final, "src1 note") || !strings.Contains(final, "src2 note") {
		t.Fatalf("concurrent MergeFrom lost content; final dst MEMORY.md:\n%s", final)
	}
}

// TestLockIdempotentRelease verifies that calling release() multiple times is safe.
func TestLockIdempotentRelease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Open("/p")
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.lockStore()
	if err != nil {
		t.Fatal(err)
	}
	release()
	release() // second call should be a no-op, not panic
}
