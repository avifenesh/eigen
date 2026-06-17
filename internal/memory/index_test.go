package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexJobQueue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	idx, err := OpenIndex()
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	if err := idx.Enqueue("mem_stage1", "scopeA", "s1"); err != nil {
		t.Fatal(err)
	}
	// Dedup: re-enqueue same key stays one row.
	_ = idx.Enqueue("mem_stage1", "scopeA", "s1")

	j, ok, err := idx.Claim(60)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if j.JobKey != "s1" || j.Status != "pending" {
		t.Fatalf("claimed wrong job: %+v", j)
	}

	// While leased, a second claim finds nothing (lease not expired).
	if _, ok2, _ := idx.Claim(60); ok2 {
		t.Fatal("a leased job must not be re-claimable")
	}

	// Finish ok → done, not re-claimable.
	if err := idx.Finish(j, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok3, _ := idx.Claim(60); ok3 {
		t.Fatal("a done job must not be claimable")
	}
}

func TestIndexClaimScopeDoesNotStealOtherScopes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	idx, err := OpenIndex()
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	if err := idx.Enqueue(JobSummary, "scopeA", "scope"); err != nil {
		t.Fatal(err)
	}
	if err := idx.Enqueue(JobSummary, "scopeB", "scope"); err != nil {
		t.Fatal(err)
	}
	j, ok, err := idx.ClaimScope("scopeB", 60)
	if err != nil || !ok {
		t.Fatalf("claim scopeB: ok=%v err=%v", ok, err)
	}
	if j.Scope != "scopeB" {
		t.Fatalf("ClaimScope should claim only scopeB, got %+v", j)
	}
	if err := idx.Finish(j, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := idx.ClaimScope("scopeB", 60); err != nil || ok {
		t.Fatalf("scopeB should now be drained: ok=%v err=%v", ok, err)
	}
	j, ok, err = idx.ClaimScope("scopeA", 60)
	if err != nil || !ok || j.Scope != "scopeA" {
		t.Fatalf("scopeA job should remain claimable, got %+v ok=%v err=%v", j, ok, err)
	}
}

func TestIndexJobRetryThenError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	idx, _ := OpenIndex()
	defer idx.Close()
	idx.Enqueue("mem_consolidate", "x", "k")
	// retry_remaining starts at 2 → fail 3 times → error.
	for n := 0; n < 3; n++ {
		j, ok, _ := idx.Claim(0) // lease 0 so it's immediately reclaimable
		if !ok {
			t.Fatalf("claim %d should succeed", n)
		}
		idx.Finish(j, fmt.Errorf("boom %d", n))
	}
	if _, ok, _ := idx.Claim(0); ok {
		t.Fatal("after exhausting retries the job should be 'error', not reclaimable")
	}
}

func TestIndexSummaryIdempotencyAndUsage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	idx, _ := OpenIndex()
	defer idx.Close()
	r := SummaryRow{Scope: "p", SessionID: "s1", Slug: "fix-bug", RawPath: "raw/x.md", Outcome: "success", Watermark: 1234, GeneratedAt: 1}
	if err := idx.RecordSummary(r); err != nil {
		t.Fatal(err)
	}
	if !idx.Summarized("p", "s1", 1234) {
		t.Fatal("should be summarized at watermark 1234")
	}
	if idx.Summarized("p", "s1", 9999) {
		t.Fatal("changed watermark must NOT count as summarized")
	}

	idx.BumpUsage("p", "s1", "s1") // used twice
	rows, _ := idx.Summaries("p")
	if len(rows) != 1 || rows[0].UsageCount != 2 {
		t.Fatalf("usage should be 2, got %+v", rows)
	}
	// Re-record (re-summarize) preserves usage_count.
	r.Watermark = 5678
	r.GeneratedAt = 2
	idx.RecordSummary(r)
	rows, _ = idx.Summaries("p")
	if rows[0].UsageCount != 2 {
		t.Fatalf("re-record must preserve usage, got %d", rows[0].UsageCount)
	}
	if rows[0].Watermark != 5678 {
		t.Fatal("watermark should update on re-record")
	}
}

func TestCommitMemoryGitVersioning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".eigen", "memory")
	os.MkdirAll(base, 0o755)
	os.WriteFile(filepath.Join(base, "global", "MEMORY.md"), []byte("- note\n"), 0o644)
	os.MkdirAll(filepath.Join(base, "global"), 0o755)
	os.WriteFile(filepath.Join(base, "global", "MEMORY.md"), []byte("- note\n"), 0o644)
	CommitMemory("test commit")
	if _, err := os.Stat(filepath.Join(base, ".git")); err != nil {
		t.Skip("git not available in this environment")
	}
	// index.sqlite must be gitignored.
	gi, _ := os.ReadFile(filepath.Join(base, ".gitignore"))
	if !filepath.IsAbs("/") || len(gi) == 0 || string(gi[:11]) != "index.sqlit" {
		t.Fatalf(".gitignore should exclude the sqlite index, got %q", gi)
	}
}
