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
		Stage1: func(_ context.Context, id, tr string) (Stage1Result, bool, error) {
			if strings.Contains(tr, "trivial") {
				return Stage1Result{}, false, nil // skip
			}
			return Stage1Result{
				RawMemory:      "session: " + id + "\nREUSABLE:\n- fact from " + tr + "\n",
				RolloutSummary: "# " + tr + "\nsession: " + id + "\n## Reusable\n- fact from " + tr + "\n",
				RolloutSlug:    "slug-" + tr,
				Outcome:        "success",
			}, true, nil
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
	if len(rows) != 1 || rows[0].SessionID != "s1" || rows[0].Outcome != "success" || !strings.Contains(rows[0].RawPath, "/rollout_summaries/") {
		t.Fatalf("stage1 should record the raw summary in index.sqlite, got %+v", rows)
	}
	stageRows, err := p.Index.Stage1Outputs(p.scopeKey(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(stageRows) != 1 || !strings.Contains(stageRows[0].RawMemory, "fact from alpha") || !strings.Contains(stageRows[0].RolloutSummary, "# alpha") {
		t.Fatalf("stage1_outputs should hold raw memory and rollout summary, got %+v", stageRows)
	}
	if strings.Contains(p.Store.Read(), "fact from alpha") {
		t.Fatal("Stage1 must not append directly to MEMORY.md; Phase2 owns consolidation")
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
	p.Store.Rewrite("- " + strings.Repeat("padding note ", 10) + "\n")
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
	p.Store.Rewrite("- " + strings.Repeat("padding note ", 10) + "\n")
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
	if !strings.Contains(report, "consolidated MEMORY.md") || !strings.Contains(report, "regenerated memory_summary.md") {
		t.Fatalf("queued scope jobs should consolidate and summarize, got %q", report)
	}
	if !strings.HasPrefix(strings.TrimSpace(p.Store.Injected()), "SUMMARY:") {
		t.Fatalf("queued summary job should update injected memory_summary.md, got %q", p.Store.Injected())
	}
	j, ok, err := p.Index.ClaimScope("other-scope", 60)
	if err != nil || !ok || j.Scope != "other-scope" {
		t.Fatalf("other scope job should be untouched, got %+v ok=%v err=%v", j, ok, err)
	}
}

func TestPipelineRunReport(t *testing.T) {
	p := fakePipe(t)
	rep, _ := p.Run(context.Background(), []Session{{ID: "s1", Transcript: "real work here that is long enough", Watermark: 1}})
	if !strings.Contains(rep, "session summaries") || !strings.Contains(rep, "memory_summary.md") {
		t.Fatalf("run report should mention summaries + summary regen, got %q", rep)
	}
}

func TestPipelinePhase2BuildsRawMemoriesFromStage1AndAdHoc(t *testing.T) {
	p := fakePipe(t)
	if n, err := p.Stage1Sessions(context.Background(), []Session{{ID: "s1", Transcript: "alpha", Watermark: 10}}); err != nil || n != 1 {
		t.Fatalf("stage1: n=%d err=%v", n, err)
	}
	if err := p.Store.Append("manual save should enter phase2"); err != nil {
		t.Fatal(err)
	}
	if did, err := p.MaybeConsolidate(context.Background(), true); err != nil || !did {
		t.Fatalf("phase2 consolidate: did=%v err=%v", did, err)
	}
	raw := p.Store.readFile(p.Store.RawMemoriesPath())
	if !strings.Contains(raw, "fact from alpha") || !strings.Contains(raw, "manual save should enter phase2") {
		t.Fatalf("raw_memories.md should merge Stage1 and ad-hoc inputs, got:\n%s", raw)
	}
	if strings.Contains(strings.Join(p.Store.AdHocNotes(0), "\n"), "manual save") && !strings.Contains(p.Store.Read(), "consolidated") {
		t.Fatal("phase2 should rewrite MEMORY.md from the merged input")
	}
}
