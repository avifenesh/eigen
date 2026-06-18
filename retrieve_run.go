package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/retrieve"
	"github.com/avifenesh/eigen/internal/tool"
)

// retrieveRunner builds the `retrieve` tool's run function for a session rooted
// at dir. The per-project index is opened LAZILY on first use (so no embedding
// work happens unless the model actually retrieves), synced incrementally
// before each search (edits since the last call are re-embedded), and the
// top-k hits are formatted. No embedder configured / reachable → a clear
// "unavailable" error and eigen works exactly as before. Mutex-guarded: the
// daemon may invoke tools from different goroutines.
func retrieveRunner(dir string) tool.RetrieveRun {
	var (
		mu    sync.Mutex
		idx   *retrieve.Index
		tried bool
	)
	return func(ctx context.Context, query string, k int) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if idx == nil {
			if tried {
				return "", fmt.Errorf("retrieval unavailable")
			}
			tried = true
			// An embedder is OPTIONAL: when none is configured/reachable, open a
			// lexical-only (BM25) index so `retrieve` still works. With an
			// embedder, Search fuses BM25 + cosine.
			emb, _ := llm.NewEmbedder()
			opened, err := retrieve.Open(dir, emb)
			if err != nil {
				return "", fmt.Errorf("retrieval unavailable: %w", err)
			}
			idx = opened
		}
		if _, err := idx.Sync(ctx); err != nil {
			// A sync failure (embedder went away mid-session) is reported, but a
			// previously-built index can still answer from what it has.
			if idx.Len() == 0 {
				return "", fmt.Errorf("retrieval unavailable: %w", err)
			}
		}
		res, err := idx.Search(ctx, query, k)
		if err != nil {
			return "", err
		}
		return formatRetrieval(query, res), nil
	}
}

// formatRetrieval renders hits as a scannable list: path:lines (score) + a
// short snippet, so the model gets location + content.
func formatRetrieval(query string, res []retrieve.Result) string {
	if len(res) == 0 {
		return "no relevant chunks found for: " + query + "\n(the index may be empty — is this a code project? try grep for exact strings)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d relevant span(s) for %q:\n", len(res), query)
	for _, r := range res {
		fmt.Fprintf(&b, "\n%s:%d-%d  (%.2f)\n", r.Path, r.Start, r.End, r.Score)
		for _, line := range strings.Split(strings.TrimRight(r.Snippet, "\n"), "\n") {
			b.WriteString("  " + line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
