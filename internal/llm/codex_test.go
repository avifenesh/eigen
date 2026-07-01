package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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
	if payload.Reasoning == nil || payload.Reasoning.Effort != "xhigh" {
		t.Fatalf("effort = %+v, want xhigh (gpt-5.5 codex default)", payload.Reasoning)
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

// Codex delivers tool calls via response.output_item.done (function_call), and
// its response.completed event has output:[] — parseResponsesSSE must collect
// the tool call from the item event, not the empty completed event. This is the
// fix for "model returned no actionable output after 3 empty turns".
func TestCodexParsesToolCallFromOutputItem(t *testing.T) {
	writeCodexAuth(t, "tok", "ref", "acct")
	sse := "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"/x\\\"}\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":5,\"output_tokens\":2}}}\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()
	t.Setenv("EIGEN_CODEX_BASE_URL", srv.URL)
	c, err := NewCodex("gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "read /x"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call from output_item.done, got %d (completed event is empty!)", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "read_file" {
		t.Fatalf("tool name = %q", resp.ToolCalls[0].Name)
	}
	if string(resp.ToolCalls[0].Arguments) != `{"path":"/x"}` {
		t.Fatalf("tool args = %s", resp.ToolCalls[0].Arguments)
	}
	// include:["reasoning.encrypted_content"] is requested.
	p := c.buildPayload(Request{Messages: []Message{{Role: RoleUser, Text: "x"}}}, true)
	if len(p.Include) != 1 || p.Include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include = %v, want [reasoning.encrypted_content]", p.Include)
	}
}

// The encrypted reasoning blob is echoed back at the ITEM level
// (encrypted_content field), NOT a content-array part — the server rejects a
// content array on a reasoning item ("expected maximum length 0").
func TestReasoningEncryptedEchoedAtItemLevel(t *testing.T) {
	msg := Message{
		Role:               RoleAssistant,
		Reasoning:          "thinking",
		ReasoningID:        "rs_abc",
		ReasoningEncrypted: "BLOB==",
	}
	items := buildInput(Request{Messages: []Message{msg}})
	var ri *responsesInputItem
	for i := range items {
		if items[i].Type == "reasoning" {
			ri = &items[i]
		}
	}
	if ri == nil {
		t.Fatal("no reasoning item emitted")
	}
	if ri.Encrypted != "BLOB==" {
		t.Fatalf("encrypted_content field = %q, want the blob", ri.Encrypted)
	}
	if ri.Summary == nil || len(*ri.Summary) != 1 || (*ri.Summary)[0].Text != "thinking" {
		t.Fatalf("summary = %#v, want preserved reasoning summary", ri.Summary)
	}
	if ri.Content != nil {
		t.Fatalf("reasoning item must NOT carry a content array (server rejects it): %s", ri.Content)
	}
}

func TestReasoningEncryptedWithoutTextStillCarriesEmptySummary(t *testing.T) {
	msg := Message{
		Role:               RoleAssistant,
		ReasoningID:        "rs_empty_summary",
		ReasoningEncrypted: "BLOB==",
	}
	payload := (&Codex{Model: "gpt-5.5", token: "tok"}).buildPayload(Request{Messages: []Message{msg}}, false)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"type":"reasoning"`) {
		t.Fatalf("payload missing reasoning item: %s", raw)
	}
	if !strings.Contains(string(raw), `"summary":[]`) {
		t.Fatalf("encrypted reasoning item must include empty summary array for Codex Responses API: %s", raw)
	}
}

// A legacy reasoning message with an id (and/or summary) but NO encrypted blob
// must be DROPPED from the input, not emitted by bare id — store:false never
// persisted it, so emitting the bare id 404s forever ("Item 'rs_…' not found").
// This is the stuck-transcript self-heal: the old turn's chain of thought is
// lost, but the conversation resumes instead of wedging.
func TestLegacyReasoningWithoutBlobIsDropped(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Text: "hi"},
		// legacy assistant turn: reasoning + tool call, but no encrypted blob
		{Role: RoleAssistant, Reasoning: "old thought", ReasoningID: "rs_09fb781d9aaa", ToolCalls: []ToolCall{{ID: "call_1", Name: "read", Arguments: json.RawMessage(`{}`)}}},
		{Role: RoleTool, ToolCallID: "call_1", Text: "result"},
		{Role: RoleUser, Text: "continue"},
	}
	items := buildInput(Request{Messages: msgs})
	// the function_call must still be present (stateless — no server
	// persistence needed); only the reasoning id is the persistence problem.
	var sawCall bool
	for _, it := range items {
		if it.Type == "reasoning" {
			t.Fatalf("legacy reasoning without blob must be dropped, got item id=%q", it.ID)
		}
		if it.Type == "function_call" {
			sawCall = true
		}
	}
	if !sawCall {
		t.Error("expected the function_call to survive (only reasoning is dropped)")
	}
}

// A turn that emits MULTIPLE reasoning items (xhigh/fast often does) must pair
// the echoed id with ITS OWN encrypted blob — not first-item-id +
// last-item-blob (the old first-id/last-blob bug 400'd: "Encrypted content
// item_id did not match the target item id"). The server verifies the pair.
func TestApplyOutputItemPairsReasoningIDWithBlob(t *testing.T) {
	out := &Response{}
	// Two reasoning items in one turn, each with its own id + blob.
	applyOutputItem(json.RawMessage(`{"type":"reasoning","id":"rs_AAA","encrypted_content":"BLOB_A","summary":[{"type":"summary_text","text":"first"}]}`), out)
	applyOutputItem(json.RawMessage(`{"type":"reasoning","id":"rs_BBB","encrypted_content":"BLOB_B","summary":[{"type":"summary_text","text":"second"}]}`), out)
	// The LAST item with a blob wins — and id + blob must come from the SAME item.
	if out.ReasoningID != "rs_BBB" {
		t.Fatalf("id = %q, want rs_BBB (paired with its blob)", out.ReasoningID)
	}
	if out.ReasoningEncrypted != "BLOB_B" {
		t.Fatalf("blob = %q, want BLOB_B (the rs_BBB blob)", out.ReasoningEncrypted)
	}
	// The mismatch that used to 400 must never happen: rs_AAA is not paired
	// with BLOB_B.
	if out.ReasoningID == "rs_AAA" && out.ReasoningEncrypted == "BLOB_B" {
		t.Fatal("id/blob mispaired across reasoning items (the 400 bug)")
	}
}

// TestCodexConcurrentRefreshRace verifies that the flock+recheck guard prevents
// concurrent token refresh races. Codex uses ROTATING refresh tokens: each
// refresh call returns a NEW refresh_token and invalidates the old one. Two
// processes that both see "token expired" and race to refresh → the loser's
// write clobbers the winner's rotated token → both invalidated → user must
// re-login. We guard with an flock on auth.json.lock: acquire → RE-CHECK
// freshness by re-reading auth.json → only refresh+write if still stale. This
// test simulates two goroutines racing to refresh and asserts that only ONE
// refresh call happens (the second sees the first's result after acquiring the
// lock and skips its refresh).
func TestCodexConcurrentRefreshRace(t *testing.T) {
	authPath := writeCodexAuth(t, "stale-tok", "ref-tok-1", "acct-1")
	var refreshCalls int
	var mu sync.Mutex
	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		refreshCalls++
		call := refreshCalls
		mu.Unlock()
		// Each refresh issues a NEW rotated refresh_token (ref-tok-2, ref-tok-3, …).
		// The second refresh (if it runs) will use the stale ref-tok-1 → BOTH are
		// invalidated (the first's ref-tok-2 is clobbered by the second's write).
		_, _ = w.Write([]byte(`{"access_token":"fresh-tok-` + string(rune('0'+call)) + `","refresh_token":"ref-tok-` + string(rune('1'+call)) + `"}`))
	}))
	defer oauth.Close()

	c1 := &Codex{authPath: authPath, oauthURL: oauth.URL, token: "stale-tok", refresh: "ref-tok-1", http: &http.Client{Timeout: 30 * time.Second}}
	c2 := &Codex{authPath: authPath, oauthURL: oauth.URL, token: "stale-tok", refresh: "ref-tok-1", http: &http.Client{Timeout: 30 * time.Second}}

	// Race two goroutines, both seeing expired token → both call refreshWithLock.
	// The flock ensures they run sequentially; the recheck inside refreshWithLock
	// makes the second one see the first's result and skip its refresh.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = c1.refreshWithLock(context.Background(), "ref-tok-1")
	}()
	go func() {
		defer wg.Done()
		_ = c2.refreshWithLock(context.Background(), "ref-tok-1")
	}()
	wg.Wait()

	mu.Lock()
	calls := refreshCalls
	mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected exactly ONE refresh (the other should recheck+skip), got %d", calls)
	}
	// Both Codex instances should see the refreshed token in memory (loaded by
	// the recheck path for the one that acquired the lock second).
	if c1.token != "fresh-tok-1" && c2.token != "fresh-tok-1" {
		t.Fatalf("at least one instance should have token=fresh-tok-1, got c1=%q c2=%q", c1.token, c2.token)
	}
	// auth.json on disk should have the single refresh's result.
	a, _ := readCodexAuth(authPath)
	if a.Tokens.AccessToken != "fresh-tok-1" {
		t.Fatalf("auth.json token = %q, want fresh-tok-1", a.Tokens.AccessToken)
	}
	if a.Tokens.RefreshToken != "ref-tok-2" {
		t.Fatalf("auth.json refresh = %q, want ref-tok-2 (rotated once)", a.Tokens.RefreshToken)
	}
}
