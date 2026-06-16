package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// writeCodexAuth writes a chatgpt-mode auth.json and points EIGEN_CODEX_AUTH at it.
func writeCodexAuth(t *testing.T, access, refresh, account string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	a := codexAuth{AuthMode: "chatgpt"}
	a.Tokens.AccessToken = access
	a.Tokens.RefreshToken = refresh
	a.Tokens.AccountID = account
	b, _ := json.MarshalIndent(a, "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EIGEN_CODEX_AUTH", path)
	return path
}

func TestNewCodexRequiresChatGPTToken(t *testing.T) {
	// API-key-only auth.json (no tokens) must be rejected.
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	_ = os.WriteFile(path, []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"sk-x"}`), 0o600)
	t.Setenv("EIGEN_CODEX_AUTH", path)
	if _, err := NewCodex("gpt-5.5"); err == nil {
		t.Fatal("API-key-only auth should be rejected by the codex provider")
	}
}

func TestCodexBuildsRequestWithTierAndEffort(t *testing.T) {
	writeCodexAuth(t, "acc-tok", "ref-tok", "acct-123")
	c, err := NewCodex("gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	// Catalog default: priority tier, high effort.
	if !c.FastMode() {
		t.Fatal("gpt-5.5 should default to fast (priority) per the catalog")
	}
	payload := c.buildPayload(Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}}, false)
	if payload.ServiceTier != "priority" {
		t.Fatalf("service_tier = %q, want priority", payload.ServiceTier)
	}
	if payload.Reasoning == nil || payload.Reasoning.Effort != "high" {
		t.Fatalf("effort = %+v, want high", payload.Reasoning)
	}
	// Toggle fast off → no service_tier sent.
	c.SetFast(false)
	if c.buildPayload(Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}}, false).ServiceTier != "" {
		t.Fatal("fast off should drop service_tier")
	}
	// Headers carry the bearer + account id.
	h := c.headers()
	if h["Authorization"] != "Bearer acc-tok" {
		t.Fatalf("auth header = %q", h["Authorization"])
	}
	if h["ChatGPT-Account-Id"] != "acct-123" {
		t.Fatalf("account header = %q", h["ChatGPT-Account-Id"])
	}
}

func TestCodexCompleteAgainstLocalServer(t *testing.T) {
	writeCodexAuth(t, "acc-tok", "ref-tok", "acct-123")
	var gotTier, gotAuth, gotAccount string
	var gotStore *bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("ChatGPT-Account-Id")
		var body responsesRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTier = body.ServiceTier
		gotStore = body.Store
		// Codex is stream-only: reply as SSE with text deltas + an empty
		// completed event (the real backend's completed output is []).
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello from codex\"}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":3}}}\n\n"))
	}))
	defer srv.Close()
	t.Setenv("EIGEN_CODEX_BASE_URL", srv.URL)

	c, err := NewCodex("gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello from codex" {
		t.Fatalf("text = %q", resp.Text)
	}
	if gotTier != "priority" {
		t.Fatalf("server saw service_tier %q, want priority", gotTier)
	}
	if gotStore == nil || *gotStore != false {
		t.Fatalf("server should see store:false, got %v", gotStore)
	}
	if gotAuth != "Bearer acc-tok" || gotAccount != "acct-123" {
		t.Fatalf("server saw auth=%q account=%q", gotAuth, gotAccount)
	}
}

func TestCodexRefreshesOn401(t *testing.T) {
	authPath := writeCodexAuth(t, "stale-tok", "ref-tok", "acct-1")
	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"fresh-tok","refresh_token":"ref2"}`))
	}))
	defer oauth.Close()

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") == "Bearer stale-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"expired"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[],\"usage\":{}}}\n\n"))
	}))
	defer srv.Close()
	t.Setenv("EIGEN_CODEX_BASE_URL", srv.URL)

	c, err := NewCodex("gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	c.oauthURL = oauth.URL
	resp, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatalf("complete after refresh: %v", err)
	}
	if resp.Text != "ok" {
		t.Fatalf("text = %q", resp.Text)
	}
	if calls < 2 {
		t.Fatalf("expected a retry after 401, got %d calls", calls)
	}
	a, _ := readCodexAuth(authPath)
	if a.Tokens.AccessToken != "fresh-tok" {
		t.Fatalf("auth.json not updated, token = %q", a.Tokens.AccessToken)
	}
}

// Codex requires the system prompt in top-level `instructions`, not as a
// developer input item (the backend 400s "Instructions are required" otherwise).
func TestCodexPutsSystemInInstructions(t *testing.T) {
	writeCodexAuth(t, "tok", "ref", "acct")
	c, err := NewCodex("gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	p := c.buildPayload(Request{System: "You are eigen.", Messages: []Message{{Role: RoleUser, Text: "hi"}}}, false)
	if p.Instructions != "You are eigen." {
		t.Fatalf("instructions = %q, want the system prompt", p.Instructions)
	}
	// The system prompt must NOT also appear as a developer input item.
	for _, it := range p.Input {
		if it.Role == "developer" {
			t.Fatal("system prompt must not be duplicated as a developer input item")
		}
	}
}
