package retrieve

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestTokenizeSplitsIdentifiers(t *testing.T) {
	got := tokenize("maxContextTokens = computeBudget(HTTPServer)")
	want := map[string]bool{
		"maxcontexttokens": true, "max": true, "context": true, "tokens": true,
		"computebudget": true, "compute": true, "budget": true,
		"httpserver": true, "http": true, "server": true,
	}
	have := map[string]bool{}
	for _, tok := range got {
		have[tok] = true
	}
	for w := range want {
		if !have[w] {
			t.Errorf("tokenize missing %q (got %v)", w, got)
		}
	}
	// 1-rune noise dropped.
	for _, tok := range got {
		if len([]rune(tok)) < 2 {
			t.Errorf("token %q shorter than 2 runes should be dropped", tok)
		}
	}
}

func TestSplitIdentifier(t *testing.T) {
	cases := map[string][]string{
		"maxContextTokens": {"max", "Context", "Tokens"},
		"HTTPServer":       {"HTTP", "Server"},
		"plain":            {"plain"},
		"getURL":           {"get", "URL"},
	}
	for in, want := range cases {
		got := splitIdentifier(in)
		if len(got) != len(want) {
			t.Fatalf("splitIdentifier(%q) = %v, want %v", in, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("splitIdentifier(%q) = %v, want %v", in, got, want)
			}
		}
	}
}

func TestBM25RanksRelevantChunkFirst(t *testing.T) {
	chunks := []Chunk{
		{Path: "a.go", Start: 1, End: 1, Text: "func computeContextBudget() int { return maxContextTokens }"},
		{Path: "b.go", Start: 1, End: 1, Text: "package main\nfunc main() { fmt.Println(\"hello\") }"},
		{Path: "c.go", Start: 1, End: 1, Text: "// unrelated helper for parsing JSON config"},
	}
	bi := buildBM25(chunks)
	rank := bi.rank("how is the context budget computed")
	if len(rank) == 0 {
		t.Fatal("expected at least one BM25 hit")
	}
	if rank[0] != 0 {
		t.Fatalf("chunk 0 (context budget) should rank first, got order %v", rank)
	}
}

func TestBM25NoMatchRanksEmpty(t *testing.T) {
	bi := buildBM25([]Chunk{{Path: "a.go", Text: "alpha beta gamma"}})
	if r := bi.rank("nonexistent zzzzz"); len(r) != 0 {
		t.Fatalf("a query with no shared terms should rank nothing, got %v", r)
	}
}

// TestBM25RankMatchesFullScan pins the inverted-index rank to the same answer a
// brute-force scan (score over every chunk) produces: only chunks sharing a
// query term may score, and the order must agree (ties broken by chunk index).
func TestBM25RankMatchesFullScan(t *testing.T) {
	chunks := []Chunk{
		{Path: "a.go", Text: "func parseConfig() { loadDefaults() }"},
		{Path: "b.go", Text: "the parser reads the config file and validates it"},
		{Path: "c.go", Text: "completely unrelated networking retry loop"},
		{Path: "d.go", Text: "config config config parse parse default default"},
		{Path: "e.go", Text: "// no shared vocabulary whatsoever zzzz"},
	}
	bi := buildBM25(chunks)
	const query = "parse the config defaults"
	got := bi.rank(query)

	// Reference: full scan via score over all chunks, same tiebreak as rank.
	terms := tokenize(query)
	type sc struct {
		i int
		s float64
	}
	var scored []sc
	for i := range chunks {
		if s := bi.score(i, terms); s > 0 {
			scored = append(scored, sc{i, s})
		}
	}
	sort.Slice(scored, func(a, b int) bool {
		if scored[a].s != scored[b].s {
			return scored[a].s > scored[b].s
		}
		return scored[a].i < scored[b].i
	})
	want := make([]int, len(scored))
	for j, x := range scored {
		want[j] = x.i
	}

	if len(got) != len(want) {
		t.Fatalf("rank len %d (%v) != full-scan len %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("inverted-index rank %v != full-scan %v", got, want)
		}
	}
	// Chunk c (no shared term) must never appear.
	for _, ci := range got {
		if ci == 2 {
			t.Fatalf("chunk with no shared query term should not rank, got %v", got)
		}
	}
}

func TestFuseRRFMergesRankings(t *testing.T) {
	// lexical likes chunk 2 then 0; vector likes chunk 0 then 1. Chunk 0 appears
	// high in both → should win the fusion.
	order, score := fuseRRF([]int{2, 0}, []int{0, 1}, 3)
	if len(order) == 0 || order[0] != 0 {
		t.Fatalf("chunk 0 (high in both lists) should fuse to first, got %v (scores %v)", order, score)
	}
	// A chunk in only one list still appears.
	seen := map[int]bool{}
	for _, ci := range order {
		seen[ci] = true
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("RRF must include chunks present in either list, got %v", order)
	}
}

func TestFuseRRFLexicalOnly(t *testing.T) {
	// No vector list (no embedder) → fusion preserves lexical order.
	order, _ := fuseRRF([]int{5, 3, 1}, nil, 6)
	want := []int{5, 3, 1}
	if len(order) != len(want) {
		t.Fatalf("got %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("lexical-only RRF should preserve order: got %v, want %v", order, want)
		}
	}
}

// TestSearchWithoutEmbedderUsesBM25 is the headline: retrieval works with NO
// embedder, ranking by BM25 over the project's files.
func TestSearchWithoutEmbedderUsesBM25(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir()) // isolate the on-disk index
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("budget.go", "package x\n// computeContextBudget returns the token budget\nfunc computeContextBudget() int { return maxContextTokens }\n")
	write("hello.go", "package x\nfunc Hello() string { return \"hi\" }\n")
	write("readme.md", "# project\nsome unrelated prose about cats and dogs\n")

	idx, err := Open(dir, nil) // nil embedder → BM25-only
	if err != nil {
		t.Fatalf("Open with nil embedder should succeed: %v", err)
	}
	if _, err := idx.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if idx.Len() == 0 {
		t.Fatal("BM25-only index should have chunks after Sync")
	}
	res, err := idx.Search(context.Background(), "how is the context budget computed", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("expected BM25 hits without an embedder")
	}
	if res[0].Path != "budget.go" {
		t.Fatalf("budget.go should rank first, got %s (all: %+v)", res[0].Path, res)
	}
}
