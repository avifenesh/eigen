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
	"time"
)

func TestAnthropicMessagesGroupsToolResults(t *testing.T) {
	msgs := anthropicMessages(Request{
		Messages: []Message{
			{Role: RoleUser, Text: "do it"},
			{Role: RoleAssistant, Text: "sure", ToolCalls: []ToolCall{
				{ID: "t1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)},
				{ID: "t2", Name: "read", Arguments: json.RawMessage(`{"path":"b"}`)},
			}},
			{Role: RoleTool, ToolCallID: "t1", Text: "A"},
			{Role: RoleTool, ToolCallID: "t2", Text: "B", ToolError: true},
		},
	})
	// user(text), assistant(text+2 tool_use), user(2 tool_result).
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Content[0].Text != "do it" {
		t.Errorf("msg0 wrong: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || len(msgs[1].Content) != 3 {
		t.Fatalf("assistant turn should have text + 2 tool_use: %+v", msgs[1])
	}
	if msgs[1].Content[1].Type != "tool_use" || msgs[1].Content[1].Name != "read" || msgs[1].Content[1].ID != "t1" {
		t.Errorf("tool_use 0 wrong: %+v", msgs[1].Content[1])
	}
	if msgs[2].Role != "user" || len(msgs[2].Content) != 2 {
		t.Fatalf("both tool results should group into one user turn: %+v", msgs[2])
	}
	if msgs[2].Content[0].Type != "tool_result" || msgs[2].Content[0].ToolUseID != "t1" || msgs[2].Content[0].Content != "A" {
		t.Errorf("tool_result 0 wrong: %+v", msgs[2].Content[0])
	}
	if !msgs[2].Content[1].IsError {
		t.Errorf("tool_result 1 should be flagged is_error: %+v", msgs[2].Content[1])
	}
}

// TestAnthropicMessagesReEmitsSignedThinking is the regression for APP-046:
// with interleaved thinking + tool use, the prior signed thinking block must be
// echoed back on the assistant turn, preceding the tool_use, or Anthropic loses
// the chain of thought (and can reject it under strict signature validation).
func TestAnthropicMessagesReEmitsSignedThinking(t *testing.T) {
	msgs := anthropicMessages(Request{
		Messages: []Message{
			{Role: RoleUser, Text: "do it"},
			{
				Role:               RoleAssistant,
				Text:               "let me read that",
				Reasoning:          "I should read the file first",
				ReasoningEncrypted: "sig-abc",
				ToolCalls:          []ToolCall{{ID: "t1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)}},
			},
			{Role: RoleTool, ToolCallID: "t1", Text: "contents"},
		},
	})
	// user(text), assistant(thinking + text + tool_use), user(tool_result).
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3: %+v", len(msgs), msgs)
	}
	asst := msgs[1]
	if asst.Role != "assistant" || len(asst.Content) != 3 {
		t.Fatalf("assistant turn should have thinking + text + tool_use: %+v", asst)
	}
	// The signed thinking block must be FIRST (preceding text and tool_use).
	if asst.Content[0].Type != "thinking" {
		t.Fatalf("first block must be thinking, got %q: %+v", asst.Content[0].Type, asst.Content[0])
	}
	if asst.Content[0].Thinking != "I should read the file first" || asst.Content[0].Signature != "sig-abc" {
		t.Errorf("thinking block wrong: %+v", asst.Content[0])
	}
	if asst.Content[1].Type != "text" || asst.Content[2].Type != "tool_use" {
		t.Errorf("text then tool_use should follow the thinking block: %+v", asst.Content)
	}

	// Reasoning WITHOUT a captured signature must NOT be re-emitted as a
	// thinking block (an unsigned thinking block is rejected); the assistant
	// turn falls back to text + tool_use only.
	unsigned := anthropicMessages(Request{
		Messages: []Message{
			{Role: RoleUser, Text: "do it"},
			{
				Role:      RoleAssistant,
				Text:      "ok",
				Reasoning: "thought without a signature",
				ToolCalls: []ToolCall{{ID: "t1", Name: "read", Arguments: json.RawMessage(`{}`)}},
			},
		},
	})
	for _, c := range unsigned[1].Content {
		if c.Type == "thinking" {
			t.Fatalf("unsigned reasoning must not emit a thinking block: %+v", unsigned[1].Content)
		}
	}
}

func TestAnthropicMessagesSkipsEmptyAssistant(t *testing.T) {
	// A reasoning-only assistant turn (no text, no tool calls) must not become
	// an empty content array (the API rejects it).
	msgs := anthropicMessages(Request{
		Messages: []Message{
			{Role: RoleUser, Text: "hi"},
			{Role: RoleAssistant, Reasoning: "thinking…"},
			{Role: RoleUser, Text: "still there?"},
		},
	})
	for _, m := range msgs {
		if len(m.Content) == 0 {
			t.Fatalf("no message should have empty content: %+v", msgs)
		}
	}
}

func TestAnthropicSystemBlocksLeadWithSpoof(t *testing.T) {
	a := &Anthropic{cache: true}
	blocks := a.systemBlocks("my real system prompt")
	if len(blocks) != 2 {
		t.Fatalf("want 2 system blocks (spoof + prompt), got %d", len(blocks))
	}
	if blocks[0].Text != claudeCodeSpoof {
		t.Errorf("first system block must be the Claude Code spoof, got %q", blocks[0].Text)
	}
	if blocks[1].Text != "my real system prompt" || blocks[1].CacheControl == nil {
		t.Errorf("second block should carry the prompt and a cache breakpoint: %+v", blocks[1])
	}
	// With no system prompt, only the spoof is sent.
	if got := (&Anthropic{}).systemBlocks(""); len(got) != 1 || got[0].Text != claudeCodeSpoof {
		t.Errorf("empty system should yield just the spoof: %+v", got)
	}
}

func TestAnthropicEffortSetter(t *testing.T) {
	a := &Anthropic{adaptive: true, effort: "high"}
	if _, ok := interface{}(a).(EffortSetter); !ok {
		t.Fatal("Anthropic should implement EffortSetter")
	}
	if !a.SetEffort("low") || a.Effort() != "low" {
		t.Fatalf("SetEffort(low) failed, effort=%q", a.Effort())
	}
	if a.SetEffort("bogus") {
		t.Error("SetEffort should reject unknown levels")
	}
}

func TestClaudeOAuthToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	// Valid unexpired token.
	write := func(tok string, exp int64) {
		b, _ := json.Marshal(map[string]any{
			"claudeAiOauth": map[string]any{"accessToken": tok, "expiresAt": exp},
		})
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("sk-ant-oat-xyz", time.Now().Add(time.Hour).UnixMilli())
	got, err := claudeOAuthToken(path)
	if err != nil || got != "sk-ant-oat-xyz" {
		t.Fatalf("valid token: got %q err %v", got, err)
	}

	// Expired token is refused with a refresh hint.
	write("sk-ant-oat-old", time.Now().Add(-time.Hour).UnixMilli())
	if _, err := claudeOAuthToken(path); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired token should error with 'expired', got %v", err)
	}

	// Missing file.
	if _, err := claudeOAuthToken(filepath.Join(dir, "nope.json")); err == nil {
		t.Error("missing credentials file should error")
	}
}

// TestAnthropicStreamEmitsIncrementalText is the regression for APP-008: the
// native Anthropic provider must implement Streamer so the default Claude path
// emits text deltas as they arrive instead of blocking on Complete (frozen UI
// mid-turn). It mocks the native Messages SSE flow (message_start →
// content_block_start/delta/stop → message_delta → message_stop) including a
// text block, a thinking delta, and a streamed tool_use whose input arrives as
// partial_json, then asserts the deltas reached the sink and the final Response
// is correctly assembled.
func TestAnthropicStreamEmitsIncrementalText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(interface{ Flush() })
		write := func(s string) { w.Write([]byte(s)); fl.Flush() }
		write("event: message_start\ndata: " + `{"type":"message_start","message":{"usage":{"input_tokens":11,"cache_read_input_tokens":3}}}` + "\n\n")
		// Text block: start, two deltas, stop.
		write("event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n")
		write("event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}` + "\n\n")
		write("event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}` + "\n\n")
		write("event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\n")
		// Thinking delta on its own block.
		write("event: content_block_start\ndata: " + `{"type":"content_block_start","index":1,"content_block":{"type":"thinking","thinking":""}}` + "\n\n")
		write("event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"hmm"}}` + "\n\n")
		write("event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":1,"delta":{"type":"signature_delta","signature":"sig-xyz"}}` + "\n\n")
		write("event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":1}` + "\n\n")
		// Tool use: input streamed as partial JSON.
		write("event: content_block_start\ndata: " + `{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"tu_1","name":"read"}}` + "\n\n")
		write("event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}` + "\n\n")
		write("event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"a\"}"}}` + "\n\n")
		write("event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":2}` + "\n\n")
		write("event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":7}}` + "\n\n")
		write("event: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n")
	}))
	defer srv.Close()

	a := &Anthropic{Model: "claude-sonnet-4-5", apiKey: "k", http: srv.Client()}
	// Point the package URL at the test server for the duration of this test.
	origURL := anthropicURL
	anthropicURL = srv.URL
	defer func() { anthropicURL = origURL }()

	var textChunks []string
	var reasoningChunks []string
	resp, err := a.Stream(context.Background(),
		Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}},
		func(c StreamChunk) {
			switch c.Kind {
			case ChunkText:
				textChunks = append(textChunks, c.Text)
			case ChunkReasoning:
				reasoningChunks = append(reasoningChunks, c.Text)
			}
		})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	// Incremental text must arrive as separate chunks, not one blob at the end.
	if len(textChunks) != 2 || textChunks[0] != "Hel" || textChunks[1] != "lo" {
		t.Fatalf("text chunks = %v, want [Hel lo]", textChunks)
	}
	if len(reasoningChunks) != 1 || reasoningChunks[0] != "hmm" {
		t.Fatalf("reasoning chunks = %v, want [hmm]", reasoningChunks)
	}
	if resp.Text != "Hello" {
		t.Fatalf("resp.Text = %q, want Hello", resp.Text)
	}
	if resp.Reasoning != "hmm" {
		t.Fatalf("resp.Reasoning = %q, want hmm", resp.Reasoning)
	}
	// The thinking block's signature must be captured (interleaved thinking) so
	// the signed block can be re-emitted on the next turn.
	if resp.ReasoningEncrypted != "sig-xyz" {
		t.Fatalf("resp.ReasoningEncrypted = %q, want sig-xyz", resp.ReasoningEncrypted)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "tu_1" || resp.ToolCalls[0].Name != "read" {
		t.Fatalf("tool call wrong: %+v", resp.ToolCalls)
	}
	if string(resp.ToolCalls[0].Arguments) != `{"path":"a"}` {
		t.Fatalf("tool args = %s, want {\"path\":\"a\"}", resp.ToolCalls[0].Arguments)
	}
	if resp.Usage.InputTokens != 11 || resp.Usage.OutputTokens != 7 || resp.Usage.CacheReadTokens != 3 {
		t.Fatalf("usage wrong: %+v", resp.Usage)
	}
	if _, ok := interface{}(a).(Streamer); !ok {
		t.Fatal("Anthropic must implement Streamer")
	}
}

func TestAnthropicProviderResolution(t *testing.T) {
	// Native Anthropic catalog entries now exist for real ids. A known native id
	// stays on the native backend, while an unknown id like claude-fable-5 still
	// leaves the requested provider untouched.
	if p := ResolveProvider("anthropic", "claude-sonnet-4-5-20250929"); p != "anthropic" {
		t.Errorf("native anthropic id should stay anthropic, got %q", p)
	}
	if p := ResolveProvider("anthropic", "claude-fable-5"); p != "anthropic" {
		t.Errorf("explicit anthropic hint should be preserved, got %q", p)
	}
	// The Bedrock id stays on converse (distinct backend, not flipped).
	if p := ResolveProvider("converse", "us.anthropic.claude-opus-4-8"); p != "converse" {
		t.Errorf("bedrock id should stay converse, got %q", p)
	}
}
