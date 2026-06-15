package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCodexAuth writes a minimal ChatGPT-mode auth.json into a temp CODEX_HOME.
func fakeCodexAuth(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	// A JWT-ish access token with a far-future exp so refresh isn't triggered.
	// header.payload.sig — payload {"exp": 9999999999}
	tok := "eyJhbGciOiJub25lIn0.eyJleHAiOjk5OTk5OTk5OTl9.sig"
	auth := map[string]any{
		"auth_mode": "chatgpt",
		"tokens":    map[string]any{"access_token": tok, "refresh_token": "r", "account_id": "acct-1"},
	}
	b, _ := json.Marshal(auth)
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", dir)
}

func TestMantleBridgeRoutesToChatGPT(t *testing.T) {
	fakeCodexAuth(t)
	t.Setenv("EIGEN_GPT_BRIDGE_URL", "http://example.invalid/v1") // not called; just assert routing
	m, err := NewMantle("openai.gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	if !m.bridge {
		t.Fatal("gpt-5.5 with a ChatGPT credential should route to the bridge")
	}
	if m.Model != "gpt-5.5" {
		t.Fatalf("bridge should use the bare slug, got %q", m.Model)
	}
	if !strings.Contains(m.Name(), "chatgpt") {
		t.Fatalf("Name should say chatgpt, got %q", m.Name())
	}
	if m.BaseURL != "http://example.invalid/v1" {
		t.Fatalf("BaseURL should be the bridge, got %q", m.BaseURL)
	}
}

func TestMantleBridgeOffStaysMantle(t *testing.T) {
	fakeCodexAuth(t)
	t.Setenv("EIGEN_GPT_BRIDGE_OFF", "1")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "tok")
	m, err := NewMantle("openai.gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	if m.bridge {
		t.Fatal("EIGEN_GPT_BRIDGE_OFF should keep the mantle path")
	}
}

// TestBridgeStreamAccumulatesToolCall proves the fix: a ChatGPT-style stream
// where response.completed has EMPTY output, and the function_call arrives only
// via response.output_item.done, still yields a tool call.
func TestBridgeStreamAccumulatesToolCall(t *testing.T) {
	fakeCodexAuth(t)
	sse := strings.Join([]string{
		`data: {"type":"response.created","response":{}}`,
		`data: {"type":"response.output_item.done","item":{"type":"function_call","name":"read","arguments":"{\"path\":\"f.txt\"}","call_id":"call_1"}}`,
		`data: {"type":"response.completed","response":{"status":"completed","output":[]}}`,
		"",
	}, "\n\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()
	t.Setenv("EIGEN_GPT_BRIDGE_URL", srv.URL+"/v1")
	m, err := NewMantle("gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	resp, _, _, err := m.streamOnce(context.Background(), []byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "read" {
		t.Fatalf("should accumulate the function_call from output_item.done, got %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].ID != "call_1" {
		t.Fatalf("call_id should be carried, got %q", resp.ToolCalls[0].ID)
	}
}
