package llm

import (
	"encoding/json"
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

func TestAnthropicProviderResolution(t *testing.T) {
	// The native model ids resolve to the anthropic provider, not converse.
	if p := ResolveProvider("converse", "claude-fable-5"); p != "anthropic" {
		t.Errorf("claude-fable-5 should resolve to anthropic, got %q", p)
	}
	// The Bedrock id stays on converse (distinct backend, not flipped).
	if p := ResolveProvider("converse", "us.anthropic.claude-opus-4-8"); p != "converse" {
		t.Errorf("bedrock id should stay converse, got %q", p)
	}
}
