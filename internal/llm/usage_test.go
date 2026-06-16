package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUsageCacheHitRate covers the headline token-efficiency metric.
func TestUsageCacheHitRate(t *testing.T) {
	cases := []struct {
		name string
		u    Usage
		want float64
	}{
		{"all-fresh", Usage{InputTokens: 100}, 0},
		{"all-cached", Usage{InputTokens: 0, CacheReadTokens: 100}, 1},
		{"half", Usage{InputTokens: 50, CacheReadTokens: 50}, 0.5},
		{"empty", Usage{}, 0},
	}
	for _, c := range cases {
		if got := c.u.CacheHitRate(); got != c.want {
			t.Errorf("%s: hit rate = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestChatUsageSplitsCachedTokens: an OpenAI-compatible usage block reports
// cached tokens inside prompt_tokens; we split them so InputTokens is the fresh
// slice and CacheReadTokens the hit slice.
func TestChatUsageSplitsCachedTokens(t *testing.T) {
	u := chatUsage{PromptTokens: 1000, CompletionTokens: 200}
	u.PromptTokensDetails.CachedTokens = 800
	got := u.usage()
	if got.InputTokens != 200 {
		t.Errorf("fresh input = %d, want 200", got.InputTokens)
	}
	if got.CacheReadTokens != 800 {
		t.Errorf("cache read = %d, want 800", got.CacheReadTokens)
	}
	if got.OutputTokens != 200 {
		t.Errorf("output = %d, want 200", got.OutputTokens)
	}
	// Defensive: cached > prompt must not produce a negative input.
	u2 := chatUsage{PromptTokens: 100}
	u2.PromptTokensDetails.CachedTokens = 500
	if got := u2.usage(); got.InputTokens < 0 {
		t.Errorf("input must never be negative, got %d", got.InputTokens)
	}
}

// TestMantleUsageSplitsCached mirrors the Responses-API split.
func TestMantleUsageSplitsCached(t *testing.T) {
	got := mantleUsage(1000, 200, 750)
	if got.InputTokens != 250 || got.CacheReadTokens != 750 || got.OutputTokens != 200 {
		t.Fatalf("mantleUsage split wrong: %+v", got)
	}
}

// TestChatCompleteParsesCachedUsage: end-to-end, the non-stream Complete path
// surfaces cached tokens from the canned response.
func TestChatCompleteParsesCachedUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":500,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":480}}}`))
	}))
	defer srv.Close()
	c := newChatClient(srv.URL, "m", "k", "test")
	out, err := c.complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Usage.CacheReadTokens != 480 {
		t.Fatalf("cache read = %d, want 480", out.Usage.CacheReadTokens)
	}
	if out.Usage.InputTokens != 20 { // 500 - 480
		t.Fatalf("fresh input = %d, want 20", out.Usage.InputTokens)
	}
	if hr := out.Usage.CacheHitRate(); hr < 0.95 {
		t.Fatalf("hit rate = %v, want ~0.96", hr)
	}
}
