package memory

import (
	"context"
	"strings"
	"testing"
)

func fakePipe(t *testing.T) *Pipeline {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s, _ := Open("/p")
	idx, _ := OpenIndex()
	t.Cleanup(func() { idx.Close() })
	return &Pipeline{
		Store: s, Index: idx, ConsolidateBytes: 50,
		Stage1: func(_ context.Context, id, tr string) (string, string, string, bool, error) {
			if strings.Contains(tr, "trivial") {
				return "", "", "", false, nil // skip
			}
			return "# " + tr + "\nsession: " + id + "\n## Reusable\n- fact from " + tr + "\n", "slug-" + tr, "success", true, nil
		},
		Consolidate: func(_ context.Context, cur string) (string, error) {
			return "- consolidated (" + itoa(len(cur)) + " bytes)\n", nil
		},
		Summarize: func(_ context.Context, mem string) (string, error) {
			return "SUMMARY: " + itoa(len(mem)) + " bytes\n", nil
		},
	}
}

func TestPipelineStage1IdempotentAndSkip(t *testing.T) {
	p := fakePipe(t)
	sess := []Session{
		{ID: "s1", Transcript: "alpha", Watermark: 1},
		{ID: "s2", Transcript: "trivial", Watermark: 1},
	}
	n, _ := p.Stage1Sessions(context.Background(), sess)
	if n != 1 {
		t.Fatalf("one non-trivial session should summarize, got %d", n)
	}
	// s1 wrote a raw file; s2 (skip) did not.
	if raws := p.Store.RawSummaries(0); len(raws) != 1 {
		t.Fatalf("only the non-trivial session writes raw, got %d", len(raws))
	}
	rows, err := p.Index.Summaries(p.scopeKey())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SessionID != "s1" || rows[0].Outcome != "success" || !strings.Contains(rows[0].RawPath, "/raw/") {
		t.Fatalf("stage1 should record the raw summary in index.sqlite, got %+v", rows)
	}
	kinds := map[string]bool{}
	for {
		j, ok, err := p.Index.Claim(60)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		kinds[j.Kind] = true
		if err := p.Index.Finish(j, nil); err != nil {
			t.Fatal(err)
		}
	}
	if !kinds[JobConsolidate] || !kinds[JobSummary] {
		t.Fatalf("stage1 should enqueue consolidate + summary jobs, got %v", kinds)
	}
	// Re-run at same watermark → idempotent (no new summaries).
	n2, _ := p.Stage1Sessions(context.Background(), sess)
	if n2 != 0 {
		t.Fatalf("same-watermark re-run must be idempotent, got %d", n2)
	}
	// Changed watermark → re-summarizes.
	sess[0].Watermark = 2
	n3, _ := p.Stage1Sessions(context.Background(), sess)
	if n3 != 1 {
		t.Fatalf("changed watermark should re-summarize, got %d", n3)
	}
}

func TestPipelineConsolidateAndSummary(t *testing.T) {
	p := fakePipe(t)
	// Append enough to MEMORY.md to cross the 50-byte threshold.
	p.Store.Append(strings.Repeat("padding note ", 10))
	did, err := p.MaybeConsolidate(context.Background(), false)
	if err != nil || !did {
		t.Fatalf("should consolidate over threshold: did=%v err=%v", did, err)
	}
	if !strings.Contains(p.Store.Read(), "consolidated") {
		t.Fatal("MEMORY.md should be the consolidated output")
	}
	did2, err := p.RegenSummary(context.Background())
	if err != nil || !did2 {
		t.Fatalf("should regen summary: did=%v err=%v", did2, err)
	}
	if !strings.HasPrefix(strings.TrimSpace(p.Store.Injected()), "SUMMARY:") {
		t.Fatalf("injection should now be the small summary, got %q", p.Store.Injected())
	}
}

func TestPipelineRunQueuedProcessesOnlyOwnScope(t *testing.T) {
	p := fakePipe(t)
	p.Store.Append(strings.Repeat("padding note ", 10))
	scope := p.scopeKey()
	if err := p.Index.Enqueue(JobSummary, "other-scope", "scope"); err != nil {
		t.Fatal(err)
	}
	if err := p.Index.Enqueue(JobSummary, scope, "scope"); err != nil {
		t.Fatal(err)
	}
	if err := p.Index.Enqueue(JobConsolidate, scope, "scope"); err != nil {
		t.Fatal(err)
	}

	report, err := p.RunQueued(context.Background(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "consolidated MEMORY.md") || !strings.Contains(report, "regenerated SUMMARY.md") {
		t.Fatalf("queued scope jobs should consolidate and summarize, got %q", report)
	}
	if !strings.HasPrefix(strings.TrimSpace(p.Store.Injected()), "SUMMARY:") {
		t.Fatalf("queued summary job should update injected SUMMARY.md, got %q", p.Store.Injected())
	}
	j, ok, err := p.Index.ClaimScope("other-scope", 60)
	if err != nil || !ok || j.Scope != "other-scope" {
		t.Fatalf("other scope job should be untouched, got %+v ok=%v err=%v", j, ok, err)
	}
}

func TestPipelineRunReport(t *testing.T) {
	p := fakePipe(t)
	rep, _ := p.Run(context.Background(), []Session{{ID: "s1", Transcript: "real work here that is long enough", Watermark: 1}})
	if !strings.Contains(rep, "session summaries") || !strings.Contains(rep, "SUMMARY.md") {
		t.Fatalf("run report should mention summaries + summary regen, got %q", rep)
	}
}
