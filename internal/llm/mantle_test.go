package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBuildInputSerializesToolTurns(t *testing.T) {
	req := Request{
		System: "sys",
		Messages: []Message{
			{Role: RoleUser, Text: "hi"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{
				{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`)},
			}},
			{Role: RoleTool, ToolCallID: "c1", ToolName: "read", Text: "filebody"},
		},
	}

	items := buildInput(req)
	if len(items) != 4 {
		t.Fatalf("got %d items, want 4: %+v", len(items), items)
	}
	if items[0].Role != "developer" || string(items[0].Content) != `"sys"` {
		t.Errorf("system not mapped to developer: %+v", items[0])
	}
	if items[1].Role != "user" || string(items[1].Content) != `"hi"` {
		t.Errorf("user message wrong: %+v", items[1])
	}
	fc := items[2]
	if fc.Type != "function_call" || fc.CallID != "c1" || fc.Name != "read" || fc.Arguments != `{"path":"x"}` {
		t.Errorf("function_call wrong: %+v", fc)
	}
	out := items[3]
	if out.Type != "function_call_output" || out.CallID != "c1" || out.Output != "filebody" {
		t.Errorf("function_call_output wrong: %+v", out)
	}
}

func TestArgNormalization(t *testing.T) {
	if got := argString(nil); got != "{}" {
		t.Errorf("argString(nil) = %q, want {}", got)
	}
	if got := argString(json.RawMessage(`{"a":1}`)); got != `{"a":1}` {
		t.Errorf("argString passthrough = %q", got)
	}
	if got := string(normalizeArgs("")); got != "{}" {
		t.Errorf("normalizeArgs(empty) = %q, want {}", got)
	}
	if got := string(normalizeArgs(`{"a":1}`)); got != `{"a":1}` {
		t.Errorf("normalizeArgs passthrough = %q", got)
	}
}

func TestToResponsesTools(t *testing.T) {
	tools := toResponsesTools([]ToolSpec{
		{Name: "read", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)},
	})
	if len(tools) != 1 || tools[0].Type != "function" || tools[0].Name != "read" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	if toResponsesTools(nil) != nil {
		t.Error("expected nil tools for empty input")
	}
}

func TestMantleSetEffort(t *testing.T) {
	m := &Mantle{effort: "high"}
	if !m.SetEffort("low") || m.Effort() != "low" {
		t.Fatalf("SetEffort(low) failed: effort=%q", m.Effort())
	}
	if m.SetEffort("bogus") {
		t.Fatal("invalid effort should return false")
	}
	if m.Effort() != "low" {
		t.Fatal("invalid effort must not change the current setting")
	}
}

func TestMantleIgnoresUnsupportedEnvEffort(t *testing.T) {
	// "max" is Anthropic-only; a GPT model must ignore it (keep its valid
	// default) rather than send it and 400. Regression for the cross-vendor
	// review/route breaking under the opus-default user's EIGEN_REASONING_EFFORT=max.
	t.Setenv("EIGEN_REASONING_EFFORT", "max")
	m, err := NewMantle("openai.gpt-5.5")
	if err != nil {
		t.Skip("no mantle creds:", err)
	}
	if m.effort == "max" {
		t.Fatalf("gpt model must not accept 'max'; got %q", m.effort)
	}
	// A SUPPORTED env value is applied.
	t.Setenv("EIGEN_REASONING_EFFORT", "low")
	m2, _ := NewMantle("openai.gpt-5.5")
	if m2.effort != "low" {
		t.Fatalf("supported env effort should apply; got %q", m2.effort)
	}
}

func TestNewMantleGPT56UsesModelSpecificResponsesRoute(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token")
	t.Setenv("EIGEN_MANTLE_REGION", "us-east-2")
	for _, id := range []string{"openai.gpt-5.6-sol", "openai.gpt-5.6-terra", "openai.gpt-5.6-luna"} {
		m, err := NewMantle(id)
		if err != nil {
			t.Fatalf("NewMantle(%q): %v", id, err)
		}
		if m.BaseURL != "https://bedrock-mantle.us-east-2.api.aws/openai/v1" {
			t.Errorf("%s BaseURL = %q", id, m.BaseURL)
		}
		if m.Effort() != "medium" {
			t.Errorf("%s default effort = %q, want medium", id, m.Effort())
		}
	}
}

// sseCompleted writes a minimal successful Responses SSE stream with one text
// message, then a response.completed event.
func sseCompleted(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	io := w.(interface{ Flush() })
	w.Write([]byte(`data: {"type":"response.output_text.delta","delta":` + mustJSON(text) + "}\n\n"))
	io.Flush()
	w.Write([]byte(`data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":` + mustJSON(text) + `}]}]}}` + "\n\n"))
	io.Flush()
}

// sseFailed writes a transient response.failed event (the intermittent Bedrock
// mantle server_error that arrives over an HTTP-200 stream).
func sseFailed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Write([]byte(`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"The server had an error while processing your request. Sorry about that!"}}}` + "\n\n"))
	w.(interface{ Flush() }).Flush()
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestMantleStreamRetriesTransientFailure is the regression for the eigen↔mantle
// bug: mantle intermittently emits response.failed (transient server_error) on
// an HTTP-200 stream. When it arrives BEFORE any output, eigen must retry rather
// than killing the turn (codex survives because its bridge posts non-streaming
// and retries the equivalent 5xx).
func TestMantleStreamRetriesTransientFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 { // first two attempts fail transiently
			sseFailed(w)
			return
		}
		sseCompleted(w, "OK")
	}))
	defer srv.Close()

	m := &Mantle{BaseURL: srv.URL, Model: "openai.gpt-5.5", effort: "high", token: "t", http: srv.Client()}
	var got strings.Builder
	resp, err := m.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}},
		func(c StreamChunk) { got.WriteString(c.Text) })
	if err != nil {
		t.Fatalf("Stream should recover from transient failures: %v", err)
	}
	if resp.Text != "OK" {
		t.Fatalf("resp.Text = %q, want OK", resp.Text)
	}
	if got.String() != "OK" {
		t.Fatalf("streamed text = %q, want OK (no duplication)", got.String())
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 attempts (2 fail + 1 success), got %d", calls.Load())
	}
}

