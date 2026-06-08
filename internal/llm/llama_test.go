package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLlamaRequestShapeAndParsing(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"choices":[{"message":{"content":"done","tool_calls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{\"path\":\"x\"}"}}]}}]}`))
	}))
	defer srv.Close()

	l := &Llama{BaseURL: srv.URL, Model: "local", http: &http.Client{Timeout: 5 * time.Second}}
	resp, err := l.Complete(context.Background(), Request{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Text: "go"}},
		Tools:    []ToolSpec{{Name: "read", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Response parsing.
	if resp.Text != "done" {
		t.Errorf("text = %q, want done", resp.Text)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "read" || string(resp.ToolCalls[0].Arguments) != `{"path":"x"}` {
		t.Fatalf("tool call wrong: %+v", resp.ToolCalls)
	}

	// Request shape: chat-completions nests tools under "function", and system
	// is role "system" (not mantle's "developer").
	var req chatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Fatalf("messages wrong: %+v", req.Messages)
	}
	if len(req.Tools) != 1 || req.Tools[0].Type != "function" || req.Tools[0].Function.Name != "read" {
		t.Fatalf("tools wrong: %+v", req.Tools)
	}
	if !strings.Contains(string(body), `"function"`) {
		t.Fatal("tools should nest under 'function'")
	}
}

func TestLlamaSerializesToolTurn(t *testing.T) {
	msgs := llamaMessages(Request{
		Messages: []Message{
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`)}}},
			{Role: RoleTool, ToolCallID: "c1", Text: "filebody"},
		},
	})
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Role != "assistant" || len(msgs[0].ToolCalls) != 1 || msgs[0].ToolCalls[0].Function.Name != "read" {
		t.Errorf("assistant tool call wrong: %+v", msgs[0])
	}
	if msgs[1].Role != "tool" || msgs[1].ToolCallID != "c1" || msgs[1].Content != "filebody" {
		t.Errorf("tool result wrong: %+v", msgs[1])
	}
}
