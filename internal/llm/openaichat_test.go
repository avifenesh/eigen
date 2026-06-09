package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatBodyShapesMessagesAndTools(t *testing.T) {
	c := newChatClient("https://x/v1", "m1", "k", "test")
	req := Request{
		System: "sys",
		Messages: []Message{
			{Role: RoleUser, Text: "hi"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`)}}},
			{Role: RoleTool, ToolCallID: "c1", Text: "body"},
		},
		Tools: []ToolSpec{{Name: "read", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}
	raw, err := c.body(req, false)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "m1" {
		t.Fatalf("model wrong: %v", got["model"])
	}
	msgs, _ := got["messages"].([]any)
	if len(msgs) != 4 { // system + user + assistant(tool) + tool
		t.Fatalf("expected 4 messages, got %d: %v", len(msgs), msgs)
	}
	if _, ok := got["tools"]; !ok {
		t.Fatal("tools should be present")
	}
	if _, ok := got["stream"]; ok {
		t.Fatal("non-stream body must not set stream")
	}
}

func TestChatBodyMergesExtraFields(t *testing.T) {
	c := newChatClient("https://x/v1", "m1", "k", "test")
	c.extra = func() map[string]any {
		return map[string]any{"search_parameters": map[string]any{"mode": "on"}}
	}
	raw, err := c.body(Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if got["stream"] != true {
		t.Fatal("stream body should set stream=true")
	}
	if _, ok := got["search_parameters"]; !ok {
		t.Fatalf("extra fields should be merged: %v", got)
	}
}

func TestChatStreamCapturesContentAndReasoning(t *testing.T) {
	// A GLM-style SSE stream: reasoning_content deltas, then a content delta,
	// then an empty trailing chunk. The assembled Response must carry the text.
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"thinking..."}}]}`,
		`data: {"choices":[{"delta":{"content":"PONG"}}]}`,
		`data: {"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`,
		"data: [DONE]",
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	c := newChatClient(srv.URL, "glm-4.6", "k", "glm")
	var streamedText, streamedReason strings.Builder
	out, err := c.stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "say PONG"}}},
		func(ch StreamChunk) {
			if ch.Kind == ChunkText {
				streamedText.WriteString(ch.Text)
			} else {
				streamedReason.WriteString(ch.Text)
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "PONG" {
		t.Fatalf("assembled text = %q, want PONG", out.Text)
	}
	if out.Reasoning != "thinking..." {
		t.Fatalf("assembled reasoning = %q", out.Reasoning)
	}
	if streamedText.String() != "PONG" {
		t.Fatalf("streamed text = %q", streamedText.String())
	}
}