// TestMantleStreamFailureAfterOutputIsNotRetried ensures a failure that arrives
// AFTER deltas were emitted is surfaced (retrying would duplicate streamed text).
func TestMantleStreamFailureAfterOutputIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(interface{ Flush() })
		w.Write([]byte(`data: {"type":"response.output_text.delta","delta":"partial"}` + "\n\n"))
		fl.Flush()
		w.Write([]byte(`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"boom"}}}` + "\n\n"))
		fl.Flush()
	}))
	defer srv.Close()

	m := &Mantle{BaseURL: srv.URL, Model: "openai.gpt-5.5", effort: "high", token: "t", http: srv.Client()}
	_, err := m.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}}, nil)
	if err == nil {
		t.Fatal("a failure after streamed output must be surfaced, not retried")
	}
	if !strings.Contains(err.Error(), "server_error") {
		t.Fatalf("error should carry the reason: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("must NOT retry after output emitted; got %d attempts", calls.Load())
	}
}

func TestCodexStreamRetriesTransientFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			sseFailed(w)
			return
		}
		sseCompleted(w, "OK")
	}))
	defer srv.Close()

	c := &Codex{BaseURL: srv.URL, Model: "gpt-5.5", token: "t", http: srv.Client()}
	var got strings.Builder
	resp, err := c.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}},
		func(c StreamChunk) { got.WriteString(c.Text) })
	if err != nil {
		t.Fatalf("Codex Stream should recover from transient pre-output failures: %v", err)
	}
	if resp.Text != "OK" {
		t.Fatalf("resp.Text = %q, want OK", resp.Text)
	}
	if got.String() != "OK" {
		t.Fatalf("streamed text = %q, want OK (no duplication)", got.String())
	}
	if calls.Load() != 3 {
		t.Fatalf("expected 3 attempts (2 fail + 1 success), got %d", calls.Load())
	}
}

func TestCodexStreamFailureAfterOutputIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(interface{ Flush() })
		w.Write([]byte(`data: {"type":"response.output_text.delta","delta":"partial"}` + "\n\n"))
		fl.Flush()
		w.Write([]byte(`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"boom"}}}` + "\n\n"))
		fl.Flush()
	}))
	defer srv.Close()

	c := &Codex{BaseURL: srv.URL, Model: "gpt-5.5", token: "t", http: srv.Client()}
	var got strings.Builder
	_, err := c.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}},
		func(c StreamChunk) { got.WriteString(c.Text) })
	if err == nil {
		t.Fatal("a Codex failure after streamed output must be surfaced, not retried")
	}
	if !strings.Contains(err.Error(), "server_error") {
		t.Fatalf("error should carry the reason: %v", err)
	}
	if got.String() != "partial" {
		t.Fatalf("streamed text = %q, want partial", got.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("must NOT retry after output emitted; got %d attempts", calls.Load())
	}
}

func TestStreamFailReason(t *testing.T) {
	r := streamFailReason(json.RawMessage(`{"error":{"code":"server_error","message":"oops"}}`))
	if r != "server_error: oops" {
		t.Fatalf("got %q", r)
	}
	if r := streamFailReason(json.RawMessage(`{"status":"failed"}`)); !strings.Contains(r, "failed") {
		t.Fatalf("fallback to raw json, got %q", r)
	}
}
