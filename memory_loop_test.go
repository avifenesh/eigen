package main

import (
	"context"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/memory"
)

type memorySummaryProvider struct{}

func (memorySummaryProvider) Name() string    { return "memory-summary" }
func (memorySummaryProvider) ModelID() string { return "memory-summary" }
func (memorySummaryProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	if strings.Contains(req.System, "SMALL injected summary") {
		return &llm.Response{Text: "## Preferences\n- Keep project summaries short."}, nil
	}
	return &llm.Response{Text: "- 2026-06-17 — Keep project summaries short."}, nil
}

func TestRefreshMemorySummaryUpdatesGlobalInjection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	gmem, err := memory.OpenGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if err := gmem.Append("a verbose global note that should be summarized before injection"); err != nil {
		t.Fatal(err)
	}
	idx, err := memory.OpenIndex()
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	did, err := refreshMemorySummary(context.Background(), memorySummaryProvider{}, gmem, idx)
	if err != nil || !did {
		t.Fatalf("refresh global summary: did=%v err=%v", did, err)
	}
	sec := gmem.Section()
	if !strings.Contains(sec, "Keep project summaries short.") {
		t.Fatalf("global section should inject memory_summary.md, got %q", sec)
	}
	if strings.Contains(sec, "verbose global note") {
		t.Fatalf("global section should not inject full MEMORY.md once memory_summary.md exists: %q", sec)
	}
}
